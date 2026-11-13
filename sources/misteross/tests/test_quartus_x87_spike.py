# SPDX-License-Identifier: MIT
"""The x87 fit spike runner's overlay and report parsing. No Quartus or Yosys needed."""
import contextlib
import io
import sys
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from scripts import quartus_x87_spike as spike

STA = '''
+-------------------------------------------------------------------------------------+
; Multicorner Timing Analysis Summary                                                                                     ;
+-------------------------------------------------------------+--------+-------+----------+---------+---------------------+
; Clock                                                       ; Setup  ; Hold  ; Recovery ; Removal ; Minimum Pulse Width ;
+-------------------------------------------------------------+--------+-------+----------+---------+---------------------+
; Worst-case Slack                                            ; 1.306  ; 0.094 ; 6.569    ; 0.403   ; 1.122               ;
;  FPGA_CLK1_50                                               ; N/A    ; N/A   ; N/A      ; N/A     ; 9.274               ;
;  system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; 2.533  ; 0.111 ; 6.569    ; 2.509   ; 6.680               ;
; Design-wide TNS                                             ; 0.0    ; 0.0   ; 0.0      ; 0.0     ; 0.0                 ;
;  system_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; 0.000  ; 0.000 ; 0.000    ; 0.000   ; 0.000               ;
+-------------------------------------------------------------+--------+-------+----------+---------+---------------------+
'''

FIT = '''
; Fitter Resource Utilization by Entity ;
+------+------+
; Compilation Hierarchy Node ; ALMs needed [=A-B+C] ; Dedicated Logic Registers ; M10Ks ; DSP Blocks ; Full Hierarchy Name ;
+------+------+
; |spike_x87                 ; 2400.5 (10.0)        ; 3000 (20)                 ; 12    ; 9          ; |spike_x87 ;
;    |x87s_unit:dut|         ; 2300.0 (1500.0)      ; 2900 (2000)               ; 12    ; 9          ; |spike_x87|x87s_unit:dut ;
;       |x87s_mul:mul|       ; 400.0 (400.0)        ; 700 (700)                 ; 0     ; 9          ; |spike_x87|x87s_unit:dut|x87s_mul:mul ;
;       |other:deep|         ; 1.0 (1.0)            ; 1 (1)                     ; 0     ; 0          ; |spike_x87|x87s_unit:dut|other:deep ;
+------+------+
'''


class OverlayTest(unittest.TestCase):
    def test_overlay_wires_the_port_once_into_the_io_path(self):
        text = (spike.ROOT / spike.SYSTEM).read_text()
        overlaid = spike.overlay_system(text, spike.VARIANTS['d-hps'])
        self.assertEqual(overlaid.count('x87s_port #(.MUL(0), .SHIFT(0), .DIVSQRT(0), .TRANS(0)) x87 ('), 1)
        self.assertIn("{24'd0, x87_io_rdata[7:0]}", overlaid)
        # Declared before the IN data uses it.
        self.assertLess(overlaid.index('wire [31:0] x87_io_rdata'), overlaid.index('io_result <= cpu_io_size'))
        self.assertNotIn('x87', text)

    def test_overlay_refuses_a_moved_anchor(self):
        with self.assertRaises(spike.SpikeError):
            spike.overlay_system('module pc386_system; endmodule', spike.VARIANTS['a'])

    def test_hierarchy_drops_only_flatten(self):
        program = 'proc -norom; flatten; opt -purge; '
        self.assertEqual(spike.flatten(program, False), program)
        self.assertEqual(spike.flatten(program, True), 'proc -norom; opt -purge; ')

    def test_plain_module_names_drop_paramod(self):
        with tempfile.TemporaryDirectory() as tmp:
            netlist = Path(tmp) / 'n.v'
            netlist.write_text("module \\$paramod$4e48c8\\x87s_unit (a);\n"
                               "  \\$paramod\\x87s_mul\\MODE=s32'0001 mul (.a(a));\nendmodule\n")
            spike.plain_module_names(netlist)
            text = netlist.read_text()
        self.assertNotIn('$paramod', text)
        self.assertRegex(text, r'module x87s_unit_[0-9a-f]{8} \(a\);')
        self.assertRegex(text, r'  x87s_mul_[0-9a-f]{8} mul ')


class ReportTest(unittest.TestCase):
    def test_multicorner_reads_each_clock(self):
        clocks = spike.multicorner(STA)
        self.assertEqual(clocks['worst'], {'setup_ns': 1.306, 'hold_ns': 0.094})
        self.assertEqual(clocks[spike.SYSTEM_CLOCK], {'setup_ns': 2.533, 'hold_ns': 0.111})
        self.assertEqual(clocks['FPGA_CLK1_50'], {'setup_ns': None, 'hold_ns': None})

    def test_entities_keep_x87_rows_below_the_depth(self):
        rows = spike.entities(FIT, depth=2)
        self.assertEqual([row['entity'] for row in rows], ['|spike_x87', '|x87s_unit:dut|', '|x87s_mul:mul|'])
        self.assertEqual(rows[2], {'entity': '|x87s_mul:mul|', 'level': 2, 'alms': 400.0, 'own_alms': 400.0,
                                   'registers': 700.0, 'm10k': 0.0, 'dsp': 9.0})

    def test_paths_collect_cells(self):
        text = ('path\tworst\t1\t0.5\t7\t15.2\ta\tb\n'
                'point\tworst\t1\tic\t0.3\tn1\n'
                'point\tworst\t1\tcell\t0.2\tn2\n')
        self.assertEqual(spike.paths(text)['worst'][0]['cells'], ['n2'])


class JobTest(unittest.TestCase):
    def test_run_stops_a_tool_at_its_cap(self):
        with tempfile.TemporaryDirectory() as tmp:
            err = io.StringIO()
            with contextlib.redirect_stderr(err), self.assertRaisesRegex(spike.SpikeError, 'cap'):
                spike.run([sys.executable, '-c', 'import time; time.sleep(60)'], Path(tmp), Path(tmp) / 'log',
                          {}, cap_s=0.5, heartbeat_s=0.1)
        self.assertIn('heartbeat: ', err.getvalue())

    def test_run_reports_a_failed_tool(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaisesRegex(spike.SpikeError, r'exit 3'):
                spike.run([sys.executable, '-c', 'raise SystemExit(3)'], Path(tmp), Path(tmp) / 'log', {})

    def test_free_space_below_30_gib_is_refused(self):
        with tempfile.TemporaryDirectory() as tmp:
            usage = mock.Mock(free=29 * 1024**3)
            with mock.patch.object(spike.shutil, 'disk_usage', return_value=usage) as disk:
                with self.assertRaisesRegex(spike.SpikeError, '30 GiB'):
                    spike.check_free_space(Path(tmp) / 'new' / 'out')
            disk.assert_called_once_with(Path(tmp))
            with mock.patch.object(spike.shutil, 'disk_usage', return_value=mock.Mock(free=31 * 1024**3)):
                spike.check_free_space(Path(tmp) / 'new')


if __name__ == '__main__':
    unittest.main()
