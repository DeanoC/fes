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
    def test_short_smoke_keeps_guards_and_records_three_sound_windows(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);probe=mock.Mock(spec=PlaybackProbe)
            probe.plan=[0x50];probe.fault=threading.Event();probe.sent=[{'usage':0x50}]
            probe.snapshot.return_value={'player':True,'initial_lives':True,'threat':False}
            video=mock.Mock(observer_guard=probe,started_ns=1,first_progress_ns=2,
                            converter_identity={'test':'scaler'})
            video.proc.poll.return_value=None
            video.path=root/'game.mkv';video.path.write_bytes(b'video')
            video.progress_path=root/'frames.jsonl';video.progress_path.write_bytes(b'frames')
            audio=mock.Mock(mapping={'hil_video':'synthetic'});audio.proc.poll.return_value=None
            audio.stop.return_value={'file':'audio.wav'}
            observer=mock.Mock();args=SimpleNamespace(evidence=root,capture_device='synthetic',
                                                     kit='A',game_short_smoke=True)
            rec={'captures':[],'hid_events':[]}
            with mock.patch.object(app,'capture_owned',return_value=((1,1,b'x'),{'file':'before'})), \
                 mock.patch.object(app,'prompt',return_value={}),mock.patch.object(app,'type_keys') as keys, \
                 mock.patch.object(app,'wait_owned') as wait, mock.patch.object(app.time,'sleep'), \
                 mock.patch.object(app,'check_video',return_value={}),mock.patch.object(app,'check_source_coverage',return_value={}):
                app.run_game(None,None,args,rec,'input',b'font',probe=probe,
                             video_factory=lambda *_:video,audio_factory=lambda *_:audio,
                             observer_factory=lambda *_:observer)
            self.assertEqual(rec['gameplay_mode'],'short-smoke')
            self.assertEqual([x['direction'] for x in rec['direction_intervals']],['left'])
            self.assertEqual(len(rec['f9_toggles']),3)
            self.assertEqual([call.args[6] for call in keys.call_args_list][-4:],[(0x50,),(0x42,),(0x42,),(0x42,)])
            self.assertIs(keys.call_args_list[-4].kwargs['dispatch_guard'],probe)
            self.assertEqual(sum(call.args[4] == 6 for call in wait.call_args_list),3)
            audio.stop.assert_called_once_with(phases=rec['f9_toggles'])
            self.assertIn('neutral and left need independent visual review',rec['game_video']['classification'])

    def test_short_smoke_rejects_a_full_direction_guard_before_game_input(self):
        args=SimpleNamespace(game_short_smoke=True)
        probe=PlaybackProbe(threading.Event(),max_host_age_ns=100000000)
        with mock.patch.object(app,'capture_owned') as capture, mock.patch.object(app,'type_keys') as keys:
            with self.assertRaisesRegex(app.pa.AcceptanceError,'one-direction guard'):
                app.run_game(None,None,args,{},'input',b'font',probe=probe,
                             video_factory=mock.Mock(),audio_factory=mock.Mock())
        capture.assert_not_called();keys.assert_not_called()

    def test_short_smoke_never_sends_f9_after_a_rejected_direction(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);probe=mock.Mock(spec=PlaybackProbe)
            probe.plan=[0x50];probe.fault=threading.Event();probe.sent=[]
            probe.snapshot.return_value={'player':True,'initial_lives':True,'threat':False}
            video=mock.Mock(observer_guard=probe,started_ns=1,first_progress_ns=2,
                            converter_identity={'test':'scaler'})
            video.proc.poll.return_value=None
            audio=mock.Mock(mapping={'hil_video':'synthetic'});audio.proc.poll.return_value=None
            observer=mock.Mock();args=SimpleNamespace(evidence=root,capture_device='synthetic',
                                                     kit='A',game_short_smoke=True)
            def guarded_keys(*args,**kwargs):
                if args[6] == (0x50,):
                    raise app.gameplay.ProbeError('player became unsafe')
            with mock.patch.object(app,'capture_owned',return_value=((1,1,b'x'),{'file':'before'})), \
                 mock.patch.object(app,'prompt',return_value={}), \
                 mock.patch.object(app,'type_keys',side_effect=guarded_keys) as keys, \
                 mock.patch.object(app,'wait_owned'), mock.patch.object(app.time,'sleep'):
                with self.assertRaisesRegex(app.gameplay.ProbeError,'player became unsafe'):
                    app.run_game(None,None,args,{'captures':[],'hid_events':[]},'input',b'font',probe=probe,
                                 video_factory=lambda *_:video,audio_factory=lambda *_:audio,
                                 observer_factory=lambda *_:observer)
            self.assertNotIn((0x42,),[call.args[6] for call in keys.call_args_list])
            audio.stop.assert_called_once_with(phases=None)
            observer.close.assert_called_once()

    def test_video_stop_failure_still_closes_observer_and_retains_uncertainty(self):
        for observer_fails in (False, True):
            with self.subTest(observer_fails=observer_fails), tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);probe=mock.Mock(spec=PlaybackProbe);probe.fault=threading.Event();probe.sent=[]
                video=mock.Mock();video.observer_guard=probe;video.proc.poll.return_value=None
                video.started_ns=1;video.first_progress_ns=2;video.converter_identity={}
                video.stop.side_effect=RuntimeError('video stop failed')
                audio=mock.Mock(mapping={'hil_video':'synthetic'});audio.proc.poll.return_value=None
                audio.stop.return_value={'file':'audio.wav'}
                observer=mock.Mock()
                if observer_fails:observer.close.side_effect=RuntimeError('observer cleanup failed')
                args=SimpleNamespace(evidence=root,capture_device='synthetic',kit='A')
                with mock.patch.object(app,'capture_owned',return_value=((1,1,b'x'),{'file':'before'})), \
                     mock.patch.object(app,'prompt',return_value={}),mock.patch.object(app,'type_keys'), \
                     mock.patch.object(app,'wait_owned'),mock.patch.object(app.time,'sleep'):
                    with self.assertRaisesRegex(RuntimeError,'video stop failed') as caught:
                        app.run_game(None,None,args,{'captures':[],'hid_events':[]},'input',b'font',
                            probe=probe,video_factory=lambda *_:video,audio_factory=lambda *_:audio,
                            observer_factory=lambda *_:observer)
                observer.close.assert_called_once()
                self.assertEqual(bool(getattr(caught.exception,'source_cleanup_incomplete',False)),observer_fails)

    def test_mismatched_stream_preserves_both_cleanup_failures(self):
        with tempfile.TemporaryDirectory() as tmp:
            probe=mock.Mock(spec=PlaybackProbe);video=mock.Mock(spec=['stop'])
            video.stop.side_effect=RuntimeError('video cleanup')
            observer=mock.Mock();observer.close.side_effect=RuntimeError('observer cleanup')
            args=SimpleNamespace(evidence=Path(tmp),capture_device='synthetic',kit='A')
            with mock.patch.object(app,'capture_owned',return_value=((1,1,b'x'),{'file':'before'})), \
                 mock.patch.object(app,'prompt',return_value={}),mock.patch.object(app,'type_keys') as keys:
                with self.assertRaisesRegex(app.pa.AcceptanceError,'no game keys sent') as caught:
                    app.run_game(None,None,args,{'captures':[],'hid_events':[]},'input',b'font',
                        probe=probe,video_factory=lambda *_:video,audio_factory=mock.Mock(),
                        observer_factory=lambda *_:observer)
            self.assertTrue(caught.exception.source_cleanup_incomplete)
            self.assertEqual(len(caught.exception.__notes__),2)
            observer.close.assert_called_once();keys.assert_not_called()

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
            verify = video.recorder.verify_capture
            def verify_after_cleanup(**kwargs):
                self.assertFalse(video.worker.is_alive())
                self.assertTrue(video.source.closed)
                self.assertTrue(video.recorder.closed)
                self.assertIsNotNone(video.source.proc.poll())
                self.assertIsNotNone(video.recorder.proc.poll())
                verify(**kwargs)
            with mock.patch.object(video.recorder, 'verify_capture', side_effect=verify_after_cleanup) as checked:
                video.stop()
            checked.assert_called_once()
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
