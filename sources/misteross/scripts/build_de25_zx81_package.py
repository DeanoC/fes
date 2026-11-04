#!/usr/bin/env python3
"""Build canonical standard DE25 ZX81; retain candidates and never program hardware.

Packaging uses the same complete timing/input-review and independent-RBF gates
as DE25 MENU. Firmware pairing and native kit acceptance remain separate.
"""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_de25_menu_package as common
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, source_input_closure
from scripts.fes_build_common import AuthenticatedTool, BuildError, _require_clean_source as require_clean_source
from scripts.functional_execution import execution_digest, execution_environment
from boards.de25nano.scanout import async_inputs

ROOT = Path(__file__).resolve().parents[1]
RECIPE = 'scripts/build_de25_zx81_package.py'
ABI = 'cores/fes-common/generated/fes_computer.vh'
COMMON_LIBRARY_INPUTS = ['cores/fes-common/rtl/' + name for name in (
    'fes_computer_mailbox.v', 'fes_display_plane.v', 'fes_display_cdc.v',
    'fes_audio_output.v', 'fes_audio_i2s.v')]
SOURCE_ROOTS = sorted(['boards/de25nano', 'cores/fes-common/generated', 'cores/fes-common/rtl/z80',
                'cores/fes-menu', 'cores/fes-splash', 'cores/fes-zx81', 'scripts', *COMMON_LIBRARY_INPUTS])
DEVICE = common.DEVICE
FABRIC = common.MANAGED_FABRIC
sha = common.sha
execution_inputs = common.execution_inputs
controlled_environment = common.controlled_environment
timing_evidence = common.timing_evidence


def _require_clean_source(root, *, identity_version=2, pinned_inputs=(RECIPE, ABI)):
    if tuple(pinned_inputs) != (RECIPE, ABI):
        raise BuildError('DE25 ZX81 requires its pinned recipe and ABI inputs')
    return require_clean_source(Path(root), identity_version=identity_version, pinned_inputs=pinned_inputs)


def computer(root):
    spec = importlib.util.spec_from_file_location('de25_canonical_zx81', Path(root) / 'boards/de25nano/zx81-hps/build.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _authenticate_tools(root, *, cache_root=None):
    paths, identities = common.authenticate_tools(computer(root), os.environ.get('QUARTUS_ROOTDIR', ''))
    return {name: AuthenticatedTool(path, identities[name]) for name, path in paths.items()}


def create_build_record(root, repository, revision, identities, execution, captured=None, *, identity_version=2):
    if type(identity_version) is not int or identity_version != 2:
        raise BuildError('DE25 ZX81 requires functional build identity 2')
    if execution.get('toolchain') != 'quartus' or set(identities) != {'quartus', 'qsys', 'ipgenerate', 'pfg'}:
        raise BuildError('DE25 ZX81 requires actual controlled Quartus execution inputs')
    root = Path(root).resolve()
    if captured is None:
        captured = computer(root).capture_standard_board(revision)
    top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())
    closure = source_input_closure(root, SOURCE_ROOTS)
    for name, data in captured.items():
        relative = Path(name).relative_to('sources/misteross').as_posix()
        if closure.get(relative) != hashlib.sha256(data).hexdigest():
            raise BuildError('captured DE25 ZX81 source differs from canonical closure: ' + name)
    return encode_build_record({'format': 2, 'toolchain': 'quartus', 'repository': repository, 'revision': revision,
        'source_path': root.relative_to(top).as_posix(), 'source_roots': SOURCE_ROOTS, 'source_inputs': closure,
        'recipe': RECIPE, 'recipe_sha256': sha(root / RECIPE), 'abi_definition': ABI,
        'abi_definition_sha256': sha(root / ABI), 'dependencies': {}, 'tools': dict(identities),
        'parameters': {'core_id': 'fes.zx81', 'device': DEVICE, 'hps_initialization': 'HPS FIRST',
            'f2sdram_master_profile': 'scanout-only', 'fabric_initiator_policy': FABRIC,
            'programming_profile': 'de25-linux-computer-v1', 'standard_core': True, 'linux_dma': True,
            'expansion_socket': False, 'internal_ram_kib': 16, 'audio_clock_requested_hz': 12288000,
            'fifo_reset_sharing': common.SHARED_FIFO_RESETS,
            'f2s_data_width': 0, 'f2sdram_data_width': 256, 'scanout_master_data_width': 128, 'hps_warm_reset_handshake': True,
            'reset_timing_policy': common.TIMED_RESET_RELEASE,
            'signoff_policy': async_inputs.POLICY, 'seed': 1, 'threads': 2,
            'hdmi_output_policy': async_inputs.vendor_reference.POLICY,
            'pixel_clock_hz': 74250000, 'system_clock_hz': 100000000,
            'execution_sha256': execution_digest(execution)}})


def manifest(record, payload):
    fields = json.loads(record)
    return encode_manifest({'format': 2,
        'core': {'id': 'fes.zx81', 'name': 'ZX81', 'version': '2.0.0',
                 'description': 'Standard DE25 ZX81 with allocated session display, controllers and cassette SAVE'},
        'target': {'platform': 'de25_nano', 'device': DEVICE, 'programming_profile': 'de25-linux-computer-v1'},
        'payload': {'file': 'core.rbf', 'size': payload.stat().st_size, 'sha256': sha(payload)},
        'abi': {'id': 'fes.computer', 'major': 1, 'minor': 0},
        'interfaces': [{'id': name, 'major': 1, 'minor': 0, 'required': True} for name in (
            'fes.video.fixed-720p60', 'fes.keyboard.hid', 'fes.gamepad.ports',
            'fes.audio.pcm-s16-stereo-48k', 'fes.media.zx81-tape', 'fes.media.zx81-tape-write',
            'fes.video.session-display', 'fes.fabric.video.display-plane')] +
            [{'id': 'fes.fabric.de25-scanout', 'major': 1, 'minor': 1, 'required': True}],
        'build': {'id': build_identity(record), 'repository': fields['repository'], 'revision': fields['revision'],
            'recipe_sha256': fields['recipe_sha256'],
            'toolchain': '; '.join(f'{name} {value}' for name, value in sorted(fields['tools'].items()))}})


def build(root=ROOT, *, quartus_root, vendor_archive, output=None, package_output=None, compile_only=False):
    root = Path(root).resolve()
    if vendor_archive is None:
        raise BuildError('DE25 ZX81 requires --vendor-archive or FES_DE25_VENDOR_ARCHIVE')
    if compile_only and package_output is not None:
        raise BuildError('compile-only cannot select a package destination')
    if output is not None and not compile_only and not Path(output).resolve().is_relative_to(root):
        raise BuildError('package build output must remain inside its declared source module')
    repository, revision = _require_clean_source(root, pinned_inputs=(RECIPE, ABI))
    builder = computer(root)
    captured = builder.capture_standard_board(revision)
    paths, identities = common.authenticate_tools(builder, quartus_root)
    with tempfile.TemporaryDirectory(prefix='fes-de25-zx81-home-') as home:
        env = execution_environment(Path(home), paths)
        execution = execution_inputs(paths, env)
        record = create_build_record(root, repository, revision, identities, execution, captured)
        selected_id = build_identity(record)
        if output is None:
            parent = root / 'build/de25-zx81' / selected_id
            parent.mkdir(parents=True, exist_ok=True)
            output = Path(tempfile.mkdtemp(prefix='attempt-', dir=parent))
        else:
            output = Path(output).resolve()
            output.mkdir(parents=True, exist_ok=False)
        (output / 'build-inputs.json').write_bytes(record)
        (output / 'execution.json').write_text(json.dumps(execution, indent=2, sort_keys=True) + '\n')
        print('DE25 ZX81 build: ' + str(output), flush=True)
        with controlled_environment(env):
            def compile_at(name):
                selected, scanout, combined, hps, display = builder.prepare_standard_board(
                    vendor_archive, output / name, revision, package_build_id=selected_id)
                builder.generate_standard_board(selected, scanout, combined, hps, display,
                    paths['quartus'], identities['quartus'].split(' sha256:')[0])
                builder.compile_standard_board(selected, scanout, hps, display, paths['quartus'])
                # Compile-only is deliberately unadmitted: retain every raw rule
                # without borrowing a reviewed input/output receipt from another fit.
                review = None if compile_only else async_inputs.collect(
                    selected, paths['quartus'].with_name('quartus_sta'), vendor_archive=vendor_archive)
                timing = timing_evidence(selected, async_review=review)
                (output / (name + '-timing.json')).write_text(json.dumps(timing, indent=2, sort_keys=True) + '\n')
                return selected / 'combined/hps/linux-boot.core.rbf', timing
            payload, timing = compile_at('signoff')
            if not compile_only:
                if not timing['package_timing_admission']:
                    raise BuildError('DE25 ZX81 package blocked by reported timing/signoff findings; candidate retained')
                repeated, other = compile_at('repeat')
                if not other['package_timing_admission'] or sha(payload) != sha(repeated):
                    raise BuildError('DE25 ZX81 independent rebuild failed timing or changed RBF')
        if (_require_clean_source(root, pinned_inputs=(RECIPE, ABI)) != (repository, revision) or
                common.authenticate_tools(builder, quartus_root) != (paths, identities) or execution_inputs(paths, env) != execution or
                create_build_record(root, repository, revision, identities, execution, builder.capture_standard_board(revision)) != record):
            raise BuildError('DE25 ZX81 source/tool/execution inputs changed during build')
        shutil.copyfile(payload, output / 'core.rbf')
        (output / 'build-summary.json').write_text(json.dumps({'build_id': selected_id, 'revision': revision,
            'classification': 'de25-zx81-compile-candidate' if compile_only else 'de25-zx81-package-candidate',
            'rbf_sha256': sha(output / 'core.rbf'), 'package_timing_admission': timing['package_timing_admission'],
            'hardware_acceptance': False, 'normal_kit_acceptance': False}, indent=2, sort_keys=True) + '\n')
        if compile_only:
            return output
        encoded = manifest(record, output / 'core.rbf')
        (output / 'manifest.toml').write_bytes(encoded)
        return export_package(encoded, output / 'core.rbf', Path(package_output or root / 'build/packages'))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=ROOT)
    parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
    parser.add_argument('--vendor-archive', type=Path, default=os.environ.get('FES_DE25_VENDOR_ARCHIVE') or None)
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--identity-version', type=int, choices=(2,), default=2)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--package-output', type=Path)
    parser.add_argument('--compile-only', action='store_true')
    args = parser.parse_args(argv)
    try:
        print(build(root=args.root, quartus_root=args.quartus_root, vendor_archive=args.vendor_archive,
                    output=args.output, package_output=args.package_output, compile_only=args.compile_only))
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        parser.exit(1, 'de25-zx81: ' + str(error) + '\n')


if __name__ == '__main__':main()
