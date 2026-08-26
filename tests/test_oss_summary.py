from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SUMMARY = ROOT / "scripts" / "oss_summary.py"


class OssSummaryTests(unittest.TestCase):
    def _run_summary(self, timing: dict, route_log: str, *, previous: bytes | None = None) -> tuple[subprocess.CompletedProcess[str], Path]:
        temporary = tempfile.TemporaryDirectory(prefix="oss-summary-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        timing_path = root / "timing.json"
        route_path = root / "route.log"
        rbf_path = root / "top.rbf"
        output_path = root / "summary.json"
        timing_path.write_text(json.dumps(timing), encoding="utf-8")
        route_path.write_text(route_log, encoding="utf-8")
        rbf_path.write_bytes(b"rbf-bytes\n")
        command = [
            os.environ.get("PYTHON", "python3"),
            str(SUMMARY),
            "--timing-json",
            str(timing_path),
            "--route-log",
            str(route_path),
            "--rbf",
            str(rbf_path),
            "--requested-mhz",
            "50",
            "--clock-prefix",
            "FPGA_CLK1_50",
            "--output",
            str(output_path),
            "--timing-output",
            str(root / "timing.txt"),
            "--authenticated-tool",
            "yosys=13b43f8c85ec430a33ee55d058fb4c32b42b6910:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        ]
        if previous is not None:
            previous_path = root / "previous.rbf"
            previous_path.write_bytes(previous)
            command.extend(["--previous-rbf", str(previous_path)])
        result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
        return result, output_path

    def test_final_timing_json_failure_rejects_earlier_log_pass(self) -> None:
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 49.0}},
                "utilization": {},
            },
            "Info: Max frequency: 300.00 MHz (PASS at 50.00 MHz)\n",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(output.exists())

    def test_missing_intended_clock_rejects_earlier_log_pass(self) -> None:
        result, output = self._run_summary(
            {
                "fmax": {"OTHER_CLOCK": {"constraint": 50, "achieved": 300.0}},
                "utilization": {},
            },
            "Info: Max frequency: 300.00 MHz (PASS at 50.00 MHz)\n",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(output.exists())

    def test_summary_contains_final_timing_resources_tools_and_stability(self) -> None:
        timing = {
            "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
            "utilization": {
                "MISTRAL_COMB": {"used": 28, "available": 83820},
                "MISTRAL_M10K": {"used": 0, "available": 553},
            },
        }
        result, output = self._run_summary(timing, "Info: Program finished normally.\n", previous=b"rbf-bytes\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        summary = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(summary["status"], "pass")
        self.assertEqual(summary["route"]["status"], "pass")
        self.assertEqual(summary["timing"]["requested_mhz"], 50.0)
        self.assertEqual(summary["timing"]["achieved_mhz"], 234.5)
        self.assertEqual(summary["resources"]["MISTRAL_COMB"]["used"], 28)
        self.assertEqual(summary["hard_blocks"]["MISTRAL_M10K"]["used"], 0)
        self.assertEqual(summary["authenticated_tools"]["yosys"]["commit"], "13b43f8c85ec430a33ee55d058fb4c32b42b6910")
        self.assertTrue(summary["reproducibility"]["rbf_stability_measured"])
        self.assertTrue(summary["reproducibility"]["rbf_stable"])


if __name__ == "__main__":
    unittest.main()
