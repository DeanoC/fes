# SPDX-License-Identifier: GPL-2.0-or-later
"""Run independent AY behavior checks against the production cart RTL."""
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-zx81"


class ZonXAySimulationTest(unittest.TestCase):
    def test_ay_behavior(self):
        verilator = shutil.which(os.environ.get("VERILATOR", "verilator"))
        if not verilator:
            self.skipTest("Verilator is required for AY behavior simulation")
        with tempfile.TemporaryDirectory(prefix="fes-zonx-ay-") as directory:
            command = [verilator, "--cc", "--exe", "--build", "--top-module", "zonx_ay",
                       "-Wall", "-Wno-UNUSEDSIGNAL", "--Mdir", directory,
                       str(CORE / "expansions" / "zonx_ay.v"),
                       str(CORE / "sim" / "zonx_ay_tb.cpp")]
            built = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(built.returncode, 0, built.stdout + built.stderr)
            result = subprocess.run([str(Path(directory) / "Vzonx_ay")],
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn("AY reference:", result.stdout)


if __name__ == "__main__":
    unittest.main()
