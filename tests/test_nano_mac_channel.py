"""Verify the private CBL channel's register, DMA and retirement contracts."""
from pathlib import Path
import subprocess
import tempfile
import unittest


FW = Path(__file__).resolve().parents[1] / "platform/embedded/firmware"


class MacChannelTests(unittest.TestCase):
    def test_matched_channel(self):
        with tempfile.TemporaryDirectory() as folder:
            exe = Path(folder) / "mac-channel"
            subprocess.run(["cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror",
                            "-fsanitize=undefined", "-fno-sanitize-recover=all",
                            "-DFES_NANO_MAC_CHANNEL_TEST", "-I", str(FW), str(FW / "nano_mac_channel.c"),
                            str(FW / "tests/nano_mac_channel_test.c"), "-lcrypto", "-o", str(exe)],
                           check=True, timeout=30)
            subprocess.run([str(exe)], check=True, timeout=15)
