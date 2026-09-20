import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, m10k_init_word, policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/730_m10k_tdp_tclk/rtl/top.v"
PROBE = ROOT / "experiments/730_m10k_tdp_tclk/hardware/probe.sh"


def _packed_init() -> str:
    packed = 0
    for address in range(256):
        packed |= m10k_init_word(address, 20) << (address * 20)
    return f"{packed:b}"


def _cell(*, clk2=None, b1en=None):
    return {
        "type": "MISTRAL_M10K_TDP",
        "parameters": {
            "CFG_ABITS": f"{9:032b}",
            "CFG_DBITS": f"{20:032b}",
            "INIT": _packed_init(),
        },
        "connections": {
            "CLK1": [1],
            "CLK2": clk2 if clk2 is not None else ["0"],
            "A1EN": [3],
            "B1EN": b1en if b1en is not None else ["0"],
            "A1WE": [4],
            "B1WE": ["0"],
            "A1DATA": list(range(10, 30)),
            "B1DATA": list(range(30, 50)),
        },
        "port_directions": {
            "CLK1": "input",
            "CLK2": "input",
            "A1EN": "input",
            "B1EN": "input",
            "A1WE": "input",
            "B1WE": "input",
        },
    }


class M10kTdpTclkTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "sim/m10k_tdp_tclk_model.v", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/730_m10k_tdp_tclk" / relative
            self.assertTrue(path.is_file(), path)

    def test_ties_unused_b_clock_low(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD429", rtl)
        self.assertIn("MISTRAL_M10K_TDP", rtl)
        self.assertIn(".CLK2(1'b0)", rtl)
        self.assertIn(".B1EN(1'b0)", rtl)
        policy = policy_for("730_m10k_tdp_tclk")
        policy.validate_source_text("experiments/730_m10k_tdp_tclk/rtl/top.v", rtl)
        self.assertTrue(policy.m10k_tdp_constant_clk2)
        self.assertEqual(policy.m10k_tdp_width, 20)

    def test_policy_accepts_constant_clk2(self) -> None:
        policy = policy_for("730_m10k_tdp_tclk")
        design = {"modules": {"top": {"cells": {"mem": _cell()}}}}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.validate_synth_json(path)

    def test_policy_rejects_live_clk2(self) -> None:
        policy = policy_for("730_m10k_tdp_tclk")
        design = {"modules": {"top": {"cells": {"mem": _cell(clk2=[2])}}}}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CLK2"):
                policy.validate_synth_json(path)

    def test_probe_covers_init_and_write(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54313", probe)
        self.assertIn("initialized 20-bit A-port words", probe)
        self.assertIn("B clock tied off", probe)
        self.assertIn("0x13579bdf", probe)
