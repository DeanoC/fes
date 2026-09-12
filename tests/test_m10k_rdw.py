import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/840_m10k_rdw/rtl/top.v"
PROBE = ROOT / "experiments/840_m10k_rdw/hardware/probe.sh"


class M10kRdwTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_rdw_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/840_m10k_rdw" / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_omits_rdw_parameters(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD840", rtl)
        self.assertIn("MISTRAL_M10K_TDP", rtl)
        self.assertNotIn(".CFG_RDW_MODE", rtl)
        self.assertNotIn("ADDRSTALL", rtl)
        policy = policy_for("840_m10k_rdw")
        policy.validate_source_text("experiments/840_m10k_rdw/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_rdw_new_data)

    def test_probe_writes_through_a_port(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("55360", probe)
        self.assertIn("0xC0000000", probe)
        self.assertIn("0x40000000", probe)

    def test_apply_synth_json_sets_rdw_contracts(self) -> None:
        policy = policy_for("840_m10k_rdw")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K_TDP",
                            "parameters": {
                                "CFG_ABITS": f"{9:032b}",
                                "CFG_DBITS": f"{20:032b}",
                            },
                            "port_directions": {
                                "CLK1": "input",
                                "CLK2": "input",
                                "A1EN": "input",
                                "B1EN": "input",
                                "A1WE": "input",
                                "B1WE": "input",
                            },
                            "connections": {
                                "CLK1": [1],
                                "CLK2": [1],
                                "A1EN": [2],
                                "B1EN": ["0"],
                                "A1WE": [3],
                                "B1WE": ["0"],
                                "A1Q": list(range(10, 30)),
                            },
                        }
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.apply_synth_json(path)
            cell = json.loads(path.read_text(encoding="utf-8"))["modules"]["top"]["cells"]["mem"]
            self.assertEqual(cell["parameters"]["CFG_RDW_MODE_A"], "NEW_DATA_NO_NBE_READ")
            self.assertEqual(cell["parameters"]["CFG_RDW_MODE_B"], "NEW_DATA_NO_NBE_READ")
            self.assertEqual(cell["parameters"]["CFG_RDW_MODE_MIXED"], "DONT_CARE")
            policy.validate_synth_json(path)
            cell["parameters"]["CFG_RDW_MODE_A"] = "OLD_DATA"
            path.write_text(json.dumps({"modules": {"top": {"cells": {"mem": cell}}}}), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_RDW_MODE_A"):
                policy.validate_synth_json(path)
