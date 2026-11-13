"""USB PIO preservation through actual owned SD and bridge/master services."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoUSBMasterTests(unittest.TestCase):
    def test_owned_sd_and_bridge_closure_with_usb_retained(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-usb-master-') as temp:
            binary = Path(temp) / 'master'
            argv = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                    '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I', str(FW)]
            argv += [str(FW / p) for p in ['nano_boot.c', 'nano_cold.c', 'nano_bridges.c',
                'nano_sd.c', 'nano_sd_platform.c', 'nano_usb_platform.c', 'nano_usb_composition.c',
                'tests/nano_usb_master_test.c']]
            subprocess.run([*argv, '-o', str(binary)], check=True, timeout=30)
            result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('PASS retained USB PIO master/owned SD/control/DDR close cycles', result.stdout)


if __name__ == '__main__':
    unittest.main()
