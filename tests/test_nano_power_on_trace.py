"""POR admission must not inherit the volatile/JTAG startup acceptance."""
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
from power_on_trace import observe

BEGIN = b'FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=0 valid=1\r\nFES_ENTRY EL1 HANDOFF\r\n'
READY = dict(loader_ready=True, errors=[])


class PowerOnTests(unittest.TestCase):
    def test_confirmed_por_plus_independent_complete_loader_report(self):
        result = observe(BEGIN, cycle_confirmed=True, native_boot=READY)
        self.assertTrue(result['power_on_boot_qualified'])
        self.assertFalse(result['failed'])
        self.assertTrue(result['boot_started'])

    def test_sticky_por_cause_does_not_admit_jtag_warm_cold_nconfig_or_rsu(self):
        for kind in range(1, 8):
            result = observe(BEGIN.replace(b'kind=0', ('kind='+str(kind)).encode()), cycle_confirmed=True, native_boot=READY)
            self.assertFalse(result['power_on_boot_qualified'])
            self.assertTrue(result['failed'])

    def test_missing_cycle_confirmation_remains_pending_even_with_old_good_trace(self):
        for raw in (b'', BEGIN, BEGIN.replace(b'kind=0', b'kind=4')):
            result = observe(raw, cycle_confirmed=False, native_boot=READY)
            self.assertEqual(result['status'], 'pending-power-cycle')
            self.assertFalse(result['failed'])
            self.assertFalse(result['boot_started'])
            self.assertFalse(result['power_on_boot_qualified'])

    def test_partial_repeated_malformed_or_invalid_begin_cannot_pass(self):
        for raw in (b'', BEGIN[:-3], BEGIN+BEGIN, BEGIN.replace(b'valid=1', b'valid=0'),
                    BEGIN.replace(b'cause=00000004', b'cause=x')):
            with self.subTest(raw=raw):
                result = observe(raw, cycle_confirmed=True, native_boot=READY)
                self.assertTrue(result['failed'])
                self.assertFalse(result['power_on_boot_qualified'])

    def test_por_scope_never_substitutes_for_full_native_loader_decoder(self):
        for report in (dict(loader_ready=False, errors=[]), dict(loader_ready=True, errors=['wrong ATF']),
                       dict(loader_ready=False, errors=['unmatched owner'])):
            result = observe(BEGIN, cycle_confirmed=True, native_boot=report)
            self.assertTrue(result['failed'])
            self.assertFalse(result['power_on_boot_qualified'])

    def test_incomplete_report_unbounded_trace_or_implicit_confirmation_rejects(self):
        for report in ({}, None, dict(loader_ready=1, errors=[])):
            with self.assertRaises(ValueError): observe(BEGIN, cycle_confirmed=True, native_boot=report)
        with self.assertRaises(ValueError): observe(BEGIN, cycle_confirmed=1, native_boot=READY)
        with self.assertRaises(ValueError): observe(b'X'*(1+(1<<20)), cycle_confirmed=True, native_boot=READY)


if __name__ == '__main__':
    unittest.main()
