from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class DdrInputTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("650_ddr_input")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/650_ddr_input/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altddio_in": 1})
        self.assertEqual(dict(policy.additional_clocks_mhz), {})
        rtl = (ROOT / "experiments/650_ddr_input/rtl/top.v").read_text()
        policy.validate_source_text("experiments/650_ddr_input/rtl/top.v", rtl)
        self.assertIn("altddio_in", rtl)
        self.assertIn(".datain(DDR_IN)", rtl)
        self.assertIn(".dataout_h(high)", rtl)
        self.assertIn(".dataout_l(low)", rtl)
        self.assertIn("16'hDD02", rtl)
        qsf = (ROOT / "experiments/650_ddr_input/pins.qsf").read_text()
        self.assertIn("PIN_Y15 -to DDR_IN", qsf)
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
