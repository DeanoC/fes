import unittest
from pathlib import Path

from scripts.experiment_policy import policy_for

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/750_dsp18x19/rtl/top.v"
PROBE = ROOT / "experiments/750_dsp18x19/hardware/probe.sh"


class Dsp18x19Tests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "sim/dsp18x19.v", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/750_dsp18x19" / relative
            self.assertTrue(path.is_file(), path)

    def test_instantiates_dual_18x19_products(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD619", rtl)
        self.assertIn("dsp18x19", rtl)
        self.assertIn(".D(19'd3)", rtl)
        policy = policy_for("750_dsp18x19")
        policy.validate_source_text("experiments/750_dsp18x19/rtl/top.v", rtl)
        self.assertEqual(dict(policy.required_synth_cells)["MISTRAL_MUL18X19"], 1)
        self.assertIn("MISTRAL_MUL18X19", policy.yosys_post_synth)

    def test_probe_covers_both_products(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("D619", probe)
        self.assertIn("ten_times_twelve", probe)
        self.assertIn("seven_times_three", probe)

    def test_apply_synth_json_marks_y_as_output(self) -> None:
        import json
        import tempfile

        policy = policy_for("750_dsp18x19")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "product.mul": {
                            "type": "MISTRAL_MUL18X19",
                            "port_directions": {
                                "A": "input",
                                "B": "input",
                                "C": "input",
                                "D": "input",
                            },
                            "connections": {
                                "A": [1],
                                "B": [2],
                                "C": [3],
                                "D": [4],
                                "Y": [5],
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
            cell = json.loads(path.read_text(encoding="utf-8"))["modules"]["top"]["cells"][
                "product.mul"
            ]
            self.assertEqual(cell["port_directions"]["A"], "input")
            self.assertEqual(cell["port_directions"]["Y"], "output")
