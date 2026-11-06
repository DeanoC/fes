"""Static variant of the standard ZX81 producer; never program hardware.

The existing CLI selects this helper explicitly. The approved source-bound board review
is required for all three revisions; internal16K review receipts do not apply.
"""
from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import re
import shutil
import tempfile
import tomllib

from scripts import build_de25_zx81_package as standard
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package
from scripts.fes_build_common import BuildError

PROFILE = 'fes.de25-standard-zx81-empty-ram16k.v1'
PROGRAMMING_PROFILE = 'de25-linux-static-computer-v1'
DEFAULT_PERSONA = 0xc001a001
SIGNOFF_POLICY = 'de25-static-stock-reference-v2'
REVIEW_DIRECTORY = 'boards/de25nano/link-proof/zx81-standard'
REVIEW_INPUTS = tuple(REVIEW_DIRECTORY + '/' + name for name in (
    'static_reference.py', 'reviewed-inputs.json', 'reviewed-topology.json',
    'review-audio.tcl', 'review-reset-direct.tcl', 'review-reset-paths.tcl'))


def reviewer(root):
    path = Path(root) / REVIEW_DIRECTORY / 'static_reference.py'
    spec = importlib.util.spec_from_file_location('de25_static_reference', path)
    selected = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(selected)
    if selected.POLICY != SIGNOFF_POLICY:
        raise BuildError('static ZX81 review policy changed')
    return selected


def timing_evidence(root, project, record):
    if json.loads(record)['parameters'].get('signoff_policy') != SIGNOFF_POLICY:
        raise BuildError('static ZX81 requires its approved source-bound board review')
    return reviewer(root).verify(project, record)


def builder(root):
    path = Path(root) / 'boards/de25nano/link-proof/build.py'
    spec = importlib.util.spec_from_file_location('de25_static_producer_builder', path)
    selected = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(selected)
    selected.PROFILE = selected.load_profile(path.parent / 'zx81-standard/profile.py')
    if selected.PROFILE.identity != PROFILE:
        raise BuildError('static ZX81 compiler profile changed')
    return selected


def create_build_record(root, repository, revision, identities, execution, captured,
                        *, control_build_id, placement, routing):
    if re.fullmatch(r'[0-9a-f]{32}', control_build_id) is None:
        raise BuildError('static ZX81 requires an independent canonical controller ID')
    for region in (placement, routing):
        if re.fullmatch(r'X[0-9]+ Y[0-9]+ X[0-9]+ Y[0-9]+', region) is None:
            raise BuildError('static ZX81 requires explicit canonical placement and routing regions')
    fields = json.loads(standard.create_build_record(
        root, repository, revision, identities, execution, captured))
    roots = sorted(set(fields['source_roots']) | set(REVIEW_INPUTS))
    fields.update(source_roots=roots, source_inputs=standard.common.source_input_closure(
        Path(root), roots, policy=standard.common.SOURCE_POLICY))
    parameters = fields['parameters']
    # The constant-PCM reference is deliberately ineligible for this shell.
    del parameters['audio_clock_reference_policy']
    del parameters['hdmi_output_policy']
    del parameters['internal_ram_kib']
    parameters.update(programming_profile=PROGRAMMING_PROFILE, expansion_socket=True,
        signoff_policy=SIGNOFF_POLICY,
        static_profile=PROFILE, static_control_build_id=control_build_id,
        static_default_persona_id=DEFAULT_PERSONA, placement=placement, routing=routing)
    return encode_build_record(fields)


def selected_artifacts(project, selected, record):
    """Recheck the compiler's sealed descriptor rather than trust its paths."""
    project = Path(project)
    inputs = json.loads((project / 'inputs.json').read_bytes())
    selected.verify_project(project, inputs)
    fields = json.loads(record)
    if (inputs.get('profile') != PROFILE or inputs.get('source_committed') is not True
            or inputs.get('package_build_id') != build_identity(record)
            or inputs['source_revision'] != fields['revision']
            or inputs['control_build_id'] != fields['parameters']['static_control_build_id']):
        raise BuildError('static ZX81 compiler and functional identities differ')
    build = json.loads((project / 'build.json').read_bytes())
    selected.verify_files(project, build['compiled_report_sha256'])
    selected.verify_files(project, {build['artifact_manifest']['path']: build['artifact_manifest']['sha256'],
                                    build['root_qdb']['path']: build['root_qdb']['sha256']})
    descriptor = json.loads((project / build['artifact_manifest']['path']).read_bytes())
    shell = descriptor['shell']
    if (descriptor['device'] != standard.DEVICE
            or shell['control_build_id'] != inputs['control_build_id']
            or shell['source_revision'] != fields['revision']
            or shell['source_closure_sha256'] != inputs['source_closure_sha256']
            or shell['root_qdb_sha256'] != build['root_qdb']['sha256']):
        raise BuildError('static ZX81 artifact descriptor differs from its compiler inputs')
    personas = descriptor['personas']
    if (len(personas) != 2 or [(p['id'], p['hardware_id']) for p in personas] !=
            [('zx81_empty', DEFAULT_PERSONA), ('zx81_ram16k', 0xc002b002)]):
        raise BuildError('static ZX81 requires exactly its empty and 16K personas')
    for persona in personas:
        for name in ('root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256'):
            if persona[name] != shell[name]:
                raise BuildError('static ZX81 persona differs from the frozen shell: ' + name)
        if persona['vendor_pof']['hash'] != shell['vendor_pof']['hash']:
            raise BuildError('static ZX81 persona POF0 differs from its base')
    for blob in [shell['base'], *personas]:
        path = project / blob['path']
        if (path.is_symlink() or not path.resolve().is_relative_to(project.resolve())
                or not path.is_file() or path.stat().st_size != blob['size']
                or standard.sha(path) != blob['sha256']):
            raise BuildError('static ZX81 artifact changed after compilation')
    return descriptor


def manifest(record, payload, persona, descriptor):
    fields = tomllib.loads(standard.manifest(record, payload).decode())
    fields['format'] = 5
    fields['target']['programming_profile'] = PROGRAMMING_PROFILE
    fields['core']['description'] = 'Standard DE25 ZX81 with static expansion and a default empty persona'
    fields['interfaces'].append(dict(id='fes.expansion.zx81-bus', major=2, minor=0,
                                     required=False))
    shell = descriptor['shell']
    fields['static_expansion'] = dict(file='persona.rbf', size=persona.stat().st_size,
        sha256=standard.sha(persona), control_build_id=shell['control_build_id'],
        hardware_id=DEFAULT_PERSONA,
        **{name: shell[name] for name in ('root_qdb_sha256', 'interface_sha256', 'clock_reset_sha256')},
        pof0_hash=shell['vendor_pof']['hash'])
    return encode_manifest(fields)


def build_static(root, *, quartus_root, vendor_archive, output, package_output,
                 compile_only, license_file, placement, routing):
    root = Path(root).resolve()
    if vendor_archive is None:
        raise BuildError('DE25 ZX81 requires --vendor-archive or FES_DE25_VENDOR_ARCHIVE')
    if placement is None or routing is None:
        raise BuildError('static ZX81 requires explicit --placement and --routing')
    if not compile_only and (placement, routing) != ('X2 Y7 X26 Y12', 'X1 Y6 X27 Y13'):
        raise BuildError('static ZX81 package requires the reviewed placement and routing regions')
    if compile_only and package_output is not None:
        raise BuildError('compile-only cannot select a package destination')
    if output is not None and not compile_only and not Path(output).resolve().is_relative_to(root):
        raise BuildError('package build output must remain inside its declared source module')
    repository, revision = standard._require_clean_source(root)
    compiler = builder(root)
    frozen = compiler.capture()
    if frozen['revision'] != revision or frozen['committed'] is not True:
        raise BuildError('static ZX81 requires its captured committed source')
    paths, identities = standard.common.authenticate_tools(standard.computer(root), quartus_root)
    tools, version = compiler.toolchain(Path(quartus_root))
    if tools['quartus_sh'].resolve() != paths['quartus'].resolve():
        raise BuildError('static ZX81 compiler differs from its authenticated execution')
    if output is None:
        parent = root / 'build/de25-zx81-static'
        parent.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix='attempt-', dir=parent))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    with tempfile.TemporaryDirectory(prefix='fes-de25-static-home-') as home:
        env = standard.execution_environment(Path(home), paths, license_file=license_file)
        execution = standard.execution_inputs(paths, env)
        record = None
        def compile_at(name):
            nonlocal record
            project = compiler.prepare(output / name, placement, routing, Path(vendor_archive))
            inputs = json.loads((project / 'inputs.json').read_bytes())
            if inputs['source_sha256'] != {n: compiler.sha(d) for n, d in frozen['files'].items()}:
                raise BuildError('static ZX81 captured source changed before preparation')
            current = create_build_record(root, repository, revision, identities, execution,
                frozen['files'], control_build_id=inputs['control_build_id'],
                placement=placement, routing=routing)
            if record is not None and current != record:
                raise BuildError('static ZX81 independent preparation changed functional inputs')
            record = current
            (output / 'build-inputs.json').write_bytes(record)
            compiler.bind_package_identity(project, build_identity(record))
            compiler.compile_project(project, tools, version, None)
            descriptor = selected_artifacts(project, compiler, record)
            if compile_only:
                timing = {r: standard.timing_evidence(project.parents[2], revision=r)
                          for r in ('base', 'persona_a', 'persona_b')}
            else:
                timing = reviewer(root).collect(project, record,
                    paths['quartus'].with_name('quartus_sta'), vendor_archive)
            (output / (name + '-timing.json')).write_text(json.dumps(timing, indent=2, sort_keys=True) + '\n')
            return project, descriptor, timing
        with standard.controlled_environment(env):
            project, descriptor, timing = compile_at('signoff')
            if not compile_only:
                if not all(t['package_timing_admission'] for t in timing.values()):
                    raise BuildError('static ZX81 whole-board timing/signoff failed; candidate retained')
                _, repeated, other = compile_at('repeat')
                a = [descriptor['shell']['base'], *descriptor['personas']]
                b = [repeated['shell']['base'], *repeated['personas']]
                if (not all(t['package_timing_admission'] for t in other.values())
                        or [(p['size'], p['sha256']) for p in a] != [(p['size'], p['sha256']) for p in b]):
                    raise BuildError('static ZX81 independent compilation changed timing or RBF bytes')
        if (standard._require_clean_source(root) != (repository, revision)
                or compiler.capture()['files'] != frozen['files']
                or standard.common.authenticate_tools(standard.computer(root), quartus_root) != (paths, identities)
                or standard.execution_inputs(paths, env) != execution):
            raise BuildError('static ZX81 source/tool/execution changed during compilation')
        (output / 'execution.json').write_text(json.dumps(execution, indent=2, sort_keys=True) + '\n')
        for blob, name in ((descriptor['shell']['base'], 'core.rbf'),
                           (descriptor['personas'][0], 'persona.rbf')):
            shutil.copyfile(project / blob['path'], output / name)
        (output / 'build-summary.json').write_text(json.dumps({
            'build_id': build_identity(record), 'revision': revision,
            'classification': 'de25-zx81-static-compile-candidate' if compile_only else 'de25-zx81-static-package-candidate',
            'package_timing_admission': all(t['package_timing_admission'] for t in timing.values()),
            'hardware_acceptance': False, 'normal_kit_acceptance': False,
            'partial_assets_exported': False}, indent=2, sort_keys=True) + '\n')
        if compile_only:
            return output
        encoded = manifest(record, output / 'core.rbf', output / 'persona.rbf', descriptor)
        (output / 'manifest.toml').write_bytes(encoded)
        return export_package(encoded, output / 'core.rbf', Path(package_output or root / 'build/packages'),
                              persona=output / 'persona.rbf')
