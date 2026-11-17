#!/usr/bin/env python3
"""Build a standard DE25 machine whose firmware is uploaded at launch.

Shared by the Spectrum and Atari ST producers. Packaging uses the same complete
timing/input-review and independent-RBF gates as the DE25 ZX81; a candidate
those gates refuse is retained and never packaged. The package is format 3: the
payload is the unmodified bitstream, and its ROM map names only the size of the
image the machine's fes.fabric.firmware-upload endpoint takes. Nothing is
programmed and no firmware is bundled.
"""
from __future__ import annotations
import argparse
import importlib.util
import json
import os

from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from boards.de25nano.producers import build_menu_package as common
from boards.de25nano.package.core_package import FIRMWARE_UPLOAD, UPLOAD_ENCODING, encode_manifest
from boards.de25nano.package.export_core_package import build_identity, encode_build_record, export_package
from scripts.input_identity import with_identities
from scripts.fes_build_common import AuthenticatedTool, BuildError, _require_clean_source as require_clean_source
from scripts.functional_execution import execution_digest
from boards.de25nano.scanout import async_inputs, audio_reference

ROOT = Path(__file__).resolve().parents[3]
SHARED = 'boards/de25nano/producers/build_machine_package.py'
ABI = 'cores/fes-common/generated/fes_computer.vh'
SOURCE_ROOTS = sorted([*common.SOURCE_ROOTS, 'cores/fes-common/generated'])
REVIEW_INPUTS = (*common.REVIEW_INPUTS, 'boards/de25nano/scanout/audio_reference.py',
                 'boards/de25nano/scanout/audio_clocks.tcl',
                 'boards/de25nano/producers/review_audio_clocks.py')
DEVICE = common.DEVICE
SHELL = ('fes.video.fixed-720p60', 'fes.keyboard.hid', 'fes.gamepad.ports',
         'fes.audio.pcm-s16-stereo-48k', 'fes.video.session-display', 'fes.fabric.video.display-plane')
sha = common.sha


class Machine:
    """One machine: its board spec, its package identity and its own interfaces."""

    def __init__(self, *, recipe, directory, core_id, name, version, descriptions, interfaces):
        self.recipe, self.directory, self.core_id = recipe, directory, core_id
        self.name, self.version, self.descriptions = name, version, descriptions
        self.interfaces = interfaces
        self.label = 'DE25 ' + name

    def spec(self, root):
        path = Path(root) / 'boards/de25nano' / self.directory / 'build.py'
        spec = importlib.util.spec_from_file_location(
            'de25_canonical_' + self.directory.replace('-', '_'), path)
        module = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = module
        spec.loader.exec_module(module)
        return module

    def model(self, spec, model):
        model = model or next(iter(spec.MODELS))
        if model not in spec.MODELS or model not in self.descriptions:
            raise BuildError(self.label + ' has no model ' + str(model))
        return model

    def require_clean_source(self, root):
        return require_clean_source(Path(root), identity_version=2, pinned_inputs=(self.recipe, ABI))

    def authenticate(self, root):
        paths, identities = common.authenticate_tools(self.spec(root), os.environ.get('QUARTUS_ROOTDIR', ''))
        return {name: AuthenticatedTool(path, identities[name]) for name, path in paths.items()}

    def create_build_record(self, root, repository, revision, identities, execution, captured=None, *,
                            model=None, identity_version=2):
        if type(identity_version) is not int or identity_version != 2:
            raise BuildError(self.label + ' requires functional build identity 2')
        if execution.get('toolchain') != 'quartus' or set(identities) != {'quartus', 'qsys', 'ipgenerate', 'pfg'}:
            raise BuildError(self.label + ' requires actual controlled Quartus execution inputs')
        root = Path(root).resolve()
        spec = self.spec(root)
        model = self.model(spec, model)
        if captured is None:
            captured = spec.board.capture(spec, revision)
        top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())
        roots, closure = common.captured_source_closure(root, SOURCE_ROOTS, captured, self.recipe, REVIEW_INPUTS)
        fields = {'format': 2, 'toolchain': 'quartus', 'repository': repository, 'revision': revision,
            'source_path': root.relative_to(top).as_posix(), 'source_roots': roots, 'source_inputs': closure,
            'recipe': self.recipe, 'recipe_sha256': sha(root / self.recipe), 'abi_definition': ABI,
            'abi_definition_sha256': sha(root / ABI), 'dependencies': {}, 'tools': dict(identities),
            'parameters': {'core_id': self.core_id, 'model': model, 'device': DEVICE,
                'hps_initialization': 'HPS FIRST', 'source_closure_policy': common.SOURCE_POLICY,
                'f2sdram_master_profile': 'scanout-only', 'fabric_initiator_policy': common.MANAGED_FABRIC,
                'programming_profile': 'de25-linux-computer-v1', 'standard_core': True, 'linux_dma': True,
                'expansion_socket': False, 'guest_memory': 'board-sdram',
                'firmware_delivery': UPLOAD_ENCODING, 'firmware_bytes': spec.FIRMWARE_BYTES[model],
                'machine_clock_hz': spec.board.MACHINE_HZ, 'audio_clock_requested_hz': 12288000,
                'fifo_reset_sharing': common.SHARED_FIFO_RESETS,
                'f2s_data_width': 0, 'f2sdram_data_width': 256, 'scanout_master_data_width': 128,
                'hps_warm_reset_handshake': True, 'reset_timing_policy': common.TIMED_RESET_RELEASE,
                'signoff_policy': async_inputs.POLICY, 'seed': 1, 'threads': 2,
                'hdmi_output_policy': async_inputs.vendor_reference.POLICY,
                'hdmi_video_timing_policy': async_inputs.video_outputs.POLICY,
                'hdmi_output_reset_review_policy': async_inputs.output_reset.POLICY,
                'audio_clock_reference_policy': audio_reference.POLICY,
                'pixel_clock_hz': 74250000, 'system_clock_hz': 100000000,
                'execution_sha256': execution_digest(execution)}}
        if execution.get('selection_only'):
            fields['parameters']['selection_only'] = True
        return encode_build_record(with_identities(fields, root=root))

    def manifest(self, record, payload):
        """The format-2 manifest; the exporter adds the ROM requirement and makes it format 3."""
        fields = json.loads(record)
        return encode_manifest({'format': 2,
            'core': {'id': self.core_id, 'name': self.name, 'version': self.version,
                     'description': self.descriptions[fields['parameters']['model']]},
            'target': {'platform': 'de25_nano', 'device': DEVICE, 'programming_profile': 'de25-linux-computer-v1'},
            'payload': {'file': 'core.rbf', 'size': payload.stat().st_size, 'sha256': sha(payload)},
            'abi': {'id': 'fes.computer', 'major': 1, 'minor': 0},
            'interfaces': [{'id': name, 'major': major, 'minor': minor, 'required': True}
                           for name, major, minor in (*((name, 1, 0) for name in SHELL), *self.interfaces,
                                                      (FIRMWARE_UPLOAD, 1, 0), ('fes.fabric.de25-scanout', 1, 3))],
            'build': {'id': build_identity(record), 'repository': fields['repository'],
                'revision': fields['revision'], 'recipe_sha256': fields['recipe_sha256'],
                'toolchain': '; '.join(f'{name} {value}' for name, value in sorted(fields['tools'].items()))}})

    def rom_map(self, record, payload):
        """Bind the base image and the firmware size; no bitstream location is patched."""
        return json.dumps({'format': 1, 'device': DEVICE, 'encoding': UPLOAD_ENCODING,
                           'base_sha256': sha(payload),
                           'source_size': json.loads(record)['parameters']['firmware_bytes']},
                          separators=(',', ':')).encode()

    def build(self, root=ROOT, *, quartus_root, vendor_archive, model=None, output=None, package_output=None,
              compile_only=False, license_file=None):
        root = Path(root).resolve()
        if vendor_archive is None:
            raise BuildError(self.label + ' requires --vendor-archive or FES_DE25_VENDOR_ARCHIVE')
        if compile_only and package_output is not None:
            raise BuildError('compile-only cannot select a package destination')
        if output is not None and not compile_only and not Path(output).resolve().is_relative_to(root):
            raise BuildError('package build output must remain inside its declared source module')
        repository, revision = self.require_clean_source(root)
        spec = self.spec(root)
        board = spec.board
        model = self.model(spec, model)
        captured = board.capture(spec, revision)
        paths, identities = common.authenticate_tools(spec, quartus_root)
        with tempfile.TemporaryDirectory(prefix='fes-de25-' + self.directory + '-home-') as home:
            env = common.execution_environment(Path(home), paths, license_file=license_file)
            execution = common.execution_inputs(paths, env)
            record = self.create_build_record(root, repository, revision, identities, execution, captured,
                                              model=model)
            selected_id = build_identity(record)
            if output is None:
                parent = root / 'build' / ('de25-' + self.directory) / selected_id
                parent.mkdir(parents=True, exist_ok=True)
                output = Path(tempfile.mkdtemp(prefix='attempt-', dir=parent))
            else:
                output = Path(output).resolve()
                output.mkdir(parents=True, exist_ok=False)
            (output / 'build-inputs.json').write_bytes(record)
            (output / 'execution.json').write_text(json.dumps(execution, indent=2, sort_keys=True) + '\n')
            print(self.label + ' build: ' + str(output), flush=True)
            with common.controlled_environment(env):
                def compile_at(name):
                    selected, scanout, combined, hps, display = board.prepare(
                        spec, vendor_archive, output / name, revision, model, package_build_id=selected_id)
                    board.generate(spec, selected, scanout, combined, hps, display,
                        paths['quartus'], identities['quartus'].split(' sha256:')[0])
                    board.compile_board(spec, selected, scanout, hps, display, paths['quartus'])
                    # Compile-only is deliberately unadmitted: retain every raw rule
                    # without borrowing a reviewed input/output receipt from another fit.
                    review = None if compile_only else async_inputs.collect(
                        selected, paths['quartus'].with_name('quartus_sta'), vendor_archive=vendor_archive)
                    audio = None if compile_only else audio_reference.collect(
                        selected, paths['quartus'].with_name('quartus_sta'), vendor_archive)
                    timing = common.timing_evidence(selected, async_review=review, audio_review=audio)
                    (output / (name + '-timing.json')).write_text(
                        json.dumps(timing, indent=2, sort_keys=True) + '\n')
                    return selected / 'combined/hps/linux-boot.core.rbf', timing
                payload, timing = compile_at('signoff')
                if not compile_only:
                    if not timing['package_timing_admission']:
                        raise BuildError(self.label + ' package blocked by reported timing/signoff findings; '
                                         'candidate retained')
                    repeated, other = compile_at('repeat')
                    if not other['package_timing_admission'] or sha(payload) != sha(repeated):
                        raise BuildError(self.label + ' independent rebuild failed timing or changed RBF')
            if (self.require_clean_source(root) != (repository, revision) or
                    common.authenticate_tools(spec, quartus_root) != (paths, identities) or
                    common.execution_inputs(paths, env) != execution or
                    self.create_build_record(root, repository, revision, identities, execution,
                                             board.capture(spec, revision), model=model) != record):
                raise BuildError(self.label + ' source/tool/execution inputs changed during build')
            shutil.copyfile(payload, output / 'core.rbf')
            kind = 'de25-' + self.directory
            (output / 'build-summary.json').write_text(json.dumps({'build_id': selected_id, 'revision': revision,
                'model': model,
                'classification': kind + ('-compile-candidate' if compile_only else '-package-candidate'),
                'rbf_sha256': sha(output / 'core.rbf'),
                'package_timing_admission': timing['package_timing_admission'],
                'hardware_acceptance': False, 'normal_kit_acceptance': False}, indent=2, sort_keys=True) + '\n')
            if compile_only:
                return output
            encoded = self.manifest(record, output / 'core.rbf')
            (output / 'manifest.toml').write_bytes(encoded)
            (output / 'rom-map.json').write_bytes(self.rom_map(record, output / 'core.rbf'))
            return export_package(encoded, output / 'core.rbf', Path(package_output or root / 'build/packages'),
                                  rom_map=output / 'rom-map.json', rom_id='machine-rom', rom_role='firmware')

    def main(self, description, argv=None):
        parser = argparse.ArgumentParser(description=description)
        parser.add_argument('--root', type=Path, default=ROOT)
        parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
        parser.add_argument('--license-file', type=Path,
                            help='explicit local Quartus license; private copy and hash are bound to execution')
        parser.add_argument('--vendor-archive', type=Path, default=os.environ.get('FES_DE25_VENDOR_ARCHIVE') or None)
        parser.add_argument('--cache-root', type=Path)
        parser.add_argument('--identity-version', type=int, choices=(2,), default=2)
        parser.add_argument('--model', choices=tuple(self.descriptions), default=next(iter(self.descriptions)))
        parser.add_argument('--output', type=Path)
        parser.add_argument('--package-output', type=Path)
        parser.add_argument('--compile-only', action='store_true')
        args = parser.parse_args(argv)
        try:
            print(self.build(root=args.root, quartus_root=args.quartus_root, vendor_archive=args.vendor_archive,
                             model=args.model, output=args.output, package_output=args.package_output,
                             compile_only=args.compile_only, license_file=args.license_file))
        except (OSError, ValueError, KeyError, RuntimeError, subprocess.CalledProcessError) as error:
            parser.exit(1, 'de25-' + self.directory + ': ' + str(error) + '\n')
