import importlib.util
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"

class ReceiptTest(unittest.TestCase):
    def test_media_sources_do_not_invalidate_cold_build_fingerprint(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        revisions = {"FogCast": "a" * 40}
        profile = {"version": "test"}
        before, _ = build.build_fingerprint(revisions, profile, "go test")
        with mock.patch.object(build, "MEDIA_RECIPE_FILES", (Path("changed-media.py"),)):
            after, _ = build.build_fingerprint(revisions, profile, "go test")
        self.assertEqual(before, after)

    def test_verified_image_rejects_missing_or_mismatched_evidence(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            (output / "linux.img").write_bytes(b"cold image")
            (output / "reproducibility.txt").write_text("run_1_sha256=wrong\nrun_2_sha256=wrong\n")
            build.write_receipt(output, "image", "cold-fingerprint",
                                ["linux.img", "reproducibility.txt"])
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_republish_read_only_selection(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            source = root / "selection"
            dest = root / "published"
            source.write_bytes(b"first")
            source.chmod(0o444)
            build.publish_file(source, dest)
            source.unlink()
            source.write_bytes(b"second")
            source.chmod(0o444)
            build.publish_file(source, dest)
            self.assertEqual(dest.read_bytes(), b"second")
            self.assertEqual(dest.stat().st_mode & 0o777, 0o444)

    def test_changed_inputs_and_corrupt_outputs_are_not_reused(self):
        self.assertTrue((SCRIPTS / "build.py").exists(), "build receipt is not implemented")
        sys.path.insert(0, str(SCRIPTS))
        spec = importlib.util.spec_from_file_location("parent_build", SCRIPTS / "build.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / "linux.img").write_bytes(b"image")
            module.write_receipt(root, "image", "inputs-a", ["linux.img"])
            self.assertTrue(module.reusable(root, "image", "inputs-a"))
            self.assertFalse(module.reusable(root, "image", "inputs-b"))
            (root / "linux.img").write_bytes(b"corrupt")
            self.assertFalse(module.reusable(root, "image", "inputs-a"))
            (root / "linux.img").unlink()
            self.assertFalse(module.reusable(root, "image", "inputs-a"))
