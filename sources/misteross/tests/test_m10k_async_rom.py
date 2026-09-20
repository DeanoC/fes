import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_init_word, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/880_m10k_async_rom/rtl/top.v"
PROBE = ROOT / "experiments/880_m10k_async_rom/hardware/probe.sh"


class M10kAsyncRomTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in (
            "rtl/top.v",
            "sim/tb.cpp",
            "sim/m10k_async_rom_model.v",
            "expected.md",
            "hardware/probe.sh",
        ):
            path = ROOT / "experiments/880_m10k_async_rom" / relative
            self.assertTrue(path.is_file(), path)

    def test_rtl_is_folded_1024x10_rom(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD880", rtl)
        self.assertIn(".CFG_ABITS(10)", rtl)
        self.assertIn(".CFG_DBITS(10)", rtl)
        self.assertIn(".CLK1(1'b0)", rtl)
        self.assertIn(".A1EN(1'b1)", rtl)
        self.assertNotIn("CFG_BYTE_ENABLE", rtl)
        self.assertNotIn("A1BE", rtl)
        policy = policy_for("880_m10k_async_rom")
        policy.validate_source_text("experiments/880_m10k_async_rom/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_async_readonly)
        self.assertFalse(policy.m10k_async_read)

    def test_probe_reads_ten_bit_payload(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("55424", probe)
        self.assertIn("status & 1023", probe)
        self.assertIn("1023", probe)

    def test_synth_json_requires_folded_clock(self) -> None:
        policy = policy_for("880_m10k_async_rom")
        init = 0
        for address in range(1024):
            init |= m10k_init_word(address, 10) << (address * 10)
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K",
                            "parameters": {
                                "CFG_ABITS": f"{10:032b}",
                                "CFG_DBITS": f"{10:032b}",
                                "CFG_ASYNC_READ": f"{1:032b}",
                                "INIT": f"{init:010240b}",
                            },
                            "connections": {
                                "CLK1": ["0"],
                                "A1EN": ["1"],
                                "B1ADDR": list(range(10)),
                                "B1DATA": list(range(10, 20)),
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
            policy.validate_synth_json(path)
            design["modules"]["top"]["cells"]["mem"]["connections"]["CLK1"] = [8]
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "folded"):
                policy.validate_synth_json(path)

        routed = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K",
                            "connections": {"CLK1": [8]},
                            "attributes": {"NEXTPNR_BEL": "MISTRAL_M10K.26.1.0"},
                        }
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "routed.json"
            path.write_text(json.dumps(routed), encoding="utf-8")
            policy.validate_routed_json(path)
            routed["modules"]["top"]["cells"]["mem"]["connections"]["CLK1"] = ["0"]
            path.write_text(json.dumps(routed), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "borrowed"):
                policy.validate_routed_json(path)
