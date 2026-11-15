"""Exercise effective Make flags, including inheritance and profile boundaries."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

FW = Path(__file__).resolve().parents[1] / 'platform/embedded/firmware'
sys.path.insert(0, str(FW))
try:
    import prepare_nano_owned as prepare
finally:
    sys.path.pop(0)


class NanoROMCompileTests(unittest.TestCase):
    def test_only_selected_shared_rom_objects_receive_o2(self):
        targets = ['nano_cold', 'nano_cold_platform', 'nano_boot', 'nano_bridges',
                   'nano_usb_platform', 'nano_usb_board', 'nano_usb_composition', 'nano_rom']
        profiles = [None, {}, {'hps_rom': {}},
                    {'hps_rom': {}, 'usb_pio': True},
                    {'hps_rom': {}, 'usb_pio': True, 'usb_shared': False},
                    {'hps_rom': None, 'usb_pio': True, 'usb_shared': True},
                    {'hps_rom': {}, 'usb_pio': True, 'usb_shared': True}]
        for profile in profiles:
            with self.subTest(profile=profile), tempfile.TemporaryDirectory() as directory:
                policy = prepare.rom_compile_policy(profile)
                selected = profile == profiles[-1]
                self.assertEqual(policy is not None, selected)
                objects = ['build/bl31/' + name + '.o' for name in targets + ['sha', 'socfpga_psci', 'nano_sd', 'unrelated']]
                if policy:
                    self.assertEqual(policy['objects'], [name + '.o' for name in targets])
                    self.assertFalse(policy['cache_state_changed'])
                    self.assertFalse(policy['source_semantics_changed'])
                makefile = Path(directory) / 'Makefile'
                makefile.write_text('BUILD_PLAT := build\nCFLAGS := -Os -fno-builtin\n' +
                                    (policy['make_rules'] if policy else '') + '\n' +
                                    ' '.join(objects) + ':\n\t@echo $@ $(CFLAGS)\n')
                result = subprocess.run(['make', '--no-print-directory', '-f', str(makefile), *objects],
                                        check=True, capture_output=True, text=True, timeout=15)
                rows = [line.split() for line in result.stdout.splitlines()]
                self.assertEqual([row[0] for row in rows], objects)
                for row in rows:
                    wanted = selected and row[0] in objects[:len(targets)]
                    self.assertIn('-fno-builtin', row)
                    self.assertEqual([flag for flag in row[1:] if flag.startswith('-O')],
                                     ['-Os', '-O2'] if wanted else ['-Os'])


if __name__ == '__main__':
    unittest.main()
