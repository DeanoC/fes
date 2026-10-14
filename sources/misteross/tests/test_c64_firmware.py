"""Original diagnostic variants; hardware qualification remains separate."""
import importlib.util
import unittest
from pathlib import Path

PATH = Path(__file__).resolve().parents[1] / "cores/fes-c64/diagnostic/firmware.py"
SPEC = importlib.util.spec_from_file_location("c64_firmware", PATH)
firmware = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(firmware)


class C64FirmwareTests(unittest.TestCase):
    def test_both_variants_have_exact_windows_and_vectors(self):
        full = firmware.build()
        vacant = firmware.build(without_cartridges=True)
        for image in (full, vacant):
            self.assertEqual(len(image), 20480)
            self.assertEqual(image[:8192], firmware.BASIC)
            self.assertEqual(image[16380:16382], b"\x00\xe0")
        self.assertNotEqual(full, vacant)
        # Absolute cartridge reads vanish only in the explicitly vacant variant.
        self.assertIn(b"\xad\x00\x80", full[8192:16384])
        self.assertNotIn(b"\xad\x00\x80", vacant[8192:16384])
        self.assertIn(b"\xad\x01\xde", full[8192:16384])
        self.assertNotIn(b"\xad\x01\xde", vacant[8192:16384])

    def test_success_is_pass_text_in_the_picture_not_a_green_border(self):
        # Screen codes for "PASS " eight times: one 40-column row.
        pass_row = bytes([16, 1, 19, 19, 32]) * 8
        green_border = bytes([0xA9, 0x05, 0x8D, 0x20, 0xD0])
        black_border = bytes([0xA9, 0x00, 0x8D, 0x20, 0xD0])
        for image in (firmware.build(), firmware.build(without_cartridges=True)):
            kernal = image[8192:16384]
            self.assertNotIn(green_border, kernal)
            self.assertIn(black_border, kernal)
            self.assertIn(pass_row, kernal)
            # The stage digit is written at $0408. Clearing only eight cells
            # leaves that 8 on a pass picture.
            self.assertIn(bytes([0xE0, 0x09]), kernal)


if __name__ == "__main__":
    unittest.main()
