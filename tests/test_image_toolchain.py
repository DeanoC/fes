import hashlib
import json
from pathlib import Path
import shutil
import sys
import tempfile
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
                     "build/target-image-container-packages.sha256"):
            destination = self.image / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / "image" / name, destination)

    def test_key_uses_all_four_inputs(self):
        original = cache.key(self.image)
        for name, replacement in ((cache.FRAGMENT, b"\n# changed options\n"),
                                  ("build/target-image-container-packages.sha256", b"changed lock\n")):
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
