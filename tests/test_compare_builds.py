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
        self.oss_rbf = self.root / "oss.rbf"
        self.oracle_rbf = self.root / "oracle.rbf"
        self.oss_rbf.write_bytes(b"oss-rbf")
        self.oracle_rbf.write_bytes(b"oracle-rbf")

    def tearDown(self):
        self.temp.cleanup()

    def _manifest(self, lane, rbf, *, status="pass", alm=28, artifact=True):
        artifacts = []
        digest = _sha256(rbf) if rbf.exists() else "0" * 64
        if artifact:
            artifacts.append({"path": str(rbf), "sha256": digest})
        return {
            "schema": 2,
            "experiment": "010_blinky",
            "lane": lane,
            "target": "5CSEBA6U23I7",
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
                "hard_blocks": {},
                "hard_block_status": "pass",
                "unknown_resources": {},
                "simulation": {"status": "pass"},
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
        missing = self.root / "does-not-exist.rbf"
        value = self._manifest("oracle", missing, artifact=True)
        value["artifacts"][0]["sha256"] = "0" * 64
        value["build"]["resources"] = {}
        oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf))
        oracle = self._write_manifest("oracle.json", value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("missing" in item.lower() for item in comparison["failures"]))
        oracle_lane = comparison["lanes"]["oracle"]
        self.assertNotIn("ALM", oracle_lane["resources"])


if __name__ == "__main__":
    unittest.main()
