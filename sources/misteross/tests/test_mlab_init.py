import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import (
    PolicyError,
    mlab_init_byte,
    mlab_init_lane,
    policy_for,
)


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/470_mlab_init/rtl/top.v"
PROBE = ROOT / "experiments/470_mlab_init/hardware/probe.sh"


class MlabInitLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/470_mlab_init" / relative
            self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signature(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^\s*module\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
            rtl,
        )
        self.assertIsNotNone(match)
        self.assertEqual(re.sub(r"\s+", " ", match.group(1).strip()), "input wire FPGA_CLK1_50")
        self.assertEqual(len(re.findall(r"\bcyclonev_hps_interface_mpu_general_purpose\b", rtl)), 1)
        self.assertIn("16'hD417", rtl)
        self.assertIn('ramstyle = "mlab"', rtl)
        self.assertNotIn("`ifdef VERILATOR", rtl)
        self.assertIn("8'hA6", rtl)
        self.assertIn("stored[i]", rtl)

    def test_init_formula_matches_address_zero(self) -> None:
        self.assertEqual(mlab_init_byte(0), 0xA6)
        self.assertEqual(mlab_init_lane(0) & 1, 0)
        self.assertEqual((mlab_init_lane(1) >> 0) & 1, 1)
        self.assertEqual((mlab_init_lane(2) >> 0) & 1, 1)
        self.assertEqual((mlab_init_lane(5) >> 0) & 1, 1)
        self.assertEqual((mlab_init_lane(7) >> 0) & 1, 1)

    def test_policy_requires_yosys_lane_init(self) -> None:
        policy = policy_for("470_mlab_init")
        self.assertTrue(policy.synth_json_mlab_init)
        self.assertFalse(policy.nolutram)
        self.assertTrue(policy.nodsp)
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_MLAB": 8})
        design = {
            "modules": {
                "top": {
                    "cells": {
                        f"storage.stored.{bit}.0.0": {
                            "type": "MISTRAL_MLAB",
                            "parameters": {"INIT": f"{mlab_init_lane(bit):032b}"},
                        }
                        for bit in range(8)
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.apply_synth_json(path)
            policy.validate_synth_json(path)
            cells = json.loads(path.read_text(encoding="utf-8"))["modules"]["top"]["cells"]
            for bit in range(8):
                self.assertEqual(
                    cells[f"storage.stored.{bit}.0.0"]["parameters"]["INIT"],
                    f"{mlab_init_lane(bit):032b}",
                )

    def test_policy_does_not_inject_init(self) -> None:
        policy = policy_for("470_mlab_init")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        f"storage.stored.{bit}.0.0": {"type": "MISTRAL_MLAB", "parameters": {}}
                        for bit in range(8)
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            policy.apply_synth_json(path)
            cells = json.loads(path.read_text(encoding="utf-8"))["modules"]["top"]["cells"]
            for bit in range(8):
                self.assertEqual(cells[f"storage.stored.{bit}.0.0"].get("parameters", {}), {})

    def test_policy_rejects_missing_init(self) -> None:
        policy = policy_for("470_mlab_init")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        f"storage.stored.{bit}.0.0": {"type": "MISTRAL_MLAB", "parameters": {}}
                        for bit in range(8)
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "INIT"):
                policy.validate_synth_json(path)

    def test_probe_covers_init_and_preserve(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("D417", probe)
        self.assertIn("all 32 initialized bytes", probe)
        self.assertIn("preserve unwritten bytes", probe)
        self.assertNotIn(">> 16", probe)


if __name__ == "__main__":
    unittest.main()
