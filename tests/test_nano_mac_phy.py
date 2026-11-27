"""Exercise the firmware's matched CBL PHY sequence and fault paths off-board."""
from pathlib import Path
import subprocess
import tempfile
import unittest


FW = Path(__file__).resolve().parents[1] / "platform/embedded/firmware"


class MacPhyTests(unittest.TestCase):
    def test_matched_control(self):
        with tempfile.TemporaryDirectory() as folder:
            exe = Path(folder) / "mac-phy"
            subprocess.run(["cc", "-std=c11", "-O2", "-Wall", "-Wextra", "-Werror",
                            "-fsanitize=undefined", "-fno-sanitize-recover=all",
                            "-DFES_NANO_MAC_PHY_TEST", "-I", str(FW), str(FW / "nano_mac_phy.c"),
                            str(FW / "tests/nano_mac_phy_test.c"), "-o", str(exe)],
                           check=True, timeout=30)
            subprocess.run([str(exe)], check=True, timeout=15)
