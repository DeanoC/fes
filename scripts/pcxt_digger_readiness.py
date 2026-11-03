"""Experimental offline Digger readiness analysis; never authorizes kit input.

These helpers deliberately have no capture-device or input implementation. The
capture path and its upstream latency have not been qualified for live gating.
The supplied templates must come from private captures of the exact game build.
"""
from dataclasses import dataclass

PLAYER_SIZE = 32 * 34 * 3
THREAT_SIZE = 80 * 76 * 3
LIVES_SIZE = 70 * 30 * 3


@dataclass(frozen=True)
class Observation:
    player: bool
    threat: bool
    initial_lives: bool
    player_distance: float
    green_pixels: int
    left_life_red_pixels: int
    right_life_red_pixels: int


def classify(player, threat, lives, templates):
    """Classify fixed 640x360 crops, not a general gameplay or safety check.

    Coordinates in the original frame are player=(304,322,32,34),
    threat=(350,280,80,76), lives=(190,0,70,30). No game pixels are
    distributed by this module. Templates and thresholds reflect only two very
    similar recorded runs and must not be promoted to a live safety decision.
    """
    if not isinstance(player, bytes) or len(player) != PLAYER_SIZE:
        raise ValueError('invalid player crop')
    if not isinstance(threat, bytes) or len(threat) != THREAT_SIZE:
        raise ValueError('invalid threat crop')
    if not isinstance(lives, bytes) or len(lives) != LIVES_SIZE:
        raise ValueError('invalid lives crop')
    if not templates or any(not isinstance(t, bytes) or len(t) != PLAYER_SIZE for t in templates):
        raise ValueError('invalid player templates')
    difference = min(sum(abs(a-b) for a, b in zip(player, t)) for t in templates)
    green = sum(g > 100 and r < 100 and b < 100
                for r, g, b in zip(threat[0::3], threat[1::3], threat[2::3]))

    def red_count(x0, x1):
        return sum(lives[i] > 110 and lives[i+1] < 80 and lives[i+2] < 80
                   for y in range(25) for x in range(x0, x1)
                   for i in [(y*70+x)*3])

    left, right = red_count(8, 36), red_count(36, 66)
    return Observation(difference <= PLAYER_SIZE, green > 20,
                       left >= 50 and right >= 50, difference / PLAYER_SIZE,
                       green, left, right)


@dataclass(frozen=True)
class Frame:
    """Sequence/time are assertions by a future qualified capture producer."""
    stream_id: str
    sequence: int
    capture_ns: int
    arrival_ns: int
    observation: Observation


class FreshnessGate:
    """Offline model of a one-shot gate. Never call this input authorization.

    Limits are explicit because no device-specific limits have been established.
    Construct once with the known game-start event time, then call observe for
    each selected source frame. A failed timing/source condition,
    or a lost candidate after the first match, closes the gate permanently.
    Calling take consumes the gate regardless of success; no implicit retry.
    """
    def __init__(self, stream_id, max_age_ns, max_gap_ns, min_span_ns, sequence_step, started_ns):
        values = (max_age_ns, max_gap_ns, min_span_ns, sequence_step)
        if (not isinstance(stream_id, str) or not stream_id
                or any(type(v) is not int or v <= 0 for v in values)
                or min_span_ns > max_gap_ns or type(started_ns) is not int or started_ns < 0):
            raise ValueError('invalid gate policy')
        self.stream_id, self.max_age_ns, self.max_gap_ns, self.min_span_ns, self.sequence_step = stream_id, *values
        self.previous = None
        self.start_ns = None
        self.started_ns = started_ns
        self.last_now_ns = started_ns
        self.closed = False
        self.reason = None

    def close(self, reason):
        self.closed = True
        self.reason = reason
        return False

    def observe(self, frame, now_ns):
        if self.closed:
            return False
        if (not isinstance(frame, Frame) or frame.stream_id != self.stream_id
                or type(frame.sequence) is not int or frame.sequence < 0
                or type(frame.capture_ns) is not int or type(frame.arrival_ns) is not int
                or type(now_ns) is not int or not isinstance(frame.observation, Observation)
                or any(type(v) is not bool for v in (frame.observation.player,
                       frame.observation.threat, frame.observation.initial_lives))
                or not self.started_ns <= frame.capture_ns <= frame.arrival_ns <= now_ns
                or now_ns < self.last_now_ns
                or now_ns - frame.capture_ns > self.max_age_ns):
            return self.close('invalid or stale frame')
        self.last_now_ns = now_ns
        if self.previous is not None:
            prev = self.previous
            if (frame.sequence != prev.sequence + self.sequence_step or frame.capture_ns <= prev.capture_ns
                    or frame.arrival_ns <= prev.arrival_ns
                    or frame.capture_ns - prev.capture_ns > self.max_gap_ns):
                return self.close('source discontinuity')
        self.previous = frame
        obs = frame.observation
        candidate = obs.player and obs.initial_lives and not obs.threat
        if self.start_ns is not None and not candidate:
            return self.close('candidate lost or threat detected')
        if candidate and self.start_ns is None:
            self.start_ns = frame.capture_ns
        return self.ready(now_ns)

    def ready(self, now_ns):
        if self.closed:
            return False
        if type(now_ns) is not int or now_ns < self.last_now_ns:
            return self.close('invalid current time')
        self.last_now_ns = now_ns
        if self.previous is None or self.start_ns is None:
            return False
        if now_ns - self.previous.capture_ns > self.max_age_ns:
            return self.close('candidate became stale')
        return self.previous.capture_ns - self.start_ns >= self.min_span_ns

    def take(self, now_ns):
        result = self.ready(now_ns)
        self.close('consumed' if result else 'not ready')
        return result
