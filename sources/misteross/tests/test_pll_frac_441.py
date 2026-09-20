import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllFrac441Tests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("190_pll_frac_441")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(dict(policy.additional_clocks_mhz), {"fractional_clock": 11.2896})
        self.assertEqual(dict(policy.required_synth_cells), {"altera_pll": 1})
        rtl = (ROOT / "experiments/190_pll_frac_441/rtl/top.v").read_text()
        policy.validate_source_text("experiments/190_pll_frac_441/rtl/top.v", rtl)
        self.assertIn('.output_clock_frequency0("11.2896 MHz")', rtl)
        self.assertIn('.fractional_vco_multiplier("true")', rtl)
        self.assertIn("16'hD716", rtl)
        self.assertIn(".outclk(fractional_clock)", rtl)
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

    def test_picosecond_constraint_quantization_is_accepted(self):
        policy = policy_for("190_pll_frac_441")
        ref = {"constraint": 50, "achieved": 195.274}
        generated = {"constraint": 11.28963079, "achieved": 340.716}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            path.write_text(
                json.dumps(
                    {
                        "fmax": {
                            "meter.refclk": ref,
                            "fractional_clock": generated,
                        }
                    }
                )
            )
            self.assertEqual(
                _timing(path, 50, policy.clock, policy),
                ("meter.refclk", 195.274),
            )
            path.write_text(json.dumps({"fmax": {"meter.refclk": ref}}))
            with self.assertRaises(SummaryError):
                _timing(path, 50, policy.clock, policy)
            path.write_text(
                json.dumps(
                    {
                        "fmax": {
                            "meter.refclk": ref,
                            "fractional_clock": {"constraint": 11.0, "achieved": 340.716},
                        }
                    }
                )
            )
            with self.assertRaises(SummaryError):
                _timing(path, 50, policy.clock, policy)
