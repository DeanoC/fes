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


def _cell(width: int, *, dual: int = 1, clk1=None, clk2=None, init=None, abits=None):
    if clk1 is None:
        clk1 = [1]
    if clk2 is None:
        clk2 = [2]
    if abits is None:
        abits = 8 if width == 40 else 9
    if init is None:
        packed = 0
        for address in range(256):
            packed |= m10k_init_word(address, width) << (address * width)
        init = packed
    return {
        "type": "MISTRAL_M10K",
        "parameters": {
            "CFG_ABITS": f"{abits:032b}",
            "CFG_DBITS": f"{width:032b}",
            "CFG_DUAL_CLOCK": f"{dual:032b}",
            "INIT": f"{init:b}",
        },
        "connections": {"CLK1": clk1, "CLK2": clk2},
    }


class M10kSdpLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for experiment in ("480_m10k_sdp20", "490_m10k_sdp40"):
            for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
                path = ROOT / "experiments" / experiment / relative
                self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signatures(self) -> None:
        cases = (
            ("480_m10k_sdp20", "16'hD418", 20),
            ("490_m10k_sdp40", "16'hD419", 40),
        )
        for experiment, signature, width in cases:
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
            self.assertIn(f"WIDTH = {width}", rtl)
            self.assertIn('ramstyle = "M10K"', rtl)
            self.assertIn("32'h13579BDF", rtl)
            self.assertIn("read_clock", rtl)
            self.assertIn("20'h00A6", rtl)
            policy = policy_for(experiment)
            policy.validate_source_text(f"experiments/{experiment}/rtl/top.v", rtl)

    def test_init_formula(self) -> None:
        self.assertEqual(m10k_init_word(0, 20), 0xA6)
        self.assertEqual(m10k_init_word(0, 40), ((~0xA6 & 0xFFFFF) << 20) | 0xA6)
        self.assertEqual(m10k_init_word(1, 20), ((1 * 73) ^ (1 >> 1) ^ 0xA6) & 0xFFFFF)

    def test_policy_requires_independent_clocks_and_init(self) -> None:
        for experiment, width in (("480_m10k_sdp20", 20), ("490_m10k_sdp40", 40)):
            policy = policy_for(experiment)
            self.assertEqual(policy.m10k_dual_clock_width, width)
            self.assertTrue(policy.require_read_clock_arc)
            self.assertFalse(policy.nobram)
            self.assertTrue(policy.nolutram)
            self.assertTrue(policy.nodsp)
            self.assertEqual(
                dict(policy.required_synth_cells),
                {"MISTRAL_M10K": 1, "altera_pll": 1, "cyclonev_clkena": 1},
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

    def test_policy_rejects_common_clock_and_missing_init(self) -> None:
        policy = policy_for("480_m10k_sdp20")
        bad_dual = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(20, dual=0),
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
        missing_init = {
            "modules": {
                "top": {
                    "cells": {
                        "mem.0.0.0": _cell(20, init=0),
                        "pll": {"type": "altera_pll"},
                        "gate": {"type": "cyclonev_clkena"},
                    }
                }
            }
        }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(bad_dual), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "CFG_DUAL_CLOCK"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(shared), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "independent"):
                policy.validate_synth_json(path)
            path.write_text(json.dumps(missing_init), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "INIT"):
                policy.validate_synth_json(path)

    def test_timing_requires_read_clock_arc(self) -> None:
        policy = policy_for("480_m10k_sdp20")
        policy.validate_timing_report(
            {
                "fmax": {
                    "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q": {
                        "constraint": 50,
                        "achieved": 400,
                    }
                },
                "critical_paths": [{"from": "posedge read_clock", "max_delay": 40}],
            }
        )
        with self.assertRaisesRegex(PolicyError, "read_clock"):
            policy.validate_timing_report(
                {
                    "fmax": {
                        "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q": {
                            "constraint": 50,
                            "achieved": 400,
                        }
                    },
                    "critical_paths": [{"from": "posedge FPGA_CLK1_50", "max_delay": 20}],
                }
            )

    def test_summary_rejects_missing_read_clock_arc(self) -> None:
        policy = policy_for("480_m10k_sdp20")
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
                    experiment="480_m10k_sdp20",
                )

    def test_probe_covers_init_stop_and_enables(self) -> None:
        cases = (
            ("480_m10k_sdp20", "54296", "20"),
            ("490_m10k_sdp40", "54297", "40"),
        )
        for experiment, signature, width in cases:
            probe = (ROOT / "experiments" / experiment / "hardware/probe.sh").read_text(
                encoding="utf-8"
            )
            self.assertIn(signature, probe)
            self.assertIn(f"all {width} data bits initialized", probe)
            self.assertIn("read clock stopped", probe)
            self.assertIn("write-enable hold", probe)
            self.assertIn("0x13579bdf", probe)


if __name__ == "__main__":
    unittest.main()
