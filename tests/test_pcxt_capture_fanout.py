import io
import json
import os
from pathlib import Path
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

from scripts import pcxt_capture_fanout as capture


def packet(seq, captured, dequeued, flags=0x2000, pixels=b'abcd'):
    return capture.HEADER.pack(capture.MAGIC, seq, captured, dequeued, flags, len(pixels)) + pixels


class SourceTests(unittest.TestCase):
    def test_frames_require_immutable_pixel_storage(self):
        with self.assertRaises(ValueError):
            capture.CapturedFrame(0, 1, 2, 3, 0x2000, bytearray(b'abcd'))

    def test_preserves_source_sequence_and_capture_times(self):
        source = io.BytesIO(packet(41, 10, 11) + packet(42, 20, 21))
        frames = list(capture.v4l2_frames(source, 4, clock=lambda: 30))
        self.assertEqual([(f.sequence, f.capture_ns, f.dequeue_ns, f.arrival_ns, f.pixels)
                          for f in frames], [(41, 10, 11, 30, b'abcd'), (42, 20, 21, 30, b'abcd')])

    def test_rejects_missing_duplicate_reordered_timestamp_and_corrupt_data(self):
        bad = [packet(5, 20, 21), packet(7, 20, 21), packet(4, 20, 21),
               packet(6, 10, 21), packet(6, 12, 9), packet(6, 20, 21, flags=0),
               packet(6, 20, 21)[:-1]]
        for data in bad:
            with self.subTest(data=data):
                with self.assertRaises(capture.CaptureError):
                    list(capture.v4l2_frames(io.BytesIO(packet(5, 10, 11)+data), 4, clock=lambda: 30))

    def test_rejects_partial_or_impossible_header(self):
        for data in (b'P', b'BAD!'+packet(1, 1, 2)[4:], packet(1, 1, 2, pixels=b'bad')):
            with self.subTest(data=data):
                with self.assertRaises(capture.CaptureError):
                    list(capture.v4l2_frames(io.BytesIO(data), 4, clock=lambda: 3))

    def test_capture_process_requires_planned_stop_and_reaps_child(self):
        with tempfile.TemporaryDirectory() as tmp:
            code = ('import os,struct,time; '
                    "os.write(1,struct.pack('<4sIQQII',b'PXT1',7,1,2,0x2000,4)+b'abcd'); "
                    'time.sleep(30)')
            source = capture.CaptureProcess([sys.executable, '-c', code], Path(tmp)/'stderr', 4)
            self.assertEqual([f.sequence for f in source.take(1)], [7])
            self.assertEqual(source.stop(), -15)
            self.assertIsNotNone(source.proc.poll())

    def test_group_signals_happen_before_leader_is_reaped(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = capture.CaptureProcess([sys.executable, '-c', 'import time; time.sleep(30)'],
                                            Path(tmp)/'stderr', 4)
            source.completed = True
            original = os.killpg
            signals = []
            def checked(pid, sig):
                if sig:
                    # waitid raises ChildProcessError if the child was reaped.
                    os.waitid(os.P_PID, pid, os.WEXITED | os.WNOHANG | os.WNOWAIT)
                    signals.append(sig)
                return original(pid, sig)
            with mock.patch.object(capture.os, 'killpg', side_effect=checked):
                source.stop()
            self.assertEqual(signals, [15, 9])

    def test_capture_process_rejects_early_eof(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = capture.CaptureProcess([sys.executable, '-c', 'pass'], Path(tmp)/'stderr', 4)
            try:
                with self.assertRaises(capture.CaptureError):
                    list(source.take(1))
                with self.assertRaises(capture.CaptureError):
                    source.stop()
            finally:
                if source.proc.poll() is None:
                    source.stop(require_complete=False)

    def test_process_to_ffv1_experiment_reaps_source_and_recorder(self):
        with tempfile.TemporaryDirectory() as tmp:
            code = ("import os,struct,time\n"
                    "for i in range(5):\n"
                    " os.write(1,struct.pack('<4sIQQII',b'PXT1',i,i+1,i+2,0x2000,512)+bytes([i])*512)\n"
                    " time.sleep(.05)\n"
                    "time.sleep(30)\n")
            source = capture.CaptureProcess([sys.executable, '-c', code], Path(tmp)/'source.stderr', 512)
            recorder = capture.FFV1Recorder(tmp, width=16, height=16)
            result = capture.capture_experiment(source, recorder, 5, lambda _: None, sample_every=1)
            self.assertEqual(result.frames_recorded, 5)
            self.assertTrue(source.closed)
            self.assertIsNotNone(source.proc.poll())
            self.assertTrue(recorder.closed)
            self.assertEqual(recorder.proc.returncode, 0)

    def test_failed_run_closes_processes_without_slow_full_decode(self):
        with tempfile.TemporaryDirectory() as tmp:
            code = ("import os,struct,time\n"
                    "for i in range(5):\n"
                    " os.write(1,struct.pack('<4sIQQII',b'PXT1',i,i+1,i+2,0x2000,512)+bytes([i])*512)\n"
                    " time.sleep(.05)\n"
                    "time.sleep(30)\n")
            source = capture.CaptureProcess([sys.executable, '-c', code], Path(tmp)/'source.stderr', 512)
            recorder = capture.FFV1Recorder(tmp, width=16, height=16)
            with mock.patch.object(recorder, '_verify_pixels', side_effect=AssertionError('full decode')):
                result = capture.capture_experiment(source, recorder, 5, lambda _: None,
                    sample_every=1, skip_recording_verification=lambda: True)
            self.assertEqual(result.frames_recorded, 5)
            self.assertTrue(source.closed); self.assertIsNotNone(source.proc.poll())
            self.assertTrue(recorder.closed); self.assertEqual(recorder.proc.returncode, 0)
            self.assertGreater(recorder.video.stat().st_size, 0)

    def test_recorder_failure_terminates_source_stuck_in_read(self):
        with tempfile.TemporaryDirectory() as tmp:
            code = ('import os,struct,time; '
                    "os.write(1,struct.pack('<4sIQQII',b'PXT1',7,1,2,0x2000,4)+b'abcd'); "
                    'time.sleep(30)')
            source = capture.CaptureProcess([sys.executable, '-c', code], Path(tmp)/'stderr', 4)
            def fail(_):
                raise OSError('disk full')
            start = time.monotonic()
            with self.assertRaises(capture.CaptureError):
                capture.fanout_frames(source.take(2), fail, lambda _: None, sample_every=1,
                    source_timeout=1, join_timeout=.2,
                    on_source_stop=lambda: source.stop(require_complete=False))
            self.assertTrue(source.closed)
            self.assertIsNotNone(source.proc.poll())
            self.assertLess(time.monotonic()-start, 2)

    def test_experiment_propagates_unverified_source_cleanup(self):
        class Source:
            closed = False
            def take(self, count, stop_event):
                yield capture.CapturedFrame(0, 1, 2, 3, 0x2000, b'x')
            def stop(self, require_complete=True):
                self.closed = True
                raise capture.CaptureError('unverified group')
        class Recorder:
            closed = False
            def write(self, frame): pass
            def abort(self): self.closed = True
            def interrupt(self): pass
        with self.assertRaises(capture.CaptureError) as caught:
            capture.capture_experiment(Source(), Recorder(), 1, lambda _: None,
                                       sample_every=1, join_timeout=.05)
        self.assertTrue(caught.exception.source_cleanup_incomplete)

    def test_experiment_rejects_early_source_exit_and_reaps_processes(self):
        with tempfile.TemporaryDirectory() as tmp:
            code = ('import os,struct; '
                    "os.write(1,struct.pack('<4sIQQII',b'PXT1',7,1,2,0x2000,512)+bytes(512))")
            source = capture.CaptureProcess([sys.executable, '-c', code], Path(tmp)/'source.stderr', 512)
            recorder = capture.FFV1Recorder(tmp, width=16, height=16)
            with self.assertRaises(capture.CaptureError):
                capture.capture_experiment(source, recorder, 2, lambda _: None, sample_every=1)
            self.assertTrue(source.closed)
            self.assertTrue(recorder.closed)
            self.assertIsNotNone(source.proc.poll())
            self.assertIsNotNone(recorder.proc.poll())

    def test_source_stop_kills_descendant_holding_capture_pipe(self):
        with tempfile.TemporaryDirectory() as tmp:
            pid_file = Path(tmp)/'descendant.pid'
            code = ("import subprocess,sys,time,pathlib\n"
                    "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'])\n"
                    f"pathlib.Path({str(pid_file)!r}).write_text(str(child.pid))\n"
                    "time.sleep(30)\n")
            source = capture.CaptureProcess([sys.executable, '-c', code], Path(tmp)/'stderr', 4)
            with self.assertRaises(capture.CaptureError) as caught:
                capture.fanout_frames(source.take(1), lambda _: None, lambda _: None, sample_every=1,
                    source_timeout=.25, join_timeout=.5,
                    on_source_stop=lambda: source.stop(require_complete=False))
            self.assertFalse(getattr(caught.exception, 'source_cleanup_incomplete', False))
            self.assertTrue(source.closed)
            self.assertIsNotNone(source.proc.poll())
            pid = int(pid_file.read_text())
            stat = Path(f'/proc/{pid}/stat')
            if stat.exists():
                # An orphaned zombie may await init's reap, but has no open pipe.
                self.assertEqual(stat.read_text().split(') ', 1)[1][0], 'Z')


class FanoutTests(unittest.TestCase):
    @staticmethod
    def frames(count):
        for i in range(count):
            time.sleep(.002)  # allow recorder to run without faking source overload
            yield capture.CapturedFrame(i, i+1, i+2, i+3, 0x2000, bytes([i]))

    def test_stalled_detector_cannot_hold_recorder(self):
        blocker = threading.Event()
        recorded = []
        start = time.monotonic()
        try:
            result = capture.fanout_frames(self.frames(30), lambda f: recorded.append(f.sequence),
                                           lambda _: blocker.wait(5), sample_every=1,
                                           record_capacity=8, join_timeout=.05, allow_source_eof=True)
        finally:
            blocker.set()
        self.assertEqual(recorded, list(range(30)))
        self.assertEqual(result.frames_recorded, 30)
        self.assertGreater(result.detector_drops, 0)
        self.assertFalse(result.detector_stopped)
        self.assertLess(time.monotonic()-start, 1)

    def test_crashed_detector_cannot_hold_recorder(self):
        recorded = []
        def crash(_):
            raise RuntimeError('detector failed')
        result = capture.fanout_frames(self.frames(20), lambda f: recorded.append(f.sequence),
                                       crash, sample_every=1, record_capacity=8, join_timeout=.05, allow_source_eof=True)
        self.assertEqual(recorded, list(range(20)))
        self.assertEqual(result.frames_recorded, 20)
        self.assertIn('detector failed', result.detector_error)

    def test_recorder_overload_fails_instead_of_dropping_video(self):
        blocker = threading.Event()
        writer_stopped = threading.Event()
        def stall(_):
            blocker.wait(5)
            writer_stopped.set()
        try:
            with self.assertRaises(capture.CaptureError):
                capture.fanout_frames(self.frames(20), stall,
                                      lambda _: None, sample_every=5,
                                      record_capacity=1, join_timeout=.02, on_recorder_stall=blocker.set)
            self.assertTrue(writer_stopped.is_set())
        finally:
            blocker.set()

    def test_recorder_exception_fails(self):
        def crash(_):
            raise OSError('disk full')
        with self.assertRaises(capture.CaptureError):
            capture.fanout_frames(self.frames(20), crash, lambda _: None,
                                  sample_every=5, record_capacity=3, join_timeout=.05)

    def test_unexpected_or_empty_eof_fails(self):
        for source, allowed in ((iter([]), True), (self.frames(1), False)):
            with self.subTest(allowed=allowed), self.assertRaises(capture.CaptureError):
                capture.fanout_frames(source, lambda _: None, lambda _: None,
                                      sample_every=1, join_timeout=.05, allow_source_eof=allowed)

    def test_failed_recorder_cannot_be_hidden_by_stalled_source(self):
        blocker = threading.Event()
        def source():
            yield capture.CapturedFrame(0, 1, 2, 3, 0x2000, b'x')
            blocker.wait(5)
        def fail(_):
            raise OSError('disk full')
        start = time.monotonic()
        try:
            with self.assertRaises(capture.CaptureError):
                capture.fanout_frames(source(), fail, lambda _: None, sample_every=1,
                                      join_timeout=.05, source_timeout=.15, on_source_stop=blocker.set)
        finally:
            blocker.set()
        self.assertLess(time.monotonic()-start, 1)

    def test_real_ffv1_recording_survives_stalled_detector(self):
        blocker = threading.Event()
        with tempfile.TemporaryDirectory() as tmp:
            recorder = capture.FFV1Recorder(tmp, width=16, height=16)
            def frames():
                for i in range(30):
                    yield capture.CapturedFrame(i, i+1, i+2, i+3, 0x2000, bytes([i])*512)
            try:
                result = capture.fanout_frames(frames(), recorder.write, lambda _: blocker.wait(5),
                                               sample_every=1, record_capacity=64, join_timeout=.1, allow_source_eof=True)
                recorder.finish()
            finally:
                blocker.set()
            lines = [json.loads(line) for line in recorder.sidecar_path.read_text().splitlines()]
            self.assertEqual([line['sequence'] for line in lines], list(range(30)))
            self.assertEqual(result.frames_recorded, 30)
            self.assertGreater(result.detector_drops, 0)

    def test_finish_rejects_changed_sidecar(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = capture.FFV1Recorder(tmp, width=16, height=16)
            recorder.write(capture.CapturedFrame(0, 1, 2, 3, 0x2000, bytes(512)))
            recorder.sidecar_path.write_text('')
            with self.assertRaises(capture.CaptureError):
                recorder.finish()

    def test_finish_rejects_sidecar_hash_not_matching_encoded_pixels(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = capture.FFV1Recorder(tmp, width=16, height=16)
            recorder.write(capture.CapturedFrame(0, 1, 2, 3, 0x2000, bytes(512)))
            recorder.entries[0]['raw_sha256'] = '0'*64
            recorder.sidecar_path.write_text(json.dumps(recorder.entries[0])+'\n')
            with self.assertRaisesRegex(capture.CaptureError, 'decoded pixels'):
                recorder.finish()


if __name__ == '__main__':
    unittest.main()
