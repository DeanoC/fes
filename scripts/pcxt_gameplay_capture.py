"""One-source FFV1 recording and Digger observation for reversible gameplay.

No kit is claimed here. Speaker audio is not captured or synchronized.
Observations are provisional; only independent post-run review can establish
whether the game responded to the ordered inputs.
"""
import ctypes
import ctypes.util
import hashlib
from pathlib import Path
import threading
import time

if __package__:
    from . import pcxt_capture_fanout as capture
    from . import pcxt_digger_readiness as readiness
else:
    import pcxt_capture_fanout as capture
    import pcxt_digger_readiness as readiness


class RGBConverter:
    """Use FFmpeg's point scaler, matching the offline study's 640x360 crops."""
    def __init__(self):
        sws_name, av_name = ctypes.util.find_library('swscale'), ctypes.util.find_library('avutil')
        if not sws_name or not av_name:
            raise capture.CaptureError('FFmpeg scale libraries are unavailable')
        self.sws = ctypes.CDLL(sws_name); av = ctypes.CDLL(av_name)
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
        self.done = threading.Event(); self.error = None; self.result = None
        self.converter = RGBConverter()
        try:
            self.source = capture.CaptureProcess([str(helper),str(device)], evidence/'game-source.stderr')
        except BaseException:
            self.converter.close()
            raise
        try:
            self.recorder = capture.FFV1Recorder(evidence)
        except BaseException:
            self.source.stop(require_complete=False); self.converter.close(); raise
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
                    stop_event=self.stop_requested)
            except BaseException as exc:
                self.error = exc; probe.fault.set()
            finally:
                self.done.set()
        self.worker = threading.Thread(target=run, daemon=True, name='pcxt-game-capture')
        self.worker.start()
        end = time.monotonic()+startup_timeout
        while not self.started.is_set() and not self.done.is_set() and time.monotonic() < end:
            self.started.wait(min(.1, max(0, end-time.monotonic())))
        if not self.started.is_set() or self.done.is_set():
            self.stop_requested.set(); self.worker.join(timeout=10)
            raise capture.CaptureError('continuous game capture did not start')

    def poll(self):
        return 1 if self.done.is_set() else None

    def stop(self):
        self.stop_requested.set(); self.worker.join(timeout=60)
        if self.worker.is_alive():
            error = capture.CaptureError('game capture cleanup unverified')
            error.source_cleanup_incomplete = True
            raise error
        if self.error is not None:
            raise self.error
        if self.result is None or not self.result.detector_valid:
            if self.result is not None and self.result.detector_stopped:
                self.converter.close()
            raise capture.CaptureError('game observer dropped frames or stopped uncleanly')
        self.converter.close()
