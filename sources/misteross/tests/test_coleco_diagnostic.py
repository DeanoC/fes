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
    def test_native_controller_preview_matches_cpu_bus_oracle(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output, image = Path(directory) / "controller.rom", Path(directory) / "native.ppm"
            legacy = Path(directory) / "oracle.ppm"
            common = [sys.executable, str(GENERATOR), "--controllers", "--output", str(output)]
            subprocess.run(common + ["--buttons", "0x11", "0x28", "--keypads", "1", "0x800", "--preview", str(image)], check=True)
            raw = output.read_bytes()
            matrix = ((1 << 40) - 1) ^ (1 << 0) ^ (1 << 4) ^ (1 << 6) ^ (1 << 11) ^ (1 << 12) ^ (1 << 35)
            subprocess.run(common + ["--matrix", hex(matrix), "--preview", str(legacy)], check=True)
            self.assertEqual(image.read_bytes(), legacy.read_bytes())
            self.assertEqual(output.read_bytes(), raw)

    def test_controller_preview_raw_bus_and_preview_only_matrix(self) -> None:
        # Literal bus fixtures catch mode/fire selection, keypad encoding,
        # player crossing, priority, and accidental use of the unused bits.
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "controller.rom"
            preview = Path(directory) / "controller.ppm"
            neutral = (1 << 40) - 1
            fixtures = [(neutral, (0x7f, 0x7f, 0x7f, 0x7f)),
                        (neutral ^ (1 << 4) ^ (1 << 11) ^ (1 << 12) ^ (1 << 35),
                         (0x3f, 0x7a, 0x7f, 0x36)),
                        (neutral ^ (1 << 13) ^ (1 << 21), (0x7f, 0x7d, 0x7f, 0x7f)),
                        ((1 << 36) - 1, (0x7f, 0x7f, 0x7f, 0x7f)),
                        (0, (0x30, 0x3a, 0x30, 0x3a))]
            raw = None
            for matrix, banks in fixtures:
                result = self.generate(output, "--controllers", "--matrix", hex(matrix),
                                       "--preview", str(preview))
                self.assertEqual(result.returncode, 0, result.stderr)
                if raw is None:
                    raw = output.read_bytes()
                self.assertEqual(output.read_bytes(), raw)
                pixels = preview.read_bytes().split(b"\n", 3)[3]
                for row in range(24):
                    for col in range(32):
                        expected = b"\x00\x00\x00"
                        if row in (0, 23) or col in (0, 31):
                            expected = b"\x00\xff\x40"
                        for bank, value in enumerate(banks):
                            for bit in range(8):
                                if 3 + 5 * bank <= row <= 4 + 5 * bank and 4 + 3 * bit <= col <= 5 + 3 * bit:
                                    expected = b"\xff\x40\x00" if value & (1 << bit) else b"\x00\xff\x40"
                        offset = ((168 + row * 16 + 8) * 1280 + 385 + col * 16 + 8) * 3
                        self.assertEqual(pixels[offset:offset + 3], expected, (matrix, row, col))
            result = self.generate(output, "--controllers", "--pad-to", "16384")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(output.read_bytes(), raw + b"\xff" * (16384 - len(raw)))

    def test_controller_matrix_formats_and_invalid_options(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "controller.rom"
            for value in ("FFFFFFFFFF", "0xffffffffff", "1099511627775"):
                result = self.generate(output, "--controllers", "--matrix", value)
                self.assertEqual(result.returncode, 0, result.stderr)
            absent = Path(directory) / "invalid.rom"
            for args in (("--controllers", "--matrix", "-1"),
                         ("--controllers", "--matrix", "0x10000000000"),
                         ("--controllers", "--matrix", "bogus"),
                         ("--controllers", "--interactive"),
                         ("--controllers", "--row0", "0"),
                         ("--matrix", "0")):
                with self.subTest(args=args):
                    result = self.generate(absent, *args)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertFalse(absent.exists())

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
