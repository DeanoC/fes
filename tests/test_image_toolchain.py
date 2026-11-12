import hashlib
import fcntl
import json
from concurrent.futures import ThreadPoolExecutor
import os
from pathlib import Path
import shlex
import shutil
import signal
import subprocess
import sys
import tempfile
import threading
import time
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

    def test_import_preserves_authenticated_archive_and_bad_copy_keeps_existing_entry(self):
        source = self.image / 'aarch64-sdk.tar.gz'; source.write_bytes(b'opaque authenticated SDK archive')
        expected_sha = cache.file_sha(source); expected_key = 'a' * 64
        directory = self.image / 'de25-cache'
        self.assertEqual(cache.import_archive(source, directory, expected_key, expected_sha), expected_sha)
        self.assertEqual((directory / 'host.tar').read_bytes(), source.read_bytes())
        local = self.image / 'private-sdk.tar.gz'
        self.assertEqual(cache.copy_archive(directory, expected_key, local), expected_sha)
        self.assertEqual(local.read_bytes(), source.read_bytes())
        source.write_bytes(b'changed source after authentication')
        with self.assertRaisesRegex(ValueError, 'changed while copying'):
            cache.import_archive(source, directory, expected_key, expected_sha)
        self.assertEqual(cache.validated_sha(directory, expected_key), expected_sha)
        with self.assertRaisesRegex(ValueError, 'key or sha256 mismatch'):
            cache.copy_archive(directory, 'b' * 64, self.image / 'not-created')
        self.assertFalse((self.image / 'not-created').exists())

    def test_private_archive_copy_holds_reader_lock(self):
        source = self.image / 'source-sdk'; source.write_bytes(b'authenticated compiler archive')
        directory = self.image / 'de25-cache'; expected = 'a' * 64
        digest = cache.file_sha(source); cache.import_archive(source, directory, expected, digest)
        entered, resume = threading.Event(), threading.Event()
        copy = cache.shutil.copyfile
        def pause(*args, **kwargs):
            entered.set()
            if not resume.wait(timeout=10):
                raise TimeoutError('test did not release cache reader')
            return copy(*args, **kwargs)
        with ThreadPoolExecutor(max_workers=1) as workers, mock.patch.object(cache.shutil, 'copyfile', side_effect=pause):
            future = workers.submit(cache.copy_archive, directory, expected, self.image / 'private-copy')
            self.assertTrue(entered.wait(timeout=5))
            descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY)
            try:
                with self.assertRaises(BlockingIOError):
                    fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            finally:
                os.close(descriptor); resume.set()
            self.assertEqual(future.result(timeout=10), digest)

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


class ToolchainBuildLockTest(unittest.TestCase):
    """Exercise the image and kit SDK entrypoints as separate worktree processes."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.shared = self.root / "shared"
        self.gate = self.root / "release-builds"
        self.log = self.root / "builds.log"
        self.builder = self.root / "fake-builder"
        self.builder.write_text('''#!/bin/sh
set -eu
repo=$1
printf '%s\\n' "$repo" >> "$FAKE_BUILD_LOG"
while [ ! -e "$FAKE_BUILD_GATE" ]; do sleep 0.05; done
if [ "${FAKE_FAIL:-0}" = 1 ]; then exit 13; fi
mkdir -p "$repo/fake-host/bin"
printf compiler > "$repo/fake-host/bin/arm-buildroot-linux-gnueabihf-gcc"
python3 "$repo/scripts/toolchain_cache.py" package "$repo/fake-host" >/dev/null
''')
        self.builder.chmod(0o755)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        flock = self.bin / "flock"
        flock.write_text('#!/bin/sh\n'
                         'if [ "$1" = -x ]; then touch "$FAKE_LOCK_ATTEMPT"; fi\n'
                         f'exec {shlex.quote(shutil.which("flock"))} "$@"\n')
        flock.chmod(0o755)
        self.worktrees = [self.worktree("one"), self.worktree("two")]

    def worktree(self, name):
        image = self.root / name / "image"
        for name in ("scripts/build-target-image.sh", "scripts/toolchain_cache.py",
                     "buildroot/configs/fogcast_toolchain.fragment",
                     "build/target-image.sources.lock.toml",
                     "build/target-image-container-packages.sha256",
                     *cache.BUILDER_CONTEXT):
            destination = image / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / "image" / name, destination)
        verifier = image / "scripts/verify-target-image-source-cache.sh"
        verifier.write_text('#!/bin/sh\ntouch "$FAKE_FETCH_MARKER"\nexit 0\n')
        verifier.chmod(0o755)
        return image

    def start(self, image, entry, *, forced=False, fail=False):
        env = dict(os.environ, FES_TARGET_IMAGE_SHARED_CACHE=str(self.shared),
                   TARGET_IMAGE_TEST_MODE="1", TARGET_IMAGE_BUILD_ONCE="/bin/true",
                   TARGET_IMAGE_TOOLCHAIN_BUILD_ONCE=str(self.builder),
                   FAKE_BUILD_GATE=str(self.gate), FAKE_BUILD_LOG=str(self.log),
                   FAKE_FETCH_MARKER=str(image / "fetch-verified"),
                   FAKE_LOCK_ATTEMPT=str(image / "build-lock-attempt"),
                   PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                   TOOLCHAIN_REBUILD="1" if forced else "0", FAKE_FAIL="1" if fail else "0")
        process = subprocess.Popen(["sh", str(image / "scripts/build-target-image.sh"), *entry],
                                   env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
        self.addCleanup(self.stop, process)
        return process

    @staticmethod
    def stop(process):
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate(timeout=10)

    def wait_for(self, condition, message):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if condition():
                return
            time.sleep(0.05)
        self.fail(message)

    def builds(self):
        return self.log.read_text().splitlines() if self.log.exists() else []

    def finish(self, process, expected=0):
        stdout, stderr = process.communicate(timeout=20)
        self.assertEqual(process.returncode, expected, f"stdout: {stdout}\nstderr: {stderr}")

    def test_shared_miss_serializes_image_and_kit_sdk(self):
        first = self.start(self.worktrees[0], ["--ensure-toolchain", "native-dev"])
        self.wait_for(lambda: len(self.builds()) == 1, "image build did not start")
        second = self.start(self.worktrees[1], ["--ensure-kit-sdk"])
        self.wait_for(lambda: (self.worktrees[1] / "fetch-verified").exists(), "kit source verification did not run")
        self.wait_for(lambda: (self.worktrees[1] / "build-lock-attempt").exists(), "kit build did not attempt lock")
        time.sleep(0.2)
        self.assertEqual(len(self.builds()), 1, "kit build started before image build released the lock")
        self.gate.touch()
        self.finish(first)
        self.finish(second)
        self.assertEqual(len(self.builds()), 1, "a waiter rebuilt the newly published SDK")
        directory = self.shared / "toolchains" / cache.key(self.worktrees[0])
        self.assertIsNotNone(cache.validated_sha(directory, cache.key(self.worktrees[0])))

    def test_failed_builder_releases_lock_for_waiter(self):
        first = self.start(self.worktrees[0], ["--ensure-kit-sdk"], fail=True)
        self.wait_for(lambda: len(self.builds()) == 1, "first build did not start")
        second = self.start(self.worktrees[1], ["--ensure-toolchain", "native-dev"])
        self.wait_for(lambda: (self.worktrees[1] / "build-lock-attempt").exists(), "waiter did not attempt lock")
        self.gate.touch()
        self.finish(first, 13)
        self.finish(second)
        self.assertEqual(len(self.builds()), 2)
        self.assertIsNotNone(cache.validated_sha(self.shared / "toolchains" / cache.key(self.worktrees[1]),
                                                 cache.key(self.worktrees[1])))

    def test_forced_rebuilds_are_serialized_and_both_run(self):
        first = self.start(self.worktrees[0], ["--ensure-toolchain", "native-dev"], forced=True)
        self.wait_for(lambda: len(self.builds()) == 1, "first build did not start")
        second = self.start(self.worktrees[1], ["--ensure-kit-sdk"], forced=True)
        self.wait_for(lambda: (self.worktrees[1] / "fetch-verified").exists(), "kit source verification did not run")
        self.wait_for(lambda: (self.worktrees[1] / "build-lock-attempt").exists(), "kit build did not attempt lock")
        time.sleep(0.2)
        self.assertEqual(len(self.builds()), 1, "forced build ran while another builder held the lock")
        self.gate.touch()
        self.finish(first)
        self.finish(second)
        self.assertEqual(len(self.builds()), 2, "forced waiter skipped its rebuild")

    def test_different_keys_build_concurrently(self):
        with (self.worktrees[1] / cache.FRAGMENT).open("a") as stream:
            stream.write("# distinct toolchain key\n")
        first = self.start(self.worktrees[0], ["--ensure-toolchain", "native-dev"])
        self.wait_for(lambda: len(self.builds()) == 1, "first build did not start")
        second = self.start(self.worktrees[1], ["--ensure-toolchain", "native-dev"])
        self.wait_for(lambda: len(self.builds()) == 2, "different key waited on first build")
        self.gate.touch()
        self.finish(first)
        self.finish(second)


if __name__ == "__main__":
    unittest.main()
