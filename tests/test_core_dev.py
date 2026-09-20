"""Host-only preparation tests: no producer, image, network or kit execution."""
from dataclasses import replace
import fcntl
import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import tomllib
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import core_dev


def sha(data):
    return hashlib.sha256(data).hexdigest()


class PrepareTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.output = self.root / "prepared"
        self.source = self.root / "source"
        self.package_id = "a" * 64
        self.package = self.source / "build/packages" / self.package_id
        self.package.mkdir(parents=True)
        self.archive = self.package.with_suffix(".fcore")
        self.archive.write_bytes(b"archive")
        self.archive.chmod(0o444)
        self.pins = {name: str(i) * 40 for i, name in enumerate(core_dev.inputs.COMPONENTS, 1)}
        self.identity = dict(package_id=self.package_id, core_id="fes.pong",
                             manifest_sha256=sha(b"manifest"), payload_sha256=sha(b"payload"))
        for target, value in [("ROOT", self.root)]:
            p = patch.object(core_dev.build, target, value)
            p.start()
            self.addCleanup(p.stop)
        # These fixtures exercise sealed v1 archives; v2 reconstruction has its own suite.
        self.mock(core_dev.recipes, "FORMAT2_RECIPES", new={
            name: replace(recipe, identity_version=1)
            for name, recipe in core_dev.recipes.FORMAT2_RECIPES.items()})
        self.validate = self.mock(core_dev.inputs, "validate", return_value=self.pins)
        self.stage = self.mock(core_dev.build, "source_checkout", return_value=self.source)
        self.resolve = self.mock(core_dev.bundle, "resolve_core_package", side_effect=self.resolver)
        self.inspect = self.mock(core_dev, "inspect_archive", return_value=self.identity)
        self.mock(core_dev.build, "main", side_effect=AssertionError("image orchestration forbidden"))

    def mock(self, obj, name, **kwargs):
        p = patch.object(obj, name, **kwargs)
        self.addCleanup(p.stop)
        return p.start()

    def resolver(self, source, revision, selection, *, recipe):
        self.assertEqual(source, self.source)
        self.assertEqual(revision, self.pins["mister-packages"])
        self.assertEqual(recipe, core_dev.recipes.recipe_for(self.identity["core_id"]))
        selection.write_bytes(b"selection")
        return dict(directory=self.package, selection_path=selection, inputs={
            "selection": {"package_id": self.package_id}, "selection_sha256": sha(b"selection"),
            "manifest_sha256": self.identity["manifest_sha256"],
            "core_rbf_sha256": self.identity["payload_sha256"]})

    def prepare(self, **kwargs):
        return core_dev.prepare("fes.pong", self.output, **kwargs)

    def test_one_core_snapshot_receipt_and_reuse_delegation(self):
        receipt = self.prepare()
        self.validate.assert_called_once_with(self.root)
        self.stage.assert_called_once_with("misteross", self.pins["misteross"])
        self.resolve.assert_called_once()
        self.assertEqual(receipt["archive"], dict(path="core.fcore", sha256=sha(b"archive"), size=7))
        self.assertEqual(receipt["selection"]["path"], "fes-pong.package-selection.toml")
        self.assertEqual(receipt["sources"], self.pins)
        self.assertNotIn("library_media", receipt)
        self.assertEqual(json.loads((self.output / "prepared.json").read_text()), receipt)
        self.assertEqual((self.output / "core.fcore").stat().st_mode & 0o777, 0o444)
        self.archive.chmod(0o644)
        self.archive.write_bytes(b"different")
        self.assertEqual((self.output / "core.fcore").read_bytes(), b"archive")

    def test_media_is_explicit_private_snapshot(self):
        media = self.root / "rom"
        data = b"r" * (65536 + 1)
        media.write_bytes(data)
        receipt = self.prepare(library_media=media, expected_media_sha256=sha(data))
        media.write_bytes(b"changed")
        self.assertEqual((self.output / "media.bin").read_bytes(), data)
        self.assertEqual(receipt["library_media"], dict(path="media.bin", sha256=sha(data), size=len(data)))

    def test_sms_uses_only_requested_recipe_without_factory_profile(self):
        self.identity["core_id"] = "fes.sms"
        receipt = core_dev.prepare("fes.sms", self.output)
        self.resolve.assert_called_once()
        self.assertEqual(receipt["core_id"], "fes.sms")
        self.assertEqual(receipt["selection"]["path"],
                         core_dev.recipes.recipe_for("fes.sms").selection_filename)

    def test_missing_cli_arguments_fail_before_build(self):
        with patch("sys.stderr", new=io.StringIO()):
            with self.assertRaises(SystemExit) as error:
                core_dev.main(["prepare", "--core", "fes.pong"])
        self.assertEqual(error.exception.code, 2)
        self.stage.assert_not_called()

    def test_missing_output_parent_fails_before_build(self):
        with self.assertRaisesRegex(ValueError, "parent directory"):
            core_dev.prepare("fes.pong", self.root / "missing/new")
        self.stage.assert_not_called()

    def test_invalid_media_fails_before_source_validation_or_build(self):
        media = self.root / "rom"
        media.write_bytes(b"rom")
        cases = [dict(library_media=media), dict(expected_media_sha256="a" * 64),
                 dict(library_media=media, expected_media_sha256="bad"),
                 dict(library_media=media, expected_media_sha256="a" * 64)]
        for arguments in cases:
            with self.subTest(arguments=arguments), self.assertRaises(ValueError):
                self.prepare(**arguments)
        self.validate.assert_not_called()
        self.stage.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_media_empty_oversize_symlink_and_fifo_rejected_before_build(self):
        import os
        for kind in ("empty", "oversize", "symlink", "fifo"):
            media = self.root / kind
            if kind == "empty":
                media.touch()
            elif kind == "oversize":
                with media.open("wb") as stream:
                    stream.truncate(core_dev.MAX_MEDIA_BYTES + 1)
            elif kind == "symlink":
                media.symlink_to(self.archive)
            else:
                os.mkfifo(media)
            with self.subTest(kind=kind), self.assertRaises((ValueError, OSError)):
                self.prepare(library_media=media, expected_media_sha256="a" * 64)
        self.stage.assert_not_called()

    def test_media_policy_edges_without_large_allocation(self):
        for size in (1, core_dev.MAX_MEDIA_BYTES):
            media = self.root / str(size)
            with media.open("wb") as stream:
                stream.truncate(size)
            self.assertEqual(core_dev.snapshot(media, limit=core_dev.MAX_MEDIA_BYTES)["size"], size)

    def test_output_exists_dangling_symlink_and_symlink_parent_rejected(self):
        self.output.mkdir()
        with self.assertRaises(ValueError):
            self.prepare()
        self.output.rmdir()
        self.output.symlink_to(self.root / "missing")
        with self.assertRaises(ValueError):
            self.prepare()
        self.output.unlink()
        link = self.root / "parent-link"
        link.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(ValueError):
            core_dev.prepare("fes.pong", link / "new")
        self.stage.assert_not_called()

    def test_unknown_core_fails_before_build(self):
        with self.assertRaises(ValueError):
            core_dev.prepare("unknown", self.output)
        self.stage.assert_not_called()

    def test_dirty_selected_input_fails_before_stage(self):
        self.validate.side_effect = ValueError("source checkout is dirty")
        with self.assertRaisesRegex(ValueError, "dirty"):
            self.prepare()
        self.stage.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_parent_lock_is_fail_fast_and_held_during_resolve(self):
        (self.root / "out").mkdir()
        with (self.root / "out/build.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaisesRegex(ValueError, "another parent build"):
                self.prepare()
        self.stage.assert_not_called()
        original = self.resolver
        def check_lock(*args, **kwargs):
            with (self.root / "out/build.lock").open("a") as lock:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            return original(*args, **kwargs)
        self.resolve.side_effect = check_lock
        self.prepare()

    def assert_failed(self):
        self.assertFalse((self.output / "prepared.json").exists())
        self.assertEqual(json.loads((self.output / "failure.json").read_text())["status"], "failed")

    def test_resolution_failure_preserves_failure_only(self):
        self.resolve.side_effect = ValueError("authentication failed")
        with self.assertRaisesRegex(ValueError, "authentication"):
            self.prepare()
        self.assert_failed()

    def test_archive_mismatch_preserves_no_success_receipt(self):
        self.inspect.return_value = {**self.identity, "manifest_sha256": "b" * 64}
        with self.assertRaisesRegex(ValueError, "identity"):
            self.prepare()
        self.assert_failed()

    def test_selection_changed_after_resolve_rejected(self):
        def change(*args):
            (self.output / "fes-pong.package-selection.toml").write_bytes(b"changed")
            return self.identity
        self.inspect.side_effect = change
        with self.assertRaisesRegex(ValueError, "SHA-256"):
            self.prepare()
        self.assert_failed()

    def test_archive_symlink_and_unsealed_rejected(self):
        self.archive.unlink()
        self.archive.symlink_to(self.root / "missing")
        with self.assertRaises(OSError):
            self.prepare()
        self.assert_failed()

    def test_unsealed_archive_rejected(self):
        self.archive.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "sealed"):
            self.prepare()
        self.assert_failed()

    def test_oversize_archive_rejected_before_copy_or_parser(self):
        self.archive.chmod(0o644)
        with self.archive.open("wb") as stream:
            stream.truncate(core_dev.MAX_ARCHIVE_BYTES + 1)
        self.archive.chmod(0o444)
        with self.assertRaisesRegex(ValueError, "exceeds"):
            self.prepare()
        self.inspect.assert_not_called()
        self.assertFalse((self.output / "core.fcore").exists())
        self.assert_failed()

    def test_media_change_after_preflight_fails_before_build(self):
        media = self.root / "rom"
        media.write_bytes(b"rom")
        def change(_):
            media.write_bytes(b"new")
            return self.pins
        self.validate.side_effect = change
        with self.assertRaisesRegex(ValueError, "SHA-256"):
            self.prepare(library_media=media, expected_media_sha256=sha(b"rom"))
        self.stage.assert_not_called()
        self.assert_failed()


class SelectedParserTest(unittest.TestCase):
    def test_real_selected_parser_checks_archive_against_directory(self):
        source = Path(__file__).resolve().parents[1] / "sources/misteross"
        if not (source / "scripts/core_package.py").is_file():
            self.skipTest("selected misteross source not available")
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            directory = root / "package"
            directory.mkdir()
            payload = b"payload"
            manifest = f'''format = 2
interfaces = []
[core]
id = "fes.pong"
name = "Test"
description = ""
version = "1.0.0"
[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"
[payload]
file = "core.rbf"
size = {len(payload)}
sha256 = "{sha(payload)}"
[abi]
id = "fes.simple-game"
major = 1
minor = 0
[build]
id = "{'a' * 32}"
repository = "https://example.com/core.git"
revision = "{'b' * 40}"
recipe_sha256 = "{'c' * 64}"
toolchain = "test"
'''.encode()
            archive = root / "core.fcore"
            with tarfile.open(archive, "w", format=tarfile.USTAR_FORMAT) as tar:
                for name, data in [("manifest.toml", manifest), ("core.rbf", payload)]:
                    (directory / name).write_bytes(data)
                    member = tarfile.TarInfo(name)
                    member.size = len(data)
                    member.mode = 0o644
                    tar.addfile(member, io.BytesIO(data))
            # Restricted format-2 TAR ends at two zero blocks, not tarfile's
            # default extra record padding.
            length = sum(512 + ((len(data) + 511) // 512) * 512
                         for data in (manifest, payload)) + 1024
            with archive.open("r+b") as stream:
                stream.truncate(length)
            recipe = core_dev.recipes.recipe_for("fes.pong")
            result = core_dev.inspect_archive(source, archive, directory, recipe)
            self.assertEqual(result["core_id"], "fes.pong")
            self.assertEqual(result["manifest_sha256"], sha(manifest))
            (directory / "manifest.toml").write_bytes(manifest.replace(b'Test', b'Other'))
            with self.assertRaises(subprocess.CalledProcessError):
                core_dev.inspect_archive(source, archive, directory, recipe)


class CachedPreparationTest(unittest.TestCase):
    def setUp(self):
        self.source = Path(__file__).resolve().parents[1] / "sources/misteross"
        if not (self.source / "scripts/export_core_package.py").is_file():
            self.skipTest("selected producer parser unavailable")
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.output = self.root / "prepared"
        self.recipe = replace(core_dev.recipes.recipe_for("fes.pong"), identity_version=2)
        self.pins = {name: "c" * 40 for name in core_dev.inputs.COMPONENTS}
        self.payload = b"original cached payload"
        self.manifest = f'''format = 2
interfaces = []
[core]
id = "fes.pong"
name = "Cached"
description = ""
version = "1.0.0"
[target]
platform = "de10_nano"
device = "5CSEBA6U23I7"
programming_profile = "fes-gp-v1"
[payload]
file = "core.rbf"
size = {len(self.payload)}
sha256 = "{sha(self.payload)}"
[abi]
id = "fes.simple-game"
major = 1
minor = 0
[build]
id = "{'a' * 32}"
repository = "https://example.com/core.git"
revision = "{'b' * 40}"
recipe_sha256 = "{'c' * 64}"
toolchain = "test"
'''.encode()
        # Actual content-addressed cache entry outside source/build/packages.
        original = {"format": 2, "repository": "https://example.com/core.git", "revision": "b" * 40,
                    "source_path": ".", "source_inputs": {"rtl/top.v": "d" * 64}}
        self.original_record = json.dumps(original, sort_keys=True).encode()
        self.selected_record = json.dumps({**original, "repository": "https://example.com/fes.git",
                                           "revision": self.pins["misteross"], "source_path": "sources/misteross"}, sort_keys=True).encode()
        # Use the real selected parser for package identity before cache layout.
        seed = self.root / "seed"
        seed.mkdir()
        (seed / "manifest.toml").write_bytes(self.manifest)
        (seed / "core.rbf").write_bytes(self.payload)
        identity = core_dev.inspect_archive(self.source, seed, seed, self.recipe)
        self.package_id = identity["package_id"]
        self.cache = self.root / "cache"
        slot = core_dev.bundle.artifact_cache.store_for(self.cache, self.original_record) / self.package_id
        slot.mkdir(parents=True)
        self.package = slot / self.package_id
        seed.rename(self.package)
        self.record_path = slot / "build-inputs.json"
        self.record_path.write_bytes(self.original_record)
        for member in (*self.package.iterdir(), self.record_path):
            member.chmod(0o444)
        self.package.chmod(0o555)
        slot.chmod(0o555)
        inspected = {"package_id": self.package_id, "manifest": tomllib.loads(self.manifest.decode()),
                     "manifest_sha256": sha(self.manifest), "core_rbf_sha256": sha(self.payload)}
        self.patches = []
        self.patch(core_dev.build, "ROOT", self.root)
        self.patch(core_dev.inputs, "validate", return_value=self.pins)
        self.patch(core_dev.build, "source_checkout", return_value=self.source)
        self.patch(core_dev.recipes, "recipe_for", return_value=self.recipe)
        self.patch(core_dev.bundle, "ARTIFACT_CACHE_ROOT", self.cache)
        self.patch(core_dev.bundle, "canonical_package_record", return_value=self.selected_record)
        self.candidates = self.patch(core_dev.bundle, "_matching_package_candidates",
                                     side_effect=[[], [(self.package, self.record_path, inspected)]])
        self.producer = self.patch(core_dev.bundle, "_build_package", side_effect=AssertionError("cache hit must not run producer"))

    def patch(self, obj, name, *args, **kwargs):
        p = patch.object(obj, name, *args, **kwargs)
        self.addCleanup(p.stop)
        return p.start()

    def test_real_cache_directory_reconstructs_archive_and_preserves_original_identity(self):
        import core_dev_accept
        self.assertFalse((self.source / "build/packages" / (self.package_id + ".fcore")).exists())
        receipt = core_dev.prepare("fes.pong", self.output)
        self.assertEqual(receipt["format"], 2)
        self.assertEqual(self.candidates.call_count, 2)
        self.assertIn("store_override", self.candidates.call_args.kwargs)
        self.producer.assert_not_called()
        self.assertEqual(self.record_path.read_bytes(), self.original_record)
        self.assertEqual((self.package / "manifest.toml").read_bytes(), self.manifest)
        with tarfile.open(self.output / "core.fcore") as archive:
            self.assertEqual(archive.extractfile("manifest.toml").read(), self.manifest)
            self.assertEqual(archive.extractfile("core.rbf").read(), self.payload)
        self.assertEqual(receipt["sources"], self.pins)
        selection = tomllib.loads((self.output / receipt["selection"]["path"]).read_text())
        self.assertEqual(selection["misteross_revision"], "b" * 40)
        sidecar = self.output / receipt["source_selection"]["path"]
        self.assertEqual(sha(sidecar.read_bytes()), receipt["source_selection"]["sha256"])
        args = core_dev_accept.candidate_arguments(self.output / "prepared.json")
        self.assertEqual(args[args.index("--expected-package-id") + 1], self.package_id)
        self.assertEqual((self.output / "core.fcore").stat().st_mode & 0o777, 0o444)

    def test_reconstruction_failure_removes_partial_archive_and_provenance(self):
        original = core_dev.subprocess.run
        def fail(*args, **kwargs):
            if any("from scripts.export_core_package import _archive_bytes" in str(arg) for arg in args[0]):
                (self.output / ".core.fcore.tmp").write_bytes(b"partial")
                raise subprocess.CalledProcessError(1, args[0])
            return original(*args, **kwargs)
        with patch.object(core_dev.subprocess, "run", side_effect=fail):
            with self.assertRaises(subprocess.CalledProcessError):
                core_dev.prepare("fes.pong", self.output)
        self.assertFalse((self.output / "core.fcore").exists())
        self.assertFalse((self.output / ".core.fcore.tmp").exists())
        self.assertFalse((self.output / "prepared.json").exists())
        self.assertFalse((self.output / "fes-pong.package-selection.provenance.json").exists())
        self.assertTrue((self.output / "failure.json").is_file())
        self.producer.assert_not_called()

    def test_reconstructed_identity_failure_removes_published_archive_and_sidecar(self):
        with patch.object(core_dev, "inspect_archive", return_value={}):
            with self.assertRaisesRegex(ValueError, "archive identity"):
                core_dev.prepare("fes.pong", self.output)
        self.assertFalse((self.output / "core.fcore").exists())
        self.assertFalse((self.output / "prepared.json").exists())
        self.assertFalse((self.output / "fes-pong.package-selection.provenance.json").exists())
        self.assertEqual(self.record_path.read_bytes(), self.original_record)

    def test_cached_member_bounds_and_sealing_are_enforced(self):
        record = {"manifest_sha256": sha(self.manifest), "core_rbf_sha256": sha(self.payload)}
        member = self.package / "core.rbf"
        member.chmod(0o644)
        with self.assertRaisesRegex(ValueError, "sealed"):
            core_dev.reconstruct_archive(self.source, self.package, self.root / "bad.fcore", self.recipe, record)
        with member.open("wb") as stream:
            stream.truncate((32 << 20) + 1)
        member.chmod(0o444)
        with self.assertRaisesRegex(ValueError, "exceeds"):
            core_dev.reconstruct_archive(self.source, self.package, self.root / "bad.fcore", self.recipe, record)
        self.assertFalse((self.root / "bad.fcore").exists())


if __name__ == "__main__":
    unittest.main()
