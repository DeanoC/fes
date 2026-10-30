"""BIOS profile inputs fail closed before compiling or running the machine."""
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import sim_fes_pcxt_bios as bios


class BiosInputsTest(unittest.TestCase):
    def test_top_aligned_rom_and_unpopulated_window(self):
        for size in (8192, 16384, 32768, 65536):
            rom = bytes(i & 255 for i in range(size))
            window = bios.firmware_window(rom)
            self.assertEqual(len(window), 65536)
            self.assertEqual(window[-size:], rom)
            self.assertEqual(window[:65536-size], b'\xff'*(65536-size))
        for size in (0, 8191, 8193, 65537):
            with self.assertRaises(ValueError):
                bios.firmware_window(bytes(size))

    def test_explicit_rom_font_and_mutated_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            rom = bytes(i & 255 for i in range(8192))
            path = work / 'rom.bin'
            path.write_bytes(rom)
            spec = {'bios': {'filename': path.name, 'size': len(rom),
                             'sha256': hashlib.sha256(rom).hexdigest()},
                    'ascii_font': {'rom_offset': 0}}
            lock = work / 'bios.lock'
            lock.write_text(json.dumps(spec))
            with patch.object(bios, 'LOCK', lock):
                bios.fixture_inputs(work, path)
                restored = bytes.fromhex((work / 'font.hex').read_text())
                self.assertEqual(restored[:8], rom[:8])
                self.assertEqual(restored[8:16], bytes(8))
                self.assertEqual(restored[2048:], bytes(2048))
                font = work / 'external-font.bin'
                font.write_bytes(bytes([0x42])*4096)
                bios.fixture_inputs(work, path, font)
                self.assertEqual(bytes.fromhex((work / 'font.hex').read_text()), font.read_bytes())
                font.write_bytes(bytes(4095))
                with self.assertRaisesRegex(ValueError, 'Font'):
                    bios.fixture_inputs(work, path, font)
                path.write_bytes(bytes(8192))
                with self.assertRaisesRegex(ValueError, 'differs'):
                    bios.fixture_inputs(work, path)
                path.unlink()
                with patch.object(bios.urllib.request, 'urlopen') as network:
                    with self.assertRaisesRegex(ValueError, 'missing'):
                        bios.fixture_inputs(work)
                    network.assert_not_called()


class BiosFrameTest(unittest.TestCase):
    def test_text_and_font_must_reach_video_output(self):
        cells = bytearray(80 * 25 * 2)
        font = bytearray(4096)
        label = b'GLaBIOS'
        for n, char in enumerate(label):
            cells[2 * (80 + n)] = char
            cells[2 * (80 + n) + 1] = 0x0f
            for y in range(8):
                font[char * 16 + y] = (0x81 >> (y % 4)) | (0x81 << (y % 4)) & 255
        frame = bytearray(1280 * 720 * 3)
        for y in range(720):
            sy = y * 200 // 720
            row, gy = divmod(sy, 8)
            if row != 1:
                continue
            for x in range(160, 160 + 84):
                sx = (x - 160) * 640 // 960
                col, gx = divmod(sx, 8)
                if col >= len(label):
                    continue
                if font[label[col] * 16 + gy] & (0x80 >> gx):
                    at = (y * 1280 + x) * 3
                    frame[at:at+3] = b'\xff\xff\xff'
        proof = bios.verify_post_frame(bytes(frame), bytes(cells), bytes(font))
        self.assertGreaterEqual(proof['score'], 0.9)
        # A volume label and a directory entry may contain the same text;
        # the caller must choose the actual listing row explicitly.
        duplicate = bytearray(cells)
        for n, char in enumerate(label):
            duplicate[2 * (160 + n)] = char
            duplicate[2 * (160 + n) + 1] = 0x0f
        with self.assertRaisesRegex(ValueError, 'exactly one line'):
            bios.verify_post_frame(bytes(frame), bytes(duplicate), bytes(font))
        selected = bios.verify_post_frame(bytes(frame), bytes(duplicate), bytes(font), label, 1)
        self.assertGreaterEqual(selected['score'], 0.9)
        for blank in (bytes(len(frame)), bytes([255]) * len(frame)):
            with self.assertRaisesRegex(ValueError, 'does not render'):
                bios.verify_post_frame(blank, bytes(cells), bytes(font))
        with self.assertRaisesRegex(ValueError, 'font lacks'):
            bios.verify_post_frame(bytes(frame), bytes(cells), bytes(4096))
