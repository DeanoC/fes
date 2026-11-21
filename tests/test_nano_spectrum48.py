"""Spectrum's retained record and the TF-A initializer share one identity."""

from copy import deepcopy
import importlib.util
from pathlib import Path
import unittest

MODULE = Path(__file__).resolve().parents[1] / 'platform/embedded/nano/spectrum48.py'
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
