import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_mixed_lane, policy_for


ROOT = Path(__file__).resolve().parents[1]


def _packed_init() -> int:
    packed = 0
    for address in range(1024):
        packed |= m10k_mixed_lane(address) << (address * 10)
    return packed


class M10kMixedByteEnableLadderTests(unittest.TestCase):
    def test_closed_resources_and_sources(self) -> None:
        policy = policy_for("680_m10k_mix20be10")
        self.assertEqual(policy.m10k_mixed_write_dbits, 20)
        self.assertEqual(policy.m10k_mixed_read_dbits, 10)
        self.assertTrue(policy.m10k_byte_enable)
        self.assertEqual(policy.nextpnr_router, "router1")
        self.assertFalse(policy.nobram)
        rtl = (ROOT / "experiments/680_m10k_mix20be10/rtl/top.v").read_text()
        policy.validate_source_text("experiments/680_m10k_mix20be10/rtl/top.v", rtl)
        self.assertIn("MISTRAL_M10K", rtl)
        self.assertIn("CFG_BYTE_ENABLE(1)", rtl)
        self.assertIn("CFG_MIXED_WIDTH(1)", rtl)
        self.assertIn("16'hD425", rtl)
        self.assertIn(".A1BE(wbe)", rtl)

    def test_policy_requires_mixed_byte_enable_geometry(self) -> None:
        policy = policy_for("680_m10k_mix20be10")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K",
                            "parameters": {
                                "CFG_ABITS": f"{9:032b}",
                                "CFG_DBITS": f"{20:032b}",
                                "CFG_RD_ABITS": f"{10:032b}",
                                "CFG_RD_DBITS": f"{10:032b}",
                                "CFG_DUAL_CLOCK": f"{1:032b}",
                                "CFG_MIXED_WIDTH": f"{1:032b}",
                                "CFG_BYTE_ENABLE": f"{1:032b}",
                                "INIT": f"{_packed_init():b}",
                            },
                            "connections": {
                                "CLK1": [1],
                                "CLK2": [2],
                                "A1EN": [3],
                                "B1EN": [4],
                                "A1BE": [5, 6],
                                "A1DATA": list(range(10, 30)),
                                "B1DATA": list(range(40, 50)),
                            },
                        },
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.validate_synth_json(path)

    def test_policy_rejects_missing_byte_enable(self) -> None:
        policy = policy_for("680_m10k_mix20be10")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": {
                            "type": "MISTRAL_M10K",
                            "parameters": {
                                "CFG_ABITS": f"{9:032b}",
                                "CFG_DBITS": f"{20:032b}",
                                "CFG_RD_ABITS": f"{10:032b}",
                                "CFG_RD_DBITS": f"{10:032b}",
                                "CFG_DUAL_CLOCK": f"{1:032b}",
                                "CFG_MIXED_WIDTH": f"{1:032b}",
                                "INIT": f"{_packed_init():b}",
                            },
                            "connections": {
                                "CLK1": [1],
                                "CLK2": [2],
                                "A1EN": [3],
                                "B1EN": [4],
                                "A1DATA": list(range(10, 30)),
                                "B1DATA": list(range(40, 50)),
                            },
                        },
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_BYTE_ENABLE"):
                policy.validate_synth_json(path)
