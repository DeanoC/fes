from __future__ import annotations

import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import (
    PolicyError,
    m10k_tdp_byte_physical_init,
    policy_for,
)
from scripts.oss_summary import SummaryError, build_summary


ROOT = Path(__file__).resolve().parents[1]


def _packed_init(width: int) -> int:
    packed = 0
    for address in range(512):
        packed |= m10k_tdp_byte_physical_init(address, width) << (address * 20)
    return packed


def _cell(width: int, *, byte_enable: int = 1, clk1=None, clk2=None, init=None, be_a=None, be_b=None):
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    if be_a is None:
        be_a = [7, 8]
    if be_b is None:
        be_b = [9, 10]
    if init is None:
        init = _packed_init(width)
    return {
        "type": "MISTRAL_M10K_TDP",
        "parameters": {
            "CFG_ABITS": f"{9:032b}",
            "CFG_DBITS": f"{20:032b}",
            "CFG_BYTE_ENABLE": f"{byte_enable:032b}",
            "INIT": f"{init:b}",
        },
        "connections": {
            "CLK1": clk1,
            "CLK2": clk2,
            "A1EN": [3],
            "B1EN": [4],
            "A1WE": [5],
            "B1WE": [6],
            "A1BE": be_a,
            "B1BE": be_b,
            "A1DATA": list(range(20, 40)),
            "B1DATA": list(range(40, 60)),
        },
        "port_directions": {
            "CLK1": "input",
            "CLK2": "input",
            "A1EN": "input",
            "B1EN": "input",
            "A1WE": "input",
            "B1WE": "input",
            "A1BE": "input",
            "B1BE": "input",
        },
    }


class M10kTdpByteLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for experiment in ("550_m10k_tdp_be20", "560_m10k_tdp_be16"):
            for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
                path = ROOT / "experiments" / experiment / relative
                self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signatures(self) -> None:
        cases = (
            ("550_m10k_tdp_be20", "16'hD41F", 20, "20'h00A6"),
            ("560_m10k_tdp_be16", "16'hD420", 16, "16'h00A6"),
        )
        for experiment, signature, width, init in cases:
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
            self.assertIn(f"[{width - 1}:0] mem [0:511]", rtl)
            self.assertIn('ram_style = "m10k_tdp_byte"', rtl)
            self.assertIn("32'h13579BDF", rtl)
            self.assertIn("read_clock", rtl)
            self.assertIn(init, rtl)
            policy = policy_for(experiment)
            policy.validate_source_text(f"experiments/{experiment}/rtl/top.v", rtl)

    def test_init_formula(self) -> None:
        self.assertEqual(m10k_tdp_byte_physical_init(0, 20), 0xA6)
        self.assertEqual(m10k_tdp_byte_physical_init(0, 16), 0xA6)
        logical = ((1 * 73) ^ (1 >> 1) ^ 0xA6) & 0xFFFF
        self.assertEqual(
            m10k_tdp_byte_physical_init(1, 16),
            (logical & 0xFF) | (((logical >> 8) & 0xFF) << 10),
        )

    def test_policy_requires_byte_masks_and_init(self) -> None:
        for experiment, width in (("550_m10k_tdp_be20", 20), ("560_m10k_tdp_be16", 16)):
            policy = policy_for(experiment)
            self.assertEqual(policy.m10k_tdp_byte_width, width)
            self.assertEqual(policy.m10k_tdp_width, 0)
            self.assertEqual(policy.nextpnr_router, "")
            self.assertTrue(policy.require_read_clock_arc)
            self.assertFalse(policy.nobram)
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

    def test_policy_rejects_unmasked_shared_clock_and_tied_masks(self) -> None:
        policy = policy_for("550_m10k_tdp_be20")
        unmasked = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(20, byte_enable=0),
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
                        "mem.0.0.0": _cell(20, clk1=[4], clk2=[4]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        tied = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(20, be_a=[7, 7]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(unmasked), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_BYTE_ENABLE"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(shared), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "independent"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(tied), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "A1BE"):
                policy.validate_synth_json(path)

    def test_timing_requires_read_clock_arc(self) -> None:
        policy = policy_for("550_m10k_tdp_be20")
        policy.validate_timing_report(
            {"critical_paths": [{"from": "posedge read_clock", "max_delay": 40}]}
        )
        with self.assertRaisesRegex(PolicyError, "read_clock"):
            policy.validate_timing_report({"critical_paths": []})

    def test_summary_rejects_missing_read_clock_arc(self) -> None:
        policy = policy_for("550_m10k_tdp_be20")
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
                    experiment="550_m10k_tdp_be20",
                )

    def test_probe_covers_masks_hold_and_enables(self) -> None:
        cases = (
            ("550_m10k_tdp_be20", "54303"),
            ("560_m10k_tdp_be16", "54304"),
        )
        for experiment, signature in cases:
            probe = (ROOT / "experiments" / experiment / "hardware/probe.sh").read_text(
                encoding="utf-8"
            )
            self.assertIn(signature, probe)
            self.assertIn("initialized reads through both ports", probe)
            self.assertIn("low/high/zero/full masks", probe)
            self.assertIn("inferred output hold", probe)
            self.assertIn("simultaneous disjoint writes", probe)
            self.assertIn("clock enable holds output", probe)
            self.assertIn("0x13579bdf", probe)


if __name__ == "__main__":
    unittest.main()
