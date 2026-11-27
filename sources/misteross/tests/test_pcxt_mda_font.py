"""The PC/XT MDA/Hercules 8x14 character ROM covers all 256 CP437 characters."""
import hashlib
import json
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts import sim_fes_pcxt as sim

CORE = ROOT / 'cores/fes-pcxt'
VGAFONTS = ROOT / 'cores/fes-pc386/firmware/vgabios/vgafonts.h'


def glyph(font, code):
    return font[code * 16:code * 16 + 16]


class MdaFontTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.font = sim.mda_font()

    def test_matches_vgafont14_table(self):
        text = VGAFONTS.read_text()
        match = re.search(r'vgafont14\[256\*14\]=\s*\{(.*?)\};', text, re.S)
        self.assertIsNotNone(match)
        expected = bytes(int(tok, 16) for tok in re.findall(r'0x[0-9a-fA-F]+', match.group(1)))
        self.assertEqual(len(expected), 256 * 14)
        self.assertEqual((CORE / 'firmware/mda-font-8x14.bin').read_bytes(), expected)

    def test_mda_cells_use_fourteen_rows(self):
        for code in range(256):
            self.assertEqual(glyph(self.font, code)[14:], bytes(2), hex(code))

    def test_only_blank_cp437_characters_are_blank(self):
        blank = {code for code in range(256) if not any(glyph(self.font, code))}
        self.assertEqual(blank, {0x00, 0x20, 0xFF})

    def test_ninth_dot_box_drawing_characters_have_ink_and_join(self):
        for code in range(0xC0, 0xE0):
            with self.subTest(code=hex(code)):
                self.assertTrue(any(glyph(self.font, code)[:14]), hex(code))
        rows = [glyph(self.font, 0xCD)[r] for r in range(14)]
        self.assertTrue(all(row in (0, 0xFF) for row in rows) and rows.count(0xFF) >= 2)
        self.assertTrue(all(glyph(self.font, 0xBA)[r] == glyph(self.font, 0xBA)[0] != 0 for r in range(14)))

    def test_hardware_dos_lock_matches(self):
        descriptor = json.loads((CORE / 'firmware/hardware-dos.json').read_text())['mda_font']
        data = (CORE / descriptor['file']).read_bytes()
        self.assertEqual(len(data), descriptor['size'])
        self.assertEqual(hashlib.sha256(data).hexdigest(), descriptor['sha256'])
        self.assertEqual(descriptor['license'], 'Public domain')


if __name__ == '__main__':
    unittest.main()
