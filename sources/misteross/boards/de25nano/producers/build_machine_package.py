#!/usr/bin/env python3
"""Build a standard DE25 machine.

Shared by the Spectrum, Atari ST, C64 and pc386 producers. Packaging uses the
same complete timing/input-review and independent-RBF gates as the DE25 ZX81;
a candidate those gates refuse is retained and never packaged. Nothing is
programmed.

A machine whose firmware is uploaded at launch (its board spec names
FIRMWARE_BYTES) is packaged as format 3: the payload is the unmodified
bitstream, and its ROM map names only the size of the image the machine's
fes.fabric.firmware-upload endpoint takes. A machine whose firmware is linked
in its netlist (pc386: the ROM firmware.lock pins) is format 2, with no ROM
map. A machine with a window in HPS DDR (spec.WINDOW) has the scanout+window
master profile and requires fes.fabric.de25-hps-window.
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
from boards.de25nano.scanout import async_inputs, audio_reference, bridge_reset

ROOT = Path(__file__).resolve().parents[3]
SHARED = 'boards/de25nano/producers/build_machine_package.py'
ABI = 'cores/fes-common/generated/fes_computer.vh'
SOURCE_ROOTS = sorted([*common.SOURCE_ROOTS, 'cores/fes-common/generated'])
REVIEW_INPUTS = (*common.REVIEW_INPUTS, 'boards/de25nano/scanout/audio_reference.py',
                 'boards/de25nano/scanout/audio_clocks.tcl',
                 'boards/de25nano/producers/review_audio_clocks.py')
NATIVE_SPECTRUM_PROFILE = 'de25-native-spectrum48-v1'
NATIVE_SPECTRUM_COMPOSER = 'boards/de25nano/spectrum/native_hps.py'
DEVICE = common.DEVICE
SHELL = ('fes.video.fixed-720p60', 'fes.keyboard.hid', 'fes.gamepad.ports',
         'fes.audio.pcm-s16-stereo-48k', 'fes.video.session-display', 'fes.fabric.video.display-plane')
WINDOW_INTERFACE = ('fes.fabric.de25-hps-window', 1, 0)
sha = common.sha


def uploads_firmware(spec):
    return hasattr(spec, 'FIRMWARE_BYTES')


class Machine:
    """One machine: its board spec, its package identity and its own interfaces."""

    def __init__(self, *, recipe, directory, core_id, rom_id, name, version, descriptions, interfaces,
                 model_core_ids=None, model_rom_ids=None, model_profiles=None, model_interfaces=None,
                 guest_memory='board-sdram', shell=SHELL, abi_minor=0):
        self.recipe, self.directory, self.core_id, self.rom_id = recipe, directory, core_id, rom_id
        # Where the guest's RAM and media live: the board SDRAM, or the
        # machine's own block RAM as on the DE10 (the C64).
        self.guest_memory = guest_memory
        self.model_core_ids = dict(model_core_ids or {})
        self.model_rom_ids = dict(model_rom_ids or {})
        self.model_profiles = dict(model_profiles or {})
        self.name, self.version, self.descriptions = name, version, descriptions
        self.interfaces, self.shell, self.abi_minor = interfaces, shell, abi_minor
        # Interfaces a model adds to the machine's own, such as a disk unit.
        self.model_interfaces = dict(model_interfaces or {})
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

    def core_id_for(self, model):
        return self.model_core_ids.get(model, self.core_id)

    def rom_id_for(self, model):
        return self.model_rom_ids.get(model, self.rom_id)

    def require_clean_source(self, root):
        return require_clean_source(Path(root), identity_version=2, pinned_inputs=(self.recipe, ABI))

    def authenticate(self, root, *, cache_root=None, gpu_router='HIP'):
        spec = self.spec(root)
        paths, identities = common.authenticate_tools(spec, os.environ.get('QUARTUS_ROOTDIR', ''))
        tools = {name: AuthenticatedTool(path, identities[name]) for name, path in paths.items()}
        if hasattr(spec, 'authenticate_front_end'):
            tools['yosys'] = spec.authenticate_front_end(cache_root=cache_root, gpu_router=gpu_router)
        return tools

    @staticmethod
    def tool_set(spec):
        """The tools a record names: Quartus's four, and a netlist front end's Yosys."""
        return {'quartus', 'qsys', 'ipgenerate', 'pfg'} | ({'yosys'} if hasattr(spec, 'authenticate_front_end') else set())

    def memory_parameters(self, root, spec, model, native_spectrum):
        """Guest memory and firmware: the parameters that differ between machines."""
        if spec.WINDOW:
            memory = {'f2sdram_master_profile': 'scanout+window', 'guest_memory': 'hps-window',
                      'window_contract': '%s %d.%d' % WINDOW_INTERFACE,
                      'window_bridge_reset_review_policy': bridge_reset.POLICY}
        else:
            memory = {'f2sdram_master_profile': 'native-scanout-and-probe' if native_spectrum else 'scanout-only',
                      'guest_memory': self.guest_memory}
        if uploads_firmware(spec):
            return {**memory, 'firmware_delivery': UPLOAD_ENCODING, 'firmware_bytes': spec.FIRMWARE_BYTES[model]}
        return {**memory, **spec.record_parameters(root)}

    def create_build_record(self, root, repository, revision, identities, execution, captured=None, *,
                            model=None, identity_version=2):
        if type(identity_version) is not int or identity_version != 2:
            raise BuildError(self.label + ' requires functional build identity 2')
        root = Path(root).resolve()
        spec = self.spec(root)
        if execution.get('toolchain') != 'quartus' or set(identities) != self.tool_set(spec):
            raise BuildError(self.label + ' requires actual controlled Quartus execution inputs')
        model = self.model(spec, model)
        if captured is None:
            captured = spec.board.capture(spec, revision)
        top = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], cwd=root, text=True).strip())
        profile = self.model_profiles.get(model, 'de25-linux-computer-v1')
        native_spectrum = profile == NATIVE_SPECTRUM_PROFILE
        review_inputs = (*REVIEW_INPUTS, NATIVE_SPECTRUM_COMPOSER) if native_spectrum else REVIEW_INPUTS
        roots, closure = common.captured_source_closure(root, SOURCE_ROOTS, captured, self.recipe, review_inputs)
        fields = {'format': 2, 'toolchain': 'quartus', 'repository': repository, 'revision': revision,
            'source_path': root.relative_to(top).as_posix(), 'source_roots': roots, 'source_inputs': closure,
            'recipe': self.recipe, 'recipe_sha256': sha(root / self.recipe), 'abi_definition': ABI,
            'abi_definition_sha256': sha(root / ABI), 'dependencies': {}, 'tools': dict(identities),
            'parameters': {'core_id': self.core_id_for(model),
                **({'rom_id': self.rom_id_for(model)} if self.rom_id_for(model) else {}),
                'model': model, 'device': DEVICE,
                'hps_initialization': 'HPS FIRST', 'source_closure_policy': common.SOURCE_POLICY,
                'fabric_initiator_policy': 'native-maintenance-absent-v1' if native_spectrum else common.MANAGED_FABRIC,
                'programming_profile': profile, 'standard_core': True, 'linux_dma': not native_spectrum,
                'expansion_socket': False, **self.memory_parameters(root, spec, model, native_spectrum),
                'machine_clock_hz': spec.MACHINE_HZ, 'audio_clock_requested_hz': 12288000,
                'fifo_reset_sharing': common.SHARED_FIFO_RESETS,
                'f2s_data_width': 0, 'f2sdram_data_width': 256, 'scanout_master_data_width': 128,
                'hps_warm_reset_handshake': True, 'reset_timing_policy': common.TIMED_RESET_RELEASE,
                'signoff_policy': async_inputs.POLICY, 'seed': 1, 'threads': 2,
                'hdmi_output_policy': async_inputs.vendor_reference.POLICY,
                'hdmi_video_timing_policy': async_inputs.video_outputs.POLICY,
                'hdmi_output_reset_review_policy': async_inputs.output_reset.POLICY,
                'audio_clock_reference_policy': audio_reference.MACHINE_POLICY,
                'pixel_clock_hz': 74250000, 'system_clock_hz': 100000000,
                'execution_sha256': execution_digest(execution)}}
        if execution.get('selection_only'):
            fields['parameters']['selection_only'] = True
        return encode_build_record(with_identities(fields, root=root))

    def manifest(self, record, payload):
        """The format-2 manifest; for an uploaded firmware the exporter adds the ROM requirement
        and makes it format 3."""
        fields = json.loads(record)
        parameters = fields['parameters']
        board = ((FIRMWARE_UPLOAD, 1, 0),) if 'firmware_delivery' in parameters else ()
        if parameters['guest_memory'] == 'hps-window':
            board += (WINDOW_INTERFACE,)
        return encode_manifest({'format': 2,
            'core': {'id': fields['parameters']['core_id'], 'name': self.name, 'version': self.version,
                     'description': self.descriptions[fields['parameters']['model']]},
            'target': {'platform': 'de25_nano', 'device': DEVICE,
                       'programming_profile': fields['parameters']['programming_profile']},
            'payload': {'file': 'core.rbf', 'size': payload.stat().st_size, 'sha256': sha(payload)},
            'abi': {'id': 'fes.computer', 'major': 1, 'minor': self.abi_minor},
            # An entry is (id, major, minor) or, for an optional one, (id, major, minor, False).
            'interfaces': [{'id': name, 'major': major, 'minor': minor, 'required': bool(required[0]) if required else True}
                           for name, major, minor, *required in (
                               *((name, 1, 0) for name in self.shell), *self.interfaces,
                               *self.model_interfaces.get(fields['parameters']['model'], ()),
                               *board, ('fes.fabric.de25-scanout', 1, 3))],
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
              compile_only=False, license_file=None, cache_root=None, gpu_router='HIP'):
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

        def front_end():
            # Authenticated here, outside the controlled environment, and handed
            # to the board's elaboration by the spec; never read from it.
            if not hasattr(spec, 'authenticate_front_end'):
                return {}
            return {'yosys': spec.authenticate_front_end(cache_root=cache_root, gpu_router=gpu_router).identity}
        identities = {**identities, **front_end()}
        with tempfile.TemporaryDirectory(prefix='fes-de25-' + self.directory + '-home-') as home:
            env = common.execution_environment(Path(home), paths, license_file=license_file)
            execution = common.execution_inputs(paths, env)
            record = self.create_build_record(root, repository, revision, identities, execution, captured,
                                              model=model)
            selected_id = build_identity(record)
            native_spectrum = self.model_profiles.get(model) == NATIVE_SPECTRUM_PROFILE
            if native_spectrum:
                from boards.de25nano.spectrum import native_hps
                if sha(Path(native_hps.__file__)) != json.loads(record)['source_inputs'][NATIVE_SPECTRUM_COMPOSER]:
                    raise BuildError('loaded Spectrum composer differs from the recorded commit')
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
                    if native_spectrum:
                        selected, hps, scanout = native_hps.project(
                            vendor_archive, output / name, revision=revision, selected_id=selected_id,
                            spectrum_spec=spec)
                        native_hps.generate(selected, hps, scanout, quartus_root)
                        native_hps.compile_package_candidate(selected, hps, scanout, paths['quartus'])
                    else:
                        selected, scanout, combined, hps, display = board.prepare(
                            spec, vendor_archive, output / name, revision, model, package_build_id=selected_id)
                        board.generate(spec, selected, scanout, combined, hps, display,
                            paths['quartus'], identities['quartus'].split(' sha256:')[0])
                        board.compile_board(spec, selected, scanout, hps, display, paths['quartus'])
                    if native_spectrum and not compile_only:
                        raw = common.timing_evidence(selected)
                        (output / (name + '-raw-timing.json')).write_text(
                            json.dumps(raw, indent=2, sort_keys=True) + '\n')
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
                    common.authenticate_tools(spec, quartus_root)[0] != paths or
                    {**common.authenticate_tools(spec, quartus_root)[1], **front_end()} != identities or
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
            if not uploads_firmware(spec):
                return export_package(encoded, output / 'core.rbf', Path(package_output or root / 'build/packages'))
            (output / 'rom-map.json').write_bytes(self.rom_map(record, output / 'core.rbf'))
            package = export_package(encoded, output / 'core.rbf', Path(package_output or root / 'build/packages'),
                                     rom_map=output / 'rom-map.json',
                                     rom_id=self.rom_id_for(model), rom_role='firmware')
            if model in self.model_profiles:
                (output / 'native-controls.json').write_bytes(
                    spec.native_controls(selected_id, sha(output / 'core.rbf')))
            return package

    def main(self, description, argv=None):
        parser = argparse.ArgumentParser(description=description)
        parser.add_argument('--root', type=Path, default=ROOT)
        parser.add_argument('--quartus-root', default=os.environ.get('QUARTUS_ROOTDIR', ''))
        parser.add_argument('--license-file', type=Path,
                            help='explicit local Quartus license; private copy and hash are bound to execution')
        parser.add_argument('--vendor-archive', type=Path, default=os.environ.get('FES_DE25_VENDOR_ARCHIVE') or None)
        parser.add_argument('--cache-root', type=Path,
                            help='toolchain cache for a netlist front end (pc386: the pc386 lock\'s Yosys)')
        parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'), default='HIP',
                            help='which lock slot authenticates that Yosys; nothing routes on a GPU')
        parser.add_argument('--identity-version', type=int, choices=(2,), default=2)
        parser.add_argument('--model', choices=tuple(self.descriptions), default=next(iter(self.descriptions)))
        parser.add_argument('--output', type=Path)
        parser.add_argument('--package-output', type=Path)
        parser.add_argument('--compile-only', action='store_true')
        args = parser.parse_args(argv)
        try:
            print(self.build(root=args.root, quartus_root=args.quartus_root, vendor_archive=args.vendor_archive,
                             model=args.model, output=args.output, package_output=args.package_output,
                             compile_only=args.compile_only, license_file=args.license_file,
                             cache_root=args.cache_root, gpu_router=args.gpu_router))
        except (OSError, ValueError, KeyError, RuntimeError, subprocess.CalledProcessError) as error:
            parser.exit(1, 'de25-' + self.directory + ': ' + str(error) + '\n')
