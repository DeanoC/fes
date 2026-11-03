# SPDX-License-Identifier: GPL-2.0-or-later
"""The open ZX81 ROM image and its assembler. `make sim-fes-zx81-openrom`
runs the ROM itself on the machine RTL."""
import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
import z80asm  # noqa: E402

spec = importlib.util.spec_from_file_location("build_zx81_openrom", ROOT / "scripts" / "build_zx81_openrom.py")
build_zx81_openrom = importlib.util.module_from_spec(spec)
spec.loader.exec_module(build_zx81_openrom)


def assemble(text: str, size: int = 0x100) -> bytes:
    with tempfile.TemporaryDirectory() as directory:
        source = Path(directory) / "t.asm"
        source.write_text(text)
        return z80asm.Assembler().assemble(source, size)


class AssemblerTest(unittest.TestCase):
    def test_encodings(self):
        image = assemble("""
        org 0
        ld a,(ix+5)
        ld (iy-2),0x12
        bit 7,(iy+1)
        res 0,(ix+3)
        ld (0x4010),de
        ex af,af'
        sbc hl,bc
        ld h,(ix+1)
        rl (ix+4)
        ld ixh,a
        im 1
        dz "AB1"
        """)
        self.assertEqual(image[:36].hex(),
                         "dd7e05" "fd36fe12" "fdcb017e" "ddcb0386" "ed531040" "08" "ed42"
                         "dd6601" "ddcb0416" "dd67" "ed56" "26279d")

    def test_invalid_forms_are_rejected(self):
        for line in ("jr p,x", "jr m,x", "jr pe,x", "jr po,x",
                     "out (0xfe),b", "in b,(0xfe)",
                     "rst 1", "rst 0x40", "bit 8,a", "set 9,(hl)",
                     "ld (hl),(hl)", "ld (ix+1),(hl)", "ld (ix+1),(iy+1)",
                     "ld h,ixl", "ld ixh,l", "ld ixh,(hl)", "ld ixh,(ix+1)",
                     "ld ixh,iyl", "adc ix,bc", "sbc iy,de", "add ix,iy"):
            with self.subTest(line=line), self.assertRaises(z80asm.AsmError):
                assemble(f"        org 0\n        {line}\nx:      ret\n")

    def test_index_loads_that_exist_still_encode(self):
        image = assemble("""
        org 0
        ld ixh,a
        ld ixl,ixh
        ld iyh,b
        ld h,(ix+1)
        ld (iy+2),l
        ld (hl),a
        ld e,(hl)
        """)
        self.assertEqual(image[:14].hex(), "dd67" "dd6c" "fd60" "dd6601" "fd7502" "77" "5e")

    def test_index_displacement_is_one_expression(self):
        image = assemble("""
        org 0
        ld a,(ix-1-1)
        ld a,(iy+1-3)
        ld a,(ix-N)
N       equ 4
        """)
        self.assertEqual(image[:9].hex(), "dd7efe" "fd7efe" "dd7efc")

    def test_far_jr_becomes_jp(self):
        image = assemble("""
        org 0
        jr nz,far
        jr near
near:   ds 200
far:    ret
        """, 0x200)
        self.assertEqual(image[:3], bytes([0xc2, 0xcd, 0x00]))   # jp nz,far
        self.assertEqual(image[3:5], bytes([0x18, 0x00]))        # jr near

    def test_floating_code_fills_gaps(self):
        image = assemble("""
        floatlimit 0x10
        org 0
        jp a
        org 8
        ret
        autofloat
a:      jp b
b:      ret
        endautofloat
        """, 0x10)
        # a (3 bytes) and b (1 byte) fit the gap between the fixed blocks.
        self.assertEqual(image[:7], bytes([0xc3, 0x03, 0x00, 0xc3, 0x06, 0x00, 0xc9]))
        self.assertEqual(image[8], 0xc9)

    def test_sections_joined_by_jr_stay_together(self):
        # Largest first and best fit would put b alone in the first gap,
        # too far from a for its jr.
        image = assemble("""
        floatlimit 0x200
        org 0
        jp a
        org 0x90
        ret
        autofloat
a:      ds 128
        jr b
b:      ret
        ds 139
        endautofloat
        """, 0x200)
        self.assertEqual(image[:3], bytes([0xc3, 0x91, 0x00]))      # jp a
        self.assertEqual(image[0x111:0x114], bytes([0x18, 0x00, 0xc9]))   # jr b, b: ret


class OpenRomImageTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory()
        cls.rom = build_zx81_openrom.build(Path(cls.directory.name))
        cls.symbols = {
            name: int(address, 16)
            for address, name in (line.split() for line in
                                  (Path(cls.directory.name) / "zx81-openrom.sym").read_text().splitlines())
        }

    @classmethod
    def tearDownClass(cls):
        cls.directory.cleanup()

    def at(self, address, data):
        self.assertEqual(self.rom[address:address + len(data)].hex(), bytes(data).hex(), hex(address))

    def test_size_and_outputs(self):
        self.assertEqual(len(self.rom), 8192)
        hex_lines = (Path(self.directory.name) / "zx81-openrom.hex").read_text().split()
        self.assertEqual(bytes(int(b, 16) for b in hex_lines), self.rom)

    def test_restarts_and_display_interrupts(self):
        self.at(0x0000, [0xd3, 0xfd])                     # OUT ($FD),A: NMI off
        self.at(0x0008, [0xe1, 0x7e, 0xfd, 0x77, 0x00])   # POP HL; LD A,(HL); LD (IY+0),A
        self.assertEqual(self.rom[0x0010], 0xc3)          # JP print
        self.at(0x0018, [0x2a, 0x16, 0x40, 0x2b])         # LD HL,(CH_ADD); DEC HL
        self.at(0x0020, [0x2a, 0x16, 0x40])               # LD HL,(CH_ADD)
        self.assertEqual(self.rom[0x0028], 0xcf)          # RST 8: no calculator yet
        self.at(0x0030, [0x2a, 0x1a, 0x40, 0xcd])         # LD HL,(STKBOT); CALL
        self.at(0x0038, [0x0d, 0xc2])                     # INT: DEC C; JP NZ
        self.at(0x0066, [0x08, 0x3c, 0xfa])               # NMI: EX AF,AF'; INC A; JP M

    def test_new_stops_display_before_rebuilding_dfile(self):
        # NEW goes FAST (which waits for the frame to end) and falls into the
        # power-on code; D_FILE is published only once its rows are valid.
        address = self.symbols["st_new"]
        fast = self.symbols["fast"]
        self.assertEqual(self.rom[address:address + 3], bytes([0xcd, fast & 0xff, fast >> 8]))
        self.assertEqual(address + 3, self.symbols["new_entry"])
        core = (build_zx81_openrom.SOURCE / "core.asm").read_text()
        new = core.split("new_entry:", 1)[1].split("; Fresh machine stack", 1)[0]
        self.assertLess(new.index("djnz .nl"), new.index("ld (D_FILE),de"))

    def test_measure_and_fast_paths_preserve_their_contracts(self):
        check_line = self.symbols["check_line"]
        self.assertIn(bytes([0xa7, 0xc9]), self.rom[check_line:check_line + 16])

        # FAST sets bit 5 before it clears bit 7, then waits until the frame
        # engine clears bit 5 as it stops.
        fast = self.symbols["fast"]
        self.at(fast + 5, [0xfd, 0xcb, 0x3b, 0xee,        # SET 5,(IY+CDFLAG)
                           0xfd, 0xcb, 0x3b, 0xbe,        # RES 7,(IY+CDFLAG)
                           0xfd, 0xcb, 0x3b, 0x6e,        # BIT 5,(IY+CDFLAG)
                           0x20, 0xfa, 0xc9])             # JR NZ,BIT; RET
        stop = self.symbols["border_ix.stop"]
        self.at(stop, [0xfd, 0xcb, 0x3b, 0xae, 0xd3, 0xfd])  # RES 5,(IY+CDFLAG); OUT ($FD),A

    def test_decode_carry_matches_the_published_entry_point(self):
        decode = self.symbols["decode"]
        decode_code = self.rom[decode:decode + 64]
        decode_code = decode_code[:decode_code.index(bytes([0x37, 0xc9])) + 2]  # success sets carry
        self.assertEqual(decode_code.count(bytes([0xa7, 0xc9])), 2)  # failure clears carry
        # Two rows or two columns down (rollover): AND leaves carry clear.
        self.assertEqual(decode_code[:6], bytes([0x79, 0x2f, 0x5f, 0x3d, 0xa3, 0xc0]))
        self.assertEqual(decode_code[6:14], bytes([0x78, 0x2f, 0xe6, 0x3e, 0x5f, 0x3d, 0xa3, 0xc0]))

    def test_key_tables(self):
        self.at(0x007e, [0x3f, 0x3d, 0x28, 0x3b])          # Z X C V
        self.at(0x00a5, [0x0e, 0x19, 0x0f, 0x18])          # : ; ? /

    def test_tape_entry_points(self):
        self.assertEqual(self.rom[0x0207], 0xc9)           # LOAD/SAVE return
        self.at(0x02fb, [0x00, 0xc3, 0x13, 0x03])          # SAVE arm, JP $0313
        self.at(0x0313, [0x21, 0x09, 0x40])                # LD HL,VERSN
        self.at(0x031e, [0x5e, 0xc9])                      # LD E,(HL); RET
        self.at(0x0347, [0xaf, 0x37, 0x30, 0xfc, 0xc3, 0x07, 0x02])

    def test_published_entry_points(self):
        for address in (0x02bb, 0x02e7, 0x07bd, 0x08f5, 0x0a2a, 0x0f2b, 0x0f46):
            self.assertEqual(self.rom[address], 0xc3, hex(address))
        self.assertEqual(self.rom[0x0281], 0xcd)           # display IX routine
        self.assertEqual(self.rom[0x02e7:0x02ea],
                         bytes([0xc3, self.symbols["fast"] & 0xff,
                                self.symbols["fast"] >> 8]))
        self.assertEqual(self.rom[0x0f23:0x0f2a],
                         bytes([0xfd, 0xcb, 0x3b, 0xb6, 0xc3,
                                self.symbols["fast"] & 0xff,
                                self.symbols["fast"] >> 8]))

    def test_explicit_fast_clears_saved_slow_request(self):
        address = self.symbols["fast_entry"]
        self.assertEqual(address, 0x0f23)
        self.assertEqual(self.rom[address:address + 4], bytes([0xfd, 0xcb, 0x3b, 0xb6]))

    def test_character_set(self):
        font = self.rom[0x1e00:]
        self.assertEqual(font[:8], bytes(8))               # space
        self.assertEqual(font[1 * 8:2 * 8], bytes([0xf0] * 4 + [0] * 4))  # top-left block
        self.assertEqual(font[3 * 8:4 * 8], bytes([0xff] * 4 + [0] * 4))  # top half
        self.assertEqual(font[8 * 8:9 * 8], bytes([0xaa, 0x55] * 4))      # grey
        self.assertNotEqual(font[38 * 8:39 * 8], bytes(8))                # A


if __name__ == "__main__":
    unittest.main()
