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


class PassiveGamepadTests(unittest.TestCase):
    def fixture(self):
        from test_nano_shared_tenfoot_trace import SharedModel, DESCRIPTOR
        model = SharedModel()
        client = model.client()
        client.key('launch', 0)
        mapping = dict(Up=82, Down=81, Left=80, Right=79, Buttons=[22,44]+[0]*30)
        pressed = [0]*5+[1]+[0]*3
        identity = client.view['native']['Identity']
        common = dict(scope='nano-computer-gamepad-v1', owner=client.owner, generation=1,
                      package_id=identity['PackageID'], build_id=identity['BuildID'],
                      descriptor_sha256=trace.CAPTURED_DESCRIPTOR_SHA256, mapping=mapping)
        down = dict(common, sequence=1, report='00800080800f0000', before=[0]*9, rows=pressed)
        up = dict(common, sequence=2, report='80800080800f0000', before=pressed, rows=[0]*9)
        for r in (down, up):
            r['state'] = trace.decode_captured_report(DESCRIPTOR, bytes.fromhex(r['report']))
        return model, client, DESCRIPTOR, mapping, pressed, [down, up]

    def raw(self, records):
        return b''.join(trace.PREFIX+json.dumps(r).encode()+b'\n' for r in records)

    def test_passive_left_release_then_normal_return_without_uart_input(self):
        model, client, descriptor, mapping, pressed, records = self.fixture()
        keys = model.keys[:]
        def receive(bound, done):
            self.assertEqual(bound, 60)
            self.assertFalse(done(self.raw(records[:1])))
            self.assertTrue(done(self.raw(records)))
            model.service.clock += .25
            return self.raw(records)
        result = trace.observe_captured_cycle(client, receive, descriptor=descriptor, mapping=mapping, pressed_rows=pressed)
        self.assertEqual(model.keys, keys)
        self.assertTrue(result['released'])
        self.assertTrue(result['descriptor_specific_report_decoded'])
        self.assertFalse(result['physical_input_verified'])
        self.assertFalse(client.retired)
        client.key('stop')
        self.assertEqual(client.view['native']['State'], 'ready')

    def test_corrupt_input_source_clock_and_ownership_retire_once(self):
        for fault in ('raw-state', 'wrong-control', 'sequence', 'owner', 'build', 'descriptor',
                      'gap', 'extra', 'partial', 'firmware', 'clock', 'late', 'claim', 'exception', 'app'):
            with self.subTest(fault=fault):
                model, client, descriptor, mapping, pressed, records = self.fixture()
                if fault == 'raw-state': records[0]['report'] = records[1]['report']
                if fault == 'wrong-control': records[0]['rows'] = [0,64]+[0]*7
                if fault == 'sequence': records[0]['sequence'] = 2
                if fault == 'owner': records[0]['owner'] += 1
                if fault == 'build': records[0]['build_id'] = 'e'*32
                if fault == 'descriptor': records[0]['descriptor_sha256'] = 'e'*64
                if fault == 'gap': records[1]['before'] = [0]*9
                raw = self.raw(records)
                if fault == 'extra': raw += self.raw(records[:1])
                if fault == 'partial': raw = raw[:-1]
                if fault == 'firmware': raw = b'FES_NANO_CORE_TRANSFER phase=complete\n'+raw
                if fault == 'app':
                    v = copy.deepcopy(client.view);v['native']['Generation'] = 3
                    raw = model.raw(v)+raw
                def receive(bound, done):
                    if fault == 'clock': model.service.clock -= 1
                    if fault == 'late': model.service.clock += bound
                    if fault == 'claim': model.service.owned = False
                    if fault == 'exception': raise OSError('read failed')
                    done(raw)
                    return raw
                keys = model.keys[:]
                with self.assertRaises((ValueError, OSError)):
                    trace.observe_captured_cycle(client, receive, descriptor=descriptor, mapping=mapping, pressed_rows=pressed)
                self.assertTrue(client.retired)
                self.assertFalse(client.active.locked())
                with self.assertRaises(ValueError):
                    trace.observe_captured_cycle(client, receive, descriptor=descriptor, mapping=mapping, pressed_rows=pressed)
                self.assertEqual(model.keys, keys)

    def test_descriptor_decoder_boundaries_and_busy_client(self):
        model, client, descriptor, mapping, pressed, _ = self.fixture()
        for raw in (b'', b'\0'*7, b'\0'*9):
            with self.assertRaises(ValueError): trace.decode_captured_report(descriptor, raw)
        with self.assertRaises(ValueError): trace.decode_captured_report(descriptor[:-1], b'\0'*8)
        decoded = trace.decode_captured_report(descriptor, bytes.fromhex('00ff000000f0ffff'))
        self.assertEqual(decoded, dict(Axes=[-32767,32767,0,0,0,0], Buttons=1023, Hat=-1))
        def unexpected(*_): self.fail('busy client invoked receive')
        client.active.acquire()
        try:
            with self.assertRaises(ValueError):
                trace.observe_captured_cycle(client, unexpected, descriptor=descriptor, mapping=mapping, pressed_rows=pressed)
        finally: client.active.release()
        self.assertFalse(client.retired)

    def test_full_three_core_cycle_with_passive_input_observer(self):
        from test_nano_shared_tenfoot_trace import SharedModel
        from tenfoot_trace import exercise
        _, _, descriptor, mapping, pressed, records = self.fixture()
        model = SharedModel();client = model.client();observed = []
        def during_play(index, running):
            self.assertEqual(running.view['native']['Generation'], 2*index+1)
            if index != 0: return
            def receive(bound, done):
                raw = self.raw(records)
                self.assertTrue(done(raw))
                model.service.clock += .1
                return raw
            observed.append(trace.observe_captured_cycle(running, receive, descriptor=descriptor, mapping=mapping, pressed_rows=pressed))
        result = exercise(client, settle=model.settle, during_play=during_play)
        self.assertTrue(result['passed'])
        self.assertEqual(len(observed), 1)
        self.assertEqual(len(model.keys), 8)
        self.assertTrue(client.retired)


if __name__ == '__main__':
    unittest.main()
