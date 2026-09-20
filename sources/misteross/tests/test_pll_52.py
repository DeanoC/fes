import json
from pathlib import Path
import tempfile
import unittest

from scripts.experiment_policy import PolicyError, policy_for
from scripts.oss_summary import SummaryError, _timing

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "experiments/760_pll_52/rtl/top.v"
PROBE = ROOT / "experiments/760_pll_52/hardware/probe.sh"


class Pll52Tests(unittest.TestCase):
    def test_sources_exist(self) -> None:
        for relative in ("rtl/top.v", "sim/tb.cpp", "sim/pll_model.v", "expected.md", "hardware/probe.sh"):
            path = ROOT / "experiments/760_pll_52" / relative
            self.assertTrue(path.is_file(), path)

    def test_closed_resources_and_52_mhz_request(self) -> None:
        rtl = RTL.read_text(encoding="utf-8")
        self.assertIn("16'hD752", rtl)
        self.assertIn('output_clock_frequency0("52.0 MHz")', rtl)
        policy = policy_for("760_pll_52")
        policy.validate_source_text("experiments/760_pll_52/rtl/top.v", rtl)
        self.assertEqual(dict(policy.additional_clocks_mhz), {"clk52": 52.0})
        self.assertEqual(policy.clock_evidence_names, ("meter.refclk",))
        resources = {
            "altera_pll": {"used": 1},
            "cyclonev_hps_interface_mpu_general_purpose": {"used": 1},
        }
        policy.validate_resources(resources)
        with self.assertRaises(PolicyError):
            policy.validate_resources({**resources, "MISTRAL_MUL18X19": {"used": 1}})

    def test_probe_accepts_52_mhz_window(self) -> None:
        probe = PROBE.read_text(encoding="utf-8")
        self.assertIn("D752", probe)
        self.assertIn("4258", probe)
        self.assertIn("4261", probe)

    def test_both_clock_domains_required(self) -> None:
        policy = policy_for("760_pll_52")
        ref = {"constraint": 50, "achieved": 200}
        generated = {"constraint": 52, "achieved": 300}
        cases = [
            ({"meter.refclk": ref, "clk52": generated}, True),
            ({"meter.refclk": ref}, False),
            ({"clk52": generated}, False),
            ({"meter.refclk": ref, "clk52": {"constraint": 50, "achieved": 300}}, False),
        ]
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "timing.json"
            for clocks, passing in cases:
                with self.subTest(clocks=clocks):
                    path.write_text(json.dumps({"fmax": clocks}))
                    if passing:
                        self.assertEqual(_timing(path, 50, policy.clock, policy), ("meter.refclk", 200))
                    else:
                        with self.assertRaises(SummaryError):
                            _timing(path, 50, policy.clock, policy)
