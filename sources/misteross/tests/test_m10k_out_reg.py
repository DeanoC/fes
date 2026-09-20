import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/800_m10k_out_reg/rtl/top.v"
PROBE = ROOT / "experiments/800_m10k_out_reg/hardware/probe.sh"


class M10kOutRegTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_out_reg_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/800_m10k_out_reg" / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_omits_output_register_parameter(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD42D", rtl)
        self.assertIn(".CLK2(FPGA_CLK1_50)", rtl)
        self.assertIn(".B1EN(1'b1)", rtl)
        self.assertNotIn(".CFG_OUT_REG", rtl)
        self.assertNotIn("altera_pll", rtl)
        policy = policy_for("800_m10k_out_reg")
        policy.validate_source_text("experiments/800_m10k_out_reg/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_out_reg_b)
        self.assertFalse(policy.m10k_async_read)

    def test_probe_samples_early_then_late(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54317", probe)
        self.assertIn("0x20000000", probe)
        self.assertIn('extra=${3:-0x20000000}', probe)

    def test_apply_synth_json_sets_cfg_out_reg_b(self) -> None:
        policy = policy_for("800_m10k_out_reg")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K",
                            "parameters": {
                                "CFG_ABITS": f"{9:032b}",
                                "CFG_DBITS": f"{20:032b}",
                            },
                            "port_directions": {
                                "CLK1": "input",
                                "CLK2": "input",
                                "A1EN": "input",
                                "A1BE": "input",
                                "B1EN": "input",
                                "B1ADDR": "input",
                                "B1DATA": "output",
                            },
                            "connections": {
                                "CLK1": [1],
                                "CLK2": [1],
                                "A1EN": [2],
                                "A1BE": [3, 4],
                                "B1EN": ["1"],
                                "B1ADDR": list(range(40, 49)),
                                "B1DATA": list(range(10, 30)),
                                "ACLR0": ["0"],
                                "ACLR1": ["0"],
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
            self.assertEqual(cell["parameters"]["CFG_OUT_REG_B"][-1], "1")
            policy.validate_synth_json(path)
            cell["parameters"].pop("CFG_OUT_REG_B")
            path.write_text(json.dumps({"modules": {"top": {"cells": {"mem": cell}}}}), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_OUT_REG_B"):
                policy.validate_synth_json(path)
