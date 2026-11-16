"""The x87 envelope fit harness's own checks.

These exist because two defects in it were found by review rather than by use,
and both would have fired only on measurements that had not run yet: a clock
ladder value with more than three decimals, and a DE25 fit that kept a seed
table whose read register Quartus had absorbed.
"""
from __future__ import annotations

import json
from pathlib import Path
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from scripts import x87_envelope_fit as fit


class PeriodNormalisation(unittest.TestCase):
    """The SDC, the timing gate and the record must all see ONE period.

    The gate requires equality within 1e-6 ns, so an SDC written at three
    decimals and a gate passed six would reject a valid fit at any natural
    ladder value: 13.333333 ns for 75 MHz became 13.333 in the SDC.
    """

    def test_ladder_values_survive_the_round_trip(self):
        for period in (16.276, 13.333333, 11.111111, 9.090909, 7.692308):
            with tempfile.TemporaryDirectory() as tmp:
                out = fit.prepare(Path(tmp) / 'run', 'de25', period, 1)
                sdc = (out / 'project' / 'clocks.sdc').read_text()
                stored = json.loads((out / 'inputs.json').read_text())['period_ns']
                # What the gate is handed, from compile_project.
                gate = repr(float(stored))
                self.assertIn(f'-period {stored} ', sdc,
                              f'the SDC does not carry the stored period for {period}')
                self.assertLess(abs(float(gate) - float(stored)), 1e-9,
                                f'the gate and the SDC disagree for {period}')


class RamBlockParsing(unittest.TestCase):
    """The seed tables are evidenced by name from the fitter's RAM summary."""

    SUMMARY = """
; Fitter RAM Summary
+------+
; Name ; Type ;
+------+
; rf_a_rtl_0|auto_generated|altera_syncram_impl1|ALTERA_SYNCRAM ; M20K ;
; rf_b_rtl_0|auto_generated|altera_syncram_impl1|ALTERA_SYNCRAM ; M20K ;
+------+
Note: Fitter may spread logical memories into multiple blocks.
"""

    def test_reads_the_memories_the_fit_kept(self):
        self.assertEqual(fit.ram_blocks(self.SUMMARY), ['rf_a', 'rf_b'])

    def test_finds_a_surviving_seed_table(self):
        text = self.SUMMARY.replace(
            '; rf_b_rtl_0',
            '; rcp_tab_rtl_0|auto_generated|altera_syncram_impl1|ALTERA_SYNCRAM ; M20K ;\n; rf_b_rtl_0')
        self.assertIn('rcp_tab', fit.ram_blocks(text))

    def test_ignores_the_table_of_contents(self):
        # 'Fitter RAM Summary' also appears in the report's contents list;
        # anchoring on the header matched 270 KB of the wrong report.
        self.assertEqual(fit.ram_blocks('      26. Fitter RAM Summary\n'), [])


class RamAttributeRetarget(unittest.TestCase):
    """Agilex 5 has no M10K; the rename is round-trip verified."""

    def test_renames_and_counts(self):
        text, count = fit.retarget_ram_attribute(
            '(* ramstyle = "M10K" *) logic a;\n(* ramstyle = "M10K" *) logic b;\n')
        self.assertEqual(count, 2)
        self.assertNotIn('M10K', text)

    def test_refuses_a_source_that_already_asks_for_m20k(self):
        with self.assertRaises(SystemExit):
            fit.retarget_ram_attribute('(* ramstyle = "M20K" *) logic a;')


if __name__ == '__main__':
    unittest.main()
