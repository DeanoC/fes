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
