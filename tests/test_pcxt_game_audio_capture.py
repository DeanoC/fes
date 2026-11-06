import shutil
import json
import subprocess
import tempfile
import time
import unittest
import wave
from pathlib import Path
from unittest import mock

from scripts import pcxt_game_audio_capture as audio


class AudioCaptureTests(unittest.TestCase):
    @unittest.skipUnless(shutil.which('ffmpeg'), 'ffmpeg required')
    def test_synthetic_audio_records_and_verifies_mapping(self):
        real_popen = subprocess.Popen
        def synthetic(argv, **kwargs):
            i = argv.index('-f'); j = argv.index('-i', i)
            return real_popen(argv[:i] + ['-re','-f','lavfi','-i',
                'anullsrc=channel_layout=stereo:sample_rate=48000'] + argv[j+2:], **kwargs)
        for kit, video, device in (('A', 'fes-shadowcast-hil', 'hw:CARD=S3,DEV=0'),
                                   ('B', 'fes-4kpro-hil', 'hw:CARD=D4KPRO,DEV=0')):
            with self.subTest(kit=kit), tempfile.TemporaryDirectory() as tmp:
                mapping = {'kit':kit,'hil_video':'/dev/v4l/by-id/'+video}
                with mock.patch.object(audio, 'capture_identity', return_value=mapping), \
                     mock.patch.object(audio.subprocess, 'Popen', side_effect=synthetic):
                    recorder = audio.AudioRecorder(Path(tmp), kit)
                    recorder.start()
                    time.sleep(1.1)
                    recorder.check()
                    result = recorder.stop()
                self.assertEqual(result['device'], device)
                self.assertGreaterEqual(result['frames'], 48000)
                self.assertIn('uncalibrated', result['clock_note'])
                self.assertEqual(len(result['sha256']), 64)
                self.assertIsNotNone(recorder.proc.poll())

    def test_failed_start_with_uncertain_cleanup_is_marked(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            with mock.patch.object(audio, 'capture_identity', side_effect=audio.AudioError('mapping failed')), \
                 mock.patch.object(recorder, '_abort_start', side_effect=RuntimeError('stuck')):
                with self.assertRaises(audio.AudioError) as caught:
                    recorder.start()
            self.assertTrue(caught.exception.audio_cleanup_incomplete)

    def test_stuck_audio_reader_marks_cleanup_and_preserves_shared_streams(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            recorder.proc = mock.Mock(); recorder.proc.poll.return_value = None
            recorder.proc.wait.return_value = 0
            recorder.reader = mock.Mock(); recorder.reader.is_alive.return_value = True
            recorder.progress = mock.Mock(); recorder.stderr = mock.Mock()
            with self.assertRaises(audio.AudioError) as caught:
                recorder.stop()
            self.assertTrue(caught.exception.audio_cleanup_incomplete)
            recorder.progress.close.assert_not_called()
            recorder.stderr.close.assert_not_called()

    def test_only_designated_kit_mappings_are_supported(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(audio.AudioRecorder(Path(tmp), 'B').device, 'hw:CARD=D4KPRO,DEV=0')
            with self.assertRaises(audio.AudioError): audio.AudioRecorder(Path(tmp), 'C')

    def test_kit_b_binds_audio_and_video_to_same_usb_and_fanout(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            sysroot, procroot, devroot = root/'sys', root/'proc', root/'dev'
            usb = sysroot/'devices/4-5'; video_if = usb/'4-5:1.0'; audio_if = usb/'4-5:1.2'
            video_if.mkdir(parents=True); audio_if.mkdir()
            for name, value in (('idVendor', '0b05'), ('idProduct', '1ab9'),
                                ('serial', '802B003090700329')):
                (usb/name).write_text(value+'\n')
            sound = sysroot/'class/sound/card4'; sound.mkdir(parents=True)
            (sound/'device').symlink_to(audio_if)
            video = sysroot/'class/video4linux/video2'; video.mkdir(parents=True)
            (video/'device').symlink_to(video_if)
            loop = sysroot/'class/video4linux/video20'; loop.mkdir(parents=True)
            (loop/'name').write_text('fes-4kpro-hil\n')
            (procroot/'asound/card4').mkdir(parents=True)
            (procroot/'asound/D4KPRO').symlink_to('card4')
            (procroot/'42').mkdir()
            by_id = devroot/'v4l/by-id'; by_id.mkdir(parents=True)
            (devroot/'video2').touch(); (devroot/'video20').touch()
            raw = by_id/'usb-ASUS_4KPRO_802B003090700329-video-index0'
            hil = by_id/'fes-4kpro-hil'
            raw.symlink_to('../../video2'); hil.symlink_to('../../video20')
            (procroot/'42/cmdline').write_bytes(b'ffmpeg\0-i\0'+str(raw).encode()+b'\0'+str(hil).encode()+b'\0')
            runner = mock.Mock(return_value=subprocess.CompletedProcess([], 0, stdout='42\n'))
            mapping = audio.capture_identity('B', sysroot=sysroot, procroot=procroot,
                                             devroot=devroot, runner=runner)
            self.assertEqual(mapping['alsa_card'], 'card4')
            self.assertEqual(mapping['hil_video'], str(hil))
            self.assertEqual(mapping['usb']['serial'], '802B003090700329')
            runner.assert_called_once_with(['systemctl', 'show', 'fes-4kpro-fanout.service',
                                            '--property=MainPID', '--value'],
                                           check=True, capture_output=True, text=True, timeout=5)

            # A different physical card with matching model and serial is not
            # the video source; equality includes the real USB parent path.
            other = sysroot/'devices/4-6'; other_if = other/'4-6:1.2'; other_if.mkdir(parents=True)
            for name in ('idVendor', 'idProduct', 'serial'):
                (other/name).write_text((usb/name).read_text())
            (sound/'device').unlink(); (sound/'device').symlink_to(other_if)
            with self.assertRaisesRegex(audio.AudioError, 'physical capture identities differ'):
                audio.capture_identity('B', sysroot=sysroot, procroot=procroot, devroot=devroot, runner=runner)
            (sound/'device').unlink(); (sound/'device').symlink_to(audio_if)

            (usb/'serial').write_text('unexpected-card\n')
            with self.assertRaisesRegex(audio.AudioError, 'physical capture identities differ'):
                audio.capture_identity('B', sysroot=sysroot, procroot=procroot, devroot=devroot, runner=runner)
            (usb/'serial').write_text('802B003090700329\n')
            (loop/'name').write_text('wrong-loopback\n')
            with self.assertRaisesRegex(audio.AudioError, 'loopback identity differs'):
                audio.capture_identity('B', sysroot=sysroot, procroot=procroot, devroot=devroot, runner=runner)
            (loop/'name').write_text('fes-4kpro-hil\n')
            runner.return_value.stdout = '0\n'
            with self.assertRaisesRegex(audio.AudioError, 'fanout is not running'):
                audio.capture_identity('B', sysroot=sysroot, procroot=procroot, devroot=devroot, runner=runner)
            runner.return_value.stdout = '42\n'
            (procroot/'42/cmdline').write_bytes(b'ffmpeg\0-i\0/dev/wrong\0'+str(hil).encode()+b'\0')
            with self.assertRaisesRegex(audio.AudioError, 'fanout does not connect'):
                audio.capture_identity('B', sysroot=sysroot, procroot=procroot, devroot=devroot, runner=runner)

    def test_progress_writer_failure_is_latched_and_blocks_input(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            recorder.proc = mock.Mock(); recorder.proc.poll.return_value = None
            recorder.proc.stdout = iter(['out_time_us=10\n'])
            recorder.progress = mock.Mock(); recorder.progress.write.side_effect = OSError('disk full')
            recorder._read_progress()
            self.assertTrue(recorder.reader_ended.is_set())
            self.assertIn('disk full', recorder.reader_error)
            with self.assertRaisesRegex(audio.AudioError, 'progress reader'):
                recorder.check()

    def test_early_progress_eof_blocks_input_while_recorder_is_running(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            recorder.proc = mock.Mock(); recorder.proc.poll.return_value = None
            recorder.reader_ended.set()
            with self.assertRaisesRegex(audio.AudioError, 'progress reader'):
                recorder.check()

    def test_truncated_pcm_is_rejected_even_if_header_claims_a_second(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            with wave.open(str(recorder.path), 'wb') as wav:
                wav.setnchannels(2); wav.setsampwidth(2); wav.setframerate(48000)
                wav.writeframes(bytes(48000*4))
            raw = recorder.path.read_bytes(); recorder.path.write_bytes(raw[:-100])
            with wave.open(str(recorder.path), 'rb') as wav:
                self.assertEqual(wav.getnframes(), 48000)
            recorder.proc = mock.Mock(); recorder.proc.poll.return_value = None
            recorder.proc.wait.return_value = 0
            recorder.reader = mock.Mock(); recorder.reader.is_alive.return_value = False
            recorder.reader_ended.set(); recorder.final_progress = True
            recorder.progress = mock.Mock(); recorder.stderr = mock.Mock()
            recorder.progress_path.write_text(json.dumps({'monotonic_ns':1, 'line':'out_time_us=1000000'})+'\n'
                                              +json.dumps({'monotonic_ns':2, 'line':'progress=end'})+'\n')
            with self.assertRaisesRegex(audio.AudioError, 'PCM data is truncated'):
                recorder.stop()

    def test_audio_progress_stalled_after_start_blocks_input(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            recorder.proc = mock.Mock(); recorder.proc.poll.return_value = None
            recorder.last_advance_ns = time.monotonic_ns()-audio.MAX_PROGRESS_STALL_NS-1
            with self.assertRaisesRegex(audio.AudioError, 'progress stalled'):
                recorder.check()

    def test_audio_progress_must_cover_each_observation_window(self):
        with tempfile.TemporaryDirectory() as tmp:
            recorder = audio.AudioRecorder(Path(tmp), 'A')
            recorder.proc = mock.Mock(); recorder.proc.poll.return_value = None
            recorder.proc.wait.return_value = 0
            recorder.reader = mock.Mock(); recorder.reader.is_alive.return_value = False
            recorder.reader_ended.set(); recorder.final_progress = True
            recorder.progress = mock.Mock(); recorder.stderr = mock.Mock()
            recorder.progress_path.write_text(json.dumps({'monotonic_ns':1, 'line':'out_time_us=1000000'})+'\n'
                                              +json.dumps({'monotonic_ns':2, 'line':'progress=end'})+'\n')
            phases = [{'observation_started_monotonic_ns':10, 'observation_finished_monotonic_ns':20}]*3
            with self.assertRaisesRegex(audio.AudioError, 'does not cover'):
                recorder.stop(phases=phases)


if __name__ == '__main__': unittest.main()
