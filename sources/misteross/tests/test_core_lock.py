import hashlib
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts import core_lock
from scripts import fetch_core


ROOT = Path(__file__).resolve().parents[1]


class CoreLockTests(unittest.TestCase):
    def test_repository_lock_loads_nes_pin(self):
        pins = core_lock.load_lock(ROOT / "cores.lock")
        pin = pins["nes"]
        self.assertEqual(pin.repo, "https://github.com/MiSTer-devel/NES_MiSTer")
        self.assertEqual(pin.commit, "9a63821173b6da4d6e95dcbe2e2a322ec8171144")
        self.assertEqual(pin.rbf_path, "releases/NES_20260823.rbf")
        self.assertEqual(pin.rbf_sha256, "a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701")
        self.assertEqual(pin.rbf_size, 3282472)
        self.assertEqual(pin.project, "NES.qpf")

    def test_repository_lock_loads_megadrive_pin(self):
        pins = core_lock.load_lock(ROOT / "cores.lock")
        pin = pins["megadrive"]
        self.assertEqual(pin.repo, "https://github.com/MiSTer-devel/MegaDrive_MiSTer")
        self.assertEqual(pin.commit, "7365a137cfd8fa6f041e964d8b953159c0ec42d9")
        self.assertEqual(pin.rbf_path, "releases/MegaDrive_20260603.rbf")
        self.assertEqual(
            pin.rbf_sha256,
            "0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839",
        )
        self.assertEqual(pin.rbf_size, 4296864)
        self.assertEqual(pin.project, "MegaDrive.qpf")
        self.assertEqual(core_lock.validate_lock(ROOT / "cores.lock"), [])

    def test_check_lock_does_not_clone(self):
        result = subprocess.run(
            [
                sys.executable,
                str(ROOT / "scripts" / "fetch_core.py"),
                "--check-lock",
                "--core",
                "megadrive",
            ],
            cwd=ROOT,
            text=True,
            capture_output=True,
            check=True,
        )
        self.assertIn("7365a137cfd8fa6f041e964d8b953159c0ec42d9", result.stdout)

    def test_fetch_core_hashes_local_git_fixture(self):
        payload = b"fixture-rbf"
        digest = hashlib.sha256(payload).hexdigest()
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = Path(tmp)
            upstream = tmp_path / "upstream"
            upstream.mkdir()
            (upstream / "releases").mkdir()
            (upstream / "releases" / "core.rbf").write_bytes(payload)
            (upstream / "core.qpf").write_text("project\n")
            subprocess.run(["git", "init"], cwd=upstream, check=True, capture_output=True)
            subprocess.run(
                ["git", "config", "user.email", "test@example.com"],
                cwd=upstream,
                check=True,
                capture_output=True,
            )
            subprocess.run(
                ["git", "config", "user.name", "test"],
                cwd=upstream,
                check=True,
                capture_output=True,
            )
            subprocess.run(["git", "add", "."], cwd=upstream, check=True, capture_output=True)
            subprocess.run(
                ["git", "commit", "-m", "pin"],
                cwd=upstream,
                check=True,
                capture_output=True,
            )
            commit = subprocess.run(
                ["git", "rev-parse", "HEAD"],
                cwd=upstream,
                text=True,
                capture_output=True,
                check=True,
            ).stdout.strip()
            lock = tmp_path / "cores.lock"
            lock.write_text(
                "\n".join(
                    [
                        "[core.fixture]",
                        f'repo = "{upstream.as_uri()}"',
                        f'commit = "{commit}"',
                        'rbf_path = "releases/core.rbf"',
                        f'rbf_sha256 = "{digest}"',
                        f"rbf_size = {len(payload)}",
                        'project = "core.qpf"',
                        'rationale = "unit fixture"',
                        "",
                    ]
                )
            )
            # HTTPS-only lock validation; drive fetch_core with a constructed pin.
            pin = core_lock.CorePin(
                name="fixture",
                repo=str(upstream),
                commit=commit,
                rbf_path="releases/core.rbf",
                rbf_sha256=digest,
                rbf_size=len(payload),
                project="core.qpf",
                rationale="unit fixture",
            )
            dest = fetch_core.fetch_core(pin, tmp_path)
            self.assertTrue((dest / "releases" / "core.rbf").is_file())
            self.assertEqual(
                subprocess.run(
                    ["git", "rev-parse", "HEAD"],
                    cwd=dest,
                    text=True,
                    capture_output=True,
                    check=True,
                ).stdout.strip(),
                commit,
            )


if __name__ == "__main__":
    unittest.main()
