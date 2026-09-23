import unittest
from pathlib import Path

from scripts.experiment_policy import policy_for


ROOT = Path(__file__).resolve().parents[1]
SDRAM = ROOT / "experiments/910_sdram_addon"
HPS = ROOT / "experiments/911_hps_ddr"
BEL = "cyclonev_hps_interface_fpga2sdram.52.53.0"
CELL = "cyclonev_hps_interface_fpga2sdram"


class OssMemoryTests(unittest.TestCase):
    def test_sdram_addon_is_the_shared_gpio_header(self) -> None:
        for relative in (
            "rtl/top.v",
            "pins.qsf",
            "sim/wrap.v",
            "sim/sdram_model.v",
            "sim/tb.cpp",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = SDRAM / relative
            self.assertTrue(path.is_file(), path)
        rtl = (SDRAM / "rtl/top.v").read_text(encoding="utf-8")
        qsf = (SDRAM / "pins.qsf").read_text(encoding="utf-8")
        self.assertIn("32'hf5000000", rtl)
        self.assertIn("7'd18", rtl)
        self.assertIn("16'h4546", rtl)
        self.assertIn("PIN_Y11 -to SDRAM_A[0]", qsf)
        self.assertIn("PIN_AH3 -to SDRAM_DQ[15]", qsf)
        self.assertIn("PIN_AD20 -to SDRAM_CLK", qsf)
        self.assertNotIn("SDRAM2", qsf)
        policy = policy_for("910_sdram_addon")
        policy.validate_source_text("experiments/910_sdram_addon/rtl/top.v", rtl)
        policy.validate_source_text("experiments/910_sdram_addon/pins.qsf", qsf)
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {"cyclonev_hps_interface_mpu_general_purpose": 1},
        )

    def test_hps_ddr_targets_the_fpga2sdram_site(self) -> None:
        for relative in (
            "rtl/top.v",
            "pins.qsf",
            "sim/hps_ddr_model.v",
            "sim/tb.cpp",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = HPS / relative
            self.assertTrue(path.is_file(), path)
        rtl = (HPS / "rtl/top.v").read_text(encoding="utf-8")
        probe = (HPS / "hardware/probe.sh").read_text(encoding="utf-8")
        self.assertIn(CELL, rtl)
        self.assertIn("32'hf5000000", rtl)
        self.assertIn("7'd18", rtl)
        self.assertIn("0xFFC25080", probe)
        self.assertIn("0x3fff", probe)
        policy = policy_for("911_hps_ddr")
        policy.validate_source_text("experiments/911_hps_ddr/rtl/top.v", rtl)
        policy.validate_source_text(
            "experiments/911_hps_ddr/pins.qsf",
            (HPS / "pins.qsf").read_text(encoding="utf-8"),
        )
        self.assertEqual(dict(policy.required_nextpnr_bels), {CELL: BEL})
        self.assertEqual(dict(policy.required_synth_cells), {CELL: 1})
