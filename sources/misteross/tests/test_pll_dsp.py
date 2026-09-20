import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]


class PllDspTests(unittest.TestCase):
    def test_closed_resources_and_sources(self):
        policy = policy_for("120_pll_dsp")
        self.assertFalse(policy.nodsp)
        self.assertTrue(policy.nobram)
        self.assertTrue(policy.nolutram)
        self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram"))
        self.assertEqual(policy.clock_evidence_names, ("host_port.FPGA_CLK1_50",))
        self.assertEqual(dict(policy.additional_clocks_mhz), {"clk25": 25.0})
        self.assertEqual(
            dict(policy.allowed_hard_blocks),
            {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_MUL9X9": 1,
            },
        )
        self.assertEqual(
            dict(policy.required_synth_cells),
            {"altera_pll": 1, "MISTRAL_MUL9X9": 1},
        )
        for relative in policy.sources:
            policy.validate_source_text(relative, (ROOT / relative).read_text())
        resources = {
            "altera_pll": {"used": 1},
            "MISTRAL_MUL9X9": {"used": 1},
            "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
        }
        policy.validate_resources(resources)
        with self.assertRaises(PolicyError):
            policy.validate_resources({**resources, "MISTRAL_MUL9X9": {"used": 0}})
        with self.assertRaises(PolicyError):
            policy.validate_resources({**resources, "altera_pll": {"used": 0}})
        with self.assertRaises(PolicyError):
            policy.validate_resources({**resources, "MISTRAL_M10K": {"used": 1}})

    def test_both_clock_domains_required(self):
        policy = policy_for("120_pll_dsp")
        ref = {"constraint": 50, "achieved": 200}
        generated = {"constraint": 25, "achieved": 300}
        cases = [
            ({"host_port.FPGA_CLK1_50": ref, "clk25": generated}, True),
            ({"host_port.FPGA_CLK1_50": ref}, False),
            ({"clk25": generated}, False),
            ({"host_port.FPGA_CLK1_50": ref, "clk25": {"constraint": 50, "achieved": 300}}, False),
            ({"host_port.FPGA_CLK1_50": ref, "clk25": {"constraint": 25, "achieved": 24}}, False),
        ]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            for clocks, passing in cases:
                with self.subTest(clocks=clocks):
                    path.write_text(json.dumps({"fmax": clocks}))
                    if passing:
                        self.assertEqual(
                            _timing(path, 50, policy.clock, policy),
                            ("host_port.FPGA_CLK1_50", 200),
                        )
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
