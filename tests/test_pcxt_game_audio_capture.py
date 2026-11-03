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
        with tempfile.TemporaryDirectory() as tmp:
            mapping = {'kit':'A','hil_video':'/dev/v4l/by-id/fes-shadowcast-hil'}
            with mock.patch.object(audio, 'capture_identity', return_value=mapping), \
                 mock.patch.object(audio.subprocess, 'Popen', side_effect=synthetic):
                recorder = audio.AudioRecorder(Path(tmp), 'A')
                recorder.start()
                time.sleep(1.1)
                recorder.check()
                result = recorder.stop()
            self.assertEqual(result['device'], 'hw:CARD=S3,DEV=0')
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

    def test_only_designated_kit_mapping_is_supported(self):
        with tempfile.TemporaryDirectory() as tmp:
            with self.assertRaises(audio.AudioError): audio.AudioRecorder(Path(tmp), 'B')

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
            recorder.progress_path.write_text(json.dumps({'monotonic_ns':1, 'line':'progress=end'})+'\n')
            with self.assertRaisesRegex(audio.AudioError, 'PCM data is truncated'):
                recorder.stop()


if __name__ == '__main__': unittest.main()
