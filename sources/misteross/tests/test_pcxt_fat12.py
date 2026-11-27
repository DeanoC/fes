"""Tests for the pure-Python 360 KiB FAT12 floppy builder and PNG helper."""
from pathlib import Path
import struct
import sys
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import build_pcxt_freedos_image as freedos
import pcxt_fat12 as fat12


class Fat12BuilderTest(unittest.TestCase):
    def test_inject_replace_and_subdirectory_files_preserve_freedos_boot(self):
        base = fat12.checked_base(ROOT)
        payload_com = b'\xb8\x00\x4c\xcd\x21' * 400  # multi-cluster
        payload_sub = b'HERCULES TEST DATA\r\n' * 150
        autoexec = b'@ECHO OFF\r\nMDATEST.COM\r\n'
        updated = fat12.inject_files(base, {
            'AUTOEXEC.BAT': autoexec,
            'MDATEST.COM': payload_com,
            'GAMES/INFO.TXT': payload_sub,
        })
        self.assertEqual(len(updated), fat12.SIZE)
        # Boot sector, FAT copies, and base FreeDOS files remain intact.
        self.assertEqual(updated[:512], base[:512])
        self.assertEqual(updated[512:1536], updated[1536:2560])
        self.assertEqual(fat12.read_file(updated, 'FESXT.TXT'), b'FES PC/XT DOS boot\r\n')
        self.assertEqual(fat12.read_file(updated, 'AUTOEXEC.BAT'), autoexec)
        self.assertEqual(fat12.read_file(updated, 'MDATEST.COM'), payload_com)
        self.assertEqual(fat12.read_file(updated, 'GAMES/INFO.TXT'), payload_sub)
        self.assertEqual(len(fat12.read_file(updated, 'KERNEL.SYS')), len(freedos.read_fat12_file(base, *freedos.read_root(base)[0][1:])[0]))

    def test_rejects_bad_83_names_and_overflow(self):
        base = fat12.checked_base(ROOT)
        with self.assertRaises(ValueError):
            fat12.format_83('toolongname.com')
        with self.assertRaises(ValueError):
            fat12.inject_files(base, {'HUGE.BIN': bytes(350000)})

    def test_write_png_and_ppm_to_png_roundtrip(self):
        rgb = bytes([255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255])
        with tempfile.TemporaryDirectory() as d:
            ppm = Path(d) / 'test.ppm'
            png = Path(d) / 'test.png'
            ppm.write_bytes(b'P6\n2 2\n255\n' + rgb)
            w, h, out_rgb = fat12.ppm_to_png(ppm, png)
            self.assertEqual((w, h, out_rgb), (2, 2, rgb))
            data = png.read_bytes()
            self.assertTrue(data.startswith(b'\x89PNG\r\n\x1a\n'))
            idat_len = struct.unpack('>I', data[33:37])[0]
            self.assertEqual(data[37:41], b'IDAT')
            raw = zlib.decompress(data[41:41 + idat_len])
            self.assertEqual(raw, b'\x00' + rgb[:6] + b'\x00' + rgb[6:])


if __name__ == '__main__':
    unittest.main()
