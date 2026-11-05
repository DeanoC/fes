"""Verify the floppy marker without requiring kit capture artifacts."""
import unittest

from scripts import pcxt_floppy_diagnostic_acceptance as acceptance
from tests.test_pcxt_diagnostic_acceptance import picture


WIDTH, HEIGHT = 1280, 720
# Interior of native CGA x=64..127, y=0..31 after the standard 720p scaling.
X0, X1 = 257, 350
Y0, Y1 = 3, 111


def fill(rgb, x0, y0, x1, y1, value):
    for y in range(y0, y1):
        start = (y * WIDTH + x0) * 3
        rgb[start:start + (x1 - x0) * 3] = bytes([value]) * ((x1 - x0) * 3)


class PCXTFloppyDiagnosticAcceptanceTests(unittest.TestCase):
    def test_empty_wrong_matching_and_ejected_frames(self):
        empty = picture()
        self.assertFalse(acceptance.inspect_rgb(WIDTH, HEIGHT, empty, marker=False)['marker_expected'])
        wrong = picture()
        self.assertTrue(acceptance.inspect_rgb(WIDTH, HEIGHT, wrong, marker=False, reference=empty)['compared_with_baseline'])
        matching = picture()
        fill(matching, X0, Y0, X1, Y1, 255)
        result = acceptance.inspect_rgb(WIDTH, HEIGHT, matching, marker=True)
        self.assertTrue(result['marker_expected'])
        self.assertEqual(result['marker_mean_error'], 0)
        ejected = picture()
        self.assertTrue(acceptance.inspect_rgb(WIDTH, HEIGHT, ejected, marker=False, reference=empty)['compared_with_baseline'])

    def test_matching_rejects_empty_partial_rows_and_partial_columns(self):
        with self.assertRaisesRegex(ValueError, 'matching disk marker mismatch'):
            acceptance.inspect_rgb(WIDTH, HEIGHT, picture(), marker=True)
        for half in ('row', 'column'):
            rgb = picture()
            fill(rgb, X0, Y0, X1, Y1, 255)
            if half == 'row':
                fill(rgb, X0, (Y0 + Y1) // 2, X1, (Y0 + Y1) // 2 + 1, 0)
            else:
                fill(rgb, (X0 + X1) // 2, Y0, (X0 + X1) // 2 + 1, Y1, 0)
            with self.subTest(missing=half), self.assertRaisesRegex(ValueError, 'matching disk marker mismatch'):
                acceptance.inspect_rgb(WIDTH, HEIGHT, rgb, marker=True)

    def test_wrong_or_ejected_frame_must_restore_the_baseline(self):
        baseline = picture()
        marker = picture()
        fill(marker, X0, Y0, X1, Y1, 255)
        for stage in ('wrong', 'ejected'):
            with self.subTest(stage=stage), self.assertRaisesRegex(ValueError, 'empty/wrong/ejected disk marker mismatch'):
                acceptance.inspect_rgb(WIDTH, HEIGHT, marker, marker=False, reference=baseline)
        with self.assertRaisesRegex(ValueError, 'baseline frame dimensions differ'):
            acceptance.inspect_rgb(WIDTH, HEIGHT, baseline, marker=False, reference=baseline[:-3])

    def test_baseline_cannot_already_contain_success(self):
        marker = picture()
        fill(marker, X0, Y0, X1, Y1, 255)
        with self.assertRaisesRegex(ValueError, 'baseline marker region'):
            acceptance.inspect_rgb(WIDTH, HEIGHT, marker, marker=False)


if __name__ == '__main__':
    unittest.main()
