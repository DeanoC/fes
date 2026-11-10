#!/usr/bin/env python3
"""Build the DE25 MENU with a canonical pre-synthesis identity; never program it.

Compile-only retains a candidate. Package export additionally requires zero
unreviewed or waived signoff findings and every reported timing check to pass,
and an independent compilation to reproduce the RBF. Physical qualification
and managed-kit acceptance remain separate gates.
"""
from __future__ import annotations

import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import stat
import subprocess
import sys
import sysconfig
import tempfile
from decimal import Decimal, InvalidOperation

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.build_fes_pcxt_quartus import tree_digest
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, source_input_closure
from scripts.fes_build_common import AuthenticatedTool, BuildError, _require_clean_source as require_clean_source
from scripts.functional_execution import execution_digest, execution_environment as base_execution_environment
from scripts.source_closure import imports as script_imports
from scripts.compiler_read_audit import POLICY as SOURCE_POLICY
from boards.de25nano.scanout import async_inputs
from boards.de25nano.scanout.build import TIMED_RESET_RELEASE, MANAGED_FABRIC, SHARED_FIFO_RESETS

ROOT = Path(__file__).resolve().parents[1]
RECIPE = 'scripts/build_de25_menu_package.py'
ABI = 'boards/de25nano/scanout/generated/fes_de25_scanout.vh'
SOURCE_ROOTS = ['cores/fes-menu/rtl', 'cores/fes-splash/rtl']
REVIEW_INPUTS = ('boards/de25nano/scanout/async_inputs.py',
                 'boards/de25nano/scanout/async_inputs.tcl',
                 'boards/de25nano/scanout/vendor_reference.py')
DEVICE = 'A5EB013BB23BE4SCS'


def _require_clean_source(root, *, identity_version=2, pinned_inputs=(RECIPE, ABI)):
    if tuple(pinned_inputs) != (RECIPE, ABI):
        raise BuildError('DE25 MENU requires its pinned recipe and ABI inputs')
    return require_clean_source(Path(root), identity_version=identity_version, pinned_inputs=pinned_inputs)


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def scanout(root):
    path = Path(root) / 'boards/de25nano/scanout/build.py'
    spec = importlib.util.spec_from_file_location('de25_menu_scanout', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def authenticate_tools(builder, quartus_root):
    # The board locator checks the actual 26.3.0 Pro executable, rather than
    # accepting a caller-supplied version string or a project-only placeholder.
    board = builder.load(builder.HERE.parent / 'build.py', 'de25_menu_tool_locator')
    tool, version = board.locate_quartus(quartus_root)
    lines = [line.strip() for line in version.splitlines()
             if re.search(r'\bVersion 26\.3\.0(?![0-9.])', line) and 'Pro Edition' in line]
    if len(lines) != 1:
        raise BuildError('expected one actual Quartus 26.3.0 Pro version line')
    version = lines[0]
    tool = tool.resolve()
    install = tool.parent.parent.parent
    paths = {'quartus': tool, 'qsys': install / 'qsys/bin/qsys-script'}
    paths.update({name: tool.with_name('quartus_' + name) for name in ('ipgenerate', 'pfg')})
    for name, path in paths.items():
        if not path.is_file() or not os.access(path, os.X_OK):
            raise BuildError('missing executable DE25 build tool: ' + name)
    identities = {name: version + ' sha256:' + sha(path) for name, path in paths.items()}
    return paths, identities


def _authenticate_tools(root, *, cache_root=None):
    # Quartus is an explicitly selected installed toolchain; the common
    # producer API's source-toolchain cache does not select or replace it.
    paths, identities = authenticate_tools(scanout(root), os.environ.get('QUARTUS_ROOTDIR', ''))
    return {name: AuthenticatedTool(path, identities[name]) for name, path in paths.items()}


def _license_bytes(path):
    # A local file is selected explicitly; ambient license/server settings never
    # enter the controlled compiler environment. Refuse devices and linked files.
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or not 0 < info.st_size <= 1024 * 1024:
            raise BuildError('Quartus license must be a nonempty regular file, at most 1 MiB')
        data = stream.read(1024 * 1024 + 1)
        after = os.fstat(stream.fileno())
        if (len(data) != info.st_size or
                (after.st_size, after.st_mtime_ns, after.st_ctime_ns) !=
                (info.st_size, info.st_mtime_ns, info.st_ctime_ns)):
            raise BuildError('Quartus license changed while reading')
        return data


def execution_environment(home, tools, *, license_file=None):
    env = base_execution_environment(home, tools)
    if license_file is not None:
        # Snapshot the selected bytes once. Later source-file edits cannot change
        # the license used between the signoff and independent repeat fit.
        data = _license_bytes(license_file)
        target = Path(home) / 'quartus-license.dat'
        with target.open('xb') as stream:
            stream.write(data)
        target.chmod(0o400)
        env['LM_LICENSE_FILE'] = str(target)
    return env


def license_execution_inputs(env):
    normalized = dict(env, HOME='<private-build-home>')
    if 'LM_LICENSE_FILE' not in env:
        return normalized, None
    path = Path(env['HOME']) / 'quartus-license.dat'
    if env['LM_LICENSE_FILE'] != str(path):
        raise BuildError('Quartus license must be the selected private snapshot')
    data = _license_bytes(path)
    normalized['LM_LICENSE_FILE'] = '<private-build-home>/quartus-license.dat'
    return normalized, {'sha256': hashlib.sha256(data).hexdigest(), 'bytes': len(data)}


def execution_inputs(paths, env, _router_device=0):
    # Quartus reads device databases, Qsys, Tcl, Java and IP outside its shell
    # wrapper. Bind the complete explicitly selected installation, not just sh.
    install = paths['quartus'].parent.parent.parent
    helpers = {'python': Path(sys.executable), 'bash': Path('/bin/bash'), 'sh': Path('/bin/sh')}
    for name in ('as', 'ld', 'git', 'uname', 'dirname', 'basename', 'which',
                 'tail', 'grep', 'sed', 'uniq', 'env', 'locale', 'awk'):
        path = shutil.which(name, path=env['PATH'])
        if path is None:
            raise BuildError('missing DE25 build helper: ' + name)
        helpers[name] = Path(path)
    stdlib = Path(sysconfig.get_path('stdlib'))
    support = {str(p): sha(p) for p in sorted(stdlib.rglob('*')) if p.is_file()
               and not {'__pycache__', 'site-packages', 'dist-packages'} & set(p.relative_to(stdlib).parts)
               and p.suffix not in ('.pyc', '.pyo')}
    if not support:
        raise BuildError('missing Python standard-library support')
    native = paths['quartus'].parent.parent / 'linux64'
    executables = [*helpers.values(),
        *(native / ('quartus_' + name) for name in ('sh', 'map', 'fit', 'asm', 'sta', 'pfg')),
        *(Path(p) for p in support if p.endswith('.so'))]
    # Pro's actual adm/qenv.sh prepends BOTH directories. The 17.0 PC/XT
    # inspector uses only quartus/linux64 and cannot resolve Pro's qcore DLLs.
    loader_paths = (native, install / 'qcore/linux64')
    if not all(path.is_dir() for path in loader_paths):
        raise BuildError('missing Quartus Pro native/qcore loader directory')
    libraries = {}
    for binary in sorted(set(executables)):
        with binary.open('rb') as stream:
            if stream.read(4) != b'\x7fELF':
                continue
        probe_env = dict(env, LD_LIBRARY_PATH=':'.join(map(str, loader_paths))) if binary.parent == native else env
        result = subprocess.run(['/usr/bin/ldd', str(binary)], env=probe_env, capture_output=True, text=True)
        output = result.stdout + result.stderr
        if 'not found' in output or (result.returncode and
                'not a dynamic executable' not in output and 'statically linked' not in output):
            raise BuildError('cannot identify runtime libraries for ' + str(binary))
        for name in re.findall(r'(?:=>\s+|^\s*)(/[^\s]+)', result.stdout, re.M):
            libraries[name] = sha(name)
    for name in ('/etc/os-release', '/etc/ld.so.cache'):
        if Path(name).is_file():
            libraries[name] = sha(name)
    normalized_env, license_inputs = license_execution_inputs(env)
    return {'version': 2, 'toolchain': 'quartus',
        'installations': {str(install): tree_digest(install)},
        'quartus_native_loader_paths': list(map(str, loader_paths)),
        'helpers': {name: {'path': str(p), 'sha256': sha(p)} for name, p in helpers.items()},
        'python': {'executable': sys.executable, 'version': sys.version,
                   'prefix': sys.prefix, 'base_prefix': sys.base_prefix, 'support': support},
        'runtime_libraries': dict(sorted(libraries.items())),
        'system': platform.system(), 'machine': platform.machine(), 'kernel': platform.release(),
        'environment': normalized_env,
        **({'license_file': license_inputs} if license_inputs is not None else {})}


def captured_source_closure(root, source_roots, captured, recipe, review_inputs):
    """Bind the staged board bytes and producer/qualification dependencies.

    The captured builders explicitly materialize their HDL and IP inputs. Their
    capture declarations remain hashed, so selecting another file changes the
    identity. Unrelated board diagnostics and script entrypoints are not inputs.
    The ordinary exporter still re-enumerates and checks every declared root.
    """
    roots = set(source_roots) | set(review_inputs)
    helpers = {recipe, *(name for name in review_inputs if name.startswith('scripts/'))}
    for helper in helpers:
        roots.update(script_imports(Path(helper).stem, root))
    roots.update(helpers)
    paths = {Path(name).relative_to('sources/misteross').as_posix(): data
             for name, data in captured.items()}
    roots.update(paths)
    roots = sorted(roots)
    closure = source_input_closure(root, roots, policy=SOURCE_POLICY)
    for name, data in paths.items():
        if closure.get(name) != hashlib.sha256(data).hexdigest():
            raise BuildError('captured DE25 source differs from canonical closure: ' + name)
    return roots, closure


def create_build_record(root, repository, revision, identities, execution, captured=None, *, identity_version=2):
    if type(identity_version) is not int or identity_version != 2:
        raise BuildError('DE25 MENU requires functional build identity 2')
    if execution.get('toolchain') != 'quartus' or set(identities) != {'quartus', 'qsys', 'ipgenerate', 'pfg'}:
        raise BuildError('DE25 MENU requires actual controlled Quartus execution inputs')
    root = Path(root).resolve()
    if captured is None:
        captured = scanout(root).capture(revision)
    top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())
    roots, closure = captured_source_closure(root, SOURCE_ROOTS, captured, RECIPE, REVIEW_INPUTS)
    fields = {'format': 2, 'toolchain': 'quartus', 'repository': repository, 'revision': revision,
        'source_path': root.relative_to(top).as_posix(), 'source_roots': roots,
        'source_inputs': closure, 'recipe': RECIPE, 'recipe_sha256': sha(root / RECIPE),
        'abi_definition': ABI, 'abi_definition_sha256': sha(root / ABI), 'dependencies': {},
        'tools': dict(identities), 'parameters': {'core_id': 'fes.menu', 'device': DEVICE,
            'source_closure_policy': SOURCE_POLICY,
            'hps_initialization': 'HPS FIRST', 'linux_dma': True,
            'f2sdram_master_profile': 'scanout-only', 'seed': 1, 'threads': 2,
            'signoff_policy': async_inputs.POLICY,
            'hdmi_output_policy': async_inputs.vendor_reference.POLICY,
            'reset_release_timing': TIMED_RESET_RELEASE,
            'fabric_initiator_policy': MANAGED_FABRIC,
            'fifo_reset_sharing': SHARED_FIFO_RESETS,
            'pixel_clock_hz': 74250000, 'system_clock_hz': 100000000,
            'execution_sha256': execution_digest(execution)}}
    return encode_build_record(fields)


def timing_evidence(directory, *, async_review=None, audio_review=None, revision='golden_top'):
    if revision not in ('golden_top', 'base', 'persona_a', 'persona_b'):
        raise BuildError('unknown DE25 timing revision')
    if revision != 'golden_top' and (async_review is not None or audio_review is not None):
        raise BuildError('standard PR revisions cannot inherit internal16K timing reviews')
    reports = Path(directory) / 'combined/hps/project/output_files'
    drc = (reports / (revision + '.tq.drc.signoff.rpt')).read_text()
    declared = set(re.findall(r'Design Assistant \(Signoff\) Results - (\d+) of (\d+) Rules Failed', drc))
    if len(declared) != 1:
        raise BuildError('missing or inconsistent DE25 signoff summary')
    failed, total = map(int, next(iter(declared)))
    rows = re.findall(r'^; ([A-Z]+-\d+) - [^;]+; (Critical|High|Medium|Low|Info)\s*;\s*(\d+)\s*;\s*(\d+)\s*;', drc, re.M)
    expected_total = 85 if revision == 'golden_top' else 88
    if total != expected_total or len(rows) != total or len({r[0] for r in rows}) != total or failed != sum(int(r[2]) > 0 for r in rows):
        raise BuildError('incomplete or inconsistent DE25 signoff rule inventory')
    if revision != 'golden_top' and not {'BBD-60000', 'BBD-60001', 'BBD-60002'} <= {r[0] for r in rows}:
        raise BuildError('missing PR partition-boundary signoff rules')
    details = {}
    for section in re.split(r'(?m)^Status:\s*', drc)[1:]:
        match = re.search(r'^(PASS|FAIL)\s+Severity:\s*(Critical|High|Medium|Low|Info)\s+Number of violations:\s*(\d+)', section)
        rule = re.search(r'^; ([A-Z]+-\d+) - [^;]+;', section, re.M)
        if match is None or rule is None or rule[1] in details:
            raise BuildError('incomplete DE25 signoff rule detail')
        details[rule[1]] = match.groups()
    if len(details) != total or any(details.get(n) != ('FAIL' if int(v) else 'PASS', s, v) for n, s, v, _ in rows):
        raise BuildError('DE25 signoff summary differs from rule details')
    text = (reports / (revision + '.sta.summary')).read_text()
    blocks = re.findall(r'^Type\s*:\s*([^\n]+)\nSlack\s*:\s*([^\s]+)[ \t]*(?:\nTNS\s*:\s*([^\n]+)\nCorner:\s*([^\n]+))?', text, re.M)
    if len(blocks) != len(re.findall(r'^Type\s*:', text, re.M)):
        raise BuildError('unparsed DE25 timing check')
    checks = []
    standard = set()
    for label, slack, tns, corner in blocks:
        matched = re.fullmatch(r"(Setup|Setup Data Delay|Hold|Recovery|Removal|Minimum Pulse Width) '([^'\n]+)'", label)
        skew = re.fullmatch(r"(.+ Model) Max Skew 'set_max_skew'", label)
        if matched and tns and corner:
            key = matched.groups()
            if key in standard:
                raise BuildError('duplicate DE25 clock timing check')
            standard.add(key)
            checks.append((*key, slack, tns, corner))
        elif skew and not tns and not corner:
            checks.append(('Max Skew', 'set_max_skew', slack, '0', skew[1]))
        elif label == "Net Delay 'set_net_delay'" and not tns and not corner:
            checks.append(('Net Delay', 'set_net_delay', slack, '0', 'not-reported'))
        else:
            raise BuildError('unknown or incomplete DE25 timing check: ' + label)
    # Quartus emits Setup Data Delay only when the fitted design has paths
    # with that check. Apply the same completeness and numeric checks to it;
    # all seven common families remain required for every DE25 candidate.
    if {r[0] for r in checks} - {'Setup Data Delay'} != {'Setup', 'Hold', 'Recovery', 'Removal', 'Minimum Pulse Width', 'Max Skew', 'Net Delay'}:
        raise BuildError('missing DE25 timing check family')
    for clock in ('pll_inst|iopll_0_outclk0', 'fes_video_inst|video_clock|iopll_0_outclk0', 'adv7513_config'):
        if not all(any(row[:2] == (kind, clock) for row in checks) for kind in ('Setup', 'Hold', 'Recovery', 'Removal')):
            raise BuildError('missing required DE25 clock timing: ' + clock)
    try:
        passing = all(Decimal(v).is_finite() and Decimal(v) >= 0 for r in checks for v in r[2:4])
    except InvalidOperation as error:
        raise BuildError('invalid DE25 timing number') from error
    reviewed = []
    proof = None
    audio_proof = None
    if audio_review is not None:
        if async_review is None:
            raise BuildError('DE25 audio-clock reference requires the existing input/vendor review')
        from boards.de25nano.scanout import audio_reference
        audio_proof = audio_reference.verify(directory, audio_review)
    if async_review is not None:
        proof = async_inputs.verify(directory, async_review)
        findings = next(r for r in rows if r[0] == 'TMC-20011')
        if int(findings[2]):
            if findings[2:] != ('2', '0'):
                raise BuildError('DE25 reviewed input finding count/waivers changed')
            reviewed.append({'id': 'TMC-20011', 'violations': 2,
                             'ports': async_inputs.input_findings(drc)})
        if 'vendor_hdmi_reference' in proof:
            outputs = next(r for r in rows if r[0] == 'TMC-20012')
            if int(outputs[2]):
                count = 32 if audio_proof is not None else 30
                if outputs[2:] != (str(count), '0'):
                    raise BuildError('DE25 vendor HDMI output finding count/waivers changed')
                reviewed.append({'id': 'TMC-20012', 'violations': count,
                    'ports': audio_reference.output_findings(drc) if audio_proof is not None else
                             async_inputs.vendor_reference.output_findings(drc),
                    'basis': audio_reference.POLICY if audio_proof is not None else
                             async_inputs.vendor_reference.POLICY})
    unreviewed = failed - len(reviewed)
    return {'signoff_rules': total, 'failed_rules': failed,
        'unreviewed_failed_rules': unreviewed, 'reviewed_findings': reviewed,
        **({'async_input_review': proof} if proof is not None else {}),
        **({'audio_clock_reference_review': audio_proof} if audio_proof is not None else {}),
        'rules': [{'id': n, 'severity': s, 'violations': int(v), 'waived': int(w)} for n, s, v, w in rows],
        'timing_checks': [{'type': t, 'clock': c, 'slack_ns': s,
            **({'tns_ns': n} if t not in ('Max Skew', 'Net Delay') else {}),
            **({'corner': k} if k != 'not-reported' else {})} for t, c, s, n, k in checks],
        'all_reported_timing_pass': passing,
        'package_timing_admission': passing and unreviewed == 0 and all(int(r[3]) == 0 for r in rows),
        'reports_sha256': {name: sha(reports / name) for name in
                          (revision + '.sta.summary', revision + '.tq.drc.signoff.rpt')}}


def manifest(record, payload):
    fields = json.loads(record)
    return encode_manifest({'format': 2,
        'core': {'id': 'fes.menu', 'name': 'FES Menu Display', 'version': '1.0.0',
                 'description': 'Runtime-owned fixed 720p DE25 allocated-buffer menu display'},
        'target': {'platform': 'de25_nano', 'device': DEVICE, 'programming_profile': 'de25-linux-menu-v1'},
        'payload': {'file': 'core.rbf', 'size': payload.stat().st_size, 'sha256': sha(payload)},
        'abi': {'id': 'fes.application', 'major': 1, 'minor': 0},
        'interfaces': [{'id': n, 'major': 1, 'minor': m, 'required': True} for n, m in (
            ('fes.video.fixed-720p60', 0), ('fes.video.menu-display', 0), ('fes.fabric.de25-scanout', 3))],
        'build': {'id': build_identity(record), 'repository': fields['repository'], 'revision': fields['revision'],
            'recipe_sha256': fields['recipe_sha256'],
            'toolchain': '; '.join(f'{n} {v}' for n, v in sorted(fields['tools'].items()))}})


@contextmanager
def controlled_environment(env):
    original = dict(os.environ)
    try:
        os.environ.clear()
        os.environ.update(env)
        yield
    finally:
        os.environ.clear()
        os.environ.update(original)


def build(root=ROOT, *, quartus_root, vendor_archive, output=None, package_output=None, compile_only=False,
          license_file=None):
    root = Path(root).resolve()
    if vendor_archive is None:
        raise BuildError('DE25 MENU requires --vendor-archive or FES_DE25_VENDOR_ARCHIVE')
    if compile_only and package_output is not None:
        raise BuildError('compile-only cannot select a package destination')
    if output is not None and not compile_only and not Path(output).resolve().is_relative_to(root):
        raise BuildError('package build output must remain inside its declared source module')
    repository, revision = _require_clean_source(root, pinned_inputs=(RECIPE, ABI))
    builder = scanout(root)
    captured = builder.capture(revision)
    paths, identities = authenticate_tools(builder, quartus_root)
    with tempfile.TemporaryDirectory(prefix='fes-de25-menu-home-') as home:
        env = execution_environment(Path(home), paths, license_file=license_file)
        execution = execution_inputs(paths, env)
        record = create_build_record(root, repository, revision, identities, execution, captured)
        selected_id = build_identity(record)
        if output is None:
            parent = root / 'build/de25-menu' / selected_id
            parent.mkdir(parents=True, exist_ok=True)
            output = Path(tempfile.mkdtemp(prefix='attempt-', dir=parent))
        else:
            output = Path(output).resolve()
            output.mkdir(parents=True, exist_ok=False)
        (output / 'build-inputs.json').write_bytes(record)
        (output / 'execution.json').write_text(json.dumps(execution, sort_keys=True, indent=2) + '\n')
        print('DE25 MENU build: ' + str(output), flush=True)
        with controlled_environment(env):
            selected, combined, hps, display = builder.prepare(vendor_archive, output / 'signoff', revision,
                hps_first=True, linux_dma=True, package_build_id=selected_id)
            version = identities['quartus'].split(' sha256:')[0]
            builder.generate(selected, combined, hps, display, paths['quartus'], version)
            builder.compile_project(selected, hps, display, paths['quartus'])
            review = async_inputs.collect(selected, paths['quartus'].with_name('quartus_sta'),
                                          vendor_archive=vendor_archive)
            timing = timing_evidence(selected, async_review=review)
            (output / 'timing.json').write_text(json.dumps(timing, sort_keys=True, indent=2) + '\n')
            if not compile_only:
                if not timing['package_timing_admission']:
                    raise BuildError('DE25 MENU package blocked by reported timing/signoff findings; candidate retained')
                repeated, c2, h2, d2 = builder.prepare(vendor_archive, output / 'repeat', revision,
                    hps_first=True, linux_dma=True, package_build_id=selected_id)
                builder.generate(repeated, c2, h2, d2, paths['quartus'], version)
                builder.compile_project(repeated, h2, d2, paths['quartus'])
                repeat_review = async_inputs.collect(repeated, paths['quartus'].with_name('quartus_sta'),
                                                     vendor_archive=vendor_archive)
                repeat_timing = timing_evidence(repeated, async_review=repeat_review)
                (output / 'repeat-timing.json').write_text(json.dumps(repeat_timing, sort_keys=True, indent=2) + '\n')
                if not repeat_timing['package_timing_admission'] or sha(selected / 'combined/hps/linux-boot.core.rbf') != sha(repeated / 'combined/hps/linux-boot.core.rbf'):
                    raise BuildError('DE25 MENU independent rebuild failed timing or changed RBF')
        if (_require_clean_source(root, pinned_inputs=(RECIPE, ABI)) != (repository, revision) or
                authenticate_tools(builder, quartus_root) != (paths, identities) or
                execution_inputs(paths, env) != execution or
                create_build_record(root, repository, revision, identities, execution, builder.capture(revision)) != record):
            raise BuildError('DE25 MENU source/tool/execution inputs changed during build')
        # The generic exporter requires the payload under its declared module.
        payload = output / 'core.rbf'
        shutil.copyfile(selected / 'combined/hps/linux-boot.core.rbf', payload)
        (output / 'build-summary.json').write_text(json.dumps({'build_id': selected_id,
            'revision': revision, 'classification': 'de25-menu-compile-candidate' if compile_only else 'de25-menu-package-candidate',
            'rbf_sha256': sha(payload), 'package_timing_admission': timing['package_timing_admission'],
            'hardware_acceptance': False, 'normal_kit_acceptance': False}, sort_keys=True, indent=2) + '\n')
        if compile_only:
            return output
        encoded = manifest(record, payload)
        (output / 'manifest.toml').write_bytes(encoded)
        return export_package(encoded, payload, Path(package_output or root / 'build/packages'))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    parser.add_argument('--license-file', type=Path,
                        help='explicit local Quartus license; private copy and hash are bound to execution')
    parser.add_argument('--vendor-archive', type=Path, default=os.environ.get('FES_DE25_VENDOR_ARCHIVE') or None)
    parser.add_argument('--cache-root', type=Path, help='shared producer API; Quartus uses the selected installed tools')
    parser.add_argument('--identity-version', type=int, choices=(2,), default=2)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--package-output', type=Path)
    parser.add_argument('--compile-only', action='store_true')
    args = parser.parse_args(argv)
    try:
        print(build(root=args.root, quartus_root=args.quartus_root, vendor_archive=args.vendor_archive,
            output=args.output, package_output=args.package_output, compile_only=args.compile_only,
            license_file=args.license_file))
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        parser.exit(1, 'de25-menu: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
