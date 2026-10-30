"""Reject incomplete uncertainty evidence and retain every native timing row."""
import copy
import json
from pathlib import Path
import tempfile
import unittest

from scripts.screen_fes_pcxt_uncertainty import CORNERS, clock_event, run, screen, uncertainties


class UncertaintyScreenTest(unittest.TestCase):
    def report(self):
        return {'timing_summary': {'final_analogue_model': True},
                'fmax': {n: {'constraint': f} for n, f in
                         [('clocks[0]', 100), ('clocks[1]', 100), ('clk_pixel', 74.25)]},
                'detailed_net_timings': [{'driver': 'launch', 'port': 'Q',
                    'event': 'posedge clocks[0]', 'net': 'data', 'endpoints': [
                        {'cell': 'capture', 'port': 'ENA', 'event': 'posedge clocks[0]',
                         'setup_checked': True, 'hold_checked': True,
                         'setup_slack_ns': 0.055, 'hold_slack_ns': 0.764}]}]}

    def test_small_native_pass_becomes_explicit_failure(self):
        result = screen(self.report(), {('posedge clocks[0]', 'posedge clocks[0]'): .1},
                        {('posedge clocks[0]', 'posedge clocks[0]'): 0})
        self.assertFalse(result['checked_setup_headroom_pass'])
        self.assertAlmostEqual(result['setup_failures'][0]['adjusted_setup_ns'], -.045)
        self.assertEqual(result['native_endpoint_rows'], 1)

    def test_missing_pixel_budget_is_not_a_pass(self):
        report = self.report()
        report['detailed_net_timings'][0]['event'] = 'posedge clk_pixel'
        result = screen(report, {
            ('posedge clk_pixel', 'posedge clocks[0]'): 0},
                        {('posedge clk_pixel', 'posedge clocks[0]'): 0})
        self.assertTrue(result['complete_native_uncertainty_coverage'])
        report['detailed_net_timings'].append(copy.deepcopy(report['detailed_net_timings'][0]))
        report['detailed_net_timings'][1]['driver'] = 'pixel'
        report['detailed_net_timings'][1]['endpoints'][0]['event'] = 'posedge clk_pixel'
        result = screen(report, {('posedge clk_pixel', 'posedge clocks[0]'): 0},
                        {('posedge clk_pixel', 'posedge clocks[0]'): 0})
        self.assertFalse(result['complete_native_uncertainty_coverage'])
        self.assertEqual(result['uncovered_clock_pairs'][0]['rows'], 1)

    def test_unpaired_row_is_retained(self):
        report = self.report()
        other = copy.deepcopy(report['detailed_net_timings'][0])
        other['driver'] = 'untimed'
        other['endpoints'][0]['hold_checked'] = False
        report['detailed_net_timings'].append(other)
        result = screen(report, {('posedge clocks[0]', 'posedge clocks[0]'): 0},
                        {('posedge clocks[0]', 'posedge clocks[0]'): 0})
        self.assertEqual(len(result['unpaired_native_rows']), 1)
        self.assertFalse(result['complete_native_uncertainty_coverage'])

    def test_duplicate_and_nonfinite_rows_reject(self):
        report = self.report()
        report['detailed_net_timings'] *= 2
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            screen(report, {})
        report = self.report()
        report['detailed_net_timings'][0]['endpoints'][0]['setup_slack_ns'] = float('nan')
        with self.assertRaisesRegex(ValueError, 'finite'):
            screen(report, {})

    def test_hold_budget_can_fail_a_native_pass(self):
        report = self.report()
        endpoint = report['detailed_net_timings'][0]['endpoints'][0]
        endpoint['hold_slack_ns'] = .05
        pair = ('posedge clocks[0]', 'posedge clocks[0]')
        result = screen(report, {pair: 0}, {pair: .07})
        self.assertTrue(result['checked_setup_headroom_pass'])
        self.assertFalse(result['checked_hold_headroom_pass'])
        self.assertAlmostEqual(result['hold_failures'][0]['adjusted_hold_ns'], -.02)
        result = screen(report, {pair: 0}, {})
        self.assertFalse(result['complete_native_uncertainty_coverage'])

    def test_producer_report_or_rbf_substitution_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            report = self.report()
            receipt = {'status': 'routed-diagnostic-unqualified',
                       'working_tree_changes': [], 'timing': report,
                       'rbf_sha256': '0' * 64}
            (root / 'diagnostic.json').write_text(json.dumps(receipt))
            (root / 'timing.json').write_text(json.dumps(report))
            (root / 'diagnostic.rbf').write_bytes(b'changed bitstream')
            with self.assertRaisesRegex(ValueError, 'RBF hash mismatch'):
                run(root, root)
            report['timing_summary']['final_analogue_model'] = False
            (root / 'timing.json').write_text(json.dumps(report))
            with self.assertRaisesRegex(ValueError, 'differs from the producer'):
                run(root, root)

    def test_relaxed_clock_or_negative_budget_rejected(self):
        report = self.report()
        report['fmax']['clocks[0]']['constraint'] = 50
        with self.assertRaisesRegex(ValueError, 'frequency'):
            screen(report, {})
        with self.assertRaisesRegex(ValueError, 'negative'):
            screen(self.report(), {('posedge clocks[0]', 'posedge clocks[0]'): -.1})

    def test_clock_identity_preserves_inversion(self):
        name = 'memory_clock|general[0].gpll~PLL_OUTPUT_COUNTER|divclk'
        self.assertEqual(clock_event(name + ' (INVERTED)'), 'negedge clocks[0]')
        with self.assertRaises(ValueError):
            clock_event(name.replace('memory_clock', 'other'))

    def test_all_corners_and_explicit_uncertainty_required(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for corner in CORNERS:
                folder = root / corner
                folder.mkdir()
                name = 'memory_clock|general[0].gpll~PLL_OUTPUT_COUNTER|divclk'
                (folder / 'internal-setup.rpt').write_text(
                    '\nPath #1: Setup slack is 1.0\n'
                    f'; Launch Clock ; {name} ;\n; Latch Clock ; {name} ;\n'
                    '; 9.9 ; -0.100 ; ; ; ; ; clock uncertainty ;\n')
            values, reports = uncertainties(root)
            self.assertEqual(values, {('posedge clocks[0]', 'posedge clocks[0]'): .1},
                        {('posedge clocks[0]', 'posedge clocks[0]'): 0})
            self.assertEqual(len(reports), 4)
            path = root / next(iter(CORNERS)) / 'internal-setup.rpt'
            path.write_text(path.read_text().replace('-0.100', '0.100'))
            with self.assertRaisesRegex(ValueError, 'reduce'):
                uncertainties(root)
            path.unlink()
            path.parent.rmdir()
            with self.assertRaisesRegex(ValueError, 'four'):
                uncertainties(root)
