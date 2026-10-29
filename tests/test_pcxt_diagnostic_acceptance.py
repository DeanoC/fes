"""Accept the ROM's intended static output and reject incomplete boots."""
import unittest
from scripts import pcxt_diagnostic_acceptance as acceptance


def picture(marker=True):
    ordinary = bytes(v for x in range(1280) for v in
                     ((255 if 160 <= x < 1120 and
                       (((x-160)*2//3) % 2 == (0 if ((x-160)*2//3) % 16 < 8 else 1)) else 0),) * 3)
    rows = []
    for y in range(720):
        row = bytearray(ordinary)
        if marker and y * 200 // 720 < 8:
            row[160*3:184*3] = b'\xff' * (24*3)
        rows.append(row)
    return bytearray().join(rows)


class PCXTDiagnosticAcceptanceTests(unittest.TestCase):
    def test_completed_static_frame_passes_repeatedly(self):
        rgb = picture()
        result = acceptance.check_rgb(1280, 720, rgb)
        self.assertTrue(result['completion_signature_verified'])
        self.assertTrue(result['static_picture_expected'])
        self.assertEqual(result['mean_pixel_error'], 0)

    def test_old_stripes_are_evidence_but_not_completed_diagnostic(self):
        rgb = picture(marker=False)
        with self.assertRaisesRegex(ValueError, 'completion block'):
            acceptance.check_rgb(1280, 720, rgb)
        result = acceptance.check_rgb(1280, 720, rgb, historical_pattern=True)
        self.assertFalse(result['completion_signature_verified'])

    def test_black_white_wrong_raster_and_partial_frame_reject(self):
        for rgb in (b'\0' * (1280*720*3), b'\xff' * (1280*720*3),
                    picture()[:1280*360*3] + b'\0' * (1280*360*3)):
            with self.subTest(kind=rgb[:8]), self.assertRaises(ValueError):
                acceptance.check_rgb(1280, 720, rgb)
        with self.assertRaisesRegex(ValueError, 'complete 16:9'):
            acceptance.check_rgb(1280, 719, picture())

    def test_wrong_stripe_phase_rejects(self):
        rgb = picture()
        shifted = bytearray().join(rgb[y*1280*3+3:(y+1)*1280*3] + b'\0\0\0'
                                   for y in range(720))
        with self.assertRaisesRegex(ValueError, 'geometry/pixels'):
            acceptance.check_rgb(1280, 720, shifted)


if __name__ == '__main__':
    unittest.main()
