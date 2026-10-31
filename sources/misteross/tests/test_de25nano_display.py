"""Board-specific DE25 standalone display preparation and actual-video RTL tests."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import unittest

DISPLAY = Path(__file__).resolve().parents[1] / 'boards/de25nano/display'
SPEC = importlib.util.spec_from_file_location('de25_display_tests', DISPLAY / 'test_build.py')
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
DisplayBuildTests = MODULE.DisplayBuildTests

class DisplaySimulationTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which('verilator') and shutil.which('c++'), 'requires Verilator and C++')
    def test_idle_and_menu_pattern_board_rtl(self):
        result = subprocess.run(['make', '-C', str(DISPLAY), 'sim'], capture_output=True, text=True, timeout=240)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
