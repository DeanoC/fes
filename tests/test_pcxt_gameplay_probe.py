import threading
import unittest
from unittest import mock
from dataclasses import replace

from scripts.pcxt_capture_fanout import CapturedFrame
from scripts.pcxt_digger_readiness import Observation
from scripts.pcxt_gameplay_probe import PlaybackProbe, ProbeError


class PlaybackProbeTests(unittest.TestCase):
    def setUp(self):
        self.fault = threading.Event()
        self.clock = [110]
        self.probe = PlaybackProbe(self.fault, max_host_age_ns=50, clock=lambda:self.clock[0])
        self.obs = Observation(True, False, True, 0, 0, 50, 50)
        self.probe.arm(90)

    def frame(self, seq, ns):
        return CapturedFrame(seq, ns, ns+1, ns+2, 0x2000, b'frame')

    def publish(self, seq, ns, obs=None):
        frame = self.frame(seq, ns)
        self.probe.source_frame(frame)
        self.probe.processed(frame, obs or self.obs)

    def test_only_planned_left_right_left_and_record_observations(self):
        action = mock.Mock()
        for sequence, usage in enumerate((0x50, 0x4f, 0x50), start=1):
            self.publish(sequence, 100+sequence)
            self.clock[0] = 110+sequence
            self.probe.dispatch(usage, action)
        self.assertEqual(action.call_count, 3)
        self.assertEqual([entry['usage'] for entry in self.probe.sent], [0x50, 0x4f, 0x50])
        with self.assertRaises(ProbeError): self.probe.dispatch(0x50, action)

    def test_short_smoke_allows_only_one_fully_guarded_left(self):
        self.probe = PlaybackProbe(self.fault, max_host_age_ns=50,
                                   clock=lambda:self.clock[0], short_smoke=True)
        self.probe.arm(90)
        self.publish(1, 100)
        action = mock.Mock()
        self.probe.dispatch(0x50, action)
        self.assertEqual(action.call_count, 1)
        self.assertEqual([item['usage'] for item in self.probe.sent], [0x50])
        with self.assertRaises(ProbeError): self.probe.dispatch(0x4f, action)
        self.assertEqual(action.call_count, 1)

    def test_short_smoke_still_blocks_unsafe_observations(self):
        for mode in ('stale', 'pending', 'threat', 'player', 'initial_lives'):
            with self.subTest(mode=mode):
                self.setUp()
                self.probe = PlaybackProbe(self.fault, max_host_age_ns=50,
                                           clock=lambda:self.clock[0], short_smoke=True)
                self.probe.arm(90)
                obs = replace(self.obs, **({mode: False} if mode in ('player', 'initial_lives') else
                                           {'threat': True} if mode == 'threat' else {}))
                self.publish(1, 100, obs)
                if mode == 'pending': self.probe.source_frame(self.frame(2, 105))
                if mode == 'stale': self.clock[0] = 153
                action = mock.Mock()
                with self.assertRaises(ProbeError): self.probe.dispatch(0x50, action)
                action.assert_not_called()

    def test_pending_or_stale_frame_cannot_dispatch(self):
        for mode in ('pending', 'stale', 'fault', 'threat', 'wrong'):
            with self.subTest(mode=mode):
                self.setUp(); self.publish(1, 100)
                if mode == 'pending': self.probe.source_frame(self.frame(2, 105))
                if mode == 'stale': self.clock[0] = 153
                if mode == 'fault': self.fault.set()
                if mode == 'threat':
                    self.publish(2, 105, replace(self.obs, threat=True))
                action = mock.Mock()
                with self.assertRaises(ProbeError):
                    self.probe.dispatch(0x4f if mode == 'wrong' else 0x50, action)
                action.assert_not_called()

    def test_initial_player_and_post_start_frame_required(self):
        self.publish(1, 100, replace(self.obs, player=False))
        with self.assertRaises(ProbeError): self.probe.dispatch(0x50, mock.Mock())
        probe = PlaybackProbe(threading.Event(), max_host_age_ns=50, clock=lambda:110)
        frame = self.frame(1, 80); probe.source_frame(frame); probe.processed(frame, self.obs); probe.arm(90)
        with self.assertRaises(ProbeError): probe.dispatch(0x50, mock.Mock())

    def test_lost_player_or_lives_stops_later_inputs(self):
        for lost in ('player', 'initial_lives'):
            for accepted in (1, 2):
                with self.subTest(lost=lost, accepted=accepted):
                    self.setUp()
                    action = mock.Mock()
                    plan = (0x50, 0x4f, 0x50)
                    for seq, usage in enumerate(plan[:accepted], 1):
                        self.publish(seq, 100 + seq)
                        self.probe.dispatch(usage, action)
                    self.publish(accepted + 1, 103, replace(self.obs, **{lost: False}))
                    with self.assertRaises(ProbeError):
                        self.probe.dispatch(plan[accepted], action)
                    self.assertEqual(action.call_count, accepted)
                    self.assertTrue(self.probe.closed)
                    with self.assertRaises(ProbeError):
                        self.probe.dispatch(plan[accepted], action)
                    self.assertEqual(action.call_count, accepted)

    def test_uncertain_action_prevents_later_keys(self):
        self.publish(1, 100)
        with self.assertRaises(RuntimeError):
            self.probe.dispatch(0x50, mock.Mock(side_effect=RuntimeError('lost reply')))
        with self.assertRaises(ProbeError): self.probe.dispatch(0x4f, mock.Mock())


if __name__ == '__main__': unittest.main()
