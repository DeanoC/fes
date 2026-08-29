import subprocess
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class RepositoryContractTests(unittest.TestCase):
    def test_help_lists_public_targets(self):
        result = subprocess.run(
            ["make", "help"], cwd=ROOT, text=True, capture_output=True, check=True
        )
        for target in (
            "toolchain",
            "doctor",
            "sim",
            "oss",
            "oracle",
            "compare",
            "program",
            "dev-load",
            "dev-preflight",
            "dev-fault-inject",
        ):
            self.assertIn(target, result.stdout)

    def test_fogcast_targets_dispatch_fixed_actions_with_environment_selectors(self):
        run_id = "0123456789abcdef0123456789abcdef"
        for target, action in (
            ("dev-load", "load"),
            ("dev-preflight", "preflight"),
            ("dev-fault-inject", "fault-inject"),
        ):
            with self.subTest(target=target):
                result = subprocess.run(
                    [
                        "make",
                        "-n",
                        target,
                        "EXP=020_linux_mailbox",
                        "BUILD=oss",
                        f"RUN_ID={run_id}",
                    ],
                    cwd=ROOT,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn(f"scripts/fogcast_dev.py {action}", result.stdout)
                self.assertIn('--experiment "$EXP"', result.stdout)
                self.assertIn('--build "$BUILD"', result.stdout)
                self.assertIn('--run-id "$RUN_ID"', result.stdout)

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
