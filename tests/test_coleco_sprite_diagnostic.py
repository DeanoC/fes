from __future__ import annotations

import collections
import hashlib
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


GENERATOR = Path(__file__).resolve().parents[1] / "cores/fes-coleco/diagnostic/sprite_io.py"


class ColecoSpriteDiagnosticTests(unittest.TestCase):
    def generate(self, output: Path, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(GENERATOR), "--output", str(output), *args],
            capture_output=True,
            text=True,
            check=False,
        )

    def test_reproducible_raw_image_and_full_aperture_padding(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "nested" / "sprites.rom"
            result = self.generate(output)
            self.assertEqual(result.returncode, 0, result.stderr)
            raw = output.read_bytes()
            self.assertGreater(len(raw), 0x66)
            self.assertLess(len(raw), 16384)
            self.assertEqual(len(raw) % 2, 1)
            self.assertEqual(len(raw), 1223)
            self.assertEqual(
                hashlib.sha256(raw).hexdigest(),
                "b3aa3558e702272cdbd019d5f4553cc5e885e754ac7c29648137f2b6ce6a831c",
            )
            self.assertEqual(self.generate(output).returncode, 0)
            self.assertEqual(output.read_bytes(), raw)
            result = self.generate(output, "--pad-to", "16384")
            self.assertEqual(result.returncode, 0, result.stderr)
            padded = output.read_bytes()
            self.assertEqual(padded, raw + b"\xff" * (16384 - len(raw)))
            self.assertEqual(
                hashlib.sha256(padded).hexdigest(),
                "5bb58354ff5c49100aae1769270fe03d32524816464dcc99ee619cd09e5a054d",
            )

    def test_preview_contains_status_sprite_geometry_and_palette(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "sprites.rom"
            preview = Path(directory) / "sprites.ppm"
            result = self.generate(output, "--preview", str(preview))
            self.assertEqual(result.returncode, 0, result.stderr)
            magic, size, maximum, pixels = preview.read_bytes().split(b"\n", 3)
            self.assertEqual((magic, size, maximum), (b"P6", b"1280 720", b"255"))
            self.assertEqual(len(pixels), 1280 * 720 * 3)
            colors = collections.Counter(zip(pixels[0::3], pixels[1::3], pixels[2::3]))
            self.assertEqual(set(colors), {(0, 0, 0), (0, 255, 64), (255, 64, 0)})

            def sample(logical_x: int, logical_y: int) -> bytes:
                x = 385 + logical_x * 2
                y = 168 + logical_y * 2
                offset = (y * 1280 + x) * 3
                return pixels[offset:offset + 3]

            self.assertEqual(sample(0, 0), b"\x00\xff\x40")
            self.assertEqual(sample(188, 81), b"\xff\x40\x00")
            self.assertEqual(sample(189, 81), b"\xff\x40\x00")
            self.assertEqual(sample(188, 82), b"\xff\x40\x00")
            self.assertEqual(sample(255, 101), b"\xff\x40\x00")
            self.assertEqual(sample(10, 131), b"\x00\xff\x40")
            self.assertEqual(sample(11, 131), b"\x00\xff\x40")
            self.assertEqual(sample(240, 101), b"\x00\x00\x00")

    def test_invalid_padding_leaves_no_output(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "sprites.rom"
            for size in ("1", "16385"):
                result = self.generate(output, "--pad-to", size)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
