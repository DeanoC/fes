"""Explicit bounded input diagnostics; no inferred core mappings or ownership."""
from contextlib import contextmanager
from dataclasses import dataclass
import hashlib
import json
import math
import os
import re
import signal
import stat
import time

MAX_BYTES = 65536
MAX_EVENTS = 64


@dataclass(frozen=True)
class Diagnostic:
    raw: bytes
    sha256: str
    interface: dict
    events: tuple


def _object(value, keys):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError("invalid input diagnostic fields")


def _integer(value, low, high):
    return type(value) is int and low <= value <= high


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate input diagnostic field")
        result[key] = value
    return result


def load_diagnostic(path, expected_sha256):
    """Read bounded regular JSON once. Invalid inputs raise ValueError."""
    if not isinstance(expected_sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", expected_sha256):
        raise ValueError("invalid expected input SHA-256")
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(fd, "rb") as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or not 0 < before.st_size <= MAX_BYTES:
                raise ValueError("input diagnostic must be a nonempty bounded regular file")
            raw = stream.read(MAX_BYTES + 1)
            after = os.fstat(stream.fileno())
    except OSError as exc:
        raise ValueError("cannot snapshot input diagnostic") from exc
    if (len(raw) != before.st_size or len(raw) > MAX_BYTES
            or before.st_mtime_ns != after.st_mtime_ns or before.st_ctime_ns != after.st_ctime_ns):
        raise ValueError("input diagnostic changed while reading")
    digest = hashlib.sha256(raw).hexdigest()
    if digest != expected_sha256:
        raise ValueError("input diagnostic SHA-256 mismatch")
    try:
        data = json.loads(raw, object_pairs_hook=_pairs)
    except (UnicodeError, ValueError) as exc:
        raise ValueError("invalid input diagnostic JSON") from exc
    _object(data, ("format", "interface", "events"))
    if type(data["format"]) is not int or data["format"] != 1:
        raise ValueError("unsupported input diagnostic format")
    interface = data["interface"]
    _object(interface, ("id", "major", "minor"))
    if (interface["id"] not in ("fes.keyboard", "fes.gamepad")
            or not _integer(interface["major"], 1, 65535)
            or not _integer(interface["minor"], 0, 65535)):
        raise ValueError("invalid input interface")
    events = data["events"]
    if not isinstance(events, list) or not 1 <= len(events) <= MAX_EVENTS:
        raise ValueError("input diagnostic requires 1..64 events")
    pressed, axes = set(), {}
    for event in events:
        _object(event, ("device", "kind", "action", "code", "value"))
        device, kind, action, code, value = (event[k] for k in ("device", "kind", "action", "code", "value"))
        if not all(type(v) is int for v in (device, kind, action, code, value)):
            raise ValueError("input event fields must be integers")
        if kind == 2:
            if device != 1 or code not in (200, 201) or action != 2 or not -32768 <= value <= 32767:
                raise ValueError("invalid absolute axis event")
            axes[code] = value
        else:
            keyboard = device == 0 and kind == 0 and 256 <= code <= 295
            button = device == 1 and kind == 1 and 100 <= code <= 112
            if not (keyboard or button) or action not in (0, 1) or value != 0:
                raise ValueError("invalid key/button event")
            if interface["id"] == "fes.gamepad" and keyboard:
                raise ValueError("keyboard event requires keyboard interface")
            if action == 1:
                if code in pressed:
                    raise ValueError("duplicate input press")
                pressed.add(code)
            else:
                if code not in pressed:
                    raise ValueError("input release without press")
                pressed.remove(code)
    if pressed or any(axes.values()):
        raise ValueError("input diagnostic must finish with released keys and neutral axes")
    return Diagnostic(raw, digest, interface, tuple(events))


def validate_timeout(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not 0 < value <= 60:
        raise ValueError("input timeout must be finite and within (0, 60] seconds")


@contextmanager
def _deadline(seconds):
    """Hard phase cap also covers DNS and trickling HTTP response bodies."""
    def expired(*_):
        raise ValueError("input diagnostic timed out")
    previous = signal.getsignal(signal.SIGALRM)
    if signal.getitimer(signal.ITIMER_REAL) != (0.0, 0.0):
        raise ValueError("input diagnostic cannot replace an existing alarm")
    signal.signal(signal.SIGALRM, expired)
    signal.setitimer(signal.ITIMER_REAL, seconds)
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)


def run_diagnostic(runner, expected, diagnostic, timeout):
    """Called inside existing owned-Stop cleanup. Never retry an event POST."""
    validate_timeout(timeout)
    input_id = resyncs = last_frames = None
    deadline = time.monotonic() + timeout
    original_timeout = runner.api.timeout

    def remaining():
        budget = deadline - time.monotonic()
        if budget <= 0:
            raise ValueError("input diagnostic timed out")
        return budget

    def clamp():
        runner.api.timeout = min(original_timeout, remaining())

    def observe():
        nonlocal input_id, resyncs, last_frames
        clamp()
        session = runner.session()
        remaining()
        if runner.session_identity(session, "input diagnostic") != expected:
            raise ValueError("input diagnostic session ownership changed")
        interfaces = (session.get("core_package") or {}).get("active_interfaces")
        required = diagnostic.interface
        if not isinstance(interfaces, list) or not any(
                isinstance(item, dict) and item.get("id") == required["id"]
                and type(item.get("major")) is int and item["major"] == required["major"]
                and type(item.get("minor")) is int and item["minor"] == required["minor"]
                for item in interfaces):
            raise ValueError("required input interface is not active")
        status = session.get("input")
        if not isinstance(status, dict) or status.get("state") != "attached" or status.get("ready") is not True:
            raise ValueError("owned input is not attached and ready")
        if status.get("source") == "launcher":
            raise ValueError("launcher owns input; refusing to steal its source")
        identity = status.get("session_id")
        if not isinstance(identity, str) or not 1 <= len(identity) <= 256:
            raise ValueError("missing owned input session identity")
        metrics = status.get("metrics")
        if not isinstance(metrics, dict):
            raise ValueError("missing input metrics")
        frames, sync = metrics.get("frames_sent"), metrics.get("state_resyncs")
        if not _integer(frames, 0, 2**64 - 1) or not _integer(sync, 0, 2**64 - 1):
            raise ValueError("invalid input counters")
        if input_id is not None and (identity != input_id or sync != resyncs):
            raise ValueError("input session changed or reconnected")
        if last_frames is not None and frames < last_frames:
            raise ValueError("input frame counter regressed")
        input_id, resyncs, last_frames = identity, sync, frames
        return frames

    try:
        with _deadline(timeout):
            before = observe()
            acknowledged = 0
            for event in diagnostic.events:
                observe()
                clamp()
                response = runner.api.post_json("/api/v1/session/input/event", {"event": event})
                remaining()
                if not isinstance(response, dict) or response.get("ok") is not True:
                    raise ValueError("input event acknowledgement is invalid")
                acknowledged += 1
            for attempt in range(runner.args.poll_attempts):
                after = observe()
                if after > before:
                    return dict(sha256=diagnostic.sha256, interface=diagnostic.interface,
                                events_requested=len(diagnostic.events), events_acknowledged=acknowledged,
                                frames_before=before, frames_after=after, frame_delta=after - before,
                                input_session_id=input_id)
                if attempt + 1 < runner.args.poll_attempts:
                    time.sleep(min(runner.args.poll_interval, remaining()))
            raise ValueError("input diagnostic observed no frame progress")
    finally:
        runner.api.timeout = original_timeout
