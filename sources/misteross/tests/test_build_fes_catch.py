from pathlib import Path
import json
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from scripts import build_fes_catch as catch
from scripts import build_fes_demo as demo
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]

class CatchProducerTests(unittest.TestCase):
    def test_cli_accepts_standard_package_resolver_arguments(self):
        with patch("sys.argv", ["build_fes_catch.py", "--root", str(ROOT),
                "--package-output", str(ROOT / "build/packages"), "--cache-root", "/tmp/cache",
                "--identity-version", "2"]), patch.object(catch, "build", return_value=Path("sealed")) as build:
            self.assertEqual(catch.main(), 0)
        build.assert_called_once_with(ROOT, ROOT / "build/packages", cache_root=Path("/tmp/cache"), identity_version=2, gpu_device=0)

    def test_real_source_identity_and_generic_interfaces(self):
        # A monorepo-shaped clean source exercises the same closure as core-dev.
        # Git objects can still be non-empty while TemporaryDirectory rmtree runs
        # (CI overlay); drop .git with ignore_errors before the context exits.
        temporary = tempfile.TemporaryDirectory(ignore_cleanup_errors=True)
        self.addCleanup(temporary.cleanup)
        repo = Path(temporary.name)
        root = repo / "sources/misteross"
        for relative in ("scripts", "boards", "cores/fes-demo", "cores/fes-common", "cores/fes-pong"):
            shutil.copytree(ROOT / relative, root / relative, ignore=shutil.ignore_patterns("__pycache__"))
        shutil.copy(ROOT / "toolchain.lock", root / "toolchain.lock")
        def git(*args):
            return subprocess.check_output(["git", "-C", str(repo), *args], text=True).strip()
        git("init", "-q"); git("config", "user.name", "Test"); git("config", "user.email", "test@example.invalid")
        git("add", "."); git("commit", "-qm", "source")
        self.addCleanup(shutil.rmtree, repo / ".git", ignore_errors=True)
        execution = {"gpu_device": 0, "version": 1, "environment": {"LANG": "C"}}
        def record():
            return catch.create_build_record(root, "https://example.invalid/source", git("rev-parse", "HEAD"),
                {"yosys": "test"}, execution=execution)
        before = record()
        fields = json.loads(before)
        self.assertEqual(fields["format"], 2)
        self.assertEqual(fields["source_path"], "sources/misteross")
        self.assertTrue(set(catch.GAME_SOURCES) <= fields["source_inputs"].keys())
        (repo / "README.md").write_text("Unrelated docs")
        git("add", "."); git("commit", "-qm", "docs")
        self.assertEqual(build_identity(before), build_identity(record()))
        path = root / catch.GAME_SOURCES[0]
        path.write_text(path.read_text() + "\n")
        self.assertNotEqual(build_identity(before), build_identity(record()))
        manifest = tomllib.loads(catch.manifest(before, {"rbf": {"size": 4, "sha256": "b"*64}},
            "https://example.invalid/source", fields["revision"], {"yosys": "test"}).decode())
        self.assertEqual(manifest["core"]["id"], "fes.catch")
        self.assertEqual(manifest["abi"], {"id": "fes.application", "major": 1, "minor": 0})
        self.assertEqual({i["id"] for i in manifest["interfaces"]},
            {"fes.gamepad", "fes.video.fixed-720p60", "fes.audio.pcm-s16-stereo-48k"})
        self.assertNotIn("media", manifest)

    def test_commands_use_shared_shell_and_isolated_output(self):
        synth, route = demo.build_commands(ROOT, "a"*32,
            {"yosys": Path("/auth/yosys"), "nextpnr-mistral": Path("/auth/nextpnr")}, audio=True, catch=True)
        for path in catch.GAME_SOURCES:
            self.assertIn(path, synth[2])
        self.assertIn("-D FES_CATCH", synth[2])
        self.assertNotIn("fes_demo_audio.v", synth[2])
        self.assertIn("-set ENABLE_GAMEPAD 1 -set ENABLE_MEDIA 0", synth[2])
        self.assertIn("build/fes-catch/core.rbf", route)
        self.assertIn(demo.AUDIO_QSF, route)

if __name__ == "__main__":
    unittest.main()
