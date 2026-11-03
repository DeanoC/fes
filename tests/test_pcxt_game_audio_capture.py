import shutil
import subprocess
import tempfile
import time
import unittest
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


if __name__ == '__main__': unittest.main()
