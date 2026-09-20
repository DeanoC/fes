from __future__ import annotations

import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import (
    PolicyError,
    m10k_tdp_abits,
    m10k_tdp_mixed_lane,
    m10k_tdp_mixed_physical_dbits,
    m10k_tdp_mixed_unit,
    policy_for,
)
from scripts.oss_summary import SummaryError, build_summary


ROOT = Path(__file__).resolve().parents[1]
CASES = (
    ("570_m10k_tdp_mix20_10", "16'hD421", 20, 10, "54305"),
    ("580_m10k_tdp_mix10_20", "16'hD422", 10, 20, "54306"),
    ("590_m10k_tdp_mix16_8", "16'hD423", 16, 8, "54307"),
    ("600_m10k_tdp_mix8_16", "16'hD424", 8, 16, "54308"),
)


def _packed_init(unit: int) -> int:
    packed = 0
    for address in range(1024):
        packed |= m10k_tdp_mixed_lane(address, unit) << (address * 10)
    return packed


def _cell(logical_a: int, logical_b: int, *, mixed=1, clk1=None, clk2=None):
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    physical_a = m10k_tdp_mixed_physical_dbits(logical_a)
    physical_b = m10k_tdp_mixed_physical_dbits(logical_b)
    unit = m10k_tdp_mixed_unit(logical_a, logical_b)
    return {
        "type": "MISTRAL_M10K_TDP",
        "parameters": {
            "CFG_ABITS": f"{m10k_tdp_abits(physical_a):032b}",
            "CFG_DBITS": f"{physical_a:032b}",
            "CFG_RD_ABITS": f"{m10k_tdp_abits(physical_b):032b}",
            "CFG_RD_DBITS": f"{physical_b:032b}",
            "CFG_MIXED_WIDTH": f"{mixed:032b}",
            "INIT": f"{_packed_init(unit):b}",
        },
        "connections": {
            "CLK1": clk1,
            "CLK2": clk2,
            "A1EN": [3],
            "B1EN": [4],
            "A1WE": [5],
            "B1WE": [6],
            "A1DATA": list(range(20, 20 + physical_a)),
            "B1DATA": list(range(40, 40 + physical_b)),
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


class M10kTdpMixedLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for experiment, *_ in CASES:
            for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
                path = ROOT / "experiments" / experiment / relative
                self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signatures(self) -> None:
        for experiment, signature, logical_a, logical_b, _decimal in CASES:
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
            self.assertIn("[UNIT-1:0] mem [0:1023]", rtl)
            self.assertIn('ram_style = "m10k_tdp_mixed"', rtl)
            self.assertIn("32'h13579BDF", rtl)
            self.assertIn("read_clock", rtl)
            policy = policy_for(experiment)
            policy.validate_source_text(f"experiments/{experiment}/rtl/top.v", rtl)
            self.assertEqual(policy.m10k_tdp_mixed_a, logical_a)
            self.assertEqual(policy.m10k_tdp_mixed_b, logical_b)

    def test_init_formula(self) -> None:
        self.assertEqual(m10k_tdp_mixed_lane(0, 10), 0xA6)
        self.assertEqual(m10k_tdp_mixed_lane(0, 8), 0xA6)
        self.assertEqual(m10k_tdp_mixed_physical_dbits(20), 20)
        self.assertEqual(m10k_tdp_mixed_physical_dbits(16), 20)
        self.assertEqual(m10k_tdp_mixed_physical_dbits(10), 10)
        self.assertEqual(m10k_tdp_mixed_physical_dbits(8), 10)
        self.assertEqual(m10k_tdp_mixed_unit(20, 10), 10)
        self.assertEqual(m10k_tdp_mixed_unit(16, 8), 8)

    def test_policy_requires_mixed_geometry_and_init(self) -> None:
        for experiment, _signature, logical_a, logical_b, _decimal in CASES:
            policy = policy_for(experiment)
            self.assertEqual(policy.nextpnr_router, "")
            self.assertTrue(policy.require_read_clock_arc)
            self.assertFalse(policy.nobram)
            design = {
                "modules": {
                    "top": {
                        "cells": {
                            "mem.0.0.0": _cell(logical_a, logical_b),
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

    def test_policy_rejects_equal_width_flag_and_shared_clock(self) -> None:
        policy = policy_for("570_m10k_tdp_mix20_10")
        unmixed = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(20, 10, mixed=0),
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
                        "mem.0.0.0": _cell(20, 10, clk1=[4], clk2=[4]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(unmixed), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_MIXED_WIDTH"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(shared), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "independent"):
                policy.validate_synth_json(path)

    def test_timing_requires_read_clock_arc(self) -> None:
        policy = policy_for("570_m10k_tdp_mix20_10")
        policy.validate_timing_report(
            {"critical_paths": [{"from": "posedge read_clock", "max_delay": 40}]}
        )
        with self.assertRaisesRegex(PolicyError, "read_clock"):
            policy.validate_timing_report({"critical_paths": []})

    def test_summary_rejects_missing_read_clock_arc(self) -> None:
        policy = policy_for("570_m10k_tdp_mix20_10")
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
                    experiment="570_m10k_tdp_mix20_10",
                )

    def test_probe_covers_init_neighbors_and_enables(self) -> None:
        for experiment, _signature, _a, _b, decimal in CASES:
            probe = (ROOT / "experiments" / experiment / "hardware/probe.sh").read_text(
                encoding="utf-8"
            )
            self.assertIn(decimal, probe)
            self.assertIn("initialized words through both port widths", probe)
            self.assertIn("NEW_DATA, cross-width readback and preserved neighbors", probe)
            self.assertIn("simultaneous disjoint writes", probe)
            self.assertIn("clock enable holds output", probe)
            self.assertIn("0x13579bdf", probe)


if __name__ == "__main__":
    unittest.main()
