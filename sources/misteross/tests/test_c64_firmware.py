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
            self.assertEqual(len(image), 16384)
            self.assertEqual(image[:8192], firmware.BASIC)
            self.assertEqual(image[-4:-2], b"\x00\xe0")
        self.assertNotEqual(full, vacant)
        # Absolute cartridge reads vanish only in the explicitly vacant variant.
        self.assertIn(b"\xad\x00\x80", full[8192:])
        self.assertNotIn(b"\xad\x00\x80", vacant[8192:])
        self.assertIn(b"\xad\x01\xde", full[8192:])
        self.assertNotIn(b"\xad\x01\xde", vacant[8192:])


if __name__ == "__main__":
    unittest.main()
