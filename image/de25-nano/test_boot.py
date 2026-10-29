"""Byte-level SD assembly and locked vendor payload tests; never use a device."""
import copy
import hashlib
import importlib.util
from pathlib import Path
import struct
import tempfile
import unittest
from unittest import mock
import zipfile

SPEC = importlib.util.spec_from_file_location("de25_boot", Path(__file__).with_name("boot.py"))
BOOT = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BOOT)


def sha(data):
    return hashlib.sha256(data).hexdigest()


class VendorBootTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        # Small synthetic FAT32 byte fixture exercises the same assembly path.
        # It is not a hardware-valid boot image or an alternative board recipe.
        self.payload = b"test-only-kernel" * 60
        self.lock = {
            "image_size": 23 * 512, "prefix_size": 7 * 512,
            "image_member": "vendor.img", "root_device": "/dev/mmcblk0p2",
            "partitions": [{"type": 0x0b, "start_sector": 2, "sectors": 5},
                           {"type": 0x83, "start_sector": 7, "sectors": 16}],
            "boot_files": [{"name": "Image", "fat_name": "IMAGE      ",
                            "size": len(self.payload), "sha256": sha(self.payload)}],
            "factory_boot": [{"name": "factory.jic", "member": "GHRD/factory.jic",
                              "archive": "resources", "role": "factory-qspi-boot-dependency",
                              "size": 7, "sha256": sha(b"factory")}],
        }
        prefix = bytearray(self.lock["prefix_size"])
        prefix[510:512] = b"\x55\xaa"
        for index, part in enumerate(self.lock["partitions"]):
            offset = 446 + 16 * index
            prefix[offset + 4] = part["type"]
            struct.pack_into("<II", prefix, offset + 8, part["start_sector"], part["sectors"])
        self.lock["mbr_sha256"] = sha(prefix[:512])
        bpb = 1024
        struct.pack_into("<H", prefix, bpb + 11, 512)
        prefix[bpb + 13] = 1
        struct.pack_into("<H", prefix, bpb + 14, 1)
        prefix[bpb + 16] = 1
        struct.pack_into("<I", prefix, bpb + 32, 5)
        struct.pack_into("<I", prefix, bpb + 36, 1)
        struct.pack_into("<I", prefix, bpb + 44, 2)
        prefix[bpb + 82:bpb + 90] = b"FAT32   "
        prefix[bpb + 510:bpb + 512] = b"\x55\xaa"
        for cluster, following in ((2, 0x0fffffff), (3, 4), (4, 0x0fffffff)):
            struct.pack_into("<I", prefix, 1536 + 4 * cluster, following)
        prefix[2048:2059] = b"IMAGE      "
        prefix[2048 + 11] = 32
        struct.pack_into("<H", prefix, 2048 + 26, 3)
        struct.pack_into("<I", prefix, 2048 + 28, len(self.payload))
        prefix[2560:2560 + len(self.payload)] = self.payload
        self.prefix_bytes = bytes(prefix)
        self.lock["prefix_sha256"] = sha(prefix)
        self.prefix = self.root / "prefix.bin"
        self.prefix.write_bytes(prefix)
        self.rootfs = self.root / "rootfs.ext4"
        rootfs = bytearray(4096)
        rootfs[1024 + 56:1024 + 58] = b"\x53\xef"
        struct.pack_into("<I", rootfs, 1024 + 96, 0x40)
        self.rootfs.write_bytes(rootfs)

    def test_locked_vendor_geometry_and_real_payloads_are_declared(self):
        lock = BOOT.load_lock()
        self.assertEqual(lock["prefix_size"], 315621888)
        self.assertEqual(lock["image_size"], 3984588800)
        self.assertEqual({x["name"] for x in lock["boot_files"]},
                         {"Image", "u-boot.itb", "socfpga_agilex5_de25_nano.dtb", "boot.scr.uimg"})
        self.assertIn("golden_top_hps.jic", {x["name"] for x in lock["factory_boot"]})

    def test_assemble_preserves_mbr_and_boot_and_replaces_only_linux_partition(self):
        image = self.root / "card.img"
        record = BOOT.assemble(self.prefix, self.rootfs, image, self.lock)
        expected = self.prefix_bytes + self.rootfs.read_bytes() + bytes(4096)
        self.assertEqual(image.read_bytes(), expected)
        self.assertEqual(record["sha256"], sha(expected))
        self.assertEqual(record["hardware_boot"], "not-run")
        with self.assertRaises(FileExistsError):
            BOOT.assemble(self.prefix, self.rootfs, image, self.lock)
        for offset, reason in ((700, "prefix"), (self.lock["prefix_size"] + 20, "rootfs"),
                               (self.lock["image_size"] - 1, "padding")):
            with self.subTest(reason=reason):
                altered = bytearray(expected)
                altered[offset] ^= 1
                image.write_bytes(altered)
                with self.assertRaisesRegex(ValueError, reason):
                    BOOT.verify_image(image, self.rootfs, self.lock)

    def test_invalid_prefix_rootfs_and_symlink_fail_before_output(self):
        with self.prefix.open("r+b") as target:
            target.seek(700)
            target.write(b"x")
        with self.assertRaisesRegex(ValueError, "identity differs"):
            BOOT.assemble(self.prefix, self.rootfs, self.root / "bad-prefix.img", self.lock)
        self.prefix.write_bytes(self.prefix_bytes)
        self.rootfs.write_bytes(b"x" * 4096)
        with self.assertRaisesRegex(ValueError, "ext4"):
            BOOT.assemble(self.prefix, self.rootfs, self.root / "bad-root.img", self.lock)
        linked = self.root / "linked"
        linked.symlink_to(self.prefix)
        with self.assertRaisesRegex(ValueError, "regular"):
            BOOT.assemble(linked, self.rootfs, self.root / "bad-link.img", self.lock)
        self.assertFalse(any(self.root.glob("bad-*.img")))

    def test_fat_reader_checks_real_chain_and_exact_file_hash(self):
        output = self.root / "boot-files"
        output.mkdir()
        records = BOOT.extract_boot_files(self.prefix, output, self.lock)
        self.assertEqual((output / "Image").read_bytes(), self.payload)
        self.assertEqual(records[0]["sha256"], sha(self.payload))
        with self.prefix.open("r+b") as target:
            target.seek(1536 + 3 * 4)
            target.write(struct.pack("<I", 3))
        with self.assertRaisesRegex(ValueError, "cluster chain"):
            BOOT.extract_boot_files(self.prefix, self.root, self.lock)

    def test_prepare_boot_keeps_factory_dependencies_separate(self):
        bsp, resources = self.root / "bsp.zip", self.root / "resources.zip"
        vendor_disk = self.prefix_bytes + bytes(8192)
        self.lock["image_sha256"] = sha(vendor_disk)
        with zipfile.ZipFile(bsp, "w") as archive:
            archive.writestr("vendor.img", vendor_disk)
        with zipfile.ZipFile(resources, "w") as archive:
            archive.writestr("GHRD/factory.jic", b"factory")
        with mock.patch.object(BOOT, "fetch_vendor", return_value={"bsp": bsp, "resources": resources}):
            record = BOOT.prepare_boot(self.root, self.root / "prepared", self.lock)
            self.assertEqual(BOOT.prepare_boot(self.root, self.root / "prepared", self.lock), record)
        self.assertEqual(Path(record["prefix"]).read_bytes(), self.prefix_bytes)
        self.assertEqual(record["factory_programming"], "not-performed")
        factory = Path(record["factory_boot_directory"])
        self.assertEqual((factory / "factory.jic").read_bytes(), b"factory")
        (self.root / "prepared/Image").write_bytes(b"modified")
        with mock.patch.object(BOOT, "fetch_vendor", return_value={"bsp": bsp, "resources": resources}):
            with self.assertRaisesRegex(ValueError, "identity differs"):
                BOOT.prepare_boot(self.root, self.root / "prepared", self.lock)

    def test_cached_artifacts_are_rehashed_without_network(self):
        lock = copy.deepcopy(self.lock)
        for group in ("bsp", "resources"):
            path = self.root / f"{group}.zip"
            path.write_bytes(group.encode())
            lock[group] = {"filename": path.name, "size": path.stat().st_size,
                           "sha256": sha(group.encode()), "url": "https://example.invalid"}
        with mock.patch.object(BOOT.urllib.request, "urlopen", side_effect=AssertionError("network")):
            BOOT.fetch_vendor(self.root, lock)
            (self.root / "bsp.zip").write_bytes(b"bad")
            with self.assertRaisesRegex(ValueError, "identity"):
                BOOT.fetch_vendor(self.root, lock)


if __name__ == "__main__":
    unittest.main()
