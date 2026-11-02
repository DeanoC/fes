"""Capture a bounded, sanitized projection of the same host-health response.

No extra request, retry, or acceptance change is performed. Local evidence
failure fails closed before the original health response can be used.
"""
import fcntl
import errno
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import stat
import socket
import threading
import time
from urllib.parse import urlsplit
from urllib.error import HTTPError, URLError


class EvidenceError(RuntimeError):
    pass


def boolean(obj, key):
    if not isinstance(obj, dict) or key not in obj:
        return 'missing'
    return obj[key] if type(obj[key]) is bool else 'invalid'


def match(obj, key, expected):
    if expected is None:
        return 'not_checked'
    if not isinstance(obj, dict) or key not in obj:
        return 'missing'
    if not isinstance(obj[key], str) or not obj[key]:
        return 'invalid'
    return 'match' if obj[key] == expected else 'mismatch'


def endpoint(value):
    if value is None:
        return {'kind': 'missing'}
    if not isinstance(value, str) or len(value) > 512:
        return {'kind': 'invalid'}
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in ('http', 'https') or parsed.username is not None or
                parsed.password is not None or parsed.query or parsed.fragment or
                parsed.path not in ('', '/') or not parsed.hostname):
            return {'kind': 'invalid'}
        hostname = parsed.hostname.lower()
        port = parsed.port or (443 if parsed.scheme == 'https' else 80)
        try:
            ip = ipaddress.ip_address(hostname)
            kind = 'ipv4' if ip.version == 4 else 'ipv6'
            hostname = ip.compressed
        except ValueError:
            if not re.fullmatch(r'[a-z0-9.-]{1,253}', hostname) or '..' in hostname:
                return {'kind': 'invalid'}
            kind = 'hostname'
        canonical = parsed.scheme + '://' + hostname + ':' + str(port)
        return {'kind': kind, 'fingerprint': hashlib.sha256(canonical.encode('ascii')).hexdigest()}
    except (ValueError, UnicodeError):
        return {'kind': 'invalid'}


def message_category(value):
    if value is None or value == '':
        return 'missing'
    if not isinstance(value, str) or len(value) > 4096:
        return 'invalid'
    text = value.lower()
    exact = {'target ownership could not be reconciled.': 'ownership',
             'target status could not be reconciled.': 'target_status',
             'target lookup backoff': 'lookup_backoff',
             'target unavailable; check its configured address.': 'target_unavailable',
             'eof': 'eof'}
    if text in exact:
        return exact[text]
    for marker, category in (('deadline exceeded', 'timeout'), ('i/o timeout', 'timeout'), ('timeout', 'timeout'),
                             ('context canceled', 'cancelled'), ('connection refused', 'connection_refused'),
                             ('connection reset', 'connection_reset'), ('no route to host', 'no_route'),
                             ('no such host', 'dns'), ('unexpected eof', 'unexpected_eof'),
                             ('unauthorized', 'unauthorized'), ('forbidden', 'forbidden'),
                             ('version mismatch', 'version_mismatch')):
        if marker in text:
            return category
    return 'other_redacted'


def exception_category(exc):
    """Classify wrapped transport exceptions without retaining their text."""
    seen = set()
    pending = [exc]
    while pending and len(seen) < 8:
        item = pending.pop(0)
        if not isinstance(item, BaseException) or id(item) in seen:
            continue
        seen.add(id(item))
        if isinstance(item, HTTPError):
            if item.code == 401: return 'unauthorized'
            if item.code == 403: return 'forbidden'
            if type(item.code) is int and 400 <= item.code < 500: return 'http_4xx'
            if type(item.code) is int and 500 <= item.code < 600: return 'http_5xx'
        if isinstance(item, (TimeoutError, socket.timeout)):
            return 'timeout'
        if isinstance(item, socket.gaierror):
            return 'dns'
        if isinstance(item, OSError):
            category = {errno.ECONNREFUSED: 'connection_refused', errno.ECONNRESET: 'connection_reset',
                        errno.ENETUNREACH: 'no_route', errno.EHOSTUNREACH: 'no_route',
                        errno.ETIMEDOUT: 'timeout'}.get(item.errno)
            if category:
                return category
        if isinstance(item, URLError) and isinstance(item.reason, BaseException):
            pending.append(item.reason)
        pending.extend((item.__cause__, item.__context__))
    return 'other_redacted'


def project(value, target_id, boot_id, bundle_id, image_id):
    if not isinstance(value, dict):
        return {'response_type': 'invalid', 'anomaly': True}
    target = value.get('target')
    target = target if isinstance(target, dict) else {}
    conn = target.get('connection')
    conn = conn if isinstance(conn, dict) else {}
    software = target.get('software')
    software = software if isinstance(software, dict) else {}
    state = conn.get('state')
    states = {'ready', 'active', 'connecting', 'disconnected', 'version_mismatch', 'busy', 'error'}
    state = state if isinstance(state, str) and state in states else 'missing_or_other'
    api = target.get('api_version')
    api = 'v1' if api == 'v1' else 'missing' if api is None else 'other'
    row = {'response_type': 'object', 'host_ready': boolean(value, 'ready'),
           'target_reachable': boolean(target, 'reachable'), 'target_ready': boolean(target, 'ready'),
           'connection_state': state, 'endpoint': endpoint(conn.get('address')),
           'target_id': match(conn, 'target_id', target_id), 'boot_id': match(conn, 'boot_id', boot_id),
           'active_bundle': match(software, 'active_bundle', bundle_id),
           'selected_bundle': match(software, 'selected', bundle_id),
           'base_image': match(software, 'base_identity', image_id),
           'target_api': api, 'connection_error': message_category(conn.get('message'))}
    row['anomaly'] = (row['host_ready'] is not True or row['target_reachable'] is not True or
                      row['target_ready'] is not True or row['target_id'] not in ('match', 'not_checked') or
                      row['boot_id'] not in ('match', 'not_checked') or row['active_bundle'] not in ('match', 'not_checked') or
                      row['selected_bundle'] not in ('match', 'not_checked') or row['base_image'] not in ('match', 'not_checked') or
                      row['target_api'] != 'v1' or row['endpoint']['kind'] in ('missing', 'invalid') or
                      row['connection_state'] not in ('ready', 'active'))
    return row


class HealthEvidence:
    """Write sanitized observations to a private, fork-safe append-only file."""
    FILE = 'health-guard-observations.jsonl'
    LOCK = 'health-guard-observations.lock'
    TRACE_FILE = 'health-stage-timings.jsonl'
    MAX_RECORDS = 512
    MAX_BYTES = 1024 * 1024
    MAX_STAGE_RECORDS = 4096
    MAX_STAGE_BYTES = 4 * 1024 * 1024
    MAX_ROW = 2048

    def __init__(self, directory, target_id, boot_id, bundle_id, image_id):
        self.directory = Path(directory)
        self.expected = (target_id, boot_id, bundle_id, image_id)
        self.fd = None
        self.compromised = False
        self.lock = threading.RLock()
        self.tracing = False
        self.trace_fd = None
        self.trace_read_fd = None
        self.stage_file_fd = None

    def prepare(self):
        """Create the file in the parent before any forked observation."""
        with self.lock:
            if self.compromised:
                raise EvidenceError('health evidence is compromised')
            if self.fd is not None:
                return
            try:
                self._open()
            except BaseException:
                self.compromised = True
                raise

    def _open(self):
        directory = os.open(self.directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            info = os.fstat(directory)
            if info.st_uid != os.getuid() or info.st_mode & 0o077:
                raise EvidenceError('health evidence directory is not private')
            fd = os.open(self.FILE, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_APPEND | os.O_NOFOLLOW,
                         0o600, dir_fd=directory)
            try:
                info = os.fstat(fd)
                if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
                    raise EvidenceError('health evidence file is not private')
                lockfd = os.open(self.LOCK, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                 0o600, dir_fd=directory)
                os.close(lockfd)
                os.fsync(directory)
                tracefd = os.open(self.TRACE_FILE, os.O_RDWR | os.O_CREAT | os.O_EXCL | os.O_APPEND | os.O_NOFOLLOW,
                                  0o600, dir_fd=directory)
                try:
                    os.fsync(directory)
                except BaseException:
                    os.close(tracefd)
                    raise
                self.stage_file_fd = tracefd
                self.fd = fd
            except BaseException:
                os.close(fd)
                raise
        finally:
            os.close(directory)

    def _write(self, rows):
        data = [(json.dumps(row, sort_keys=True, separators=(',', ':')) + '\n').encode('ascii') for row in rows]
        if any(len(row) > self.MAX_ROW for row in data):
            raise EvidenceError('health evidence limit reached')
        if self.fd is None:
            raise EvidenceError('health evidence was not prepared before observation')
        lockfd = os.open(self.directory / self.LOCK, os.O_RDWR | os.O_NOFOLLOW)
        try:
            lock_info = os.fstat(lockfd)
            if not stat.S_ISREG(lock_info.st_mode) or lock_info.st_uid != os.getuid() or lock_info.st_mode & 0o077:
                raise EvidenceError('health evidence lock is not private')
            fcntl.flock(lockfd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            info = os.fstat(self.fd)
            path_info = os.stat(self.directory / self.FILE, follow_symlinks=False)
            if (info.st_dev, info.st_ino) != (path_info.st_dev, path_info.st_ino):
                raise EvidenceError('health evidence file changed')
            size = info.st_size
            if size > self.MAX_BYTES:
                raise EvidenceError('health evidence limit reached')
            previous = os.pread(self.fd, size, 0)
            if len(previous) != size or (previous and not previous.endswith(b'\n')):
                raise EvidenceError('health evidence file changed')
            try:
                if any(not isinstance(json.loads(line), dict) for line in previous.splitlines()):
                    raise ValueError('invalid row')
            except (ValueError, UnicodeError) as exc:
                raise EvidenceError('health evidence file changed') from exc
            if (len(previous.splitlines()) + len(data) > self.MAX_RECORDS or
                    size + sum(map(len, data)) > self.MAX_BYTES):
                raise EvidenceError('health evidence limit reached')
            for row in data:
                view = memoryview(row)
                while view:
                    count = os.write(self.fd, view)
                    if count <= 0:
                        raise EvidenceError('health evidence write failed')
                    view = view[count:]
            os.fsync(self.fd)
        finally:
            os.close(lockfd)

    def record(self, row):
        with self.lock:
            if self.compromised:
                raise EvidenceError('health evidence is compromised')
            try:
                self._write([row])
            except BaseException:
                self.compromised = True
                raise

    STAGES = frozenset(('guard', 'host_guard', 'font', 'source_provenance',
                        'private_config', 'claim', 'settings_get', 'health_get',
                        'session_get', 'target_session_get', 'library_entry_get',
                        'media_binding', 'other_get'))

    def _check_stage_file(self):
        info = os.fstat(self.stage_file_fd)
        path_info = os.stat(self.directory / self.TRACE_FILE, follow_symlinks=False)
        if ((info.st_dev, info.st_ino) != (path_info.st_dev, path_info.st_ino) or
                not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077 or
                info.st_size > self.MAX_STAGE_BYTES):
            raise EvidenceError('stage evidence file changed')
        previous = os.pread(self.stage_file_fd, info.st_size, 0)
        if len(previous) != info.st_size or (previous and not previous.endswith(b'\n')):
            raise EvidenceError('stage evidence file changed')
        if len(previous.splitlines()) > self.MAX_STAGE_RECORDS:
            raise EvidenceError('stage evidence limit reached')
        for line in previous.splitlines():
            if not isinstance(json.loads(line), dict):
                raise EvidenceError('stage evidence file changed')
        return info, previous

    def begin_trace(self):
        """Prepare a bounded nonblocking pipe before the guarded fork.

        The parent drains only after the existing guard child is reaped. A full
        pipe immediately fails the child closed, rather than changing its deadline.
        """
        self.prepare()
        if self.trace_fd is not None or self.trace_read_fd is not None:
            raise EvidenceError('stage trace already active')
        try:
            self._check_stage_file()
            self.trace_read_fd, self.trace_fd = os.pipe2(os.O_NONBLOCK | os.O_CLOEXEC)
        except BaseException:
            self.compromised = True
            raise

    def end_trace(self):
        """Verify EOF and persist the drained trace before allowing mutations."""
        if self.trace_fd is None or self.trace_read_fd is None:
            self.compromised = True
            raise EvidenceError('stage trace is not active')
        write_fd, read_fd = self.trace_fd, self.trace_read_fd
        self.trace_fd, self.trace_read_fd = None, None
        try:
            os.close(write_fd)
            chunks = []
            length = 0
            while True:
                try:
                    chunk = os.read(read_fd, 4096)
                except BlockingIOError as exc:
                    raise EvidenceError('stage writer still open after guard') from exc
                if not chunk: break
                length += len(chunk)
                if length > self.MAX_STAGE_BYTES:
                    raise EvidenceError('stage evidence limit reached')
                chunks.append(chunk)
            data = b''.join(chunks)
            if data and not data.endswith(b'\n'):
                raise EvidenceError('stage record incomplete')
            info, previous = self._check_stage_file()
            if (info.st_size + len(data) > self.MAX_STAGE_BYTES or
                    len(previous.splitlines()) + data.count(b'\n') > self.MAX_STAGE_RECORDS):
                raise EvidenceError('stage evidence limit reached')
            for line in data.splitlines():
                if len(line) > 511 or not isinstance(json.loads(line), dict):
                    raise EvidenceError('stage record invalid')
            view = memoryview(data)
            while view:
                written = os.write(self.stage_file_fd, view)
                if written <= 0:
                    raise EvidenceError('stage evidence write failed')
                view = view[written:]
            os.fsync(self.stage_file_fd)
            self._check_stage_file()
        except BaseException:
            self.compromised = True
            raise
        finally:
            os.close(read_fd)

    def _stage_event(self, row):
        if self.compromised or self.trace_fd is None:
            raise EvidenceError('stage collector is not active')
        data = (json.dumps(row, sort_keys=True, separators=(',', ':')) + '\n').encode('ascii')
        try:
            # Pipe writes at or below PIPE_BUF are atomic; O_NONBLOCK prevents
            # collector backpressure from stalling the identity observation.
            if len(data) > min(512, os.fpathconf(self.trace_fd, 'PC_PIPE_BUF')):
                raise EvidenceError('stage record too large')
            if os.write(self.trace_fd, data) != len(data):
                raise EvidenceError('stage record incomplete')
        except BaseException:
            self.compromised = True
            raise

    def run_stage(self, name, callback):
        """Emit a start before the call so a killed fork identifies its stage."""
        if name not in self.STAGES:
            raise EvidenceError('unknown observation stage')
        started = time.monotonic_ns()
        ident = '%d-%d' % (os.getpid(), started)
        base = {'record': 'stage', 'stage': name, 'id': ident}
        self._stage_event(dict(base, event='start', monotonic_ns=started))
        try:
            value = callback()
        except BaseException as exc:
            try:
                self._stage_event(dict(base, event='finish', monotonic_ns=time.monotonic_ns(), ok=False))
            except BaseException:
                exc.add_note('stage evidence could not be persisted; claim must be retained')
            raise
        self._stage_event(dict(base, event='finish', monotonic_ns=time.monotonic_ns(), ok=True))
        return value

    @staticmethod
    def get_stage(path):
        if path == '/api/v1/health': return 'health_get'
        if path == '/api/v1/library/settings': return 'settings_get'
        if path == '/api/v1/session': return 'session_get'
        if path.startswith('/api/v1/session?'): return 'target_session_get'
        if path.startswith('/api/v1/library/'): return 'library_entry_get'
        return 'other_get'

    def close(self):
        if self.trace_read_fd is not None:
            self.end_trace()
        if self.stage_file_fd is not None:
            fd, self.stage_file_fd = self.stage_file_fd, None
            try:
                os.close(fd)
            except BaseException:
                self.compromised = True
                raise
        with self.lock:
            if self.fd is not None:
                fd, self.fd = self.fd, None
                try:
                    os.close(fd)
                except BaseException:
                    self.compromised = True
                    raise

    def install(self, api):
        original = api.get
        def observed(path, **kwargs):
            if self.compromised:
                raise EvidenceError('health evidence is compromised')
            def request():
                return original(path, **kwargs)
            def traced_request():
                if self.tracing:
                    return self.run_stage(self.get_stage(path), request)
                return request()
            if path != '/api/v1/health':
                return traced_request()
            self.prepare()
            started = time.monotonic_ns()
            try:
                result = traced_request()
            except BaseException as exc:
                category = exception_category(exc)
                try:
                    self.record({'response_type': 'exception', 'anomaly': True, 'error_category': category,
                                 'started_monotonic_ns': started, 'finished_monotonic_ns': time.monotonic_ns()})
                except BaseException:
                    exc.add_note('health evidence could not be persisted; claim must be retained')
                raise
            try:
                row = project(result, *self.expected)
            except BaseException as exc:
                self.compromised = True
                exc.add_note('health evidence projection failed; claim must be retained')
                raise
            row.update(started_monotonic_ns=started, finished_monotonic_ns=time.monotonic_ns())
            self.record(row)
            return result
        api.get = observed


def traced_app_validation(app, observer, callback):
    """Instrument existing guard calls only in the forked observation process."""
    replacements = ((app, 'check_claim', 'claim'),
                    (app, 'source_provenance', 'source_provenance'),
                    (app, 'check_private_config', 'private_config'),
                    (app.visual, 'load_font', 'font'))
    originals = [(obj, name, getattr(obj, name)) for obj, name, _ in replacements]
    previous = observer.tracing
    try:
        for (obj, name, label), (_, _, original) in zip(replacements, originals):
            def wrapped(*args, _original=original, _label=label, **kwargs):
                return observer.run_stage(_label, lambda: _original(*args, **kwargs))
            setattr(obj, name, wrapped)
        observer.tracing = True
        return observer.run_stage('guard', callback)
    finally:
        observer.tracing = previous
        for obj, name, original in originals:
            setattr(obj, name, original)
