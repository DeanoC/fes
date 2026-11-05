"""One-source FFV1 recording and Digger observation for reversible gameplay.

No kit is claimed here. Speaker audio is not captured or synchronized.
Observations are provisional; only independent post-run review can establish
whether the game responded to the ordered inputs.
"""
import ctypes
import ctypes.util
import hashlib
import os
from pathlib import Path
import shutil
import signal
import subprocess
import threading
import time

if __package__:
    from . import pcxt_capture_fanout as capture
    from . import pcxt_digger_readiness as readiness
else:
    import pcxt_capture_fanout as capture
    import pcxt_digger_readiness as readiness


def build_capture_helper(evidence, source, expected_source_sha256, *, check_interrupted=lambda:None):
    """Compile a pinned source snapshot and retain compiler/source/binary hashes."""
    evidence, source = Path(evidence), Path(source)
    data = source.read_bytes()
    if hashlib.sha256(data).hexdigest() != expected_source_sha256:
        raise capture.CaptureError('capture helper source differs from diagnostic provenance')
    compiler_path = shutil.which('gcc')
    if compiler_path is None:
        raise capture.CaptureError('capture helper compiler is unavailable')
    compiler = Path(compiler_path).resolve(strict=True)
    with compiler.open('rb') as f:
        compiler_sha = hashlib.file_digest(f, 'sha256').hexdigest()
    snapshot, binary, log = evidence/'pcxt-v4l2-source.c', evidence/'pcxt-v4l2-capture', evidence/'pcxt-v4l2-build.log'
    with snapshot.open('xb') as f:
        f.write(data)
    args = [str(compiler), '-Wall', '-Wextra', '-Werror', '-std=c11', '-O2', str(snapshot), '-o', str(binary)]
    with log.open('xb') as output:
        proc = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=output, stderr=output, start_new_session=True)
        observed = None
        try:
            deadline = time.monotonic()+60
            while observed is None:
                check_interrupted()
                observed = os.waitid(os.P_PID, proc.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
                if observed is None:
                    if time.monotonic() >= deadline:
                        raise capture.CaptureError('capture helper build timed out')
                    time.sleep(.02)
        finally:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                result = proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                raise capture.CaptureError('capture helper build cleanup unverified') from None
        try:
            os.killpg(proc.pid, 0)
        except ProcessLookupError:
            pass
        else:
            raise capture.CaptureError('capture helper build group cleanup unverified')
    if result != 0 or not binary.is_file() or not binary.stat().st_size:
        raise capture.CaptureError('capture helper build failed')
    if source.read_bytes() != data:
        raise capture.CaptureError('capture helper source changed during build')
    with compiler.open('rb') as f:
        if hashlib.file_digest(f, 'sha256').hexdigest() != compiler_sha:
            raise capture.CaptureError('capture helper compiler changed during build')
    with binary.open('rb') as f:
        binary_sha = hashlib.file_digest(f, 'sha256').hexdigest()
    return binary, {'source_sha256':expected_source_sha256, 'compiler':str(compiler),
                    'compiler_sha256':compiler_sha, 'binary_sha256':binary_sha,
                    'flags':['-Wall','-Wextra','-Werror','-std=c11','-O2'],
                    'source_snapshot':snapshot.name, 'binary':binary.name, 'log':log.name,
                    'note':'built from the pinned source with the identified host compiler; system headers and libraries are host inputs'}


class RGBConverter:
    """Use FFmpeg's point scaler, matching the offline study's 640x360 crops."""
    def __init__(self):
        sws_name, av_name = ctypes.util.find_library('swscale'), ctypes.util.find_library('avutil')
        if not sws_name or not av_name:
            raise capture.CaptureError('FFmpeg scale libraries are unavailable')
        self.sws = ctypes.CDLL(sws_name); av = ctypes.CDLL(av_name)
        def library_identity(soname):
            paths = set()
            for line in Path('/proc/self/maps').read_text().splitlines():
                parts = line.split(None, 5)
                if len(parts) == 6 and Path(parts[5]).name.startswith(soname):
                    paths.add(Path(parts[5]).resolve())
            if len(paths) != 1:
                raise capture.CaptureError('cannot identify the loaded FFmpeg scaler library')
            path = paths.pop()
            with path.open('rb') as f:
                digest = hashlib.file_digest(f, 'sha256').hexdigest()
            return {'path':str(path), 'sha256':digest}
        self.sws.swscale_version.restype = ctypes.c_uint
        av.avutil_version.restype = ctypes.c_uint
        self.identity = {'swscale':{**library_identity(sws_name), 'version':self.sws.swscale_version()},
                         'avutil':{**library_identity(av_name), 'version':av.avutil_version()}}
        av.av_get_pix_fmt.argtypes = [ctypes.c_char_p]; av.av_get_pix_fmt.restype = ctypes.c_int
        self.ptr = ctypes.POINTER(ctypes.c_uint8); self.planes = self.ptr*4; self.strides = ctypes.c_int*4
        self.sws.sws_getContext.argtypes = [ctypes.c_int]*7+[ctypes.c_void_p]*3
        self.sws.sws_getContext.restype = ctypes.c_void_p
        self.sws.sws_scale.argtypes = [ctypes.c_void_p, self.planes, self.strides, ctypes.c_int,
                                     ctypes.c_int, self.planes, self.strides]
        self.sws.sws_scale.restype = ctypes.c_int
        self.sws.sws_freeContext.argtypes = [ctypes.c_void_p]
        self.ctx = self.sws.sws_getContext(1920,1080,av.av_get_pix_fmt(b'yuyv422'),
                                            640,360,av.av_get_pix_fmt(b'rgb24'),0x10,None,None,None)
        if not self.ctx:
            raise capture.CaptureError('cannot create FFmpeg point scaler')

    def convert(self, data):
        if not isinstance(data, bytes) or len(data) != 1920*1080*2:
            raise capture.CaptureError('invalid game source frame')
        src = (ctypes.c_uint8*len(data)).from_buffer_copy(data)
        dst = (ctypes.c_uint8*(640*360*3))()
        rows = self.sws.sws_scale(self.ctx, self.planes(ctypes.cast(src,self.ptr),None,None,None),
            self.strides(3840,0,0,0), 0, 1080,
            self.planes(ctypes.cast(dst,self.ptr),None,None,None), self.strides(1920,0,0,0))
        if rows != 360:
            raise capture.CaptureError('FFmpeg point scaler returned incomplete output')
        return bytes(dst)

    def close(self):
        if self.ctx:
            self.sws.sws_freeContext(self.ctx); self.ctx = None


def crop(rgb, x, y, width, height):
    if len(rgb) != 640*360*3:
        raise capture.CaptureError('invalid scaled game frame')
    return b''.join(rgb[(row*640+x)*3:(row*640+x+width)*3] for row in range(y,y+height))


def inspect_frame(converter, pixels, templates):
    rgb = converter.convert(pixels)
    return readiness.classify(crop(rgb,304,322,32,34), crop(rgb,350,280,80,76),
                              crop(rgb,190,0,70,30), templates)


class GameCapture:
    """Record one native source while an independent observer drains it."""
    def __init__(self, device, evidence, probe, helper, helper_sha256, templates, *, startup_timeout=30):
        evidence, helper = Path(evidence), Path(helper)
        if (helper.is_symlink() or not helper.is_file() or not 0 < helper.stat().st_size <= 32*1024*1024
                or hashlib.sha256(helper.read_bytes()).hexdigest() != helper_sha256):
            raise capture.CaptureError('capture helper identity differs')
        if not templates or any(not isinstance(t, bytes) or len(t) != readiness.PLAYER_SIZE for t in templates):
            raise capture.CaptureError('private game templates are invalid')
        self.observer_guard = probe
        self.started_ns = time.monotonic_ns()
        self.started = threading.Event(); self.stop_requested = threading.Event()
        self.failed_run = threading.Event()
        self.done = threading.Event(); self.error = None; self.result = None
        self.converter = RGBConverter()
        self.converter_identity = self.converter.identity
        try:
            self.source = capture.CaptureProcess([str(helper),str(device)], evidence/'game-source.stderr')
        except BaseException:
            self.converter.close()
            raise
        try:
            self.recorder = capture.FFV1Recorder(evidence)
        except BaseException as error:
            try:
                self.source.stop(require_complete=False)
            except BaseException as cleanup:
                error.source_cleanup_incomplete = True
                error.add_note('capture source cleanup failed: '+repr(cleanup))
            self.converter.close()
            raise
        self.path = self.recorder.video
        self.progress_path = self.recorder.sidecar_path
        self.proc = self  # poll() reports our coordinator, without reaping the source leader.
        self.first_progress_ns = None
        def detect(frame):
            result = inspect_frame(self.converter, frame.pixels, templates)
            probe.processed(frame, result)
            if not self.started.is_set():
                self.first_progress_ns = time.monotonic_ns()
                self.started.set()
        def run():
            try:
                self.result = capture.capture_experiment(self.source, self.recorder, 7200, detect,
                    sample_every=1, detector_fault=probe.fault, on_source_frame=probe.source_frame,
                    stop_event=self.stop_requested, defer_recording_verification=True)
            except BaseException as exc:
                self.error = exc; probe.fault.set()
            finally:
                self.done.set()
        self.worker = threading.Thread(target=run, daemon=True, name='pcxt-game-capture')
        try:
            self.worker.start()
        except BaseException as error:
            for cleanup in (lambda: self.source.stop(require_complete=False), self.recorder.abort):
                try:
                    cleanup()
                except BaseException as failure:
                    error.source_cleanup_incomplete = True
                    error.add_note('capture startup cleanup failed: '+repr(failure))
            self.converter.close()
            raise
        end = time.monotonic()+startup_timeout
        while not self.started.is_set() and not self.done.is_set() and time.monotonic() < end:
            self.started.wait(min(.1, max(0, end-time.monotonic())))
        if not self.started.is_set() or self.done.is_set():
            self.stop_requested.set(); self.worker.join(timeout=10)
            error = capture.CaptureError('continuous game capture did not start')
            if (self.worker.is_alive() or getattr(self.error, 'source_cleanup_incomplete', False)
                    or getattr(self.error, 'recorder_worker_running', False)):
                error.source_cleanup_incomplete = True
                error.add_note('capture startup cleanup is unverified; retain the kit claim')
            raise error

    def poll(self):
        return 1 if self.done.is_set() else None

    def skip_failed_recording_verification(self):
        """A failed run retains its recording without the slow full decode."""
        self.failed_run.set()

    def stop(self):
        self.stop_requested.set(); self.worker.join(timeout=60)
        if self.worker.is_alive():
            error = capture.CaptureError('game capture cleanup unverified')
            error.source_cleanup_incomplete = True
            raise error
        if self.error is not None:
            if getattr(self.error, 'recorder_worker_running', False):
                self.error.source_cleanup_incomplete = True
            raise self.error
        if self.result is None or not self.result.detector_valid:
            if self.result is not None and self.result.detector_stopped:
                self.converter.close()
            raise capture.CaptureError('game observer dropped frames or stopped uncleanly')
        try:
            for library in self.converter_identity.values():
                with Path(library['path']).open('rb') as stream:
                    if hashlib.file_digest(stream, 'sha256').hexdigest() != library['sha256']:
                        raise capture.CaptureError('game frame converter library changed during capture')
        finally:
            self.converter.close()
        # Recording verification can take minutes on large lossless captures.
        # It runs only after the source, recorder and worker have exited, so
        # its independent bounded phase cannot be mistaken for cleanup failure.
        if not self.failed_run.is_set():
            self.recorder.verify_capture()
