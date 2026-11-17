# SPDX-License-Identifier: MIT
"""The timing gate's claims, presented with designs that violate them.

fit_timing.tcl's gates are assertions about what was analysed, and the only
evidence that one catches what it claims is a design that breaks the claim and
is refused. Running the real script against a stubbed STA API gets that
evidence in milliseconds; the alternative is sweeping a clock domain out of
pc386_system and watching a forty-minute machine fit wave it through.
"""
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
DRIVER = HERE / "run_fit_timing_stub.tcl"
TCLSH = shutil.which("tclsh")


@unittest.skipIf(TCLSH is None, "tclsh is not installed")
class FitTimingGate(unittest.TestCase):
    def run_gate(self, clocks="clk pixel_clk", top="pc386_system", **stub):
        with tempfile.TemporaryDirectory() as temporary:
            environment = dict(os.environ, FES_FIT_CLOCKS=clocks)
            for name, value in stub.items():
                environment[name] = value
            finished = subprocess.run([TCLSH, str(DRIVER), temporary, top],
                                      capture_output=True, text=True, env=environment)
            rows = []
            table = Path(temporary) / "timing.tsv"
            if table.exists():
                lines = table.read_text().splitlines()[1:]
                rows = [line.split("\t") for line in lines]
            return finished, rows

    def test_a_two_clock_design_is_analysed_in_each_domain(self):
        finished, rows = self.run_gate()
        self.assertEqual(finished.returncode, 0, finished.stderr)
        self.assertIn("each of clk_clock/pixel_clk_clock analysed within its own domain",
                      finished.stdout)
        checks = {row[2] for row in rows}
        for expected in ("clk_clock_setup", "clk_clock_hold",
                         "pixel_clk_clock_setup", "pixel_clk_clock_hold"):
            self.assertIn(expected, checks)

    def test_a_swept_second_domain_is_refused(self):
        # The counterexample. Both clock ports survive, so both create_clock
        # objects exist and the clock-count gate is satisfied, but only one
        # domain has registers. The design-wide setup and hold queries are
        # answered by the surviving domain, and before the per-clock
        # requirement this published as analysed at every corner.
        finished, _ = self.run_gate(STUB_PATH_CLOCKS="clk_clock")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("pixel_clk_clock has no setup paths within its own domain",
                      finished.stderr)

    def test_a_swept_second_domain_is_refused_on_a_hold_only_corner(self):
        # A hold-only corner has no setup model, so the refusal there must come
        # from hold rather than silently having nothing to check.
        finished, _ = self.run_gate(STUB_PATH_CLOCKS="clk_clock",
                                    STUB_CORNERS="Slow 100C Model",
                                    STUB_HOLD_ONLY="Slow 100C Model")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("pixel_clk_clock has no hold paths within its own domain",
                      finished.stderr)

    def test_a_hold_only_corner_requires_hold_per_clock_and_not_setup(self):
        finished, rows = self.run_gate(STUB_CORNERS="Slow 100C Model",
                                       STUB_HOLD_ONLY="Slow 100C Model")
        self.assertEqual(finished.returncode, 0, finished.stderr)
        checks = {row[2] for row in rows}
        self.assertIn("clk_clock_hold", checks)
        self.assertIn("pixel_clk_clock_hold", checks)
        self.assertNotIn("clk_clock_setup", checks)

    def test_a_single_clock_design_passes_and_names_its_one_clock(self):
        finished, rows = self.run_gate(clocks="clk", top="fes_i386_cpu",
                                       STUB_CLOCKS="clk_clock")
        self.assertEqual(finished.returncode, 0, finished.stderr)
        self.assertIn("each of clk_clock analysed within its own domain", finished.stdout)
        self.assertNotIn("pixel_clk_clock_setup", {row[2] for row in rows})

    def test_a_missing_clock_is_still_refused_by_the_count_gate(self):
        finished, _ = self.run_gate(STUB_CLOCKS="clk_clock")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("expected exactly 2 clock(s), found 1", finished.stderr)

    def test_a_negative_per_clock_slack_fails_the_fit(self):
        # Scoped paths are gated on slack as the design-wide ones are, so a
        # domain that only misses inside itself cannot pass on another's slack.
        finished, _ = self.run_gate(STUB_SLACK="-0.5")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("timing FAIL", finished.stderr)

    def test_asynchronous_resets_that_vanished_are_refused(self):
        finished, _ = self.run_gate(FES_FIT_ASYNC_RESET="1")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("asynchronous resets", finished.stderr)

    def test_declared_asynchronous_resets_with_paths_pass(self):
        finished, rows = self.run_gate(FES_FIT_ASYNC_RESET="1", STUB_ASYNC_PATHS="1")
        self.assertEqual(finished.returncode, 0, finished.stderr)
        self.assertIn("recovery", {row[2] for row in rows})


if __name__ == "__main__":
    unittest.main()
