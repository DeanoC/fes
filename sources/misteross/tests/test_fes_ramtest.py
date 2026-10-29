# SPDX-License-Identifier: GPL-2.0-or-later
"""The RAM tester stays on fes.application and scans the whole HPS DDR window."""

import contextlib
import io
import unittest
from unittest.mock import patch
from scripts import build_fes_ramtest as producer
from scripts import build_fes_ramtest_quartus as quartus
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-ramtest"
COMMON = ROOT / "cores" / "fes-common"

# The TimeQuest 17.0.2 tables the Quartus diagnostic's timing gate reads, at 130 MHz.
MEMORY_ROW = "; ram_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk   ; 0.412 ; 0.000         ;"
PIXEL_ROW = "; video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; 5.205 ; 0.000         ;"
ILLEGAL_ROW = "; Illegal Clocks                  ; 0     ; 0    ;"
UNCONSTRAINED_ROW = "; Unconstrained Clocks            ; 0     ; 0    ;"
PASSING_REPORT = f"""\
+--------------------------------------------------------------------------------------------+
; Clocks                                                                                     ;
+-----------------------------------------------------------+-----------+--------+-----------+
; Clock Name                                                ; Type      ; Period ; Frequency ;
+-----------------------------------------------------------+-----------+--------+-----------+
; FPGA_CLK1_50                                              ; Base      ; 20.000 ; 50.0 MHz  ;
; ram_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk   ; Generated ; 7.692  ; 130.0 MHz ;
; ram_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk   ; Generated ; 7.692  ; 130.0 MHz ;
; video_clock|pll|general[0].gpll~PLL_OUTPUT_COUNTER|divclk ; Generated ; 13.464 ; 74.27 MHz ;
+-----------------------------------------------------------+-----------+--------+-----------+


+-----------------------------------------------------------------------------------+
; Slow 1100mV 100C Model Setup Summary                                              ;
+-----------------------------------------------------------+-------+---------------+
; Clock                                                     ; Slack ; End Point TNS ;
+-----------------------------------------------------------+-------+---------------+
{MEMORY_ROW}
; ram_clock|pll|general[1].gpll~PLL_OUTPUT_COUNTER|divclk   ; 1.904 ; 0.000         ;
{PIXEL_ROW}
+-----------------------------------------------------------+-------+---------------+


+------------------------------------------------+
; Unconstrained Paths Summary                    ;
+---------------------------------+-------+------+
; Property                        ; Setup ; Hold ;
+---------------------------------+-------+------+
{ILLEGAL_ROW}
{UNCONSTRAINED_ROW}
; Unconstrained Input Ports       ; 2     ; 2    ;
; Unconstrained Input Port Paths  ; 2     ; 2    ;
; Unconstrained Output Ports      ; 34    ; 34   ;
; Unconstrained Output Port Paths ; 1102  ; 1102 ;
+---------------------------------+-------+------+
"""
# Each report and the error the gate must raise for it.
FAILING_REPORTS = (
    ("memory setup miss", PASSING_REPORT.replace(MEMORY_ROW, MEMORY_ROW.replace("; 0.412 ; 0.000 ", "; -0.123 ; -0.123")),
     "memory setup slack is -0.123 ns"),
    ("sub-picosecond memory setup miss",
     PASSING_REPORT.replace(MEMORY_ROW, MEMORY_ROW.replace("; 0.412 ; 0.000 ", "; -0.000 ; -0.000")),
     "memory setup slack is -0.000 ns"),
    ("pixel setup miss", PASSING_REPORT.replace(PIXEL_ROW, PIXEL_ROW.replace("; 5.205 ; 0.000 ", "; -1.250 ; -9.875")),
     "pixel setup slack is -1.250 ns"),
    ("sub-picosecond pixel setup miss",
     PASSING_REPORT.replace(PIXEL_ROW, PIXEL_ROW.replace("; 5.205 ; 0.000 ", "; -0.000 ; -0.000")),
     "pixel setup slack is -0.000 ns"),
    ("no unconstrained paths summary",
     PASSING_REPORT.replace(f"{ILLEGAL_ROW}\n", "").replace(f"{UNCONSTRAINED_ROW}\n", ""),
     "unconstrained paths summary"),
    ("no illegal clocks row", PASSING_REPORT.replace(f"{ILLEGAL_ROW}\n", ""), "unconstrained paths summary"),
    ("no unconstrained clocks row", PASSING_REPORT.replace(f"{UNCONSTRAINED_ROW}\n", ""),
     "unconstrained paths summary"),
    ("unconstrained clocks row twice",
     PASSING_REPORT.replace(UNCONSTRAINED_ROW, f"{UNCONSTRAINED_ROW}\n{UNCONSTRAINED_ROW}"),
     "unconstrained paths summary"),
    ("unconstrained setup clock", PASSING_REPORT.replace(UNCONSTRAINED_ROW, UNCONSTRAINED_ROW.replace("; 0     ;", "; 1     ;")),
     "unconstrained clocks: 1 setup, 0 hold"),
    ("illegal hold clock", PASSING_REPORT.replace(ILLEGAL_ROW, ILLEGAL_ROW.replace("; 0    ;", "; 3    ;")),
     "illegal clocks: 0 setup, 3 hold"),
)


class RamTestAbiTest(unittest.TestCase):
    def test_parent_cli_selects_100_mhz_and_preserves_explicit_130(self):
        for extra, expected in (([], 100), (["--memory-mhz", "130"], 130)):
            with self.subTest(memory_mhz=expected), patch.object(producer, "build") as build:
                with patch("sys.argv", ["build_fes_ramtest.py", "--root", str(ROOT),
                        "--package-output", str(ROOT / "build/packages"),
                        "--cache-root", "/tmp/ramtest-cache", "--identity-version", "2", *extra]):
                    self.assertEqual(producer.main(), 0)
                self.assertEqual(build.call_args.kwargs["memory_mhz"], expected)
                self.assertEqual(build.call_args.kwargs["package_store"], ROOT / "build/packages")
                self.assertEqual(build.call_args.kwargs["identity_version"], 2)

    def test_parent_authentication_uses_shipped_rate_and_shared_cache(self):
        with patch.object(producer, "authenticate_for", return_value={}) as authenticate:
            self.assertEqual(producer._authenticate_tools(ROOT, cache_root=Path("/tmp/cache")), {})
            authenticate.assert_called_once_with(ROOT, 100, Path("/tmp/cache"))

    def test_mailbox_and_video_are_fes_application(self):
        top = (CORE / "rtl" / "top.v").read_text()
        self.assertIn("fes_application_gp", top)
        self.assertIn("ENABLE_GAMEPAD(1), .ENABLE_HPS_DDR(1)", top)
        self.assertIn("fes_video_720p", top)
        self.assertIn("16'h5555", (CORE / "rtl" / "mem_channel.v").read_text())
        self.assertIn("16'hAAAA", (CORE / "rtl" / "mem_channel.v").read_text())
        self.assertIn("pattern_latch", top)
        self.assertIn("pattern_latch.v", (ROOT / "scripts" / "sim_fes_ramtest.py").read_text())
        self.assertNotIn("opcode 18", top.lower())
        recipe = (ROOT / "scripts" / "build_fes_ramtest.py").read_text()
        self.assertIn("cores/fes-ramtest/rtl/pattern_latch.v", recipe)
        self.assertIn('"id": "fes.application"', recipe)
        self.assertIn("fes.video.fixed-720p60", recipe)
        self.assertIn("fes.gamepad", recipe)
        self.assertIn("fes-gp-v1", recipe)
        self.assertIn('{"id": "fes.memory.hps-ddr", "major": 1, "minor": 0, "required": True}', recipe)
        self.assertIn('TOOLCHAIN_LOCK = "cores/fes-ramtest/toolchain.lock"', recipe)
        qsf = (CORE / "constraints.qsf").read_text()
        self.assertIn("HDMI_TX_CLK", qsf)
        self.assertIn("SDRAM_DQ[15]", qsf)
        port = (CORE / "rtl" / "sdram_addon_port.v").read_text()
        self.assertIn(".datain_h(1'b0)", port)
        self.assertIn(".datain_l(1'b1)", port)
        self.assertIn("`ifdef QUARTUS", top)
        self.assertIn("`RAMTEST_BUILD_ID", top)
        quartus = (ROOT / "scripts" / "build_fes_ramtest_quartus.py").read_text()
        self.assertIn("RAMTEST_BUILD_ID", quartus)
        self.assertIn("cores/fes-ramtest/rtl/pattern_latch.v", quartus)
        self.assertIn("RAM_RATE_SWEEP", top)
        self.assertIn("altclkctrl", top)
        self.assertIn('.enable_bus_hold("OFF")', top)
        self.assertIn('.enable_bus_hold("FALSE")', top)
        pll = (CORE / "rtl" / "ram_pll.v").read_text()
        self.assertIn("75.0 MHz", pll)
        self.assertIn("100.0 MHz", pll)
        self.assertIn("2'b10", top)
        self.assertIn("2'b11", top)


    def test_hps_ddr_ports_use_the_shared_layout_and_window(self):
        wrapper = (COMMON / "rtl" / "fes_hps_ddr.v").read_text()
        self.assertIn("cyclonev_hps_interface_fpga2sdram", wrapper)
        for macro in ("PORT_WIDTH", "CPORT_TYPE", "CPORT_WFIFO_MAP", "CPORT_RFIFO_MAP",
                      "WFIFO_CPORT_MAP", "RFIFO_CPORT_MAP", "AXI_MM_SELECT"):
            self.assertIn(f"`FES_APPLICATION_HPS_DDR_CFG_{macro}", wrapper)
        # Quartus f2sdram::add_port wiring: port 0 readdatavalid is data port 1.
        self.assertIn(".m_readdatavalid(rd_valid_1)", wrapper)
        self.assertIn(".cmd_data_0({18'd0, m0_burstcount, 4'd0, m0_address, command_write0, m0_read})", wrapper)
        for parameter in ("P0_WRITE_ENABLE", "P1_ENABLE", "P2_ENABLE"):
            self.assertIn(f"parameter [0:0] {parameter} = 1'b1", wrapper)
        self.assertIn("wire command_write0 = P0_WRITE_ENABLE && m0_write;", wrapper)
        self.assertIn("fes_hps_ddr_guard", wrapper)
        top = (CORE / "rtl" / "top.v").read_text()
        self.assertIn("`FES_APPLICATION_HPS_DDR_WINDOW_BASE", top)
        self.assertIn("`FES_APPLICATION_HPS_DDR_WINDOW_BYTES / 2", top)
        self.assertEqual(top.count("ddr_channel #("), 3)
        channel = (CORE / "rtl" / "ddr_channel.v").read_text()
        for name in ("ZERO", "ONES", "CHCK", "WALK", "INVR", "BYTE", "ADDR"):
            self.assertIn(name, channel)
        for recipe in ("build_fes_ramtest.py", "build_fes_ramtest_quartus.py"):
            text = (ROOT / "scripts" / recipe).read_text()
            for source in ("fes_hps_ddr.v", "fes_hps_ddr_guard.v", "ddr_channel.v", "ddr_rates.v",
                           "lat_stats.v", "lat_monitor.v"):
                self.assertIn(source, text)
            self.assertNotIn("hps_ddr_port.v", text)
        sim = (ROOT / "scripts" / "sim_fes_ramtest.py").read_text()
        self.assertIn("fes_hps_ddr_guard.v", sim)
        for source in ("lat_stats.v", "lat_monitor.v", "lat_stats_tb.cpp"):
            self.assertIn(source, sim)

    def test_quartus_timing_gate_requires_every_clock_analysed(self):
        with patch.object(quartus, "MHZ", "130"):
            with contextlib.redirect_stdout(io.StringIO()) as output:
                quartus.validate_timing(PASSING_REPORT)
            self.assertIn("memory setup slack 0.412 ns", output.getvalue())
            self.assertIn("pixel setup slack 5.205 ns", output.getvalue())
            for name, report, message in FAILING_REPORTS:
                self.assertNotEqual(report, PASSING_REPORT, name)
                with self.subTest(name), contextlib.redirect_stdout(io.StringIO()), \
                        self.assertRaisesRegex(quartus.BuildError, message):
                    quartus.validate_timing(report)


if __name__ == "__main__":
    unittest.main()
