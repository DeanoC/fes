import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_init_word, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/700_m10k_aclr/rtl/top.v"
PROBE = ROOT / "experiments/700_m10k_aclr/hardware/probe.sh"


def _packed_init() -> str:
    packed = 0
    for address in range(256):
        packed |= m10k_init_word(address, 20) << (address * 20)
    return f"{packed:b}"


def _cell(*, aclr1=None, clk1=None, clk2=None):
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    cell = {
        "type": "MISTRAL_M10K",
        "parameters": {
            "CFG_ABITS": f"{9:032b}",
            "CFG_DBITS": f"{20:032b}",
            "CFG_DUAL_CLOCK": f"{1:032b}",
            "INIT": _packed_init(),
        },
        "connections": {
            "CLK1": clk1,
            "CLK2": clk2,
            "A1EN": [3],
            "B1EN": [4],
        },
        "port_directions": {
            "CLK1": "input",
            "CLK2": "input",
            "A1EN": "input",
            "B1EN": "input",
        },
    }
    if aclr1 is not None:
        cell["connections"]["ACLR1"] = aclr1
        cell["port_directions"]["ACLR1"] = "input"
    return cell


class M10kAclrTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/700_m10k_aclr" / relative
            self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signature(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD426", rtl)
        self.assertIn('ramstyle = "M10K"', rtl)
        self.assertIn("32'h13579BDF", rtl)
        self.assertIn("gp_out[5]", rtl)
        self.assertIn("{gp_out[8:6], 2'b00, gp_out[3:0]}", rtl)
        policy = policy_for("700_m10k_aclr")
        policy.validate_source_text("experiments/700_m10k_aclr/rtl/top.v", rtl)
        self.assertEqual(policy.m10k_aclr1_gpo_bit, 5)
        self.assertEqual(m10k_init_word(0, 20), 0xA6)
        self.assertEqual(m10k_init_word(7, 20), ((7 * 73) ^ (7 >> 1) ^ 0xA6) & 0xFFFFF)

    def test_apply_synth_json_attaches_fabric_aclr1(self) -> None:
        policy = policy_for("700_m10k_aclr")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(),
                        "hps": {
                            "type": "cyclonev_hps_interface_mpu_general_purpose",
                            "connections": {"gp_out": list(range(200, 232))},
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
            policy.apply_synth_json(path)
            policy.validate_synth_json(path)
            attached = json.loads(path.read_text(encoding="utf-8"))
            mem = attached["modules"]["top"]["cells"]["mem.0.0.0"]
            self.assertEqual(mem["connections"]["ACLR1"], [205])
            self.assertEqual(mem["port_directions"]["ACLR1"], "input")

    def test_policy_rejects_constant_aclr1(self) -> None:
        policy = policy_for("700_m10k_aclr")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(aclr1=["0"]),
                        "hps": {
                            "type": "cyclonev_hps_interface_mpu_general_purpose",
                            "connections": {"gp_out": list(range(200, 232))},
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
            with self.assertRaisesRegex(PolicyError, "ACLR1"):
                policy.validate_synth_json(path)

    def test_probe_covers_clear_and_restore(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54310", probe)
        self.assertIn("ACLR1 clears the sampled output register", probe)
        self.assertIn("restores INIT from memory", probe)
        self.assertIn("restores the written word from memory", probe)
        self.assertIn("0x13579bdf", probe)
