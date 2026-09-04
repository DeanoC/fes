import os
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
            "fetch-core",
            "rebuild-core",
            "select-core",
            "export-core-bundle",
            "program",
        ):
            self.assertIn(target, result.stdout)
        self.assertIn("ARTIFACT=rebuild", result.stdout)
        for removed in ("dev-bundle", "dev-load", "dev-preflight", "dev-fault-inject"):
            self.assertNotIn(removed, result.stdout)

    def test_make_select_core_uses_makefile_defaults(self):
        env = os.environ.copy()
        env.pop("CORE", None)
        env.pop("ARTIFACT", None)
        env["PYTHON"] = "printf '%s\\n'"
        result = subprocess.run(
            ["make", "select-core"],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
            check=True,
        )
        self.assertEqual(
            result.stdout.splitlines(),
            ["scripts/select_core.py", "--core", "megadrive", "--artifact", "rebuild"],
        )

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

    def test_documents_separate_compile_select_and_export_paths(self):
        for relative_path in ("README.md", "docs/architecture.md"):
            with self.subTest(document=relative_path):
                document = (ROOT / relative_path).read_text(encoding="utf-8")
                for command in (
                    "make fetch-core CORE=megadrive",
                    "make rebuild-core CORE=megadrive",
                    "make select-core CORE=megadrive",
                    "make export-core-bundle CORE=megadrive",
                ):
                    self.assertIn(command, document)

    def test_documents_the_immutable_fogcast_bundle_handoff(self):
        for relative_path in ("README.md", "docs/architecture.md"):
            with self.subTest(document=relative_path):
                document = (ROOT / relative_path).read_text(encoding="utf-8")
                self.assertIn("megadrive.rbf", document)
                self.assertIn("megadrive-rbf.toml", document)
                self.assertIn("build/bundles/megadrive/<rbf-sha256>/", document)
                self.assertIn("printed bundle path", document)
                self.assertIn("build/current/megadrive.rbf", document)


if __name__ == "__main__":
    unittest.main()
