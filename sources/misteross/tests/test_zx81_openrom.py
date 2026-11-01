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
        for address in (0x08, 0x10, 0x18, 0x20, 0x28, 0x30):
            self.assertEqual(self.rom[address], 0xc3, hex(address))
        self.at(0x0038, [0x0d, 0xc2])                     # INT: DEC C; JP NZ
        self.at(0x0066, [0x08, 0x3c, 0xfa])               # NMI: EX AF,AF'; INC A; JP M

    def test_new_stops_display_before_rebuilding_dfile(self):
        address = self.symbols["new_entry"]
        self.assertEqual(self.rom[address:address + 2], bytes([0xd3, 0xfd]))
        core = (build_zx81_openrom.SOURCE / "core.asm").read_text()
        new = core.split("new_entry:", 1)[1].split("; Fresh machine stack", 1)[0]
        dfile = new.index("ld (D_FILE),de")
        self.assertLess(new.index("out (0xfd),a"), dfile)
        self.assertLess(new.index("djnz .nl"), dfile)

    def test_measure_and_fast_paths_preserve_their_contracts(self):
        check_line = self.symbols["check_line"]
        self.assertIn(bytes([0xa7, 0xc9]), self.rom[check_line:check_line + 16])

        fast = self.symbols["fast"]
        kscan = self.symbols["kscan"]
        fast_code = self.rom[fast:kscan]
        sampled = fast_code.index(bytes([0x3a, 0x34, 0x40, 0x5f]))  # LD A,(FRAMES); LD E,A
        stopped = fast_code.index(bytes([0xfd, 0xcb, 0x3b, 0xbe]))  # RES 7,(IY+CDFLAG)
        self.assertLess(sampled, stopped)

    def test_decode_carry_matches_the_published_entry_point(self):
        decode = self.symbols["decode"]
        save_loop = self.symbols["save_loop"]
        decode_code = self.rom[decode:save_loop]
        self.assertEqual(decode_code.count(bytes([0xa7, 0xc9])), 2)  # failure clears carry
        self.assertEqual(decode_code.count(bytes([0x37, 0xc9])), 1)  # success sets carry

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
        for address in (0x02bb, 0x02e7, 0x07bd, 0x08f5, 0x0a2a, 0x0f23, 0x0f2b, 0x0f46):
            self.assertEqual(self.rom[address], 0xc3, hex(address))
        self.assertEqual(self.rom[0x0281], 0xcd)           # display IX routine

    def test_character_set(self):
        font = self.rom[0x1e00:]
        self.assertEqual(font[:8], bytes(8))               # space
        self.assertEqual(font[1 * 8:2 * 8], bytes([0xf0] * 4 + [0] * 4))  # top-left block
        self.assertEqual(font[3 * 8:4 * 8], bytes([0xff] * 4 + [0] * 4))  # top half
        self.assertEqual(font[8 * 8:9 * 8], bytes([0xaa, 0x55] * 4))      # grey
        self.assertNotEqual(font[38 * 8:39 * 8], bytes(8))                # A


if __name__ == "__main__":
    unittest.main()
