import re
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments" / "100_dsp_rom" / "rtl" / "top.v"
TB = ROOT / "experiments" / "100_dsp_rom" / "sim" / "tb.cpp"
EXPECTED = ROOT / "experiments" / "100_dsp_rom" / "expected.md"
ORACLE_QSF = ROOT / "experiments" / "100_dsp_rom" / "oracle" / "top.qsf"
ORACLE_QPF = ROOT / "experiments" / "100_dsp_rom" / "oracle" / "top.qpf"
MODEL = ROOT / "experiments" / "020_linux_mailbox" / "sim" / "hps_gp_model.v"


class DspRomSourcePolicyTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for path in (RTL, TB, EXPECTED, ORACLE_QSF, ORACLE_QPF, MODEL):
            self.assertTrue(path.is_file(), path)

    def test_rtl_exposes_clock_only_ports_product_and_initialized_table(self) -> None:
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
        self.assertIn("16'hD910", rtl)
        self.assertRegex(
            rtl,
            r'\(\*\s*multstyle\s*=\s*"dsp"\s*\*\)\s*wire\s+\[15:0\]\s+wide_product',
        )
        self.assertRegex(
            rtl,
            r'\(\*\s*ramstyle\s*=\s*"M10K"\s*\*\)\s*reg\s+\[7:0\]\s+stored',
        )
        self.assertRegex(rtl, r"index\[7:0\]\s*\^\s*PATTERN")
        self.assertNotRegex(rtl, r"\bwe\b")
        for forbidden in ("LED", "PLL", "LUTRAM", "HDMI", "MLAB"):
            self.assertNotRegex(
                rtl,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_production_source_satisfies_closed_policy(self) -> None:
        policy = policy_for("100_dsp_rom")
        relative = "experiments/100_dsp_rom/rtl/top.v"
        policy.validate_source_text(relative, RTL.read_text(encoding="utf-8"))
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                relative,
                RTL.read_text(encoding="utf-8") + "\nwire LED;\n",
            )

    def test_oracle_project_omits_simulation_model_and_led(self) -> None:
        qsf = ORACLE_QSF.read_text(encoding="utf-8")
        self.assertIn("experiments/100_dsp_rom/rtl/top.v", qsf)
        self.assertNotIn("LED", qsf)
        self.assertNotIn("hps_gp_model.v", qsf)
        self.assertNotIn("/sim/", qsf)
