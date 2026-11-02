"""i386 speed governor tables: the generator's checks, and that the checked-in files are current."""
from __future__ import annotations

from pathlib import Path
import re
import sys
import tomllib
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i386_governor as governor
from scripts import test_fes_i386_lockstep as lockstep

ROOT = Path(__file__).resolve().parents[1]
COMPUTER_IO = ROOT.parent / 'mister-packages/docs/computer-io.md'


def source() -> dict:
    with governor.SOURCE.open('rb') as stream:
        return tomllib.load(stream)


class GeneratedTest(unittest.TestCase):
    def test_checked_in_files_are_current(self):
        for path, text in governor.generate().items():
            with self.subTest(path=path.name):
                self.assertEqual(path.read_text(), text)

    def test_key_matches_the_package(self):
        # gov_t's fields, in order, as fes_i386_pkg declares them: the key, then ea3.
        package = (governor.RTL / 'fes_i386_pkg.sv').read_text()
        end = package.index('} gov_t;')
        body = package[package.rindex('typedef struct packed {', 0, end):end]
        declared = []
        for line in body.splitlines():
            match = re.fullmatch(r'logic\s*(\[(\d+):0\])?\s*(\w+);', line.split('//')[0].strip())
            if match:
                declared.append((match.group(3), int(match.group(2)) + 1 if match.group(2) else 1))
        self.assertEqual(declared, [*governor.KEY_FIELDS, ('ea3', 1)])


class TableTest(unittest.TestCase):
    def test_every_built_encoding_has_a_class(self):
        self.assertEqual(governor.scope_gaps(governor.load(), lockstep.SCOPE), [])

    def test_presets_are_the_records(self):
        # mister-packages docs/computer-io.md, fes.pc386.machine word 0.
        row = next(line for line in COMPUTER_IO.read_text().splitlines() if line.startswith('| 0 | Governor preset'))
        names = dict((int(n), name) for n, name in re.findall(r'(\d) ([0-9A-Z]+-[0-9]+)', row))
        table = governor.load()
        self.assertEqual({p.id: p.name for p in table.presets}, names)
        for p in table.presets:
            self.assertEqual(p.frequency, round(float(re.search(r'-(\d+)$', p.name).group(1)) * 10 + (
                3 if p.name.endswith('33') else 7 if p.name.endswith('66') else 0)), p.name)

    def test_classify_examples(self):
        table = governor.load()
        cases = {
            (0, 0x8B, 0, 0, 0, 0): 'mov_load', (0, 0x8B, 0, 1, 0, 0): 'mov_r', (0, 0xA4, 0, 0, 0, 1): 'rep_movs',
            (0, 0xA4, 0, 0, 0, 0): 'movs', (1, 0x84, 0, 0, 0, 0): 'jcc', (0, 0xF7, 6, 1, 1, 0): 'div32_r',
            (0, 0xF7, 6, 1, 0, 0): 'div16_r', (0, 0x83, 7, 0, 0, 0): 'test_m', (0, 0xCD, 0, 0, 0, 0): 'int',
            (1, 0x00, 0, 1, 0, 0): 'default',
        }
        for key, name in cases.items():
            with self.subTest(key=key):
                self.assertEqual(governor.classify(table, *key).name, name)

    def test_branch_and_string_columns(self):
        table = governor.load()
        self.assertEqual(governor.cost(table, 'jcc', '386', 1), 8)
        self.assertEqual(governor.cost(table, 'rep_movs', '286', 1), 4)
        self.assertEqual(governor.cost(table, 'mov_r', '486', 1), governor.cost(table, 'mov_r', '486'))

    def test_overlapping_stems_are_refused(self):
        bad = source()
        bad['class'].append({'name': 'again', 'stems': ['8B/m'], 'cost': [1, 1, 1]})
        with self.assertRaisesRegex(governor.GovernorError, 'also in mov_load'):
            governor.parse(bad)

    def test_bad_entries_are_refused(self):
        for change, message in (
                (lambda s: s['preset'].pop(), 'presets must be'),
                (lambda s: s['preset'][0].update(frequency=1001), 'frequency'),
                (lambda s: s['class'][0].update(cost=[1, 2]), 'clock counts'),
                (lambda s: s['class'][0].update(stems=['8G']), 'not a stem'),
                (lambda s: s['class'][0].update(taken=[1, 1, 1], element=[1, 1, 1]), 'not both')):
            bad = source()
            change(bad)
            with self.subTest(message=message), self.assertRaisesRegex(governor.GovernorError, message):
                governor.parse(bad)


if __name__ == '__main__':
    unittest.main()
