"""DE25 Spectrum, Atari ST, C64 and PC/XT derivation guards and standard-shell simulations."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import sys
import unittest

BOARD = Path(__file__).resolve().parents[1] / "boards/de25nano"
TOOLS = shutil.which("verilator") and shutil.which("c++")


def suite(name, directory):
    spec = importlib.util.spec_from_file_location(name, BOARD / directory / "test_build.py")
    result = importlib.util.module_from_spec(spec)
    sys.modules[name] = result
    spec.loader.exec_module(result)
    return result.Derivations


SpectrumDerivations = suite("de25_spectrum_build_tests", "spectrum")
AtariSTDerivations = suite("de25_atari_st_build_tests", "atari-st")
C64Derivations = suite("de25_c64_build_tests", "c64")
PCXTDerivations = suite("de25_pcxt_build_tests", "pcxt")


class MachineSimulationTests(unittest.TestCase):
    def simulate(self, directory, timeout):
        result = subprocess.run([sys.executable, "sim.py"], cwd=BOARD / directory,
                                capture_output=True, text=True, timeout=timeout)
        self.assertEqual(result.returncode, 0, result.stdout[-4000:] + result.stderr[-4000:])

    @unittest.skipUnless(TOOLS, "requires Verilator and C++")
    def test_spectrum_models_on_the_standard_shell(self):
        self.simulate("spectrum", 1800)

    @unittest.skipUnless(TOOLS, "requires Verilator and C++")
    def test_atari_st_on_the_standard_shell(self):
        self.simulate("atari-st", 2400)

    @unittest.skipUnless(TOOLS, "requires Verilator and C++")
    def test_c64_diagnostic_on_the_standard_shell(self):
        self.simulate("c64", 2400)

    @unittest.skipUnless(TOOLS, "requires Verilator and C++")
    def test_pcxt_on_the_standard_shell(self):
        self.simulate("pcxt", 3600)
