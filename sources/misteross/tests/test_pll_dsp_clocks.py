import json
import tempfile
import unittest
from pathlib import Path

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]

EXPERIMENTS = (
    ("140_pll_dsp_20", "clk20", 20.0, "16'hDC20", "20.0 MHz"),
    ("150_pll_dsp_80", "clk80", 80.0, "16'hDC80", "80.0 MHz"),
    ("160_pll_dsp_100", "clk100", 100.0, "16'hDC64", "100.0 MHz"),
)


class PllDspClockFamilyTests(unittest.TestCase):
    def test_closed_resources_sources_and_clocks(self):
        for name, clock, mhz, signature, freq in EXPERIMENTS:
            with self.subTest(name=name):
                policy = policy_for(name)
                self.assertFalse(policy.nodsp)
                self.assertEqual(policy.synth_intel_alm_flags, ("-nobram", "-nolutram"))
                self.assertEqual(policy.clock_evidence_names, ("host_port.FPGA_CLK1_50",))
                self.assertEqual(dict(policy.additional_clocks_mhz), {clock: mhz})
                self.assertEqual(
                    dict(policy.allowed_hard_blocks),
                    {
                        "cyclonev_hps_interface_mpu_general_purpose": 1,
                        "altera_pll": 1,
                        "MISTRAL_MUL9X9": 1,
                    },
                )
                rtl = ROOT / "experiments" / name / "rtl" / "top.v"
                tb = ROOT / "experiments" / name / "sim" / "tb.cpp"
                model = ROOT / "experiments" / name / "sim" / "pll_model.v"
                probe = ROOT / "experiments" / name / "hardware" / "probe.sh"
                for path in (rtl, tb, model, probe, ROOT / "experiments" / name / "expected.md"):
                    self.assertTrue(path.is_file(), path)
                text = rtl.read_text(encoding="utf-8")
                policy.validate_source_text(f"experiments/{name}/rtl/top.v", text)
                self.assertIn(signature, text)
                self.assertIn(f'.output_clock_frequency0("{freq}")', text)
                self.assertIn(f".outclk({clock})", text)
                self.assertNotIn("clk25", text)
                policy.validate_resources(
                    {
                        "altera_pll": {"used": 1},
                        "MISTRAL_MUL9X9": {"used": 1},
                        "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
                    }
                )
                with self.assertRaises(PolicyError):
                    policy.validate_source_text(
                        f"experiments/{name}/rtl/top.v",
                        text + "\nwire LED;\n",
                    )

    def test_both_clock_domains_required(self):
        for name, clock, mhz, _signature, _freq in EXPERIMENTS:
            with self.subTest(name=name):
                policy = policy_for(name)
                ref = {"constraint": 50, "achieved": 200}
                generated = {"constraint": mhz, "achieved": 300}
                cases = [
                    ({"host_port.FPGA_CLK1_50": ref, clock: generated}, True),
                    ({"host_port.FPGA_CLK1_50": ref}, False),
                    ({clock: generated}, False),
                    (
                        {
                            "host_port.FPGA_CLK1_50": ref,
                            clock: {"constraint": mhz, "achieved": mhz - 1},
                        },
                        False,
                    ),
                ]
                with tempfile.TemporaryDirectory() as temporary:
                    path = Path(temporary) / "timing.json"
                    for clocks, passing in cases:
                        path.write_text(json.dumps({"fmax": clocks}))
                        if passing:
                            self.assertEqual(
                                _timing(path, 50, policy.clock, policy),
                                ("host_port.FPGA_CLK1_50", 200),
                            )
                        else:
                            with self.assertRaises(SummaryError):
                                _timing(path, 50, policy.clock, policy)
