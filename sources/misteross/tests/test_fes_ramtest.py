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


if __name__ == "__main__":
    unittest.main()
