import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
EXP = ROOT / "experiments/850_hps_location"
RTL = EXP / "rtl/top.v"
QSF = EXP / "pins.qsf"
PROBE = EXP / "hardware/probe.sh"
BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0"
CELL = "cyclonev_hps_interface_peripheral_i2c"


class HpsLocationTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "pins.qsf",
            "sim/hps_i2c_model.v",
            "sim/tb.cpp",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = EXP / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_omits_bel_and_keeps_qsf_target(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD850", rtl)
        self.assertIn(CELL, rtl)
        self.assertIn("hdmi_i2c", rtl)
        self.assertNotIn("(* BEL", rtl)
        self.assertNotIn("52.60.0", rtl)
        policy = policy_for("850_hps_location")
        policy.validate_source_text("experiments/850_hps_location/rtl/top.v", rtl)
        self.assertEqual(
            dict(policy.required_nextpnr_bels),
            {CELL: BEL},
        )

    def test_qsf_assigns_hps_location_to_hdmi_i2c(self) -> None:
        qsf = QSF.read_text(encoding="utf-8")
        self.assertIn("HPS_LOCATION", qsf)
        self.assertIn("HPSINTERFACEPERIPHERALI2C_X52_Y60_N111", qsf)
        self.assertIn("-entity top", qsf)
        self.assertIn("-to hdmi_i2c", qsf)
        policy = policy_for("850_hps_location")
        policy.validate_source_text("experiments/850_hps_location/pins.qsf", qsf)
        self.assertEqual(
            policy.constraints,
            (
                "experiments/850_hps_location/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
        )

    def test_probe_checks_signature_payload(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("55376", probe)
        self.assertIn("166", probe)

    def test_routed_json_requires_exact_i2c_bel(self) -> None:
        policy = policy_for("850_hps_location")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "hdmi_i2c": {
                            "type": CELL,
                            "attributes": {"NEXTPNR_BEL": BEL},
                        }
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "routed.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.validate_routed_json(path)
            design["modules"]["top"]["cells"]["hdmi_i2c"]["attributes"][
                "NEXTPNR_BEL"
            ] = "cyclonev_hps_interface_peripheral_i2c.52.58.0"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "52.60.0"):
                policy.validate_routed_json(path)
