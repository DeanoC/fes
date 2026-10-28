"""Exercise the standalone HPS mailbox builder and real diagnostic RTL."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest


HPS = Path(__file__).resolve().parents[1] / "boards/de25nano/hps"
SPEC = importlib.util.spec_from_file_location("de25_hps_build_tests", HPS / "test_build.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
HpsBuildTests = MODULE.HpsBuildTests


class HpsMailboxSimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"),
                         "HPS mailbox RTL simulation requires Verilator and C++")
    def test_mailbox_transactions(self):
        result = subprocess.run(["make", "-C", str(HPS), "sim"],
                                text=True, capture_output=True, timeout=120)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
