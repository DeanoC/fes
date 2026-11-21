"""POR admission must not inherit the volatile/JTAG startup acceptance."""
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT/'platform/embedded/nano'))
from power_on_trace import observe as observe_trace, split_capture

BUILD = '0123456789abcdef'
BANNER = b'U-Boot SPL 2025.01-fes-nano-0123456789abcdef (fixture)\r\n'
BEGIN = b'FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=0 valid=1\r\n'
HANDOFF = b'FES_ENTRY EL1 HANDOFF\r\n'
CURRENT = BANNER + BEGIN + HANDOFF
READY = dict(loader_ready=True, errors=[])


def observe(raw, report=READY, *, cycle_confirmed=True, build_id=BUILD, seen=None):
    def decode(segment):
        if seen is not None:
            seen.append(segment)
        return report
    return observe_trace(raw, build_id=build_id, cycle_confirmed=cycle_confirmed, native_boot=decode)


class PowerOnTests(unittest.TestCase):
    def test_split_passive_window_keeps_old_tail_and_requires_one_matching_boundary(self):
        tail = b'partial row\r\nFES_ENTRY EL1 HANDOFF\r\n'
        self.assertEqual(split_capture(tail + CURRENT, BUILD), (tail, CURRENT))
        for raw in (BEGIN, CURRENT+CURRENT, BANNER+b'gap\n'+BEGIN, BANNER+BEGIN+BEGIN):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                split_capture(raw, BUILD)
        with self.assertRaises(ValueError):
            split_capture(CURRENT, 'abcdef0123456789')

    def test_confirmed_por_plus_independent_complete_loader_report(self):
        seen = []
        result = observe(CURRENT, seen=seen)
        self.assertTrue(result['power_on_boot_qualified'])
        self.assertFalse(result['failed'])
        self.assertTrue(result['boot_started'])
        self.assertEqual(seen, [CURRENT])

    def test_old_tail_markers_are_excluded_and_decoder_receives_only_matching_segment(self):
        old_tail = b'old FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=0 valid=1\r\n'
        old_tail += HANDOFF
        seen = []
        result = observe(old_tail + CURRENT, seen=seen)
        self.assertTrue(result['power_on_boot_qualified'])
        self.assertEqual(seen, [CURRENT])

    def test_unframed_stale_por_and_handoff_cannot_create_failure_evidence(self):
        seen = []
        with self.assertRaises(ValueError):
            observe(BEGIN + HANDOFF, seen=seen)
        self.assertEqual(seen, [])

    def test_sticky_por_cause_does_not_admit_jtag_warm_cold_nconfig_or_rsu(self):
        for kind in range(1, 8):
            result = observe((BANNER + BEGIN.replace(b'kind=0', ('kind='+str(kind)).encode()) + HANDOFF))
            self.assertFalse(result['power_on_boot_qualified'])
            self.assertFalse(result['boot_started'])
            self.assertTrue(result['failed'])

    def test_missing_cycle_confirmation_remains_pending_even_with_old_good_trace(self):
        for raw in (b'', BEGIN, CURRENT, BANNER + BEGIN.replace(b'kind=0', b'kind=4')):
            result = observe(raw, cycle_confirmed=False)
            self.assertEqual(result['status'], 'pending-power-cycle')
            self.assertFalse(result['failed'])
            self.assertFalse(result['boot_started'])
            self.assertFalse(result['power_on_boot_qualified'])

    def test_confirmed_cycle_without_an_spl_begin_does_not_prove_boot_started(self):
        for raw in (b'', b'old uart bytes\r\n'):
            with self.assertRaises(ValueError):
                observe(raw)

    def test_partial_repeated_malformed_or_invalid_begin_cannot_pass(self):
        for raw in (BANNER + BEGIN[:-3], BANNER + BEGIN + BEGIN,
                    BANNER + BEGIN.replace(b'cause=00000004', b'cause=x')):
            with self.subTest(raw=raw):
                with self.assertRaises(ValueError):
                    observe(raw)
        invalid = observe(BANNER + BEGIN.replace(b'valid=1', b'valid=0') + HANDOFF)
        self.assertTrue(invalid['failed'])
        self.assertFalse(invalid['boot_started'])

    def test_por_scope_never_substitutes_for_full_native_loader_decoder(self):
        for report in (dict(loader_ready=False, errors=[]), dict(loader_ready=True, errors=['wrong ATF']),
                       dict(loader_ready=False, errors=['unmatched owner'])):
            result = observe(CURRENT, report)
            self.assertTrue(result['failed'])
            self.assertFalse(result['power_on_boot_qualified'])

    def test_incomplete_report_unbounded_trace_or_implicit_confirmation_rejects(self):
        for report in ({}, None, dict(loader_ready=1, errors=[])):
            with self.assertRaises(ValueError): observe(CURRENT, report)
        with self.assertRaises(ValueError): observe(CURRENT, cycle_confirmed=1)
        with self.assertRaises(ValueError): observe(b'X'*(1+(1<<20)))
        with self.assertRaises(ValueError): observe(CURRENT, build_id='abcdef0123456789')


if __name__ == '__main__':
    unittest.main()
