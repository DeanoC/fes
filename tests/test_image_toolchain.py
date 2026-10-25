import hashlib
import fcntl
import json
from concurrent.futures import ThreadPoolExecutor
import os
from pathlib import Path
import shutil
import sys
import tempfile
import threading
import tomllib
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "image/scripts"))
sys.path.insert(0, str(ROOT / "scripts"))
import toolchain_cache as cache
import build


class ToolchainCacheTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.image = Path(self.temp.name) / "image"
        for name in ("buildroot/configs/fogcast_toolchain.fragment",
                     "buildroot/configs/fogcast_toolchain_only_defconfig",
                     "buildroot/configs/fogcast_target_native_dev_defconfig",
                     "build/target-image.sources.lock.toml",
                     "build/target-image-container-packages.sha256",
                     *cache.BUILDER_CONTEXT):
            destination = self.image / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / "image" / name, destination)

    def test_key_uses_toolchain_and_builder_inputs(self):
        original = cache.key(self.image)
        for name, replacement in ((cache.FRAGMENT, b"\n# changed options\n"),
                                  ("build/target-image-container-packages.sha256", b"changed lock\n"),
                                  ("containers/target-image/Dockerfile", b"\n# changed builder\n"),
                                  ("containers/target-image/create-builder-user.sh", b"\n# changed user\n")):
            path = self.image / name
            old = path.read_bytes()
            path.write_bytes(old + replacement)
            self.assertNotEqual(cache.key(self.image), original)
            path.write_bytes(old)
        lock = self.image / "build/target-image.sources.lock.toml"
        old = lock.read_text()
        lock.write_text(old.replace("004a792dcf10e6c474070c9571f7504411e786cc", "0" * 40))
        self.assertNotEqual(cache.key(self.image), original)
        lock.write_text(old)
        container = tomllib.loads(old)["container"]
        for field in cache.CONTAINER_FIELDS:
            with self.subTest(field=field):
                value = container[field]
                self.assertEqual(old.count(f"'{value}'"), 1)
                lock.write_text(old.replace(f"'{value}'", f"'{value}-changed'"))
                self.assertNotEqual(cache.key(self.image), original)
                lock.write_text(old)
        self.assertEqual(cache.key(self.image), original)
        self.assertNotEqual(cache.key(self.image, cache.EPOCH + 1), original)

    def test_validated_hit_and_corruption(self):
        host = self.image / "host"
        (host / "bin").mkdir(parents=True)
        (host / "bin" / (cache.PREFIX + "-gcc")).write_bytes(b"compiler")
        directory = cache.cache_dir(self.image)
        expected = cache.key(self.image)
        archive_sha = cache.package(host, directory, expected)
        self.assertEqual(cache.validated_sha(directory, expected), archive_sha)
        (directory / "host.tar").write_bytes(b"corrupt")
        self.assertIsNone(cache.validated_sha(directory, expected))
        self.assertEqual(cache.package(host, directory, expected), archive_sha)
        record = json.loads((directory / "receipt.json").read_text())
        record["key"] = "wrong"
        (directory / "receipt.json").write_text(json.dumps(record))
        self.assertIsNone(cache.validated_sha(directory, expected))

    def test_extract_holds_shared_lock_after_validation(self):
        host = self.image / "host"
        (host / "bin").mkdir(parents=True)
        (host / "bin" / (cache.PREFIX + "-gcc")).write_bytes(b"compiler")
        directory = cache.cache_dir(self.image)
        expected = cache.key(self.image)
        archive_sha = cache.package(host, directory, expected)
        destination = self.image / "extracted"
        archive_opened = threading.Event()
        continue_extracting = threading.Event()
        original_open = cache.tarfile.open

        def pause_archive_open(*args, **kwargs):
            archive_opened.set()
            if not continue_extracting.wait(timeout=10):
                raise TimeoutError("test did not release archive extraction")
            return original_open(*args, **kwargs)

        with ThreadPoolExecutor(max_workers=1) as extractor:
            with mock.patch.object(cache.tarfile, "open", pause_archive_open):
                future = extractor.submit(cache.extract, directory, expected, destination)
                self.assertTrue(archive_opened.wait(timeout=5), "extraction did not open the validated archive")
                descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
                acquired = False
                try:
                    try:
                        fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
                        acquired = True
                    except BlockingIOError:
                        pass
                    self.assertFalse(acquired, "exclusive publisher lock was available during extraction")
                finally:
                    if acquired:
                        fcntl.flock(descriptor, fcntl.LOCK_UN)
                    os.close(descriptor)
                    continue_extracting.set()
                self.assertEqual(future.result(timeout=10), archive_sha)

        self.assertEqual((destination / "host" / "bin" / (cache.PREFIX + "-gcc")).read_bytes(), b"compiler")

    def test_concurrent_publishers_leave_a_valid_cache_entry(self):
        hosts = [self.image / "host-one", self.image / "host-two"]
        for index, host in enumerate(hosts, start=1):
            (host / "bin").mkdir(parents=True)
            (host / "bin" / (cache.PREFIX + "-gcc")).write_bytes(b"compiler " + bytes([index]))
            (host / "payload").write_bytes(bytes([index]) * (2 * 1024 * 1024))

        directory = cache.cache_dir(self.image)
        expected = cache.key(self.image)
        publish_barrier = threading.Barrier(2)
        original_publish = cache._publish

        def synchronized_publish(*args):
            publish_barrier.wait(timeout=10)
            return original_publish(*args)

        with mock.patch.object(cache, "_publish", synchronized_publish):
            with ThreadPoolExecutor(max_workers=2) as publishers:
                results = list(publishers.map(
                    lambda host: cache.package(host, directory, expected), hosts))

        self.assertTrue(all(len(result) == 64 for result in results))
        self.assertIn(cache.validated_sha(directory, expected), results)
        self.assertEqual({path.name for path in directory.iterdir()}, {"host.tar", "receipt.json"})

    def test_shared_paths_and_ccache_configuration(self):
        shared_temp = tempfile.TemporaryDirectory()
        self.addCleanup(shared_temp.cleanup)
        shared = Path(shared_temp.name).resolve()  # e.g. macOS /var -> /private/var
        with mock.patch.dict("os.environ", {"FES_TARGET_IMAGE_SHARED_CACHE": str(shared)}):
            self.assertEqual(cache.cache_dir(self.image), shared / "toolchains" / cache.key(self.image))
            with mock.patch.dict("os.environ", {"FES_TARGET_IMAGE_SHARED_CACHE": cache.SHARED_CONTAINER_PATH}):
                self.assertEqual(cache.shared_root(Path("/work")), Path(cache.SHARED_CONTAINER_PATH))
            external = self.image / "external-shared"
            toolchain = self.image / "toolchain-shared"
            internal = self.image / "internal-shared"
            cache.config(self.image, "external", external)
            cache.config(self.image, "toolchain", toolchain)
            cache.config(self.image, "internal", internal)
            for option in cache.CCACHE_OPTIONS:
                self.assertIn(option, external.read_text().splitlines())
                self.assertNotIn(option, toolchain.read_text().splitlines())
                self.assertNotIn(option, internal.read_text().splitlines())
            cache.validate_config("external", external)
            cache.validate_config("toolchain", toolchain)
            external.write_text(external.read_text().replace("BR2_CCACHE=y", "# BR2_CCACHE is not set"))
            with self.assertRaisesRegex(ValueError, "BR2_CCACHE"):
                cache.validate_config("external", external)
            toolchain.write_text(toolchain.read_text() + "BR2_CCACHE=y\n")
            with self.assertRaisesRegex(ValueError, "must not enable ccache"):
                cache.validate_config("toolchain", toolchain)
        self.assertEqual(cache.cache_dir(self.image),
                         self.image / "build/cache/target-image/toolchains" / cache.key(self.image))
        ordinary = self.image / "external-ordinary"
        empty = self.image / "external-empty"
        cache.config(self.image, "external", ordinary)
        with mock.patch.dict("os.environ", {"FES_TARGET_IMAGE_SHARED_CACHE": ""}):
            cache.config(self.image, "external", empty)
        self.assertEqual(ordinary.read_bytes(), empty.read_bytes())
        self.assertNotIn("BR2_CCACHE=y", ordinary.read_text())
        for path in ("relative", str(self.image / "nested")):
            with self.subTest(path=path), mock.patch.dict("os.environ", {"FES_TARGET_IMAGE_SHARED_CACHE": path}):
                with self.assertRaisesRegex(ValueError, "absolute|outside the worktree"):
                    cache.cache_dir(self.image)

    def test_config_shares_arch_and_guards_drift(self):
        internal = self.image / "internal"
        external = self.image / "external"
        cache.config(self.image, "internal", internal)
        cache.config(self.image, "external", external)
        for option in ("BR2_arm=y", "BR2_cortex_a9=y", "BR2_ARM_ENABLE_VFP=y",
                       "BR2_ARM_EABIHF=y", "BR2_REPRODUCIBLE=y"):
            self.assertIn(option, internal.read_text())
            self.assertIn(option, external.read_text())
        self.assertIn(f'BR2_TOOLCHAIN_EXTERNAL_PATH="{cache.EXTERNAL_PATH}"', external.read_text())
        self.assertIn("BR2_TOOLCHAIN_EXTERNAL_GCC_9=y", external.read_text())
        self.assertIn("BR2_TOOLCHAIN_EXTERNAL_HEADERS_5_10=y", external.read_text())
        # glibc 2.32 (Buildroot 2021.02) has no SunRPC; the custom default would fail.
        self.assertIn("# BR2_TOOLCHAIN_EXTERNAL_INET_RPC is not set", external.read_text().splitlines())
        cache.validate_config("external", external)
        external.write_text(external.read_text().replace("# BR2_TOOLCHAIN_EXTERNAL_INET_RPC is not set", "BR2_TOOLCHAIN_EXTERNAL_INET_RPC=y"))
        with self.assertRaisesRegex(ValueError, "INET_RPC"):
            cache.validate_config("external", external)
        with (self.image / cache.FRAGMENT).open("a") as stream:
            stream.write("BR2_GCC_VERSION_10_X=y\n")
        with self.assertRaisesRegex(ValueError, "drifted"):
            cache.config(self.image, "external", external)

    def test_parent_fingerprint_and_evidence(self):
        with mock.patch.object(build, "toolchain_key", return_value="a" * 64), \
             mock.patch.object(build, "recipe_fingerprint", return_value={}), \
             mock.patch.object(build, "image_recipe_files", return_value=()), \
             mock.patch.object(build, "development_classification", return_value=None):
            first, info = build.build_fingerprint({}, {}, {})
            self.assertEqual(info["image_toolchain_key"], "a" * 64)
        with mock.patch.object(build, "toolchain_key", return_value="b" * 64), \
             mock.patch.object(build, "recipe_fingerprint", return_value={}), \
             mock.patch.object(build, "image_recipe_files", return_value=()), \
             mock.patch.object(build, "development_classification", return_value=None):
            second, _ = build.build_fingerprint({}, {}, {})
            self.assertNotEqual(first, second)
        output = Path(self.temp.name)
        (output / "qemu-smoke.log").write_bytes(b"smoke")
        (output / "reproducibility.txt").write_text(
            "toolchain_key=" + "a" * 64 + "\ntoolchain_sha256=" + "b" * 64 + "\n")
        receipt = build.verification_record(output, "image", None)
        self.assertEqual(receipt["toolchain_key"], "a" * 64)
        self.assertEqual(receipt["toolchain_sha256"], "b" * 64)


if __name__ == "__main__":
    unittest.main()
