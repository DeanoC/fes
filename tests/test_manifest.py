from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COLLECTOR = ROOT / "scripts" / "collect_manifest.py"
RUN_LOGGED = ROOT / "scripts" / "run_logged.sh"
TARGET = "5CSEBA6U23I7"


class ManifestTests(unittest.TestCase):
    def setUp(self) -> None:
        (ROOT / "build").mkdir(exist_ok=True)
        self.fixture = Path(tempfile.mkdtemp(prefix="manifest-test-", dir=ROOT / "build"))
        self.output = self.fixture / "build" / "oss" / "010_blinky"
        self.output.mkdir(parents=True)
        self.source = self.fixture / "sources" / "top.v"
        self.source.parent.mkdir(parents=True)
        self.source.write_text("module top; endmodule\n", encoding="utf-8")
        self.artifact = self.output / "top.rbf"
        self.artifact.write_bytes(b"fixture-rbf\n")
        self.log = self.output / "yosys.log"
        self.log.write_text("command: yosys -p 'stat'\nlog output\n", encoding="utf-8")

    def tearDown(self) -> None:
        shutil.rmtree(self.fixture, ignore_errors=True)

    def _collect(self, *, source: Path | None = None, artifact: Path | None = None) -> subprocess.CompletedProcess[str]:
        return self._collect_with(
            source=source,
            artifact=artifact,
        )

    def _collect_with(
        self,
        *,
        source: Path | None = None,
        artifact: Path | None = None,
        manifest: Path | None = None,
        repo_root: Path = ROOT,
        build_root: Path = ROOT / "build",
        output: Path | None = None,
        command_log: Path | None = None,
    ) -> subprocess.CompletedProcess[str]:
        output = output or self.output
        command_log = command_log or self.log
        command = [
            sys.executable,
            str(COLLECTOR),
            "--repo-root",
            str(repo_root),
            "--build-root",
            str(build_root),
            "--output-dir",
            str(output),
            "--experiment",
            "010_blinky",
            "--lane",
            "oss",
            "--target",
            TARGET,
            "--source",
            str(source or self.source),
            "--command-log",
            str(command_log),
            "--artifact",
            str(artifact or self.artifact),
        ]
        if manifest is not None:
            command.extend(["--manifest", str(manifest)])
        return subprocess.run(
            command,
            cwd=ROOT,
            env={**os.environ, "SOURCE_DATE_EPOCH": "1787702400"},
            text=True,
            capture_output=True,
        )

    def test_repeated_generation_is_byte_identical(self) -> None:
        first = self._collect()
        self.assertEqual(first.returncode, 0, first.stderr)
        manifest = self.output / "manifest.json"
        first_bytes = manifest.read_bytes()

        second = self._collect()
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(first_bytes, manifest.read_bytes())

    def test_manifest_records_hashes_metadata_and_lock_pins(self) -> None:
        result = self._collect()
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))

        self.assertEqual(manifest["target"], TARGET)
        self.assertEqual(manifest["experiment"], "010_blinky")
        self.assertEqual(manifest["lane"], "oss")
        self.assertRegex(manifest["timestamp"], r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")
        self.assertIsInstance(manifest["host"], dict)
        self.assertIsInstance(manifest["git"], dict)
        self.assertIn("dirty", manifest["git"])
        self.assertIn("state", manifest["git"])
        self.assertIsInstance(manifest["commands"], list)
        self.assertTrue(manifest["commands"])

        for entry in [*manifest["sources"], *manifest["artifacts"]]:
            self.assertRegex(entry["sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(
            manifest["sources"][0]["sha256"],
            hashlib.sha256(self.source.read_bytes()).hexdigest(),
        )
        self.assertEqual(
            manifest["artifacts"][0]["sha256"],
            hashlib.sha256(self.artifact.read_bytes()).hexdigest(),
        )

        from scripts import lockfile

        pins = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertEqual(set(manifest["tool_pins"]), set(pins))
        for name, pin in pins.items():
            self.assertEqual(manifest["tool_pins"][name]["commit"], pin.commit)
            self.assertEqual(manifest["tool_pins"][name]["repo"], pin.repo)

    def test_missing_artifact_is_refused(self) -> None:
        missing = self.output / "does-not-exist.rbf"
        result = self._collect(artifact=missing)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing", (result.stderr + result.stdout).lower())

    def test_artifact_outside_build_root_is_refused(self) -> None:
        outside = self.fixture.parent.parent.parent / "manifest-outside-artifact.rbf"
        outside.write_bytes(b"outside\n")
        try:
            result = self._collect(artifact=outside)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("outside", (result.stderr + result.stdout).lower())
        finally:
            outside.unlink(missing_ok=True)

    def test_artifact_directory_is_refused(self) -> None:
        artifact_directory = self.output / "artifact-directory"
        artifact_directory.mkdir()
        result = self._collect(artifact=artifact_directory)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("regular file", (result.stderr + result.stdout).lower())

    def test_symlink_outside_root_is_refused(self) -> None:
        outside = self.fixture / "outside.v"
        outside.write_text("module outside; endmodule\n", encoding="utf-8")
        link = self.fixture / "sources" / "escape.v"
        link.symlink_to(outside)
        result = self._collect(source=link)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", (result.stderr + result.stdout).lower())

    def test_manifest_cannot_overwrite_repository_file(self) -> None:
        with tempfile.TemporaryDirectory(prefix="manifest-repository-") as temporary:
            repository = Path(temporary)
            shutil.copy2(ROOT / "toolchain.lock", repository / "toolchain.lock")
            scripts = repository / "scripts"
            scripts.mkdir()
            shutil.copy2(ROOT / "scripts" / "lockfile.py", scripts / "lockfile.py")
            build_root = repository / "build"
            output = build_root / "oss" / "010_blinky"
            output.mkdir(parents=True)
            source = repository / "source.v"
            source.write_text("module source; endmodule\n", encoding="utf-8")
            artifact = output / "top.rbf"
            artifact.write_bytes(b"fixture\n")
            command_log = output / "build.log"
            command_log.write_text("command: trusted-tool\n", encoding="utf-8")
            protected = repository / "toolchain.lock"
            original = protected.read_bytes()

            result = self._collect_with(
                source=source,
                artifact=artifact,
                manifest=protected,
                repo_root=repository,
                build_root=build_root,
                output=output,
                command_log=command_log,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(protected.read_bytes(), original)
            self.assertIn("manifest", (result.stderr + result.stdout).lower())

    def test_commands_use_only_first_runner_header(self) -> None:
        self.log.write_text(
            "command: trusted-tool --flag\n"
            "tool output\n"
            "command: forged-tool --dangerous\n",
            encoding="utf-8",
        )
        result = self._collect()
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["commands"], ["command: trusted-tool --flag"])

    def test_missing_declared_build_root_is_created(self) -> None:
        build_root = self.fixture / "new-build-root"
        output = build_root / "oss" / "010_blinky"
        result = subprocess.run(
            [
                sys.executable,
                str(COLLECTOR),
                "--repo-root",
                str(ROOT),
                "--build-root",
                str(build_root),
                "--output-dir",
                str(output),
                "--experiment",
                "010_blinky",
                "--lane",
                "oss",
                "--target",
                TARGET,
            ],
            cwd=ROOT,
            env={**os.environ, "SOURCE_DATE_EPOCH": "1787702400"},
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((output / "manifest.json").is_file())


class LoggedCommandTests(unittest.TestCase):
    def test_failure_preserves_output_status_and_reports_log(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log = Path(temporary) / "nested" / "failure.log"
            result = subprocess.run(
                [
                    str(RUN_LOGGED),
                    str(log),
                    "bash",
                    "-c",
                    "printf 'stdout line\\n'; printf 'stderr line\\n' >&2; exit 7",
                ],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 7)
            self.assertTrue(log.is_file())
            contents = log.read_text(encoding="utf-8")
            self.assertIn("command:", contents)
            self.assertIn("stdout line", contents)
            self.assertIn("stderr line", contents)
            self.assertIn("stdout line", result.stdout)
            self.assertIn("stderr line", result.stdout)
            self.assertIn("failed command", result.stderr)
            self.assertIn(str(log), result.stderr)

    def test_log_write_failure_is_failure_when_command_succeeds(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log_directory = Path(temporary) / "not-a-log-file"
            log_directory.mkdir()
            result = subprocess.run(
                [str(RUN_LOGGED), str(log_directory), "bash", "-c", "printf 'success\\n'"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("failed command", result.stderr)
            self.assertIn(str(log_directory), result.stderr)

    def test_command_failure_status_wins_over_log_write_failure(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log_directory = Path(temporary) / "not-a-log-file"
            log_directory.mkdir()
            result = subprocess.run(
                [str(RUN_LOGGED), str(log_directory), "bash", "-c", "exit 7"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 7)

    def test_command_header_escapes_arguments_on_one_line(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log = Path(temporary) / "header.log"
            argument = "argument with spaces\nand a newline"
            result = subprocess.run(
                [
                    str(RUN_LOGGED),
                    str(log),
                    "bash",
                    "-c",
                    "printf '%s\\n' \"$1\"",
                    "bash",
                    argument,
                ],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            header = log.read_text(encoding="utf-8").splitlines()[0]
            self.assertTrue(header.startswith("command:"))
            self.assertEqual(header.count("\n"), 0)
            self.assertIn("$'argument with spaces\\nand a newline'", header)
            self.assertIn("argument with spaces\n", result.stdout)

    def test_failure_summary_escapes_log_path_on_one_line(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log = Path(temporary) / "log\nwith-newline.txt"
            result = subprocess.run(
                [str(RUN_LOGGED), str(log), "bash", "-c", "exit 9"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 9)
            self.assertEqual(len(result.stderr.splitlines()), 1)
            self.assertIn("failed command", result.stderr)
            self.assertIn("$'", result.stderr)


if __name__ == "__main__":
    unittest.main()
