"""Exercise shared concurrent SD + active FPGA + confirmed Ethernet C firmware."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / "platform/embedded/firmware"


class NanoConcurrentTests(unittest.TestCase):
    def test_concurrent_sd_fpga_and_ethernet_policy(self):
        with tempfile.TemporaryDirectory(prefix="fes-nano-concurrent-") as temp:
            binary = Path(temp) / "concurrent-test"
            flags = [
                "-std=c11", "-Wall", "-Wextra", "-Werror",
                "-fsanitize=undefined", "-fno-sanitize-recover=all",
                "-I" + str(FW),
            ]
            sources = [
                FW / "nano_bridges.c",
                FW / "nano_bridge_sip.c",
                FW / "nano_sd.c",
                FW / "nano_mac.c",
                FW / "nano_usb_service.c",
                FW / "nano_identity.c",
                FW / "nano_configuration.c",
                FW / "tests/nano_concurrent_test.c",
            ]
            subprocess.run(["cc", *flags, *map(str, sources), "-o", str(binary)],
                           check=True, capture_output=True, text=True, timeout=30)
            result = subprocess.run([str(binary)], check=True, capture_output=True, text=True, timeout=10)
            self.assertIn("PASS concurrent SD + active FPGA + confirmed Ethernet", result.stdout)


if __name__ == "__main__":
    unittest.main()
