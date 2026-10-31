"""DE25 combined HPS/display composition and actual derived-video simulation."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest

COMBINED = Path(__file__).resolve().parents[1] / "boards/de25nano/combined"
SPEC = importlib.util.spec_from_file_location("de25_combined_tests", COMBINED / "test_build.py")
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
CombinedBuildTests = MODULE.CombinedBuildTests


class CombinedSimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which("verilator") and shutil.which("c++"), "requires Verilator and C++")
    def test_derived_video_idle_and_menu_pattern(self):
        result = subprocess.run(["make", "-C", str(COMBINED), "sim"], capture_output=True, text=True, timeout=240)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
