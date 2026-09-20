from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class DdrClockTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("620_ddr_clock")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/620_ddr_clock/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altddio_out": 1})
        self.assertEqual(dict(policy.additional_clocks_mhz), {})
        rtl = (ROOT / "experiments/620_ddr_clock/rtl/top.v").read_text()
        policy.validate_source_text("experiments/620_ddr_clock/rtl/top.v", rtl)
        self.assertIn("altddio_out", rtl)
        self.assertIn(".datain_h(1'b1)", rtl)
        self.assertIn(".datain_l(1'b0)", rtl)
        self.assertIn("16'hDD01", rtl)
        self.assertIn(".dataout(DDR_OUT)", rtl)
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
