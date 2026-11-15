"""Exercise the real owned SiP dispatcher with both private session engines."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoProgramControlTests(unittest.TestCase):
    def test_joined_dispatcher_cycles_and_failures(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            (base / 'common').mkdir()
            (base / 'common/debug.h').write_text('#include <stdio.h>\n#define NOTICE(...) printf(__VA_ARGS__)\n')
            profile = '\n'.join('#define ' + item for item in (
                'FES_NANO_PROGRAM_ROLES 1', 'FES_NANO_COLD_CONFIG 1', 'FES_NANO_COLD_COMPUTER 1',
                'FES_NANO_COLD_ROM 1', 'FES_NANO_WINDOW_READ_DIAGNOSTIC 1', 'FES_NANO_MENU_MEMORY 1',
                'FES_NANO_CONTROLLED_SD 1',
                'FES_NANO_WINDOW_STORAGE_RETURN 1', 'FES_NANO_COMPUTER_STORAGE_RETURN 1',
                'FES_NANO_HPS_CONTEXT_SHA {0x42}')) + '\n'
            (base / 'fes_nano_cold_profile.h').write_text(profile)
            # Real source inventories install this include alongside both engines.
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-include',
                       str(base / 'fes_nano_cold_profile.h'), '-fsanitize=undefined',
                       '-fno-sanitize-recover=all', '-I' + str(base), '-I' + str(FW),
                       *(str(FW / name) for name in ('nano_bridges.c', 'nano_bridge_sip.c',
                         'nano_configuration.c', 'nano_program.c', 'nano_identity.c', 'nano_sd.c',
                         'nano_mac.c', 'nano_usb_service.c', 'tests/nano_program_control_test.c')),
                       '-o', str(base / 'dispatcher')]
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stderr)
            for mode in (0, 2, 3, 4, 5, 6, 7, 8, 9, *range(10, 26)):
                with self.subTest(mode=mode):
                    result = subprocess.run([base / 'dispatcher', str(mode)], capture_output=True,
                                            text=True, timeout=15)
                    self.assertEqual(result.returncode, 0, result.stderr + result.stdout[-3000:])
                    self.assertIn('PASS', result.stdout)
            # Both complete return policies are required at compile time.
            # Merely combining the former mutually exclusive flags still fails.
            for define in ('FES_NANO_PROGRAM_ROLES', 'FES_NANO_COLD_ROM',
                           'FES_NANO_WINDOW_READ_DIAGNOSTIC', 'FES_NANO_CONTROLLED_SD',
                           'FES_NANO_WINDOW_STORAGE_RETURN', 'FES_NANO_COMPUTER_STORAGE_RETURN'):
                with self.subTest(missing_policy=define):
                    (base / 'fes_nano_cold_profile.h').write_text(profile.replace(
                        '#define ' + define + ' 1', '#define ' + define + ' 0'))
                    result = subprocess.run(command, capture_output=True, text=True, timeout=30)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('error:', result.stderr)


if __name__ == '__main__':
    unittest.main()
