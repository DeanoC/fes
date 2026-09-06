import re
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments" / "120_pll_dsp" / "rtl" / "top.v"
TB = ROOT / "experiments" / "120_pll_dsp" / "sim" / "tb.cpp"
EXPECTED = ROOT / "experiments" / "120_pll_dsp" / "expected.md"
PROBE = ROOT / "experiments" / "120_pll_dsp" / "hardware" / "probe.sh"
MODEL = ROOT / "experiments" / "090_pll_clock" / "sim" / "pll_model.v"
HPS = ROOT / "experiments" / "020_linux_mailbox" / "sim" / "hps_gp_model.v"


class PllDspSourcePolicyTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for path in (RTL, TB, EXPECTED, PROBE, MODEL, HPS):
            self.assertTrue(path.is_file(), path)

    def test_rtl_exposes_clock_only_ports_pll_and_dsp_product(self) -> None:
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
        self.assertEqual(len(re.findall(r"\baltera_pll\b", rtl)), 1)
        self.assertIn("16'hDC10", rtl)
        self.assertRegex(
            rtl,
            r'\(\*\s*multstyle\s*=\s*"dsp"\s*\*\)\s*wire\s+\[15:0\]\s+product',
        )
        self.assertRegex(rtl, r"product\s*=\s*left_sync\s*\*\s*right_sync")
        self.assertIn(".outclk(clk25)", rtl)
        self.assertIn(".rst(1'b0)", rtl)
        for forbidden in ("LED", "M10K", "MLAB", "LUTRAM", "HDMI", "SDRAM"):
            self.assertNotRegex(
                rtl,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_production_source_satisfies_closed_policy(self) -> None:
        policy = policy_for("120_pll_dsp")
        relative = "experiments/120_pll_dsp/rtl/top.v"
        policy.validate_source_text(relative, RTL.read_text(encoding="utf-8"))
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                relative,
                RTL.read_text(encoding="utf-8") + "\nwire LED;\n",
            )
