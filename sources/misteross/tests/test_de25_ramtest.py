"""Discover DE25 RAM Tester preparation, offline JTAG and real RTL checks."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest

RAMTEST = Path(__file__).resolve().parents[1] / "boards/de25nano/ramtest"


def suite(name):
    spec = importlib.util.spec_from_file_location("de25_ramtest_" + name, RAMTEST / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


RamtestBuildTests = suite("test_build").RamtestBuildTests
RamtestJtagTests = suite("test_jtag").RamtestJtagTests


class RamtestSimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_actual_scan_endpoints_and_board_sdram_controller(self):
        result = subprocess.run(["make", "-C", str(RAMTEST), "sim"], capture_output=True, text=True, timeout=240)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
