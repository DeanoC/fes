"""Synthetic write receipts; no physical input evidence."""
import copy
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'platform/embedded/nano'))
import gamepad_delivery_trace as trace


class GamepadDeliveryTests(unittest.TestCase):
    def fixture(self):
        mapping = dict(Up=82, Down=81, Left=80, Right=79, Buttons=[22]+[0]*31)
        expected = dict(owner=123, generation=1, package_id='a'*64, build_id='b'*32,
                        descriptor_sha256='c'*64, mapping=mapping)
        down = dict(expected, scope='nano-computer-gamepad-v1', sequence=1,
                    report='80800080801f0000', state=dict(Axes=[0]*6, Buttons=1, Hat=-1),
                    before=[0]*9, rows=[0,64]+[0]*7)
        up = copy.deepcopy(down)
        up.update(sequence=2, report='80800080800f0000', before=down['rows'][:], rows=[0]*9)
        up['state']['Buttons'] = 0
        return expected, [down, up]

    def encode(self, records):
        return [json.dumps(r).encode() for r in records]

    def test_bound_press_and_release_and_no_physical_claim(self):
        expected, records = self.fixture()
        result = trace.validate_cycle(self.encode(records), **expected)
        self.assertEqual(result, dict(completed_writes=2, released=True, physical_input_verified=False))

    def test_corrupt_receipts_reject(self):
        expected, records = self.fixture()
        mutations = [
            ('owner', True), ('generation', 2), ('sequence', True), ('sequence', 1 << 64),
            ('scope', 'physical-input'), ('package_id', 'A'*64), ('build_id', 'b'*64),
            ('descriptor_sha256', 'bad'), ('report', '00'*65), ('report', '0'),
            ('state', dict(Axes=[True]*6, Buttons=1, Hat=-1)),
            ('state', dict(Axes=[0]*6, Buttons=True, Hat=-1)),
            ('state', dict(Axes=[0]*6, Buttons=1, Hat=8)),
            ('state', dict(Axes=[0]*6, Buttons=0, Hat=-1)),
            ('before', [0]*8), ('rows', [0,True]+[0]*7), ('rows', [0]*9),
            ('mapping', dict(expected['mapping'], Buttons=[1]+[0]*31)),
        ]
        for key, value in mutations:
            with self.subTest(key=key, value=value):
                bad = copy.deepcopy(records)
                bad[0][key] = value
                with self.assertRaises(ValueError):
                    trace.validate_cycle(self.encode(bad), **expected)
        raw = json.dumps(records[0]).encode()
        for bad in (raw[:-1], raw[:-1]+b',"sequence":1}', raw[:-1]+b',"extra":NaN}'):
            with self.assertRaises(ValueError):
                trace.observation(bad)

    def test_stale_foreign_missing_duplicate_and_unreleased_chains(self):
        expected, records = self.fixture()
        variants = [[], records[:1], records*2]
        for key, value in [('owner', 124), ('generation', 3), ('package_id', 'd'*64),
                           ('build_id', 'd'*32), ('descriptor_sha256', 'd'*64), ('sequence', 3)]:
            bad = copy.deepcopy(records)
            bad[1][key] = value
            variants.append(bad)
        bad = copy.deepcopy(records)
        bad[1]['before'] = [1]+[0]*8
        variants.append(bad)
        for bad in variants:
            with self.subTest(records=bad), self.assertRaises(ValueError):
                trace.validate_cycle(self.encode(bad), **expected)

    def test_hat_override_and_modifier_mapping(self):
        _, records = self.fixture()
        r = records[0]
        r['mapping']['Up'] = 225
        r['state'] = dict(Axes=[32767,32767,0,0,0,0], Buttons=0, Hat=0)
        r['rows'] = [0]*8+[2]
        self.assertEqual(trace.observation(self.encode([r])[0])['rows'], r['rows'])

    def test_legacy_cycle_cannot_silently_count_delivery_as_firmware(self):
        from test_nano_tenfoot_trace import Model
        import tenfoot_trace
        _, records = self.fixture()
        model = Model()
        raw = trace.PREFIX+self.encode(records)[0]+b'\n'+model.raw(model.state())
        with self.assertRaises(ValueError):
            tenfoot_trace.packet(raw)


if __name__ == '__main__':
    unittest.main()
