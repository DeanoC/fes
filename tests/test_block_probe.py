"""Fake sysfs trees for optical, empty, USB and SD/FESDATA classification."""

import os
import tempfile
import time
import unittest
from pathlib import Path
from unittest.mock import patch

from scripts.block_probe import NO_MEDIA, OPTICAL, ZERO, _open_timeout, reason, reason_for


def write_dev(block: Path, name: str, size: str, removable: str, scsi_type: str) -> None:
    directory = block / name / "device"
    directory.mkdir(parents=True)
    (block / name / "size").write_text(size + "\n")
    (block / name / "removable").write_text(removable + "\n")
    if scsi_type:
        (directory / "type").write_text(scsi_type + "\n")


class BlockProbeTests(unittest.TestCase):
    def test_fake_sysfs_skips_optical_and_zero_keeps_usb_and_sd(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            block = root / "class" / "block"
            write_dev(block, "sda", "0", "1", "5")
            write_dev(block, "sr0", "0", "1", "5")
            write_dev(block, "sr1", "2048", "1", "5")
            write_dev(block, "sdc", "0", "1", "0")
            write_dev(block, "sdd", "0", "0", "0")
            write_dev(block, "sdb", "15633408", "1", "0")
            write_dev(block, "sdb1", "15633376", "1", "0")
            write_dev(block, "mmcblk0", "62333952", "1", "")
            write_dev(block, "mmcblk0p1", "2097152", "1", "")
            write_dev(block, "mmcblk0p3", "41943040", "1", "")
            os.mkfifo(block / "sda" / "device" / "inquiry")

            wide = block / "sda" / "sda10"
            wide.mkdir()
            (wide / "size").write_text("2048\n")
            self.assertEqual(reason(block, "sda10"), OPTICAL)

            self.assertEqual(reason(block, "sda"), OPTICAL)
            self.assertEqual(reason(block, "sr0"), OPTICAL)
            self.assertEqual(reason(block, "sr1"), OPTICAL)
            self.assertEqual(reason(block, "sdc"), NO_MEDIA)
            self.assertEqual(reason(block, "sdd"), ZERO)
            for name in ("sdb", "sdb1", "mmcblk0", "mmcblk0p1", "mmcblk0p3"):
                self.assertEqual(reason(block, name), "", name)

            # reason_for only consults absolute /dev paths against sysfs.
            self.assertEqual(reason_for("/dev/sdb", sysfs=str(root)), "")
            self.assertEqual(reason_for("/dev/sda", sysfs=str(root)), OPTICAL)
            self.assertEqual(reason_for("/dev/mmcblk0p3", sysfs=str(root)), "")
            self.assertEqual(reason_for(str(root / "card.img"), sysfs=str(root)), "")

            devices = root / "devices"
            write_dev(devices, "optical", "0", "1", "5")
            write_dev(devices, "stick", "15633408", "1", "0")
            (block / "optical").symlink_to(devices / "optical")
            (block / "stick").symlink_to(devices / "stick")
            self.assertEqual(reason(block, "optical"), OPTICAL)
            self.assertEqual(reason(block, "stick"), "")

    def test_timed_out_open_closes_the_descriptor(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "stick"
            path.write_bytes(b"usb")
            real_open = os.open

            def slow_open(target, flags, mode=0o777):
                time.sleep(0.4)
                return real_open(target, flags, mode)

            before = len(os.listdir("/proc/self/fd"))
            with patch("scripts.block_probe.os.open", slow_open):
                with self.assertRaises(TimeoutError):
                    _open_timeout(str(path), "rb", 0.05)
            time.sleep(0.6)
            self.assertLessEqual(len(os.listdir("/proc/self/fd")), before)


if __name__ == "__main__":
    unittest.main()
