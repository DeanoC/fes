import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllDutyTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("210_pll_duty")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(dict(policy.additional_clocks_mhz), {"duty_clock": 25.0})
        self.assertEqual(dict(policy.required_synth_cells), {"altera_pll": 1})
        rtl = (ROOT / "experiments/210_pll_duty/rtl/top.v").read_text()
        policy.validate_source_text("experiments/210_pll_duty/rtl/top.v", rtl)
        self.assertIn('.output_clock_frequency0("25.0 MHz")', rtl)
        self.assertIn(".duty_cycle0(25)", rtl)
        self.assertIn('.fractional_vco_multiplier("false")', rtl)
        self.assertIn("16'hD718", rtl)
        self.assertIn("always @(posedge duty_clock)", rtl)
        self.assertIn("always @(negedge duty_clock)", rtl)
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

    def test_duty_clock_timing_is_required(self):
        policy = policy_for("210_pll_duty")
        ref = {"constraint": 50, "achieved": 195.274}
        generated = {"constraint": 25, "achieved": 243.902}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            path.write_text(
                json.dumps({"fmax": {"meter.refclk": ref, "duty_clock": generated}})
            )
            self.assertEqual(
                _timing(path, 50, policy.clock, policy),
                ("meter.refclk", 195.274),
            )
            path.write_text(json.dumps({"fmax": {"meter.refclk": ref}}))
            with self.assertRaises(SummaryError):
                _timing(path, 50, policy.clock, policy)
