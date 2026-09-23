# SPDX-License-Identifier: GPL-2.0-or-later
"""The RAM tester stays on fes.application and the existing video interface."""

import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-ramtest"


class RamTestAbiTest(unittest.TestCase):
    def test_mailbox_and_video_are_fes_application(self):
        top = (CORE / "rtl" / "top.v").read_text()
        self.assertIn("fes_application_gp", top)
        self.assertIn("ENABLE_GAMEPAD(1)", top)
        self.assertIn("fes_video_720p", top)
        self.assertIn("16'h5555", (CORE / "rtl" / "mem_channel.v").read_text())
        self.assertIn("16'hAAAA", (CORE / "rtl" / "mem_channel.v").read_text())
        self.assertIn("cyclonev_hps_interface_fpga2sdram", (CORE / "rtl" / "hps_ddr_port.v").read_text())
        self.assertNotIn("opcode 18", top.lower())
        recipe = (ROOT / "scripts" / "build_fes_ramtest.py").read_text()
        self.assertIn('"id": "fes.application"', recipe)
        self.assertIn("fes.video.fixed-720p60", recipe)
        self.assertIn("fes.gamepad", recipe)
        self.assertIn("fes-gp-v1", recipe)
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


if __name__ == "__main__":
    unittest.main()
