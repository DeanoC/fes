"""Self-contained 8086 DOS .COM video test suite generator (MDA & Hercules).

Generates deterministic DOS .COM executables with no external assembler needed:
- MDATEST.COM: verifies MDA BDA/equipment word/INT 10h mode 07h, draws attribute
  and 9th-dot CP437 box-drawing test rows into B000:0000, prints FES MDA PASS.
- HGCTEST.COM: detects Hercules via 3BAh bit 7 transitions, enters 720x348 Full
  graphics mode (3BFh=03h), draws distinct Page 0 and Page 1 patterns across all
  four interleave banks, flips between Page 0 and Page 1 on keypresses, restores
  MDA 80x25 text mode, prints FES HGC PASS.
"""
from __future__ import annotations

import struct


class Asm8086:
    """Minimal deterministic 8086 assembler for .COM images (ORG 0x100)."""

    def __init__(self, org: int = 0x100):
        self.org = org
        self.buf = bytearray()
        self.labels: dict[str, int] = {}
        self.fixups: list[tuple[int, str, str]] = []  # (offset, kind, label)

    @property
    def pc(self) -> int:
        return self.org + len(self.buf)

    def label(self, name: str) -> None:
        if name in self.labels:
            raise ValueError(f'duplicate label: {name}')
        self.labels[name] = self.pc

    def db(self, *vals: int | bytes) -> None:
        for v in vals:
            if isinstance(v, bytes):
                self.buf.extend(v)
            else:
                self.buf.append(v & 0xff)

    def dw(self, *vals: int) -> None:
        for v in vals:
            self.buf.extend(struct.pack('<H', v & 0xffff))

    def emit(self, *bs: int) -> None:
        self.buf.extend(b & 0xff for b in bs)

    def mov_r16_imm(self, reg: int, imm: int) -> None:
        # reg: AX=0 CX=1 DX=2 BX=3 SP=4 BP=5 SI=6 DI=7
        self.emit(0xb8 + reg)
        self.dw(imm)

    def mov_r16_label(self, reg: int, name: str) -> None:
        self.emit(0xb8 + reg)
        self.fixups.append((len(self.buf), 'abs16', name))
        self.dw(0)

    def mov_r8_imm(self, reg: int, imm: int) -> None:
        # reg: AL=0 CL=1 DL=2 BL=3 AH=4 CH=5 DH=6 BH=7
        self.emit(0xb0 + reg, imm)

    def mov_es_ax(self) -> None:
        self.emit(0x8e, 0xc0)

    def mov_ds_ax(self) -> None:
        self.emit(0x8e, 0xd8)

    def push_es(self) -> None:
        self.emit(0x06)

    def pop_es(self) -> None:
        self.emit(0x07)

    def push_ds(self) -> None:
        self.emit(0x1e)

    def pop_ds(self) -> None:
        self.emit(0x1f)

    def xor_r16_r16(self, dst: int, src: int) -> None:
        self.emit(0x31, 0xc0 | (src << 3) | dst)

    def add_r16_imm(self, reg: int, imm: int) -> None:
        self.emit(0x81, 0xc0 | reg)
        self.dw(imm)

    def cmp_ax_imm(self, imm: int) -> None:
        self.emit(0x3d)
        self.dw(imm)

    def cmp_al_imm(self, imm: int) -> None:
        self.emit(0x3c, imm)

    def test_al_imm(self, imm: int) -> None:
        self.emit(0xa8, imm)

    def and_al_imm(self, imm: int) -> None:
        self.emit(0x24, imm)

    def inc_r16(self, reg: int) -> None:
        self.emit(0x40 + reg)

    def dec_r16(self, reg: int) -> None:
        self.emit(0x48 + reg)

    def cld(self) -> None:
        self.emit(0xfc)

    def stosb(self) -> None:
        self.emit(0xaa)

    def stosw(self) -> None:
        self.emit(0xab)

    def rep_stosw(self) -> None:
        self.emit(0xf3, 0xab)

    def out_dx_al(self) -> None:
        self.emit(0xee)

    def in_al_dx(self) -> None:
        self.emit(0xec)

    def mov_ax_es_mem(self, addr: int) -> None:
        self.emit(0x26, 0xa1)
        self.dw(addr)

    def mov_es_di_al(self) -> None:
        self.emit(0x26, 0x88, 0x05)

    def mov_es_di_ax(self) -> None:
        self.emit(0x26, 0x89, 0x05)

    def int_n(self, vec: int) -> None:
        self.emit(0xcd, vec)

    def call(self, name: str) -> None:
        self.emit(0xe8)
        self.fixups.append((len(self.buf), 'rel16', name))
        self.dw(0)

    def ret(self) -> None:
        self.emit(0xc3)

    def jmp(self, name: str) -> None:
        self.emit(0xe9)
        self.fixups.append((len(self.buf), 'rel16', name))
        self.dw(0)

    def jcc8(self, opcode: int, name: str) -> None:
        self.emit(opcode)
        self.fixups.append((len(self.buf), 'rel8', name))
        self.emit(0)

    def jz(self, name: str) -> None:
        self.jcc8(0x74, name)

    def jnz(self, name: str) -> None:
        self.jcc8(0x75, name)

    def jnz_far(self, name: str) -> None:
        self.emit(0x74, 0x03)  # jz +3 (skip near jmp)
        self.jmp(name)

    def jae(self, name: str) -> None:
        self.jcc8(0x73, name)

    def loop(self, name: str) -> None:
        self.jcc8(0xe2, name)

    def loop_far(self, name: str) -> None:
        self.dec_r16(1)
        self.jnz_far(name)

    def build(self) -> bytes:
        out = bytearray(self.buf)
        for offset, kind, name in self.fixups:
            target = self.labels[name]
            if kind == 'abs16':
                out[offset:offset + 2] = struct.pack('<H', target & 0xffff)
            elif kind == 'rel16':
                disp = target - (self.org + offset + 2)
                if not -32768 <= disp <= 32767:
                    raise ValueError(f'rel16 out of range for {name}: {disp}')
                out[offset:offset + 2] = struct.pack('<h', disp)
            elif kind == 'rel8':
                disp = target - (self.org + offset + 1)
                if not -128 <= disp <= 127:
                    raise ValueError(f'rel8 out of range for {name}: {disp}')
                out[offset] = disp & 0xff
            else:
                raise ValueError(f'unknown fixup kind: {kind}')
        return bytes(out)


def build_mdatest_com() -> bytes:
    """Build MDATEST.COM: verify MDA BIOS state, draw attributes + 9th-dot box."""
    a = Asm8086(0x100)
    # 1. INT 11h equipment check: bits 5:4 must be 11 (0x30) for 80x25 mono.
    a.int_n(0x11)
    a.and_al_imm(0x30)
    a.cmp_al_imm(0x30)
    a.jnz_far('fail')

    # 2. BDA 0040:0063 must be 0x03B4 (MDA CRTC base).
    a.mov_r16_imm(0, 0x0040)
    a.mov_es_ax()
    a.mov_ax_es_mem(0x0063)
    a.cmp_ax_imm(0x03b4)
    a.jnz_far('fail')

    # 3. INT 10h AH=0Fh current video mode must be 07h.
    a.mov_r8_imm(4, 0x0f)  # AH = 0Fh
    a.int_n(0x10)
    a.cmp_al_imm(0x07)
    a.jnz_far('fail')

    # Clear screen cleanly (INT 10h AX=0007h), then draw test rows directly to B000:0000.
    a.mov_r16_imm(0, 0x0007)
    a.int_n(0x10)
    a.mov_r16_imm(0, 0xb000)
    a.mov_es_ax()
    a.cld()

    def draw_row(row: int, text: bytes, attr: int) -> None:
        a.mov_r16_imm(7, row * 160)  # DI
        for ch in text:
            a.mov_r16_imm(0, (attr << 8) | ch)
            a.stosw()

    draw_row(0, b'FES MDA PASS', 0x07)
    draw_row(2, b'BRIGHT NORMAL 0F', 0x0f)
    draw_row(3, b'UNDERLINE ATTR 01', 0x01)
    draw_row(4, b'BRIGHT UNDERLINE 09', 0x09)
    draw_row(5, b'REVERSE VIDEO 70', 0x70)
    draw_row(6, b'HIDDEN', 0x00)
    # Row 7: CP437 box drawing (C9h, CDh*10, BBh) exercises 9th-dot continuation!
    draw_row(7, bytes([0xc9] + [0xcd] * 10 + [0xbb]), 0x07)

    # Set cursor to row 9, col 0 so DOS prompt appears below our test card.
    a.mov_r8_imm(4, 0x02)  # AH=02h
    a.mov_r8_imm(7, 0x00)  # BH=page 0
    a.mov_r16_imm(2, 0x0900)  # DH=9, DL=0
    a.int_n(0x10)

    a.mov_r16_imm(0, 0x4c00)
    a.int_n(0x21)

    a.label('fail')
    a.mov_r16_label(2, 'msg_fail')
    a.mov_r8_imm(4, 0x09)
    a.int_n(0x21)
    a.mov_r16_imm(0, 0x4c01)
    a.int_n(0x21)

    a.label('msg_fail')
    a.db(b'FES MDA FAIL\r\n$')
    return a.build()


def build_hgctest_com() -> bytes:
    """Build HGCTEST.COM: detect Hercules 3BAh vsync, draw Page 0 & Page 1 720x348."""
    a = Asm8086(0x100)

    # 1. BDA 0040:0063 must be 0x03B4.
    a.mov_r16_imm(0, 0x0040)
    a.mov_es_ax()
    a.mov_ax_es_mem(0x0063)
    a.cmp_ax_imm(0x03b4)
    a.jnz_far('fail')

    # 2. Detect Hercules VSync bit 7 transitions on port 3BAh.
    # Poll up to 65535 times for bit 7 == 0 and bit 7 == 1.
    a.mov_r16_imm(2, 0x03ba)
    a.mov_r16_imm(1, 0xffff)
    a.label('wait_vs0')
    a.in_al_dx()
    a.test_al_imm(0x80)
    a.jz('saw_vs0')
    a.loop('wait_vs0')
    a.jmp('fail')

    a.label('saw_vs0')
    a.mov_r16_imm(1, 0xffff)
    a.label('wait_vs1')
    a.in_al_dx()
    a.test_al_imm(0x80)
    a.jnz('saw_vs1')
    a.loop('wait_vs1')
    a.jmp('fail')

    a.label('saw_vs1')
    # 3. Enable Hercules Full mode (3BFh = 03h) while keeping 3B8h blanked/text
    #    so both Page 0 (B0000) and Page 1 (B8000) are populated before enabling graphics.
    a.mov_r16_imm(2, 0x03bf)
    a.mov_r8_imm(0, 0x03)
    a.out_dx_al()

    a.mov_r16_imm(2, 0x03b8)
    a.mov_r8_imm(0, 0x00)
    a.out_dx_al()

    # Clear Page 0 (B000:0000, 16384 words).
    a.mov_r16_imm(0, 0xb000)
    a.mov_es_ax()
    a.xor_r16_r16(7, 7)  # DI = 0
    a.xor_r16_r16(0, 0)  # AX = 0
    a.mov_r16_imm(1, 0x4000)
    a.cld()
    a.rep_stosw()

    # Draw Page 0 pattern:
    # - Top line y=0 (bank 0, off 0): 90 bytes of 0xFF
    a.mov_r16_imm(7, 0x0000)
    a.mov_r16_imm(0, 0xffff)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()
    # - y=1 (bank 1, off 0x2000): 90 bytes of 0xAA
    a.mov_r16_imm(7, 0x2000)
    a.mov_r16_imm(0, 0xaaaa)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()
    # - y=2 (bank 2, off 0x4000): 90 bytes of 0xCC
    a.mov_r16_imm(7, 0x4000)
    a.mov_r16_imm(0, 0xcccc)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()
    # - y=3 (bank 3, off 0x6000): 90 bytes of 0xF0
    a.mov_r16_imm(7, 0x6000)
    a.mov_r16_imm(0, 0xf0f0)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()
    # - Bottom line y=347 (bank 3, row 86 -> 0x6000 + 86*90 = 0x7E3C): 90 bytes of 0xFF
    a.mov_r16_imm(7, 0x6000 + 86 * 90)
    a.mov_r16_imm(0, 0xffff)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()
    # - Left & right borders across y=40..139, plus solid block x=64..191 (bytes 8..23 = 8 words) in all 4 banks.
    # For row_group = 10..34 (25 groups -> y=40..139):
    a.mov_r16_imm(3, 10 * 90)  # BX = row_group * 90
    a.mov_r16_imm(1, 25)       # CX = 25 row groups
    a.label('p0_rows')
    for bank in range(4):
        bank_off = bank * 0x2000
        # Left edge (col 0): 0x80
        a.emit(0x89, 0xdf)  # mov di, bx
        if bank_off:
            a.add_r16_imm(7, bank_off)
        a.mov_r8_imm(0, 0x80)
        a.mov_es_di_al()
        # Right edge (col 89): 0x01
        a.emit(0x89, 0xdf)
        a.add_r16_imm(7, bank_off + 89)
        a.mov_r8_imm(0, 0x01)
        a.mov_es_di_al()
        # Block x=64..191 (col 8..23 = 8 words)
        a.emit(0x89, 0xdf)
        a.add_r16_imm(7, bank_off + 8)
        a.mov_r16_imm(0, 0xffff)
        for _ in range(8):
            a.stosw()
    a.add_r16_imm(3, 90)
    a.loop_far('p0_rows')

    # Clear Page 1 (B800:0000, 16384 words) before activating graphics.
    a.mov_r16_imm(0, 0xb800)
    a.mov_es_ax()
    a.xor_r16_r16(7, 7)
    a.xor_r16_r16(0, 0)
    a.mov_r16_imm(1, 0x4000)
    a.rep_stosw()

    # Draw Page 1 pattern: top/bottom borders + solid block in bottom-right (x=448..575, y=200..299).
    a.mov_r16_imm(7, 0x0000)
    a.mov_r16_imm(0, 0xffff)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()
    a.mov_r16_imm(7, 0x6000 + 86 * 90)
    a.mov_r16_imm(0, 0xffff)
    a.mov_r16_imm(1, 45)
    a.rep_stosw()

    a.mov_r16_imm(3, 50 * 90)  # row_group = 50..74 (y=200..299)
    a.mov_r16_imm(1, 25)
    a.label('p1_rows')
    for bank in range(4):
        bank_off = bank * 0x2000
        a.emit(0x89, 0xdf)
        if bank_off:
            a.add_r16_imm(7, bank_off)
        a.mov_r8_imm(0, 0x80)
        a.mov_es_di_al()
        a.emit(0x89, 0xdf)
        a.add_r16_imm(7, bank_off + 89)
        a.mov_r8_imm(0, 0x01)
        a.mov_es_di_al()
        a.emit(0x89, 0xdf)
        a.add_r16_imm(7, bank_off + 56)  # x=448..575 -> byte 56..71 (8 words)
        a.mov_r16_imm(0, 0xffff)
        for _ in range(8):
            a.stosw()
    a.add_r16_imm(3, 90)
    a.loop_far('p1_rows')

    # Program 720x348 HGC CRTC registers and enable Page 0 (3B8h = 0Ah).
    a.mov_r16_imm(2, 0x03b8)
    a.mov_r8_imm(0, 0x02)  # graphics mode, video blanked during CRTC update
    a.out_dx_al()

    hgc_crtc = (0x35, 0x2d, 0x2e, 0x07, 0x5b, 0x02, 0x57, 0x57, 0x02, 0x03, 0x00, 0x00)
    a.mov_r16_imm(2, 0x03b4)
    for idx, val in enumerate(hgc_crtc):
        a.mov_r8_imm(0, idx)
        a.out_dx_al()
        a.inc_r16(2)  # 0x3b5
        a.mov_r8_imm(0, val)
        a.out_dx_al()
        a.dec_r16(2)  # 0x3b4

    a.mov_r16_imm(2, 0x03b8)
    a.mov_r8_imm(0, 0x0a)
    a.out_dx_al()

    # Wait for first keypress ('1') while displaying Page 0.
    a.xor_r16_r16(0, 0)
    a.int_n(0x16)

    # Flip active display to Page 1 (3B8h = 8Ah).
    a.mov_r16_imm(2, 0x03b8)
    a.mov_r8_imm(0, 0x8a)
    a.out_dx_al()

    # Wait for second keypress ('2') while displaying Page 1.
    a.xor_r16_r16(0, 0)
    a.int_n(0x16)

    # Restore MDA 80x25 text mode cleanly: 3B8=00, CRTC text table, 3B8=28, 3BF=00, INT 10h AX=0007h.
    a.mov_r16_imm(2, 0x03b8)
    a.mov_r8_imm(0, 0x00)
    a.out_dx_al()
    mda_crtc = (0x61, 0x50, 0x52, 0x0f, 0x19, 0x06, 0x19, 0x19, 0x02, 0x0d, 0x0b, 0x0c)
    a.mov_r16_imm(2, 0x03b4)
    for idx, val in enumerate(mda_crtc):
        a.mov_r8_imm(0, idx)
        a.out_dx_al()
        a.inc_r16(2)
        a.mov_r8_imm(0, val)
        a.out_dx_al()
        a.dec_r16(2)
    a.mov_r16_imm(2, 0x03b8)
    a.mov_r8_imm(0, 0x28)
    a.out_dx_al()
    a.mov_r16_imm(2, 0x03bf)
    a.mov_r8_imm(0, 0x00)
    a.out_dx_al()

    a.mov_r16_imm(0, 0x0007)
    a.int_n(0x10)

    a.mov_r16_label(2, 'msg_pass')
    a.mov_r8_imm(4, 0x09)
    a.int_n(0x21)
    a.mov_r16_imm(0, 0x4c00)
    a.int_n(0x21)

    a.label('fail')
    a.mov_r16_label(2, 'msg_fail')
    a.mov_r8_imm(4, 0x09)
    a.int_n(0x21)
    a.mov_r16_imm(0, 0x4c01)
    a.int_n(0x21)

    a.label('msg_pass')
    a.db(b'\r\nFES HGC PASS\r\n$')
    a.label('msg_fail')
    a.db(b'\r\nFES HGC FAIL\r\n$')
    return a.build()
