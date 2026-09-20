import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "scripts/inputs.py"
sys.path.insert(0, str(SCRIPT.parent))

def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()

class InputsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        git(self.root, "init", "-q")
        (self.root / ".gitignore").write_text("/out/\n")
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

    def test_standalone_runtime_default_does_not_constrain_fes(self):
        child = self.root / "sources/FogCast"
        (child / "build/native-runtime.inputs.lock.toml").write_text("[mister_runtime]\ncommit = '" + "0" * 40 + "'\n")
        git(child, "add", ".")
        git(child, "commit", "-qm", "incompatible")
        git(self.root, "update-index", "--cacheinfo", "160000", git(child, "rev-parse", "HEAD"), "sources/FogCast")
        result = self.checker().validate(self.root)
        self.assertEqual(result["libmister-runtime"], git(self.root / "sources/libmister-runtime", "rev-parse", "HEAD"))

    def test_historical_profile_selects_matching_old_pair(self):
        old = self.checker().validate(self.root)
        runtime = self.root / "sources/libmister-runtime"
        git(runtime, "commit", "--allow-empty", "-qm", "new runtime")
        git(self.root, "update-index", "--cacheinfo", "160000",
            git(runtime, "rev-parse", "HEAD"), "sources/libmister-runtime")
        # The checkout now has the new runtime; historical validation must read
        # the old selected commit's lock rather than require current HEADs match it.
        self.assertEqual(self.checker().validate(self.root, {"sources": old}), old)
        result = self.checker().validate(self.root)
        self.assertEqual(result["libmister-runtime"], git(self.root / "sources/libmister-runtime", "rev-parse", "HEAD"))

    def test_assembly_lock_uses_selected_runtime_and_preserves_artifacts(self):
        raw = "format = 1\n[mister_runtime]\ncommit = '" + "1" * 40 + "'\nmount_path = '/runtime-source'\n[idle_rbf]\ncommit = '" + "2" * 40 + "'\n"
        selected = self.checker().selected_runtime_lock(raw, "3" * 40)
        self.assertIn("commit = '" + "3" * 40 + "'", selected)
        self.assertIn("commit = '" + "2" * 40 + "'", selected)
        self.assertIn("mount_path = '/runtime-source'", selected)
        with self.assertRaisesRegex(ValueError, "revision"):
            self.checker().selected_runtime_lock(raw, "invalid")

    def test_runtime_policy_needs_no_duplicate_commit(self):
        raw = "format = 1\n[mister_runtime]\nmount_path = '/runtime-source'\n"
        selected = self.checker().selected_runtime_lock(raw, "3" * 40)
        self.assertIn("commit = '" + "3" * 40 + "'", selected)

    def test_profile_rejects_missing_revision(self):
        with self.assertRaisesRegex(ValueError, "revision"):
            self.checker().validate(self.root, {"sources": {"FogCast": "0" * 40}})

if __name__ == "__main__":
    unittest.main()


class ModuleInputsTest(unittest.TestCase):
    def test_module_selection_uses_real_root_revision_and_relative_git_show(self):
        import inputs
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            git(root, 'init', '-q')
            git(root, 'config', 'user.name', 'Test')
            git(root, 'config', 'user.email', 'test@example.invalid')
            for name in inputs.COMPONENTS:
                module = root / 'sources' / name
                module.mkdir(parents=True)
                (module / 'README').write_text(name)
            (root / 'sources/FogCast/go.mod').write_text('module example.invalid/fogcast\n')
            git(root, 'add', '.')
            git(root, 'commit', '-qm', 'import modules')
            revision = git(root, 'rev-parse', 'HEAD')
            (root / 'notes').write_text('unrelated local work')
            self.assertEqual(inputs.validate(root), dict.fromkeys(inputs.COMPONENTS, revision))
            self.assertEqual(inputs.git(root / 'sources/FogCast', 'show', revision + ':go.mod'),
                             'module example.invalid/fogcast')
            (root / 'sources/FogCast/README').write_text('dirty module')
            with self.assertRaisesRegex(ValueError, 'dirty'):
                inputs.validate(root)
