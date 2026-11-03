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
            (1, 0x00, 0, 1, 0, 0): 'sldt_r', (1, 0x03, 2, 0, 0, 0): 'lsl_m', (0, 0x63, 1, 1, 0, 0): 'arpl_r',
            (1, 0x0B, 0, 1, 0, 0): 'default',
        }
        for key, name in cases.items():
            with self.subTest(key=key):
                self.assertEqual(governor.classify(table, *key).name, name)

    def test_branch_and_string_columns(self):
        table = governor.load()
        self.assertEqual(governor.cost(table, 'jcc', '386', 1), 8)
        self.assertEqual(governor.cost(table, 'rep_movs', '286', 1), 4)
        self.assertEqual(governor.cost(table, 'mov_r', '486', 1), governor.cost(table, 'mov_r', '486'))

    def test_isa_cycles_round_exactly(self):
        # An ISA cycle in reference clocks: 720 ns (six 120 ns clocks) is
        # 8.64 clocks at 12 MHz, rounded to 9; 18 at 25 MHz; 360 ns is 4.32
        # and 9. The RTL's fixed point rounds as the exact figure does.
        isa = governor.load().isa
        self.assertEqual((isa.clock_ns, isa.io8, isa.io16, isa.vga, isa.ports16), (120, 6, 3, 6, (0x1F0,)))
        self.assertEqual([isa.clocks(f, 6) for f in (60, 120, 160, 250, 400)], [4, 9, 12, 18, 29])
        self.assertEqual([isa.clocks(f, 3) for f in (120, 250)], [4, 9])
        for frequency in range(1, 1 << governor.FREQUENCY_BITS):
            for cycle in (isa.io8, isa.io16, isa.vga):
                exact = frequency * cycle * isa.clock_ns / 10000
                self.assertLessEqual(abs(isa.clocks(frequency, cycle) - exact), 0.5)

    def test_isa_charges(self):
        table = governor.load()
        presets = {p.name: p for p in table.presets}
        p286, p25 = presets['286-12'], presets['386DX-25']
        # I/O: the cycles, less the data sheet's two-clock bus cycle.
        self.assertEqual(governor.io_charge(table, 120, 0x61, 0), 7)
        self.assertEqual(governor.io_charge(table, 120, 0x61, 1), 16)       # a word: two byte cycles
        self.assertEqual(governor.io_charge(table, 250, 0x300, 2), 70)      # a dword: four
        self.assertEqual(governor.io_charge(table, 250, 0x1F0, 1), 7)       # one 16-bit cycle
        self.assertEqual(governor.io_charge(table, 250, 0x1F0, 2), 16)      # two
        self.assertEqual(governor.io_charge(table, 10, 0x1F0, 0), 0)        # never below zero
        # MMIO: a cycle per 16-bit half, less the bus cycle and the wait states
        # (and a dword's second cycle on a 16-bit bus) already charged.
        self.assertEqual(governor.mmio_charge(table, p286, 120, 1, False), 6)
        self.assertEqual(governor.mmio_charge(table, p286, 120, 2, True), 12)
        self.assertEqual(governor.mmio_charge(table, p25, 250, 2, True), 34)
        self.assertEqual(governor.mmio_charge(table, p286, 10, 1, False), 0)
        self.assertEqual([governor.halves(a, n) for a, n in ((0, 1), (1, 1), (0, 2), (1, 2), (0, 4), (2, 4), (1, 4))],
                         [1, 1, 1, 2, 2, 2, 3])

    def test_port16_is_generated(self):
        package = governor.PACKAGE.read_text()
        self.assertIn("port16 = port == 16'h01f0;", package)
        self.assertIn(f"ISA_STEP = {governor.load().isa.step.bit_length()}'d{governor.load().isa.step};", package)

    def test_bad_isa_is_refused(self):
        for change, message in (
                (lambda s: s.pop('isa'), 'isa has'),
                (lambda s: s['isa'].pop('vga'), 'isa has'),
                (lambda s: s['isa'].update(io8=0), 'io8 is 1 to'),
                (lambda s: s['isa'].update(ports16=['1G0']), 'ports16'),
                (lambda s: s['isa'].update(clock_ns=1000, vga=15), 'over 255 clocks')):
            bad = source()
            change(bad)
            with self.subTest(message=message), self.assertRaisesRegex(governor.GovernorError, message):
                governor.parse(bad)

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
