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
            self.assertEqual(len(raw), 1245)
            self.assertEqual(
                hashlib.sha256(raw).hexdigest(),
                "8504dcd0c36d6936c9475183b3bcc9a030b3d6d1fa02fbd8f2a2b88d47bd9d91",
            )
            self.assertEqual(self.generate(output).returncode, 0)
            self.assertEqual(output.read_bytes(), raw)
            result = self.generate(output, "--pad-to", "16384")
            self.assertEqual(result.returncode, 0, result.stderr)
            padded = output.read_bytes()
            self.assertEqual(padded, raw + b"\xff" * (16384 - len(raw)))
            self.assertEqual(
                hashlib.sha256(padded).hexdigest(),
                "0ec9b48bfa04d48e1db6cd795f5c75b10cab2ad35c4e407ceff3f8d6692b112b",
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
            self.assertEqual(set(colors), {(0, 0, 0), (33, 200, 66), (212, 82, 77)})

            def sample(logical_x: int, logical_y: int) -> bytes:
                x = 385 + logical_x * 2
                y = 168 + logical_y * 2
                offset = (y * 1280 + x) * 3
                return pixels[offset:offset + 3]

            self.assertEqual(sample(0, 0), b"\x21\xc8\x42")
            self.assertEqual(sample(188, 81), b"\xd4\x52\x4d")
            self.assertEqual(sample(189, 81), b"\xd4\x52\x4d")
            self.assertEqual(sample(188, 82), b"\xd4\x52\x4d")
            self.assertEqual(sample(255, 101), b"\xd4\x52\x4d")
            self.assertEqual(sample(10, 131), b"\x21\xc8\x42")
            self.assertEqual(sample(11, 131), b"\x21\xc8\x42")
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
