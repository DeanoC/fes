import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class RepositoryContractTests(unittest.TestCase):
    def test_help_lists_public_targets(self):
        result = subprocess.run(
            ["make", "help"], cwd=ROOT, text=True, capture_output=True, check=True
        )
        for target in ("toolchain", "doctor", "sim", "oss", "oracle", "compare", "program"):
            self.assertIn(target, result.stdout)

    def test_unknown_experiment_is_rejected(self):
        result = subprocess.run(
            ["make", "sim", "EXP=../../tmp"], cwd=ROOT, text=True, capture_output=True
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid EXP", result.stderr + result.stdout)

    def test_oss_rejects_format_valid_unknown_experiment(self):
        result = subprocess.run(
            ["make", "oss", "EXP=999_not_implemented"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unknown experiment", (result.stderr + result.stdout).lower())

    def test_generated_directories_are_ignored(self):
        result = subprocess.run(
            ["git", "check-ignore", "build/oss/010_blinky/top.rbf"],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
