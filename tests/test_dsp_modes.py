import re
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
FORBIDDEN = (
    "LED",
    "PLL",
    "BRAM",
    "M10K",
    "MLAB",
    "LUTRAM",
    "altsyncram",
    "HDMI",
)


def _rtl(name: str) -> Path:
    return ROOT / "experiments" / name / "rtl" / "top.v"


class DspModeLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        extras = {
            "440_dsp_preadder": ("sim/dsp9_preadder.v",),
            "450_dsp_mac": ("sim/dsp18_mac.v",),
            "460_dsp_reg": ("sim/dsp18_reg.v",),
        }
        for name in (
            "420_dsp_mul18",
            "430_dsp_mul27",
            "440_dsp_preadder",
            "450_dsp_mac",
            "460_dsp_reg",
        ):
            for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
                path = ROOT / "experiments" / name / relative
                self.assertTrue(path.is_file(), path)
            for extra in extras.get(name, ()):
                path = ROOT / "experiments" / name / extra
                self.assertTrue(path.is_file(), path)

    def test_clock_only_hps_ports_and_signatures(self) -> None:
        signatures = {
            "420_dsp_mul18": "16'hD612",
            "430_dsp_mul27": "16'hD613",
            "440_dsp_preadder": "16'hD614",
            "450_dsp_mac": "16'hD615",
            "460_dsp_reg": "16'hD616",
        }
        for name, signature in signatures.items():
            rtl = _rtl(name).read_text(encoding="utf-8")
            match = re.search(
                r"(?ms)^\s*module\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
                rtl,
            )
            self.assertIsNotNone(match, name)
            self.assertEqual(
                re.sub(r"\s+", " ", match.group(1).strip()),
                "input wire FPGA_CLK1_50",
                name,
            )
            self.assertEqual(
                len(re.findall(r"\bcyclonev_hps_interface_mpu_general_purpose\b", rtl)),
                1,
                name,
            )
            self.assertIn(signature, rtl)
            for forbidden in FORBIDDEN:
                self.assertNotRegex(
                    rtl,
                    re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                    f"{name} {forbidden}",
                )

    def test_production_sources_satisfy_closed_policy(self) -> None:
        for name in (
            "420_dsp_mul18",
            "430_dsp_mul27",
            "440_dsp_preadder",
            "450_dsp_mac",
            "460_dsp_reg",
        ):
            policy = policy_for(name)
            relative = f"experiments/{name}/rtl/top.v"
            policy.validate_source_text(relative, _rtl(name).read_text(encoding="utf-8"))
            with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
                policy.validate_source_text(
                    relative,
                    _rtl(name).read_text(encoding="utf-8") + "\nwire LED;\n",
                )

    def test_mul18_and_mul27_use_native_yosys_cells(self) -> None:
        eighteen = policy_for("420_dsp_mul18")
        twentyseven = policy_for("430_dsp_mul27")
        self.assertFalse(eighteen.nodsp)
        self.assertFalse(twentyseven.nodsp)
        self.assertEqual(eighteen.yosys_post_synth, "")
        self.assertEqual(twentyseven.yosys_post_synth, "")
        self.assertEqual(dict(twentyseven.synth_json_input_ports), {})
        self.assertEqual(dict(eighteen.required_synth_cells), {"MISTRAL_MUL18X18": 1})
        self.assertEqual(dict(twentyseven.required_synth_cells), {"MISTRAL_MUL27X27": 1})
        self.assertIn("left * right", _rtl("420_dsp_mul18").read_text(encoding="utf-8"))
        self.assertIn("left * right", _rtl("430_dsp_mul27").read_text(encoding="utf-8"))
        self.assertNotIn("dsp27", _rtl("430_dsp_mul27").read_text(encoding="utf-8"))
        self.assertNotIn("NEGATE", _rtl("430_dsp_mul27").read_text(encoding="utf-8"))

    def test_control_mode_experiments_chtype_and_setparam(self) -> None:
        preadder = policy_for("440_dsp_preadder")
        mac = policy_for("450_dsp_mac")
        registered = policy_for("460_dsp_reg")
        self.assertIn("chtype -set MISTRAL_MUL9X9 t:dsp9_preadder", preadder.yosys_post_synth)
        self.assertIn("setparam -set PREADDER_EN 1", preadder.yosys_post_synth)
        self.assertIn("setparam -set PREADDER_SUB 1", preadder.yosys_post_synth)
        self.assertNotIn("setattr -set PREADDER", preadder.yosys_post_synth)
        self.assertIn("chtype -set MISTRAL_MUL18X18 t:dsp18_mac", mac.yosys_post_synth)
        self.assertIn("setparam -set INREG_CTRL_AX 1", registered.yosys_post_synth)
        self.assertIn("setparam -set OREG_CTRL 1", registered.yosys_post_synth)
        self.assertEqual(dict(preadder.required_source_identifiers)["dsp9_preadder"], 2)
        self.assertEqual(dict(mac.required_source_identifiers)["dsp18_mac"], 2)
        self.assertEqual(dict(registered.required_source_identifiers)["dsp18_reg"], 2)
        self.assertEqual(dict(preadder.synth_json_input_ports), {"MISTRAL_MUL9X9": ("Z",)})
        self.assertEqual(dict(mac.synth_json_input_ports), {"MISTRAL_MUL18X18": ("C",)})
        self.assertEqual(dict(registered.synth_json_input_ports), {"MISTRAL_MUL18X18": ("CLK",)})

    def test_apply_synth_json_marks_extra_ports_as_inputs(self) -> None:
        import json
        import tempfile

        policy = policy_for("440_dsp_preadder")
        design = {
            "modules": {
                "top": {
                    "cells": {
                        "product.preadder": {
                            "type": "MISTRAL_MUL9X9",
                            "port_directions": {
                                "A": "input",
                                "B": "input",
                                "Y": "output",
                                "Z": "output",
                            },
                            "connections": {"A": [1], "B": [2], "Y": [3], "Z": [4]},
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
                "product.preadder"
            ]
            self.assertEqual(cell["port_directions"]["Z"], "input")

    def test_sim_models_match_intended_arithmetic(self) -> None:
        preadder = (ROOT / "experiments/440_dsp_preadder/sim/dsp9_preadder.v").read_text(
            encoding="utf-8"
        )
        mac = (ROOT / "experiments/450_dsp_mac/sim/dsp18_mac.v").read_text(encoding="utf-8")
        registered = (ROOT / "experiments/460_dsp_reg/sim/dsp18_reg.v").read_text(
            encoding="utf-8"
        )
        self.assertIn("A * (B - Z)", preadder)
        self.assertIn("(A * B) + C", mac)
        self.assertIn("Y <= a_q * b_q", registered)
        self.assertNotIn("p_q", registered)

    def test_probes_use_hex_words_without_shell_shifts(self) -> None:
        for name, signature, token in (
            ("420_dsp_mul18", "D612", "0x%04X%04X"),
            ("430_dsp_mul27", "D613", "0x%02X%05X"),
            ("440_dsp_preadder", "D614", "ten_times_ten"),
            ("450_dsp_mac", "D615", "sixteen_sq"),
            ("460_dsp_reg", "D616", "hex_12_times_34"),
        ):
            probe = (ROOT / "experiments" / name / "hardware" / "probe.sh").read_text(
                encoding="utf-8"
            )
            self.assertIn(signature, probe)
            self.assertIn(token, probe)
            self.assertNotIn(">> 16", probe)

    def test_resources_require_the_selected_dsp_mode(self) -> None:
        cases = (
            ("420_dsp_mul18", "MISTRAL_MUL18X18", 112),
            ("430_dsp_mul27", "MISTRAL_MUL27X27", 112),
            ("440_dsp_preadder", "MISTRAL_MUL9X9", 336),
            ("450_dsp_mac", "MISTRAL_MUL18X18", 112),
            ("460_dsp_reg", "MISTRAL_MUL18X18", 112),
        )
        for name, cell, available in cases:
            policy = policy_for(name)
            self.assertTrue(policy.nobram)
            self.assertTrue(policy.nolutram)
            self.assertFalse(policy.nodsp)
            self.assertEqual(policy.clock_evidence_names, ("product.FPGA_CLK1_50",))
            policy.validate_resources(
                {
                    "MISTRAL_COMB": {"used": 20, "available": 83820},
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                    cell: {"used": 1, "available": available},
                    "MISTRAL_M10K": {"used": 0, "available": 553},
                }
            )
            with self.assertRaisesRegex(PolicyError, "MUL"):
                policy.validate_resources(
                    {
                        "cyclonev_hps_interface_mpu_general_purpose": {
                            "used": 1,
                            "available": 1,
                        },
                        cell: {"used": 0, "available": available},
                    }
                )


if __name__ == "__main__":
    unittest.main()
