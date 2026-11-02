import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from scripts import pcxt_dos_hil_acceptance as visual
from scripts import pcxt_dos_keyboard_acceptance as acceptance


WIDTH, HEIGHT = 1280, 720


def frame(font, lines):
    rgb = bytearray(WIDTH * HEIGHT * 3)
    for row, text in lines.items():
        for col, character in enumerate(text):
            glyph = font[ord(character) * 16:ord(character) * 16 + 8]
            for gy in range(8):
                for gx in range(8):
                    if glyph[gy] & (0x80 >> gx):
                        x = round(160 + (col * 8 + gx + 0.5) * 1.5)
                        y = round((row * 8 + gy + 0.5) * HEIGHT / 200)
                        rgb[(y * WIDTH + x) * 3:(y * WIDTH + x) * 3 + 3] = b'\xff\xff\xff'
    return WIDTH, HEIGHT, bytes(rgb)


class PCXTDosKeyboardAcceptanceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.font = visual.load_font()
        cls.before = frame(cls.font, {2: 'A:\\>'})
        cls.after = frame(cls.font, {2: 'A:\\>dir', 5: 'FESXT   TXT       20', 9: 'A:\\>'})

    def test_private_host_input_config_is_required(self):
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / 'private.toml'
            for text in ('', '[remote_input]\nenabled = false\n',
                         '[remote_input]\nenabled = "true"\n'):
                config.write_text(text)
                with self.subTest(text=text), self.assertRaisesRegex(ValueError, 'enable remote input'):
                    acceptance.check_host_input_config(config)
            config.write_text('[remote_input]\nenabled = true\n')
            self.assertEqual(acceptance.check_host_input_config(config), {'remote_input_enabled': True})

    def test_invalid_config_never_prints_credential(self):
        secret = 'private-bearer-fixture'
        with tempfile.TemporaryDirectory() as tmp:
            config = Path(tmp) / 'private.toml'
            config.write_text('credential = "' + secret + '"\n[remote_input\n')
            errors = io.StringIO()
            with contextlib.redirect_stderr(errors), self.assertRaises(SystemExit):
                acceptance.main(['preflight', '--config', str(config)])
            self.assertIn('cannot be read or parsed', errors.getvalue())
            self.assertNotIn(secret, errors.getvalue())

    def test_visual_directory_and_later_prompt(self):
        result = acceptance.check_captures(self.before, self.after, self.font)
        self.assertEqual((result['entry_row'], result['prompt_row']), (5, 9))
        self.assertIn('FESXT', result['entry'])
        self.assertFalse(result['whole_board_qualified'])

    def test_receipt_does_not_publish_unrelated_screen_text(self):
        after = frame(self.font, {2: 'A:\\>dir', 5: 'FESXT   TXT   PRIVATE',
                                  9: 'A:\\>PRIVATE'})
        result = acceptance.check_captures(self.before, after, self.font)
        self.assertNotIn('PRIVATE', json.dumps(result))
        self.assertEqual((result['entry'], result['prompt']), ('FESXT.TXT', 'A:\\>'))

    def test_before_listing_or_missing_after_listing_is_rejected(self):
        with self.assertRaisesRegex(ValueError, 'already contains'):
            acceptance.check_captures(self.after, self.after, self.font)
        # A stale listing is still disqualifying when its following prompt
        # has not appeared yet. A later prompt must not turn it into proof.
        stale = frame(self.font, {2: 'A:\\>', 5: 'FESXT   TXT'})
        with self.assertRaisesRegex(ValueError, 'already contains'):
            acceptance.check_captures(stale, self.after, self.font)
        with self.assertRaisesRegex(ValueError, 'lacks the directory'):
            acceptance.check_captures(self.before, self.before, self.font)

    def test_filename_and_later_prompt_must_be_on_correct_rows(self):
        self.assertIsNone(acceptance.find_listing(['FESXT', 'TXT', 'A:\\>']))
        self.assertIsNone(acceptance.find_listing(['FESXTNOISE TXT', 'A:\\>']))
        self.assertIsNone(acceptance.find_listing(['A:\\>', 'FESXT   TXT']))
        self.assertEqual(acceptance.find_listing(['A:\\>', 'FESXT   TXT', 'A:\\>']), (1, 2))

    def test_existing_output_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'result.json'
            output.write_text('previous evidence')
            errors = io.StringIO()
            with patch.object(visual, 'decode_capture') as decode, contextlib.redirect_stderr(errors):
                with self.assertRaises(SystemExit):
                    acceptance.main(['captures', '--before', 'old', '--after', 'new', '--output', str(output)])
                decode.assert_not_called()
            self.assertEqual(output.read_text(), 'previous evidence')
            self.assertIn('output already exists', errors.getvalue())

    def test_capture_command_records_hashes_and_result(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'result.json'
            captures = [(*self.before, 'a' * 64), (*self.after, 'b' * 64)]
            with patch.object(visual, 'decode_capture', side_effect=captures), \
                 patch.object(visual, 'load_font', return_value=self.font), \
                 contextlib.redirect_stdout(io.StringIO()):
                acceptance.main(['captures', '--before', 'old', '--after', 'new', '--output', str(output)])
            result = json.loads(output.read_text())
            self.assertEqual(result['captures'], [{'role': 'before', 'sha256': 'a' * 64},
                                                  {'role': 'after', 'sha256': 'b' * 64}])
            self.assertEqual((result['entry_row'], result['prompt_row']), (5, 9))


if __name__ == '__main__':
    unittest.main()
