import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/860_m10k_selectors/rtl/top.v"
PROBE = ROOT / "experiments/860_m10k_selectors/hardware/probe.sh"


class M10kSelectorTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_selector_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/860_m10k_selectors" / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_has_two_dual_clock_srams(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD860", rtl)
        self.assertEqual(rtl.count("MISTRAL_M10K"), 2)
        self.assertIn(".CFG_DUAL_CLOCK(1)", rtl)
        self.assertNotIn("altera_pll", rtl)
        policy = policy_for("860_m10k_selectors")
        policy.validate_source_text("experiments/860_m10k_selectors/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_selector_pair)
        self.assertEqual(dict(policy.required_packed_sites), {"MISTRAL_M10K": 2})

    def test_probe_selects_banks(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("55392", probe)
        self.assertIn("0x40000000", probe)
        self.assertIn("(bank << 9)", probe)
        self.assertIn("(written << 10)", probe)

    def test_synth_and_routed_json_require_two_live_clocks(self) -> None:
        policy = policy_for("860_m10k_selectors")

        def cell(init: int) -> dict:
            return {
                "type": "MISTRAL_M10K",
                "parameters": {
                    "CFG_ABITS": f"{9:032b}",
                    "CFG_DBITS": f"{20:032b}",
                    "CFG_DUAL_CLOCK": f"{1:032b}",
                    "CFG_OUT_REG_B": f"{1:032b}",
                    "INIT": f"{init:010240b}",
                },
                "port_directions": {
                    "CLK1": "input",
                    "CLK2": "input",
                    "A1EN": "input",
                    "B1EN": "input",
                },
                "connections": {
                    "CLK1": [1],
                    "CLK2": [1],
                    "A1EN": [2],
                    "B1EN": [3],
                    "B1ADDR": [4],
                    "B1DATA": [5],
                },
                "attributes": {},
            }

        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem0": cell(0xA6),
                        "mem1": cell(0xB7),
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.validate_synth_json(path)
            design["modules"]["top"]["cells"]["mem1"]["connections"].pop("CLK2")
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CLK2"):
                policy.validate_synth_json(path)

        routed = {
            "modules": {
                "top": {
                    "cells": {
                        "mem0": {
                            "type": "MISTRAL_M10K",
                            "connections": {"CLK2": [1]},
                            "attributes": {"NEXTPNR_BEL": "MISTRAL_M10K.26.1.0"},
                        },
                        "mem1": {
                            "type": "MISTRAL_M10K",
                            "connections": {"CLK2": [1]},
                            "attributes": {"NEXTPNR_BEL": "MISTRAL_M10K.26.2.0"},
                        },
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "routed.json"
            path.write_text(json.dumps(routed), encoding="utf-8")
            policy.validate_routed_json(path)
            routed["modules"]["top"]["cells"]["mem1"]["attributes"][
                "NEXTPNR_BEL"
            ] = "MISTRAL_M10K.26.1.0"
            path.write_text(json.dumps(routed), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "unique"):
                policy.validate_routed_json(path)
