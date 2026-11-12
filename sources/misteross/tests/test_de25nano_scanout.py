"""DE25 allocated-buffer source composition and real native RTL tests."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest

SCANOUT = Path(__file__).resolve().parents[1] / "boards/de25nano/scanout"
SPEC = importlib.util.spec_from_file_location("de25_scanout_tests", SCANOUT / "test_build.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
ScanoutBuildTests = MODULE.ScanoutBuildTests
LinuxDmaTests = MODULE.LinuxDmaTests


class ScanoutSimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_actual_endpoint_frame_ack_and_clock_monitor(self):
        result = subprocess.run(["make", "-C", str(SCANOUT), "sim"], capture_output=True, text=True, timeout=240)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
