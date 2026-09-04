import hashlib
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts import core_lock
from scripts import fetch_core


ROOT = Path(__file__).resolve().parents[1]
SELECT = ROOT / "scripts" / "select_core.py"


class SelectCoreTests(unittest.TestCase):
    def setUp(self):
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = Path(self.tempdir.name) / "repo"
        self.root.mkdir()

    def tearDown(self):
        self.tempdir.cleanup()

    def _git(self, args, cwd):
        subprocess.run(["git", *args], cwd=cwd, check=True, capture_output=True)

    def _pin_and_fetch(self, payload=b"upstream-rbf"):
        digest = hashlib.sha256(payload).hexdigest()
        upstream = Path(self.tempdir.name) / "upstream"
        upstream.mkdir()
        (upstream / "releases").mkdir()
        (upstream / "releases" / "core_20260603.rbf").write_bytes(payload)
        (upstream / "core.qpf").write_text("project\n", encoding="utf-8")
        self._git(["init"], upstream)
        self._git(["config", "user.email", "test@example.com"], upstream)
        self._git(["config", "user.name", "test"], upstream)
        self._git(["add", "."], upstream)
        self._git(["commit", "-m", "pin"], upstream)
        commit = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=upstream,
            text=True,
            capture_output=True,
            check=True,
        ).stdout.strip()
        pin = core_lock.CorePin(
            name="fixture",
            repo=str(upstream),
            commit=commit,
            rbf_path="releases/core_20260603.rbf",
            rbf_sha256=digest,
            rbf_size=len(payload),
            project="core.qpf",
            rationale="unit fixture",
        )
        lock = self.root / "cores.lock"
        lock.write_text(
            "\n".join(
                [
                    "[core.fixture]",
                    'repo = "https://example.invalid/fixture.git"',
                    f'commit = "{commit}"',
                    'rbf_path = "releases/core_20260603.rbf"',
                    f'rbf_sha256 = "{digest}"',
                    f"rbf_size = {len(payload)}",
                    'project = "core.qpf"',
                    'rationale = "unit fixture"',
                    "",
                ]
            ),
            encoding="utf-8",
        )
        fetch_core.fetch_core(pin, self.root)
        return pin, lock, payload

    def _run(self, *args):
        return subprocess.run(
            [sys.executable, str(SELECT), *args],
            cwd=ROOT,
            text=True,
            capture_output=True,
        )

    def test_select_upstream_copies_locked_rbf(self):
        pin, lock, payload = self._pin_and_fetch()
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            "--artifact",
            "upstream",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        dest = self.root / "build" / "current" / "fixture.rbf"
        self.assertEqual(dest.read_bytes(), payload)
        self.assertEqual(
            (self.root / "build" / "current" / "fixture.artifact").read_text(
                encoding="utf-8"
            ).strip(),
            "upstream",
        )
        self.assertIn("upstream", result.stdout)

    def test_select_rebuild_does_not_require_lock_hash(self):
        pin, lock, payload = self._pin_and_fetch()
        rebuilt = b"our-rebuild"
        rebuild_dir = self.root / "build" / "rebuild" / "fixture"
        rebuild_dir.mkdir(parents=True)
        (rebuild_dir / "fixture.rbf").write_bytes(rebuilt)
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            "--artifact",
            "rebuild",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        dest = self.root / "build" / "current" / "fixture.rbf"
        self.assertEqual(dest.read_bytes(), rebuilt)
        self.assertEqual(
            (self.root / "build" / "current" / "fixture.artifact").read_text(
                encoding="utf-8"
            ).strip(),
            "rebuild",
        )

    def test_default_artifact_is_rebuild(self):
        pin, lock, payload = self._pin_and_fetch()
        rebuilt = b"our-rebuild"
        rebuild_dir = self.root / "build" / "rebuild" / "fixture"
        rebuild_dir.mkdir(parents=True)
        (rebuild_dir / "fixture.rbf").write_bytes(rebuilt)
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        dest = self.root / "build" / "current" / "fixture.rbf"
        self.assertEqual(dest.read_bytes(), rebuilt)
        self.assertEqual(
            (self.root / "build" / "current" / "fixture.artifact").read_text(
                encoding="utf-8"
            ).strip(),
            "rebuild",
        )

    def test_select_rebuild_without_artifact_fails(self):
        pin, lock, payload = self._pin_and_fetch()
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            "--artifact",
            "rebuild",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rebuild-core", result.stderr)
        self.assertFalse((self.root / "build" / "current" / "fixture.rbf").exists())

    def test_select_upstream_rejects_tampered_rbf(self):
        pin, lock, payload = self._pin_and_fetch()
        fetched = self.root / "build" / "cores" / "fixture" / pin.rbf_path
        fetched.write_bytes(b"tampered")
        result = self._run(
            "--core",
            "fixture",
            "--lock",
            str(lock),
            "--root",
            str(self.root),
            "--artifact",
            "upstream",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not match lock", result.stderr)


if __name__ == "__main__":
    unittest.main()
