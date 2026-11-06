"""Reversible game-input probe; gameplay acceptance remains independent review.

This module uses host freshness only to avoid obviously stale observations; it
makes no claim about capture-device buffering, guest-input time, or audio/video
synchronization. Ambiguous recorded response is inconclusive.
"""
import threading
import time


class ProbeError(RuntimeError):
    pass


class PlaybackProbe:
    """Bind each request to the newest processed frame from one source.

    The source reader calls `source_frame` before queuing each frame for the
    detector. The detector calls `processed` with its result. The same fault
    event must be set for any source, recorder, queue or detector failure.
    Frames observed before `arm` cannot authorize a key. This gate does not
    establish that a key reached the guest or prove gameplay.
    """
    def __init__(self, fault, *, max_host_age_ns, clock=time.monotonic_ns, short_smoke=False):
        if (not isinstance(fault, threading.Event) or type(max_host_age_ns) is not int
                or max_host_age_ns <= 0 or type(short_smoke) is not bool):
            raise ValueError('explicit fault event and positive host age are required')
        self.fault, self.max_host_age_ns, self.clock = fault, max_host_age_ns, clock
        self.lock = threading.Lock()
        self.latest_source = self.latest_result = None
        self.started_ns = None
        self.plan = [0x50] if short_smoke else [0x50, 0x4f, 0x50]
        self.sent = []
        self.closed = False

    def source_frame(self, frame):
        with self.lock:
            if self.latest_source is not None and frame.sequence != self.latest_source.sequence+1:
                self.fault.set()
            self.latest_source = frame

    def processed(self, frame, observation):
        with self.lock:
            if self.latest_result is not None and frame.sequence <= self.latest_result[0].sequence:
                self.fault.set()
            self.latest_result = (frame, observation)

    def arm(self, started_ns):
        with self.lock:
            if self.started_ns is not None or self.closed or type(started_ns) is not int or started_ns < 0:
                self.closed = True
                raise ProbeError('invalid or repeated game-start arm')
            self.started_ns = started_ns

    def snapshot(self, *, require_player):
        with self.lock:
            now = self.clock()
            if (self.closed or self.fault.is_set() or self.started_ns is None
                    or self.latest_source is None or self.latest_result is None):
                raise ProbeError('game observation is unavailable or faulted')
            frame, obs = self.latest_result
            if (frame.sequence != self.latest_source.sequence
                    or frame.capture_ns < self.started_ns
                    or type(now) is not int or not frame.arrival_ns <= now <= frame.arrival_ns+self.max_host_age_ns
                    or obs.threat or (require_player and (not obs.player or not obs.initial_lives))):
                raise ProbeError('latest game observation is stale, pending, or unsuitable')
            return {'sequence':frame.sequence, 'capture_ns':frame.capture_ns,
                    'arrival_ns':frame.arrival_ns, 'checked_ns':now,
                    'player':obs.player, 'initial_lives':obs.initial_lives, 'threat':obs.threat}

    def dispatch(self, usage, action):
        if self.closed or not self.plan or usage != self.plan[0]:
            self.closed = True
            raise ProbeError('game input is outside the declared left/right/left sequence')
        try:
            # The newest source frame may currently be in the detector. Wait
            # only for that pending read-only observation, never retry input.
            end = time.monotonic()+.25
            while True:
                try:
                    proof = self.snapshot(require_player=True)
                    break
                except ProbeError:
                    with self.lock:
                        pending = (not self.fault.is_set() and self.latest_source is not None
                                   and self.latest_result is not None
                                   and self.latest_source.sequence != self.latest_result[0].sequence)
                    if not pending or time.monotonic() >= end:
                        raise
                    time.sleep(.005)
            value = action()
        except BaseException:
            self.closed = True
            raise
        self.sent.append({'usage': usage, 'observation': proof})
        self.plan.pop(0)
        if not self.plan:
            self.closed = True
        return value
