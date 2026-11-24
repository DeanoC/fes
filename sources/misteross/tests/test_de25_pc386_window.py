"""The DE25 pc386 window's directed benches: the control block's grant rules, the arbiter and the fault record."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
PC386 = ROOT / 'boards/de25nano/pc386'
COMMON = ROOT / 'cores/fes-common'


@unittest.skipUnless(shutil.which('verilator'), 'Verilator is required for the window benches')
class De25Pc386WindowBenchTests(unittest.TestCase):
    def run_bench(self, top, rtl, bench, extra=()):
        with tempfile.TemporaryDirectory() as work:
            subprocess.run(['verilator', '--cc', '--exe', '--build', '-O2', '-Wno-fatal', '--top-module', top,
                            f'-I{PC386}', f'-I{COMMON / "generated"}', '--Mdir', work, str(PC386 / rtl),
                            *map(str, extra), str(PC386 / bench)],
                           check=True, capture_output=True, text=True)
            result = subprocess.run([str(Path(work) / f'V{top}')], capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('PASS', result.stdout)

    def test_control_grant_rules(self):
        self.run_bench('fes_de25_hps_window_control', 'fes_de25_hps_window_control.v', 'window_control_tb.cpp')

    def test_arbiter(self):
        self.run_bench('fes_de25_window_arbiter', 'fes_de25_window_arbiter.v', 'window_arbiter_tb.cpp')

    def test_fault_record_keeps_the_first_fault(self):
        self.run_bench('fes_de25_hps_window', 'fes_de25_hps_window.v', 'window_fault_tb.cpp',
                       (COMMON / 'rtl/fes_hps_ddr_guard.v',))


if __name__ == '__main__':
    unittest.main()
