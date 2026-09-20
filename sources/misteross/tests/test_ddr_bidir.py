from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class DdrBidirTests(unittest.TestCase):
    def test_closed_resources_and_sources(self) -> None:
        policy = policy_for("690_ddr_bidir")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/690_ddr_bidir/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )
        self.assertEqual(dict(policy.required_synth_cells), {"altddio_bidir": 1})
        self.assertEqual(dict(policy.additional_clocks_mhz), {})
        rtl = (ROOT / "experiments/690_ddr_bidir/rtl/top.v").read_text()
        policy.validate_source_text("experiments/690_ddr_bidir/rtl/top.v", rtl)
        self.assertIn("altddio_bidir", rtl)
        self.assertIn(".datain_h(high)", rtl)
        self.assertIn(".datain_l(low)", rtl)
        self.assertIn(".dataout_h(dataout_h)", rtl)
        self.assertIn(".dataout_l(dataout_l)", rtl)
        self.assertIn(".combout(combout)", rtl)
        self.assertIn(".oe(1'b1)", rtl)
        self.assertNotIn(".datain_h(1'b1)", rtl)
        self.assertNotIn(".datain_l(1'b0)", rtl)
        self.assertIn("16'hDD04", rtl)
        self.assertIn(".padio(DDR_IO)", rtl)
        self.assertIn(".oe_out()", rtl)
        qsf = (ROOT / "experiments/690_ddr_bidir/pins.qsf").read_text()
        self.assertIn("PIN_W15 -to DDR_IO", qsf)
        self.assertIn("PIN_V11 -to FPGA_CLK1_50", qsf)
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
