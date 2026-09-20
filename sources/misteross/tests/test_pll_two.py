import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllTwoTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("350_pll_two")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(
            dict(policy.additional_clocks_mhz),
            {"integer_clock": 25.0, "fractional_clock": 12.288},
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altera_pll": 2})
        rtl = (ROOT / "experiments/350_pll_two/rtl/top.v").read_text()
        policy.validate_source_text("experiments/350_pll_two/rtl/top.v", rtl)
        self.assertIn("integer_pll", rtl)
        self.assertIn("fractional_pll", rtl)
        self.assertNotIn("audio", rtl.lower())
        self.assertIn('.output_clock_frequency0("25.0 MHz")', rtl)
        self.assertIn('.output_clock_frequency0("12.288 MHz")', rtl)
        self.assertIn('.fractional_vco_multiplier("true")', rtl)
        self.assertIn("16'hD726", rtl)
        policy.validate_resources(
            {
                "altera_pll": {"used": 2},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
            }
        )
        with self.assertRaises(PolicyError):
            policy.validate_resources(
                {
                    "altera_pll": {"used": 1},
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
                }
            )

    def test_both_pll_clocks_are_required(self):
        policy = policy_for("350_pll_two")
        ref = {"constraint": 50, "achieved": 188.359}
        clk25 = {"constraint": 25, "achieved": 300}
        clkfrac = {"constraint": 12.28803158, "achieved": 225.276}
        cases = [
            (
                {
                    "meter.refclk": ref,
                    "integer_clock": clk25,
                    "fractional_clock": clkfrac,
                },
                True,
            ),
            ({"meter.refclk": ref, "integer_clock": clk25}, False),
            ({"meter.refclk": ref, "fractional_clock": clkfrac}, False),
        ]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            for clocks, passing in cases:
                with self.subTest(clocks=clocks):
                    path.write_text(json.dumps({"fmax": clocks}))
                    if passing:
                        self.assertEqual(
                            _timing(path, 50, policy.clock, policy),
                            ("meter.refclk", 188.359),
                        )
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
