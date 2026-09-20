import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllFracDualTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("200_pll_frac_dual")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(
            dict(policy.additional_clocks_mhz),
            {"clocks[0]": 12.288, "clocks[1]": 24.576},
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altera_pll": 1})
        rtl = (ROOT / "experiments/200_pll_frac_dual/rtl/top.v").read_text()
        policy.validate_source_text("experiments/200_pll_frac_dual/rtl/top.v", rtl)
        self.assertIn('.number_of_clocks(2)', rtl)
        self.assertIn('.output_clock_frequency0("12.288 MHz")', rtl)
        self.assertIn('.output_clock_frequency1("24.576 MHz")', rtl)
        self.assertIn('.fractional_vco_multiplier("true")', rtl)
        self.assertIn("16'hD717", rtl)
        policy.validate_resources(
            {
                "altera_pll": {"used": 1},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
            }
        )
        with self.assertRaises(PolicyError):
            policy.validate_resources(
                {
                    "altera_pll": {"used": 1},
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
                    "MISTRAL_MUL9X9": {"used": 1},
                }
            )

    def test_three_clock_domains_required(self):
        policy = policy_for("200_pll_frac_dual")
        ref = {"constraint": 50, "achieved": 188.359}
        clk0 = {"constraint": 12.28803158, "achieved": 225.276}
        clk1 = {"constraint": 24.57606291, "achieved": 371.471}
        cases = [
            ({"meter.refclk": ref, "clocks[0]": clk0, "clocks[1]": clk1}, True),
            ({"meter.refclk": ref, "clocks[0]": clk0}, False),
            ({"meter.refclk": ref, "clocks[1]": clk1}, False),
            (
                {
                    "meter.refclk": ref,
                    "clocks[0]": clk0,
                    "clocks[1]": {"constraint": 24.0, "achieved": 371.471},
                },
                False,
            ),
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
