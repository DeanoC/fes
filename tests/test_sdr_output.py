from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class SdrOutputTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("630_sdr_output")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/630_sdr_output/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )
        self.assertEqual(dict(policy.required_synth_cells), {})
        self.assertEqual(dict(policy.additional_clocks_mhz), {})
        rtl = (ROOT / "experiments/630_sdr_output/rtl/top.v").read_text()
        policy.validate_source_text("experiments/630_sdr_output/rtl/top.v", rtl)
        self.assertIn("output reg SDR_OUT", rtl)
        self.assertIn("SDR_OUT <= beat[7]", rtl)
        self.assertIn("16'h5D01", rtl)
        qsf = (ROOT / "experiments/630_sdr_output/pins.qsf").read_text()
        self.assertIn("FAST_OUTPUT_REGISTER ON", qsf)
        self.assertIn("PIN_W15 -to SDR_OUT", qsf)
        policy.validate_resources(
            {
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
            }
        )
        with self.assertRaises(PolicyError):
            policy.validate_resources(
                {
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
                    "altera_pll": {"used": 1},
                }
            )
