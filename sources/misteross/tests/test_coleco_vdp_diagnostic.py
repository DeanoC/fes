import collections
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


GENERATOR = Path(__file__).resolve().parents[1] / "cores/fes-coleco/diagnostic/vdp_io.py"


class ColecoVDPDiagnosticTests(unittest.TestCase):
    def test_pass_preview_and_output_collision(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "vdp.rom"
            preview = Path(directory) / "pass.ppm"
            command = [sys.executable, str(GENERATOR), "--output", str(output), "--preview"]
            result = subprocess.run(command + [str(preview)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            header, size, maximum, pixels = preview.read_bytes().split(b"\n", 3)
            self.assertEqual((header, size, maximum), (b"P6", b"1280 720", b"255"))
            self.assertEqual(len(pixels), 1280*720*3)
            colors = collections.Counter(zip(pixels[0::3], pixels[1::3], pixels[2::3]))
            self.assertEqual(colors, {(33, 200, 66): 27648, (0, 0, 0): 893952})
            original = output.read_bytes()
            result = subprocess.run(command + [str(output)], capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(output.read_bytes(), original)

    def test_reproducible_raw_image_and_full_aperture(self):
        # The real CPU simulation supplies semantic coverage; this catches
        # nondeterministic output, padding damage, and an unusable raw image.
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "nested/vdp-io.rom"
            command = [sys.executable, str(GENERATOR), "--output", str(output)]
            result = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            raw = output.read_bytes()
            self.assertGreater(len(raw), 0x66)
            self.assertLess(len(raw), 16384)
            self.assertEqual(len(raw) % 2, 1)
            subprocess.run(command, check=True, capture_output=True)
            self.assertEqual(output.read_bytes(), raw)
            subprocess.run(command + ["--pad-to", "16384"], check=True, capture_output=True)
            self.assertEqual(output.read_bytes(), raw + b"\xff" * (16384 - len(raw)))

    def test_invalid_padding_leaves_no_output(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "vdp.rom"
            for size in ("1", "16385"):
                result = subprocess.run([sys.executable, str(GENERATOR), "--output", str(output),
                                         "--pad-to", size], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(output.exists())
