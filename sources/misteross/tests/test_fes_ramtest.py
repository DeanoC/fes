# SPDX-License-Identifier: GPL-2.0-or-later
"""The RAM tester stays on fes.application and scans the whole HPS DDR window."""

import unittest
from unittest.mock import patch
from scripts import build_fes_ramtest as producer
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-ramtest"
COMMON = ROOT / "cores" / "fes-common"


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
        self.assertNotIn("opcode 18", top.lower())
        recipe = (ROOT / "scripts" / "build_fes_ramtest.py").read_text()
        self.assertIn('"id": "fes.application"', recipe)
        self.assertIn("fes.video.fixed-720p60", recipe)
        self.assertIn("fes.gamepad", recipe)
        self.assertIn("fes-gp-v1", recipe)
        self.assertIn('{"id": "fes.memory.hps-ddr", "major": 1, "minor": 0, "required": True}', recipe)
        self.assertIn('TOOLCHAIN_LOCK = "toolchain.lock"', recipe)
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
            for source in ("fes_hps_ddr.v", "fes_hps_ddr_guard.v", "ddr_channel.v", "ddr_rates.v"):
                self.assertIn(source, text)
            self.assertNotIn("hps_ddr_port.v", text)
        sim = (ROOT / "scripts" / "sim_fes_ramtest.py").read_text()
        self.assertIn("fes_hps_ddr_guard.v", sim)


if __name__ == "__main__":
    unittest.main()
