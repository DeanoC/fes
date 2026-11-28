from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
NANO = ROOT / "platform/embedded/nano"


class ReceiveDiagnostic(unittest.TestCase):
    def test_receive_and_retire(self):
        with tempfile.TemporaryDirectory() as scratch:
            binary = Path(scratch) / "test-rx"
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-O2", "-I", str(NANO),
                            str(ROOT / "tests/test_nano_mac_rx.c"), str(NANO / "mac_rx_diagnostic.c"),
                            "-o", str(binary)], check=True)
            subprocess.run([binary], check=True, timeout=10)
