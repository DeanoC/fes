"""Feature-qualified read-only RAS snapshots never admit startup or recovery."""
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import fault_status as status
from test_nano_fault_status import lines as inventory_lines
from test_nano_native_trace import trace, fixture, BUILD, SEED


def lines(values=None, outcome='selected-record'):
    values = [0x11112222, 0, 2, 0, 0x701, 0x109, 0xc0000011,
              0x123456789abcdef0, 0xc0000011, 0] if values is None else values
    rows = []
    for index, value in enumerate(values):
        rows += [f'FES_NANO_RAM RAS before={index:016x}',
                 f'FES_NANO_RAM RAS value={index:016x} data={value:016x}']
    return rows + [('FES_NANO_RAM RAS END' if outcome == 'selected-record'
                    else 'FES_NANO_RAM RAS SKIP reason='+outcome)]


def decode(rows, **context):
    result = status.ras_state()
    arguments = dict(exception=True, inventory_complete=True);arguments.update(context)
    errors = [error for number, line in enumerate(rows, 1)
              if (error := status.observe_ras(result, line, number, **arguments))]
    return result, errors


class FaultRASTests(unittest.TestCase):
    def test_current_core_or_shared_record_is_exact_and_bounded(self):
        for selector in (0, 1, 0xfffe):
            values = [0x11112222, 0x80000011, 0xffff, selector,
                      1, 2, 3, 4, 5, selector]
            result, errors = decode(lines(values))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'] and result['record_complete'])
            self.assertEqual(result['outcome'], 'selected-record')
            self.assertEqual(result['selected'], selector)
            self.assertEqual([row['value'] for row in result['records']], values)
            self.assertEqual([row['target'] for row in result['records']],
                             [register for _, register in status.RAS_READS])
            self.assertTrue(all(row['before_line'] < row['line'] for row in result['records']))

    def test_unsupported_feature_never_accesses_ras_registers(self):
        for feature in range(16):
            if feature == 1:continue
            result, errors = decode(lines([feature << 28], 'feature'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['record_complete'])
            self.assertEqual(result['outcome'], 'feature')
            result, errors = decode(lines([feature << 28, 0], 'feature'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 1)

    def test_zero_or_reserved_count_skips_even_selector_read(self):
        for count in (0, 0x10000, 0x10002, (1 << 64)-1):
            values = [1 << 28, 0, count]
            result, errors = decode(lines(values, 'count'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['record_complete'])
            result, errors = decode(lines(values+[0], 'count'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 3)

    def test_invalid_selector_skips_all_selected_registers(self):
        for count, selector in ((1, 1), (2, 2), (2, 0x10000), (0xffff, 0xffff)):
            values = [1 << 28, 0, count, selector]
            result, errors = decode(lines(values, 'selector'))
            self.assertEqual(errors, [])
            self.assertTrue(result['complete'])
            self.assertFalse(result['record_complete'])
            result, errors = decode(lines(values+[0], 'selector'))
            self.assertTrue(errors)
            self.assertEqual(len(result['records']), 4)

    def test_changed_selector_is_not_a_complete_record(self):
        values = [1 << 28, 0, 2, 0, 1, 2, 3, 4, 5, 1]
        result, errors = decode(lines(values, 'changed'))
        self.assertEqual(errors, [])
        self.assertTrue(result['complete'])
        self.assertFalse(result['record_complete'])
        result, errors = decode(lines(values))
        self.assertTrue(errors)
        self.assertFalse(result['complete'])

    def test_each_access_failure_retains_exact_partial_boundary(self):
        for index in range(len(status.RAS_READS)):
            result, errors = decode(lines()[:index*2+1])
            self.assertEqual(errors, [])
            self.assertEqual(len(result['records']), index)
            self.assertEqual(result['pending']['index'], index)
            self.assertFalse(result['complete'] or result['record_complete'])

    def test_wrong_skip_incomplete_end_reorder_and_malformed_rows_reject(self):
        good = lines()
        cases = [good[1:], good[2:4]+good[:2]+good[4:], good[:1]+good,
                 good[:-1]+['FES_NANO_RAM RAS SKIP reason=changed'],
                 good[:4]+['FES_NANO_RAM RAS END'], good[:4]+['FES_NANO_RAM RAS SKIP reason=count'],
                 good[:-1]+['FES_NANO_RAM RAS END extra=1'], good[:-1]+['FES_NANO_RAM RAS SKIP reason=other'],
                 good[:1]+[good[1]+' extra=1'], [good[0].replace('0000000000000000', 'ffffffffffffffff')]]
        for rows in cases:
            with self.subTest(rows=rows):
                result, errors = decode(rows)
                self.assertTrue(errors)
                self.assertFalse(result['complete'] or result['record_complete'])

    def test_flood_stays_invalid_and_preserves_only_ten_records(self):
        result, errors = decode(lines()*20)
        self.assertTrue(errors)
        self.assertFalse(result['valid'] or result['complete'] or result['record_complete'])
        self.assertEqual(len(result['records']), 10)

    def test_exception_and_complete_inventory_are_required(self):
        for context in (dict(exception=False), dict(inventory_complete=False)):
            result = status.ras_state()
            arguments = dict(exception=True, inventory_complete=True);arguments.update(context)
            self.assertIsNotNone(status.observe_ras(result, lines()[0], 1, **arguments))
            self.assertFalse(result['valid'])
            self.assertEqual(result['records'], [])

    def test_full_boot_decoder_keeps_ras_terminal_and_enforces_inventory(self):
        fault = 'FES_NANO_RAM EXCEPTION '+' '.join(name+'=0000000000000000' for name in
                 ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))
        anchor = 'NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        for inventory in (inventory_lines(), [], inventory_lines()[:-1]):
            raw = fixture().replace(anchor, '\r\n'.join([fault]+inventory+lines())+'\r\n'+anchor)
            report = trace.decode_native_boot(raw, BUILD, SEED)
            self.assertFalse(report['complete'] or report['loader_ready'])
            self.assertFalse(report['firmware_ram_trace']['valid'])
            ras = report['firmware_ram_trace']['ras_probe']
            self.assertEqual(ras['complete'], inventory == inventory_lines())
        raw = fixture().replace(anchor, '\r\n'.join(lines())+'\r\n'+anchor)
        report = trace.decode_native_boot(raw, BUILD, SEED)
        self.assertFalse(report['loader_ready'])
        self.assertFalse(report['firmware_ram_trace']['ras_probe']['valid'])

    def test_generated_ras_has_only_marked_reads_and_no_selector_or_status_writes(self):
        text = status.ras_assembly()
        self.assertFalse(re.search(r'(?m)^\s*(?:str|stp|msr|bl|blr|ret|eret)\b', text))
        self.assertNotRegex(text, r'\b(?:sp|erxaddr_el1|erxmisc1_el1)\b')
        self.assertEqual(re.findall(r'(?m)^mrs x0, (\w+)$', text),
                         [register for _, register in status.RAS_READS])
        self.assertEqual(text.count('fes_ram_uart_text fes_ras_before, 1'), 10)
        self.assertLess(text.index('cmp x8, #1'), text.index('mrs x0, disr_el1'))
        self.assertLess(text.index('cbz x0, .Lfes_ras_count'), text.index('mrs x0, errselr_el1'))
        self.assertLess(text.index('b.hs .Lfes_ras_selector'), text.index('mrs x0, erxfr_el1'))


if __name__ == '__main__':unittest.main()
