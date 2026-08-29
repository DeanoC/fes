from __future__ import annotations

import unittest

from scripts.experiment_policy import PolicyError, policy_for


class ExperimentPolicyTests(unittest.TestCase):
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

    def test_wrong_top_and_source_list_are_rejected(self) -> None:
        policy = policy_for("010_blinky")
        with self.assertRaisesRegex(PolicyError, "top"):
            policy.validate_design(top="not_top", sources=policy.sources)
        with self.assertRaisesRegex(PolicyError, "source"):
            policy.validate_design(top=policy.top, sources=("wrong/top.v",))


if __name__ == "__main__":
    unittest.main()
