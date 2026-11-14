"""Bounded USB syscall evidence from an already-owned private JTAG server.

The caller still owns its server, clock, scan, program gate and cleanup. This
attaches only to that server and never opens a USB node. Attachment pauses
server threads; cleanup signals only the tracer. This changes host timing and
is diagnostic evidence, not qualification.
"""
import hashlib
import json
import os
from pathlib import Path
import re
import resource
import signal
import subprocess
import time

SERVER = Path('/data/altera_pro/26.3/quartus/linux64/jtagd')
LAUNCHER = str(SERVER.parent.parent/'bin/jtagd')
MAX_BYTES = 8 << 20
MAX_SECONDS = 120


def _sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def _snapshot(pid):
    base = Path('/proc')/str(pid)
    before = (base/'stat').read_text().rsplit(')', 1)[1].split()
    status = (base/'status').read_text()
    result = dict(pid=pid, start_ticks=before[19], state=before[0],
                  uid=base.stat().st_uid, executable=str((base/'exe').readlink()),
                  argv=(base/'cmdline').read_bytes().rstrip(b'\0').decode().split('\0'),
                  tracer_pid=int(re.search(r'^TracerPid:\s+(\d+)$', status, re.M)[1]))
    after = (base/'stat').read_text().rsplit(')', 1)[1].split()
    if before[19] != after[19] or after[0] == 'Z':
        raise ValueError('server process ended or changed during observation')
    return result


class JtagServerTrace:
    def __init__(self, directory, *, server_record, server_sha256, owns):
        if not callable(owns) or not re.fullmatch('[0-9a-f]{64}', server_sha256):
            raise ValueError('owned server and pinned executable required')
        self.directory, self.record_path = Path(directory), Path(server_record)
        self.server_sha256, self.owns = server_sha256, owns
        self.process = self.log = None
        self.started = self.closed = self.attached = False
        self.failed_owner = False
        self.ended_early = False
        self.original = self.record_bytes = None
        self.started_at = None

    def _owned(self):
        try:
            owned = self.owns() is True
        except BaseException:
            self.failed_owner = True
            raise
        if not owned or self.failed_owner:
            self.failed_owner = True
            raise ValueError('private JTAG trace ownership lost')

    def _server(self, *, tracer):
        if self.record_path.is_symlink():
            raise ValueError('regular private server record required')
        raw = self.record_path.read_bytes()
        if self.record_bytes is not None and raw != self.record_bytes:
            raise ValueError('private server record changed')
        record = json.loads(raw)
        pid, ticks = record.get('owned_pid'), record.get('process_start_ticks')
        if (record.get('serial') != 'TRWU8CBL' or record.get('status') != 'adapter-ready' or
                type(pid) is not int or pid <= 0 or not isinstance(ticks, str) or
                not ticks.isdecimal()):
            raise ValueError('live top-only private server record required')
        args = record.get('server_args')
        expected = [LAUNCHER, '--foreground', '--no-config', '--auto-detect-filter',
                    'TRWU8CBL', '--port', '0', '--port-file', str(self.record_path.parent/'private-port'),
                    '--pid-file', str(self.record_path.parent/'private-pid')]
        if args != expected:
            raise ValueError('private server arguments differ')
        current = _snapshot(pid)
        if (current['start_ticks'] != ticks or current['uid'] != 1000 or
                current['executable'] != str(SERVER) or not current['argv'] or
                current['argv'][0] not in (str(SERVER), SERVER.name) or current['argv'][1:] != args[1:] or
                current['tracer_pid'] != tracer or current['state'] == 'Z' or
                _sha(SERVER) != self.server_sha256):
            raise ValueError('private server identity or tracer differs')
        identity = dict(pid=pid, start_ticks=ticks, executable_sha256=self.server_sha256,
                        server_record_sha256=hashlib.sha256(raw).hexdigest())
        if self.original is not None and identity != self.original:
            raise ValueError('private server epoch changed')
        self.record_bytes = raw
        return identity

    @staticmethod
    def _limit():
        os.umask(0o077)
        resource.setrlimit(resource.RLIMIT_FSIZE, (MAX_BYTES, MAX_BYTES))

    def start(self):
        if self.started or self.closed:
            raise ValueError('private server trace already consumed')
        self.started = True
        self._owned()
        if os.geteuid() != 0:
            raise ValueError('root private-server tracing wrapper required')
        self.original = self._server(tracer=0)
        self.directory.mkdir(mode=0o700)
        # O_EXCL output creation plus a fresh directory prevents transcript reuse.
        trace = self.directory/'usb-ioctl.strace'
        with trace.open('xb') as stream:
            os.fchmod(stream.fileno(), 0o600)
        self.log = (self.directory/'tracer.log').open('xb')
        args = ['/usr/bin/strace', '-I', '1', '-f', '-ttt', '-T', '-s', '32', '-yy',
                '-e', 'trace=ioctl', '-o', str(trace), '-p', str(self.original['pid'])]
        self.started_at = time.monotonic()
        try:
            self.process = subprocess.Popen(args, stdout=self.log, stderr=subprocess.STDOUT,
                                            preexec_fn=self._limit)
            own = _snapshot(self.process.pid)
            record = dict(pid=self.process.pid, start_ticks=own['start_ticks'], argv=args,
                          target=self.original, signal_target=False)
            with (self.directory/'owned-worker.json').open('x') as stream:
                json.dump(record, stream, sort_keys=True);stream.write('\n')
                stream.flush();os.fsync(stream.fileno())
            until = time.monotonic()+5
            while True:
                self._owned()
                if self.process.poll() is not None:
                    raise ValueError('private server tracer exited before attachment')
                if _snapshot(self.original['pid'])['tracer_pid'] == self.process.pid:
                    self._server(tracer=self.process.pid)
                    break
                if time.monotonic() >= until:
                    raise TimeoutError('private server tracer attachment exceeded five seconds')
                time.sleep(.01)
            self.attached = True
            return self.check()
        except BaseException:
            self.close()
            raise

    def check(self):
        if not self.attached or self.closed or self.process.poll() is not None:
            raise ValueError('private server trace is not attached')
        self._owned()
        if time.monotonic()-self.started_at >= MAX_SECONDS:
            raise TimeoutError('private server trace exceeded 120 seconds')
        if (self.directory/'usb-ioctl.strace').stat().st_size >= MAX_BYTES:
            raise ValueError('private server trace exceeded eight MiB')
        return self._server(tracer=self.process.pid)

    def close(self):
        # Always detach our tracer, including after lost board ownership. Never
        # send a signal to the target PID or use --kill-on-exit with attachment.
        if self.closed:
            return
        self.closed = True
        self.ended_early = self.process is not None and self.process.poll() is not None
        try:
            if self.process is not None and self.process.poll() is None:
                self.process.send_signal(signal.SIGINT)
                try:
                    self.process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    self.process.kill();self.process.wait(timeout=5)
        finally:
            if self.log is not None:
                self.log.close()
        if self.original is not None:
            self._server(tracer=0)

    def receipt(self):
        if not self.closed or self.process is None or self.process.poll() is None:
            raise ValueError('closed owned tracer required')
        self._server(tracer=0)
        trace = self.directory/'usb-ioctl.strace'
        try:
            self._owned()
            owned = True
        except BaseException:
            owned = False
        return dict(scope='nano-private-jtag-server-trace-v1', target=self.original,
                    attached=self.attached, tracer_stopped=True, target_detached=True,
                    ownership_verified=owned,
                    tracer_exited_before_close=self.ended_early,
                    tracer_returncode=self.process.returncode,
                    trace_sha256=_sha(trace), trace_bytes=trace.stat().st_size,
                    trace_limit_bytes=MAX_BYTES, trace_limit_seconds=MAX_SECONDS,
                    hardware_acceptance=False, programming_authority=False,
                    reset_completed=False, recovery_authority=False)
