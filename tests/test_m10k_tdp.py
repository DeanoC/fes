from __future__ import annotations

import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import (
    PolicyError,
    m10k_init_word,
    m10k_tdp_abits,
    policy_for,
)
from scripts.oss_summary import SummaryError, build_summary


ROOT = Path(__file__).resolve().parents[1]


def _packed_init(width: int) -> int:
    packed = 0
    depth = 1 << m10k_tdp_abits(width)
    for address in range(depth):
        packed |= m10k_init_word(address, width) << (address * width)
    return packed


def _cell(width: int, *, mixed: int | None = None, clk1=None, clk2=None, init=None, abits=None):
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    if abits is None:
        abits = m10k_tdp_abits(width)
    if init is None:
        init = _packed_init(width)
    parameters = {
        "CFG_ABITS": f"{abits:032b}",
        "CFG_DBITS": f"{width:032b}",
        "INIT": f"{init:b}",
    }
    if mixed is not None:
        parameters["CFG_MIXED_WIDTH"] = f"{mixed:032b}"
    return {
        "type": "MISTRAL_M10K_TDP",
        "parameters": parameters,
        "connections": {
            "CLK1": clk1,
            "CLK2": clk2,
            "A1EN": [3],
            "B1EN": [4],
            "A1WE": [5],
            "B1WE": [6],
            "A1DATA": list(range(10, 10 + width)),
            "B1DATA": list(range(100, 100 + width)),
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


class M10kTdpLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for experiment in ("530_m10k_tdp10", "540_m10k_tdp20"):
            for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
                path = ROOT / "experiments" / experiment / relative
                self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signatures(self) -> None:
        cases = (
            ("530_m10k_tdp10", "16'hD41D", 10, 1024),
            ("540_m10k_tdp20", "16'hD41E", 20, 512),
        )
        for experiment, signature, width, depth in cases:
            rtl = (ROOT / "experiments" / experiment / "rtl/top.v").read_text(encoding="utf-8")
            match = re.search(
                r"(?ms)^\s*module\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
                rtl,
            )
            self.assertIsNotNone(match)
            self.assertEqual(re.sub(r"\s+", " ", match.group(1).strip()), "input wire FPGA_CLK1_50")
            self.assertEqual(len(re.findall(r"\bcyclonev_hps_interface_mpu_general_purpose\b", rtl)), 1)
            self.assertEqual(len(re.findall(r"\baltera_pll\b", rtl)), 1)
            self.assertEqual(len(re.findall(r"\bcyclonev_clkena\b", rtl)), 1)
            self.assertIn(signature, rtl)
            self.assertIn(f"[{width - 1}:0] mem [0:{depth - 1}]", rtl)
            self.assertIn('ram_style = "m10k_tdp"', rtl)
            self.assertIn("32'h13579BDF", rtl)
            self.assertIn("read_clock", rtl)
            self.assertIn("10'h0A6" if width == 10 else "20'h00A6", rtl)
            policy = policy_for(experiment)
            policy.validate_source_text(f"experiments/{experiment}/rtl/top.v", rtl)

    def test_init_formula(self) -> None:
        self.assertEqual(m10k_init_word(0, 10), 0xA6)
        self.assertEqual(m10k_init_word(0, 20), 0xA6)
        self.assertEqual(m10k_init_word(1, 10), ((1 * 73) ^ (1 >> 1) ^ 0xA6) & 0x3FF)
        self.assertEqual(m10k_tdp_abits(10), 10)
        self.assertEqual(m10k_tdp_abits(20), 9)

    def test_policy_requires_independent_clocks_and_init(self) -> None:
        for experiment, width in (("530_m10k_tdp10", 10), ("540_m10k_tdp20", 20)):
            policy = policy_for(experiment)
            self.assertEqual(policy.m10k_tdp_width, width)
            self.assertEqual(policy.nextpnr_router, "")
            self.assertTrue(policy.require_read_clock_arc)
            self.assertFalse(policy.nobram)
            self.assertTrue(policy.nolutram)
            self.assertTrue(policy.nodsp)
            self.assertEqual(
                dict(policy.required_synth_cells),
                {"MISTRAL_M10K_TDP": 1, "altera_pll": 1, "cyclonev_clkena": 1},
            )
            self.assertEqual(
                dict(policy.synth_json_input_ports),
                {"MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")},
            )
            design = {
                "modules": {
                    "top": {
                        "cells": {
                            "mem.0.0.0": _cell(width),
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

    def test_policy_rejects_mixed_width_shared_clock_and_missing_init(self) -> None:
        policy = policy_for("530_m10k_tdp10")
        mixed = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(10, mixed=1),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        shared = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(10, clk1=[4], clk2=[4]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        missing_init = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(10, init=0),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(mixed), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_MIXED_WIDTH"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(shared), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "independent"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(missing_init), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "INIT"):
                policy.validate_synth_json(path)

    def test_timing_requires_read_clock_arc(self) -> None:
        policy = policy_for("530_m10k_tdp10")
        policy.validate_timing_report(
            {"critical_paths": [{"from": "posedge read_clock", "max_delay": 40}]}
        )
        with self.assertRaisesRegex(PolicyError, "read_clock"):
            policy.validate_timing_report({"critical_paths": []})

    def test_summary_rejects_missing_read_clock_arc(self) -> None:
        policy = policy_for("530_m10k_tdp10")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            timing = root / "timing.json"
            route = root / "route.log"
            rbf = root / "top.rbf"
            rbf.write_bytes(b"rbf")
            route.write_text("complete\n", encoding="utf-8")
            timing.write_text(
                json.dumps(
                    {
                        "fmax": {
                            policy.clock_evidence_names[1]: {"constraint": 50, "achieved": 400}
                        },
                        "utilization": {
                            "MISTRAL_COMB": {"used": 1, "available": 10},
                            "altera_pll": {"used": 1, "available": 6},
                            "MISTRAL_M10K": {"used": 1, "available": 553},
                            "cyclonev_hps_interface_mpu_general_purpose": {
                                "used": 1,
                                "available": 1,
                            },
                        },
                        "critical_paths": [],
                    }
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(SummaryError, "read_clock"):
                build_summary(
                    timing,
                    route,
                    rbf,
                    requested_mhz=50,
                    clock_prefix="FPGA_CLK1_50",
                    experiment="530_m10k_tdp10",
                )

    def test_probe_covers_init_writers_and_enables(self) -> None:
        cases = (
            ("530_m10k_tdp10", "54301"),
            ("540_m10k_tdp20", "54302"),
        )
        for experiment, signature in cases:
            probe = (ROOT / "experiments" / experiment / "hardware/probe.sh").read_text(
                encoding="utf-8"
            )
            self.assertIn(signature, probe)
            self.assertIn("initialized reads through both ports", probe)
            self.assertIn("own-port new data", probe)
            self.assertIn("simultaneous disjoint writes", probe)
            self.assertIn("clock enable holds output", probe)
            self.assertIn("0x13579bdf", probe)


if __name__ == "__main__":
    unittest.main()
