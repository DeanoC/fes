import unittest

from scripts import pcxt_dos_hil_acceptance as acceptance


WIDTH, HEIGHT = 1280, 720


def frame(color=(0, 0, 0)):
    return bytearray(color * (WIDTH * HEIGHT))


def prompt_frame(font):
    rgb = frame()
    for index, character in enumerate(acceptance.PROMPT):
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
        self.assertTrue(result['boot_frames_changed'])
        self.assertEqual(result['prompt']['text'], r'A:\>')
        self.assertEqual(result['prompt']['row'], 24)
        self.assertEqual(result['prompt']['column'], 0)

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
