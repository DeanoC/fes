import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments" / "410_dsp_triple" / "rtl" / "top.v"
TB = ROOT / "experiments" / "410_dsp_triple" / "sim" / "tb.cpp"
EXPECTED = ROOT / "experiments" / "410_dsp_triple" / "expected.md"
PROBE = ROOT / "experiments" / "410_dsp_triple" / "hardware" / "probe.sh"
MODEL = ROOT / "experiments" / "020_linux_mailbox" / "sim" / "hps_gp_model.v"


def _routed(bels: list[str]) -> str:
    cells = {
        f"mul{index}": {
            "type": "MISTRAL_MUL9X9",
            "attributes": {"NEXTPNR_BEL": bel},
        }
        for index, bel in enumerate(bels)
    }
    return json.dumps({"modules": {"top": {"cells": cells}}})


class DspTripleTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for path in (RTL, TB, EXPECTED, PROBE, MODEL):
            self.assertTrue(path.is_file(), path)

    def test_rtl_exposes_clock_only_ports_and_three_hps_products(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^\s*module\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
            rtl,
        )
        self.assertIsNotNone(match, "top module declaration is missing")
        self.assertEqual(
            re.sub(r"\s+", " ", match.group(1).strip()),
            "input wire FPGA_CLK1_50",
        )
        self.assertEqual(
            len(re.findall(r"\bcyclonev_hps_interface_mpu_general_purpose\b", rtl)),
            1,
        )
        self.assertIn("16'hD611", rtl)
        self.assertEqual(len(re.findall(r"\bpacked_product\b", rtl)), 4)
        self.assertIn("keep_hierarchy", rtl)
        self.assertIn("left * right", rtl)
        self.assertIn("~right", rtl)
        self.assertIn("8'h01", rtl)
        self.assertIn("hps_to_fpga[18:17]", rtl)
        for forbidden in (
            "LED",
            "PLL",
            "BRAM",
            "M10K",
            "MLAB",
            "LUTRAM",
            "altsyncram",
            "HDMI",
        ):
            self.assertNotRegex(
                rtl,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_production_source_satisfies_closed_policy(self) -> None:
        policy = policy_for("410_dsp_triple")
        relative = "experiments/410_dsp_triple/rtl/top.v"
        policy.validate_source_text(relative, RTL.read_text(encoding="utf-8"))
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                relative,
                RTL.read_text(encoding="utf-8") + "\nwire LED;\n",
            )

    def test_closed_resources_require_three_logical_lanes(self) -> None:
        policy = policy_for("410_dsp_triple")
        self.assertTrue(policy.nobram)
        self.assertTrue(policy.nolutram)
        self.assertFalse(policy.nodsp)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram"))
        self.assertEqual(
            policy.yosys_post_synth,
            "setattr -mod -unset keep_hierarchy packed_product; flatten; ",
        )
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL9X9": 3,
            },
        )
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_MUL9X9": 3})
        self.assertEqual(dict(policy.required_packed_sites), {"MISTRAL_MUL9X9": 1})
        self.assertEqual(policy.clock_evidence_names, ("product.FPGA_CLK1_50",))
        policy.validate_resources(
            {
                "MISTRAL_COMB": {"used": 20, "available": 83820},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                "MISTRAL_MUL9X9": {"used": 3, "available": 336},
                "MISTRAL_M10K": {"used": 0, "available": 553},
            }
        )
        with self.assertRaisesRegex(PolicyError, "MUL"):
            policy.validate_resources(
                {
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                    "MISTRAL_MUL9X9": {"used": 1, "available": 336},
                }
            )

    def test_routed_json_requires_one_physical_dsp_site(self) -> None:
        policy = policy_for("410_dsp_triple")
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "routed.json"
            path.write_text(
                _routed(
                    [
                        "MISTRAL_MUL9X9.32.79.0",
                        "MISTRAL_MUL9X9.32.79.1",
                        "MISTRAL_MUL9X9.32.79.2",
                    ]
                )
            )
            policy.validate_routed_json(path)
            path.write_text(
                _routed(
                    [
                        "MISTRAL_MUL9X9.32.79.0",
                        "MISTRAL_MUL9X9.32.80.1",
                        "MISTRAL_MUL9X9.32.81.2",
                    ]
                )
            )
            with self.assertRaisesRegex(PolicyError, "physical site"):
                policy.validate_routed_json(path)
            path.write_text(
                _routed(
                    [
                        "MISTRAL_MUL9X9.32.79.0",
                        "MISTRAL_MUL9X9.32.79.1",
                    ]
                )
            )
            with self.assertRaisesRegex(PolicyError, "exactly 3"):
                policy.validate_routed_json(path)
            path.write_text(
                _routed(
                    [
                        "MISTRAL_MUL9X9.32.79.0",
                        "MISTRAL_MUL9X9.32.79.0",
                        "MISTRAL_MUL9X9.32.79.1",
                    ]
                )
            )
            with self.assertRaisesRegex(PolicyError, "lanes"):
                policy.validate_routed_json(path)

    def test_probe_matches_hex_signature_without_shell_shifts(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("D611", probe)
        self.assertIn("ten_times_twelve_anotb", probe)
        self.assertIn("ten_times_twelve_xor", probe)
        self.assertNotIn(">> 16", probe)

    def test_existing_dsp_mul_does_not_require_packed_sites(self) -> None:
        policy = policy_for("060_dsp_mul")
        self.assertEqual(dict(policy.required_packed_sites), {})
        self.assertNotIn("required_packed_sites", policy.as_dict())


if __name__ == "__main__":
    unittest.main()
