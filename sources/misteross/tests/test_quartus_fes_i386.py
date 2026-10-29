# SPDX-License-Identifier: MIT
"""The i386 Quartus oracle's netlist names, stage labels and TimeQuest
parsing. No Quartus or Yosys needed."""
import unittest

from scripts import benchmark_fes_i386 as benchmark
from scripts import quartus_fes_i386 as oracle

STAGES = benchmark.STAGES['cpu']

NETLIST = r'''
  wire [473:0] \dut.ag_out ;
  wire [261:0] \dut.ag_s ;
  wire [473:0] \dut.m1_s ;
  wire [473:0] \dut.f$result ;
  reg [7:0] _00010_;
  assign { \dut.ag_out [3:0], \dut.ag_s [5:3], \dut.ag_s [0] } = _00010_;
  reg [15:0] _00011_;
  assign \dut.ag_s [229:214] = _00011_;
  reg [5:0] _00012_;
  assign { \dut.f$result [3:0], \dut.m1_s [1:0] } = _00012_;
'''


class NamesTest(unittest.TestCase):
    def test_register_bits_map_to_the_signals_they_drive(self):
        names = oracle.register_names(NETLIST)
        self.assertEqual(names[('_00010_', 0)], 'dut.ag_s')
        self.assertEqual(names[('_00010_', 3)], 'dut.ag_s')
        self.assertEqual(names[('_00010_', 4)], 'dut.ag_out')
        self.assertEqual(names[('_00011_', 15)], 'dut.ag_s')
        # A function's $-named result takes its register's other name.
        self.assertEqual(names[('_00012_', 5)], 'dut.m1_s')
        self.assertEqual(names[('dut.f$result', -1)], 'dut.m1_s')

    def test_labels_follow_the_benchmark_stages(self):
        names = oracle.register_names(NETLIST)
        self.assertEqual(oracle.node_label('dut.frontend.align.q[7]~DUPLICATE', STAGES), 'Q')
        self.assertEqual(oracle.node_label('_00011_[2]', STAGES, names), 'D')
        self.assertEqual(oracle.node_label('_00012_[5]', STAGES, names), 'AG')
        self.assertEqual(oracle.node_label('\\dut.f$result_reg', STAGES, names), 'AG')
        self.assertEqual(oracle.node_label('dut.m1_s_reg[4]', STAGES, names), 'AG')
        shift = 'altshift_taps:_00010__rtl_0|shift_taps_agv:auto_generated|dffe3a[1]'
        self.assertEqual(oracle.node_label(shift, STAGES, names), 'D')
        ram = 'altsyncram:l1i_tags_rtl_0|altsyncram_o3q1:auto_generated|ram_block1a66~portb_re_reg'
        self.assertEqual(oracle.node_label(ram, STAGES, names), 'F1')
        self.assertEqual(oracle.node_label('stimulus.lfsr[3]', STAGES, names), 'envelope')


class ParseTest(unittest.TestCase):
    def test_worst_path_per_stage_and_critical_points(self):
        text = '\n'.join([
            'fmax\tclk 73.81 73.81 {}',
            'path\tdut.frontend.align.start[1]\tdut.frontend.align.q[7]\t-3.549\t7\t6.235\t6.649',
            'path\tdut.frontend.align.start[1]\tdut.frontend.align.q[6]\t-1.000\t9\t5.0\t5.0',
            'path\tdut.ex.u[3]\tdut.wb_s[1]\t-2.000\t11\t7.0\t5.0',
            'point\t1\tlaunch\t0.000\t',
            'point\t1\tclknet\t4.396\tFPGA_CLK1_50',
            'point\t1\tcell\t0.300\tdut.frontend.align.start[1]|q',
            'point\t1\tic\t1.200\t',
            'point\t1\tcell\t0.500\tMux1|combout',
        ])
        result = oracle.parse_sta(text, 100.0, STAGES)
        self.assertEqual(result['fmax_mhz'], 73.81)
        self.assertEqual(result['stages']['Q']['slack_ns'], -3.549)
        self.assertEqual(result['stages']['Q']['fmax_mhz'], 73.81)
        self.assertEqual(result['stages']['Q']['max_logic_levels'], 9)
        self.assertEqual(result['stages']['EX']['logic_levels'], 11)
        self.assertEqual(result['critical_paths'], [{'rank': 1, 'delay_ns': 2.0, 'interconnect_ns': 1.2, 'cells': 2,
                                                     'nodes': ['dut.frontend.align.start[1]|q', 'Mux1|combout']}])


if __name__ == '__main__':
    unittest.main()
