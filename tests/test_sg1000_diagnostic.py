from __future__ import annotations

import hashlib
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


GENERATOR = Path(__file__).resolve().parents[1] / "cores/fes-sg1000/diagnostic/generate.py"


class SG1000DiagnosticTests(unittest.TestCase):
    def generate(self, output: Path, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(GENERATOR), "--output", str(output), *args],
            capture_output=True, text=True, check=False,
        )

    def test_controller_diagnostic_renders_active_low_dc_and_dd(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "controller.rom"
            preview = Path(directory) / "controller.ppm"
            matrix = "0xffffffffff"
            result = self.generate(output, "--controllers", "--matrix", matrix,
                                   "--preview", str(preview))
            self.assertEqual(result.returncode, 0, result.stderr)
            raw = output.read_bytes()
            self.assertLess(len(raw), 16384)
            self.assertIn(b"\xdb\xdc", raw)
            self.assertIn(b"\xdb\xdd", raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(),
                             "2491ddeea7f768b5ed0febf47233cc58776cbc348f0eb8a2ef3b9e1f32e8ecd8")

            pixels = preview.read_bytes().split(b"\n", 3)[3]
            for port_row, value in ((4, 0xff), (8, 0xff)):
                for bit in range(8):
                    x = 385 + (4 + 3 * bit) * 16 + 8
                    y = 168 + port_row * 16 + 8
                    offset = (y * 1280 + x) * 3
                    self.assertEqual(pixels[offset:offset + 3], b"\x00\xff\x40",
                                     (port_row, bit, value))

            result = self.generate(output, "--controllers", "--matrix", "0xffffffffff",
                                   "--pad-to", "16384")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(len(output.read_bytes()), 16384)

    def test_controller_preview_maps_keyboard_matrix_to_sg_ports(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "controller.rom"
            preview = Path(directory) / "controller.ppm"
            # P1 Up clears DC bit 0; P2 Fire 1 clears DD bit 1.
            matrix = hex(0xffffffffff ^ (1 << 0) ^ (1 << 9))
            result = self.generate(output, "--controllers", "--matrix", matrix,
                                   "--preview", str(preview))
            self.assertEqual(result.returncode, 0, result.stderr)
            pixels = preview.read_bytes().split(b"\n", 3)[3]
            for port_row, bit, expected in (
                (4, 0, b"\xff\x40\x00"),
                (8, 1, b"\xff\x40\x00"),
                (4, 1, b"\x00\xff\x40"),
                (8, 0, b"\x00\xff\x40"),
            ):
                x = 385 + (4 + 3 * bit) * 16 + 8
                y = 168 + port_row * 16 + 8
                offset = (y * 1280 + x) * 3
                self.assertEqual(pixels[offset:offset + 3], expected,
                                 (port_row, bit))

    def test_controller_options_reject_invalid_matrix(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "controller.rom"
            for value in ("-1", "0x10000000000", "bogus"):
                result = self.generate(output, "--controllers", "--matrix", value)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
