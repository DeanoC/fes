import json
import re
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


ROOT = Path(__file__).resolve().parents[1]
NAME = "755_dsp_signed"


class DspSignedLadderTests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "expected.md", "hardware/probe.sh"):
            self.assertTrue((ROOT / "experiments" / NAME / relative).is_file(), relative)

    def test_rtl_exposes_every_mode_with_full_width_operands(self) -> None:
        rtl = (ROOT / "experiments" / NAME / "rtl" / "top.v").read_text(encoding="utf-8")
        self.assertIn("16'hD61A", rtl)
        self.assertIn("$signed(left) * $signed(right)", rtl)
        self.assertIn("left * right", rtl)
        self.assertIn("left18 * right18", rtl)
        self.assertIn("left27 * right27", rtl)
        self.assertIn("hps_to_fpga[8:0]", rtl)
        self.assertIn("hps_to_fpga[17:9]", rtl)
        self.assertIn("hps_to_fpga[20:19]", rtl)
        # Registered products keep four DSP cells: Yosys's share pass would
        # otherwise merge the mutually exclusive views into one multiplier.
        self.assertIn("signed9_q <= signed9", rtl)
        self.assertIn("signed27_q <= signed27", rtl)
        match = re.search(r"(?ms)^\s*module\s+top\b\s*\((.*?)\)\s*;", rtl)
        self.assertIsNotNone(match)
        self.assertEqual(match.group(1).split(), ["input", "wire", "FPGA_CLK1_50"])

    def test_policy_requires_each_signedness_mode(self) -> None:
        policy = policy_for(NAME)
        self.assertTrue(policy.nobram)
        self.assertTrue(policy.nolutram)
        self.assertFalse(policy.nodsp)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram"))
        self.assertEqual(
            dict(policy.required_synth_cells),
            {"MISTRAL_MUL9X9": 2, "MISTRAL_MUL18X18": 1, "MISTRAL_MUL27X27": 1},
        )
        self.assertEqual(dict(policy.required_packed_sites), {"MISTRAL_MUL9X9": 2})
        self.assertEqual(policy.clock_evidence_names, ("product.FPGA_CLK1_50",))
        resources = {
            "MISTRAL_COMB": {"used": 120, "available": 83820},
            "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
            "MISTRAL_MUL9X9": {"used": 2, "available": 336},
            "MISTRAL_MUL18X18": {"used": 1, "available": 112},
            "MISTRAL_MUL27X27": {"used": 1, "available": 112},
            "MISTRAL_M10K": {"used": 0, "available": 553},
        }
        policy.validate_resources(resources)
        with self.assertRaisesRegex(PolicyError, "MUL"):
            policy.validate_resources({**resources, "MISTRAL_MUL9X9": {"used": 1, "available": 336}})

    def test_signed_and_unsigned_9x9_need_two_sites(self) -> None:
        policy = policy_for(NAME)
        cells = {
            "signed9": {"type": "MISTRAL_MUL9X9", "attributes": {"NEXTPNR_BEL": "MISTRAL_MUL9X9.32.2.0"}},
            "unsigned9": {"type": "MISTRAL_MUL9X9", "attributes": {"NEXTPNR_BEL": "MISTRAL_MUL9X9.32.4.0"}},
            "s18": {"type": "MISTRAL_MUL18X18", "attributes": {"NEXTPNR_BEL": "MISTRAL_MUL18X18.32.6.3"}},
            "s27": {"type": "MISTRAL_MUL27X27", "attributes": {"NEXTPNR_BEL": "MISTRAL_MUL27X27.32.8.4"}},
        }
        shared = dict(cells)
        shared["unsigned9"] = {"type": "MISTRAL_MUL9X9", "attributes": {"NEXTPNR_BEL": "MISTRAL_MUL9X9.32.2.1"}}
        with tempfile.TemporaryDirectory() as tmp:
            routed = Path(tmp) / "routed.json"
            routed.write_text(json.dumps({"modules": {"top": {"cells": cells}}}), encoding="utf-8")
            policy.validate_routed_json(routed)
            routed.write_text(json.dumps({"modules": {"top": {"cells": shared}}}), encoding="utf-8")
            with self.assertRaisesRegex(PolicyError, "site"):
                policy.validate_routed_json(routed)

    def test_probe_and_expected_agree_on_distinguishing_products(self) -> None:
        probe = (ROOT / "experiments" / NAME / "hardware" / "probe.sh").read_text(encoding="utf-8")
        expected = (ROOT / "experiments" / NAME / "expected.md").read_text(encoding="utf-8")
        self.assertIn("D61A", probe)
        for view in ("0x0003FC01", "0x00000001", "0xA5A1E878", "0x39C71C72"):
            self.assertIn(view, probe)
            self.assertIn(view, expected)
        self.assertNotIn("printf '0x%04X%04X'", probe)


if __name__ == "__main__":
    unittest.main()
