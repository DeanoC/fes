"""The DE25 pc386 window's directed benches: the control block's grant rules and the arbiter."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
PC386 = ROOT / 'boards/de25nano/pc386'


@unittest.skipUnless(shutil.which('verilator'), 'Verilator is required for the window benches')
class De25Pc386WindowBenchTests(unittest.TestCase):
    def run_bench(self, top, rtl, bench):
        with tempfile.TemporaryDirectory() as work:
            subprocess.run(['verilator', '--cc', '--exe', '--build', '-O2', '-Wno-fatal', '--top-module', top,
                            f'-I{PC386}', '--Mdir', work, str(PC386 / rtl), str(PC386 / bench)],
                           check=True, capture_output=True, text=True)
            result = subprocess.run([str(Path(work) / f'V{top}')], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('PASS', result.stdout)

    def test_control_grant_rules(self):
        self.run_bench('fes_de25_hps_window_control', 'fes_de25_hps_window_control.v', 'window_control_tb.cpp')

    def test_arbiter(self):
        self.run_bench('fes_de25_window_arbiter', 'fes_de25_window_arbiter.v', 'window_arbiter_tb.cpp')


if __name__ == '__main__':
    unittest.main()
