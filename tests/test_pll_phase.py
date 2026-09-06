import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllPhaseTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("220_pll_phase")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(
            dict(policy.additional_clocks_mhz),
            {"meter.testclk": 25.0, "phase90": 25.0},
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altera_pll": 1})
        rtl = (ROOT / "experiments/220_pll_phase/rtl/top.v").read_text()
        policy.validate_source_text("experiments/220_pll_phase/rtl/top.v", rtl)
        self.assertIn('.number_of_clocks(2)', rtl)
        self.assertIn('.output_clock_frequency0("25.0 MHz")', rtl)
        self.assertIn('.output_clock_frequency1("25.0 MHz")', rtl)
        self.assertIn('.phase_shift1("10000 ps")', rtl)
        self.assertIn("16'hD719", rtl)
        self.assertIn("always @(posedge phase0) begin", rtl)
        self.assertIn("always @(posedge phase90) begin", rtl)
        self.assertIn("phase0_alive", rtl)
        self.assertIn("phase90_alive", rtl)
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

    def test_both_phase_clocks_are_required(self):
        policy = policy_for("220_pll_phase")
        ref = {"constraint": 50, "achieved": 200}
        clk0 = {"constraint": 25, "achieved": 240}
        clk90 = {"constraint": 25, "achieved": 240}
        cases = [
            ({"meter.refclk": ref, "meter.testclk": clk0, "phase90": clk90}, True),
            ({"meter.refclk": ref, "meter.testclk": clk0}, False),
            ({"meter.refclk": ref, "phase90": clk90}, False),
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
