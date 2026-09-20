import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_init_word, policy_for


ROOT = Path(__file__).resolve().parents[1]


class SlotM10kTests(unittest.TestCase):
    def test_combined_sources_lock_the_reserved_bel(self) -> None:
        rtl = (ROOT / "experiments/890_slot_m10k/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("16'hD890", rtl)
        self.assertIn('BEL = "MISTRAL_M10K.26.1.0"', rtl)
        self.assertIn("FES_SLOT = 1", rtl)
        policy = policy_for("890_slot_m10k")
        policy.validate_source_text("experiments/890_slot_m10k/rtl/top.v", rtl)
        self.assertEqual(dict(policy.required_nextpnr_bels), {"MISTRAL_M10K": "MISTRAL_M10K.26.1.0"})
        self.assertTrue(policy.m10k_async_readonly)
        qsf = (ROOT / "experiments/890_slot_m10k/pins.qsf").read_text(encoding="utf-8")
        self.assertIn("FES_RESERVED_BEL", qsf)
        self.assertIn("FES_RESERVED_RECT", qsf)
        mapping = (ROOT / "experiments/890_slot_m10k/link.toml").read_text(encoding="utf-8")
        self.assertIn("x0 = 2096", mapping)
        self.assertIn("y0 = 80", mapping)
        self.assertIn("y1 = 780", mapping)
        self.assertIn("MISTRAL_M10K.26.1.0", mapping)

    def test_base_has_no_m10k_cell(self) -> None:
        rtl = (ROOT / "experiments/891_slot_m10k_base/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("16'hD891", rtl)
        self.assertNotIn("MISTRAL_M10K", rtl)
        policy = policy_for("891_slot_m10k_base")
        policy.validate_source_text("experiments/891_slot_m10k_base/rtl/top.v", rtl)
        self.assertEqual(dict(policy.required_synth_cells), {})

    def test_cart_locks_the_same_bel(self) -> None:
        rtl = (ROOT / "experiments/892_slot_m10k_cart/rtl/top.v").read_text(encoding="utf-8")
        self.assertIn("16'hD892", rtl)
        self.assertIn('BEL = "MISTRAL_M10K.26.1.0"', rtl)
        policy = policy_for("892_slot_m10k_cart")
        policy.validate_source_text("experiments/892_slot_m10k_cart/rtl/top.v", rtl)
        self.assertEqual(dict(policy.required_nextpnr_bels), {"MISTRAL_M10K": "MISTRAL_M10K.26.1.0"})

    def test_combined_synth_json_accepts_folded_clock(self) -> None:
        policy = policy_for("890_slot_m10k")
        init = 0
        for address in range(1024):
            init |= m10k_init_word(address, 10) << (address * 10)
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "slot_mem": {
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
        routed = {
            "modules": {
                "top": {
                    "cells": {
                        "slot_mem": {
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
            routed["modules"]["top"]["cells"]["slot_mem"]["attributes"][
                "NEXTPNR_BEL"
            ] = "MISTRAL_M10K.26.2.0"
            path.write_text(json.dumps(routed), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "BEL"):
                policy.validate_routed_json(path)
