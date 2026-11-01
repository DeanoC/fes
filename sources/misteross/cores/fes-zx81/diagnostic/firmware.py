#!/usr/bin/env python3
"""Open ZX81 diagnostic firmware: an 8 KiB image for the zx81_machine ROM port.

Nothing here is derived from a Sinclair ROM; the code and glyphs are written
for FES. Only the addresses zx81_machine.sv patches are shared with BASIC,
because the RTL keys its LOAD and SAVE hooks on M1 fetches there.

At reset the program turns the NMI generator off (OUT ($FD),A, the first
instruction rom_init_tb.cpp checks), then:
  - probes RAM in 1 KiB steps from $4400 and stores RAMTOP at $4004. A write
    that does not read back, or that lands on $4000, ends the probe, so the
    vacant 1 KiB socket gives $4400 and a 16 KiB pack gives $8000;
  - copies a 24-row display file to $4080 and stores that address at $400C;
  - writes a 16-byte block at $4009 whose word at $4014 is $4019, so SAVE has
    a .p-shaped image before any LOAD.
Each frame then runs at a fixed T-state count:
  - VSYNC: reads the eight keyboard rows into $4048-$404F (any IN of an even
    port starts VSYNC) and ends it with OUT ($FF),A;
  - top border, then the display file. JP (HL) runs it at $C080: codes with
    bit 6 clear are fetched as NOPs and the ULA draws them from the glyph
    table at $1E00 (I = $1E); $76 is HALT. R is loaded so A6 (the INT line)
    falls on the first M1 after each HALT, and the IM 1 handler at $0038
    reruns a row for eight scanlines, then moves to the next. Every line is
    207 T, so INT acknowledge keeps HSYNC on the ULA's period;
  - bottom border: draws the key rows as 0/1 digits and the frame count
    ($4041), and starts LOAD on a new L press or SAVE on a new S press.
LOAD jumps to $0347, which the RTL replaces with its mailbox loader; the
bytes land from $4009 and the loader exits to $0207. SAVE jumps to $02FB
(arms the hook), the patched $02FC reaches $0313, and the loop CALLs $031E
(LD E,(HL)) for each byte from $4009 to the word at $4014; the RTL captures
E at the patched RET at $031F and closes the .p at $0207. $0207 redraws the
tape row and the LOAD/SAVE counts ($4042/$4043). The unpatched bytes at
$0347, $02FC and $031F set error bits in $4044 instead, as does an NMI.
$4040 holds $5A once the first frame has been drawn.
"""

from __future__ import annotations

import argparse
from pathlib import Path

SIZE = 8192
RAM_SENTINEL = 0x4000
RAMTOP = 0x4004
D_FILE_PTR = 0x400C
TAPE = 0x4009
E_LINE = 0x4014
VARS = 0x4040
READY, FRAMES, LOADS, SAVES, ERRORS, PENDING, LATCH = (VARS + i for i in range(7))
KEYS = 0x4048
D_FILE = 0x4080
STACK = 0x4400
FONT = 0x1E00
CODE = 0x0400

ROWS, COLUMNS = 24, 32
# R after LD R,A: EI, JP (HL), 32 codes and HALT are 35 M1 fetches with A6
# high; the 36th, the first M1 after HALT, has A6 low and takes the INT.
R_LINE = 0xDD
# BASIC's raster: 310 lines, first display line 59 after VSYNC. The top
# border also sets the ULA's row counter (HSYNCs since VSYNC, mod 8) to 0 on
# the first row.
TOP_BORDER_T = 207 * 54 + 138
BOTTOM_BORDER_T = 207 * 56
VSYNC_T = 207 * 6

# ZX81 character codes: space $00, digits $1C-$25, letters $26-$3F; bit 7
# inverts. Hex digit n is $1C + n.
GLYPHS = {
    ' ': '.......',
    '0': '.###./#...#/#..##/#.#.#/##..#/#...#/.###.',
    '1': '..#../.##../..#../..#../..#../..#../.###.',
    '2': '.###./#...#/....#/..##./.#.../#..../#####',
    '3': '####./....#/....#/.###./....#/....#/####.',
    '4': '...#./..##./.#.#./#..#./#####/...#./...#.',
    '5': '#####/#..../####./....#/....#/#...#/.###.',
    '6': '..##./.#.../#..../####./#...#/#...#/.###.',
    '7': '#####/....#/...#./..#../.#.../.#.../.#...',
    '8': '.###./#...#/#...#/.###./#...#/#...#/.###.',
    '9': '.###./#...#/#...#/.####/....#/...#./.##..',
    'A': '.###./#...#/#...#/#####/#...#/#...#/#...#',
    'B': '####./#...#/#...#/####./#...#/#...#/####.',
    'C': '.###./#...#/#..../#..../#..../#...#/.###.',
    'D': '####./#...#/#...#/#...#/#...#/#...#/####.',
    'E': '#####/#..../#..../####./#..../#..../#####',
    'F': '#####/#..../#..../####./#..../#..../#....',
    'G': '.###./#...#/#..../#.###/#...#/#...#/.####',
    'H': '#...#/#...#/#...#/#####/#...#/#...#/#...#',
    'I': '.###./..#../..#../..#../..#../..#../.###.',
    'J': '..###/...#./...#./...#./...#./#..#./.##..',
    'K': '#...#/#..#./#.#../##.../#.#../#..#./#...#',
    'L': '#..../#..../#..../#..../#..../#..../#####',
    'M': '#...#/##.##/#.#.#/#.#.#/#...#/#...#/#...#',
    'N': '#...#/##..#/#.#.#/#..##/#...#/#...#/#...#',
    'O': '.###./#...#/#...#/#...#/#...#/#...#/.###.',
    'P': '####./#...#/#...#/####./#..../#..../#....',
    'Q': '.###./#...#/#...#/#...#/#.#.#/#..#./.##.#',
    'R': '####./#...#/#...#/####./#.#../#..#./#...#',
    'S': '.####/#..../#..../.###./....#/....#/####.',
    'T': '#####/..#../..#../..#../..#../..#../..#..',
    'U': '#...#/#...#/#...#/#...#/#...#/#...#/.###.',
    'V': '#...#/#...#/#...#/#...#/#...#/.#.#./..#..',
    'W': '#...#/#...#/#...#/#.#.#/#.#.#/#.#.#/.#.#.',
    'X': '#...#/#...#/.#.#./..#../.#.#./#...#/#...#',
    'Y': '#...#/#...#/.#.#./..#../..#../..#../..#..',
    'Z': '#####/....#/...#./..#../.#.../#..../#####',
}
CODES = {' ': 0x00, **{str(d): 0x1C + d for d in range(10)},
         **{chr(ord('A') + i): 0x26 + i for i in range(26)}}


def glyph_bytes(code: int) -> bytes:
    """Eight rows; the other codes get a distinct pattern of their six bits."""
    char = next((c for c, n in CODES.items() if n == code), None)
    if char is None:
        return bytes([0x00] + [0x3C if code >> row & 1 else 0x24 for row in range(6)] + [0x00])
    rows = GLYPHS[char].split('/') if char != ' ' else ['.....'] * 7
    return bytes([0x00] + [int(row.replace('#', '1').replace('.', '0'), 2) << 2 for row in rows])


def text(string: str) -> list[int]:
    return [CODES[c] for c in string]


def cell(row: int, column: int) -> int:
    return D_FILE + 1 + row * (COLUMNS + 1) + column


RAMTOP_CELL = cell(2, 7)
FRAME_CELL = cell(3, 6)
KEY_CELL = [cell(5 + row, 5) for row in range(8)]
TAPE_CELL = cell(15, 0)
LOAD_CELL, SAVE_CELL, ERROR_CELL = cell(16, 5), cell(16, 13), cell(16, 20)


def display_file() -> bytes:
    rows = [[0] * COLUMNS for _ in range(ROWS)]

    def put(row: int, column: int, codes: list[int]) -> None:
        rows[row][column:column + len(codes)] = codes

    put(0, 0, [0x80] + text(' FES ZX81 DIAGNOSTIC'))
    put(2, 0, text('RAMTOP'))
    put(3, 0, text('FRAME'))
    for row in range(8):
        put(5 + row, 0, text(f'ROW{row}'))
    put(14, 0, text('TAPE'))
    put(16, 0, text('LOAD    SAVE    ERR'))
    put(18, 0, list(range(0x00, 0x20)))
    put(19, 0, list(range(0x20, 0x40)))
    put(20, 0, list(range(0x80, 0xA0)))
    put(21, 0, list(range(0xA0, 0xC0)))
    return bytes([0x76] + [code for row in rows for code in row + [0x76]])


DEFAULT_TAPE = bytes([0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA,
                      0x19, 0x40, 0xDD, 0xEE, 0xFF])


class W(int):
    """A 16-bit operand below $0100."""


class Asm:
    """Bytes at fixed addresses with label fixups and a T-state tally."""

    def __init__(self) -> None:
        self.image = bytearray(SIZE)
        self.used: set[int] = set()
        self.pc = 0
        self.labels: dict[str, int] = {}
        self.fixups: list[tuple[int, str, str]] = []
        self.t = 0

    def org(self, address: int) -> None:
        self.pc = address

    def label(self, name: str) -> None:
        assert name not in self.labels, name
        self.labels[name] = self.pc

    def put(self, data: bytes) -> None:
        for byte in data:
            assert self.pc not in self.used, hex(self.pc)
            self.used.add(self.pc)
            self.image[self.pc] = byte
            self.pc += 1

    def op(self, t: int, *items: int | str | tuple[str, str]) -> None:
        """Emit one instruction: ints are bytes (words above $FF or as W),
        'w:' and 'r:' strings are label fixups."""
        for item in items:
            if isinstance(item, str):
                kind, name = item.split(':')
                self.fixups.append((self.pc, kind, name))
                self.put(bytes(2 if kind == 'w' else 1))
            elif isinstance(item, W) or item > 0xFF:
                self.put(item.to_bytes(2, 'little'))
            else:
                self.put(bytes([item]))
        self.t += t

    def delay(self, t: int) -> None:
        """Exactly t T-states; clobbers B and nothing else."""
        while t > 3330 + 40:
            self.op(7, 0x06, 0x00)            # ld b,0
            self.op(3323, 0x10, 0xFE)         # djnz $
            t -= 3330
        best = None
        for loops in range(257):
            rest = t - (13 * loops + 2 if loops else 0)
            for jumps in range(max(rest, 0) // 10 + 1):
                nops, odd = divmod(rest - 10 * jumps, 4)
                if rest >= 0 and not odd and (best is None or nops + jumps < best[1] + best[2]):
                    best = (loops, nops, jumps)
        assert best is not None, t
        loops, nops, jumps = best
        if loops:
            self.op(7, 0x06, loops & 0xFF)    # ld b,n
            self.op(13 * loops - 5, 0x10, 0xFE)
        for _ in range(jumps):
            self.op(10, 0xC3, self.pc + 3)    # jp $+3
        for _ in range(nops):
            self.op(4, 0x00)

    def link(self) -> bytes:
        for address, kind, name in self.fixups:
            target = self.labels[name]
            if kind == 'w':
                self.image[address:address + 2] = target.to_bytes(2, 'little')
            else:
                offset = target - (address + 1)
                assert -128 <= offset < 128, name
                self.image[address] = offset & 0xFF
        return bytes(self.image)


def ld_a_hex(a: Asm, low: bool) -> None:
    """A = hex digit code of A's low (or high) nibble."""
    if not low:
        for _ in range(4):
            a.op(4, 0x0F)                     # rrca
    a.op(7, 0xE6, 0x0F)                       # and $0F
    a.op(7, 0xC6, 0x1C)                       # add a,$1C


def build() -> bytes:
    a = Asm()

    a.org(0x0000)
    a.op(11, 0xD3, 0xFD)                      # out ($FD),a   NMI generator off
    a.op(10, 0xC3, 'w:start')

    # IM 1 line handler. Both paths are 41 T to GO, 58 T to the first code.
    a.org(0x0038)
    a.op(4, 0x0D)                             # dec c         scanlines left
    a.op(7, 0x20, 'r:again')                  # jr nz,again
    a.op(10, 0xE1)                            # pop hl        next row
    a.op(7, 0x0E, 0x08)                       # ld c,8
    a.op(13, 0x10, 'r:go')                    # djnz go
    a.op(10, 0xC3, 'w:bottom')
    a.label('again')
    a.op(10, 0xD1)                            # pop de        same row
    a.op(4, 0x00)
    a.op(4, 0x00)
    a.op(7, 0x16, 0x00)                       # ld d,0
    a.label('go')
    a.op(9, 0xED, 0x4F)                       # ld r,a
    a.op(4, 0xFB)                             # ei
    a.op(4, 0xE9)                             # jp (hl)

    a.org(0x0066)
    a.op(0, 0xF5)                             # push af
    a.op(0, 0x3E, 0x04)
    a.op(0, 0xC3, 'w:nmi')

    # Hook addresses zx81_machine.sv patches. Each unpatched byte is an error.
    a.org(0x0207)
    a.op(0, 0xC3, 'w:resume')                 # LOAD and SAVE both exit here
    a.org(0x02FB)
    a.op(0, 0x00)                             # nop           M1 arms SAVE
    a.op(0, 0xC3, 'w:save_unpatched')         # read as JP $0313 while armed
    a.org(0x0313)
    a.op(0, 0x21, TAPE)                       # ld hl,$4009   M1 opens the .p
    a.op(0, 0xC3, 'w:save_loop')
    a.org(0x031E)
    a.op(0, 0x5E)                             # ld e,(hl)     the emitted byte
    a.op(0, 0xC3, 'w:save_unpatched')         # $031F reads as RET while armed
    a.org(0x0347)
    a.op(0, 0xC3, 'w:load_unpatched')         # the mailbox loader replaces this

    a.org(CODE)
    a.label('start')
    a.op(0, 0xF3)                             # di
    a.op(0, 0x31, STACK)                      # ld sp,$4400
    a.op(0, 0xAF)                             # xor a
    a.op(0, 0x32, RAM_SENTINEL)               # ld ($4000),a
    a.op(0, 0x21, 0x4400)                     # ld hl,$4400
    a.op(0, 0x11, 0x0400)                     # ld de,$0400
    a.label('probe')
    a.op(0, 0x36, 0xA5)                       # ld (hl),$A5
    a.op(0, 0x7E)                             # ld a,(hl)
    a.op(0, 0xFE, 0xA5)                       # cp $A5
    a.op(0, 0x20, 'r:probed')                 # jr nz,probed  no RAM here
    a.op(0, 0x3A, RAM_SENTINEL)               # ld a,($4000)
    a.op(0, 0xFE, 0xA5)                       # cp $A5
    a.op(0, 0x28, 'r:probed')                 # jr z,probed   aliased onto $4000
    a.op(0, 0x19)                             # add hl,de
    a.op(0, 0xCB, 0x7C)                       # bit 7,h
    a.op(0, 0x28, 'r:probe')                  # jr z,probe
    a.label('probed')
    a.op(0, 0x22, RAMTOP)                     # ld ($4004),hl
    a.op(0, 0x21, VARS)                       # clear $4040-$407F
    a.op(0, 0x06, 0x40)
    a.label('clear')
    a.op(0, 0x36, 0x00)
    a.op(0, 0x23)
    a.op(0, 0x10, 'r:clear')
    for source, target, length in (('w:dfile_image', D_FILE, len(display_file())),
                                   ('w:tape_image', TAPE, len(DEFAULT_TAPE))):
        a.op(0, 0x21, source)                 # ld hl,image
        a.op(0, 0x11, target)                 # ld de,target
        a.op(0, 0x01, W(length))              # ld bc,length
        a.op(0, 0xED, 0xB0)                   # ldir
    a.op(0, 0x21, D_FILE)
    a.op(0, 0x22, D_FILE_PTR)                 # ld ($400C),hl
    a.op(0, 0x21, RAMTOP_CELL)
    a.op(0, 0x3A, RAMTOP + 1)
    a.op(0, 0xCD, 'w:hex')
    a.op(0, 0x3A, RAMTOP)
    a.op(0, 0xCD, 'w:hex')
    a.op(0, 0x3E, 0x1E)
    a.op(0, 0xED, 0x47)                       # ld i,a        glyphs at $1E00
    a.op(0, 0xED, 0x56)                       # im 1
    a.op(0, 0xC3, 'w:resume')

    # $0207: redraw what LOAD or SAVE changed, count it, return to frames.
    a.label('resume')
    a.op(0, 0x31, STACK)
    a.op(0, 0x21, PENDING)
    a.op(0, 0x7E)                             # ld a,(hl)
    a.op(0, 0x36, 0x00)                       # ld (hl),0
    a.op(0, 0x21, LOADS)
    a.op(0, 0x3D)                             # dec a
    a.op(0, 0x20, 'r:not_load')
    a.op(0, 0x34)                             # inc (hl)
    a.label('not_load')
    a.op(0, 0x23)                             # hl = SAVES
    a.op(0, 0x3D)
    a.op(0, 0x20, 'r:not_save')
    a.op(0, 0x34)
    a.label('not_save')
    a.op(0, 0x11, TAPE)
    a.op(0, 0x21, TAPE_CELL)
    a.op(0, 0x06, 16)
    a.label('tape_row')
    a.op(0, 0x1A)                             # ld a,(de)
    a.op(0, 0xCD, 'w:hex')
    a.op(0, 0x13)                             # inc de
    a.op(0, 0x10, 'r:tape_row')
    for source, target in ((LOADS, LOAD_CELL), (SAVES, SAVE_CELL), (ERRORS, ERROR_CELL)):
        a.op(0, 0x21, target)
        a.op(0, 0x3A, source)
        a.op(0, 0xCD, 'w:hex')

    # The frame: fixed T from the last INT acknowledge to the next display.
    a.label('frame')
    a.t = 0
    keys_t = a.t
    for row in range(8):
        a.op(10, 0x01, 0xFE | (0xFF ^ (1 << row)) << 8)  # ld bc,row port
        a.op(12, 0xED, 0x78)                  # in a,(c)      starts VSYNC
        a.op(13, 0x32, KEYS + row)            # ld (key),a
    a.delay(VSYNC_T - (a.t - keys_t))
    a.op(11, 0xD3, 0xFF)                      # out ($FF),a   ends VSYNC
    a.delay(TOP_BORDER_T)
    a.op(7, 0x3E, 0x1E)
    a.op(9, 0xED, 0x47)                       # ld i,a
    a.op(10, 0x21, D_FILE | 0x8000)           # ld hl,$C080   run the display
    a.op(10, 0x01, (ROWS + 1) << 8 | 1)       # ld b,25 / ld c,1
    a.op(7, 0x3E, R_LINE)
    a.op(10, 0xC3, 'w:go')
    top_t = a.t

    a.label('bottom')
    a.t = 0
    for row in range(8):
        a.op(13, 0x3A, KEYS + row)            # ld a,(key)
        a.op(4, 0x5F)                         # ld e,a
        for bit in range(5):
            a.op(4, 0xAF)                     # xor a
            a.op(8, 0xCB, 0x3B)               # srl e
            a.op(7, 0xCE, 0x1C)               # adc a,$1C     '0' or '1'
            a.op(13, 0x32, KEY_CELL[row] + bit)
    a.op(10, 0x21, FRAMES)
    a.op(11, 0x34)                            # inc (hl)
    a.op(7, 0x7E)                             # ld a,(hl)
    a.op(4, 0x4F)                             # ld c,a
    ld_a_hex(a, low=False)
    a.op(13, 0x32, FRAME_CELL)
    a.op(4, 0x79)                             # ld a,c
    ld_a_hex(a, low=True)
    a.op(13, 0x32, FRAME_CELL + 1)
    a.op(7, 0x3E, 0x5A)
    a.op(13, 0x32, READY)
    # New presses: L is row 6 bit 1, S is row 1 bit 1 (low = down).
    a.op(13, 0x3A, KEYS + 6)
    a.op(4, 0x2F)                             # cpl
    a.op(7, 0xE6, 0x02)                       # and 2         L down
    a.op(4, 0x47)                             # ld b,a
    a.op(13, 0x3A, KEYS + 1)
    a.op(4, 0x2F)
    a.op(7, 0xE6, 0x02)
    a.op(4, 0x07)                             # rlca          S down: 4
    a.op(4, 0xB0)                             # or b
    a.op(4, 0x47)
    a.op(13, 0x3A, LATCH)
    a.op(4, 0x2F)
    a.op(4, 0xA0)                             # and b         newly down
    a.op(4, 0x4F)
    a.op(4, 0x78)
    a.op(13, 0x32, LATCH)
    a.op(4, 0x79)
    a.op(7, 0xE6, 0x02)
    a.op(10, 0xC2, 'w:load')                  # jp nz,load
    a.op(4, 0x79)
    a.op(7, 0xE6, 0x04)
    a.op(10, 0xC2, 'w:save')                  # jp nz,save
    a.delay(BOTTOM_BORDER_T - a.t - 10)
    a.op(10, 0xC3, 'w:frame')
    bottom_t = a.t

    a.label('load')
    a.op(0, 0x3E, 0x01)
    a.op(0, 0x32, PENDING)
    a.op(0, 0xC3, 0x0347)                     # jp $0347

    # SAVE emits $4009 up to the word at $4014, which must be $400A-$4040.
    a.label('save')
    a.op(0, 0xED, 0x4B, E_LINE)               # ld bc,($4014)
    a.op(0, 0x21, TAPE)
    a.op(0, 0xB7)                             # or a
    a.op(0, 0xED, 0x42)                       # sbc hl,bc
    a.op(0, 0x30, 'r:save_range')             # jr nc         end <= $4009
    a.op(0, 0x21, VARS)
    a.op(0, 0xB7)
    a.op(0, 0xED, 0x42)
    a.op(0, 0x38, 'r:save_range')             # jr c          end > $4040
    a.op(0, 0x3E, 0x02)
    a.op(0, 0x32, PENDING)
    a.op(0, 0xC3, 0x02FB)                     # jp $02FB
    a.label('save_range')
    a.op(0, 0x3E, 0x08)
    a.op(0, 0xC3, 'w:fault')

    a.label('save_loop')
    a.op(0, 0xCD, 0x031E)                     # call $031E
    a.op(0, 0x23)                             # inc hl
    a.op(0, 0x7D)                             # ld a,l
    a.op(0, 0xB9)                             # cp c
    a.op(0, 0x20, 'r:save_loop')
    a.op(0, 0x7C)                             # ld a,h
    a.op(0, 0xB8)                             # cp b
    a.op(0, 0x20, 'r:save_loop')
    a.op(0, 0xC3, 0x0207)                     # jp $0207      closes the .p

    a.label('load_unpatched')
    a.op(0, 0x3E, 0x01)
    a.op(0, 0x18, 'r:fault')
    a.label('save_unpatched')
    a.op(0, 0x3E, 0x02)
    a.label('fault')
    a.op(0, 0x21, ERRORS)
    a.op(0, 0xB6)                             # or (hl)
    a.op(0, 0x77)                             # ld (hl),a
    a.op(0, 0xAF)
    a.op(0, 0x32, PENDING)
    a.op(0, 0xC3, 'w:resume')
    a.label('nmi')
    a.op(0, 0xE5)                             # push hl
    a.op(0, 0x21, ERRORS)
    a.op(0, 0xB6)
    a.op(0, 0x77)
    a.op(0, 0xE1)                             # pop hl
    a.op(0, 0xF1)                             # pop af
    a.op(0, 0xED, 0x45)                       # retn

    # Two hex digits of A at HL; HL += 2.
    a.label('hex')
    a.op(0, 0x4F)                             # ld c,a
    ld_a_hex(a, low=False)
    a.op(0, 0x77)
    a.op(0, 0x23)
    a.op(0, 0x79)
    ld_a_hex(a, low=True)
    a.op(0, 0x77)
    a.op(0, 0x23)
    a.op(0, 0xC9)

    a.label('dfile_image')
    a.put(display_file())
    a.label('tape_image')
    a.put(DEFAULT_TAPE)
    assert a.pc <= FONT, hex(a.pc)

    a.org(FONT)
    a.put(b''.join(glyph_bytes(code) for code in range(64)))
    assert top_t > 0 and bottom_t == BOTTOM_BORDER_T, (top_t, bottom_t)
    return a.link()


IMAGE = build()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    (args.output_dir / 'firmware.hex').write_text(
        ''.join(f'{byte:02x}\n' for byte in IMAGE), encoding='ascii')
    (args.output_dir / 'firmware.rom').write_bytes(IMAGE)


if __name__ == '__main__':
    main()
