"""The DE25 pc386 window's directed benches: the control block's grant rules, the arbiter, the hold path
through the guards, and the fault record."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest

from boards.de25nano.pc386.derive import retarget_window_guard

ROOT = Path(__file__).resolve().parents[1]
PC386 = ROOT / 'boards/de25nano/pc386'
COMMON = ROOT / 'cores/fes-common'


@unittest.skipUnless(shutil.which('verilator'), 'Verilator is required for the window benches')
class De25Pc386WindowBenchTests(unittest.TestCase):
    def run_bench(self, top, rtl, bench, extra=(), seeds=('1',), *, guard=False):
        with tempfile.TemporaryDirectory() as work:
            if guard:
                source = COMMON / 'rtl/fes_hps_ddr_guard.v'
                derived = Path(work) / source.name
                derived.write_bytes(retarget_window_guard(source.read_bytes())[0])
                extra = (*extra, derived)
            subprocess.run(['verilator', '--cc', '--exe', '--build', '-O2', '-Wno-fatal', '--top-module', top,
                            f'-I{PC386}', f'-I{COMMON / "generated"}', '--Mdir', work, str(PC386 / rtl),
                            *map(str, extra), str(PC386 / bench)],
                           check=True, capture_output=True, text=True)
            for seed in seeds:
                with self.subTest(seed=seed):
                    result = subprocess.run([str(Path(work) / f'V{top}')], capture_output=True, text=True,
                                            env={**os.environ, 'SEED': seed})
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertIn('PASS', result.stdout)

    def test_control_grant_rules(self):
        self.run_bench('fes_de25_hps_window_control', 'fes_de25_hps_window_control.v', 'window_control_tb.cpp')

    def test_arbiter(self):
        # Write gaps, guard-style cuts, 4 KiB crossings: three seeds.
        self.run_bench('fes_de25_window_arbiter', 'fes_de25_window_arbiter.v', 'window_arbiter_tb.cpp',
                       seeds=('1', '2', '3'))

    def test_holds_cut_bursts_through_the_guards(self):
        self.run_bench('window_hold_tb', 'window_hold_tb.v', 'window_hold_tb.cpp',
                       (PC386 / 'fes_de25_hps_window.v', PC386 / 'fes_de25_window_arbiter.v'),
                       seeds=('1', '2', '3'), guard=True)

    def test_hps_reset_holds_pc386_until_a_fresh_grant_reaches_a_held_machine(self):
        self.run_bench('pc386_reset_hold_tb', 'fes_de25_pc386_standard.v',
                       'sim/pc386_reset_hold_tb.cpp',
                       (PC386 / 'fes_de25_hps_window.v', PC386 / 'sim/pc386_reset_hold_tb.v'), guard=True)
        wrapper = (PC386 / 'fes_de25_pc386_standard.v').read_text()
        self.assertIn('.p0_reset(p0_reset), .p1_reset(p1_reset), .p2_reset(p2_reset)', wrapper)
        self.assertIn('assign p0_reset = p0_guard_reset || hps_reset_hold;', wrapper)
        self.assertIn('.hold(window_hold)', wrapper)
        self.assertIn('.machine_held(window_hold)', wrapper)
        self.assertIn('.exec_reset(exec_reset || hps_reset_hold)', wrapper)

    def test_fault_record_keeps_the_first_fault(self):
        self.run_bench('fes_de25_hps_window', 'fes_de25_hps_window.v', 'window_fault_tb.cpp',
                       guard=True)


if __name__ == '__main__':
    unittest.main()
