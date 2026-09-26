import re
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments" / "080_dsp_mem" / "rtl" / "top.v"
TB = ROOT / "experiments" / "080_dsp_mem" / "sim" / "tb.cpp"
EXPECTED = ROOT / "experiments" / "080_dsp_mem" / "expected.md"
MODEL = ROOT / "experiments" / "020_linux_mailbox" / "sim" / "hps_gp_model.v"


class DspMemSourcePolicyTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for path in (RTL, TB, EXPECTED, MODEL):
            self.assertTrue(path.is_file(), path)

    def test_rtl_exposes_clock_only_ports_product_and_tables(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^\s*module\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
            rtl,
        )
        self.assertIsNotNone(match, "top module declaration is missing")
        self.assertEqual(
            re.sub(r"\s+", " ", match.group(1).strip()),
            "input wire FPGA_CLK1_50",
        )
        self.assertEqual(
            len(re.findall(r"\bcyclonev_hps_interface_mpu_general_purpose\b", rtl)),
            1,
        )
        self.assertIn("16'hD810", rtl)
        self.assertRegex(
            rtl,
            r'\(\*\s*multstyle\s*=\s*"dsp"\s*\*\)\s*wire\s+\[15:0\]\s+wide_product',
        )
        self.assertRegex(
            rtl,
            r'\(\*\s*ramstyle\s*=\s*"mlab"\s*\*\)\s*reg\s+\[7:0\]\s+lab_store',
        )
        self.assertRegex(
            rtl,
            r'\(\*\s*ramstyle\s*=\s*"M10K"\s*\*\)\s*reg\s+\[7:0\]\s+block_store',
        )
        for forbidden in ("LED", "PLL", "LUTRAM", "HDMI"):
            self.assertNotRegex(
                rtl,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_production_source_satisfies_closed_policy(self) -> None:
        policy = policy_for("080_dsp_mem")
        relative = "experiments/080_dsp_mem/rtl/top.v"
        policy.validate_source_text(relative, RTL.read_text(encoding="utf-8"))
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                relative,
                RTL.read_text(encoding="utf-8") + "\nwire LED;\n",
            )
