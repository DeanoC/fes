import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllClkenaBranchTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("380_pll_clkena_branch")
        self.assertEqual(
            dict(policy.additional_clocks_mhz),
            {"meter.testclk": 25.0, "gated_clock": 25.0},
        )
        rtl = (ROOT / "experiments/380_pll_clkena_branch/rtl/top.v").read_text()
        policy.validate_source_text("experiments/380_pll_clkena_branch/rtl/top.v", rtl)
        self.assertIn(".inclk(pll_clock)", rtl)
        self.assertIn(".testclk(pll_clock)", rtl)
        self.assertIn(".testclk(gated_clock)", rtl)
        self.assertIn('.ena_register_power_up("low")', rtl)
        self.assertIn("16'hD729", rtl)
        policy.validate_resources(
            {
                "altera_pll": {"used": 1},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
                "MISTRAL_CLKENA": {"used": 3},
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

    def test_both_branch_clocks_are_required(self):
        policy = policy_for("380_pll_clkena_branch")
        ref = {"constraint": 50, "achieved": 200}
        clk25 = {"constraint": 25, "achieved": 300}
        cases = [
            ({"meter.refclk": ref, "meter.testclk": clk25, "gated_clock": clk25}, True),
            ({"meter.refclk": ref, "meter.testclk": clk25}, False),
            ({"meter.refclk": ref, "gated_clock": clk25}, False),
        ]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            for clocks, passing in cases:
                with self.subTest(clocks=clocks):
                    path.write_text(json.dumps({"fmax": clocks}))
                    if passing:
                        self.assertEqual(
                            _timing(path, 50, policy.clock, policy),
                            ("meter.refclk", 200),
                        )
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
