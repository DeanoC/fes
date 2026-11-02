"""Exercise the DOS BAT device through the real XT PPI/cable receiver."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VERILATOR = shutil.which(os.environ.get('VERILATOR', 'verilator'))


@unittest.skipUnless(VERILATOR, 'requires Verilator')
class KeyboardBatTest(unittest.TestCase):
    def test_real_ppi_reset_handshake_and_cancellation(self):
        sources = [ROOT / 'cores/fes-pcxt/rtl' / name for name in
                   ('pcxt_xt_keyboard.sv', 'pcxt_xt_keyboard_bat.sv', 'pcxt_xtio.sv')]
        vendor = ROOT / 'cores/fes-pcxt/vendor/kf8255'
        sources += sorted(vendor.glob('*.sv'))
        sources += [ROOT / 'cores/fes-pcxt/sim' / name for name in
                    ('keyboard_bat_top.sv', 'keyboard_bat_tb.cpp')]
        with tempfile.TemporaryDirectory(prefix='pcxt-bat-') as directory:
            result = subprocess.run([VERILATOR, '--cc', '--exe', '--build', '-j', '2', '-Wall',
                str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'),
                '--top-module', 'keyboard_bat_top', '--Mdir', directory, '-I' + str(vendor),
                '-CFLAGS', '-std=c++17 -O2', *map(str, sources)],
                capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            result = subprocess.run([str(Path(directory) / 'Vkeyboard_bat_top')],
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('PASS XT BAT:', result.stdout)
