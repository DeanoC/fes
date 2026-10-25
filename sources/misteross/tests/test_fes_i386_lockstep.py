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
    def test_register_forms_only_where_the_stem_has_a_modrm(self):
        self.assertTrue(lockstep.in_scope('00', moo_test(bytes([0x00, 0xd8, 0xf4]))))      # add al, bl
        self.assertFalse(lockstep.in_scope('00', moo_test(bytes([0x00, 0x18, 0xf4]))))     # add [bx+si], bl
        self.assertTrue(lockstep.in_scope('6601', moo_test(bytes([0x2e, 0x66, 0x01, 0xc0, 0xf4]))))
        self.assertTrue(lockstep.in_scope('0FA4', moo_test(bytes([0x0f, 0xa4, 0xc8, 0x05, 0xf4]))))
        self.assertFalse(lockstep.in_scope('0FA4', moo_test(bytes([0x0f, 0xa4, 0x08, 0x05, 0xf4]))))
        # LEA's memory forms access nothing; its register form is #UD. Both run.
        self.assertTrue(lockstep.in_scope('8D', moo_test(bytes([0x8d, 0x00, 0xf4]))))
        self.assertTrue(lockstep.in_scope('8D', moo_test(bytes([0x8d, 0xc0, 0xf4]))))
        self.assertTrue(lockstep.in_scope('04', moo_test(bytes([0x04, 0x12, 0xf4]))))
        self.assertFalse(lockstep.in_scope('F6.4', moo_test(bytes([0xf6, 0xe0, 0xf4]))))  # MUL comes later
        self.assertFalse(lockstep.in_scope('A0', moo_test(bytes([0xa0, 0x00, 0x00, 0xf4]))))

    def test_unbuilt_encodings_cover_what_scope_leaves_out(self):
        lines = lockstep.unbuilt_encodings()
        self.assertEqual(len(lines), len(set(lines)))
        for encoding in ('06c0', '27c0', '3fff', '0000', 'f6e0', 'a000', '0f0b00'):
            with self.subTest(encoding=encoding):
                self.assertIn(encoding, lines)
        for encoding in ('00c0', '04ff', '8d00', '0fa4c0', 'f4c0'):
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

    def test_cpu_sources_exist(self):
        for name in lockstep.CPU_RTL:
            with self.subTest(source=name):
                self.assertTrue((lockstep.RTL / name).is_file())
        self.assertTrue((lockstep.SIM / 'lockstep_tb.cpp').is_file())


@unittest.skipUnless(VERILATOR and BOCHS and CORPUS, 'Verilator, the pinned Bochs checkout or the corpus is absent')
class LockstepRunTest(unittest.TestCase):
    ARGS = ['--opcode', '00', '--opcode', 'D3.4', '--opcode', '670FAD', '--opcode', '678D', '--opcode', 'E2',
            '--random-programs', '100', '--random-seeds', '2', '--jobs', str(min(os.cpu_count() or 2, 8))]

    def run_lockstep(self, *extra):
        env = dict(os.environ, VERILATOR=VERILATOR)
        result = subprocess.run([sys.executable, 'scripts/test_fes_i386_lockstep.py', *self.ARGS, *extra],
                                cwd=lockstep.ROOT, env=env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn(', 0 fail;', result.stdout)
        self.assertIn('trace unchanged', result.stdout)
        return result.stdout

    def test_rtl_retires_in_lockstep(self):
        self.assertIn('corpus: 5 files, 5 opcodes', self.run_lockstep())

    @unittest.skipUnless(YOSYS, 'FES_I386_YOSYS names no Yosys')
    def test_yosys_netlist_retires_in_lockstep(self):
        self.run_lockstep('--yosys', YOSYS)


if __name__ == '__main__':
    unittest.main()
