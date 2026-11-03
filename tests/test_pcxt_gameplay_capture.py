import hashlib
import json
import os
from pathlib import Path
import tempfile
import threading
import time
import unittest
from types import SimpleNamespace
from unittest import mock

from scripts import pcxt_dos_application_acceptance as app
from scripts import pcxt_gameplay_capture as capture_module
from scripts.pcxt_digger_readiness import Observation
from scripts.pcxt_gameplay_capture import RGBConverter, GameCapture, crop, inspect_frame
from scripts.pcxt_gameplay_probe import PlaybackProbe


class GameCaptureTests(unittest.TestCase):
    def test_build_binds_source_compiler_and_binary(self):
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(capture_module.__file__).with_name('pcxt_v4l2_capture.c')
            source_hash = hashlib.sha256(source.read_bytes()).hexdigest()
            binary, receipt = capture_module.build_capture_helper(Path(tmp), source, source_hash)
            self.assertEqual(receipt['source_sha256'], source_hash)
            self.assertEqual(receipt['binary_sha256'], hashlib.sha256(binary.read_bytes()).hexdigest())
            self.assertEqual(len(receipt['compiler_sha256']), 64)
            with tempfile.TemporaryDirectory() as other:
                with self.assertRaises(capture_module.capture.CaptureError):
                    capture_module.build_capture_helper(Path(other), source, '0'*64)

    def test_stuck_capture_startup_marks_cleanup_unverified(self):
        with tempfile.TemporaryDirectory() as tmp:
            helper = Path(tmp)/'helper'; helper.write_bytes(b'helper')
            probe = PlaybackProbe(threading.Event(), max_host_age_ns=1000000000)
            worker = mock.Mock(); worker.is_alive.return_value = True
            with mock.patch.object(capture_module, 'RGBConverter'), \
                 mock.patch.object(capture_module.capture, 'CaptureProcess'), \
                 mock.patch.object(capture_module.capture, 'FFV1Recorder'), \
                 mock.patch.object(capture_module.threading, 'Thread', return_value=worker):
                with self.assertRaises(capture_module.capture.CaptureError) as caught:
                    GameCapture('synthetic', Path(tmp), probe, helper,
                                hashlib.sha256(helper.read_bytes()).hexdigest(), [bytes(3264)], startup_timeout=0)
            self.assertTrue(caught.exception.source_cleanup_incomplete)

    def test_synthetic_frames_reach_observer_and_lossless_recorder(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            helper = root/'source'
            helper.write_text('''#!/usr/bin/python3
import os,struct,time
pixels=bytes([16,128,16,128])*(1920*1080//2)
for i in range(200):
 now=time.monotonic_ns()
 data=memoryview(struct.pack('<4sIQQII',b'PXT1',i,now,now,0x2000,len(pixels))+pixels)
 while data:
  n=os.write(1,data);data=data[n:]
 time.sleep(.1)
''')
            helper.chmod(0o700)
            data = bytes([16,128,16,128])*(1920*1080//2)
            converter = RGBConverter()
            try:
                template = crop(converter.convert(data),304,322,32,34)
                self.assertTrue(inspect_frame(converter, data, [template]).player)
            finally:
                converter.close()
            probe = PlaybackProbe(threading.Event(), max_host_age_ns=1000000000)
            video = GameCapture('synthetic', root, probe, helper,
                                hashlib.sha256(helper.read_bytes()).hexdigest(), [template], startup_timeout=10)
            time.sleep(.35)
            video.stop()
            rows = [json.loads(line) for line in video.progress_path.read_text().splitlines()]
            self.assertGreaterEqual(len(rows), 2)
            self.assertEqual([r['sequence'] for r in rows], list(range(len(rows))))
            self.assertTrue(video.result.detector_valid)
            self.assertTrue(video.source.closed)
            self.assertTrue(video.recorder.closed)
            self.assertIs(video.observer_guard, probe)

    def test_game_path_records_the_same_stream_as_ordered_requests(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); helper = root/'source'
            helper.write_text('''#!/usr/bin/python3
import os,struct,time
pixels=bytes([16,128,16,128])*(1920*1080//2)
for i in range(200):
 now=time.monotonic_ns()
 data=memoryview(struct.pack('<4sIQQII',b'PXT1',i,now,now,0x2000,len(pixels))+pixels)
 while data:
  n=os.write(1,data);data=data[n:]
 time.sleep(.1)
''')
            helper.chmod(0o700)
            probe = PlaybackProbe(threading.Event(), max_host_age_ns=1000000000)
            runner = mock.Mock(); runner.api.timeout = 20
            runner.api.post_json.return_value = {'ok':True, 'host_duration_ms':1}
            args = SimpleNamespace(evidence=root, capture_device='synthetic', kit='A')
            rec = {'captures':[], 'hid_events':[]}
            audio = mock.Mock(mapping={'hil_video':'synthetic'})
            audio.proc.poll.return_value = None
            audio.stop.side_effect = lambda **_kwargs: (time.sleep(.2) or {'file':'hdmi-audio.wav'})
            source_hash = hashlib.sha256(helper.read_bytes()).hexdigest()
            def factory(device, evidence, selected_probe):
                return GameCapture(device, evidence, selected_probe, helper, source_hash, [bytes(3264)], startup_timeout=10)
            def short_wait(_runner, _owned, _args, _disk, seconds, _video, **_kwargs):
                if seconds == 6: time.sleep(.2)
            positive = Observation(True,False,True,0,0,100,100)
            real_fork = os.fork
            with mock.patch.object(capture_module, 'inspect_frame', return_value=positive), \
                 mock.patch.object(app, 'capture_owned', return_value=((1,1,b'x'), {'file':'before'})), \
                 mock.patch.object(app, 'prompt', return_value={}), \
                 mock.patch.object(app, 'wait_owned', side_effect=short_wait), mock.patch.object(app, 'validate_active'), \
                 mock.patch.object(app, '_observe_until', return_value={'owner':'fixture'}), \
                 mock.patch.object(app.os, 'fork', side_effect=real_fork) as fork, \
                 mock.patch.object(app, 'input_status'), mock.patch.object(app, 'check_video', return_value={}):
                app.run_game(runner, SimpleNamespace(), args, rec, 'input', b'font',
                             probe=probe, video_factory=factory, audio_factory=lambda *_:audio)
            self.assertEqual(fork.call_count, 1)
            self.assertEqual([entry['direction'] for entry in rec['direction_intervals']], ['left','right','left'])
            self.assertEqual([item['usage'] for item in rec['direction_observations']], [0x50,0x4f,0x50])
            self.assertIn('independent visual review', rec['game_video']['classification'])
            self.assertEqual(rec['game_video']['frame_metadata_sha256'],
                             hashlib.sha256((root/rec['game_video']['frame_metadata_file']).read_bytes()).hexdigest())
            self.assertEqual(rec['classification'], 'game and audio captured; independent gameplay and speaker review pending')


if __name__ == '__main__': unittest.main()
