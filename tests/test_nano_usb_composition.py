"""Actual cold, USB and SD platform code under one injected register model."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoUSBCompositionTests(unittest.TestCase):
    def test_composed_platform_ownership_and_failures(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-usb-composition-') as temp:
            binary = Path(temp) / 'composition'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I', str(FW)]
            command += [str(FW / name) for name in (
                'nano_cold.c', 'nano_boot.c', 'nano_bridges.c', 'nano_sd.c', 'nano_sd_platform.c',
                'nano_usb_platform.c', 'nano_usb_composition.c',
                'tests/nano_usb_composition_test.c')]
            subprocess.run([*command, '-o', str(binary)], check=True, timeout=30)
            result = subprocess.run([str(binary)], capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('PASS composed USB PIO/cold/SD/bridge-target ownership', result.stdout)


if __name__ == '__main__':
    unittest.main()
