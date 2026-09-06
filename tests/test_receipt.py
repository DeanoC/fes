import importlib.util
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1] / "scripts"

class ReceiptTest(unittest.TestCase):
    def make_verified_output(self, build, output):
        image = b"cold image"
        qemu_log = b"qemu passed\n"
        image_sha256 = hashlib.sha256(image).hexdigest()
        qemu_log_sha256 = hashlib.sha256(qemu_log).hexdigest()
        (output / "linux.img").write_bytes(image)
        (output / "qemu-smoke.log").write_bytes(qemu_log)
        (output / "reproducibility.txt").write_text(
            f"run_1_sha256={image_sha256}\nrun_2_sha256={image_sha256}\n")
        (output / "verification.json").write_text(json.dumps({
            "image_sha256": image_sha256,
            "qemu_log_sha256": qemu_log_sha256,
            "qemu_packaging": "pass",
            "structural": "pass",
            "two_pass_reproducibility": "pass",
        }))
        build.write_receipt(output, "image", "cold-fingerprint",
                            ["linux.img", "reproducibility.txt"])
        return image_sha256, qemu_log_sha256

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

    def test_verified_image_rejects_receipt_without_linux_image(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            build.write_receipt(output, "image", "cold-fingerprint", ["reproducibility.txt"])
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_verified_image_rejects_missing_qemu_log(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            (output / "qemu-smoke.log").unlink()
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_verified_image_rejects_mismatched_qemu_log_digest(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            self.make_verified_output(build, output)
            (output / "qemu-smoke.log").write_bytes(b"qemu changed\n")
            with self.assertRaisesRegex(ValueError, "run make verify"):
                build.load_verified_image(output, "cold-fingerprint")

    def test_verified_image_returns_bound_evidence_digests(self):
        sys.path.insert(0, str(SCRIPTS))
        import build
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp)
            image_sha256, qemu_log_sha256 = self.make_verified_output(build, output)
            self.assertEqual(build.load_verified_image(output, "cold-fingerprint"), {
                "rootfs_sha256": image_sha256,
                "image_receipt_sha256": hashlib.sha256((output / "image.json").read_bytes()).hexdigest(),
                "verification_sha256": hashlib.sha256((output / "verification.json").read_bytes()).hexdigest(),
                "qemu_log_sha256": qemu_log_sha256,
            })

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
