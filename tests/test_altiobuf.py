from pathlib import Path
import unittest

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]


class AltiobufTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("670_altiobuf")
        self.assertEqual(
            policy.constraints,
            (
                "experiments/670_altiobuf/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )
        self.assertEqual(
            dict(policy.required_synth_cells),
            {"altiobuf_in": 1, "altiobuf_out": 1, "altiobuf_bidir": 1},
        )
        self.assertEqual(dict(policy.additional_clocks_mhz), {})
        rtl = (ROOT / "experiments/670_altiobuf/rtl/top.v").read_text()
        policy.validate_source_text("experiments/670_altiobuf/rtl/top.v", rtl)
        self.assertIn("altiobuf_in", rtl)
        self.assertIn("altiobuf_out", rtl)
        self.assertIn("altiobuf_bidir", rtl)
        self.assertIn(".datain(ALTI_IN)", rtl)
        self.assertIn(".dataout(ALTI_OUT)", rtl)
        self.assertIn(".dataio(ALTI_BIDIR)", rtl)
        self.assertIn("16'hAB01", rtl)
        qsf = (ROOT / "experiments/670_altiobuf/pins.qsf").read_text()
        self.assertIn("PIN_Y15 -to ALTI_IN", qsf)
        self.assertIn("PIN_W15 -to ALTI_OUT", qsf)
        self.assertIn("PIN_V16 -to ALTI_BIDIR", qsf)
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
