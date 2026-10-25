# SPDX-License-Identifier: MIT
"""i386 front end and AG address unit: the check runner, the timing harness's
report reading, and (when the tools are present) the RTL itself. No network
access. Verilator cases skip without Verilator; the oracle-backed run skips
unless the pinned Bochs checkout is already in build/."""
import os
import shutil
import subprocess
import sys
import unittest

from scripts import benchmark_fes_i386 as benchmark
from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_frontend as frontend

VERILATOR = shutil.which(os.environ.get('VERILATOR', 'verilator'))
BOCHS = (oracle.BUILD / ('bochs-' + oracle.BOCHS_REVISION) / '.git').is_dir()


class RunnerOutputTest(unittest.TestCase):
    def test_parse_splits_records_and_failures(self):
        text = ('{"default_size": 16, "cases": 4, "compared": 3, "oracle_undefined": 1, "mismatches": 0}\n'
                'MISMATCH 16-bit [0f 0d 00 00 00] slot 0: rtl length 2 prefix 0 run 0, oracle length 3\n'
                'FAIL random-bytes: expected too_long at 00000010\n\n')
        records, failures = frontend.parse(text)
        self.assertEqual(records[0]['compared'], 3)
        self.assertEqual(len(failures), 2)
        with self.assertRaises(frontend.FrontendError):
            frontend.parse('segfault\n')

    def test_failed_needs_records_and_zero_counts(self):
        self.assertTrue(frontend.failed([]))
        self.assertFalse(frontend.failed([{'mismatches': 0, 'directed_failures': 0}]))
        for key in ('mismatches', 'failures', 'directed_failures', 'timing_failures'):
            with self.subTest(key=key):
                self.assertTrue(frontend.failed([{key: 1}]))

    def test_summary_reports_throughput(self):
        stream = {'stream': 'prefix-free', 'default_size': 32, 'instructions': 10, 'compared': 10,
                  'too_long': 0, 'failures': 0, 'issue_cycles': 10, 'timing_failures': 0}
        lines = frontend.summary([], [stream], [{'agu_vectors': 5, 'wrapped_16': 2, 'failures': 0}])
        self.assertIn('10 issued in 10 cycles', lines[0])
        self.assertIn('AG address unit: 5 vectors', lines[1])

    def test_every_bench_names_existing_sources(self):
        for name, (top, rtl, sim, source, _) in frontend.BENCHES.items():
            with self.subTest(bench=name):
                for path in (*(frontend.RTL / item for item in rtl), *(frontend.SIM / item for item in sim),
                             frontend.SIM / source):
                    self.assertTrue(path.is_file(), path)


class HarnessReportTest(unittest.TestCase):
    def test_path_summary_names_signals_and_carry_cells(self):
        def segment(kind, cell, delay):
            return {'type': kind, 'delay': delay, 'from': {'cell': cell}, 'to': {'cell': cell}}
        path = {'path': [
            segment('clk-to-q', 'stage_reg_MISTRAL_FF_Q', 0.7),
            segment('routing', 'agu.b_MISTRAL_ALUT6_Q', 1.0),
            segment('logic', 'agu.b_MISTRAL_ALUT6_Q', 0.5),
            segment('logic', 'agu.linear_MISTRAL_ALUT_ARITH_SO_1', 1.0),
            segment('routing', 'm1_MISTRAL_FF_Q_3', 0.8),
            segment('setup', 'm1_MISTRAL_FF_Q_3', -0.2),
        ]}
        summary = benchmark.path_summary(path)
        self.assertEqual(summary['signals'], ['stage_reg', 'agu.b', 'agu.linear', 'm1'])
        self.assertEqual((summary['logic_cells'], summary['carry_cells']), (2, 1))
        self.assertAlmostEqual(summary['routing_ns'], 1.8)
        self.assertFalse(summary['m10k_source'])

    def test_backend_follows_the_toolchain_identity(self):
        self.assertEqual(benchmark.expected_backend('nextpnr 1 gpu-router=HIP; hip-architectures=gfx1100'), 'hip')
        self.assertEqual(benchmark.expected_backend('nextpnr 1 gpu-router=CUDA; cuda-architectures=86'), 'cuda')

    def test_spike_sources_exist(self):
        for spike in benchmark.SPIKES:
            with self.subTest(spike=spike):
                for path in benchmark.sources(spike):
                    self.assertTrue((benchmark.ROOT / path).is_file(), path)
        self.assertTrue((benchmark.ROOT / benchmark.SPIKE / 'spike.qsf').is_file())


@unittest.skipUnless(VERILATOR, 'Verilator not installed')
class RtlTest(unittest.TestCase):
    def test_spikes_and_their_rtl_lint(self):
        for spike, (top, _) in benchmark.SPIKES.items():
            with self.subTest(spike=spike):
                files = [str(benchmark.ROOT / path) for path in benchmark.sources(spike)]
                result = subprocess.run([VERILATOR, '--lint-only', '-Wall', '-Wno-UNUSEDSIGNAL', '--top-module', top,
                                         *files], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_agu_matches_architectural_addresses(self):
        env = dict(os.environ, VERILATOR=VERILATOR)
        result = subprocess.run([sys.executable, 'scripts/test_fes_i386_frontend.py', '--bench', 'agu', '--jobs', '2'],
                                cwd=frontend.ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(' 0 failures', result.stdout)


@unittest.skipUnless(VERILATOR and BOCHS, 'Verilator or the pinned Bochs checkout is absent')
class OracleFrontEndTest(unittest.TestCase):
    def test_length_decoders_and_front_end_match_the_oracle(self):
        env = dict(os.environ, VERILATOR=VERILATOR)
        jobs = str(min(os.cpu_count() or 2, 8))
        result = subprocess.run([sys.executable, 'scripts/test_fes_i386_frontend.py', '--bench', 'lendec',
                                 '--bench', 'frontend', '--instructions', '4000', '--jobs', jobs],
                                cwd=frontend.ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('length decoders, 32-bit: 393216000 cases', result.stdout)
        self.assertIn('front end prefix-free, 16-bit: 4000 instructions', result.stdout)


if __name__ == '__main__':
    unittest.main()
