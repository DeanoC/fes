"""i386 microcode ROM: the generator's checks, and that the checked-in ROM is current."""
from __future__ import annotations

from pathlib import Path
import re
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i386_ucode as ucode


def routine(name, uops, **extra):
    return {'name': name, 'uops': uops, **extra}


class GeneratedTest(unittest.TestCase):
    def test_checked_in_rom_is_current(self):
        for path, text in ucode.generate().items():
            with self.subTest(path=path.name):
                self.assertEqual(path.read_text(), text)

    def test_fields_match_the_package(self):
        # ucode_t's fields, in order, as fes_i386_pkg declares them.
        package = (ucode.RTL / 'fes_i386_pkg.sv').read_text()
        end = package.index('} ucode_t;')
        body = package[package.rindex('typedef struct packed {', 0, end):end]
        declared = []
        for line in body.splitlines():
            line = line.split('//')[0].strip()
            match = re.fullmatch(r'logic\s*(\[(\d+):0\])?\s*(\w+);', line)
            if match:
                declared.append((match.group(3), int(match.group(2)) + 1 if match.group(2) else 1))
        self.assertEqual(declared, list(ucode.FIELDS))

    def test_word_address_width_matches_dc(self):
        package = (ucode.RTL / 'fes_i386_pkg.sv').read_text()
        self.assertRegex(package, rf'logic \[{(ucode.DEPTH - 1).bit_length() - 1}:0\]\s+upc;')


class AssembleTest(unittest.TestCase):
    def test_routines_end_and_fall_through(self):
        words, entries = ucode.assemble({'routine': [
            routine('a', [{'cls': 'alu'}], falls_through=True),
            routine('b', [{'cls': 'alu'}, {'cls': 'branch', 'op': 'jmp'}]),
        ]})
        self.assertEqual(entries, {'A': 0, 'B': 1})
        last = 1 << (ucode.WIDTH - 1)
        self.assertEqual([bool(word & last) for word in words], [False, False, True])

    def test_the_instructions_register(self):
        # s1 or s2 = "reg" sets that source's valid bit and rg, its index 0.
        field = {name: n for n, (name, _) in enumerate(ucode.FIELDS)}
        def bits(word):
            values, shift = {}, ucode.WIDTH
            for name, width in ucode.FIELDS:
                shift -= width
                values[name] = word >> shift & ((1 << width) - 1)
            return values
        words, _ = ucode.assemble({'routine': [routine('a', [{'s1': 'reg'}, {'s2': 'reg'}, {'s2': 'edi'}])]})
        self.assertIn('rg', field)
        self.assertEqual([(bits(w)['s1'], bits(w)['s2'], bits(w)['s2_idx'], bits(w)['rg']) for w in words],
                         [(1, 0, 0, 1), (0, 1, 0, 1), (0, 1, 7, 0)])

    def test_rejects(self):
        for table, message in (
            ({'routine': []}, 'no routines'),
            ({'routine': [routine('a', [])]}, 'no uops'),
            ({'routine': [routine('a', [{'bogus': 1}])]}, 'unknown field'),
            ({'routine': [routine('a', [{'stk': 'sideways'}])]}, 'not a known value'),
            ({'routine': [routine('a', [{'s1': 'r9'}])]}, 'not a GPR'),
            ({'routine': [routine('a', [{'op': 99}])]}, 'does not fit'),
            ({'routine': [routine('a', [{}]), routine('a', [{}])]}, 'defined twice'),
            ({'routine': [routine('a', [{}], falls_through=True)]}, 'falls through'),
            ({'routine': [routine('a', [{}] * (ucode.DEPTH + 1))]}, 'do not fit'),
        ):
            with self.subTest(message=message):
                with self.assertRaisesRegex(ucode.UcodeError, message):
                    ucode.assemble(table)


if __name__ == '__main__':
    unittest.main()
