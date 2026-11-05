import unittest
from dataclasses import replace

from scripts import pcxt_digger_readiness as readiness


class ReadinessTests(unittest.TestCase):
    def setUp(self):
        self.player = bytes(readiness.PLAYER_SIZE)
        self.threat = bytearray(readiness.THREAT_SIZE)
        self.lives = bytearray(readiness.LIVES_SIZE)
        for y in range(3):
            for x in list(range(8, 36)) + list(range(36, 66)):
                self.lives[(y*70+x)*3] = 200
        self.positive = readiness.classify(self.player, bytes(self.threat), bytes(self.lives), [self.player])
        self.negative = replace(self.positive, player=False)
        self.hazard = replace(self.positive, threat=True)

    def frame(self, seq, capture, obs=None, stream='capture', arrival=None):
        return readiness.Frame(stream, seq, capture, capture if arrival is None else arrival,
                               self.positive if obs is None else obs)

    def gate(self):
        return readiness.FreshnessGate('capture', max_age_ns=100, max_gap_ns=80,
                                       min_span_ns=20, sequence_step=1, started_ns=50)

    def test_classify_and_bounds(self):
        self.assertTrue(self.positive.player)
        self.assertTrue(self.positive.initial_lives)
        self.assertFalse(self.positive.threat)
        self.threat[:63] = bytes([0, 200, 0])*21
        self.assertTrue(readiness.classify(self.player, bytes(self.threat), bytes(self.lives), [self.player]).threat)
        with self.assertRaises(ValueError):
            readiness.classify(self.player[:-1], bytes(self.threat), bytes(self.lives), [self.player])
        with self.assertRaises(ValueError):
            readiness.classify(self.player, bytes(self.threat), bytes(self.lives), [])

    def test_requires_two_fresh_samples_and_is_one_shot(self):
        gate = self.gate()
        self.assertFalse(gate.observe(self.frame(1, 100), 101))
        self.assertTrue(gate.observe(self.frame(2, 125), 126))
        self.assertTrue(gate.ready(150))
        self.assertTrue(gate.take(150))
        self.assertFalse(gate.take(150))
        self.assertFalse(gate.observe(self.frame(3, 150), 151))

    def test_failed_preconditions_permanently_close(self):
        variants = [self.frame(1, 0), self.frame(1, 49), self.frame(1, 101, stream='other'),
                    self.frame(1, 101, arrival=99)]
        for frame in variants:
            with self.subTest(frame=frame):
                gate = self.gate()
                self.assertFalse(gate.observe(frame, 102))
                self.assertTrue(gate.closed)
                self.assertFalse(gate.observe(self.frame(2, 103), 104))

    def test_duplicate_reordered_and_gap_close(self):
        for frame in (self.frame(1, 125), self.frame(3, 125), self.frame(2, 100),
                      self.frame(2, 99), self.frame(2, 181)):
            with self.subTest(frame=frame):
                gate = self.gate()
                gate.observe(self.frame(1, 100), 100)
                self.assertFalse(gate.observe(frame, 181))
                self.assertTrue(gate.closed)

    def test_threat_or_respawn_after_candidate_closes(self):
        for observation in (self.hazard, self.negative, replace(self.positive, initial_lives=False)):
            with self.subTest(observation=observation):
                gate = self.gate()
                gate.observe(self.frame(1, 100), 100)
                self.assertFalse(gate.observe(self.frame(2, 125, observation), 125))
                self.assertFalse(gate.observe(self.frame(3, 150), 150))

    def test_stale_after_guard_and_unsuccessful_take_are_terminal(self):
        gate = self.gate()
        gate.observe(self.frame(1, 100), 100)
        gate.observe(self.frame(2, 125), 125)
        self.assertFalse(gate.take(226))
        self.assertTrue(gate.closed)
        gate = self.gate()
        gate.observe(self.frame(1, 100), 100)
        self.assertFalse(gate.take(100))
        self.assertTrue(gate.closed)

    def test_cannot_make_frame_younger_by_moving_clock_backwards(self):
        gate = self.gate()
        gate.observe(self.frame(1, 100), 100)
        gate.observe(self.frame(2, 125), 125)
        self.assertTrue(gate.ready(220))
        self.assertFalse(gate.ready(150))
        self.assertTrue(gate.closed)
        self.assertFalse(gate.take(220))
        gate = self.gate()
        self.assertFalse(gate.ready(200))
        self.assertFalse(gate.observe(self.frame(1, 150), 150))
        self.assertTrue(gate.closed)

    def test_non_candidate_before_first_candidate_does_not_arm(self):
        gate = self.gate()
        self.assertFalse(gate.observe(self.frame(1, 100, self.negative), 100))
        self.assertFalse(gate.observe(self.frame(2, 125), 125))
        self.assertTrue(gate.observe(self.frame(3, 150), 150))


if __name__ == '__main__':
    unittest.main()
