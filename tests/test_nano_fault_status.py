"""Post-fault inventory models; completion never constitutes runtime admission."""
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
import fault_status as status
from test_nano_native_trace import trace, fixture, BUILD, SEED


def lines():
    rows = []
    for index, (_, kind, _) in enumerate(status.PROBES):
        value = 0xffffffffffffffff if kind == 'cpu' else 0xffffffff
        rows += [f'FES_NANO_RAM PROBE before={index:016x}',
                 f'FES_NANO_RAM PROBE value={index:016x} data={value:016x}']
    return rows + ['FES_NANO_RAM PROBE END']


def decode(rows, exception=True):
    result = status.state()
    errors = [error for number, line in enumerate(rows, 1)
              if (error := status.observe(result, line, number, exception=exception))]
    return result, errors


class FaultStatusTests(unittest.TestCase):
    def test_complete_preserves_exact_sources_and_values(self):
        result, errors = decode(lines())
        self.assertEqual(errors, [])
        self.assertTrue(result['complete'])
        self.assertTrue(result['valid'])
        self.assertIsNone(result['pending'])
        for index, row in enumerate(result['records']):
            name, kind, target = status.PROBES[index]
            self.assertEqual((row['index'], row['name'], row['source'], row['target']),
                             (index, name, kind, target))
            self.assertLess(row['before_line'], row['line'])

    def test_each_failed_read_preserves_pending_index_and_no_completion(self):
        for index in range(len(status.PROBES)):
            result, errors = decode(lines()[:index*2+1])
            self.assertEqual(errors, [])
            self.assertEqual(len(result['records']), index)
            self.assertEqual(result['pending']['index'], index)
            self.assertFalse(result['complete'])

    def test_missing_duplicate_reordered_and_unknown_rows_fail(self):
        good = lines()
        cases = [good[1:], good[:-1], good[:1]+good, good[:2]+good[1:],
                 good[2:4]+good[:2]+good[4:], good+good[-1:],
                 good[:-1]+['FES_NANO_RAM PROBE before=ffffffffffffffff'],
                 ['FES_NANO_RAM PROBE value=0000000000000000 data=0000000000000001']]
        for rows in cases:
            result, errors = decode(rows)
            self.assertFalse(result['complete'])
            if rows != good[:-1]:self.assertTrue(errors)
        result, errors = decode(good, exception=False)
        self.assertTrue(errors);self.assertFalse(result['complete'])

    def test_mmio_width_bad_hex_and_extra_fields_reject(self):
        index = next(i for i, row in enumerate(status.PROBES) if row[1] == 'mmio')
        value = lines()[index*2+1]
        for bad in (value.replace('00000000ffffffff', 'ffffffffffffffff'),
                    value.replace('00000000ffffffff', '-1'), value+' extra=1',
                    value.replace('value=', 'before=')):
            result, errors = decode(lines()[:index*2+1]+[bad])
            self.assertTrue(errors);self.assertFalse(result['valid'])
            self.assertEqual(len(result['records']), index)

    def test_flood_is_bounded_and_cannot_recover(self):
        result, errors = decode(lines()+lines()*20)
        self.assertTrue(errors)
        self.assertFalse(result['complete'])
        self.assertFalse(result['valid'])
        self.assertEqual(len(result['records']), len(status.PROBES))

    def test_probe_is_terminal_in_full_boot_decoder(self):
        raw = fixture()
        self.assertTrue(trace.decode_native_boot(raw, BUILD, SEED)['complete'])
        fault = ('FES_NANO_RAM EXCEPTION '+' '.join(name+'=0000000000000000' for name in
                 ('vector','esr','elr','far','spsr','r0','r1','r2','r3'))+'\r\n')
        prefix = 'NOTICE:  FES_NANO_EL3_CAPTURE phase=before'
        for records, present in ((lines(), True), (lines()[:7], True), (lines(), False)):
            stream = raw.replace(prefix, (fault if present else '')+'\r\n'.join(records)+'\r\n'+prefix)
            report = trace.decode_native_boot(stream, BUILD, SEED)
            self.assertFalse(report['complete']);self.assertFalse(report['loader_ready'])
            probe = report['firmware_ram_trace']['status_probe']
            self.assertEqual(probe['complete'], present and records == lines())
        for partial in ('FES_NANO_RAM PROBE before=0000000000000000',
                        'FES_NANO_RAM PROBE value=0000000000000000 data=0000000000000001'):
            report = trace.decode_native_boot((fault+partial).encode(), BUILD, SEED)
            self.assertFalse(report['loader_ready'])
            self.assertFalse(report['firmware_ram_trace']['status_probe']['complete'])

    def test_generated_reads_exclude_writes_and_optional_ras_accesses(self):
        assembly = status.assembly()
        self.assertFalse(re.search(r'(?m)^\s*(?:str|stp|msr|bl|blr)\b', assembly))
        self.assertNotRegex(assembly, r'\b(?:sp|errselr_el1|erxstatus_el1|disr_el1)\b')
        self.assertEqual(re.findall(r'(?m)^mrs x0, (\w+)$', assembly), [reg for _, reg in status.CPU])
        self.assertEqual([int(address, 16) for address in re.findall(r'(?m)^ldr x8, =(0x[0-9a-f]+)$', assembly)],
                         [address for _, address in status.MMIO])
        self.assertTrue(set(address for _, address in status.MMIO).isdisjoint(
                        (0x108cc024, 0x108cc078, 0x108cc07c, 0x1c000114, 0x1c00c12c)))
        self.assertIn('fes_probe_end: .asciz "FES_NANO_RAM PROBE END\\r\\n"', assembly)

    def test_injection_is_restricted_to_aligned_mmio_indices(self):
        index = next(i for i, row in enumerate(status.PROBES) if row[1] == 'mmio')
        self.assertIn('ldr x8, =0x80010000', status.assembly({index:0x80010000}))
        for invalid in ({0:0x80010000}, {True:4}, {-1:4}, {len(status.PROBES):4},
                        {index:1}, {index:-4}, {index:1<<64}, {index:True}, []):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):status.assembly(invalid)


if __name__ == '__main__':unittest.main()
