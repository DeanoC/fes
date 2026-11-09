"""The PC/XT CGA character ROM covers all 256 CP437 characters (fes#1537).

Rogue draws its dungeon map from CP437 codes 80h-FFh in CGA text mode: walls
BAh-BCh, C8h, C9h, CDh, doors CEh, passages B1h and floor FAh (codes and
attributes read from MAME ibm5150 text memory). The package font used to hold
only GLaBIOS's 128 ASCII glyphs, so every upper character rendered blank.
"""
import hashlib
import json
from pathlib import Path
import sys
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts import sim_fes_pcxt as sim
from scripts import build_fes_pcxt_quartus as producer

CORE = ROOT / 'cores/fes-pcxt'
ROGUE_MAP = (0xB1, 0xBA, 0xBB, 0xBC, 0xC8, 0xC9, 0xCD, 0xCE, 0xFA)


def glyph(font, code):
    return font[code * 16:code * 16 + 16]


class CgaFontTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.font = sim.cga_font()

    def test_rogue_map_glyphs_have_ink(self):
        for code in ROGUE_MAP:
            with self.subTest(code=hex(code)):
                self.assertTrue(any(glyph(self.font, code)[:8]), hex(code))

    def test_only_blank_cp437_characters_are_blank(self):
        blank = {code for code in range(256) if not any(glyph(self.font, code))}
        self.assertEqual(blank, {0x00, 0x20, 0xFF})

    def test_cga_cells_use_eight_rows(self):
        for code in range(256):
            self.assertEqual(glyph(self.font, code)[8:], bytes(8), hex(code))

    def test_ascii_half_matches_the_bios_graphics_table(self):
        # GLaBIOS's INT 10h graphics table for 00h-7Fh, so BIOS text and the
        # existing prompt templates keep their exact glyphs.
        bios = (CORE / 'firmware/GLABIOS_0.4.2_8X.ROM').read_bytes()
        for code in range(128):
            self.assertEqual(glyph(self.font, code)[:8], bios[6766 + code * 8:6766 + code * 8 + 8], hex(code))

    def test_box_drawing_joins_cell_edges(self):
        rows = [glyph(self.font, 0xCD)[r] for r in range(8)]
        self.assertTrue(all(row in (0, 0xFF) for row in rows) and rows.count(0xFF) == 2)
        self.assertTrue(all(glyph(self.font, 0xBA)[r] == glyph(self.font, 0xBA)[0] != 0 for r in range(8)))

    def test_producer_packages_the_locked_font(self):
        descriptor = json.loads((CORE / 'firmware/hardware-dos.json').read_text())['font']
        data = (CORE / descriptor['file']).read_bytes()
        self.assertEqual(hashlib.sha256(data).hexdigest(), descriptor['sha256'])
        self.assertEqual(producer.dos_assets(ROOT)[1], self.font)


if __name__ == '__main__':
    unittest.main()
