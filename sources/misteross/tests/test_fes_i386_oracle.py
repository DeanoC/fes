# SPDX-License-Identifier: MIT
"""i386 oracle corpus runners: MOO reading, failure classification, the
divergence list and test386 listing handling. No network access; the oracle
smoke test runs only when the pinned Bochs checkout is already in build/."""
import struct
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock

from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_test386 as t386
from scripts import test_fes_i386_vectors as vectors


def chunk(tag: str, payload: bytes) -> bytes:
    return tag.encode('ascii') + struct.pack('<I', len(payload)) + payload


def registers(values: dict[int, int]) -> bytes:
    mask = sum(1 << index for index in values)
    return struct.pack('<I', mask) + b''.join(struct.pack('<I', values[index]) for index in sorted(values))


def ram(entries) -> bytes:
    return struct.pack('<I', len(entries)) + b''.join(struct.pack('<IB', a, v) for a, v in entries)


def moo_test(index=0, initial=None, final=None, initial_ram=(), final_ram=(), exception=None,
             digest=b'\x11' * 20, extra=b'') -> bytes:
    initial = initial or {n: 0 for n in range(20)}
    name = b'add [bx],al'
    parts = (chunk('NAME', struct.pack('<I', len(name)) + name)
             + chunk('BYTS', struct.pack('<I', 3) + b'\x00\x07\xf4')
             + chunk('INIT', chunk('RG32', registers(initial)) + chunk('RAM ', ram(initial_ram)))
             + chunk('FINA', chunk('RG32', registers(final or {})) + chunk('RAM ', ram(final_ram)))
             + (chunk('EXCP', struct.pack('<BI', *exception)) if exception else b'')
             + chunk('HASH', digest) + extra)
    return chunk('TEST', struct.pack('<I', index) + parts)


def moo_file(tests, count=None, cpu=b'386E') -> bytes:
    header = chunk('MOO ', struct.pack('<BBxxI4s', 1, 1, len(tests) if count is None else count, cpu))
    return header + chunk('META', bytes(32)) + b''.join(tests)


class MooFormatTest(unittest.TestCase):
    def test_parses_states_ram_exception_and_hash(self):
        initial = {n: n + 1 for n in range(20)}
        data = moo_file([moo_test(7, initial, {16: 0x1234, 17: 0x2}, [(0x100, 0xAA)], [(0x100, 0xBB)],
                                  (13, 0x2000), b'\xab' * 20, extra=chunk('GMET', b'unknown chunk'))])
        moo = vectors.parse_moo(data)
        self.assertEqual(moo.cpu, '386E')
        test = moo.tests[0]
        self.assertEqual((test.index, test.name, test.instruction), (7, 'add [bx],al', b'\x00\x07\xf4'))
        self.assertEqual(test.initial, [n + 1 for n in range(20)])
        self.assertEqual(test.final, {16: 0x1234, 17: 0x2})
        self.assertEqual(test.final_mask, (1 << 16) | (1 << 17))
        self.assertEqual((test.initial_ram, test.final_ram), ([(0x100, 0xAA)], [(0x100, 0xBB)]))
        self.assertEqual(test.exception, (13, 0x2000))
        self.assertEqual(test.hash, 'ab' * 20)

    def test_rejects_malformed_files(self):
        with self.assertRaisesRegex(vectors.CorpusError, 'not a MOO'):
            vectors.parse_moo(chunk('JSON', b''))
        with self.assertRaisesRegex(vectors.CorpusError, 'promises 2'):
            vectors.parse_moo(moo_file([moo_test()], count=2))
        with self.assertRaisesRegex(vectors.CorpusError, 'every register'):
            vectors.parse_moo(moo_file([moo_test(initial={0: 1})]))
        with self.assertRaisesRegex(vectors.CorpusError, 'overruns'):
            vectors.parse_moo(moo_file([moo_test()])[:-3])
        with self.assertRaisesRegex(vectors.CorpusError, 'duplicate RAM'):
            vectors.parse_moo(moo_file([moo_test(initial_ram=[(1, 2), (1, 3)])]))

    def test_revocation_list(self):
        text = '# Revocation list.\n8abbbc61a5866292b0bc816660d7b334bea7962a\n\n'
        self.assertEqual(vectors.revoked_hashes(text), {'8abbbc61a5866292b0bc816660d7b334bea7962a'})
        with self.assertRaises(vectors.CorpusError):
            vectors.revoked_hashes('not-a-hash\n')

    def test_stream_line_matches_the_testbench_format(self):
        test = vectors.parse_moo(moo_file([moo_test(3, {n: 0x10 + n for n in range(20)}, {2: 0xFF},
                                                    [(0x10, 1)], [], (6, 0x30))])).tests[0]
        words = vectors.stream_line('6701', test).split()
        self.assertEqual(words[0], '6701#3')
        self.assertEqual(words[1:21], [f'{0x10 + n:x}' for n in range(20)])
        self.assertEqual(words[21:23], ['4', 'ff'])
        self.assertEqual(words[23:], ['1', '10', '1', '0', '6'])


class ClassificationTest(unittest.TestCase):
    def test_flag_stack_ram_and_write_keys(self):
        test = vectors.parse_moo(moo_file([moo_test(exception=(0, 0x400))])).tests[0]
        keys = vectors.failure_keys(test, [('eflags', 0x802, 0x002), ('ram.400', 0x10, 0x00),
                                           ('ram.401', 0x08, 0x00), ('ram.500', 1, 2),
                                           ('write.600', 0, 9), ('esp', 4, 2)])
        self.assertEqual(keys, ['eflags.OF', 'esp', 'ram', 'stack.flags.AF', 'stack.flags.OF', 'unlisted-write'])

    def predicates(self, stem, code, ecx=0):
        test = vectors.MooTest(0, '', bytes(code), [0] * 4 + [ecx] + [0] * 15, [], 0, {}, [], None, '0' * 40)
        return vectors.predicates(stem, test)

    def test_invalid_sib_needs_32_bit_addressing_and_a_scale(self):
        self.assertIn('invalid-sib', self.predicates('678D', [0x67, 0x8D, 0x04, 0x60, 0xF4]))
        self.assertNotIn('invalid-sib', self.predicates('678D', [0x67, 0x8D, 0x04, 0x20, 0xF4]))
        self.assertNotIn('invalid-sib', self.predicates('8D', [0x8D, 0x04, 0xF4]))

    def test_shift_counts_against_operand_width(self):
        self.assertEqual(self.predicates('D2.4', [0xD2, 0xE0, 0xF4], ecx=8),
                         {'count>0', 'count>1', 'count>=width'})
        self.assertEqual(self.predicates('C1.4', [0xC1, 0xE0, 0x21, 0xF4]), {'count>0'})  # 33 & 31
        self.assertEqual(self.predicates('0FA4', [0x0F, 0xA4, 0x06, 0x34, 0x12, 17, 0xF4]),
                         {'count>0', 'count>1', 'count>=width', 'count>width'})
        self.assertEqual(self.predicates('660FA4', [0x66, 0x0F, 0xA4, 0xC0, 17, 0xF4]),
                         {'count>0', 'count>1', 'opsize32'})

    def test_operand_size_from_the_prefix(self):
        self.assertEqual(self.predicates('6661', [0x66, 0x61, 0xF4]), {'opsize32'})
        self.assertEqual(self.predicates('61', [0x61, 0xF4]), set())
        self.assertEqual(self.predicates('F6.6', [0xF6, 0xF0, 0xF4]), set())

    def test_aam_zero_immediate(self):
        self.assertEqual(self.predicates('D4', [0xD4, 0x00, 0xF4]), {'imm8=0'})
        self.assertEqual(self.predicates('D4', [0xD4, 0x0A, 0xF4]), set())


class DivergenceListTest(unittest.TestCase):
    def test_checked_in_list_is_well_formed(self):
        entries = vectors.load_divergences()
        self.assertTrue(entries)
        self.assertTrue(all(entry['follow'] in ('corpus', 'oracle') for entry in entries))
        corpora = t386.load_corpora_divergences()
        self.assertEqual(corpora, [])

    def write(self, text: str) -> Path:
        handle = tempfile.NamedTemporaryFile('w', suffix='.toml', delete=False)
        handle.write(text)
        handle.close()
        self.addCleanup(Path(handle.name).unlink)
        return Path(handle.name)

    BASE = '''[[vectors]]
id = "{id}"
{scope}
fields = {fields}
deciding_case = "{case}"
deciding_hash = "{digest}"
oracle = "o"
corpus = "c"
follow = "corpus"
'''

    def entry(self, **values):
        fields = dict(id='a', scope='opcodes = ["D3.4"]\nwhen = ["count>1"]', fields='["eflags.OF"]',
                      case='66D3.4#2', digest='a' * 40)
        fields.update(values)
        return self.BASE.format(**fields)

    def test_union_of_entries_covers_a_failure(self):
        path = self.write(self.entry() + self.entry(id='b', scope='opcodes = ["D3.4"]\nwhen = ["count>0"]',
                                                    fields='["eflags.AF"]'))
        entries = vectors.load_divergences(path)
        failure = {'label': 'D3.4#1', 'hash': 'b' * 40, 'keys': ['eflags.AF', 'eflags.OF'],
                   'predicates': ['count>0', 'count>1']}
        self.assertEqual(vectors.coverage(entries, '66D3.4', failure), ['a', 'b'])
        failure['predicates'] = ['count>0']
        self.assertIsNone(vectors.coverage(entries, '66D3.4', failure))

    def test_cases_match_label_and_hash(self):
        path = self.write(self.entry(scope='cases = [["61#681", "' + 'c' * 40 + '"]]', fields='["*"]',
                                     case='61#681', digest='c' * 40))
        entries = vectors.load_divergences(path)
        failure = {'label': '61#681', 'hash': 'c' * 40, 'keys': ['esi', 'edi'], 'predicates': []}
        self.assertEqual(vectors.coverage(entries, '61', failure), ['a'])
        failure['hash'] = 'd' * 40
        self.assertIsNone(vectors.coverage(entries, '61', failure))

    def test_schema_errors(self):
        for values, message in ((dict(case='D2.4#1'), 'deciding case'),
                                (dict(scope='opcodes = ["66D3.4"]'), 'without 66/67'),
                                (dict(scope='opcodes = ["D3.4"]\nwhen = ["count>9"]'), 'unknown predicate'),
                                (dict(scope='opcodes = ["D3.4"]\nfiles = ["D3.4"]'), 'one of'),
                                (dict(digest='xyz'), 'deciding_hash')):
            with self.subTest(message=message), self.assertRaisesRegex(vectors.CorpusError, message):
                vectors.load_divergences(self.write(self.entry(**values)))


LISTING = '''\
    30                              <1> section .system_bios_extensions_area start=0x00000
    62 00000000 90                      nop
   105                                  section .system_bios_area start=0x10000 vstart=0
   200                                  %macro check 1
   300                                  	testBCDflags   aaa, 0x0000, 0,           PS_PF|PS_ZF
    54 0000C33F 66B80000            <1>  mov ax, %3
    63 0000C356 0F855F3C0000        <1>  jne error
   400                                  error:
   401 0000FFBB 8CC8                    	mov ax, cs
   402 0000FFBD 66A9070075FE            	test ax, 7
   403 0000FFC3 C7051234567812345678-   	mov dword [0x78563412], 0x78563412
   403 0000FFCD 9090
'''


class Test386Test(unittest.TestCase):
    def test_configuration_sets_every_equ(self):
        text = ''.join(f'{name} equ 0\n' for name in t386.CONFIGURATION)
        configured = t386.configure(text)
        self.assertIn('TEST_UNDEF equ 1', configured)
        self.assertIn('OUT_PORT equ 0xE9', configured)
        with self.assertRaisesRegex(t386.Test386Error, 'exactly once'):
            t386.configure(text.replace('ROM128 equ 0\n', ''))
        with self.assertRaisesRegex(t386.Test386Error, 'changed'):
            t386.configure(text + 'NEW_OPTION equ 1\n')

    def test_listing_sections_labels_origins_and_lengths(self):
        instructions, labels = t386.parse_listing(LISTING)
        self.assertEqual(instructions[0xE0000]['source'], 'nop')
        check = instructions[0xFC356]
        self.assertEqual((check['source'], check['length'], check['bytes']), ('jne error', 6, '0f855f3c0000'))
        self.assertEqual(check['origin'], 'testBCDflags   aaa, 0x0000, 0,           PS_PF|PS_ZF')
        self.assertEqual(labels['error'], 0xFFFBB)
        self.assertEqual(instructions[0xFFFC3]['length'], 12)  # continued on the next line

    def test_divergence_by_origin_and_by_ram_address(self):
        instructions, _ = t386.parse_listing(LISTING)
        entries = '''[[test386]]
id = "aaa"
post = 0xE0
kind = "branch"
listing = "jne error"
origins = ["testBCDflags   aaa, 0x0000, 0,           PS_PF|PS_ZF"]
deciding_case = "testBCDflags   aaa, 0x0000, 0,           PS_PF|PS_ZF"
oracle = "o"
corpus = "c"
follow = "corpus"

[[test386]]
id = "ram"
post = 0x0E
kind = "fault"
listing = "test ax, 7"
address = 0x26000
bytes = "66a9070075fe"
deciding_case = "copied check"
oracle = "o"
corpus = "c"
follow = "corpus"
'''
        path = Path(tempfile.mkdtemp()) / 'divergences.toml'
        path.write_text(entries)
        loaded = t386.load_divergences(instructions, path)
        self.assertEqual(loaded[0]['checks'], {0xFC356: (loaded[0]['deciding_case'], 6)})
        self.assertEqual(loaded[1]['checks'], {0x26000: ('copied check', 6)})
        path.write_text(entries.replace('66a9070075fe', '66a90700'))
        with self.assertRaisesRegex(t386.Test386Error, 'match no listing line'):
            t386.load_divergences(instructions, path)

    def test_ee_comparison(self):
        self.assertEqual(t386.compare_ee('a\nb\n', 'a\nb\n'), [])
        self.assertEqual(t386.compare_ee('a\nx\n', 'a\nb\nc\n'), [('b', 'x'), ('c', '<missing>')])

    def test_undefined_flag_cases(self):
        root = Path(tempfile.mkdtemp())
        (root / 'src').mkdir()
        (root / 'src/x86_e.asm').write_text('PS_CF equ 0x0001\nPS_AF equ 0x0010\nPS_OF equ 0x0800\n'
                                            'PS_PF equ 0x0004\nPS_ZF equ 0x0040\n')
        (root / 'src/test386.asm').write_text('''bcd386FlagsTest:
\tPS_CAO  equ PS_CF|PS_AF|PS_OF
\ttestBCDflags   aad, 0x0001, PS_CAO,      0
rotate386FlagsTest:
\ttestShiftBFlags   rcr, 0x40, 9, 0,           PS_OF
\ttestBittestFlags   0x01, 2, PS_CF, PS_OF
\tjmp arithLogicTests
''')
        cases = t386.test386_cases(root)
        self.assertEqual(cases[0], {'family': 'bcd', 'op': 'aad', 'ax': 1, 'flags': 0x811, 'expect': 0})
        self.assertEqual(cases[1]['count'], 9)
        self.assertEqual(len(cases), 2 + 8)  # BT, BTC, BTR, BTS in 16 and 32 bits
        self.assertEqual(t386.case_label(cases[1]), 'rcr 8-bit 0x40 by 9 FLAGS=0000')


@unittest.skipUnless((oracle.BUILD / ('bochs-' + oracle.BOCHS_REVISION) / '.git').is_dir(),
                     'pinned Bochs not fetched (run a runner with --download)')
class OracleSmokeTest(unittest.TestCase):
    def test_vectors_testbench_passes_and_fails_states(self):
        testbench = oracle.build_testbench('i386-vectors', oracle.SIM / 'vectors_tb.cpp')
        # CS=DS=SS=1000h, IP=100h: ADD AX,BX then HLT.
        regs = {n: 0 for n in range(20)}
        regs.update({0: 0x7FFFFFF0, 2: 0x1234, 3: 0x0101, 9: 0xFFFE, 10: 0x1000, 11: 0x1000, 15: 0x1000,
                     16: 0x100, 17: 0x2, 18: 0xFFFF0FF0})
        code = [(0x10100, 0x01), (0x10101, 0xD8), (0x10102, 0xF4)]
        # ADD changes EAX, but this vector does not list EAX in its final RG32.
        good = moo_test(0, regs, {16: 0x103, 17: 0x6}, code)  # PF set
        # EAX is listed here, so its wrong expected value must remain a failure.
        bad = moo_test(1, regs, {2: 0x1336, 16: 0x103, 17: 0x6}, code)
        # DIV BL with BL=0: #DE through vector 0 (handler 2000h:0000h holds HLT).
        divide = {**regs, 3: 0}
        divide_ram = [(0x10100, 0xF6), (0x10101, 0xF3), (0x10102, 0xF4), (0, 0), (1, 0), (2, 0), (3, 0x20),
                      (0x20000, 0xF4)]
        frame = [(0x1FFF8, 0x00), (0x1FFF9, 0x01), (0x1FFFA, 0x00), (0x1FFFB, 0x10), (0x1FFFC, 0x02),
                 (0x1FFFD, 0x00)]
        trap = moo_test(2, divide, {9: 0xFFF8, 10: 0x2000, 16: 0x1}, divide_ram, frame, (0, 0x1FFFC))
        moo = vectors.parse_moo(moo_file([good, bad, trap]))
        lines = ''.join(vectors.stream_line('01', test) + '\n' for test in moo.tests)
        result = subprocess.run([str(testbench)], input=lines, capture_output=True, text=True, check=True)
        self.assertEqual(result.stdout.splitlines()[-1], 'SUMMARY 2 1')
        self.assertIn('FAIL 01#1 eax 1336 1335', result.stdout)


class OracleFetchTest(unittest.TestCase):
    def test_no_download_without_permission(self):
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(oracle, 'BUILD', Path(directory)):
            with self.assertRaisesRegex(oracle.OracleError, '--download'):
                oracle.bochs_source(download=False)


if __name__ == '__main__':
    unittest.main()
