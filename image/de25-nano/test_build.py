"""Headless target provenance and failure gates without Docker or hardware."""
from contextlib import redirect_stderr
import copy
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("de25_linux_builder", Path(__file__).with_name("build.py"))
BUILD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILD)


class HeadlessBuildTests(unittest.TestCase):
    def test_output_rejects_symlinks_and_special_files_before_writing(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / "output"
            BUILD.validate_output(output)
            original = root / "retained"
            original.write_bytes(b"retained bytes")
            link = output / "rootfs.ext4"
            link.symlink_to(original)
            with self.assertRaisesRegex(ValueError, "regular files"):
                BUILD.validate_output(output)
            self.assertEqual(original.read_bytes(), b"retained bytes")
            link.unlink()
            import os
            os.mkfifo(output / "rootfs.ext4")
            with self.assertRaisesRegex(ValueError, "regular files"):
                BUILD.validate_output(output)

    def test_elf_architecture_rejects_armv7_x86_big_endian_and_truncation(self):
        header = bytearray(64)
        header[:6] = b"\x7fELF\x02\x01"
        header[18:20] = (183).to_bytes(2, "little")
        BUILD.elf_aarch64(header)
        variants = [b"", bytes(header[:19])]
        for position, replacement in ((4, b"\x01"), (5, b"\x02"),
                                      (18, (40).to_bytes(2, "little")),
                                      (18, (62).to_bytes(2, "little"))):
            altered = bytearray(header)
            altered[position:position + len(replacement)] = replacement
            variants.append(altered)
        for data in variants:
            with self.subTest(header=bytes(data[:20]).hex()):
                with self.assertRaisesRegex(ValueError, "ELF64 AArch64"):
                    BUILD.elf_aarch64(data)

    def test_receipt_binds_target_recipe_and_actual_artifact_bytes(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            names = ["rootfs.ext4", "vendor-boot/Image"]
            for name in names:
                path = output / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(("artifact:" + name).encode())
            record = {"format": 1, "board": "de25-nano", "architecture": "aarch64",
                      "fes_revision": "a" * 40, "recipe_sha256": "b" * 64,
                      "files": {name: BUILD.digest(output / name) for name in names}}

            def write(value):
                (output / "image.json").write_text(json.dumps(value))

            with mock.patch.object(BUILD, "recipe_key", return_value="b" * 64), \
                    mock.patch.object(BUILD, "artifact_names", return_value=names):
                write(record)
                self.assertEqual(BUILD.verify_receipt(output), record)
                for key, bad in (("board", "de10-nano"), ("architecture", "arm"),
                                 ("fes_revision", "not-a-commit"), ("recipe_sha256", "c" * 64)):
                    altered = dict(record, **{key: bad})
                    write(altered)
                    with self.subTest(field=key), self.assertRaisesRegex(ValueError, "target or recipe"):
                        BUILD.verify_receipt(output)
                altered = copy.deepcopy(record)
                altered["files"].pop(names[0])
                write(altered)
                with self.assertRaisesRegex(ValueError, "missing or unexpected"):
                    BUILD.verify_receipt(output)
                write(record)
                (output / names[0]).write_bytes(b"changed bytes")
                with self.assertRaisesRegex(ValueError, "artifact hash or path"):
                    BUILD.verify_receipt(output)
                (output / names[0]).unlink()
                (output / names[0]).symlink_to(output / names[1])
                with self.assertRaisesRegex(ValueError, "artifact hash or path"):
                    BUILD.verify_receipt(output)

    def test_recipe_and_rootfs_keys_track_functional_inputs_without_docs_or_tests(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)

            def put(relative, value):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(value)
                return path

            for relative in BUILD.BUILDER_FILES:
                put(relative, "builder input: " + relative)
            overlay = put("image/de25-nano/rootfs/overlay/etc/hostname", "fes-de25-nano")
            put("image/de25-nano/buildroot.lock.toml", "locked Buildroot input")
            boot = put("image/de25-nano/boot.py", "boot assembly recipe")
            original = BUILD.recipe_key(root), BUILD.rootfs_key(root)
            put("docs/de25-linux.md", "documentation")
            put("image/de25-nano/README.md", "documentation")
            put("image/de25-nano/test_build.py", "unit test")
            put("image/de25-nano/__pycache__/build.pyc", "local bytecode")
            self.assertEqual((BUILD.recipe_key(root), BUILD.rootfs_key(root)), original)
            overlay.write_text("different target identity")
            self.assertNotEqual(BUILD.recipe_key(root), original[0])
            self.assertNotEqual(BUILD.rootfs_key(root), original[1])
            changed = BUILD.recipe_key(root), BUILD.rootfs_key(root)
            boot.write_text("changed boot assembly")
            self.assertNotEqual(BUILD.recipe_key(root), changed[0])
            self.assertEqual(BUILD.rootfs_key(root), changed[1])
            changed = BUILD.recipe_key(root), BUILD.rootfs_key(root)
            (root / BUILD.BUILDER_FILES[2]).write_text("changed compiler container")
            self.assertNotEqual(BUILD.recipe_key(root), changed[0])
            self.assertNotEqual(BUILD.rootfs_key(root), changed[1])

    def test_snapshot_rejects_uncommitted_and_modified_retained_source(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)

            def git(*args):
                return subprocess.check_output(["git", "-C", str(root), *args], stderr=subprocess.PIPE)

            git("init", "-q")
            git("config", "user.name", "FES test")
            git("config", "user.email", "fes-test@example.invalid")
            (root / ".gitignore").write_text("/out/\n")
            tracked = root / "tracked"
            tracked.write_text("committed source")
            git("add", ".")
            git("commit", "-qm", "fixture")
            with mock.patch.object(BUILD, "ROOT", root):
                revision, source = BUILD.snapshot()
                self.assertEqual(revision, git("rev-parse", "HEAD").decode().strip())
                self.assertEqual((source / "tracked").read_bytes(), tracked.read_bytes())
                tracked.write_text("uncommitted source")
                with self.assertRaisesRegex(ValueError, "committed source"):
                    BUILD.snapshot()
                git("checkout", "--", "tracked")
                (source / "tracked").write_text("modified snapshot")
                with self.assertRaisesRegex(ValueError, "retained DE25 source snapshot"):
                    BUILD.snapshot()

    def test_failed_build_invalidates_previous_success_receipts(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output, cache = root / "output", root / "cache"
            output.mkdir()
            for name in ("image.json", "verify.json", "smoke.json"):
                (output / name).write_text("previous success")
            fake_boot = SimpleNamespace(prepare_boot=mock.Mock())
            with mock.patch.dict(sys.modules, {"boot": fake_boot}), \
                    mock.patch.object(BUILD, "snapshot", return_value=("a" * 40, root)), \
                    mock.patch.object(BUILD, "ensure_builder", return_value="sha256:" + "b" * 64), \
                    mock.patch.object(BUILD, "download_buildroot"), \
                    mock.patch.object(BUILD, "container", side_effect=[None, subprocess.CalledProcessError(1, "build")]), \
                    redirect_stderr(io.StringIO()):
                with self.assertRaises(SystemExit) as failure:
                    BUILD.main(["build", "--output", str(output), "--cache", str(cache)])
                self.assertEqual(failure.exception.code, 1)
            for name in ("image.json", "verify.json", "smoke.json"):
                self.assertFalse((output / name).exists())


if __name__ == "__main__":
    unittest.main()
