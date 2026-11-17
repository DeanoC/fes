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


class RecordGenerator(unittest.TestCase):
    """docs/validation/2026-11-16-de25-x87-envelope/generate.py produces the
    record's tables. It once defined `integration_table`, `ab_table` and `main`
    TWICE after two rounds of string-replace edits: Python uses the last
    definition, so an edit to the first was dead code and the stale sentence
    kept appearing in the output. Nothing failed -- the file ran, the record
    regenerated, and the claim in it was simply the old one."""

    def test_every_function_is_defined_once(self):
        import ast
        path = (ROOT.parents[1] / 'docs/validation/2026-11-16-de25-x87-envelope/generate.py')
        names = [n.name for n in ast.parse(path.read_text()).body
                 if isinstance(n, ast.FunctionDef)]
        duplicated = sorted({n for n in names if names.count(n) > 1})
        self.assertEqual(duplicated, [], f'defined more than once: {duplicated}')


class GuardedOnlyRegisters(unittest.TestCase):
    """The exclusion gate's absent list is derived from the RTL."""

    def test_includes_each_sequenced_datapath(self):
        regs = fit.guarded_only_registers((fit.RTL / 'fes_i386_x87.sv').read_text())
        for name in ('ua_add', 'ub_add', 'qy_s', 'dv_c', 'gs_n'):
            self.assertIn(name, regs)

    def test_excludes_registers_a_kept_form_writes(self):
        # FPREM is kept in both configurations and writes dv_rem.
        regs = fit.guarded_only_registers((fit.RTL / 'fes_i386_x87.sv').read_text())
        self.assertNotIn('dv_rem', regs)

    def test_an_unguarded_arm_drops_its_registers(self):
        # Q_CHECK arrived unguarded through a merge. Unguarding it must take
        # the registers it writes out of the guarded-only list, which is what
        # makes a DE25 fit fail on them instead of relying on an audit.
        rtl = (fit.RTL / 'fes_i386_x87.sv').read_text()
        broken = rtl.replace('Q_CHECK: if (X87_BACKEND == 0) begin', 'Q_CHECK: begin', 1)
        self.assertNotEqual(rtl, broken)
        before = set(fit.guarded_only_registers(rtl))
        after = set(fit.guarded_only_registers(broken))
        self.assertEqual(before - after, {'dv_c', 'dv_sub'})

    def test_the_default_direction_is_the_de10_arms(self):
        rtl = (fit.RTL / 'fes_i386_x87.sv').read_text()
        self.assertEqual(fit.guarded_only_registers(rtl), fit.guarded_only_registers(rtl, 0))


class De25OnlyRegisters(unittest.TestCase):
    """The mirror direction (#1958): registers written only inside arms
    guarded by `X87_BACKEND == 1`, which a DE10 build must not hold."""

    BCD = {'bl_acc', 'bl_dig', 'bl_n', 'bs_bin', 'bs_bcd', 'bs_n'}

    def test_is_exactly_the_packed_decimal_registers(self):
        regs = fit.guarded_only_registers((fit.RTL / 'fes_i386_x87.sv').read_text(), 1)
        self.assertEqual(set(regs), self.BCD)

    def test_the_two_directions_share_nothing(self):
        rtl = (fit.RTL / 'fes_i386_x87.sv').read_text()
        self.assertEqual(set(fit.guarded_only_registers(rtl, 0)) & set(fit.guarded_only_registers(rtl, 1)), set())

    def test_an_unguarded_arm_drops_its_registers(self):
        # The mirror of the Q_CHECK test: T_BST without its guard would be
        # built into a DE10 unit, and exactly the registers it writes must
        # leave the list, so a DE10 fit stops being required to lack them
        # by an audit and starts failing on them.
        rtl = (fit.RTL / 'fes_i386_x87.sv').read_text()
        broken = rtl.replace('T_BST: if (X87_BACKEND == 1) begin', 'T_BST: begin', 1)
        self.assertNotEqual(rtl, broken)
        before = set(fit.guarded_only_registers(rtl, 1))
        after = set(fit.guarded_only_registers(broken, 1))
        self.assertEqual(before - after, {'bs_bin', 'bs_bcd', 'bs_n'})
        self.assertEqual(after, {'bl_acc', 'bl_dig', 'bl_n'})

    def test_refuses_another_backend_value(self):
        with self.assertRaises(ValueError):
            fit.guarded_only_registers((fit.RTL / 'fes_i386_x87.sv').read_text(), 2)



class EnginePresence(unittest.TestCase):
    """The transcendental engine is an entity, so it is found by name."""

    def test_finds_the_entity_and_its_memories(self):
        rows = [{'name': 'fes_i386_x87', 'alms': '5000'}, {'name': '|fes_x87_trans:trans|', 'alms': '1200'}]
        found = fit.engine_presence(rows, ['rf_a', 'program_rom', 'constant_rom'])
        self.assertEqual(found['entities'], ['|fes_x87_trans:trans|'])
        self.assertEqual(found['memories'], ['constant_rom', 'program_rom'])

    def test_a_fit_without_it_is_empty(self):
        found = fit.engine_presence([{'name': 'fes_i386_x87'}, {'name': 'fes_x87_core:core'}], ['rf_a', 'rf_b'])
        self.assertEqual(found, {'entities': [], 'memories': []})

    def test_the_rtl_still_names_what_the_check_looks_for(self):
        # If the engine or its memories are renamed, the check would find
        # nothing and pass for the wrong reason.
        rtl = fit.RTL
        self.assertIn(f'module {fit.ENGINE_ENTITY} ', (rtl / 'fes_x87_trans.sv').read_text())
        text = (rtl / 'fes_x87_trans.sv').read_text() + (rtl / 'fes_x87_trans_rom.sv').read_text()
        for memory in fit.ENGINE_MEMORIES:
            self.assertRegex(text, rf'\b{memory} \[0:')

if __name__ == '__main__':
    unittest.main()
