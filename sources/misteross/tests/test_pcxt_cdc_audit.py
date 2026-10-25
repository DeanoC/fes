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

    def test_estimated_timing_rejects(self):
        self.timing['timing_summary']['final_analogue_model'] = False
        self.reject()


if __name__ == '__main__':
    unittest.main()
