import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_init_word, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/720_m10k_aclr_infer/rtl/top.v"
PROBE = ROOT / "experiments/720_m10k_aclr_infer/hardware/probe.sh"


def _packed_init() -> str:
    packed = 0
    for address in range(256):
        packed |= m10k_init_word(address, 20) << (address * 20)
    return f"{packed:b}"


def _cell(*, aclr1=None):
    return {
        "type": "MISTRAL_M10K",
        "parameters": {
            "CFG_ABITS": f"{9:032b}",
            "CFG_DBITS": f"{20:032b}",
            "CFG_DUAL_CLOCK": f"{1:032b}",
            "INIT": _packed_init(),
        },
        "connections": {
            "CLK1": [1],
            "CLK2": [2],
            "A1EN": [3],
            "ACLR1": aclr1 if aclr1 is not None else [205],
        },
        "port_directions": {
            "CLK1": "input",
            "CLK2": "input",
            "A1EN": "input",
            "ACLR1": "input",
        },
    }


class M10kAclrInferTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/720_m10k_aclr_infer" / relative
            self.assertTrue(path.is_file(), path)

    def test_infers_async_clear_without_json_attach(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD428", rtl)
        self.assertIn('ramstyle = "M10K"', rtl)
        self.assertIn("posedge gp_out[5]", rtl)
        self.assertNotIn("MISTRAL_M10K", rtl)
        policy = policy_for("720_m10k_aclr_infer")
        policy.validate_source_text("experiments/720_m10k_aclr_infer/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_require_aclr1)
        self.assertIsNone(policy.m10k_aclr1_gpo_bit)

    def test_policy_accepts_yosys_inferred_aclr1(self) -> None:
        policy = policy_for("720_m10k_aclr_infer")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": _cell(),
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

    def test_policy_rejects_constant_aclr1(self) -> None:
        policy = policy_for("720_m10k_aclr_infer")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem": _cell(aclr1=["0"]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "ACLR1"):
                policy.validate_synth_json(path)

    def test_probe_covers_clear_and_restore(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54312", probe)
        self.assertIn("ACLR1 clears the sampled output register", probe)
        self.assertIn("restores INIT from memory", probe)
        self.assertIn("0x13579bdf", probe)
