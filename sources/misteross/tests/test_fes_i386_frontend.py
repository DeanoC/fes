# SPDX-License-Identifier: MIT
"""i386 front end, D's address operands and AG: the check runner, the timing
harness's report reading, and (when the tools are present) the RTL itself. No
network access. Verilator cases skip without Verilator; the oracle-backed run
skips unless the pinned Bochs checkout is already in build/; the netlist run
needs a Yosys named by FES_I386_YOSYS."""
import json
import os
import shutil
import subprocess
import sys
import unittest

from scripts import benchmark_fes_i386 as benchmark
from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_frontend as frontend

VERILATOR = shutil.which(os.environ.get('VERILATOR', 'verilator'))
YOSYS = shutil.which(os.environ.get('FES_I386_YOSYS', '')) if os.environ.get('FES_I386_YOSYS') else None
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
                  'too_long': 0, 'failures': 0, 'issue_cycles': 10, 'timing_failures': 0,
                  'redirect_to_issue': {'5': 3}}
        dag = {'uops': 7, 'dag_cases': 9, 'addresses': 5, 'wrapped_16': 2, 'late_base': [1, 0, 0],
               'late_index': [0, 1, 0], 'stall_not_ready': 2, 'stall_both_late': 1, 'stall_scaled_late': 1,
               'released': 2, 'wait_cycles': {'early': [0, 0, 1], 'load': [2, 2, 3], 'late': [3, 3, 4]},
               'failures': 0}
        lines = frontend.summary([], [stream], [dag])
        self.assertIn('10 issued in 10 cycles', lines[0])
        self.assertIn('redirect to first issue: 5 cycles x3', lines[0])
        self.assertIn('D and AG: 7 uops in 9 D cycles, 5 addresses', lines[1])
        self.assertIn('load [2, 2, 3]', lines[1])
        self.assertIn('1 late scaled index', lines[1])

    def test_every_bench_names_existing_sources(self):
        for name, (top, rtl, sim, source, _, synthesized) in frontend.BENCHES.items():
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
        path['path'][0] = {'type': 'clk-to-q', 'delay': 1.0, 'from': {'cell': 'tags.2.0.0', 'port': 'B1DATA[2]'},
                           'to': {'cell': 'tags.2.0.0', 'port': 'B1DATA[2]'}}
        self.assertTrue(benchmark.path_summary(path)['m10k_source'])

    def test_stage_figures_take_each_stages_worst_slack(self):
        def net(*endpoints):
            return {'endpoints': [{'cell': cell, 'setup_checked': checked, 'setup_slack_ns': slack}
                                  for cell, slack, checked in endpoints]}
        report = {'detailed_net_timings': [
            net(('dut.fetch.l1_bytes_MISTRAL_FF_Q_3', 1.5, True), ('dut.align.start_MISTRAL_FF_Q', -0.5, True)),
            net(('dut.align.fill_data_MISTRAL_FF_Q_9', 0.5, True), ('dut.align.dec_MISTRAL_FF_Q_1', 2.0, True)),
            net(('signature_MISTRAL_FF_Q', 4.0, True), ('dut.align.start_MISTRAL_ALUT6', -9.0, False)),
            net(('dut.align.redirect_linear_MISTRAL_FF_Q', 5.0, True)),
        ]}
        stages = benchmark.STAGES['frontend']
        figures = benchmark.stage_figures(report, stages, 100.0)
        self.assertEqual(figures['L1'], {'setup_slack_ns': 0.5, 'fmax_mhz': 105.26, 'endpoint': 'dut.align.fill_data'})
        self.assertEqual(figures['F2']['endpoint'], 'dut.fetch.l1_bytes')
        self.assertEqual(figures['Q']['fmax_mhz'], 95.24)
        self.assertEqual(figures['L2']['endpoint'], 'dut.align.dec')
        self.assertEqual(figures['envelope']['endpoint'], 'signature')
        self.assertEqual([item['endpoint'] for item in benchmark.envelope_endpoints(report, stages)],
                         ['signature', 'dut.align.redirect_linear'])

    @staticmethod
    def netlist(cells, names):
        def cell(kind, **ports):
            outputs = {'Q', 'SO', 'CO', 'B1DATA'}
            return {'type': kind, 'connections': ports,
                    'port_directions': {port: 'output' if port in outputs else 'input' for port in ports}}
        return {'modules': {'top': {
            'cells': {name: cell(kind, **ports) for name, (kind, ports) in cells.items()},
            'netnames': {name: {'bits': bits, 'hide_name': 0} for name, bits in names.items()}}}}

    def test_registers_are_labelled_by_any_alias_then_their_input(self):
        netlist = self.netlist({
            # The F2 register, named only as an L1 decoder input, is found by its input.
            'dut.fetch.decoders.g_oplen[4].oplen.o_MISTRAL_FF_Q': ('MISTRAL_FF', {'Q': [10], 'DATAIN': [11]}),
            # The L1/L2 register named after L2's input port.
            'dut.fetch.sums.mb32_MISTRAL_FF_Q': ('MISTRAL_FF', {'Q': [12], 'DATAIN': [13]}),
            # A stimulus register that took Q's port name stays envelope.
            'redirect_linear_MISTRAL_FF_Q': ('MISTRAL_FF', {'Q': [14], 'DATAIN': [15]}),
            # So does one whose input is named after Q's record.
            'signature_MISTRAL_FF_Q': ('MISTRAL_FF', {'Q': [18], 'DATAIN': [19]}),
            'l1i_data0.0.0.0': ('MISTRAL_M10K', {'B1DATA': [16], 'B1ADDR': [17]}),
        }, {'dut.fetch.decoders.g_oplen[4].oplen.o': [10], 'dut.fetch.mem_data': [11],
            'dut.fetch.sums.mb32': [12], 'redirect_linear': [14], 'dut.align.redirect_linear': [14],
            'dut.align.q': [19]})
        labels = benchmark.register_labels(netlist, 'top', benchmark.STAGES['frontend'])
        self.assertEqual(labels, {'dut.fetch.decoders.g_oplen[4].oplen.o_MISTRAL_FF_Q': 'F2',
                                  'dut.fetch.sums.mb32_MISTRAL_FF_Q': 'L1',
                                  'redirect_linear_MISTRAL_FF_Q': 'envelope', 'signature_MISTRAL_FF_Q': 'envelope',
                                  'l1i_data0.0.0.0': 'F1'})
        self.assertEqual(benchmark.cells_by_stage(netlist, 'top', labels),
                         {'F1': 1, 'F2': 1, 'L1': 1, 'envelope': 2})

    def test_route_jsonl_row_joins_route_with_synthesis_and_tool_identity(self):
        identity = {
            'lock': 'toolchain.lock', 'lock_sha256': 'lock-sha', 'synthesis': 'proc -norom; synth_intel_alm',
            'sources': {'rtl.sv': 'source-sha'},
            'tools': {'nextpnr-mistral': 'commit=123456789abcdef; sha256=tool-sha; gpu-router=HIP'},
        }
        synthesis = {
            'cells': {'MISTRAL_ALUT4': 3, 'MISTRAL_ALUT_ARITH': 2, 'MISTRAL_FF': 5, 'MISTRAL_M10K': 1},
            'cells_by_stage': {'F1': 1, 'F2': 5}, 'logic_depth': {'F1': {'lut_levels': 2}},
            'synth_json_sha256': 'synth-sha',
        }
        run = {
            'argv': ['nextpnr-mistral', '--seed', '2'], 'elapsed_seconds': 4.5, 'envelope_worst': [],
            'fmax_mhz': 92.0, 'gpu_backend': 'hip', 'gpu_device_granted': ['0'], 'seed': 2,
            'spike': 'frontend', 'stages': {'F1': {'fmax_mhz': 92.0}},
            'critical_paths': [{'from': 'a', 'to': 'b'}],
        }
        row = benchmark.route_record(run, identity, 'fingerprint', synthesis, 'round 4')
        self.assertEqual(row['nextpnr'], '12345678')
        self.assertEqual((row['comb'], row['ff'], row['m10k']), (5, 5, 1))
        self.assertEqual(row['cells_by_stage'], {'F1': 1, 'F2': 5})
        self.assertEqual(row['critical_path'], {'from': 'a', 'to': 'b'})
        self.assertEqual((row['source_fingerprint'], row['synth_json_sha256'], row['version']),
                         ('fingerprint', 'synth-sha', 'round 4'))
        self.assertIn('tools', row)
        self.assertNotIn('registers_by_stage', row)
        checked_record = json.loads((benchmark.ROOT / 'docs/validation/2026-10-25-i386-phase1-front-end-fixes.jsonl')
                                    .read_text().splitlines()[0])
        self.assertEqual(set(row), set(checked_record))

    def test_logic_depth_counts_luts_and_carry_cells(self):
        netlist = self.netlist({
            'amode.sib_MISTRAL_FF_Q': ('MISTRAL_FF', {'Q': [1], 'DATAIN': [20]}),
            'lut_a': ('MISTRAL_ALUT2', {'A': [1], 'B': [1], 'Q': [2]}),
            'not_a': ('MISTRAL_NOT', {'A': [2], 'Q': [3]}),
            'lut_b': ('MISTRAL_ALUT3', {'A': [3], 'B': [1], 'C': [1], 'Q': [4]}),
            'carry_0': ('MISTRAL_ALUT_ARITH', {'A': [4], 'B': ['0'], 'CI': ['0'], 'SO': [5], 'CO': [6]}),
            'carry_1': ('MISTRAL_ALUT_ARITH', {'A': ['0'], 'B': [1], 'CI': [6], 'SO': [7], 'CO': [8]}),
            'ag_row_MISTRAL_FF_Q': ('MISTRAL_FF', {'Q': [9], 'DATAIN': [7]}),
        }, {'amode.sib': [1], 'dfwd.row': [4], 'ag_row': [9]})
        depth = benchmark.logic_depth(netlist, 'top', benchmark.STAGES['dag'])
        self.assertEqual(depth['D'], {'lut_levels': 3, 'carry_cells': 2, 'endpoint': 'ag_row',
                                      'path': ['amode.sib', 'dfwd.row']})

    def test_backend_follows_the_toolchain_identity(self):
        self.assertEqual(benchmark.expected_backend('nextpnr 1 gpu-router=HIP; hip-architectures=gfx1100'), 'hip')
        self.assertEqual(benchmark.expected_backend('nextpnr 1 gpu-router=CUDA; cuda-architectures=86'), 'cuda')

    def test_spike_sources_exist(self):
        for spike in benchmark.SPIKES:
            with self.subTest(spike=spike):
                for path in benchmark.sources(spike):
                    self.assertTrue((benchmark.ROOT / path).is_file(), path)
        self.assertTrue((benchmark.ROOT / benchmark.SPIKE / 'spike.qsf').is_file())

    def test_spikes_default_to_the_i386_lock(self):
        from scripts import lockfile
        self.assertEqual(benchmark.LOCK, benchmark.RTL / 'toolchain.lock')
        self.assertEqual(lockfile.validate_lock(benchmark.ROOT / benchmark.LOCK), [])


@unittest.skipUnless(VERILATOR, 'Verilator not installed')
class RtlTest(unittest.TestCase):
    def test_spikes_and_their_rtl_lint(self):
        for spike, (top, _) in benchmark.SPIKES.items():
            with self.subTest(spike=spike):
                files = [str(benchmark.ROOT / path) for path in benchmark.sources(spike)]
                result = subprocess.run([VERILATOR, '--lint-only', '-Wall', '-Wno-UNUSEDSIGNAL', '--top-module', top,
                                         *files], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_d_and_ag_follow_the_late_operand_rule(self):
        env = dict(os.environ, VERILATOR=VERILATOR, FES_I386_DAG_ROUNDS='2')
        result = subprocess.run([sys.executable, 'scripts/test_fes_i386_frontend.py', '--bench', 'dag', '--jobs', '2'],
                                cwd=frontend.ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('D and AG: 196608 uops in', result.stdout)
        self.assertIn('load [2, 2, 3], late [3, 3, 4]', result.stdout)
        self.assertIn('; 0 failures', result.stdout)

    @unittest.skipUnless(YOSYS, 'FES_I386_YOSYS names no Yosys')
    def test_yosys_reads_d_and_ag_as_simulated(self):
        env = dict(os.environ, VERILATOR=VERILATOR, FES_I386_DAG_ROUNDS='2')
        result = subprocess.run([sys.executable, 'scripts/test_fes_i386_frontend.py', '--bench', 'dag', '--jobs', '2',
                                 '--yosys', YOSYS], cwd=frontend.ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('; 0 failures', result.stdout)


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
        self.assertIn('front end fetch-stalled, 32-bit:', result.stdout)
        self.assertIn('cross-half instruction', result.stdout)


if __name__ == '__main__':
    unittest.main()
