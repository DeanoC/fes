import hashlib
import importlib.util
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import unittest
import zlib


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/media_inputs.py"
IMAGE_CREATOR_REPOSITORY = "https://github.com/MiSTer-devel/Linux_Image_creator_MiSTer"
IMAGE_CREATOR_COMMIT = "8aba321b2162e54b56522aa30758b22d97eec8da"
UBOOT_SHA256 = "21533e9903675329e3273aebced63c87dfa781f79327c121f24153ee2a11ecb9"
UPSTREAM_UBOOT_SHA256 = "e2d46cf9fe1ec40ca2c9c7409870249f267e06f70e5736dc6d30b4e21fe62a64"
KERNEL_SHA256 = "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae"

LOCK_TEXT = f'''format = 1
repository = "{IMAGE_CREATOR_REPOSITORY}"
commit = "{IMAGE_CREATOR_COMMIT}"
layout = "de10-nano-mister-v1"
sector_size = 512
total_sectors = 1052672
disk_id = 0x46455331
fat_serial = 0xf35d0001
fat_label = "FESDATA"

[partition_1]
start_sector = 2048
sector_count = 1048576
type = 0x0c
active = true
chs_start = "feffff"
chs_end = "feffff"

[partition_2]
start_sector = 1050624
sector_count = 2048
type = 0xa2
active = false
chs_start = "feffff"
chs_end = "feffff"

[uboot]
path = "uboot.img"
size = 515141
sha256 = "{UBOOT_SHA256}"
environment = [
  "mmcroot=/dev/mmcblk0p1",
  "bootimage=/linux/zImage_dtb",
  "core=idle.rbf",
  "loop=linux/linux.img ro rootwait",
]

[uboot_upstream]
path = "uboot.img"
size = 515141
sha256 = "{UPSTREAM_UBOOT_SHA256}"

[kernel]
path = "zImage_dtb"
size = 7380857
sha256 = "{KERNEL_SHA256}"
'''


def legacy_uboot(data):
    """Return an SPL-prefixed legacy mkimage fixture like the MiSTer uboot.img."""
    header = bytearray(struct.pack(">7I4B32s", 0x27051956, 0, 1743596165, len(data), 0x1000040, 0,
                                   zlib.crc32(data), 17, 2, 5, 0, b"U-Boot fixture"))
    struct.pack_into(">I", header, 4, zlib.crc32(header))
    return b"S" * (4 * 65536) + bytes(header) + data


class MediaInputsTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.source = self.root / "source"
        self.source.mkdir()
        self.git(self.source, "init", "-q")
        self.git(self.source, "config", "user.email", "test@example.invalid")
        self.git(self.source, "config", "user.name", "Test")
        (self.source / "seed").write_text("seed")
        (self.source / ".gitignore").write_text("ignored\n")
        self.git(self.source, "add", "seed", ".gitignore")
        self.git(self.source, "commit", "-qm", "seed")
        self.uboot_bytes = legacy_uboot(b"prefix\0mmcroot=/dev/mmcblk0p1\0"
                                        b"bootimage=/linux/zImage_dtb\0core=menu.rbf\0"
                                        b"loop=linux/linux.img ro rootwait\0suffix")
        self.kernel_bytes = b"kernel payload"
        (self.source / "uboot.img").write_bytes(self.uboot_bytes)
        (self.source / "zImage_dtb").write_bytes(self.kernel_bytes)
        self.git(self.source, "add", "uboot.img", "zImage_dtb")
        self.git(self.source, "commit", "-qm", "payloads")
        self.commit = self.git(self.source, "rev-parse", "HEAD").strip()
        self.calls = []
        self.module = self.load_module()
        self.uboot_module = sys.modules["media_uboot"]
        self.lock = self.fixture_lock()

    def tearDown(self):
        self.temporary.cleanup()

    def git(self, directory, *args):
        return subprocess.check_output(["git", "-C", str(directory), *args], text=True)

    def load_module(self):
        self.assertTrue(SCRIPT.exists(), "boot payload resolver is not implemented")
        sys.path.insert(0, str(SCRIPT.parent))
        self.addCleanup(sys.path.remove, str(SCRIPT.parent))
        spec = importlib.util.spec_from_file_location("media_inputs", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def fixture_lock(self):
        derived = self.uboot_module.derive(self.uboot_bytes)
        return LOCK_TEXT.replace(IMAGE_CREATOR_COMMIT, self.commit).replace(
            "size = 515141", f"size = {len(self.uboot_bytes)}").replace(
            UBOOT_SHA256, hashlib.sha256(derived).hexdigest()).replace(
            UPSTREAM_UBOOT_SHA256, hashlib.sha256(self.uboot_bytes).hexdigest()).replace(
            "size = 7380857", f"size = {len(self.kernel_bytes)}").replace(
            KERNEL_SHA256, hashlib.sha256(self.kernel_bytes).hexdigest())

    def cache(self):
        return self.root / "out/work/boot-media" / ("image-creator-" + self.commit)

    def make_image_creator_cache(self, *, commit=None):
        cache = self.cache()
        cache.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(["git", "clone", "-q", "--no-hardlinks", str(self.source), str(cache)], check=True)
        self.git(cache, "checkout", "-q", "--detach", self.commit if commit is None else commit)
        return cache

    def recording_run(self, args):
        self.calls.append(tuple(map(str, args)))
        self.assertEqual(args[:2], ["git", "clone"])
        destination = Path(args[-1])
        subprocess.run(["git", "clone", "-q", "--no-hardlinks", str(self.source), str(destination)], check=True)

    def rejecting_run(self, args):
        raise AssertionError("offline cache reuse must not invoke git")

    def test_lock_rejects_unknown_fields_and_wrong_geometry(self):
        with self.assertRaisesRegex(ValueError, "unknown boot-media lock field"):
            self.module.MediaLock.loads(LOCK_TEXT + "\nunknown = true\n")
        with self.assertRaisesRegex(ValueError, "sector geometry"):
            self.module.MediaLock.loads(LOCK_TEXT.replace("total_sectors = 1052672", "total_sectors = 1"))

    def test_lock_rejects_noncanonical_scalar_types(self):
        cases = (
            ("format = 1", "format = true"),
            ("active = true", "active = 1"),
            ("fat_serial = 0xf35d0001", "fat_serial = 4082958337.0"),
        )
        for original, replacement in cases:
            with self.subTest(replacement=replacement):
                with self.assertRaises(ValueError):
                    self.module.MediaLock.loads(LOCK_TEXT.replace(original, replacement))

    def test_cached_payloads_require_exact_revision_and_hash(self):
        cache = self.make_image_creator_cache(commit=self.commit)
        (cache / "uboot.img").write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError, "uboot.img digest"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.rejecting_run)

    def test_cached_payloads_reject_wrong_detached_revision_and_dirt(self):
        cache = self.make_image_creator_cache()
        (cache / "untracked").write_text("dirt")
        with self.assertRaisesRegex(ValueError, "image-creator cache is changed"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.rejecting_run)
        (cache / "untracked").unlink()
        self.git(cache, "checkout", "-q", "--detach", "HEAD^")
        with self.assertRaisesRegex(ValueError, "image-creator cache revision"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.rejecting_run)

    def test_cached_payloads_reject_ignored_untracked_files(self):
        cache = self.make_image_creator_cache()
        (cache / "ignored").write_text("dirt")
        with self.assertRaisesRegex(ValueError, "image-creator cache is changed"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.rejecting_run)

    def test_cached_payloads_reject_symlinked_cache_path(self):
        cache = self.cache()
        cache.parent.mkdir(parents=True, exist_ok=True)
        cache.symlink_to(self.source, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, "cache path must not be a symlink"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.rejecting_run)

    def test_missing_cache_fetches_once_then_reuses_offline(self):
        lock = self.module.MediaLock.loads(self.fixture_lock())
        first = self.module.resolve_payloads(self.root, lock, self.recording_run)
        second = self.module.resolve_payloads(self.root, lock, self.rejecting_run)
        self.assertEqual(first, second)
        self.assertEqual(self.calls[0][0:2], ("git", "clone"))
        self.assertEqual(first.uboot, self.root / "out/work/boot-media" / (
            "uboot-" + hashlib.sha256(self.uboot_module.derive(self.uboot_bytes)).hexdigest() + ".img"))
        self.assertIn(b"\0core=idle.rbf\0", first.uboot.read_bytes())
        self.assertNotIn(b"menu.rbf", first.uboot.read_bytes())
        self.assertEqual((self.cache() / "uboot.img").read_bytes(), self.uboot_bytes)
        self.assertEqual(first.kernel, self.cache() / "zImage_dtb")

    def commit_uboot(self, data, message):
        self.uboot_bytes = data
        (self.source / "uboot.img").write_bytes(self.uboot_bytes)
        self.git(self.source, "add", "uboot.img")
        self.git(self.source, "commit", "-qm", message)
        self.commit = self.git(self.source, "rev-parse", "HEAD").strip()

    def test_uboot_requires_locked_environment_strings(self):
        self.commit_uboot(legacy_uboot(b"mmcroot=/dev/mmcblk0p1\0bootimage=/linux/zImage_dtb\0core=menu.rbf\0"),
                          "missing environment")
        with self.assertRaisesRegex(ValueError, "U-Boot environment string"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.recording_run)

    def test_changed_derived_cache_is_rewritten(self):
        lock = self.module.MediaLock.loads(self.fixture_lock())
        first = self.module.resolve_payloads(self.root, lock, self.recording_run)
        expected = first.uboot.read_bytes()
        first.uboot.chmod(0o644)
        first.uboot.write_bytes(b"stale")
        again = self.module.resolve_payloads(self.root, lock, self.rejecting_run)
        self.assertEqual(again.uboot.read_bytes(), expected)

    def test_lock_requires_distinct_upstream_uboot(self):
        with self.assertRaisesRegex(ValueError, "missing boot-media lock field: uboot_upstream"):
            self.module.MediaLock.loads(LOCK_TEXT.split("[uboot_upstream]")[0] + LOCK_TEXT.split(
                UPSTREAM_UBOOT_SHA256 + '"\n')[1])
        with self.assertRaisesRegex(ValueError, "must differ"):
            self.module.MediaLock.loads(LOCK_TEXT.replace(UPSTREAM_UBOOT_SHA256, UBOOT_SHA256))
        with self.assertRaisesRegex(ValueError, "environment strings differ"):
            self.module.MediaLock.loads(LOCK_TEXT.replace('"core=idle.rbf"', '"core=menu.rbf"'))


class UbootDerivationTest(unittest.TestCase):
    UPSTREAM = Path(os.environ.get("FES_UPSTREAM_UBOOT", ROOT / "out/work/boot-media"
                                   / ("image-creator-" + IMAGE_CREATOR_COMMIT) / "uboot.img"))

    def setUp(self):
        sys.path.insert(0, str(SCRIPT.parent))
        self.addCleanup(sys.path.remove, str(SCRIPT.parent))
        import media_uboot
        import media_inputs
        self.media_uboot = media_uboot
        self.lock = media_inputs.MediaLock.load(ROOT / "boot-media.lock.toml")

    def test_patch_changes_only_the_core_value_and_crcs(self):
        upstream = legacy_uboot(b"a\0core=menu.rbf\0fpgaload=load mmc 0:1 $core\0")
        derived = self.media_uboot.derive(upstream)
        self.assertEqual(len(derived), len(upstream))
        changed = [index for index in range(len(upstream)) if upstream[index] != derived[index]]
        header = 4 * 65536
        value = upstream.index(b"menu")
        self.assertTrue(set(changed) <= set(range(header + 4, header + 8)) | set(range(header + 24, header + 28))
                        | set(range(value, value + 4)))
        self.assertEqual(self.media_uboot.check(derived), len(upstream) - header - 64)

    def test_patch_rejects_bad_crc_and_ambiguous_environment(self):
        upstream = bytearray(legacy_uboot(b"a\0core=menu.rbf\0"))
        upstream[-3] ^= 1
        with self.assertRaisesRegex(ValueError, "data CRC"):
            self.media_uboot.derive(upstream)
        with self.assertRaisesRegex(ValueError, "exactly once"):
            self.media_uboot.derive(legacy_uboot(b"\0core=menu.rbf\0\0core=menu.rbf\0"))
        with self.assertRaisesRegex(ValueError, "magic"):
            self.media_uboot.derive(b"\0" * (4 * 65536 + 64))

    @unittest.skipUnless(UPSTREAM.is_file(), "locked upstream uboot.img cache is absent")
    def test_locked_upstream_derives_locked_uboot(self):
        derived = self.media_uboot.derive_locked(self.UPSTREAM.read_bytes(), self.lock)
        self.assertEqual(hashlib.sha256(derived).hexdigest(), self.lock.uboot.sha256)
        self.assertEqual(self.lock.uboot_upstream.sha256, UPSTREAM_UBOOT_SHA256)


if __name__ == "__main__":
    unittest.main()
