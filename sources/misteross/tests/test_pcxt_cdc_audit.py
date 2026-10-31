"""Corrupt actual native synchronizer excerpts; every unsafe change must reject."""
import copy
import gzip
import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('pcxt_cdc', ROOT / 'scripts/audit_fes_pcxt_cdc.py')
cdc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(cdc)
FIXTURE = json.loads(gzip.decompress((ROOT / 'tests/fixtures/pcxt_cdc_board50.json.gz').read_bytes()))


class PcxtCdcAuditTests(unittest.TestCase):
    def setUp(self):
        self.routed = copy.deepcopy(FIXTURE['routed'])
        self.timing = copy.deepcopy(FIXTURE['timing'])
        self.good = cdc.audit(self.routed, self.timing)
        self.chain = next(c for c in self.good['chains'] if c['identity_buffers'])
        self.cells = self.routed['modules']['top']['cells']

    def reject(self):
        with self.assertRaises(ValueError):
            cdc.audit(self.routed, self.timing)

    def change_pin(self, cell, pin, value):
        attrs = self.cells[cell]['attributes']
        data = json.loads(bytes.fromhex(attrs['FES_PINMAP_V1']))
        data['pins'][pin][0] = value
        attrs['FES_PINMAP_V1'] = json.dumps(data).encode().hex()

    def tail_row(self):
        return next(e for n in self.timing['detailed_net_timings'] for e in n['endpoints']
                    if e['cell'] == self.chain['tail'] and e['port'] == 'DATAIN')

    def test_actual_excerpt_keeps_six_chains_without_signoff(self):
        self.assertEqual(len(self.good['chains']), 6)
        self.assertFalse(self.good['timing_exception_valid'])
        self.assertFalse(self.good['physical_qualified'])

    def test_extra_interstage_consumer_rejects(self):
        head = self.cells[self.chain['head']]
        self.cells['extra'] = {'type': 'MISTRAL_OB', 'connections': {'I': head['connections']['Q']},
                               'port_directions': {'I': 'input'}}
        self.reject()

    def declare_pixel_identity(self):
        name = 'declared_pixel_output'
        m = self.routed['modules']['top']
        bits = m['netnames'].pop('clk_pixel')['bits']
        m['netnames'][name] = {'bits': bits}
        clocks = json.loads(m['settings']['timing/io_clocks'])
        for clock in clocks:
            if clock['net'] == 'clk_pixel':
                clock.update(net=name, group='video_clock')
        m['settings']['timing/io_clocks'] = json.dumps(clocks)
        self.timing['fmax'][name] = self.timing['fmax'].pop('clk_pixel')
        for net in self.timing['detailed_net_timings']:
            for row in [net, *net['endpoints']]:
                for field in ('event', 'launch_event'):
                    if field in row:
                        row[field] = row[field].replace('clk_pixel', name)
        self.cells['video_clock'] = {'type': 'altera_pll',
            'connections': {'outclk': [999996], 'outclk[1]': [999997]},
            'port_directions': {'outclk': 'output', 'outclk[1]': 'output'}}
        self.cells['declared_buffer'] = {'type': 'MISTRAL_CLKBUF', 'parameters': {},
            'connections': {'A': [999996], 'Q': bits},
            'port_directions': {'A': 'input', 'Q': 'output'},
            'attributes': {'FES_PINMAP_V1': json.dumps(
                {'count': 2, 'pins': {'A': [0, 'A'], 'Q': [0, 'Q']}}).encode().hex()}}
        return name

    def test_explicit_pixel_identity_requires_architecture_owned_root(self):
        name = self.declare_pixel_identity()
        self.reject()  # Ordinary callers still require the original identity.
        result = cdc.audit(self.routed, self.timing, pixel_clock=name)
        self.assertEqual(len(result['chains']), 6)
        self.assertFalse(result['physical_qualified'])

    def test_declared_pixel_bound_to_the_forwarded_counter_rejects(self):
        name = self.declare_pixel_identity()
        self.cells['declared_buffer']['connections']['A'] = [999997]
        with self.assertRaisesRegex(ValueError, 'output zero'):
            cdc.audit(self.routed, self.timing, pixel_clock=name)

    def test_missing_declared_pixel_root_or_pin_metadata_rejects(self):
        name = self.declare_pixel_identity()
        self.cells['declared_buffer']['attributes'].clear()
        with self.assertRaisesRegex(ValueError, 'pin metadata'):
            cdc.audit(self.routed, self.timing, pixel_clock=name)
        self.cells.pop('video_clock')
        with self.assertRaisesRegex(ValueError, 'output zero'):
            cdc.audit(self.routed, self.timing, pixel_clock=name)

    def test_nonzero_declared_pixel_phase_rejects(self):
        name = self.declare_pixel_identity()
        m = self.routed['modules']['top']
        clocks = json.loads(m['settings']['timing/io_clocks'])
        for clock in clocks:
            if clock['net'] == name:
                clock['phase'] = 8.979
        m['settings']['timing/io_clocks'] = json.dumps(clocks)
        with self.assertRaisesRegex(ValueError, 'phase'):
            cdc.audit(self.routed, self.timing, pixel_clock=name)

    def test_inverted_physical_buffer_rejects(self):
        self.change_pin(self.chain['identity_buffers'][0], 'A', 3)
        self.reject()

    def test_different_stage_clock_rejects(self):
        self.cells[self.chain['tail']]['connections']['CLK'] = [999999]
        self.reject()

    def test_disabled_stage_enable_rejects(self):
        self.change_pin(self.chain['head'], 'ENA', 1)
        self.reject()

    def test_mismatched_reset_polarity_rejects(self):
        self.change_pin(self.chain['tail'], 'ACLR', 3)
        self.reject()

    def test_lost_synchronizer_stage_rejects(self):
        self.cells[self.chain['head']]['attributes'].pop('async_reg')
        self.reject()

    def test_missing_native_pin_metadata_rejects(self):
        self.cells[self.chain['head']]['attributes'].pop('FES_PINMAP_V1')
        self.reject()

    def test_cut_interstage_check_rejects(self):
        self.tail_row()['hold_checked'] = False
        self.reject()

    def test_wrong_capture_event_rejects(self):
        self.tail_row()['event'] = 'negedge ' + self.chain['clock']
        self.reject()

    def test_failed_or_nonfinite_interstage_margin_rejects(self):
        row = self.tail_row()
        for value in (-0.001, float('nan'), float('inf')):
            with self.subTest(value=value):
                row['setup_slack_ns'] = value
                self.reject()

    def consumer(self):
        stages = {c[k] for c in self.good['chains'] for k in ('head', 'tail')}
        cone = self.good['reset_consumers'][0]
        return next(c for c in cone['terminals'] if c['cell'] not in stages and
                    c['port'] == 'DATAIN')

    def test_reset_cones_are_timed_but_additional_async_controls_stay_open(self):
        self.assertEqual(sum(len(c['terminals']) for c in self.good['reset_consumers']), 1021)
        self.assertEqual(sum(c['asynchronous_clear_consumers']
                             for c in self.good['reset_consumers']), 2)
        self.assertFalse(self.good['reset_consumer_policy_valid'])
        self.assertFalse(self.good['timing_exception_valid'])

    def test_missing_reset_consumer_row_rejects(self):
        c = self.consumer()
        for net in self.timing['detailed_net_timings']:
            net['endpoints'] = [e for e in net['endpoints']
                                if (e['cell'], e['port']) != (c['cell'], c['port'])]
        self.reject()

    def test_different_reset_consumer_clock_rejects(self):
        c = self.consumer()
        self.cells[c['cell']]['connections']['CLK'] = [999999]
        self.reject()

    def test_cut_reset_consumer_hold_rejects(self):
        c = self.consumer()
        for net in self.timing['detailed_net_timings']:
            for row in net['endpoints']:
                if (row['cell'], row['port']) == (c['cell'], c['port']):
                    row['hold_checked'] = False
        self.reject()

    def test_new_unmodeled_async_consumer_rejects(self):
        c = self.consumer()
        self.cells['extra_reset'] = copy.deepcopy(self.cells[c['cell']])
        self.cells['extra_reset']['connections']['Q'] = [999999]
        self.cells['extra_reset']['connections']['ACLR'] = self.cells[
            self.good['reset_consumers'][0]['tail']]['connections']['Q']
        self.change_pin('extra_reset', 'ACLR', 0)
        self.reject()

    def test_combinational_reset_loop_rejects(self):
        root = self.cells[self.good['reset_consumers'][0]['tail']]['connections']['Q']
        self.cells['extra_loop'] = {
            'type': 'MISTRAL_ALUT2', 'connections': {'A': root, 'B': [999999], 'Q': [999999]},
            'port_directions': {'A': 'input', 'B': 'input', 'Q': 'output'}}
        self.reject()

    def test_estimated_timing_rejects(self):
        self.timing['timing_summary']['final_analogue_model'] = False
        self.reject()


if __name__ == '__main__':
    unittest.main()
