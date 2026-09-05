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
