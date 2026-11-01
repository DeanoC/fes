"""Regress the application-fixture and paired-frame acceptance boundaries."""
import hashlib
import json
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import pcxt_acceptance_media as media
import sim_fes_pcxt_dos as simulation


class AcceptanceMediaTest(unittest.TestCase):
    @unittest.skipUnless(all(shutil.which(x) for x in ('as', 'ld', 'mcopy')), 'binutils and mtools required')
    def test_com_recreates_pinned_disk_and_rejects_damage(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'com.img'
            receipt = media.build('com', output)
            self.assertEqual(receipt['image_sha256'], media.SCENARIOS['com']['image_sha256'])
            self.assertEqual(receipt['program_sha256'], media.SCENARIOS['com']['program_sha256'])
            original = output.read_bytes()
            media.build('com', output)
            self.assertEqual(output.read_bytes(), original)
            damaged = bytearray(original)
            damaged[-1] ^= 1
            with self.assertRaisesRegex(ValueError, 'digest mismatch'):
                media.validate(bytes(damaged), 'com')

    def test_game_executable_is_external_and_must_match(self):
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            output = temp / 'game.img'
            with self.assertRaisesRegex(ValueError, 'independently acquired'):
                media.build('game', output)
            bogus = temp / 'DIGGER.EXE'
            bogus.write_bytes(b'not the selected game')
            with self.assertRaisesRegex(ValueError, 'executable digest mismatch'):
                media.build('game', output, bogus)
            self.assertFalse(output.exists())


class GameFrameOracleTest(unittest.TestCase):
    def test_rejects_changed_frame_without_publishing_result(self):
        header = b'P6\n1280 720\n255\n'
        with tempfile.TemporaryDirectory() as temp:
            build = Path(temp)
            (build / 'result-pending.json').write_text(json.dumps({'classification': 'candidate'}))
            refs = {}
            for index, name in enumerate(simulation.GAME_FRAME_SHA256):
                data = header + bytes([index]) * (1280 * 720 * 3)
                (build / (name + '.ppm')).write_bytes(data)
                refs[name] = hashlib.sha256(data).hexdigest()
            (build / 'right-key.ppm').write_bytes(header + bytes([9]) * (1280 * 720 * 3))
            with mock.patch.object(simulation, 'GAME_FRAME_SHA256', refs):
                with self.assertRaisesRegex(ValueError, 'differs from reviewed reference'):
                    simulation.verify_game(build)
            self.assertFalse((build / 'result.json').exists())
            self.assertTrue((build / 'right-key.ppm').exists())
            self.assertTrue((build / 'result-pending.json').exists())


if __name__ == '__main__':
    unittest.main()
