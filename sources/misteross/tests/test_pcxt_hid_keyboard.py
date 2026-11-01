"""Exercise host HID snapshots and XT byte flow in synthesizable RTL."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VERILATOR = shutil.which(os.environ.get('VERILATOR', 'verilator'))


@unittest.skipUnless(VERILATOR, 'requires Verilator')
class XTKeyboardHidTest(unittest.TestCase):
    def test_hid_rows_to_xt_bytes(self):
        with tempfile.TemporaryDirectory(prefix='pcxt-hid-') as directory:
            command = [VERILATOR, '--cc', '--exe', '--build', '-j', '2', '-Wall',
                       '--top-module', 'pcxt_hid_keyboard', '--Mdir', directory,
                       '-GSTABLE_CYCLES=20', '-GREPEAT_DELAY_CYCLES=5000',
                       '-GREPEAT_PERIOD_CYCLES=2000', '-CFLAGS', '-std=c++17 -O2',
                       str(ROOT / 'cores/fes-pcxt/rtl/pcxt_hid_keyboard.sv'),
                       str(ROOT / 'cores/fes-pcxt/sim/hid_keyboard_tb.cpp')]
            result = subprocess.run(command, capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            result = subprocess.run([str(Path(directory) / 'Vpcxt_hid_keyboard')],
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('PASS XT HID:', result.stdout)
