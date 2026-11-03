"""Experimental one-shot game input guard; calibration must be independently reviewed.

This module cannot establish a hardware latency bound. Calibration files pin
operator-supplied bounds and identity to retained evidence; their measurements
and applicability must be assessed independently before a physical run.
"""
from dataclasses import dataclass
import hashlib
import json
from pathlib import Path
import re
import threading
import time

if __package__:
    from . import pcxt_digger_readiness as readiness
else:
    import pcxt_digger_readiness as readiness


class GuardError(RuntimeError):
    pass


_VERIFIED = object()
IDENTITY_KEYS = {'kit', 'target_id', 'source_commit', 'package_id', 'capture_device', 'capture_producer_sha256'}


@dataclass(frozen=True)
class Calibration:
    identity: dict
    upstream_ns: int
    guest_ns: int
    safe_age_ns: int
    evidence_sha256: str
    _verified: object


def load_calibration(path, evidence_path, expected_identity, calibration_sha256):
    """Check pinning/schema only; this does not validate the physical experiment."""
    try:
        path, evidence_path = Path(path), Path(evidence_path)
        if (path.is_symlink() or evidence_path.is_symlink() or not path.is_file()
                or not evidence_path.is_file() or path.stat().st_size > 1024*1024
                or not 0 < evidence_path.stat().st_size <= 64*1024*1024):
            raise ValueError('invalid calibration inputs')
        raw = path.read_bytes()
        if (not isinstance(calibration_sha256, str) or not re.fullmatch(r'[0-9a-f]{64}', calibration_sha256)
                or hashlib.sha256(raw).hexdigest() != calibration_sha256):
            raise ValueError('calibration digest differs')
        doc = json.loads(raw)
        evidence = hashlib.sha256(evidence_path.read_bytes()).hexdigest()
        identity = doc['identity']
        bounds = [doc[name] for name in ('upstream_ns', 'guest_ns', 'safe_age_ns')]
        if (doc.get('schema') != 'pcxt-game-timing-calibration-v1'
                or doc.get('measurement_method') != 'independent-hardware-reference'
                or not isinstance(expected_identity, dict) or set(expected_identity) != IDENTITY_KEYS
                or any(not isinstance(v, str) or not v for v in expected_identity.values())
                or not re.fullmatch(r'[0-9a-f]{40}', expected_identity['source_commit'])
                or not re.fullmatch(r'[0-9a-f]{64}', expected_identity['package_id'])
                or not re.fullmatch(r'[0-9a-f]{64}', expected_identity['capture_producer_sha256'])
                or identity != expected_identity or doc.get('evidence_sha256') != evidence
                or any(type(v) is not int or v <= 0 for v in bounds)
                or bounds[0] + bounds[1] >= bounds[2]):
            raise ValueError('calibration identity, evidence or bounds differ')
        return Calibration(dict(identity), *bounds, evidence, _VERIFIED)
    except (OSError, ValueError, TypeError, KeyError, json.JSONDecodeError) as exc:
        raise GuardError('timing calibration cannot be verified') from exc


class GameInputGuard:
    """Check the latest observation after ownership checks, then dispatch once.

    `observe` is called by the independent capture observer; `dispatch` is called
    by the input path after its ownership and attachment checks. A future live
    caller must pin and independently review the actual calibration evidence.
    """
    def __init__(self, calibration, stream_id, started_ns, plan, *, max_gap_ns,
                 min_span_ns, sequence_step, clock=time.monotonic_ns):
        if not isinstance(calibration, Calibration) or calibration._verified is not _VERIFIED:
            raise GuardError('an explicit timing calibration is required')
        if (not isinstance(plan, (list, tuple)) or not plan
                or any(not isinstance(x, tuple) or len(x) != 2 or type(x[0]) is not int
                       or type(x[1]) is not int or x[1] <= started_ns for x in plan)
                or any(plan[i][1] >= plan[i+1][1] for i in range(len(plan)-1))):
            raise GuardError('invalid planned inputs')
        self.calibration, self.stream_id, self.plan = calibration, stream_id, list(plan)
        self.gap, self.span, self.step, self.clock = max_gap_ns, min_span_ns, sequence_step, clock
        self.gate = self._new_gate(started_ns)
        self.lock = threading.Lock()
        self.closed = False
        self.failure = None

    def _new_gate(self, started_ns):
        return readiness.FreshnessGate(self.stream_id, self.calibration.safe_age_ns,
                                       self.gap, self.span, self.step, started_ns)

    def abort(self, reason):
        self.closed = True
        self.failure = reason
        raise GuardError(reason)

    def observe(self, frame, now_ns):
        with self.lock:
            if self.closed:
                return False
            value = self.gate.observe(frame, now_ns)
            if self.gate.closed:
                self.closed, self.failure = True, self.gate.reason
            return value

    def require_identity(self, expected):
        with self.lock:
            if self.closed or self.calibration.identity != expected:
                self.abort('calibration does not match the current capture and target')

    def dispatch(self, usage, action):
        with self.lock:
            if self.closed or not self.plan:
                self.abort('game input guard is closed')
            expected, deadline = self.plan[0]
            now = self.clock()
            if type(now) is not int or usage != expected or now > deadline:
                self.abort('game input does not match its planned key or deadline')
            if not self.gate.ready(now):
                self.abort('no fresh game observation after ownership check')
            frame = self.gate.previous
            age = now-frame.capture_ns+self.calibration.upstream_ns+self.calibration.guest_ns
            if age > self.calibration.safe_age_ns or now+self.calibration.guest_ns > deadline:
                self.abort('calibrated freshness or input deadline exceeded')
            if not self.gate.take(now):
                self.abort('game observation was consumed or became stale')
            try:
                value = action()
            except BaseException:
                self.closed, self.failure = True, 'input result uncertain'
                raise
            end = self.clock()
            if type(end) is not int or end < now or end-now > self.calibration.guest_ns or end > deadline:
                self.abort('input completion exceeded calibrated bound or deadline')
            self.plan.pop(0)
            if self.plan:
                self.gate = self._new_gate(end)
            else:
                self.closed = True
            return value
