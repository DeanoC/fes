import hashlib
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/media_inputs.py"
IMAGE_CREATOR_REPOSITORY = "https://github.com/MiSTer-devel/Linux_Image_creator_MiSTer"
IMAGE_CREATOR_COMMIT = "8aba321b2162e54b56522aa30758b22d97eec8da"
UBOOT_SHA256 = "e2d46cf9fe1ec40ca2c9c7409870249f267e06f70e5736dc6d30b4e21fe62a64"
KERNEL_SHA256 = "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae"

LOCK_TEXT = f'''format = 1
repository = "{IMAGE_CREATOR_REPOSITORY}"
commit = "{IMAGE_CREATOR_COMMIT}"
layout = "de10-nano-mister-v1"
sector_size = 512
total_sectors = 528384
disk_id = 0x46455331
fat_serial = 0xf35d0001
fat_label = "FESDATA"

[partition_1]
start_sector = 2048
sector_count = 524288
type = 0x0c
active = true
chs_start = "feffff"
chs_end = "feffff"

[partition_2]
start_sector = 526336
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
  "core=menu.rbf",
  "loop=linux/linux.img ro rootwait",
]

[kernel]
path = "zImage_dtb"
size = 7380857
sha256 = "{KERNEL_SHA256}"
'''


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
        self.uboot_bytes = (b"prefix\0mmcroot=/dev/mmcblk0p1\0"
                            b"bootimage=/linux/zImage_dtb\0core=menu.rbf\0"
                            b"loop=linux/linux.img ro rootwait\0suffix")
        self.kernel_bytes = b"kernel payload"
        (self.source / "uboot.img").write_bytes(self.uboot_bytes)
        (self.source / "zImage_dtb").write_bytes(self.kernel_bytes)
        self.git(self.source, "add", "uboot.img", "zImage_dtb")
        self.git(self.source, "commit", "-qm", "payloads")
        self.commit = self.git(self.source, "rev-parse", "HEAD").strip()
        self.calls = []
        self.lock = self.fixture_lock()
        self.module = self.load_module()

    def tearDown(self):
        self.temporary.cleanup()

    def git(self, directory, *args):
        return subprocess.check_output(["git", "-C", str(directory), *args], text=True)

    def load_module(self):
        self.assertTrue(SCRIPT.exists(), "boot payload resolver is not implemented")
        spec = importlib.util.spec_from_file_location("media_inputs", SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def fixture_lock(self):
        return LOCK_TEXT.replace(IMAGE_CREATOR_COMMIT, self.commit).replace(
            "size = 515141", f"size = {len(self.uboot_bytes)}").replace(
            UBOOT_SHA256, hashlib.sha256(self.uboot_bytes).hexdigest()).replace(
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
            self.module.MediaLock.loads(LOCK_TEXT.replace("total_sectors = 528384", "total_sectors = 1"))

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
        self.assertEqual(first.uboot, self.cache() / "uboot.img")
        self.assertEqual(first.kernel, self.cache() / "zImage_dtb")

    def test_uboot_requires_locked_environment_strings(self):
        self.uboot_bytes = b"mmcroot=/dev/mmcblk0p1\0bootimage=/linux/zImage_dtb\0core=menu.rbf"
        (self.source / "uboot.img").write_bytes(self.uboot_bytes)
        self.git(self.source, "add", "uboot.img")
        self.git(self.source, "commit", "-qm", "missing environment")
        self.commit = self.git(self.source, "rev-parse", "HEAD").strip()
        with self.assertRaisesRegex(ValueError, "U-Boot environment string"):
            self.module.resolve_payloads(self.root, self.module.MediaLock.loads(self.fixture_lock()), self.recording_run)


if __name__ == "__main__":
    unittest.main()
