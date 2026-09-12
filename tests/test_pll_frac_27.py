import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/830_pll_frac_27/rtl/top.v"
PROBE = ROOT / "experiments/830_pll_frac_27/hardware/probe.sh"


class PllFrac27Tests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "sim/pll_model.v", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/830_pll_frac_27" / relative
            self.assertTrue(path.is_file(), path)

    def test_closed_resources_and_27_mhz_request(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD827", rtl)
        self.assertIn('output_clock_frequency0("27.0 MHz")', rtl)
        self.assertIn('fractional_vco_multiplier("true")', rtl)
        policy = policy_for("830_pll_frac_27")
        policy.validate_source_text("experiments/830_pll_frac_27/rtl/top.v", rtl)
        self.assertEqual(dict(policy.additional_clocks_mhz), {"clk27": 27.0})
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        resources = {
            "altera_pll": {"used": 1},
            "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
        }
        policy.validate_resources(resources)
        with self.assertRaises(PolicyError):
            policy.validate_resources({**resources, "MISTRAL_MUL18X19": {"used": 1}})

    def test_probe_accepts_27_mhz_window(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("D827", probe)
        self.assertIn("2210", probe)
        self.assertIn("2213", probe)

    def test_both_clock_domains_required(self) -> None:
        policy = policy_for("830_pll_frac_27")
        ref = {"constraint": 50, "achieved": 200}
        generated = {"constraint": 27, "achieved": 300}
        cases = [
            ({"meter.refclk": ref, "clk27": generated}, True),
            ({"meter.refclk": ref}, False),
            ({"clk27": generated}, False),
            ({"meter.refclk": ref, "clk27": {"constraint": 50, "achieved": 300}}, False),
        ]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            for clocks, passing in cases:
                with self.subTest(clocks=clocks):
                    path.write_text(json.dumps({"fmax": clocks}))
                    if passing:
                        self.assertEqual(_timing(path, 50, policy.clock, policy), ("meter.refclk", 200))
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
