import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/790_m10k_addrstall/rtl/top.v"
PROBE = ROOT / "experiments/790_m10k_addrstall/hardware/probe.sh"


class M10kAddrstallTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_addrstall_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/790_m10k_addrstall" / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_omits_addrstall_port(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD42C", rtl)
        self.assertIn("ADDRSTALLA(gp_out[29])", rtl)
        self.assertNotIn(".ADDRSTALLB", rtl)
        self.assertIn("`ifdef VERILATOR", rtl)
        policy = policy_for("790_m10k_addrstall")
        policy.validate_source_text("experiments/790_m10k_addrstall/rtl/top.v", rtl)
        self.assertEqual(policy.m10k_addrstalla_gpo_bit, 29)

    def test_probe_stalls_then_releases(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54316", probe)
        self.assertIn("0x20000000", probe)
        self.assertIn('extra=${3:-0x20000000}', probe)

    def test_apply_synth_json_attaches_addrstalla(self) -> None:
        policy = policy_for("790_m10k_addrstall")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "hps": {
                            "type": "cyclonev_hps_interface_mpu_general_purpose",
                            "connections": {"gp_out": list(range(100, 132))},
                            "port_directions": {"gp_out": "output"},
                        },
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
                        },
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.apply_synth_json(path)
            cell = json.loads(path.read_text(encoding="utf-8"))["modules"]["top"]["cells"]["mem"]
            self.assertEqual(cell["connections"]["ADDRSTALLA"], [129])
            self.assertEqual(cell["port_directions"]["ADDRSTALLA"], "input")
            policy.validate_synth_json(path)
            with self.assertRaisesRegex(PolicyError, "ADDRSTALLA"):
                cell["connections"].pop("ADDRSTALLA")
                path.write_text(json.dumps({"modules": {"top": {"cells": {"mem": cell}}}}), encoding="utf-8")
                policy.validate_synth_json(path)
