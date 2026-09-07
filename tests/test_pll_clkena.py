import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllClkenaTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("360_pll_clkena")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(dict(policy.additional_clocks_mhz), {"gated_clock": 25.0})
        self.assertEqual(
            dict(policy.required_synth_cells),
            {"altera_pll": 1, "cyclonev_clkena": 1},
        )
        rtl = (ROOT / "experiments/360_pll_clkena/rtl/top.v").read_text()
        policy.validate_source_text("experiments/360_pll_clkena/rtl/top.v", rtl)
        self.assertIn("cyclonev_clkena", rtl)
        self.assertIn('.ena_register_mode("falling edge")', rtl)
        self.assertIn('.ena_register_power_up("high")', rtl)
        self.assertIn('.disable_mode("low")', rtl)
        self.assertIn(".ena(gpo[3])", rtl)
        self.assertIn(".inclk(pll_clock)", rtl)
        self.assertIn(".outclk(gated_clock)", rtl)
        self.assertIn("16'hD727", rtl)
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
        policy = policy_for("360_pll_clkena")
        ref = {"constraint": 50, "achieved": 200}
        gated = {"constraint": 25, "achieved": 300}
        cases = [
            ({"meter.refclk": ref, "gated_clock": gated}, True),
            ({"meter.refclk": ref}, False),
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
