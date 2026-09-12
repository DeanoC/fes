from __future__ import annotations

import collections
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


GENERATOR = Path(__file__).resolve().parents[1] / "cores/fes-coleco/diagnostic/generate.py"


class ColecoDiagnosticTests(unittest.TestCase):
    def generate(self, output: Path, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(GENERATOR), "--output", str(output), *args],
            capture_output=True, text=True, check=False,
        )

    def test_reproducible_raw_cartridge_and_full_aperture_padding(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "nested" / "graphics-i.rom"
            result = self.generate(output)
            self.assertEqual(result.returncode, 0, result.stderr)
            raw = output.read_bytes()
            self.assertGreater(len(raw), 0)
            self.assertLess(len(raw), 16384)
            self.assertEqual(len(raw) % 2, 1, "compact image exercises GP odd tail")
            result = self.generate(output)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_bytes(), raw)
            result = self.generate(output, "--pad-to", "16384")
            self.assertEqual(result.returncode, 0, result.stderr)
            padded = output.read_bytes()
            self.assertEqual(len(padded), 16384)
            self.assertEqual(padded[:len(raw)], raw)
            self.assertEqual(padded[len(raw):], b"\xff" * (16384 - len(raw)))

    def test_reference_image_has_documented_palette_and_pixel_counts(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "graphics-i.rom"
            preview = Path(directory) / "graphics-i.ppm"
            result = self.generate(output, "--preview", str(preview))
            self.assertEqual(result.returncode, 0, result.stderr)
            magic, size, maximum, pixels = preview.read_bytes().split(b"\n", 3)
            self.assertEqual((magic, size, maximum), (b"P6", b"1280 720", b"255"))
            self.assertEqual(len(pixels), 1280 * 720 * 3)
            colors = collections.Counter(zip(pixels[0::3], pixels[1::3], pixels[2::3]))
            self.assertEqual(colors, {
                (0, 0, 0): 798912,
                (0, 255, 64): 75168,
                (255, 64, 0): 47520,
            })

    def test_invalid_size_or_shared_output_is_rejected_before_writing(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "graphics-i.rom"
            for args in (("--pad-to", "1"), ("--pad-to", "16385"),
                         ("--preview", str(output))):
                with self.subTest(args=args):
                    result = self.generate(output, *args)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse(output.exists())
