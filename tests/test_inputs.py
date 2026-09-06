import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/inputs.py"

def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()

class InputsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        git(self.root, "init", "-q")
        for name in ("FogCast", "libmister-runtime", "misteross", "mister-packages"):
            child = self.root / "sources" / name
            child.mkdir(parents=True)
            git(child, "init", "-q")
            git(child, "config", "user.name", "Test")
            git(child, "config", "user.email", "test@example.invalid")
            (child / "README").write_text("fixture\n")
            git(child, "add", ".")
            git(child, "commit", "-qm", "fixture")
        runtime = self.root / "sources/libmister-runtime"
        fogcast = self.root / "sources/FogCast"
        (fogcast / "build").mkdir()
        (fogcast / "build/native-runtime.inputs.lock.toml").write_text(
            "[mister_runtime]\ncommit = '" + git(runtime, "rev-parse", "HEAD") + "'\n")
        git(fogcast, "add", ".")
        git(fogcast, "commit", "-qm", "lock")
        for name in ("FogCast", "libmister-runtime", "misteross", "mister-packages"):
            sha = git(self.root / "sources" / name, "rev-parse", "HEAD")
            git(self.root, "update-index", "--add", "--cacheinfo", "160000", sha, "sources/" + name)

    def checker(self):
        self.assertTrue(SCRIPT.exists(), "source pin checker is not implemented")
        spec = importlib.util.spec_from_file_location("inputs", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_clean_pinned_pair(self):
        result = self.checker().validate(self.root)
        self.assertEqual(result["libmister-runtime"], git(self.root / "sources/libmister-runtime", "rev-parse", "HEAD"))

    def test_untracked_source_is_rejected(self):
        (self.root / "sources/libmister-runtime/extra.cpp").write_text("changed")
        with self.assertRaisesRegex(ValueError, "dirty"):
            self.checker().validate(self.root)

    def test_moved_head_is_rejected(self):
        child = self.root / "sources/libmister-runtime"
        git(child, "commit", "--allow-empty", "-qm", "moved")
        with self.assertRaisesRegex(ValueError, "pin"):
            self.checker().validate(self.root)

    def test_incompatible_lock_is_rejected(self):
        child = self.root / "sources/FogCast"
        (child / "build/native-runtime.inputs.lock.toml").write_text("[mister_runtime]\ncommit = '" + "0" * 40 + "'\n")
        git(child, "add", ".")
        git(child, "commit", "-qm", "incompatible")
        git(self.root, "update-index", "--cacheinfo", "160000", git(child, "rev-parse", "HEAD"), "sources/FogCast")
        with self.assertRaisesRegex(ValueError, "lock"):
            self.checker().validate(self.root)

    def test_historical_profile_selects_matching_old_pair(self):
        old = self.checker().validate(self.root)
        runtime = self.root / "sources/libmister-runtime"
        git(runtime, "commit", "--allow-empty", "-qm", "new runtime")
        git(self.root, "update-index", "--cacheinfo", "160000",
            git(runtime, "rev-parse", "HEAD"), "sources/libmister-runtime")
        # The checkout now has the new runtime; historical validation must read
        # the old selected commit's lock rather than require current HEADs match it.
        self.assertEqual(self.checker().validate(self.root, {"sources": old}), old)
        with self.assertRaisesRegex(ValueError, "lock"):
            self.checker().validate(self.root)

    def test_profile_rejects_missing_revision(self):
        with self.assertRaisesRegex(ValueError, "revision"):
            self.checker().validate(self.root, {"sources": {"FogCast": "0" * 40}})

if __name__ == "__main__":
    unittest.main()
