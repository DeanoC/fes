#!/usr/bin/env python3
"""Build a sealed PC/XT Quartus profile with the pinned board toolchain.

Every declared seed must pass the complete reported internal timing inventory.
An independent seed-1 rebuild must reproduce the RBF. External interface/reset
qualification and exact-artifact hardware acceptance remain separate gates.
"""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import sysconfig
import tempfile
import tomllib

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import quartus_fes_pcxt as board
from scripts.export_core_package import (build_identity, encode_build_record,
    export_package, source_input_closure)
from scripts.core_package import encode_manifest
from scripts.fes_build_common import BuildError, _require_clean_source as require_clean_source
from scripts.functional_execution import execution_digest, execution_environment
from scripts.quartus_tools import locate_quartus, quartus_version_line

ROOT = Path(__file__).resolve().parents[1]
RECIPE = 'scripts/build_fes_pcxt_quartus.py'
LOCK = 'cores/fes-pcxt/quartus.lock'
ABI = 'cores/fes-common/generated/fes_computer.vh'
REGISTRY = ROOT.parents[1] / 'config/core-recipes.toml'


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def selected_recipe(root=ROOT, core_id='fes.pcxt'):
    data = tomllib.loads((Path(root).parents[1] / 'config/core-recipes.toml').read_text())
    entry = next(r for r in data['recipes'] if r['core_id'] == core_id)
    if entry.get('toolchain') != 'quartus' or entry['producer_script'] != RECIPE:
        raise BuildError('PC/XT must select its Quartus package producer')
    return entry


def source_roots(root):
    # Keep the complete core and vendor directories, including documentation.
    # Outside them capture every board input and the producer's Python helpers.
    roots = {'cores/fes-pcxt', 'cores/fes-common/vendor/kf8259',
             'cores/fes-common/vendor/kf8253'}
    paths = [str(p) for p in board.input_paths(Path(root))]
    paths += [RECIPE, 'scripts/core_package.py', 'scripts/export_core_package.py',
              'scripts/fes_build_common.py', 'scripts/functional_execution.py',
              'scripts/compiler_read_audit.py', 'scripts/source_provenance.py',
              'scripts/source_repository.py', ABI]
    roots.update(p for p in paths if not any(p == r or p.startswith(r + '/') for r in roots))
    return sorted(roots)


def _require_clean_source(root, *, identity_version=2):
    if identity_version != 2:
        raise BuildError('PC/XT requires functional identity version 2')
    return require_clean_source(Path(root), pinned_inputs=(RECIPE, LOCK, ABI))


@dataclass(frozen=True)
class Tool:
    path: Path
    identity: str


def authenticate_tools(root, *, cache_root=None, core_id='fes.pcxt'):
    root = Path(root)
    lock = tomllib.loads((root / LOCK).read_text())
    _, quartus = locate_quartus(os.environ.get('QUARTUS_ROOTDIR', ''), root)
    version = quartus_version_line(quartus)
    if version != lock['version'] or selected_recipe(root, core_id)['quartus']['version'] != version:
        raise BuildError('Quartus version differs from the PC/XT lock/recipe')
    candidates = ([Path(os.environ['FES_PCXT_YOSYS'])] if os.environ.get('FES_PCXT_YOSYS') else
                  sorted(Path(cache_root).glob('slots/*/install/bin/yosys')) if cache_root else [])
    yosys = next((p for p in candidates if p.is_file() and not p.is_symlink()
                  and sha(p) == lock['yosys_sha256']), None)
    if yosys is None:
        raise BuildError('set FES_PCXT_YOSYS to the PC/XT lock\'s Yosys binary')
    yosys = yosys.resolve()
    yversion = subprocess.check_output([str(yosys), '-V'], text=True).strip()
    if 'git sha1 ' + lock['yosys_commit'][:9] not in yversion:
        raise BuildError('Yosys version differs from the PC/XT lock')
    return {'yosys': Tool(yosys, yversion + ' sha256:' + sha(yosys)),
            'quartus': Tool(quartus.resolve(), version + ' sha256:' + sha(quartus))}


def tree_digest(root):
    digest, count = hashlib.sha256(), 0
    for path in sorted(Path(root).rglob('*')):
        if path.is_file():
            digest.update(str(path.relative_to(root)).encode() + b'\0')
            digest.update(bytes.fromhex(sha(path)))
            count += 1
    if not count:
        raise BuildError('empty tool installation')
    return {'sha256': digest.hexdigest(), 'files': count}


def runtime_dependencies(executables, env, quartus_native):
    files = {}
    for binary in sorted(set(executables)):
        with binary.open('rb') as stream:
            if stream.read(4) != b'\x7fELF':
                continue
        # qenv.sh prepends this directory for the native Quartus processes.
        native_env = dict(env, LD_LIBRARY_PATH=str(quartus_native)) if binary.parent == quartus_native else env
        result = subprocess.run(['/usr/bin/ldd', str(binary)], env=native_env,
                                capture_output=True, text=True, check=False)
        output = result.stdout + result.stderr
        if 'not found' in output or (result.returncode and
                'not a dynamic executable' not in output and 'statically linked' not in output):
            raise BuildError('cannot identify runtime libraries for ' + str(binary))
        for name in re.findall(r'(?:=>\s+|^\s*)(/[^\s]+)', result.stdout, re.MULTILINE):
            files[name] = sha(Path(name))
    return dict(sorted(files.items()))


def execution_inputs(paths, env, _unused_gpu_device=0):
    # Quartus uses device databases, Tcl and native DLLs outside its wrapper.
    # Bind the complete install trees; no GPU or driver enters this lane.
    installations = {str(paths['quartus'].parent.parent): tree_digest(paths['quartus'].parent.parent),
                     str(paths['yosys'].parent.parent): tree_digest(paths['yosys'].parent.parent)}
    helpers = {'python': Path(sys.executable), 'bash': Path('/bin/bash'), 'sh': Path('/bin/sh')}
    for name in ('as', 'ld', 'git', 'uname', 'dirname', 'basename', 'which',
                 'tail', 'grep', 'sed', 'uniq', 'env', 'locale', 'awk'):
        path = shutil.which(name, path=env['PATH'])
        if path is None:
            raise BuildError('missing build helper: ' + name)
        helpers[name] = Path(path)
    # Bind the interpreter actually used by build()/the proof subprocesses,
    # including its standard-library sources and native extension modules.
    # Bytecode caches are derived from these sources and change during imports.
    stdlib = Path(sysconfig.get_path('stdlib'))
    support = {str(p): sha(p) for p in sorted(stdlib.rglob('*')) if p.is_file()
               and not {'__pycache__', 'site-packages', 'dist-packages'} & set(p.relative_to(stdlib).parts)
               and p.suffix not in ('.pyc', '.pyo')}
    if not support:
        raise BuildError('missing Python standard-library support')
    native = paths['quartus'].parent.parent / 'linux64'
    executables = [*helpers.values(), paths['yosys'], paths['yosys'].with_name('yosys-abc'),
                   *(native / ('quartus_' + name) for name in ('sh', 'map', 'fit', 'asm', 'sta')),
                   *(Path(p) for p in support if p.endswith('.so'))]
    libraries = runtime_dependencies(executables, env, native)
    for name in ('/etc/os-release', '/etc/ld.so.cache'):
        if Path(name).is_file():
            libraries[name] = sha(name)
    return {'version': 2, 'toolchain': 'quartus', 'installations': installations,
            'helpers': {name: {'path': str(p), 'sha256': sha(p)} for name, p in helpers.items()},
            'python': {'executable': sys.executable, 'version': sys.version,
                       'prefix': sys.prefix, 'base_prefix': sys.base_prefix, 'support': support},
            'runtime_libraries': dict(sorted(libraries.items())),
            'system': platform.system(), 'machine': platform.machine(), 'kernel': platform.release(),
            'environment': dict(env, HOME='<private-build-home>')}


def dos_assets(root):
    """Verify the checked-in redistributable BIOS and FreeDOS media inputs."""
    root = Path(root)
    core = root / 'cores/fes-pcxt'
    metadata = json.loads((core / 'firmware/hardware-dos.json').read_text())
    floppy, bios = metadata['floppy'], metadata['bios']
    for descriptor in (floppy, bios):
        path = core / descriptor['file']
        if path.stat().st_size != descriptor['size'] or sha(path) != descriptor['sha256']:
            raise BuildError('PC/XT hardware DOS input differs from its provenance lock: ' + path.name)
    bios_lock = json.loads((core / 'sim/bios.lock').read_text())['bios']
    if (bios_lock['sha256'] != bios['sha256'] or bios_lock['url'] != bios['url']
            or bios_lock['source'] != bios['source'] or bios_lock['license'] != bios['license']):
        raise BuildError('PC/XT DOS BIOS differs from the simulation BIOS lock')
    fd_spec = json.loads((core / 'sim/freedos.lock').read_text())
    floppy_lock = fd_spec['package_image']
    locked_sources = [
        {'url': fd_spec['packages'][name]['repository'],
         'commit': fd_spec['packages'][name]['commit'],
         'license': fd_spec['packages'][name]['license']}
        for name in ('kernel', 'freecom')]
    if (floppy_lock['sha256'] != floppy['sha256'] or floppy_lock['bytes'] != floppy['size']
            or floppy_lock['file'] != floppy['file']
            or floppy_lock['source_urls'] != [item['url'] for item in locked_sources]
            or floppy_lock['license'] != floppy['license']
            or floppy['sources'] != locked_sources):
        raise BuildError('PC/XT DOS floppy differs from the FreeDOS lock')
    font = bytearray(4096)
    rom = (core / bios['file']).read_bytes()
    offset = json.loads((core / 'sim/bios.lock').read_text())['ascii_font']['rom_offset']
    for char in range(128):
        font[char * 16:char * 16 + 8] = rom[offset + char * 8:offset + char * 8 + 8]
    return metadata, bytes(font)


def create_build_record(root, repository, revision, tool_identities, *, identity_version=2,
                        execution=None, core_id='fes.pcxt', dos_profile=False):
    if identity_version != 2 or execution is None or execution.get('toolchain') != 'quartus':
        raise BuildError('Quartus package requires controlled functional execution inputs')
    root = Path(root).resolve()
    top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())
    roots = source_roots(root)
    entry = selected_recipe(root, core_id)
    parameters = {'device': board.DEVICE, 'core_id': core_id,
                  'hardware_dos': dos_profile,
                  'half_rate_board': False, 'phased_hdmi_clock': True,
                  'enable_fes_computer': True, 'video_mailbox_contract': True,
                  'system_clock_hz': 100000000, 'pixel_clock_hz': 74250000,
                  'hdmi_phase_ps': 8979,
                  'quartus_recipe_sha256': hashlib.sha256(json.dumps(entry, sort_keys=True).encode()).hexdigest(),
                  'execution_sha256': execution_digest(execution)}
    if dos_profile:
        metadata, font = dos_assets(root)
        parameters.update(freedos_image_sha256=metadata['floppy']['sha256'],
                          freedos_source_urls='; '.join(item['url'] for item in metadata['floppy']['sources']),
                          glabios_rom_sha256=metadata['bios']['sha256'],
                          glabios_font_sha256=hashlib.sha256(font).hexdigest())
    fields = {'format': 2, 'toolchain': 'quartus', 'repository': repository, 'revision': revision,
              'source_path': root.relative_to(top).as_posix(), 'source_roots': roots,
              'source_inputs': source_input_closure(root, roots),
              'recipe': RECIPE, 'recipe_sha256': sha(root / RECIPE),
              'abi_definition': ABI, 'abi_definition_sha256': sha(root / ABI),
              'dependencies': {}, 'tools': dict(tool_identities), 'parameters': parameters}
    return encode_build_record(fields)


def require_timing(summary, seeds, *, dos_profile=False):
    if (summary.get('status') != 'diagnostic-captured' or summary.get('seeds') != seeds
            or not summary.get('video_mailbox_contract')
            or summary.get('profile', {}).get('HALF_RATE_BOARD') != 0
            or summary.get('profile', {}).get('ENABLE_FES_COMPUTER') != 1
            or summary.get('profile', {}).get('HARDWARE_DOS') != int(dos_profile)
            or len(summary.get('runs', [])) != len(seeds)
            or summary.get('elaboration', {}).get('hps_primitive_instances') != {
                'hdmi_i2c': 'cyclonev_hps_interface_peripheral_i2c',
                'fes_computer_runtime_hps_gp': 'cyclonev_hps_interface_mpu_general_purpose'}):
        raise BuildError('incomplete or mismatched Quartus package configuration')
    for seed, run in zip(seeds, summary['runs']):
        if (run.get('seed') != seed or not run.get('capture_complete')
                or not run.get('reported_endpoint_checks_pass')
                or len(run.get('minimum_pulse_width', {})) != 4
                or any(r['failures'] for r in run['minimum_pulse_width'].values())
                or len(run.get('mailbox_interstage', [])) != 24
                or any(r['slack_ns'].startswith('-') for r in run['mailbox_interstage'])
                or any(run.get('timing_coverage_issues', {}).get(k, 1)
                       for k in ('loops', 'latches', 'multiple_clock', 'generated_clock',
                                 'partial_input_delay', 'partial_output_delay', 'io_min_max_delay_consistency',
                                 'reference_pin', 'pll_cross_check', 'uncertainty'))
                or len(run.get('no_clock_registers', [])) != run.get('timing_coverage_issues', {}).get('no_clock')
                or any(not re.fullmatch(r'(hdmi_i2c|fes_computer_runtime_hps_gp)~FF_[0-9]+', name)
                       for name in run.get('no_clock_registers', []))):
            raise BuildError('Quartus internal timing failed for seed ' + str(seed))


def manifest(record, payload, *, dos_profile=False, metadata=None):
    fields = json.loads(record)
    if dos_profile:
        floppy = metadata['floppy']
        sources = '; '.join(f"{item['url']} ({item['license']})" for item in floppy['sources'])
        description = (f"FreeDOS 1.4 boots to A:\\> from a read-only XT floppy served through SDRAM. "
                       f"Image source: {sources}; SHA-256 {floppy['sha256']}. See PC/XT third-party notices.")
        core = {'id': 'fes.pcxt-dos', 'name': 'FES PC/XT FreeDOS', 'version': '0.1.0',
                'description': description}
    else:
        core = {'id': 'fes.pcxt', 'name': 'FES PC/XT Diagnostic', 'version': '0.2.0',
                'description': 'Quartus 8088 SDRAM/DMA diagnostic with a static CGA completion signature'}
    return encode_manifest({'format': 2,
        'core': core,
        'target': {'platform': 'de10_nano', 'device': board.DEVICE, 'programming_profile': 'fes-gp-v1'},
        'payload': {'file': 'core.rbf', 'size': payload.stat().st_size, 'sha256': sha(payload)},
        'abi': {'id': 'fes.computer', 'major': 1, 'minor': 0},
        'interfaces': [{'id': 'fes.video.fixed-720p60', 'major': 1, 'minor': 0, 'required': True}],
        'build': {'id': build_identity(record), 'repository': fields['repository'],
                  'revision': fields['revision'], 'recipe_sha256': fields['recipe_sha256'],
                  'toolchain': '; '.join(f'{k} {fields["tools"][k]}' for k in sorted(fields['tools']))}})


def build(root=ROOT, *, package_output=None, cache_root=None, identity_version=2, dos_profile=False):
    root = Path(root).resolve()
    core_id = 'fes.pcxt-dos' if dos_profile else 'fes.pcxt'
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    tools = authenticate_tools(root, cache_root=cache_root, core_id=core_id)
    paths = {name: tool.path for name, tool in tools.items()}
    identities = {name: tool.identity for name, tool in tools.items()}
    with tempfile.TemporaryDirectory(prefix='fes-pcxt-quartus-home-') as home:
        env = execution_environment(Path(home), paths)
        execution = execution_inputs(paths, env)
        record = create_build_record(root, repository, revision, identities, execution=execution,
                                     core_id=core_id, dos_profile=dos_profile)
        build_id = build_identity(record)
        attempts = root / 'build/fes-pcxt-quartus' / build_id
        attempts.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix='attempt-', dir=attempts))
        (output / 'build-inputs.json').write_bytes(record)
        recipe = selected_recipe(root, core_id)['quartus']
        summaries = []
        for name, seeds in [('signoff', recipe['seeds']), ('repeat', [recipe['seeds'][0]])]:
            command = [sys.executable, str(root / 'scripts/quartus_fes_pcxt.py'),
                       '--yosys', str(paths['yosys']), '--quartus-root', str(paths['quartus'].parent.parent),
                       '--package-profile', '--video-mailbox-contract', '--build-id', build_id,
                       '--threads', str(recipe['threads']), '--output', str(output / name)]
            if dos_profile:
                command.append('--dos-profile')
            for seed in seeds:
                command.extend(('--seed', str(seed)))
            subprocess.run(command, cwd=root, env=env, check=True)
            summary = json.loads((output / name / 'summary.json').read_text())
            require_timing(summary, seeds, dos_profile=dos_profile)
            summaries.append(summary)
        payload = output / 'core.rbf'
        original = output / 'signoff' / f'seed-{recipe["seeds"][0]}' / 'output_files/top.rbf'
        repeated = output / 'repeat' / f'seed-{recipe["seeds"][0]}' / 'output_files/top.rbf'
        if sha(original) != sha(repeated):
            raise BuildError('independent Quartus rebuild changed the RBF')
        if (create_build_record(root, repository, revision, identities, execution=execution,
                                core_id=core_id, dos_profile=dos_profile) != record
                or execution_inputs(paths, env) != execution
                or authenticate_tools(root, cache_root=cache_root, core_id=core_id) != tools):
            raise BuildError('Quartus build inputs changed during compilation')
        shutil.copyfile(original, payload)
        metadata = dos_assets(root)[0] if dos_profile else None
        encoded = manifest(record, payload, dos_profile=dos_profile, metadata=metadata)
        (output / 'manifest.toml').write_bytes(encoded)
        (output / 'build-summary.json').write_text(json.dumps({
            'classification': 'installable-quartus-hardware-dos-package' if dos_profile else 'installable-quartus-diagnostic-package',
            'build_id': build_id, 'revision': revision, 'seeds': recipe['seeds'],
            'core_id': core_id, 'hardware_dos': dos_profile,
            'freedos_image_sha256': metadata['floppy']['sha256'] if dos_profile else None,
            'internal_timing_pass': True, 'repeat_rbf_sha256': sha(repeated),
            'whole_board_qualified': False, 'hardware_acceptance': False,
            'limits': ['external IO/HPS timing and physical reset qualification remain open',
                       'no exact-artifact hardware acceptance recorded']}, indent=2) + '\n')
        package = export_package(encoded, payload, Path(package_output or root / 'build/packages'))
    return package


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--package-output', type=Path)
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--identity-version', type=int, default=2)
    parser.add_argument('--dos-profile', action='store_true', help='build the FreeDOS SDRAM floppy hardware package')
    args = parser.parse_args()
    try:
        package = build(args.root, package_output=args.package_output,
                        cache_root=args.cache_root, identity_version=args.identity_version,
                        dos_profile=args.dos_profile)
        print(package)
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
