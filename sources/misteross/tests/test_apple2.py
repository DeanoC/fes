"""Apple II open diagnostic, character set and slot-socket contract checks."""

import re
import sys
import unittest
from pathlib import Path

from scripts import apple2_slots

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'cores' / 'fes-apple2' / 'diagnostic'))

import asm6502  # noqa: E402
import firmware  # noqa: E402
import font  # noqa: E402
import probe_card  # noqa: E402


class Apple2DiagnosticTest(unittest.TestCase):
    def test_assembler_encodings_and_zero_page_choice(self):
        image = asm6502.assemble('''
FWD = $30
        .org $F000
start:  LDA #$01
        STA $0400,X
        LDA (FWD),Y
        STA ADDR,Y
        BNE start
        JMP (vec)
vec:    .word start
ADDR = $2C
''', 0xF000, 17)
        self.assertEqual(image.hex(), 'a9019d0004b130992c00d0f46c0ff000f0')
        with self.assertRaises(asm6502.AsmError):
            asm6502.assemble(' BNE far\n .fill 200, 0\nfar: RTS\n', 0x1000, 256)

    def test_font_is_open_and_tracked(self):
        table = font.table()
        self.assertEqual(len(table), 512)
        self.assertEqual(len(set(font.ORDER)), 64)
        for glyph in range(64):
            rows = table[glyph * 8:glyph * 8 + 8]
            self.assertEqual(rows[7], 0)
            self.assertTrue(all(value & 0x81 == 0 for value in rows))
        tracked = (ROOT / 'cores/fes-apple2/rtl/apple2_font.vh').read_text()
        self.assertEqual(tracked, font.verilog())
        self.assertEqual(font.screen_code('A'), 0xC1)
        self.assertEqual(font.screen_code('A', 'inverse'), 0x01)
        self.assertEqual(font.screen_code('A', 'flash'), 0x41)

    def test_probe_card_page(self):
        page = probe_card.page()
        self.assertEqual(page[0xF8:], b"FESPROBE")
        tracked = (ROOT / 'cores/fes-apple2/expansions/probe_rom.vh').read_text()
        self.assertEqual(tracked, probe_card.verilog())
        firmware_image = firmware.build_firmware()
        self.assertIn(b"FESPROBE", firmware_image)  # the slot scan signature

    def test_firmware_layout(self):
        image = firmware.build_firmware()
        self.assertEqual(len(image), 16384)
        reset = image[0x3FFC] | image[0x3FFD] << 8
        self.assertEqual(reset, 0xE000)
        # Disk II slot-scan signature at $C601/$C603/$C605/$C607.
        self.assertEqual(bytes(image[0x0601:0x0608:2]), bytes([0x20, 0x00, 0x03, 0x3C]))
        self.assertEqual(image[0x3F58], 0x60)  # IORTS
        self.assertEqual(image[0x1000 + firmware.TRANSLATE62[5]], 5)  # decode table at $D000

    def test_disk_image(self):
        disk = firmware.build_disk()
        self.assertEqual(len(disk), firmware.DISK_SIZE)
        self.assertEqual(disk[1], 0x86)  # stage 1 at $0801: STX DSLOT
        stage2 = firmware.DOS_SKEW[1] * 256
        self.assertEqual(disk[stage2:stage2 + 2], bytes([0xA9, 0xB2]))
        for track, physical, _page in firmware.DISK_READS:
            f = track * 16 + firmware.DOS_SKEW[physical]
            self.assertEqual(disk[f * 256:(f + 1) * 256], firmware.progression_sector(f))
            self.assertEqual(sorted(firmware.progression_sector(f)), list(range(256)))


class Apple2SlotTest(unittest.TestCase):
    def test_rtl_is_generated_from_the_table(self):
        self.assertEqual(apple2_slots.RTL.read_text(), apple2_slots.socket_rtl())

    def test_sockets_match_the_go_linker_layout(self):
        rbf = (ROOT / 'expansion' / 'rbf.go').read_text()
        table = dict(re.findall(
            r'(\d): \{Apple2Slot, Apple2Map, (\d+, \d+, \d+, \d+)\}', rbf))
        self.assertEqual(sorted(int(k) for k in table), [s.slot for s in apple2_slots.SOCKETS])
        for socket in apple2_slots.SOCKETS:
            self.assertEqual(tuple(int(v) for v in table[str(socket.slot)].split(', ')), socket.cram)

    def test_sockets_are_disjoint_and_pins_are_inside_their_rectangle(self):
        seen = set()
        sockets = apple2_slots.SOCKETS
        for a in sockets:
            for b in sockets:
                if a is not b:
                    self.assertTrue(a.last_row + 2 < b.first_row or b.last_row + 2 < a.first_row)
                    ax0, ay0, ax1, ay1 = a.cram
                    bx0, by0, bx1, by1 = b.cram
                    self.assertFalse(ax0 < bx1 and bx0 < ax1 and ay0 < by1 and by0 < ay1)
            bels = apple2_slots.boundary_bels(a)
            anchors = apple2_slots.clock_anchor_bels(a)
            self.assertEqual(len(bels), len(anchors) + apple2_slots.REQUEST_BITS + apple2_slots.RESPONSE_BITS)
            rows = {(int(bel.split('.')[1]), int(bel.split('.')[2])) for bel in anchors}
            for row in range(a.first_row, a.last_row + 1):
                self.assertIn((apple2_slots.COLUMN + 4, row), rows)
                self.assertTrue(row < a.first_row + 3 or (apple2_slots.COLUMN, row) in rows)
            for bel in bels.values():
                _, x, y, _z = bel.split('.')
                self.assertIn(int(x), (apple2_slots.COLUMN, apple2_slots.COLUMN + 4))
                self.assertTrue(a.first_row <= int(y) <= a.last_row)
                self.assertNotIn(bel, seen)
                seen.add(bel)
            self.assertEqual(a.placement.split()[0], f'slot{a.slot}')


if __name__ == '__main__':
    unittest.main()
