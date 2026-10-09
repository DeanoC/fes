# SPDX-License-Identifier: MIT
"""Route C Z80 semantics: named debug ports vs current-main, real Verilator.

The unresolved merge's overlapping ALU datapaths must not compile. After
reconciliation the working engine must match pinned-main named-state programs.
This is host simulation, not a Yosys next-state proof.
"""
import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / "cores/fes-common/rtl/z80"
SIM = ROOT / "cores/fes-common/sim/z80"
ENGINE = RTL / "fes_z80_engine.sv"
ALU = RTL / "fes_z80_alu.sv"
TB = SIM / "route_c_semantics_tb.cpp"
MARKER_RE = re.compile(
    r"^<<<<<<< HEAD\n(.*?)\n=======\n(.*?)\n>>>>>>> [^\n]+\n",
    re.M | re.S,
)


def _verilator():
    return shutil.which(os.environ.get("VERILATOR", "verilator"))


def keep_both_conflict_bodies(text: str) -> str:
    """Naive unresolved merge: retain both sides, drop only the markers."""
    return MARKER_RE.sub(lambda match: match.group(1) + "\n" + match.group(2) + "\n", text)


def _build_and_run(engine_text: str, nmos: bool, directory: Path) -> subprocess.CompletedProcess:
    engine = directory / "fes_z80_engine.sv"
    engine.write_text(engine_text)
    shutil.copy(ALU, directory / "fes_z80_alu.sv")
    shutil.copy(TB, directory / "route_c_semantics_tb.cpp")
    defines = ["-DZ80_ROUTE_C_NMOS"] if nmos else []
    params = ["-GNMOS=1'b1"] if nmos else ["-GNMOS=1'b0"]
    built = subprocess.run(
        [_verilator(), "--cc", "--exe", "--build", "--top-module", "fes_z80_engine",
         "-Wall", "-Wno-UNUSEDSIGNAL", "--Mdir", str(directory / "obj"),
         "-CFLAGS", "-std=c++17 -O2" + (" -DZ80_ROUTE_C_NMOS" if nmos else ""),
         *params, *defines,
         str(engine), str(directory / "fes_z80_alu.sv"),
         str(directory / "route_c_semantics_tb.cpp")],
        capture_output=True, text=True, cwd=ROOT)
    if built.returncode:
        return built
    return subprocess.run(
        [str(directory / "obj" / "Vfes_z80_engine")],
        capture_output=True, text=True, cwd=ROOT)


class RouteCZ80SemanticsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not _verilator():
            raise unittest.SkipTest("Verilator is required for Z80 named-state programs")

    def test_pinned_main_named_state_programs_pass_nmos_and_fast(self):
        repo = ROOT.parents[1]
        main_ref = "MERGE_HEAD" if (repo / ".git" / "MERGE_HEAD").exists() else "01db9555b3d1e77d7bbf1a7b81d733d98a341999"
        main = subprocess.check_output(
            ["git", "show", f"{main_ref}:sources/misteross/cores/fes-common/rtl/z80/fes_z80_engine.sv"],
            cwd=repo, text=True)
        for nmos in (True, False):
            with self.subTest(nmos=nmos), tempfile.TemporaryDirectory(prefix="fes-z80-main-") as directory:
                result = _build_and_run(main, nmos, Path(directory))
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("named-state programs", result.stdout)

    def test_working_engine_matches_main_named_state_or_rejects_overlap(self):
        text = ENGINE.read_text()
        unresolved = "<<<<<<<" in text
        candidate = keep_both_conflict_bodies(text) if unresolved else text
        failures = []
        for nmos in (True, False):
            with tempfile.TemporaryDirectory(prefix="fes-z80-work-") as directory:
                result = _build_and_run(candidate, nmos, Path(directory))
                log = result.stdout + result.stderr
                if unresolved:
                    if result.returncode == 0:
                        failures.append(f"nmos={nmos}: overlapping datapaths compiled")
                    elif "<<<<<<<" in log:
                        failures.append(f"nmos={nmos}: failed only on conflict-marker syntax")
                    else:
                        # Genuine overlapping-driver / parse failure of keep-both bodies.
                        self.assertNotEqual(result.returncode, 0)
                else:
                    self.assertEqual(result.returncode, 0, log)
                    self.assertIn("named-state programs", result.stdout)
        if unresolved:
            self.assertFalse(failures, "\n".join(failures))
            self.fail("working engine still has overlapping unresolved datapaths")


if __name__ == "__main__":
    unittest.main()
