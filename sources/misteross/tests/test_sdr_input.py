from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class SdrInputTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("640_sdr_input")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/640_sdr_input/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )
        self.assertEqual(dict(policy.required_synth_cells), {})
        self.assertEqual(dict(policy.additional_clocks_mhz), {})
        rtl = (ROOT / "experiments/640_sdr_input/rtl/top.v").read_text()
        policy.validate_source_text("experiments/640_sdr_input/rtl/top.v", rtl)
        self.assertIn("input wire SDR_IN", rtl)
        self.assertIn("captured <= SDR_IN", rtl)
        self.assertIn("16'h5E01", rtl)
        qsf = (ROOT / "experiments/640_sdr_input/pins.qsf").read_text()
        self.assertIn("FAST_INPUT_REGISTER ON", qsf)
        self.assertIn("PIN_Y15 -to SDR_IN", qsf)
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
