import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import (
    PolicyError,
    m10k_mixed_abits,
    m10k_mixed_lane,
    policy_for,
)
from scripts.oss_summary import SummaryError, build_summary


ROOT = Path(__file__).resolve().parents[1]


def _packed_init() -> int:
    packed = 0
    for address in range(1024):
        packed |= m10k_mixed_lane(address) << (address * 10)
    return packed


def _cell(write_bits: int, read_bits: int, *, mixed: int = 1, dual: int = 1, clk1=None, clk2=None, init=None):
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    if init is None:
        init = _packed_init()
    return {
        "type": "MISTRAL_M10K",
        "parameters": {
            "CFG_ABITS": f"{m10k_mixed_abits(write_bits):032b}",
            "CFG_DBITS": f"{write_bits:032b}",
            "CFG_RD_ABITS": f"{m10k_mixed_abits(read_bits):032b}",
            "CFG_RD_DBITS": f"{read_bits:032b}",
            "CFG_DUAL_CLOCK": f"{dual:032b}",
            "CFG_MIXED_WIDTH": f"{mixed:032b}",
            "INIT": f"{init:b}",
        },
        "connections": {
            "CLK1": clk1,
            "CLK2": clk2,
            "A1EN": [3],
            "B1EN": [4],
            "A1DATA": list(range(10, 10 + write_bits)),
            "B1DATA": list(range(100, 100 + read_bits)),
        },
        "port_directions": {
            "CLK1": "input",
            "CLK2": "input",
            "A1EN": "input",
            "B1EN": "input",
        },
    }


class M10kMixedWidthLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for experiment in ("510_m10k_mix40r10", "520_m10k_mix10r40"):
            for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
                path = ROOT / "experiments" / experiment / relative
                self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signatures(self) -> None:
        cases = (
            ("510_m10k_mix40r10", "16'hD41B", 40, 10),
            ("520_m10k_mix10r40", "16'hD41C", 10, 40),
        )
        for experiment, signature, write_bits, read_bits in cases:
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
            self.assertIn('ram_style = "m10k_mixed"', rtl)
            self.assertIn("32'h13579BDF", rtl)
            self.assertIn("{wa, INDEX}" if write_bits == 40 else "mem[wa]", rtl)
            self.assertIn("{ra, INDEX}" if read_bits == 40 else "mem[ra]", rtl)
            policy = policy_for(experiment)
            policy.validate_source_text(f"experiments/{experiment}/rtl/top.v", rtl)

    def test_init_formula(self) -> None:
        self.assertEqual(m10k_mixed_lane(0), 0xA6)
        self.assertEqual(m10k_mixed_lane(1), ((1 * 73) ^ (1 >> 1) ^ 0xA6) & 0x3FF)
        self.assertEqual(m10k_mixed_abits(40), 8)
        self.assertEqual(m10k_mixed_abits(10), 10)

    def test_policy_requires_mixed_geometry_and_init(self) -> None:
        for experiment, write_bits, read_bits in (("510_m10k_mix40r10", 40, 10), ("520_m10k_mix10r40", 10, 40)):
            policy = policy_for(experiment)
            self.assertEqual(policy.m10k_mixed_write_dbits, write_bits)
            self.assertEqual(policy.m10k_mixed_read_dbits, read_bits)
            self.assertEqual(policy.nextpnr_router, "router1")
            self.assertTrue(policy.require_read_clock_arc)
            self.assertFalse(policy.nobram)
            design = {
                "modules": {
                    "top": {
                        "cells": {
                            "mem.0.0.0": _cell(write_bits, read_bits),
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

    def test_policy_rejects_equal_width_and_missing_mixed_flag(self) -> None:
        policy = policy_for("510_m10k_mix40r10")
        missing = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(40, 10, mixed=0),
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
                        "mem.0.0.0": _cell(40, 10, clk1=[4], clk2=[4]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(missing), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_MIXED_WIDTH"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(shared), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "independent"):
                policy.validate_synth_json(path)

    def test_timing_requires_read_clock_arc(self) -> None:
        policy = policy_for("510_m10k_mix40r10")
        policy.validate_timing_report(
            {"critical_paths": [{"from": "posedge read_clock", "max_delay": 40}]}
        )
        with self.assertRaisesRegex(PolicyError, "read_clock"):
            policy.validate_timing_report({"critical_paths": []})

    def test_summary_rejects_missing_read_clock_arc(self) -> None:
        policy = policy_for("510_m10k_mix40r10")
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
                    experiment="510_m10k_mix40r10",
                )

    def test_probe_covers_init_lanes_and_enables(self) -> None:
        cases = (
            ("510_m10k_mix40r10", "54299", "40-bit write"),
            ("520_m10k_mix10r40", "54300", "10-bit write"),
        )
        for experiment, signature, phrase in cases:
            probe = (ROOT / "experiments" / experiment / "hardware/probe.sh").read_text(
                encoding="utf-8"
            )
            self.assertIn(signature, probe)
            self.assertIn(phrase, probe)
            self.assertIn("read clock stopped", probe)
            self.assertIn("read-enable hold", probe)
            self.assertIn("0x13579bdf", probe)


if __name__ == "__main__":
    unittest.main()
