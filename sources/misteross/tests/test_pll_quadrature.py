import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllQuadratureTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("280_pll_quadrature")
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        self.assertEqual(
            dict(policy.additional_clocks_mhz),
            {
                "clocks[0]": 25.0,
                "clocks[1]": 25.0,
                "clocks[2]": 25.0,
                "clocks[3]": 25.0,
            },
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altera_pll": 1})
        rtl = (ROOT / "experiments/280_pll_quadrature/rtl/top.v").read_text()
        policy.validate_source_text("experiments/280_pll_quadrature/rtl/top.v", rtl)
        self.assertIn('.number_of_clocks(4)', rtl)
        self.assertIn('.output_clock_frequency0("25.0 MHz")', rtl)
        self.assertIn('.output_clock_frequency3("25.0 MHz")', rtl)
        self.assertIn('.phase_shift0("0 ps")', rtl)
        self.assertIn('.phase_shift1("10000 ps")', rtl)
        self.assertIn('.phase_shift2("20000 ps")', rtl)
        self.assertIn('.phase_shift3("30000 ps")', rtl)
        self.assertIn("16'hD71F", rtl)
        self.assertIn("alive0", rtl)
        self.assertIn("alive3", rtl)
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

    def test_all_output_clocks_are_required(self):
        policy = policy_for("280_pll_quadrature")
        ref = {"constraint": 50, "achieved": 200}
        clk25 = {"constraint": 25, "achieved": 300}
        full = {
            "meter.refclk": ref,
            "clocks[0]": clk25,
            "clocks[1]": clk25,
            "clocks[2]": clk25,
            "clocks[3]": clk25,
        }
        cases = [
            (full, True),
            (
                {
                    "meter.refclk": ref,
                    "clocks[0]": clk25,
                    "clocks[1]": clk25,
                    "clocks[2]": clk25,
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
                            ("meter.refclk", 200),
                        )
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
