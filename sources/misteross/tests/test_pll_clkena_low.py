import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllClkenaLowTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("370_pll_clkena_low")
        self.assertEqual(dict(policy.additional_clocks_mhz), {"gated_clock": 25.0})
        rtl = (ROOT / "experiments/370_pll_clkena_low/rtl/top.v").read_text()
        policy.validate_source_text("experiments/370_pll_clkena_low/rtl/top.v", rtl)
        self.assertIn('.ena_register_power_up("low")', rtl)
        self.assertIn('.ena_register_mode("falling edge")', rtl)
        self.assertIn("16'hD728", rtl)
        policy.validate_resources(
            {
                "altera_pll": {"used": 1},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
                "MISTRAL_CLKENA": {"used": 2},
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

    def test_gated_clock_is_required(self):
        policy = policy_for("370_pll_clkena_low")
        ref = {"constraint": 50, "achieved": 200}
        gated = {"constraint": 25, "achieved": 300}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            path.write_text(json.dumps({"fmax": {"meter.refclk": ref, "gated_clock": gated}}))
            self.assertEqual(_timing(path, 50, policy.clock, policy), ("meter.refclk", 200))
            path.write_text(json.dumps({"fmax": {"meter.refclk": ref}}))
            with self.assertRaises(SummaryError):
                _timing(path, 50, policy.clock, policy)
