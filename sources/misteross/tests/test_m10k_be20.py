import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import (
    PolicyError,
    m10k_init_word,
    policy_for,
)
from scripts.oss_summary import SummaryError, build_summary


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/500_m10k_be20/rtl/top.v"
PROBE = ROOT / "experiments/500_m10k_be20/hardware/probe.sh"


def _cell(*, byte_enable: int = 1, dual: int = 1, be=None, clk1=None, clk2=None, init=None):
    if be is None:
        be = [10, 11]
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    if init is None:
        packed = 0
        for address in range(256):
            packed |= m10k_init_word(address, 20) << (address * 20)
        init = packed
    return {
        "type": "MISTRAL_M10K",
        "parameters": {
            "CFG_ABITS": f"{9:032b}",
            "CFG_DBITS": f"{20:032b}",
            "CFG_DUAL_CLOCK": f"{dual:032b}",
            "CFG_BYTE_ENABLE": f"{byte_enable:032b}",
            "INIT": f"{init:b}",
        },
        "connections": {
            "CLK1": clk1,
            "CLK2": clk2,
            "A1EN": [3],
            "A1BE": be,
        },
        "port_directions": {
            "CLK1": "input",
            "CLK2": "input",
            "A1EN": "input",
            "A1BE": "input",
        },
    }


class M10kByteEnableLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/500_m10k_be20" / relative
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
        self.assertEqual(len(re.findall(r"\baltera_pll\b", rtl)), 1)
        self.assertEqual(len(re.findall(r"\bcyclonev_clkena\b", rtl)), 1)
        self.assertIn("16'hD41A", rtl)
        self.assertIn('ramstyle = "M10K"', rtl)
        self.assertIn("32'h13579BDF", rtl)
        self.assertIn("wbe", rtl)
        self.assertIn("mem[waddr][9:0]", rtl)
        self.assertIn("mem[waddr][19:10]", rtl)
        policy = policy_for("500_m10k_be20")
        policy.validate_source_text("experiments/500_m10k_be20/rtl/top.v", rtl)

    def test_init_formula(self) -> None:
        self.assertEqual(m10k_init_word(0, 20), 0xA6)
        self.assertEqual(m10k_init_word(1, 20), ((1 * 73) ^ (1 >> 1) ^ 0xA6) & 0xFFFFF)

    def test_policy_requires_byte_enables_and_init(self) -> None:
        policy = policy_for("500_m10k_be20")
        self.assertTrue(policy.m10k_byte_enable)
        self.assertTrue(policy.require_read_clock_arc)
        self.assertFalse(policy.nobram)
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(),
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

    def test_policy_rejects_missing_byte_enable(self) -> None:
        policy = policy_for("500_m10k_be20")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(byte_enable=0),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_BYTE_ENABLE"):
                policy.validate_synth_json(path)

    def test_policy_rejects_tied_byte_lanes(self) -> None:
        policy = policy_for("500_m10k_be20")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(be=[4, 4]),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "A1BE"):
                policy.validate_synth_json(path)

    def test_timing_requires_read_clock_arc(self) -> None:
        policy = policy_for("500_m10k_be20")
        policy.validate_timing_report(
            {
                "critical_paths": [{"from": "posedge read_clock", "max_delay": 40}],
            }
        )
        with self.assertRaisesRegex(PolicyError, "read_clock"):
            policy.validate_timing_report({"critical_paths": []})

    def test_summary_rejects_missing_read_clock_arc(self) -> None:
        policy = policy_for("500_m10k_be20")
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
                    experiment="500_m10k_be20",
                )

    def test_probe_covers_lanes_and_zero_mask(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("54298", probe)
        self.assertIn("BYTEENABLEA[0]", probe)
        self.assertIn("BYTEENABLEA[1]", probe)
        self.assertIn("zero byte mask", probe)
        self.assertIn("0x13579bdf", probe)


if __name__ == "__main__":
    unittest.main()
