from __future__ import annotations

import json
import hashlib
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SUMMARY = ROOT / "scripts" / "oss_summary.py"
YOSYS_COMMIT = "ec34fcf38986217af9b5558936044b7197d968a7"
NEXTPNR_COMMIT = "47c4251acc89eb9bf6742e32204af744a23446e0"
YOSYS_DIGEST = "a" * 64
NEXTPNR_DIGEST = "b" * 64
SOURCE_HASHES = {
    "boards/de10nano/clocks.sdc": "1" * 64,
    "boards/de10nano/pins.qsf": "2" * 64,
    "experiments/010_blinky/rtl/top.v": "3" * 64,
    "scripts/build_oss.sh": "4" * 64,
    "scripts/collect_manifest.py": "5" * 64,
    "scripts/oss_summary.py": "6" * 64,
    "scripts/run_logged.sh": "7" * 64,
    "toolchain.lock": "8" * 64,
}
MAILBOX_SOURCE_HASHES = {
    **SOURCE_HASHES,
    "experiments/010_blinky/rtl/top.v": "3" * 64,
    "experiments/020_linux_mailbox/rtl/top.v": "9" * 64,
}


class OssSummaryTests(unittest.TestCase):
    def _run_summary(
        self,
        timing: dict,
        route_log: str,
        *,
        previous: bytes | None = None,
        previous_manifest: dict | None = None,
        source_hashes: dict[str, str] | None = None,
        tool_pins: dict[str, str] | None = None,
        tool_auth: dict[str, tuple[str, str]] | None = None,
        lane: str = "oss",
        experiment: str = "010_blinky",
    ) -> tuple[subprocess.CompletedProcess[str], Path]:
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
            "--lane",
            lane,
            "--experiment",
            experiment,
            "--authenticated-tool",
            f"yosys={YOSYS_COMMIT}:{(tool_auth or {}).get('yosys', (YOSYS_COMMIT, YOSYS_DIGEST))[1]}",
            "--authenticated-tool",
            f"nextpnr-mistral={NEXTPNR_COMMIT}:{(tool_auth or {}).get('nextpnr-mistral', (NEXTPNR_COMMIT, NEXTPNR_DIGEST))[1]}",
        ]
        selected_sources = source_hashes or (MAILBOX_SOURCE_HASHES if experiment == "020_linux_mailbox" else SOURCE_HASHES)
        for path, digest in sorted(selected_sources.items()):
            command.extend(["--source-hash", f"{path}={digest}"])
        for name, commit in sorted((tool_pins or {"nextpnr": NEXTPNR_COMMIT, "yosys": YOSYS_COMMIT}).items()):
            command.extend(["--tool-pin", f"{name}={commit}"])
        if previous is not None:
            previous_path = root / "previous.rbf"
            previous_path.write_bytes(previous)
            command.extend(["--previous-rbf", str(previous_path)])
        if previous_manifest is not None:
            previous_manifest_path = root / "previous-manifest.json"
            previous_manifest_path.write_text(json.dumps(previous_manifest), encoding="utf-8")
            command.extend(["--previous-manifest", str(previous_manifest_path)])
        result = subprocess.run(command, cwd=ROOT, text=True, capture_output=True)
        return result, output_path

    def _valid_previous_manifest(
        self,
        *,
        rbf: bytes = b"rbf-bytes\n",
        experiment: str = "010_blinky",
        source_hashes: dict[str, str] | None = None,
    ) -> dict:
        rbf_digest = hashlib.sha256(rbf).hexdigest()
        selected_sources = source_hashes or (MAILBOX_SOURCE_HASHES if experiment == "020_linux_mailbox" else SOURCE_HASHES)
        return {
            "lane": "oss",
            "experiment": experiment,
            "target": "5CSEBA6U23I7",
            "sources": [{"path": path, "sha256": digest} for path, digest in sorted(selected_sources.items())],
            "tool_pins": {
                "nextpnr": {"commit": NEXTPNR_COMMIT},
                "yosys": {"commit": YOSYS_COMMIT},
            },
            "artifacts": [{"path": f"build/oss/{experiment}/top.rbf", "sha256": rbf_digest}],
            "build": {
                "status": "pass",
                "build_status": "pass",
                "route_status": "pass",
                "route": {"status": "pass", "unrouted": False},
                "timing": {"status": "pass"},
                "hard_block_status": "pass",
                "authenticated_tools": {
                    "yosys": {
                        "commit": YOSYS_COMMIT,
                        "sha256": YOSYS_DIGEST,
                    },
                    "nextpnr-mistral": {
                        "commit": NEXTPNR_COMMIT,
                        "sha256": NEXTPNR_DIGEST,
                    },
                },
                "reproducibility": {"rbf_sha256": rbf_digest},
            },
        }

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
                "MISTRAL_BUF": {"used": 3, "available": 0},
                "MISTRAL_M10K": {"used": 0, "available": 553},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 0, "available": 1},
            },
        }
        previous = self._valid_previous_manifest()
        result, output = self._run_summary(
            timing,
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        summary = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(summary["status"], "pass")
        self.assertEqual(summary["route"]["status"], "pass")
        self.assertEqual(summary["timing"]["requested_mhz"], 50.0)
        self.assertEqual(summary["timing"]["achieved_mhz"], 234.5)
        self.assertEqual(summary["resources"]["MISTRAL_COMB"]["used"], 28)
        self.assertEqual(summary["resources"]["MISTRAL_BUF"]["used"], 3)
        self.assertNotIn("MISTRAL_BUF", summary["hard_blocks"])
        self.assertEqual(summary["hard_blocks"]["MISTRAL_M10K"]["used"], 0)
        self.assertEqual(summary["hard_block_status"], "pass")
        self.assertEqual(summary["authenticated_tools"]["yosys"]["commit"], "ec34fcf38986217af9b5558936044b7197d968a7")
        self.assertTrue(summary["reproducibility"]["rbf_stability_measured"])
        self.assertTrue(summary["reproducibility"]["rbf_stable"])

    def test_nonzero_forbidden_resource_fails_acceptance(self) -> None:
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {
                    "MISTRAL_COMB": {"used": 28, "available": 83820},
                    "MISTRAL_M10K": {"used": 1, "available": 553},
                },
            },
            "Info: Program finished normally.\n",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("forbidden", (result.stderr + result.stdout).lower())
        self.assertTrue(output.exists())
        self.assertEqual(json.loads(output.read_text(encoding="utf-8"))["hard_block_status"], "fail")

    def test_known_mistral_dsp_primitives_are_forbidden(self) -> None:
        timing = {
            "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
            "utilization": {
                "MISTRAL_COMB": {"used": 28, "available": 83820},
                "MISTRAL_MUL9X9": {"used": 1, "available": 8},
                "MISTRAL_MUL18X18": {"used": 0, "available": 4},
                "MISTRAL_MUL27X27": {"used": 0, "available": 2},
            },
        }
        result, output = self._run_summary(timing, "Info: Program finished normally.\n")
        self.assertNotEqual(result.returncode, 0)
        summary = json.loads(output.read_text(encoding="utf-8"))
        for name in ("MISTRAL_MUL9X9", "MISTRAL_MUL18X18", "MISTRAL_MUL27X27"):
            self.assertEqual(summary["resource_classes"][name], "forbidden")
            self.assertIn(name, summary["hard_blocks"])
        self.assertEqual(summary["hard_blocks"]["MISTRAL_MUL9X9"]["used"], 1)
        self.assertEqual(summary["hard_block_status"], "fail")

    def test_unknown_resource_fails_conservatively(self) -> None:
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {"MISTRAL_UNCLASSIFIED": {"used": 0, "available": 1}},
            },
            "Info: Program finished normally.\n",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unknown", (result.stderr + result.stdout).lower())
        self.assertTrue(output.exists())
        summary = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(summary["hard_block_status"], "fail")
        self.assertIn("MISTRAL_UNCLASSIFIED", summary["unknown_resources"])

    def test_stability_is_measured_for_matching_successful_provenance(self) -> None:
        previous = self._valid_previous_manifest()
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {},
            },
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        stability = json.loads(output.read_text(encoding="utf-8"))["reproducibility"]
        self.assertTrue(stability["rbf_stability_measured"])
        self.assertTrue(stability["rbf_stable"])
        self.assertIn("matched", stability["rbf_stability_reason"])

    def test_mailbox_summary_accepts_exact_hps_primitive_and_rejects_other_hard_blocks(self) -> None:
        timing = {
            "fmax": {"protocol.FPGA_CLK1_50": {"constraint": 50, "achieved": 130.8}},
            "utilization": {
                "MISTRAL_COMB": {"used": 76, "available": 83820},
                "MISTRAL_FF": {"used": 19, "available": 167640},
                "MISTRAL_IO": {"used": 1, "available": 472},
                "MISTRAL_CLKENA": {"used": 1, "available": 2},
                "MISTRAL_BUF": {"used": 8, "available": 0},
                "MISTRAL_M10K": {"used": 0, "available": 553},
                "cyclonev_oscillator": {"used": 0, "available": 1},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
            },
        }
        previous = self._valid_previous_manifest(experiment="020_linux_mailbox")
        result, output = self._run_summary(
            timing,
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
            experiment="020_linux_mailbox",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        summary = json.loads(output.read_text(encoding="utf-8"))
        self.assertEqual(summary["experiment"], "020_linux_mailbox")
        self.assertEqual(summary["hard_blocks"]["cyclonev_hps_interface_mpu_general_purpose"]["used"], 1)
        self.assertEqual(summary["hard_block_status"], "pass")

    def test_mailbox_summary_rejects_missing_or_extra_hps_usage(self) -> None:
        for resource_name, used, expected in (
            ("cyclonev_hps_interface_mpu_general_purpose", 0, "exactly 1"),
            ("MISTRAL_M10K", 1, "forbidden"),
        ):
            with self.subTest(resource_name=resource_name):
                result, output = self._run_summary(
                    {
                        "fmax": {"protocol.FPGA_CLK1_50": {"constraint": 50, "achieved": 130.8}},
                        "utilization": {
                            "MISTRAL_COMB": {"used": 1, "available": 83820},
                            "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                            resource_name: {"used": used, "available": 553 if resource_name == "MISTRAL_M10K" else 1},
                        },
                    },
                    "Info: Program finished normally.\n",
                    experiment="020_linux_mailbox",
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(expected, (result.stderr + result.stdout).lower())
                self.assertTrue(output.exists())

    def test_timing_rejects_adjacent_or_unrelated_clock_name_collisions(self) -> None:
        cases = (
            ("010_blinky", "FPGA_CLK1_500_FAKE"),
            ("010_blinky", "FPGA_CLK1_50_EXTRA"),
            ("010_blinky", "FPGA_CLK1_50X"),
            ("020_linux_mailbox", "protocol.FPGA_CLK1_500_FAKE"),
            ("020_linux_mailbox", "protocol.FPGA_CLK1_50_EXTRA"),
            ("020_linux_mailbox", "unrelated.FPGA_CLK1_50"),
        )
        for experiment, hostile_clock in cases:
            with self.subTest(experiment=experiment, hostile_clock=hostile_clock):
                utilization = {}
                if experiment == "020_linux_mailbox":
                    utilization = {
                        "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1},
                    }
                result, output = self._run_summary(
                    {
                        "fmax": {hostile_clock: {"constraint": 50, "achieved": 130.8}},
                        "utilization": utilization,
                    },
                    "Info: Program finished normally.\n",
                    experiment=experiment,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("exactly one intended clock", (result.stderr + result.stdout).lower())
                self.assertFalse(output.exists())

    def test_timing_accepts_real_mailbox_and_decorated_blinky_clock_names(self) -> None:
        for experiment, clock_name, utilization in (
            (
                "020_linux_mailbox",
                "protocol.FPGA_CLK1_50",
                {"cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1}},
            ),
            (
                "010_blinky",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
                {},
            ),
        ):
            with self.subTest(experiment=experiment, clock_name=clock_name):
                result, output = self._run_summary(
                    {
                        "fmax": {clock_name: {"constraint": 50, "achieved": 130.8}},
                        "utilization": utilization,
                    },
                    "Info: Program finished normally.\n",
                    experiment=experiment,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(output.read_text(encoding="utf-8"))["timing"]["clock"], clock_name)

    def test_stale_source_does_not_claim_stability(self) -> None:
        previous = self._valid_previous_manifest()
        stale_sources = dict(SOURCE_HASHES)
        stale_sources["experiments/010_blinky/rtl/top.v"] = "f" * 64
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {},
            },
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
            source_hashes=stale_sources,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        stability = json.loads(output.read_text(encoding="utf-8"))["reproducibility"]
        self.assertFalse(stability["rbf_stability_measured"])
        self.assertIsNone(stability["rbf_stable"])
        self.assertIn("source", stability["rbf_stability_reason"])

    def test_stale_tool_digest_does_not_claim_stability(self) -> None:
        previous = self._valid_previous_manifest()
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {},
            },
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
            tool_auth={"yosys": (YOSYS_COMMIT, "f" * 64)},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        stability = json.loads(output.read_text(encoding="utf-8"))["reproducibility"]
        self.assertFalse(stability["rbf_stability_measured"])
        self.assertIsNone(stability["rbf_stable"])
        self.assertIn("digest", stability["rbf_stability_reason"])

    def test_failed_previous_manifest_does_not_claim_stability(self) -> None:
        previous = self._valid_previous_manifest()
        previous["build"]["status"] = "fail"
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {},
            },
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        stability = json.loads(output.read_text(encoding="utf-8"))["reproducibility"]
        self.assertFalse(stability["rbf_stability_measured"])
        self.assertIsNone(stability["rbf_stable"])
        self.assertIn("successful", stability["rbf_stability_reason"])

    def test_mismatched_previous_experiment_does_not_claim_stability(self) -> None:
        previous = self._valid_previous_manifest()
        previous["experiment"] = "011_other"
        result, output = self._run_summary(
            {
                "fmax": {"FPGA_CLK1_50_MISTRAL": {"constraint": 50, "achieved": 234.5}},
                "utilization": {},
            },
            "Info: Program finished normally.\n",
            previous=b"rbf-bytes\n",
            previous_manifest=previous,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        stability = json.loads(output.read_text(encoding="utf-8"))["reproducibility"]
        self.assertFalse(stability["rbf_stability_measured"])
        self.assertIsNone(stability["rbf_stable"])
        self.assertIn("experiment", stability["rbf_stability_reason"])


if __name__ == "__main__":
    unittest.main()
