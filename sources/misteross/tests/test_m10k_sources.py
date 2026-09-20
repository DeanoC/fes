import re
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments" / "030_m10k_rom" / "rtl" / "top.v"
TB = ROOT / "experiments" / "030_m10k_rom" / "sim" / "tb.cpp"
EXPECTED = ROOT / "experiments" / "030_m10k_rom" / "expected.md"
ORACLE_QSF = ROOT / "experiments" / "030_m10k_rom" / "oracle" / "top.qsf"
ORACLE_QPF = ROOT / "experiments" / "030_m10k_rom" / "oracle" / "top.qpf"


class M10kSourcePolicyTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for path in (RTL, TB, EXPECTED, ORACLE_QSF, ORACLE_QPF):
            self.assertTrue(path.is_file(), path)

    def test_rtl_exposes_clock_led_and_parameterized_table(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertRegex(rtl, r"module\s+top\s*#\s*\(")
        self.assertRegex(rtl, r"parameter\s+integer\s+ADDR_BITS\s*=\s*8")
        self.assertRegex(rtl, r"input\s+wire\s+FPGA_CLK1_50")
        self.assertRegex(rtl, r"output\s+wire\s+\[0:0\]\s+LED")
        self.assertRegex(rtl, r"reg\s+\[WIDTH-1:0\]\s+stored\s+\[0:DEPTH-1\]")
        self.assertRegex(rtl, r"always\s*@\s*\(posedge\s+FPGA_CLK1_50\)")
        self.assertRegex(rtl, r"LED\s*\[0\]")
        for forbidden in (
            "PLL",
            "BRAM",
            "LUTRAM",
            "HPS",
            "DSP",
            "ALTPLL",
            "altsyncram",
            "M10K",
            "cyclonev",
            "MLAB",
        ):
            self.assertNotRegex(
                rtl,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_production_source_satisfies_closed_policy(self) -> None:
        policy = policy_for("030_m10k_rom")
        relative = "experiments/030_m10k_rom/rtl/top.v"
        policy.validate_source_text(relative, RTL.read_text(encoding="utf-8"))
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                relative,
                RTL.read_text(encoding="utf-8") + "\naltsyncram extra();\n",
            )

    def test_oracle_project_binds_the_production_rtl_and_led(self) -> None:
        qsf = ORACLE_QSF.read_text(encoding="utf-8")
        self.assertIn("experiments/030_m10k_rom/rtl/top.v", qsf)
        self.assertIn("PIN_W15", qsf)
        self.assertIn("LED[0]", qsf)
        self.assertNotIn("hps_gp_model.v", qsf)
        self.assertNotIn("/sim/", qsf)
