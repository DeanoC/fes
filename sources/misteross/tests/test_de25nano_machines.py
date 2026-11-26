"""DE25 Spectrum, Atari ST, C64 and PC/XT derivation guards and standard-shell simulations."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
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

    @unittest.skipUnless(TOOLS, "requires Verilator and C++")
    def test_pcxt_video_keeps_procassinit_for_other_signals(self):
        # Mutation check: the real DE25 mailbox's power-on values are waived,
        # but a new initialized register in the same generated file must fail.
        board = BOARD / "pcxt"
        root = BOARD.parents[1]
        pcxt, common = root / "cores/fes-pcxt", root / "cores/fes-common"
        spec = importlib.util.spec_from_file_location("pcxt_lint_derive", board / "derive.py")
        derive = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(derive)
        source = derive.retarget_video((pcxt / "rtl/pcxt_video.sv").read_bytes())[0]
        before = b"reg [10:0] horizontal;"
        self.assertEqual(source.count(before), 1)
        with tempfile.TemporaryDirectory(prefix="de25-pcxt-lint-") as tmp:
            video = Path(tmp) / "pcxt_video_de25.sv"
            video.write_bytes(source.replace(before, b"reg [10:0] horizontal = 0;"))
            result = subprocess.run([
                "verilator", "--lint-only", "-Wall", "-Wno-WIDTHEXPAND", "-Wno-DECLFILENAME",
                "--timescale-override", "1ns/1ps", str(pcxt / "sim/cpu.vlt"), str(board / "video.vlt"),
                "--top-module", "video_top", "-I" + str(common / "generated"),
                str(common / "rtl/coleco_video_dpram.v"), str(common / "rtl/fes_video_part_direct.v"),
                str(common / "rtl/fes_video_part_scanlines.v"), str(video),
                str(pcxt / "rtl/pcxt_video_output.sv"), str(pcxt / "sim/video_top.sv"),
            ], cwd=root, capture_output=True, text=True, timeout=60)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("%Warning-PROCASSINIT", result.stderr)
        self.assertIn("'horizontal'", result.stderr)
