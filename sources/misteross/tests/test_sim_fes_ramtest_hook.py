# SPDX-License-Identifier: GPL-2.0-or-later
"""The RAM tester's guard check must run with its configured simulation tools."""
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

from scripts import sim_fes_ramtest

ROOT = Path(__file__).resolve().parents[1]


class GuardHookTest(unittest.TestCase):
    def test_relative_tools_are_absolute_in_guard_subprocess(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            tools = directory / "bin"
            tools.mkdir()
            for name in ("verilator", "make", "c++"):
                path = tools / name
                path.write_text("#!/bin/sh\nexit 0\n")
                path.chmod(0o755)
            previous = Path.cwd()
            try:
                os.chdir(directory)
                with patch.dict(os.environ, {"PATH": "bin", "VERILATOR": "bin/verilator"}, clear=True), \
                        patch.object(sim_fes_ramtest.subprocess, "run") as run:
                    result = sim_fes_ramtest.guard_regression()
            finally:
                os.chdir(previous)
            self.assertEqual(result, str(tools / "verilator"))
            self.assertEqual(run.call_args.kwargs["cwd"], ROOT)
            env = run.call_args.kwargs["env"]
            self.assertEqual(env["VERILATOR"], result)
            self.assertEqual(env["PATH"], str(tools))
            self.assertEqual(env["FES_HPS_DDR_GUARD_REQUIRED"], "1")
            self.assertTrue(run.call_args.kwargs["check"])

    def test_required_guard_test_fails_when_tools_are_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            result = subprocess.run(
                [sys.executable, "-m", "unittest", "-v", "tests.test_fes_hps_ddr_guard"],
                cwd=ROOT, env={"PATH": tmp, "VERILATOR": str(Path(tmp) / "missing"),
                               "FES_HPS_DDR_GUARD_REQUIRED": "1"},
                capture_output=True, text=True, timeout=10)
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("missing guard simulation tools", result.stderr)
        self.assertNotIn("skipped", result.stderr)


if __name__ == "__main__":
    unittest.main()
