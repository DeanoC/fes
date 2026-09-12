from __future__ import annotations

import collections
import hashlib
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

    def test_interactive_preview_tracks_independent_active_low_player_rows(self) -> None:
        # Catches swapped players, inverted polarity, and crossed indicator bits.
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "input.rom"
            preview = Path(directory) / "input.ppm"
            for row0, row1 in ((31, 31), (30, 29), (21, 10), (0, 0)):
                result = self.generate(output, "--interactive", "--preview", str(preview),
                                       "--row0", str(row0), "--row1", str(row1))
                self.assertEqual(result.returncode, 0, result.stderr)
                pixels = preview.read_bytes().split(b"\n", 3)[3]
                for player, bits in enumerate((row0, row1)):
                    for bit in range(5):
                        x, y = 385 + (3 + 6 * bit) * 16, 168 + (6 + 10 * player) * 16
                        offset = (y * 1280 + x) * 3
                        self.assertEqual(pixels[offset:offset + 3],
                                         b"\xff\x40\x00" if bits & (1 << bit) else b"\x00\xff\x40")
                offset = (400 * 1280 + 640) * 3  # gap between players
                self.assertEqual(pixels[offset:offset + 3], b"\x00\x00\x00")
            raw = output.read_bytes()
            self.assertEqual(len(raw) % 2, 1)
            result = self.generate(output, "--interactive", "--pad-to", "16384")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_bytes(), raw + b"\xff" * (16384 - len(raw)))
            result = self.generate(output)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(hashlib.sha256(output.read_bytes()).hexdigest(),
                             "9f9fa280b141e0538a571bb66f1e2447f691eecb853ae05547720f2ccc20783c")

    def test_invalid_interactive_rows_do_not_write_outputs(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "input.rom"
            for option, value in (("--row0", "-1"), ("--row1", "32")):
                result = self.generate(output, "--interactive", option, value)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(output.exists())
