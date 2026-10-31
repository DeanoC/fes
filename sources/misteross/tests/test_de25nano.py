"""Include the standalone board build checks in the FPGA software lane."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest


BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano"
SPEC = importlib.util.spec_from_file_location("de25nano_build_tests", BOARD / "test_build.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
DiagnosticBuildTests = MODULE.DiagnosticBuildTests


class DiagnosticSimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"),
                         "DE25-Nano RTL simulation requires Verilator and a C++ compiler")
    def test_board_clock_and_led_sequence(self):
        result = subprocess.run(["make", "-C", str(BOARD), "sim"],
                                capture_output=True, text=True, timeout=120)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
