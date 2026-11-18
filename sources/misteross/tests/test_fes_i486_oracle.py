# SPDX-License-Identifier: MIT
"""The level-4 oracle provisioner, the directed 486 cases and their runner's
rules. No network access and no Bochs build."""
import fnmatch
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import fes_i386_oracle as level3
from scripts import fes_i486_cases as cases
from scripts import fes_i486_oracle as level4
from scripts import test_fes_i486_lockstep as runner


class ProvisionerTest(unittest.TestCase):
    def test_configure_differs_from_level_3_only_in_the_level(self):
        self.assertEqual(len(level4.BOCHS_CONFIGURE), len(level3.BOCHS_CONFIGURE))
        changed = [(a, b) for a, b in zip(level3.BOCHS_CONFIGURE, level4.BOCHS_CONFIGURE) if a != b]
        self.assertEqual(changed, [('--enable-cpu-level=3', '--enable-cpu-level=4')])

    def test_shares_the_pin_and_adapter_sources(self):
        self.assertEqual(level4.BOCHS_REVISION, level3.BOCHS_REVISION)
        self.assertEqual(level4.ORACLE_SOURCES, level3.ORACLE_SOURCES)
        self.assertEqual(level4.LINKED_NAMES, (*level3.LINKED_NAMES, 'cpudb'))

    def test_adapter_and_testbench_name_both_levels(self):
        adapter = (level4.SIM / 'bochs_oracle.cpp').read_text()
        self.assertIn('"bochs-level4"', adapter)
        self.assertIn('"bochs-level3"', adapter)
        testbench = (level4.SIM / 'lockstep_tb.cpp').read_text()
        # Only the i486 mode runs against the level-4 oracle, and it against no other.
        self.assertIn('if (i486 != (mode == "i486"))', testbench)

    def test_level_3_sweeps_cannot_delete_level_4_outputs(self):
        # fes_i386_oracle.py deletes bochs-level3-* trees under BUILD and, in
        # BUILD/bin, bochs_oracle-*.o and <testbench>-* files.
        self.assertFalse(fnmatch.fnmatch(level4.TREE_PREFIX + '0123456789abcdef', 'bochs-level3-*'))
        self.assertEqual(level4.OUTPUT.parent, level3.BUILD)
        self.assertNotEqual(level4.OUTPUT, level3.BUILD / 'bin')

    def test_record_names_the_level_and_both_provisioners(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            tree = root / (level4.TREE_PREFIX + 'k')
            (tree / 'instrument/fes').mkdir(parents=True)
            inputs = [(level4.SIM / name).read_bytes() for name in level4.ORACLE_SOURCES]
            (tree / 'instrument/fes/instrument.h').write_bytes(inputs[2])
            linked = [root / f'bochs_oracle-{level4._adapter_key(tree, inputs)}.o', tree / 'fes-paramtree.o',
                      *(root / name for name in ('cpu.a', 'fpu.a', 'softfloat.a', 'cpudb.a'))]
            for path in linked:
                path.write_bytes(path.name.encode())
            record = level4.build_record(linked)
            self.assertEqual(record['cpu_level'], 4)
            self.assertEqual(record['provisioner_sha256'], level4.PROVISIONER_SHA256)
            self.assertEqual(record['level3_provisioner_sha256'], level3.PROVISIONER_SHA256)
            self.assertEqual(record['adapter_sha256'], level3.adapter_source_sha256(level3.SIM))
            self.assertEqual(set(record['linked_sha256']), set(level4.LINKED_NAMES))
            # A level-3 tree is not a level-4 build.
            other = root / 'bochs-level3-k'
            tree.rename(other)
            with self.assertRaises(level4.OracleError):
                level4.build_record([linked[0], other / 'fes-paramtree.o', *linked[2:]])

    def test_a_changed_provisioner_is_refused(self):
        with mock.patch.object(level4, 'PROVISIONER_SHA256', '0' * 64):
            with self.assertRaisesRegex(level4.OracleError, 'level-4 provisioner changed'):
                level4.require_provisioner_unchanged()


class CasesTest(unittest.TestCase):
    def setUp(self):
        self.cases = cases.cases()

    def test_labels_are_unique_and_every_row_is_sourced(self):
        labels = [case.label for case in self.cases]
        self.assertEqual(len(labels), len(set(labels)))
        for case in self.cases:
            self.assertTrue(case.source and case.what and case.expect.get('end'), case.label)
            # A row where Bochs is not a 486 says what a 486 does instead.
            self.assertEqual(case.divergence is None, case.i486 is None, case.label)

    def test_the_delta_is_covered(self):
        text = ' '.join(case.label for case in self.cases)
        for item in ('bswap', 'xadd', 'cmpxchg', 'invd', 'wbinvd', 'invlpg', 'cr0-', 'wp-', 'ac-toggle', 'id-toggle',
                     'ac-fault', 'cpuid-0', 'cpuid-1', 'mov-tr3', 'mov-cr4'):
            self.assertIn(item, text)

    def test_lines_place_code_at_1000h_and_the_probe_inside_it(self):
        for case in self.cases:
            label, probe, runs = case.line().split(' ')
            self.assertEqual(label, case.label)
            placed = {int(run.split(':')[0], 16): bytes.fromhex(run.split(':')[1]) for run in runs.split(';')}
            self.assertEqual(placed, case.image)
            code = placed[cases.CODE]
            if case.probe is None:
                self.assertEqual(probe, '0')
            else:
                self.assertTrue(cases.CODE <= int(probe, 16) < cases.CODE + len(code), case.label)
            for address, data in placed.items():
                self.assertLess(address + len(data), 0x30000, case.label)  # what i486_tb clears between cases

    def test_reset_row_has_the_adapted_values(self):
        self.assertEqual(cases.RESET['edx'][0], 0x480)
        self.assertEqual(cases.RESET['dr6'][0], 0xffff0ff0)
        self.assertEqual(cases.RESET['cr0'][0], cases.START_CR0)


class RunnerRulesTest(unittest.TestCase):
    def case(self, probe):
        return cases.Case('c', 'source', {'end': 'halt'}, {cases.CODE: b'\xf4'}, probe)

    def test_a_stop_before_the_probe_is_not_isolated(self):
        row = runner.judge([self.case(8)], {'c': 'stop @1004.eax 1 2'}, None)[0]
        self.assertFalse(row['isolated'])
        row = runner.judge([self.case(8)], {'c': 'stop @1008.unexpected-exception 0 6'}, None)[0]
        self.assertTrue(row['isolated'])

    def test_a_case_with_no_probe_must_agree(self):
        self.assertFalse(runner.judge([self.case(None)], {'c': 'stop @1000.eax 1 2'}, None)[0]['isolated'])
        self.assertTrue(runner.judge([self.case(None)], {'c': 'agree'}, None)[0]['isolated'])

    def test_a_stop_that_names_no_eip_is_not_isolated(self):
        self.assertFalse(runner.judge([self.case(0)], {'c': 'stop limit 0 1'}, None)[0]['isolated'])

    def test_the_expected_list_is_exact(self):
        expected = {'c': 'stop @1008.unexpected-exception 0 6'}
        self.assertTrue(runner.judge([self.case(8)], dict(expected), expected)[0]['as_expected'])
        self.assertFalse(runner.judge([self.case(8)], {'c': 'agree'}, expected)[0]['as_expected'])
        self.assertFalse(runner.judge([self.case(8)], {'c': 'stop @1008.eax 1 2'}, expected)[0]['as_expected'])

    def test_expected_list_round_trip_and_duplicates(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'stops.txt'
            with mock.patch.object(runner, 'EXPECTED', path):
                runner.write_expected([self.case(8)], {'c': 'stop @1008.eax 1 2'})
                self.assertEqual(runner.read_expected(), {'c': 'stop @1008.eax 1 2'})
                path.write_text(path.read_text() + 'c agree\n')
                with self.assertRaisesRegex(runner.I486Error, 'twice'):
                    runner.read_expected()

    def test_committed_list_names_exactly_the_cases(self):
        self.assertEqual(set(runner.read_expected()), {case.label for case in cases.cases()})

    def test_ending_prefers_the_exception(self):
        self.assertEqual(runner.ending({'end': 'halt', 'exception': '17', 'int': '-'}), 'exception:17')
        self.assertEqual(runner.ending({'end': 'halt', 'exception': '-', 'int': '3'}), 'int:3')
        self.assertEqual(runner.ending({'end': 'halt', 'exception': '-', 'int': '-'}), 'halt')


if __name__ == '__main__':
    unittest.main()
