"""DE25 ZX81 build guards and the real machine/board simulation."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest

BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano/zx81"
SPEC = importlib.util.spec_from_file_location("de25_zx81_tests", BOARD / "test_build.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
ZX81BuildTests = MODULE.ZX81BuildTests


class ZX81SimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_real_machine_on_de25_board(self):
        result = subprocess.run(["make", "-C", str(BOARD), "sim"],
                                capture_output=True, text=True, timeout=600)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
