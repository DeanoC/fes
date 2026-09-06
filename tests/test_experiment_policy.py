from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for


class ExperimentPolicyTests(unittest.TestCase):
    ROOT = Path(__file__).resolve().parents[1]

    def test_unknown_experiment_is_rejected(self) -> None:
        with self.assertRaisesRegex(PolicyError, "unknown experiment"):
            policy_for("030_unknown")

    def test_blinky_preserves_zero_hard_block_policy(self) -> None:
        policy = policy_for("010_blinky")
        self.assertEqual(policy.name, "010_blinky")
        self.assertEqual(policy.top, "top")
        self.assertEqual(policy.clock, "FPGA_CLK1_50")
        self.assertEqual(policy.clock_mhz, 50.0)
        self.assertEqual(dict(policy.allowed_hard_blocks), {})
        self.assertEqual(
            policy.sources,
            ("experiments/010_blinky/rtl/top.v",),
        )
        self.assertIn("M10K", policy.forbidden_resource_patterns)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram", "-nodsp"))
        self.assertEqual(policy.yosys_post_synth, r"cd top; rename LED \LED[0]; ")
        self.assertEqual(len(policy.sim_jobs), 1)
        self.assertEqual(policy.sim_jobs[0].name, "main")
        self.assertEqual(policy.sim_jobs[0].parameters["COUNTER_BITS"], "4")

    def test_mailbox_requires_exactly_one_hps_general_purpose_primitive(self) -> None:
        policy = policy_for("020_linux_mailbox")
        self.assertEqual(policy.top, "top")
        self.assertEqual(policy.clock, "FPGA_CLK1_50")
        self.assertEqual(policy.clock_mhz, 50.0)
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {"cyclonev_hps_interface_mpu_general_purpose": 1},
        )
        self.assertEqual(
            policy.sources,
            ("experiments/020_linux_mailbox/rtl/top.v",),
        )
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram", "-nodsp"))
        self.assertEqual(policy.yosys_post_synth, "")
        self.assertEqual([job.name for job in policy.sim_jobs], ["main", "wrap"])
        self.assertFalse(policy.sim_jobs[1].lint)

    def test_forbidden_source_pattern_is_rejected(self) -> None:
        policy = policy_for("020_linux_mailbox")
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                "experiments/020_linux_mailbox/rtl/top.v",
                "module top; cyclonev_oscillator oscillator; endmodule\n",
            )

    def test_mailbox_resource_policy_accepts_only_the_hps_count(self) -> None:
        policy = policy_for("020_linux_mailbox")
        policy.validate_resources(
            {
                "MISTRAL_COMB": {"used": 1, "available": 10},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                "MISTRAL_M10K": {"used": 0, "available": 553},
            }
        )
        with self.assertRaisesRegex(PolicyError, "M10K"):
            policy.validate_resources(
                {
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                    "MISTRAL_M10K": {"used": 1, "available": 553},
                }
            )

    def test_unknown_resource_is_rejected_even_when_unused(self) -> None:
        policy = policy_for("020_linux_mailbox")
        with self.assertRaisesRegex(PolicyError, "unknown resource"):
            policy.validate_resources({"MISTRAL_UNCLASSIFIED": {"used": 0, "available": 1}})

    def test_m10k_rom_requires_exactly_one_block_and_drops_nobram(self) -> None:
        policy = policy_for("030_m10k_rom")
        self.assertEqual(policy.top, "top")
        self.assertFalse(policy.nobram)
        self.assertTrue(policy.nolutram)
        self.assertTrue(policy.nodsp)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nolutram", "-nodsp"))
        self.assertEqual(dict(policy.allowed_hard_blocks), {"MISTRAL_M10K": 1})
        self.assertEqual(
            policy.sources,
            ("experiments/030_m10k_rom/rtl/top.v",),
        )
        policy.validate_resources(
            {
                "MISTRAL_COMB": {"used": 1, "available": 10},
                "MISTRAL_M10K": {"used": 1, "available": 553},
            }
        )
        with self.assertRaisesRegex(PolicyError, "M10K"):
            policy.validate_resources(
                {
                    "MISTRAL_COMB": {"used": 1, "available": 10},
                    "MISTRAL_M10K": {"used": 0, "available": 553},
                }
            )

    def test_mlab_ram_requires_eight_tables_and_one_hps(self) -> None:
        policy = policy_for("040_mlab_ram")
        self.assertTrue(policy.nobram)
        self.assertFalse(policy.nolutram)
        self.assertTrue(policy.nodsp)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nodsp"))
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {"cyclonev_hps_interface_mpu_general_purpose": 1},
        )
        self.assertEqual(dict(policy.required_synth_cells), {"MISTRAL_MLAB": 8})
        policy.validate_resources(
            {
                "MISTRAL_COMB": {"used": 1, "available": 10},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                "MISTRAL_M10K": {"used": 0, "available": 553},
            }
        )
        with self.assertRaisesRegex(PolicyError, "MLAB"):
            policy.validate_resources(
                {
                    "MISTRAL_MLAB": {"used": 1, "available": 41910},
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                }
            )
        with tempfile.TemporaryDirectory() as directory:
            synth = Path(directory) / "synth.json"
            cells = {f"cell{index}": {"type": "MISTRAL_MLAB"} for index in range(8)}
            synth.write_text(
                json.dumps({"modules": {"top": {"cells": cells}}}),
                encoding="utf-8",
            )
            policy.validate_synth_json(synth)
            cells["cell0"] = {"type": "MISTRAL_FF"}
            synth.write_text(
                json.dumps({"modules": {"top": {"cells": cells}}}),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(PolicyError, "MISTRAL_MLAB"):
                policy.validate_synth_json(synth)

    def test_lut_mul_requires_hps_keeps_nodsp_and_rejects_vendor_product_cells(self) -> None:
        policy = policy_for("050_lut_mul")
        self.assertTrue(policy.nobram)
        self.assertTrue(policy.nolutram)
        self.assertTrue(policy.nodsp)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram", "-nodsp"))
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {"cyclonev_hps_interface_mpu_general_purpose": 1},
        )
        self.assertEqual(dict(policy.required_synth_cells), {})
        self.assertEqual(policy.clock_evidence_names, ("product.FPGA_CLK1_50",))
        policy.validate_resources(
            {
                "MISTRAL_COMB": {"used": 40, "available": 83820},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                "MISTRAL_M10K": {"used": 0, "available": 553},
                "MISTRAL_MUL9X9": {"used": 0, "available": 112},
            }
        )
        with self.assertRaisesRegex(PolicyError, "MUL"):
            policy.validate_resources(
                {
                    "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                    "MISTRAL_MUL9X9": {"used": 1, "available": 112},
                }
            )

    def test_wrong_top_and_source_list_are_rejected(self) -> None:
        policy = policy_for("010_blinky")
        with self.assertRaisesRegex(PolicyError, "top"):
            policy.validate_design(top="not_top", sources=policy.sources)
        with self.assertRaisesRegex(PolicyError, "source"):
            policy.validate_design(top=policy.top, sources=("wrong/top.v",))

    def test_mailbox_source_policy_uses_tokens_hashes_and_synthesis_evidence(self) -> None:
        policy = policy_for("020_linux_mailbox")
        relative = "experiments/020_linux_mailbox/rtl/top.v"
        source = (self.ROOT / relative).read_text(encoding="utf-8")

        policy.validate_source_text(relative, source)
        with self.assertRaisesRegex(PolicyError, "forbidden source pattern"):
            policy.validate_source_text(
                relative,
                source.replace("mailbox_fsm protocol (", "mailbox_fsm video ("),
            )
        with self.assertRaisesRegex(PolicyError, "source identifier"):
            policy.validate_source_text(
                relative,
                source
                + "\ncyclonev_hps_interface_mpu_general_purpose second_gp();\n",
            )
        with self.assertRaisesRegex(PolicyError, "source includes"):
            policy.validate_source_text(
                relative,
                '`include "extra_mailbox_logic.v"\n' + source,
            )


if __name__ == "__main__":
    unittest.main()
