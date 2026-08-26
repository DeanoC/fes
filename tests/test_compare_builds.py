import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COMPARE = ROOT / "scripts" / "compare_builds.py"


def _sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class CompareBuildsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.output = self.root / "comparison"
        self.oss_rbf = self.root / "build" / "oss" / "010_blinky" / "top.rbf"
        self.oracle_rbf = self.root / "build" / "oracle" / "010_blinky" / "top.rbf"
        self.oss_rbf.parent.mkdir(parents=True)
        self.oracle_rbf.parent.mkdir(parents=True)
        self.oss_rbf.write_bytes(b"oss-rbf")
        self.oracle_rbf.write_bytes(b"oracle-rbf")

    def tearDown(self):
        self.temp.cleanup()

    def _manifest(self, lane, rbf, *, status="pass", alm=28, artifact=True, artifact_path=None):
        artifacts = []
        digest = _sha256(rbf) if rbf.exists() else "0" * 64
        if artifact:
            artifacts.append(
                {
                    "path": artifact_path or f"build/{lane}/010_blinky/top.rbf",
                    "sha256": digest,
                }
            )
        sources = [
            {"path": "experiments/010_blinky/rtl/top.v", "sha256": "a" * 64},
            {"path": "boards/de10nano/pins.qsf", "sha256": "b" * 64},
            {"path": "boards/de10nano/clocks.sdc", "sha256": "c" * 64},
        ]
        hard_blocks = {
            "PLL": {"used": 0, "available": 4, "evidence_kind": "fitter_summary", "measured": True},
            "BRAM/M10K": {"used": 0, "available": 10, "evidence_kind": "fitter_summary", "measured": True},
            "MLAB/LUTRAM": {
                "used": 0,
                "available": None,
                "evidence_kind": "static_exclusion",
                "measured": False,
                "exclusion": {
                    "basis": "static source/project exclusion",
                    "patterns": ["mlab", "lutram"],
                    "sources": [{"path": "experiments/010_blinky/rtl/top.v", "sha256": "a" * 64}],
                },
            },
            "DSP": {"used": 0, "available": 2, "evidence_kind": "fitter_summary", "measured": True},
            "HPS": {
                "used": 0,
                "available": None,
                "evidence_kind": "static_exclusion",
                "measured": False,
                "exclusion": {
                    "basis": "static source/project exclusion",
                    "patterns": ["hps", "hard processor"],
                    "sources": [{"path": "experiments/010_blinky/oracle/top.qsf", "sha256": "a" * 64}],
                },
            },
        }
        hard_block_evidence = json.loads(json.dumps(hard_blocks))
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        return {
            "schema": 2,
            "experiment": "010_blinky",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": sources,
            "artifacts": artifacts,
            "build": {
                "status": status,
                "build_status": status,
                "route_status": "pass",
                "route": {"status": "pass", "unrouted": False},
                "timing": {
                    "status": "pass",
                    "requested_mhz": 50.0,
                    "achieved_mhz": 100.0,
                    "clock": "FPGA_CLK1_50",
                },
                "resources": {
                    "ALM": {"used": alm, "available": 100, "utilization_percent": alm}
                },
                "source_hashes": {
                    source["path"]: source["sha256"] for source in sources
                },
                "hard_blocks": hard_blocks,
                "hard_block_evidence": hard_block_evidence,
                "hard_block_status": "pass",
                "unknown_resources": {},
                "simulation": {"status": "pass"},
                "authenticated_tools": {"quartus_sh": provenance},
                "tool_pins": {"quartus": provenance},
                "reproducibility": {
                    "rbf_sha256": digest,
                    "rbf_size_bytes": rbf.stat().st_size if rbf.exists() else 0,
                },
            },
        }

    def _write_manifest(self, name, value):
        path = self.root / name
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def _run(self, oss, oracle):
        env = os.environ.copy()
        return subprocess.run(
            [
                str(COMPARE),
                "--oss-manifest",
                str(oss),
                "--oracle-manifest",
                str(oracle),
                "--output-dir",
                str(self.output),
            ],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
        )

    def test_resource_and_rbf_differences_are_informational(self):
        oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf, alm=28))
        oracle = self._write_manifest(
            "oracle.json", self._manifest("oracle", self.oracle_rbf, alm=31)
        )

        result = self._run(oss, oracle)

        self.assertEqual(result.returncode, 0, result.stderr)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass")
        self.assertTrue(comparison["differences"])
        self.assertIn("ALM", (self.output / "comparison.md").read_text())
        self.assertIn("informational", (self.output / "comparison.md").read_text().lower())

    def test_failed_oracle_build_is_nonzero(self):
        oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf))
        oracle = self._write_manifest(
            "oracle.json",
            self._manifest("oracle", self.oracle_rbf, status="fail"),
        )

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "fail")
        self.assertTrue(any("build" in item.lower() for item in comparison["failures"]))

    def test_missing_artifact_is_failure_and_not_zero_resource(self):
        oracle_bytes = self.oracle_rbf.read_bytes()
        self.oracle_rbf.unlink()
        try:
            value = self._manifest("oracle", self.oracle_rbf, artifact=True)
            value["build"]["resources"] = {}
            oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf))
            oracle = self._write_manifest("oracle.json", value)

            result = self._run(oss, oracle)
        finally:
            self.oracle_rbf.write_bytes(oracle_bytes)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("missing" in item.lower() for item in comparison["failures"]))
        oracle_lane = comparison["lanes"]["oracle"]
        self.assertNotIn("ALM", oracle_lane["resources"])

    def test_common_source_hashes_must_be_present_and_identical(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        oracle_value["sources"] = [
            source
            for source in oracle_value["sources"]
            if source["path"] != "boards/de10nano/pins.qsf"
        ]
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("common" in item.lower() or "source" in item.lower() for item in comparison["failures"]))

    def test_build_summary_source_hashes_must_match_manifest_sources(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        oracle_value["build"]["source_hashes"]["boards/de10nano/clocks.sdc"] = "d" * 64
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("build summary" in item.lower() for item in comparison["failures"]))

    def test_hard_block_map_must_be_complete_and_well_formed(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        del oracle_value["build"]["hard_blocks"]["DSP"]
        del oracle_value["build"]["hard_block_evidence"]["DSP"]
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("hard" in item.lower() for item in comparison["failures"]))

    def test_rbf_must_use_exact_lane_path_and_match_summary(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        oracle_value["artifacts"][0]["path"] = "build/oss/010_blinky/top.rbf"
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("rbf" in item.lower() for item in comparison["failures"]))

    def test_oracle_hard_block_evidence_kind_and_completeness_are_required(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        del oracle_value["build"]["hard_block_evidence"]["MLAB/LUTRAM"]["exclusion"]
        oracle_value["build"]["hard_block_evidence"]["MLAB/LUTRAM"]["evidence_kind"] = "fitter_summary"
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("evidence" in item.lower() for item in comparison["failures"]))

    def test_oracle_quartus_provenance_is_required_and_exact(self):
        for mutation in ("missing", "wrong-version"):
            oss_value = self._manifest("oss", self.oss_rbf)
            oracle_value = self._manifest("oracle", self.oracle_rbf)
            if mutation == "missing":
                del oracle_value["build"]["authenticated_tools"]
            else:
                oracle_value["build"]["authenticated_tools"]["quartus_sh"]["version"] = "Quartus Prime Version 17.0.0 Build 595"
            oss = self._write_manifest("oss.json", oss_value)
            oracle = self._write_manifest("oracle.json", oracle_value)

            result = self._run(oss, oracle)

            self.assertNotEqual(result.returncode, 0)
            comparison = json.loads((self.output / "comparison.json").read_text())
            self.assertTrue(any("provenance" in item.lower() or "quartus" in item.lower() for item in comparison["failures"]))


if __name__ == "__main__":
    unittest.main()
