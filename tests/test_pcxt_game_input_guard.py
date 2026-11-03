import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from scripts import pcxt_digger_readiness as readiness
from scripts import pcxt_game_input_guard as guard
from scripts import pcxt_dos_application_acceptance as app


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.evidence = root/'evidence.json'; self.evidence.write_text('synthetic unit test evidence')
        self.identity = {'kit':'A', 'target_id':'target', 'source_commit':'b'*40,
                         'package_id':'a'*64, 'capture_device':'fixture', 'capture_producer_sha256':'c'*64}
        doc = {'schema':'pcxt-game-timing-calibration-v1', 'measurement_method':'independent-hardware-reference',
               'identity':self.identity, 'upstream_ns':10, 'guest_ns':20, 'safe_age_ns':100,
               'evidence_sha256':hashlib.sha256(self.evidence.read_bytes()).hexdigest()}
        self.document = root/'calibration.json'; self.document.write_text(json.dumps(doc))
        self.sha = hashlib.sha256(self.document.read_bytes()).hexdigest()
        self.calibration = guard.load_calibration(self.document, self.evidence, self.identity, self.sha)
        self.clock = [126]
        self.positive = readiness.Observation(True, False, True, 0, 0, 50, 50)

    def make_guard(self, plan=((0x50, 300),)):
        return guard.GameInputGuard(self.calibration, 'stream', 1, plan, max_gap_ns=80,
                                    min_span_ns=20, sequence_step=1, clock=lambda:self.clock[0])

    def frame(self, seq, ns, observation=None):
        return readiness.Frame('stream', seq, ns, ns, observation or self.positive)

    def arm(self, value):
        value.observe(self.frame(1, 100), 100)
        value.observe(self.frame(2, 125), 125)

    def test_calibration_must_be_bound_and_evidence_unmodified(self):
        with self.assertRaises(guard.GuardError):
            guard.GameInputGuard(None, 'stream', 1, [(0x50,300)], max_gap_ns=80, min_span_ns=20, sequence_step=1)
        with self.assertRaises(guard.GuardError):
            guard.load_calibration(self.document, self.evidence, {'kit':'different'}, self.sha)
        self.evidence.write_text('changed')
        with self.assertRaises(guard.GuardError):
            guard.load_calibration(self.document, self.evidence, self.identity, self.sha)

    def test_fresh_observation_dispatches_once(self):
        g = self.make_guard(); self.arm(g); action = mock.Mock(return_value='ok')
        self.assertEqual(g.dispatch(0x50, action), 'ok')
        with self.assertRaises(guard.GuardError): g.dispatch(0x50, action)
        action.assert_called_once()

    def test_freshness_rechecked_after_owner_delay_and_threat(self):
        for update in ('delay', 'threat'):
            with self.subTest(update=update):
                g = self.make_guard(); self.arm(g); action = mock.Mock()
                if update == 'delay': self.clock[0] = 250
                else:
                    g.observe(self.frame(3, 130, readiness.Observation(True, True, True, 0, 21, 50, 50)), 130)
                    self.clock[0] = 130
                with self.assertRaises(guard.GuardError): g.dispatch(0x50, action)
                action.assert_not_called()
                self.clock[0] = 126

    def test_wrong_key_deadline_and_uncertain_response_close_guard(self):
        for case in ('wrong', 'deadline', 'uncertain'):
            with self.subTest(case=case):
                g = self.make_guard(); self.arm(g)
                action = mock.Mock(side_effect=RuntimeError('lost') if case == 'uncertain' else None)
                if case == 'deadline': self.clock[0] = 290
                with self.assertRaises((guard.GuardError, RuntimeError)):
                    g.dispatch(0x4f if case == 'wrong' else 0x50, action)
                if case != 'uncertain': action.assert_not_called()
                with self.assertRaises(guard.GuardError): g.dispatch(0x50, action)
                self.clock[0] = 126

    def test_new_input_requires_new_post_input_observations(self):
        g = self.make_guard(((0x50, 300), (0x4f, 500))); self.arm(g)
        first = mock.Mock(); second = mock.Mock(); g.dispatch(0x50, first)
        with self.assertRaises(guard.GuardError): g.dispatch(0x4f, second)
        second.assert_not_called()

    def test_three_planned_keys_require_new_observations_and_finish(self):
        g = self.make_guard(((0x50, 300), (0x4f, 500), (0x50, 700)))
        self.arm(g)
        action = mock.Mock()
        g.dispatch(0x50, action)
        g.observe(self.frame(3, 150), 150); g.observe(self.frame(4, 175), 175)
        self.clock[0] = 176; g.dispatch(0x4f, action)
        g.observe(self.frame(5, 200), 200); g.observe(self.frame(6, 225), 225)
        self.clock[0] = 226; g.dispatch(0x50, action)
        self.assertEqual(action.call_count, 3)
        self.assertTrue(g.closed)

    def test_late_input_response_stops_remaining_plan(self):
        g = self.make_guard(((0x50, 300), (0x4f, 500))); self.arm(g)
        def slow(): self.clock[0] = 147
        with self.assertRaisesRegex(guard.GuardError, 'completion exceeded'):
            g.dispatch(0x50, slow)
        with self.assertRaises(guard.GuardError): g.dispatch(0x4f, mock.Mock())

    def test_api_path_checks_guard_after_owner_and_before_post(self):
        g = self.make_guard(); self.arm(g); self.clock[0] = 126
        runner = mock.Mock(); runner.api.timeout = 20
        events = []
        def owner(*_):
            self.clock[0] = 250  # frame expires during ownership validation
        with mock.patch.object(app, 'validate_active', side_effect=owner), \
             mock.patch.object(app, 'input_status'), mock.patch.object(app.time, 'sleep'):
            with self.assertRaises(guard.GuardError):
                app.type_keys(runner, object(), object(), 'disk', 'input', events, (0x50,), dispatch_guard=g)
        runner.api.post_json.assert_not_called()


if __name__ == '__main__':
    unittest.main()
