"""Qualified bank selection, restoration and partial failure never admit boot."""
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import fault_status as status
from test_nano_fault_status import lines as inventory_lines
from test_nano_native_trace import trace, fixture, BUILD, SEED

VALUES = [0x11112222, 0x412fd050, 0x80000011, 2, 0,
          0, 0xa9a6, 0, 0xc0000011, 0x123456789abcdef0, 0xc0000011, 0,
          1, 0x55aa, 0, 0xc0000022, 0xfedcba9876543210, 0xc0000022, 1, 0]


def lines(values=None, outcome='both-records'):
    values = VALUES if values is None else values
    rows = []
    for index, value in enumerate(values):
        rows += [f'FES_NANO_RAM RAS2 before={index:016x}',
                 f'FES_NANO_RAM RAS2 value={index:016x} data={value:016x}']
    return rows + [('FES_NANO_RAM RAS2 END' if outcome == 'both-records'
                    else 'FES_NANO_RAM RAS2 SKIP reason='+outcome)]


def decode(rows, **context):
    result = status.ras_state()
    arguments = dict(exception=True, inventory_complete=True);arguments.update(context)
    errors = [error for number, line in enumerate(rows, 1)
              if (error := status.observe_ras(result, line, number, **arguments))]
    return result, errors


class FaultRASTests(unittest.TestCase):
    def test_both_records_and_original_selector_are_exact(self):
        for selector in (0, 1):
            values = VALUES.copy();values[4] = values[19] = selector
            result, errors = decode(lines(values))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'] and result['record_complete'] and result['selector_restored'])
            self.assertFalse(result['selector_may_have_changed'])
            self.assertEqual(result['outcome'], 'both-records')
            self.assertEqual(result['original_selector'], selector)
            self.assertEqual([row['value'] for row in result['records']], values)
            self.assertEqual([row['target'] for row in result['records']],
                             [step[2] for step in status.RAS_STEPS])
            self.assertTrue(all(row['before_line'] < row['line'] for row in result['records']))
            self.assertEqual([(r['operation'], r['requested']) for r in result['records'] if r['operation'] != 'read'],
                             [('select', 0), ('select', 1), ('restore', None)])

    def test_unsupported_feature_never_accesses_ras_registers(self):
        for feature in range(16):
            if feature == 1:continue
            result, errors = decode(lines([feature << 28], 'feature'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['record_complete'] or result['selector_may_have_changed'])
            result, errors = decode(lines([feature << 28, 0], 'feature'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 1)

    def test_unreviewed_cpu_skips_all_optional_accesses(self):
        for midr in (0, 0x411fd050, 0x412fd051, 0x412fd0b0, 0x512fd050):
            values = [1 << 28, midr]
            result, errors = decode(lines(values, 'cpu'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['selector_may_have_changed'])
            result, errors = decode(lines(values+[0], 'cpu'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 2)

    def test_only_exact_two_records_permit_selector_read(self):
        for count in (0, 1, 3, 0xffff, 0x10002, (1 << 64)-1):
            values = VALUES[:4];values[3] = count
            result, errors = decode(lines(values, 'count'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['selector_may_have_changed'])
            result, errors = decode(lines(values+[0], 'count'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 4)

    def test_invalid_original_selector_prevents_every_write(self):
        for selector in (2, 0x10000, (1 << 64)-1):
            values = VALUES[:5];values[4] = selector
            result, errors = decode(lines(values, 'selector'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['selector_may_have_changed'])
            result, errors = decode(lines(values+[0], 'selector'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 5)

    def test_every_selection_or_restoration_mismatch_is_terminal_and_unrestored(self):
        for index in (5, 11, 12, 18, 19):
            values = VALUES[:index+1];values[index] ^= 1
            result, errors = decode(lines(values, 'changed'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'] and result['selector_may_have_changed'])
            self.assertFalse(result['record_complete'] or result['selector_restored'])
            result, errors = decode(lines(values))
            self.assertTrue(errors)
            if index < 19:
                extended = values+VALUES[index+1:]
                result, errors = decode(lines(extended))
                self.assertTrue(errors)
                self.assertEqual(len(result['records']), index+1)

    def test_each_read_or_write_failure_retains_pending_action_and_uncertainty(self):
        for index in range(len(status.RAS_STEPS)):
            result, errors = decode(lines()[:index*2+1])
            self.assertEqual(errors, [])
            self.assertEqual(len(result['records']), index)
            self.assertEqual(result['pending']['index'], index)
            self.assertEqual(result['selector_may_have_changed'], index >= 5)
            self.assertFalse(result['complete'] or result['record_complete'] or result['selector_restored'])

    def test_restore_readback_proves_only_restore_before_end(self):
        result, errors = decode(lines()[:-1])
        self.assertEqual(errors, [])
        self.assertTrue(result['selector_restored'])
        self.assertFalse(result['selector_may_have_changed'] or result['complete'] or result['record_complete'])

    def test_missing_end_reorder_malformed_and_wrong_skip_reject(self):
        good = lines()
        cases = [good[1:], good[2:4]+good[:2]+good[4:], good[:1]+good,
                 good[:-1]+['FES_NANO_RAM RAS2 SKIP reason=changed'],
                 good[:4]+['FES_NANO_RAM RAS2 END'], good[:4]+['FES_NANO_RAM RAS2 SKIP reason=count'],
                 good[:-1]+['FES_NANO_RAM RAS2 END extra=1'], good[:-1]+['FES_NANO_RAM RAS2 SKIP reason=other'],
                 good[:1]+[good[1]+' requested=1'], [good[0].replace('0000000000000000', 'ffffffffffffffff')],
                 [row.replace('RAS2', 'RAS') for row in good]]
        for rows in cases:
            with self.subTest(rows=rows):
                result, errors = decode(rows)
                self.assertTrue(errors)
                self.assertFalse(result['complete'] or result['record_complete'])

    def test_flood_stays_invalid_and_retains_only_twenty_actions(self):
        result, errors = decode(lines()*20)
        self.assertTrue(errors)
        self.assertFalse(result['valid'] or result['complete'] or result['record_complete'])
        self.assertEqual(len(result['records']), 20)

    def test_exception_and_complete_inventory_are_required(self):
        for context in (dict(exception=False), dict(inventory_complete=False)):
            result, errors = decode(lines(), **context)
            self.assertTrue(errors)
            self.assertFalse(result['valid'] or result['selector_may_have_changed'])
            self.assertEqual(result['records'], [])

    def test_full_boot_decoder_keeps_all_ras2_outcomes_terminal(self):
        fault = 'FES_NANO_RAM EXCEPTION '+' '.join(name+'=0000000000000000' for name in
                 ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))
        anchor = 'NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        for inventory in (inventory_lines(), [], inventory_lines()[:-1]):
            for ras in (lines(), lines([0], 'feature'), lines()[:11]):
                raw = fixture().replace(anchor, '\r\n'.join([fault]+inventory+ras)+'\r\n'+anchor)
                report = trace.decode_native_boot(raw, BUILD, SEED)
                self.assertFalse(report['complete'] or report['loader_ready'])
                self.assertFalse(report['firmware_ram_trace']['valid'])
                state = report['firmware_ram_trace']['ras_probe']
                self.assertEqual(state['complete'], inventory == inventory_lines() and ras in (lines(), lines([0], 'feature')))
        raw = fixture().replace(anchor, '\r\n'.join(lines())+'\r\n'+anchor)
        report = trace.decode_native_boot(raw, BUILD, SEED)
        self.assertFalse(report['loader_ready'])
        self.assertFalse(report['firmware_ram_trace']['ras_probe']['valid'])

    def test_generated_code_has_exact_selector_only_writes_and_identity_gates(self):
        text = status.ras_assembly()
        self.assertFalse(re.search(r'(?m)^\s*(?:str|stp|bl|blr|ret|eret)\b', text))
        self.assertNotRegex(text, r'\b(?:sp|erxaddr_el1|erxmisc1_el1)\b')
        self.assertEqual(re.findall(r'(?m)^msr (.+)$', text), ['errselr_el1, x0']*3)
        self.assertEqual(text.count('msr errselr_el1, x0\nisb\nmrs x0, errselr_el1'), 3)
        self.assertEqual(re.findall(r'(?m)^mrs x0, (\w+)$', text), [step[2] for step in status.RAS_STEPS])
        self.assertEqual(text.count('fes_ram_uart_text fes_ras_before, 1'), 20)
        self.assertLess(text.index('cmp x8, #1'), text.index('mrs x0, disr_el1'))
        self.assertLess(text.index('b.ne .Lfes_ras_cpu'), text.index('mrs x0, disr_el1'))
        self.assertLess(text.index('b.ne .Lfes_ras_count'), text.index('mrs x0, errselr_el1'))
        self.assertLess(text.index('b.hs .Lfes_ras_selector'), text.index('msr errselr_el1'))
        self.assertIn('mov x0, #0\nmsr errselr_el1', text)
        self.assertIn('mov x0, #1\nmsr errselr_el1', text)
        self.assertIn('mov x0, x25\nmsr errselr_el1', text)
        for index in (5, 11, 12, 18, 19):
            self.assertIn(f'mov x19, #{index}\n', text)


if __name__ == '__main__':unittest.main()
