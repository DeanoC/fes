"""Experimental single-source capture fanout; never sends input to a kit.

V4L2 timestamps are device/driver timestamps and do not bound buffering
upstream of the capture device. This module makes no hardware timing claim.
"""
from dataclasses import dataclass
import hashlib
import json
import os
from pathlib import Path
import queue
import select
import signal
import struct
import subprocess
import threading
import time

MAGIC = b'PXT1'
HEADER = struct.Struct('<4sIQQII')
V4L2_TIMESTAMP_MASK = 0xe000
V4L2_TIMESTAMP_MONOTONIC = 0x2000


class CaptureError(RuntimeError):
    pass


class CaptureProcess:
    """Own a native acquisition process and distinguish planned stop from EOF."""
    def __init__(self, command, stderr_path, frame_size=1920*1080*2):
        if not command or not all(isinstance(part, str) and part for part in command):
            raise ValueError('invalid capture command')
        self.stderr = os.fdopen(os.open(stderr_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w')
        try:
            self.proc = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                         stderr=self.stderr, bufsize=0, start_new_session=True)
        except BaseException:
            self.stderr.close()
            raise
        self.frame_size = frame_size
        self.completed = False
        self.closed = False

    def take(self, count, stop_event=None):
        if type(count) is not int or count <= 0:
            raise ValueError('invalid capture frame count')
        source = iter(v4l2_frames(self.proc.stdout, self.frame_size))
        for index in range(count):
            if stop_event is not None and stop_event.is_set() and index:
                self.completed = True
                return
            try:
                yield next(source)
            except StopIteration:
                raise CaptureError('capture child ended before planned stop') from None
        self.completed = True

    def stop(self, require_complete=True, timeout=5):
        if self.closed:
            raise CaptureError('capture child already closed')
        try:
            # WNOWAIT preserves the leader's PID until after every group
            # signal, preventing us from signalling a reused process group.
            observed = os.waitid(os.P_PID, self.proc.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            running = observed is None
            try:
                os.killpg(self.proc.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            end = time.monotonic() + timeout
            while observed is None and time.monotonic() < end:
                time.sleep(min(.01, max(0, end-time.monotonic())))
                observed = os.waitid(os.P_PID, self.proc.pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
            timed_out = observed is None
            # A descendant may have inherited the pipe even after the leader
            # exits. The leader is still unreaped while we kill the whole group.
            try:
                os.killpg(self.proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                result = self.proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                raise CaptureError('capture child cleanup unverified') from None
            try:
                os.killpg(self.proc.pid, 0)
            except ProcessLookupError:
                pass
            else:
                raise CaptureError('capture process group cleanup unverified')
            if timed_out:
                raise CaptureError('capture child did not stop')
            if require_complete and (not self.completed or not running or result != -signal.SIGTERM):
                raise CaptureError('capture child did not reach its planned stop')
            return result
        except BaseException as error:
            error.source_cleanup_incomplete = True
            raise
        finally:
            self.proc.stdout.close()
            self.stderr.close()
            self.closed = True


class FFV1Recorder:
    """Record contiguous raw YUYV frames and a per-frame timestamp sidecar.

    The frame rate is a playback convention, not proof of actual capture or
    target timing. The owner must call abort after any failed fanout.
    """
    def __init__(self, directory, width=1920, height=1080, fps=30):
        self.directory = Path(directory)
        self.video = self.directory/'capture.mkv'
        self.sidecar_path = self.directory/'capture-frames.jsonl'
        self.stderr_path = self.directory/'capture-ffmpeg.stderr'
        if any(type(v) is not int or v <= 0 for v in (width, height, fps)) or width % 2:
            raise ValueError('invalid recorder format')
        if not self.directory.is_dir() or self.directory.stat().st_mode & 0o077:
            raise CaptureError('capture directory must be private')
        self.size = width * height * 2
        if self.video.exists():
            raise CaptureError('capture output already exists')
        self.width, self.height = width, height
        self.sidecar = os.fdopen(os.open(self.sidecar_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w')
        try:
            self.stderr = os.fdopen(os.open(self.stderr_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w')
        except BaseException:
            self.sidecar.close()
            raise
        try:
            self.proc = subprocess.Popen(['ffmpeg', '-hide_banner', '-loglevel', 'error', '-nostdin',
                '-f', 'rawvideo', '-pixel_format', 'yuyv422', '-video_size', f'{width}x{height}',
                '-framerate', str(fps), '-i', 'pipe:0', '-an', '-c:v', 'ffv1', '-level', '3',
                '-fps_mode', 'passthrough', '-n', str(self.video)], stdin=subprocess.PIPE,
                stdout=subprocess.DEVNULL, stderr=self.stderr, bufsize=0)
        except BaseException:
            self.stderr.close(); self.sidecar.close()
            raise
        self.count = 0
        self.entries = []
        self.closed = False

    def write(self, frame):
        if not isinstance(frame, CapturedFrame) or len(frame.pixels) != self.size:
            raise CaptureError('invalid recorder frame')
        if (frame.flags & V4L2_TIMESTAMP_MASK != V4L2_TIMESTAMP_MONOTONIC
                or not 0 < frame.capture_ns <= frame.dequeue_ns <= frame.arrival_ns):
            raise CaptureError('invalid frame timestamp')
        if self.entries and (frame.sequence != self.entries[-1]['sequence']+1
                             or frame.capture_ns <= self.entries[-1]['capture_ns']
                             or frame.dequeue_ns <= self.entries[-1]['dequeue_ns']):
            raise CaptureError('recorder frame sequence or time discontinuity')
        data = memoryview(frame.pixels)
        while data:
            size = self.proc.stdin.write(data)
            if not size:
                raise CaptureError('recorder input closed')
            data = data[size:]
        entry = {'sequence': frame.sequence, 'capture_ns': frame.capture_ns,
                 'dequeue_ns': frame.dequeue_ns, 'arrival_ns': frame.arrival_ns, 'flags': frame.flags,
                 'raw_sha256': hashlib.sha256(frame.pixels).hexdigest()}
        self.sidecar.write(json.dumps(entry)+'\n')
        self.sidecar.flush()
        self.entries.append(entry)
        self.count += 1

    def finish(self, timeout=30):
        try:
            self.proc.stdin.close()
            if self.proc.wait(timeout=timeout) != 0:
                raise CaptureError('FFV1 recorder failed')
            if not self.count or not self.video.is_file() or not self.video.stat().st_size:
                raise CaptureError('FFV1 recording is empty')
            try:
                rows = [json.loads(line) for line in self.sidecar_path.read_text().splitlines()]
            except (OSError, ValueError) as exc:
                raise CaptureError('recording sidecar cannot be read') from exc
            if len(rows) != self.count or rows != self.entries:
                raise CaptureError('recording sidecar was truncated or changed')
            probe = subprocess.run(['ffprobe', '-v', 'error', '-count_frames', '-select_streams', 'v:0',
                '-show_entries', 'stream=codec_name,width,height,nb_read_frames', '-of', 'json', str(self.video)],
                check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
            streams = json.loads(probe.stdout).get('streams', [])
            if (len(streams) != 1 or streams[0].get('codec_name') != 'ffv1'
                    or streams[0].get('width') != self.width or streams[0].get('height') != self.height
                    or streams[0].get('nb_read_frames') != str(self.count)):
                raise CaptureError('FFV1 decoded frame count or format differs')
            self._verify_pixels()
        finally:
            if self.proc.poll() is None:
                self.proc.kill(); self.proc.wait(timeout=5)
            self.sidecar.close(); self.stderr.close()
            self.closed = True

    def finish_unverified(self, timeout=30):
        """Close a failed diagnostic's recording without a full decode pass.

        The partial capture is retained for diagnosis and must not be treated
        as verified gameplay evidence. Process exit is still required.
        """
        try:
            self.proc.stdin.close()
            if self.proc.wait(timeout=timeout) != 0:
                raise CaptureError('FFV1 recorder failed during failed-run cleanup')
        finally:
            if self.proc.poll() is None:
                self.proc.kill(); self.proc.wait(timeout=5)
            self.sidecar.close(); self.stderr.close()
            self.closed = True

    def _verify_pixels(self):
        """Bind each sidecar row to the corresponding losslessly decoded frame."""
        decoder = subprocess.Popen(['ffmpeg', '-nostdin', '-hide_banner', '-loglevel', 'error',
            '-xerror', '-i', str(self.video), '-map', '0:v:0', '-pix_fmt', 'yuyv422',
            '-f', 'rawvideo', 'pipe:1'], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, bufsize=0)
        try:
            for row in self.entries:
                digest = hashlib.sha256()
                remaining = self.size
                while remaining:
                    if not select.select([decoder.stdout], [], [], 10)[0]:
                        raise CaptureError('FFV1 verification stalled')
                    chunk = os.read(decoder.stdout.fileno(), min(remaining, 1024*1024))
                    if not chunk:
                        raise CaptureError('FFV1 verification ended early')
                    digest.update(chunk)
                    remaining -= len(chunk)
                if digest.hexdigest() != row['raw_sha256']:
                    raise CaptureError('FFV1 decoded pixels differ from captured frame')
            if not select.select([decoder.stdout], [], [], 10)[0]:
                raise CaptureError('FFV1 verification did not finish')
            if os.read(decoder.stdout.fileno(), 1):
                raise CaptureError('FFV1 contains unexpected decoded frames')
            if decoder.wait(timeout=10) != 0:
                raise CaptureError('FFV1 verification failed')
        finally:
            if decoder.poll() is None:
                decoder.kill(); decoder.wait(timeout=5)
            decoder.stdout.close()

    def abort(self):
        self.interrupt()
        self.proc.stdin.close()
        self.sidecar.close(); self.stderr.close()
        self.closed = True

    def interrupt(self):
        """Unblock a writer by killing FFmpeg; close files only after it joins."""
        if self.proc.poll() is None:
            self.proc.kill()
        self.proc.wait(timeout=5)


@dataclass(frozen=True)
class CapturedFrame:
    sequence: int
    capture_ns: int
    dequeue_ns: int
    arrival_ns: int
    flags: int
    pixels: bytes

    def __post_init__(self):
        if not isinstance(self.pixels, bytes):
            raise ValueError('captured pixels must be immutable bytes')


def read_exact(stream, size):
    chunks = []
    while size:
        data = stream.read(size)
        if not data:
            raise CaptureError('truncated capture stream')
        chunks.append(data)
        size -= len(data)
    return b''.join(chunks)


def v4l2_frames(stream, frame_size=1920*1080*2, clock=time.monotonic_ns):
    """Decode the native acquisition program's stream, retaining its metadata.

    The caller owns and must stop the subprocess when the experiment ends.
    Timestamp validity here means only that the kernel reported monotonic time.
    """
    previous = None
    while True:
        first = stream.read(1)
        if not first:
            return
        magic, sequence, captured, dequeued, flags, size = HEADER.unpack(first + read_exact(stream, HEADER.size-1))
        if magic != MAGIC or size != frame_size:
            raise CaptureError('invalid capture header')
        pixels = read_exact(stream, size)
        arrived = clock()
        if (flags & V4L2_TIMESTAMP_MASK != V4L2_TIMESTAMP_MONOTONIC
                or not 0 < captured <= dequeued <= arrived):
            raise CaptureError('capture timestamp is not monotonic or ordered')
        if previous is not None and (sequence != previous.sequence + 1 or captured <= previous.capture_ns
                                     or dequeued <= previous.dequeue_ns):
            raise CaptureError('capture sequence or time discontinuity')
        previous = CapturedFrame(sequence, captured, dequeued, arrived, flags, pixels)
        yield previous


@dataclass(frozen=True)
class FanoutResult:
    frames_recorded: int
    frames_sampled: int
    detector_drops: int
    detector_error: str | None
    detector_stopped: bool

    @property
    def detector_valid(self):
        return (self.frames_sampled > 0 and self.detector_drops == 0
                and self.detector_error is None and self.detector_stopped)


def fanout_frames(frames, record, detect, *, sample_every, record_capacity=8, join_timeout=2,
                  source_timeout=5, allow_source_eof=False, on_recorder_stall=None, on_source_stop=None,
                  detector_fault=None, on_source_frame=None):
    """Record every source frame and send selected frames to a latest-only observer.

    Detector overload or failure never blocks recording. The returned detector
    result is unusable if frames were dropped, it raised, or it didn't finish.
    Recorder overload/failure terminates the experiment with CaptureError.
    `record` and `detect` must not be used for target input. Callers own cleanup
    of source and recording processes; threads are daemonized so a stalled
    callback cannot prevent bounded return. A real recorder must supply an
    on_recorder_stall callback that interrupts its process without closing
    shared files; only the owner may close them after the writer has joined.
    This is experimental plumbing, not a completed live game diagnostic.
    """
    if (type(sample_every) is not int or sample_every <= 0 or type(record_capacity) is not int
            or record_capacity <= 0 or not isinstance(join_timeout, (int, float)) or join_timeout <= 0
            or not isinstance(source_timeout, (int, float)) or source_timeout <= 0
            or type(allow_source_eof) is not bool):
        raise ValueError('invalid fanout policy')
    records = queue.Queue(record_capacity)
    observations = queue.Queue(1)
    sentinel = object()
    record_error, detector_error, source_error = [], [], []
    recorded = [0]
    source_queue = queue.Queue(1)
    stopping = threading.Event()

    def source_reader():
        try:
            for frame in frames:
                if on_source_frame is not None:
                    on_source_frame(frame)
                while not stopping.is_set():
                    try:
                        source_queue.put(frame, timeout=.05)
                        break
                    except queue.Full:
                        continue
                if stopping.is_set():
                    return
        except BaseException as exc:
            source_error.append(repr(exc))
            if detector_fault is not None:
                detector_fault.set()
        finally:
            while not stopping.is_set():
                try:
                    source_queue.put(sentinel, timeout=.05)
                    break
                except queue.Full:
                    continue

    def writer():
        try:
            while True:
                try:
                    frame = records.get(timeout=.05)
                except queue.Empty:
                    if stopping.is_set(): return
                    continue
                if frame is sentinel:
                    return
                record(frame)
                recorded[0] += 1
        except BaseException as exc:
            record_error.append(repr(exc))
            if detector_fault is not None:
                detector_fault.set()

    def observer():
        try:
            while True:
                try:
                    frame = observations.get(timeout=.05)
                except queue.Empty:
                    if stopping.is_set(): return
                    continue
                if frame is sentinel:
                    return
                detect(frame)
        except BaseException as exc:
            detector_error.append(repr(exc))
            if detector_fault is not None:
                detector_fault.set()

    wt = threading.Thread(target=writer, daemon=True, name='pcxt-recorder')
    dt = threading.Thread(target=observer, daemon=True, name='pcxt-detector')
    st = threading.Thread(target=source_reader, daemon=True, name='pcxt-source')
    wt.start(); dt.start(); st.start()
    sampled = drops = 0
    failure = None
    try:
        index = 0
        deadline = time.monotonic() + source_timeout
        while True:
            if record_error or not wt.is_alive():
                raise CaptureError('recorder failed')
            try:
                frame = source_queue.get(timeout=min(.05, max(0, deadline-time.monotonic())))
            except queue.Empty:
                if time.monotonic() >= deadline:
                    raise CaptureError('capture source stalled') from None
                continue
            if frame is sentinel:
                if source_error:
                    raise CaptureError('capture source failed: '+source_error[0])
                if not allow_source_eof or index == 0:
                    raise CaptureError('unexpected or empty capture EOF')
                break
            if not isinstance(frame, CapturedFrame) or not isinstance(frame.pixels, bytes):
                raise CaptureError('invalid source frame')
            deadline = time.monotonic() + source_timeout
            try:
                records.put_nowait(frame)
            except queue.Full:
                raise CaptureError('recorder fell behind capture') from None
            if index % sample_every == 0:
                sampled += 1
                try:
                    observations.put_nowait(frame)
                except queue.Full:
                    if detector_fault is not None:
                        detector_fault.set()
                    try:
                        observations.get_nowait()
                    except queue.Empty:
                        pass
                    drops += 1
                    try:
                        observations.put_nowait(frame)
                    except queue.Full:
                        drops += 1
            index += 1
    except BaseException as exc:
        failure = exc
        if detector_fault is not None:
            detector_fault.set()
    finally:
        stopping.set()
        if failure is not None and on_source_stop is not None:
            try:
                on_source_stop()
            except BaseException as exc:
                failure.add_note('source interruption failed: '+repr(exc))
                failure.source_cleanup_incomplete = True
        st.join(timeout=join_timeout)
        if st.is_alive() and failure is not None:
            failure.add_note('source worker is still running; source cleanup is incomplete')
            failure.source_cleanup_incomplete = True
        try:
            records.put(sentinel, timeout=join_timeout)
        except queue.Full:
            if detector_fault is not None:
                detector_fault.set()
            failure = failure or CaptureError('recorder did not drain')
        wt.join(timeout=join_timeout)
        if wt.is_alive():
            failure = failure or CaptureError('recorder did not stop')
            if on_recorder_stall is not None:
                try:
                    on_recorder_stall()
                except BaseException as exc:
                    failure.add_note('recorder interruption failed: '+repr(exc))
                wt.join(timeout=join_timeout)
            if wt.is_alive():
                failure.add_note('recorder worker is still running; do not close its files')
                failure.recorder_worker_running = True
        if record_error:
            failure = failure or CaptureError('recorder failed: '+record_error[0])
        try:
            observations.put_nowait(sentinel)
        except queue.Full:
            try:
                observations.get_nowait()
                drops += 1
            except queue.Empty:
                pass
            try:
                observations.put_nowait(sentinel)
            except queue.Full:
                drops += 1
        dt.join(timeout=join_timeout)
    if failure is not None:
        raise failure
    return FanoutResult(recorded[0], sampled, drops,
                        detector_error[0] if detector_error else None, not dt.is_alive())


def capture_experiment(source, recorder, count, detect, *, sample_every, source_timeout=5, join_timeout=2,
                       detector_fault=None, on_source_frame=None, stop_event=None,
                       skip_recording_verification=None):
    """Run a bounded capture without target input and clean up both processes.

    The capture is valid only if all requested frames were received, the source
    was still alive at the planned stop, and the recorder passed its lossless
    verification. A false detector_valid result cannot authorize observations.
    """
    try:
        result = fanout_frames(source.take(count, stop_event), recorder.write, detect,
            sample_every=sample_every, source_timeout=source_timeout, join_timeout=join_timeout,
            allow_source_eof=True, on_recorder_stall=recorder.interrupt,
            on_source_stop=lambda: source.stop(require_complete=False),
            detector_fault=detector_fault, on_source_frame=on_source_frame)
        if result.frames_recorded != count and not (stop_event is not None and stop_event.is_set()
                                                    and result.frames_recorded > 0):
            raise CaptureError('recorded frame count differs from planned count')
        try:
            source.stop()
        except BaseException as error:
            error.source_cleanup_incomplete = True
            raise
        if skip_recording_verification is not None and skip_recording_verification():
            recorder.finish_unverified()
        else:
            recorder.finish()
        return result
    except BaseException as error:
        if not source.closed:
            try:
                source.stop(require_complete=False)
            except BaseException as cleanup:
                error.source_cleanup_incomplete = True
                error.add_note('capture source cleanup failed: '+repr(cleanup))
        if not recorder.closed and not getattr(error, 'recorder_worker_running', False):
            try:
                recorder.abort()
            except BaseException as cleanup:
                error.source_cleanup_incomplete = True
                error.add_note('recorder cleanup failed: '+repr(cleanup))
        raise
