import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/770_m10k_async_read/rtl/top.v"
PROBE = ROOT / "experiments/770_m10k_async_read/hardware/probe.sh"


class M10kAsyncReadTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_async_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/770_m10k_async_read" / relative
            self.assertTrue(path.is_file(), path)

    def test_primitive_omits_read_enable_and_second_clock(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD42B", rtl)
        self.assertIn(".B1EN(1'b1)", rtl)
        self.assertNotIn(".CLK2", rtl)
        self.assertNotIn(".CFG_ASYNC_READ", rtl)
        self.assertNotIn("altera_pll", rtl)
        policy = policy_for("770_m10k_async_read")
        policy.validate_source_text("experiments/770_m10k_async_read/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_async_read)
        self.assertFalse(policy.require_read_clock_arc)

    def test_probe_reads_without_enable_or_read_clock(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54315", probe)
        self.assertIn("0x80000000", probe)
        self.assertNotIn("0x40000000", probe)
        self.assertNotIn("0x60000000", probe)

    def test_apply_synth_json_preserves_constant_b1en_and_drops_legacy_clk2(self) -> None:
        policy = policy_for("770_m10k_async_read")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K",
                            "parameters": {
                                "CFG_ABITS": f"{9:032b}",
                                "CFG_DBITS": f"{20:032b}",
                                "CFG_DUAL_CLOCK": f"{1:032b}",
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
                                "CLK1": [100],
                                "CLK2": [101],
                                "A1EN": [3],
                                "A1BE": [4, 5],
                                "B1EN": ["1"],
                                "B1ADDR": list(range(40, 49)),
                                "B1DATA": list(range(10, 30)),
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
            self.assertEqual(cell["parameters"]["CFG_ASYNC_READ"][-1], "1")
            self.assertEqual(int(cell["parameters"]["CFG_DUAL_CLOCK"], 2), 0)
            self.assertEqual(cell["connections"]["B1EN"], ["1"])
            self.assertNotIn("CLK2", cell["connections"])
            policy.validate_synth_json(path)
            broken = json.loads(path.read_text(encoding="utf-8"))
            broken["modules"]["top"]["cells"]["mem"]["connections"]["B1EN"] = [6]
            path.write_text(json.dumps(broken), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "B1EN"):
                policy.validate_synth_json(path)
