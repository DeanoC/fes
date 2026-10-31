import base64
import unittest
import zlib

from scripts import pcxt_dos_hil_acceptance as acceptance


WIDTH, HEIGHT = 1280, 720


def frame(color=(0, 0, 0)):
    return bytearray(color * (WIDTH * HEIGHT))


def prompt_frame(font, text=acceptance.PROMPT):
    rgb = frame()
    for index, character in enumerate(text):
        glyph = font[character * 16:character * 16 + 8]
        for gy in range(8):
            for gx in range(8):
                if glyph[gy] & (0x80 >> gx):
                    sx, sy = index * 8 + gx, 24 * 8 + gy
                    x = round((160 + (sx + 0.5) * 1.5) * WIDTH / 1280)
                    y = round((sy + 0.5) * HEIGHT / 200)
                    offset = (y * WIDTH + x) * 3
                    rgb[offset:offset + 3] = b'\xff\xff\xff'
    return bytes(rgb)


# Actual grayscale RGB samples from the A:\> region of Kit B capture
# boot-003.png, SHA-256 bb30b0093d9e3e086683a7e5491c7f3c5abf04ae16d6c1139a6b97d9cd124183.
# The 32 x 8 pixels are the source-glyph center samples at row 21, column 0
# in the 1920 x 1080 capture. Keep the captured variation, including dark
# edge pixels, so this regression is independent of the renderer's RGB.
KIT_B_PROMPT_SAMPLES = (
    b'c-nnVK@NZ*4C@!*uzzceeqqT>N_e3P12iG_)U91Rz!nS$>1=bdtETXfbFKU21}pRJ_nVf}^T_blJIFY?sAl@rCDjV%ILmLp)p@ET14juF{6j91wJLT'
)


def kit_b_prompt_samples(*, omit_last_glyph=False, low_contrast=False):
    width, height = 1920, 1080
    samples = zlib.decompress(base64.b85decode(KIT_B_PROMPT_SAMPLES))
    if len(samples) != 32 * 8:
        raise AssertionError('captured sample fixture size changed')
    rgb = bytearray(width * height * 3)
    for gy in range(8):
        for sx in range(32):
            value = samples[gy * 32 + sx]
            if omit_last_glyph and sx >= 24:
                value = 0
            if low_contrast:
                value //= 4
            x = round((160 + (sx + 0.5) * 1.5) * width / 1280)
            y = round((21 * 8 + gy + 0.5) * height / 200)
            offset = (y * width + x) * 3
            rgb[offset:offset + 3] = bytes((value, value, value))
    return width, height, bytes(rgb)


class PCXTDosHILAcceptanceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.font = acceptance.load_font()

    def test_changing_boot_frames_and_final_prompt_pass(self):
        blank = bytes(frame())
        progress = bytes(frame((48, 48, 48)))
        prompt = prompt_frame(self.font)
        result = acceptance.check_rgb_frames([(WIDTH, HEIGHT, blank),
                                             (WIDTH, HEIGHT, progress),
                                             (WIDTH, HEIGHT, prompt)], self.font)
        self.assertEqual(result['classification'], 'PC/XT FreeDOS prompt capture diagnostic')
        self.assertTrue(result['boot_frames_changed'])
        self.assertEqual(result['prompt']['text'], r'A:\>')
        self.assertEqual(result['prompt']['row'], 24)
        self.assertEqual(result['prompt']['column'], 0)

    def test_kit_b_prompt_color_variation_matches_locked_glyphs(self):
        width, height, captured = kit_b_prompt_samples()
        result = acceptance.find_prompt(width, height, captured, self.font)
        self.assertEqual(result['text'], r'A:\>')
        self.assertEqual((result['row'], result['column']), (21, 0))
        self.assertEqual(result['foreground_rgb'], [175, 175, 175])

    def test_kit_b_prompt_missing_glyph_is_rejected(self):
        width, height, captured = kit_b_prompt_samples(omit_last_glyph=True)
        with self.assertRaisesRegex(ValueError, 'does not contain'):
            acceptance.find_prompt(width, height, captured, self.font)

    def test_kit_b_prompt_without_contrast_is_rejected(self):
        width, height, captured = kit_b_prompt_samples(low_contrast=True)
        with self.assertRaisesRegex(ValueError, 'does not contain'):
            acceptance.find_prompt(width, height, captured, self.font)

    def test_near_miss_printable_glyphs_are_rejected(self):
        # Both incorrect glyphs differ from the expected locked glyphs by
        # only two pixels. The former aggregate score accepted this frame.
        for text in (b'A;\\)', b'A;\\>', b'A:\\)'):
            with self.subTest(text=text):
                with self.assertRaisesRegex(ValueError, 'does not contain'):
                    acceptance.find_prompt(WIDTH, HEIGHT, prompt_frame(self.font, text), self.font)

    def test_static_signature_is_rejected_even_if_prompt_text_is_visible(self):
        prompt = prompt_frame(self.font)
        with self.assertRaisesRegex(ValueError, 'meaningful screen change'):
            acceptance.check_rgb_frames([(WIDTH, HEIGHT, prompt)] * 3, self.font)

    def test_missing_prompt_rejects_after_a_real_transition(self):
        blank = bytes(frame())
        progress = bytes(frame((48, 48, 48)))
        with self.assertRaisesRegex(ValueError, 'does not contain'):
            acceptance.check_rgb_frames([(WIDTH, HEIGHT, blank),
                                         (WIDTH, HEIGHT, progress),
                                         (WIDTH, HEIGHT, blank)], self.font)

    def test_requires_multiple_complete_frames(self):
        with self.assertRaisesRegex(ValueError, 'at least three'):
            acceptance.check_rgb_frames([(WIDTH, HEIGHT, bytes(frame()))] * 2, self.font)


if __name__ == '__main__':
    unittest.main()
