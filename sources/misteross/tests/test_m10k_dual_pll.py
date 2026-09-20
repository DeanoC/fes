import unittest
from pathlib import Path

from scripts.experiment_policy import policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/740_m10k_dual_pll/rtl/top.v"
PROBE = ROOT / "experiments/740_m10k_dual_pll/hardware/probe.sh"


class M10kDualPllTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/740_m10k_dual_pll" / relative
            self.assertTrue(path.is_file(), path)

    def test_two_plls_and_m10k(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD42A", rtl)
        self.assertEqual(rtl.count("altera_pll"), 2)
        self.assertIn('ramstyle = "M10K"', rtl)
        self.assertIn("74.25 MHz", rtl)
        self.assertIn("25.0 MHz", rtl)
        policy = policy_for("740_m10k_dual_pll")
        policy.validate_source_text("experiments/740_m10k_dual_pll/rtl/top.v", rtl)
        self.assertEqual(dict(policy.required_synth_cells)["altera_pll"], 2)
        self.assertEqual(dict(policy.required_synth_cells)["MISTRAL_M10K"], 1)
        self.assertEqual(policy.nextpnr_router, "")

    def test_probe_covers_init_and_write(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54314", probe)
        self.assertIn("initialized 20-bit words", probe)
        self.assertIn("0x13579bdf", probe)
