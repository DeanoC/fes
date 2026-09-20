import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_narrow_init_word, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/870_m10k_narrow/rtl/top.v"
PROBE = ROOT / "experiments/870_m10k_narrow/hardware/probe.sh"


class M10kNarrowTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_narrow_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/870_m10k_narrow" / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_instantiates_8192x1_tdp(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD870", rtl)
        self.assertIn("MISTRAL_M10K_TDP", rtl)
        self.assertIn(".CFG_ABITS(13)", rtl)
        self.assertIn(".CFG_DBITS(1)", rtl)
        self.assertNotIn("CFG_RDW_MODE", rtl)
        policy = policy_for("870_m10k_narrow")
        policy.validate_source_text("experiments/870_m10k_narrow/rtl/top.v", rtl)
        self.assertEqual(policy.m10k_tdp_narrow_width, 1)

    def test_probe_checks_one_bit_payload(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("55408", probe)
        self.assertIn("status & 1", probe)
        self.assertIn("(bit << 13)", probe)
        self.assertIn("0x13579bdf", probe)

    def test_synth_json_requires_narrow_geometry(self) -> None:
        policy = policy_for("870_m10k_narrow")
        init = 0
        for address in range(8192):
            init |= m10k_narrow_init_word(address, 1) << address
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K_TDP",
                            "parameters": {
                                "CFG_ABITS": f"{13:032b}",
                                "CFG_DBITS": f"{1:032b}",
                                "INIT": f"{init:08192b}",
                            },
                            "port_directions": {
                                "CLK1": "input",
                                "CLK2": "input",
                                "A1EN": "input",
                                "A1WE": "input",
                            },
                            "connections": {
                                "CLK1": [8],
                                "CLK2": [8],
                                "A1EN": [9],
                                "A1WE": [10],
                                "A1DATA": [11],
                            },
                        }
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.validate_synth_json(path)
            design["modules"]["top"]["cells"]["mem"]["parameters"]["CFG_DBITS"] = f"{20:032b}"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_DBITS"):
                policy.validate_synth_json(path)
