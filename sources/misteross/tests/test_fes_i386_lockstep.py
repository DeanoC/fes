# SPDX-License-Identifier: MIT
"""i386 whole-CPU lockstep: the runner's scope, divergence and output handling,
and (when the tools are present) a short lockstep run of the RTL. No network
access. The run needs Verilator, the pinned Bochs checkout and the pinned
SingleStepTests corpus already in build/; the netlist run also needs a Yosys
named by FES_I386_YOSYS."""
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_lockstep as lockstep
from scripts import test_fes_i386_vectors as vectors

VERILATOR = shutil.which(os.environ.get('VERILATOR', 'verilator'))
YOSYS = shutil.which(os.environ.get('FES_I386_YOSYS', '')) if os.environ.get('FES_I386_YOSYS') else None
BOCHS = (oracle.BUILD / ('bochs-' + oracle.BOCHS_REVISION) / '.git').is_dir()
CORPUS = (vectors.OUTPUT / 'corpus' / vectors.REVISION / '00.MOO.gz').is_file()


def moo_test(instruction: bytes, index: int = 0) -> vectors.MooTest:
    return vectors.MooTest(index, 'test', instruction, [0] * len(vectors.REGISTERS), [], 0, {}, [], None, '0' * 40)


class ScopeTest(unittest.TestCase):
    def test_scope_takes_every_form_of_a_stem(self):
        for stem in ('00', '6601', '0FA4', '8D', '04', '8E', 'F6.3', 'FF.1', '0F95', '6650', '0FA9', 'C2', 'FF.4',
                     'C4', '660FB2', 'EA', '669A', 'CB', 'FF.3', 'FF.5', '60', '6661', '9C', '9D', 'CC', 'CD', 'CE',
                     '66CF', '87', '6694', 'A0', '67A3', 'D6', 'D7', 'FF.2', 'FF.6', '8F', '668F',
                     'F6.4', '66F7.5', '67F6.6', 'F7.7', '0FAF', '6669', '6B', 'D4', 'D5', 'A4', '67A5', '6766AF', 'C8',
                     '66C8', 'E4', '66E5', 'EE', '6C', '666D', '676F', '0FBA.4', '670FBA.7', '660FBC', '0FBD',
                     '0FA3', '670FAB', '660FB3', '67660FBB', '62', '676662', '0F06', '9B', '678F', '67668F',
                     '27', '2F', '37', '3F', '0F01', '660F01', '0F20', '0F22'):
            with self.subTest(stem=stem):
                self.assertTrue(lockstep.in_scope(stem))
        # LOADALL; BT's group /0 to /3.
        for stem in ('0F07', '0FBA.0', '0FBA.3'):
            with self.subTest(stem=stem):
                self.assertFalse(lockstep.in_scope(stem))

    def test_unbuilt_encodings_cover_what_scope_leaves_out(self):
        lines = lockstep.unbuilt_encodings()
        self.assertEqual(len(lines), len(set(lines)))
        for encoding in ('0f0700', '0f0b00', '0fbac0', '0fba18'):
            with self.subTest(encoding=encoding):
                self.assertIn(encoding, lines)
        for encoding in ('00c0', '0000', '04ff', '8d00', '0fa4c0', '0fa400', 'f4c0', '06c0', 'ff20', '0fa1c0',
                         'c400', 'ea00', 'ff18', 'ff28', '0fb500', 'ff30', 'a000', 'ff10', '8f00', '9200',
                         'f6e0', 'f7f8', '0fafc0', '69c0', '6b00', 'd40a', 'd500', 'a400', 'afff', 'c800',
                         'e4c0', 'ef00', '6c00', '6fff', '0fbae0', '0fba20', '0fbcc0', '0fbd00', '0fa3c0', '0fbb00',
                         '0fabc0', '0fb3c0', '0fbbc0', '27c0', '2f00', '37ff', '3f90', '6200', '62c0',
                         '0f0600', '9bc0', '9bff', '9b90', '0f0110', '0f01e0', '0f01f8', '0f20c0', '0f22d8'):
            with self.subTest(encoding=encoding):
                self.assertNotIn(encoding, lines)

    def test_scope_names_corpus_stems(self):
        for stem in lockstep.SCOPE:
            with self.subTest(stem=stem):
                self.assertTrue(vectors.STEM.fullmatch(stem), stem)
                self.assertEqual(vectors.base_opcode(stem), stem)


class DivergenceTest(unittest.TestCase):
    def test_table_holds_the_opcode_entries(self):
        entries = vectors.load_divergences()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'table.txt'
            lockstep.divergence_table(entries, path)
            lines = path.read_text().splitlines()
        self.assertEqual(len(lines), sum('opcodes' in entry for entry in entries))
        by_id = {line.split()[0]: line.split() for line in lines}
        self.assertEqual(by_id['shift-af'][1:2] + by_id['shift-af'][3:], ['corpus', 'count>0', 'eflags.AF,stack.flags.AF'])
        self.assertEqual(by_id['invalid-sib-scales-base'][2:], ['*', 'invalid-sib', '*'])

    def test_label_fields_split_by_side(self):
        entries = [
            {'id': 'a', 'files': ['6661'], 'fields': ['esp'], 'follow': 'corpus'},
            {'id': 'b', 'cases': {'E5#422': 'f' * 40}, 'fields': ['eax'], 'follow': 'oracle'},
            {'id': 'c', 'opcodes': ['*'], 'fields': ['*'], 'follow': 'corpus'},
        ]
        test = moo_test(bytes([0x61, 0xf4]))
        self.assertEqual(lockstep.label_fields(entries, '6661', test), (['esp'], []))
        test = vectors.MooTest(422, 'in', bytes([0xe5, 0x1f, 0xf4]), [], [], 0, {}, [], None, 'f' * 40)
        self.assertEqual(lockstep.label_fields(entries, 'E5', test), ([], ['eax']))
        test.hash = 'e' * 40
        self.assertEqual(lockstep.label_fields(entries, 'E5', test), ([], []))


class OutputTest(unittest.TestCase):
    def test_parse_output_takes_the_summary_and_failures(self):
        summary, failures = lockstep.parse_output('FAIL 00#3 @10.eax 1 2\n{"runs": 4, "passed": 3}\n')
        self.assertEqual(summary['runs'], 4)
        self.assertEqual(failures, ['FAIL 00#3 @10.eax 1 2'])
        with self.assertRaises(lockstep.LockstepError):
            lockstep.parse_output('FAIL 00#3 @10.eax 1 2\n')
        with self.assertRaises(lockstep.LockstepError):
            lockstep.parse_output('Segmentation fault\n{"runs": 1}\n')

    def test_holds_are_checked_cycle_exactly(self):
        quiet = {'store_load': {}, 'after_store': {}, 'stack_stack': {}}
        self.assertEqual(lockstep.hold_failures({'shift_consumer': {2: 5}, 'after_shift': {1: 7}, **quiet}), [])
        self.assertEqual(lockstep.hold_failures({'shift_consumer': {1: 3, 2: 5}, 'after_shift': {2: 4}, **quiet}),
                         ['FAIL flag hold: 3 shift_consumer retirements 1 cycles apart',
                          'FAIL flag hold: 4 after_shift retirements 2 cycles apart'])
        self.assertEqual(lockstep.hold_failures({'shift_consumer': {}, 'after_shift': {},
                                                 'store_load': {1: 2, 2: 6, 4: 1}, 'after_store': {1: 9, 2: 3},
                                                 'stack_stack': {1: 8, 2: 1}}),
                         ['FAIL store hold: 2 store_load retirements 1 cycles apart',
                          'FAIL store hold: 3 after_store retirements 2 cycles apart',
                          'FAIL stack hold: 1 stack_stack retirements 2 cycles apart'])

    def test_same_as_compares_the_protected_mode_counts(self):
        def report(pm: int, cs: int) -> dict:
            mode = {'summary': {'pm_retirements': pm, 'cs_rule': cs}}
            runs = {'totals': {'pm_retirements': pm, 'cs_rule': cs}}
            return {'corpus': {'totals': {}, 'by_opcode': {}}, 'random': runs, 'pmode': runs,
                    'directed_cases': mode, 'unreal_cases': mode, 'memory_cases': mode, 'reset_store': mode,
                    'unbuilt': {'encodings': 0}}
        self.assertEqual(lockstep.same_as(report(75, 2), report(75, 2)), [])
        differences = lockstep.same_as(report(74, 3), report(75, 2))
        for where in ('random', 'pmode', 'directed_cases', 'unreal_cases', 'memory_cases', 'reset_store'):
            with self.subTest(where=where):
                self.assertIn(f'{where} pm_retirements: 74 here, 75 at max', differences)
                self.assertIn(f'{where} cs_rule: 3 here, 2 at max', differences)

    def test_cpu_sources_exist(self):
        for name in lockstep.CPU_RTL:
            with self.subTest(source=name):
                self.assertTrue((lockstep.RTL / name).is_file())
        self.assertTrue((lockstep.SIM / 'lockstep_tb.cpp').is_file())


@unittest.skipUnless(VERILATOR and BOCHS and CORPUS, 'Verilator, the pinned Bochs checkout or the corpus is absent')
class LockstepRunTest(unittest.TestCase):
    ARGS = ['--opcode', '00', '--opcode', 'D3.4', '--opcode', '670FAD', '--opcode', '678D', '--opcode', 'E2',
            '--random-programs', '100', '--random-seeds', '2', '--pm-programs', '100',
            '--jobs', str(min(os.cpu_count() or 2, 8))]

    def run_lockstep(self, *extra):
        env = dict(os.environ, VERILATOR=VERILATOR)
        result = subprocess.run([sys.executable, 'scripts/test_fes_i386_lockstep.py', *self.ARGS, *extra],
                                cwd=lockstep.ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(', 0 fail;', result.stdout)
        self.assertIn('directed cases: 40 pass, 0 fail', result.stdout)
        self.assertIn('unreal-mode cases: 6 pass, 0 fail', result.stdout)
        self.assertRegex(result.stdout, r'protected mode: 2 seeds x 100 programs .*: 200 pass, 0 fail;')
        self.assertIn('memory cases (DMA, FLUSH): 26 pass, 0 fail', result.stdout)
        self.assertIn('trace unchanged', result.stdout)
        return result.stdout

    def test_rtl_retires_in_lockstep(self):
        self.assertIn('corpus: 5 files, 5 opcodes', self.run_lockstep())

    @unittest.skipUnless(YOSYS, 'FES_I386_YOSYS names no Yosys')
    def test_yosys_netlist_retires_in_lockstep(self):
        self.run_lockstep('--yosys', YOSYS)


if __name__ == '__main__':
    unittest.main()
