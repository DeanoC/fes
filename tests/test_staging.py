import importlib.util
from pathlib import Path
import sys
import unittest
import test_inputs

git = test_inputs.git

class StagingTest(unittest.TestCase):
    setUp = test_inputs.InputsTest.setUp
    def test_staging_has_standalone_git_metadata_and_rejects_edits(self):
        scripts = Path(__file__).resolve().parents[1] / "scripts"
        sys.path.insert(0, str(scripts))
        spec = importlib.util.spec_from_file_location("staging_build", scripts / "build.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        module.ROOT = self.root
        self.assertTrue(hasattr(module, "source_checkout"), "standalone component staging is missing")
        revision = git(self.root / "sources/FogCast", "rev-parse", "HEAD")
        staged = module.source_checkout("FogCast", revision)
        self.assertTrue((staged / ".git").is_dir())
        self.assertEqual(git(staged, "rev-parse", "HEAD"), revision)
        self.assertEqual(git(staged, "status", "--porcelain"), "")
        (staged / "README").write_text("changed")
        with self.assertRaisesRegex(ValueError, "changed"):
            module.source_checkout("FogCast", revision)

    def test_profile_specific_output_volume_is_distinct(self):
        scripts = Path(__file__).resolve().parents[1] / "scripts"
        sys.path.insert(0, str(scripts))
        spec = importlib.util.spec_from_file_location("volume_build", scripts / "build.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertNotEqual(
            module.output_volume(Path("/repo"), "native-dev"),
            module.output_volume(Path("/repo"), "native-source-dev"),
        )

    def test_staging_restores_only_declared_generated_overlay(self):
        scripts = Path(__file__).resolve().parents[1] / "scripts"
        sys.path.insert(0, str(scripts))
        spec = importlib.util.spec_from_file_location("restore_build", scripts / "build.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        module.ROOT = self.root
        child = self.root / "sources/FogCast"
        overlay = "build/native-runtime.inputs.lock.toml"
        revision = git(child, "rev-parse", "HEAD")
        staged = module.source_checkout("FogCast", revision, "-source", (overlay,))
        expected = (staged / overlay).read_bytes()
        (staged / overlay).write_text("generated overlay")
        same = module.source_checkout("FogCast", revision, "-source", (overlay,))
        self.assertEqual((same / overlay).read_bytes(), expected)
        (staged / "README").write_text("unexpected")
        with self.assertRaisesRegex(ValueError, "changed"):
            module.source_checkout("FogCast", revision, "-source", (overlay,))
