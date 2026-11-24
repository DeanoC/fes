"""Spectrum's retained record and the TF-A initializer share one identity."""

import ast
from copy import deepcopy
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / 'platform/embedded/nano/spectrum48.py'
PRODUCER = ROOT / 'sources/misteross/boards/de25nano/producers/build_machine_package.py'
spec = importlib.util.spec_from_file_location('test_spectrum48', MODULE)
spectrum = importlib.util.module_from_spec(spec)
spec.loader.exec_module(spectrum)


class SpectrumAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.context = {'build_id': 'a' * 32}
        self.shared = {'hps_parameters_sha256': 'b' * 64, 'hps_configuration_sha256': 'c' * 64}
        self.platform = {'platform': self.shared, 'boot_context_sha256': spectrum.sha(spectrum.encode(self.context))}
        self.configuration = {'chunk_bytes': spectrum.CHUNK}
        self.proof = dict(self.shared, package_id='1' * 64, build_id='2' * 32, core_size=spectrum.CHUNK + 4,
                          core_sha256='3' * 64, core_sha384='4' * 96, chunk_sha384=['5' * 96, '6' * 96],
                          handoff_sha256='7' * 64)
        self.rom = dict(package_id=self.proof['package_id'], source_size=16384,
                        source_sha256='8' * 64, rom_receipt_sha256='9' * 64)
        self.profile = dict(program='fpgaload', selected_programs=True, computer_reload=True,
                            computer_storage_return=True, tenfoot_serial=True, hps_menu_program={'format': 1},
                            hps_context=self.context, hps_configuration=self.configuration)

    def selection(self):
        return spectrum.record_for(self.proof, self.context, self.platform, self.configuration, self.rom)

    def profile_for(self, record):
        return dict(self.profile, spectrum48_program=record, spectrum48_program_sha256=spectrum.sha(spectrum.encode(record)))

    def test_only_matched_hps_and_rom_selection_build_the_companion(self):
        self.assertIsNone(spectrum.checked({}))
        record = self.selection()
        self.assertEqual(spectrum.checked(self.profile_for(record)), record)
        header = spectrum.header(record)
        self.assertIn('#define FES_NANO_SPECTRUM48_SCANOUT_BASE 0x20010200UL', header)
        self.assertIn('0x11,0x11', header)
        self.assertIn('{0x55,0x55', header)
        self.assertIn('{0x66,0x66', header)
        for key in self.shared:
            with self.subTest(key=key):
                wrong = dict(self.proof, **{key: '0' * 64})
                with self.assertRaises(ValueError):
                    spectrum.record_for(wrong, self.context, self.platform, self.configuration, self.rom)
        wrong_rom = dict(self.rom, package_id='f' * 64)
        with self.assertRaises(ValueError):
            spectrum.record_for(self.proof, self.context, self.platform, self.configuration, wrong_rom)

    def test_geometry_and_boot_binding_survive_receipt_rehash(self):
        for key, value in (('computer_cpu_base', 0x20010400), ('core_size', spectrum.CHUNK),
                           ('chunk_sha384', ['5' * 96]), ('boot_context_sha256', '7' * 64)):
            with self.subTest(key=key), self.assertRaises(ValueError):
                spectrum.checked(self.profile_for(dict(self.selection(), **{key: value})))
        profile = self.profile_for(self.selection())
        with self.assertRaises(ValueError):
            spectrum.checked(dict(profile, hps_menu_program=None))
        original = deepcopy(profile)
        profile['spectrum48_program']['core_sha256'] = 'a' * 64
        with self.assertRaises(ValueError):
            spectrum.checked(profile)
        self.assertEqual(spectrum.checked(original), self.selection())

    def test_shared_shell_uses_only_the_paired_platform(self):
        profile = dict(self.profile, shared_core_shell=True)
        self.assertTrue(spectrum.shared_shell(profile))
        self.assertFalse(spectrum.shared_shell(self.profile))
        pinned = self.profile_for(self.selection())
        for changes in (dict(shared_core_shell=1), dict(hps_menu_program=None),
                        dict(computer_storage_return=False),
                        dict(spectrum48_program=pinned['spectrum48_program'])):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                spectrum.shared_shell(dict(profile, **changes))


class SealedBuildStagingTests(unittest.TestCase):
    """The SDK stages a native build laid out as the DE25 machine producer writes it."""

    REVIEWS = ['signoff-async-input-review', 'signoff-audio-clock-review']

    def sealed_build(self, *, c64):
        """One signoff fit: build_machine_package.py has no repeat compile since #2090."""
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = Path(directory.name).resolve()
        profile, core, rom = ((spectrum.C64_PROFILE, 'fes.c64', spectrum.C64_ROM_BYTES) if c64 else
                              (spectrum.PROFILE, 'fes.spectrum', spectrum.ROM_BYTES))
        self.identity, self.payload, self.pinmux = '2' * 32, bytes(range(256)) * 16, b'\x01pinmux'
        fields = {'parameters': {'programming_profile': profile,
                                 'fabric_initiator_policy': 'native-maintenance-absent-v1'},
                  'revision': 'a' * 40, 'repository': 'fes', 'recipe_sha256': 'b' * 64}
        record = json.dumps(fields).encode()
        build = root / 'build'
        hps = build / 'signoff/combined/hps'
        (hps / 'project/output_files').mkdir(parents=True)
        (build / 'build-inputs.json').write_bytes(record)
        (build / 'core.rbf').write_bytes(self.payload)
        self.record_timing(build, admitted=True)
        (hps / 'linux-boot.core.rbf').write_bytes(self.payload)
        (hps / 'project/output_files/hps_bootloader_handoff.bin').write_bytes(self.pinmux)
        self.hw = {'spectrum_control': {'endpoint': {'build_id': self.identity}, 'regions': [
                       {'name': 'subsys_periph.fes_scanout.computer', 'base': 0x10300, 'span': 32},
                       {'name': 'subsys_periph.fes_scanout_guard.avs', 'base': 0x10200, 'span': 256},
                       {'name': 'subsys_periph.fes_ddr_probe.avs', 'base': 0x10100, 'span': 128}]},
                   'ddr_ingress': ['subsys_periph_fes_ddr_probe.avm', 'subsys_periph_fes_mem_cdc.m0'],
                   'hps_parameters': {'f2sdram_data_width': 256}, **({'vendor_fpga_ocm': 'absent'} if c64 else {})}
        handoff = json.dumps(self.hw).encode()
        (hps / 'native-spectrum-handoff.json').write_bytes(handoff)
        (hps / 'generation.json').write_text(json.dumps({'handoff_sha256': spectrum.sha(handoff)}))
        (hps / 'build.json').write_text(json.dumps({'generated_handoff_and_sdc_sha256': {
            'output_files/hps_bootloader_handoff.bin': spectrum.sha(self.pinmux)}}))
        package_id = 'c' * 64
        archive = root / (package_id + '.fcore')
        archive.write_bytes(b'sealed archive')
        archive.with_suffix('.build-inputs.json').write_bytes(record)
        self.package = types.SimpleNamespace(package_id=package_id, payload_bytes=self.payload, fields={
            'format': 3, 'core': {'id': core}, 'abi': {'id': 'fes.computer', 'major': 1, 'minor': 0},
            'target': {'platform': 'de25_nano', 'device': 'A5EB013BB23BE4SCS', 'programming_profile': profile},
            'rom': {'role': 'firmware', 'source_size': rom},
            'build': dict({key: fields[key] for key in ('revision', 'repository', 'recipe_sha256')},
                          id=self.identity, toolchain='quartus 26.3')})
        return build, archive

    def record_timing(self, build, *, admitted):
        (build / 'signoff-timing.json').write_text(json.dumps(
            {'package_timing_admission': admitted, 'project': 'signoff', 'reviews': self.REVIEWS}))

    def inspect(self, build, archive, *, c64, admitted=True):
        """Run inspect with the private producer imports it makes replaced by fixtures."""
        evaluated = []

        def timing_evidence(project, *, async_review, audio_review):
            evaluated.append(project.relative_to(build).as_posix())
            return {'package_timing_admission': admitted, 'project': project.name,
                    'reviews': [async_review.relative_to(build).as_posix(), audio_review.relative_to(build).as_posix()]}

        def module(name, **attributes):
            value = types.ModuleType(name)
            vars(value).update(attributes)
            return value

        names = ('boards', 'boards.de25nano', 'boards.de25nano.package', 'boards.de25nano.producers',
                 'boards.de25nano.scanout')
        modules = {name: module(name) for name in names}
        modules.update({
            'boards.de25nano.package.core_package': module('core_package', read_package=lambda path: self.package),
            'boards.de25nano.package.export_core_package': module(
                'export_core_package', build_identity=lambda record: self.identity,
                manifest_toolchain=lambda fields: 'quartus 26.3',
                verify_record_source_at_revision=lambda root, record: json.loads(record)),
            'boards.de25nano.producers.build_menu_package': module('build_menu_package',
                                                                   timing_evidence=timing_evidence),
            'boards.de25nano.scanout.async_inputs': module(
                'async_inputs', native_c64=lambda params: params['programming_profile'] == spectrum.C64_PROFILE,
                native_spectrum48=lambda params: params['programming_profile'] == spectrum.PROFILE),
            'hps_io': module('hps_io', configuration_bytes=lambda raw: b'configuration:' + raw)})
        with mock.patch.dict(sys.modules, modules):
            return spectrum.inspect(build, archive, c64=c64), evaluated

    def test_a_signoff_only_build_stages_for_both_native_machines(self):
        for c64 in (False, True):
            with self.subTest(c64=c64):
                build, archive = self.sealed_build(c64=c64)
                proof, evaluated = self.inspect(build, archive, c64=c64)
                self.assertEqual(evaluated, ['signoff'])
                handoff = (build / 'signoff/combined/hps/native-spectrum-handoff.json').read_bytes()
                self.assertEqual(proof, dict(
                    package_id=self.package.package_id, build_id=self.identity, core_size=len(self.payload),
                    core_sha256=spectrum.sha(self.payload), core_sha384=hashlib.sha384(self.payload).hexdigest(),
                    chunk_sha384=[hashlib.sha384(self.payload).hexdigest()], handoff_sha256=spectrum.sha(handoff),
                    hps_parameters_sha256=spectrum.sha(spectrum.encode(self.hw['hps_parameters'])),
                    hps_configuration_sha256=spectrum.sha(b'configuration:' + self.pinmux),
                    archive_sha256=spectrum.sha(archive.read_bytes())))

    def test_the_signoff_fit_still_has_to_pass_and_carry_the_package(self):
        build, archive = self.sealed_build(c64=True)
        with self.assertRaisesRegex(ValueError, 'package timing'):
            self.inspect(build, archive, c64=True, admitted=False)
        self.record_timing(build, admitted=False)
        with self.assertRaisesRegex(ValueError, 'package timing'):
            self.inspect(build, archive, c64=True, admitted=False)
        self.record_timing(build, admitted=True)
        self.inspect(build, archive, c64=True)
        (build / 'signoff/combined/hps/linux-boot.core.rbf').write_bytes(self.payload[::-1])
        with self.assertRaisesRegex(ValueError, 'package timing'):
            self.inspect(build, archive, c64=True)

    def test_stages_the_fit_the_machine_producer_compiles(self):
        tree = ast.parse(PRODUCER.read_text())
        compiled = [node.args[0].value for node in ast.walk(tree) if isinstance(node, ast.Call) and
                    isinstance(node.func, ast.Name) and node.func.id == 'compile_at']
        self.assertEqual(compiled, [spectrum.FIT])
