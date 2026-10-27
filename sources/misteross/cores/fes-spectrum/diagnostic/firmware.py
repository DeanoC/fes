#!/usr/bin/env python3
"""Open ZX Spectrum diagnostic firmware (16, 32, 48 or 64 KiB) and a PZX tape.

Nothing here is derived from a Sinclair or Amstrad ROM. Hand-assembled; the
program at $0100:
  - paints a red border, one black ink byte on a white attribute cell, and
    writes $A5 to $8000;
  - pages bank 7 to $C000 through $7FFD, writes $77, pages bank 0 back and
    writes $66 there (a 48K has no paging, so $66 lands where $77 did);
  - selects AY register 8 through $FFFD, writes $0F through $BFFD and stores
    the read-back at $8005;
  - waits for a frame interrupt (IM1; $0038 is EI/RET), delays into the
    paper area and writes 64 contended screen bytes, then $C3 to $8006 so
    the simulation can check the T state of that write;
  - loads the diagnostic tape with its own turbo loader, which counts reads
    of port $FE in B as tape loaders do (so the deck's loader detection plays
    the tape): after 64 pilot pulses of 20..32 counts a short sync pulse,
    then each bit's first pulse, long for a 1, into four bytes at $8100;
  - loops reading the keyboard A row, the Kempston port, probe socket 1's id
    and its scratch register into $8001-$8004. The probe ports use high byte
    $80: on 128K-class models a low port with A15 and A1 clear is $7FFD.
The last 16 KiB page of a larger image repeats the first, so every ROM page
selection reaches the same program. The 48 KiB Pentagon image's third page
stands in for TR-DOS, which the Pentagon pages in at reset: it selects 48
BASIC and jumps to $0000 through RAM, which pages TR-DOS out as TR-DOS's own
exit does.
"""

from __future__ import annotations

import argparse
from pathlib import Path

ORG = 0x0100
SETUP = bytes([
    0xF3,                    # di
    0x31, 0x00, 0x7E,        # ld sp,$7E00
    0x3E, 0x02,              # ld a,2
    0xD3, 0xFE,              # out ($FE),a       red border
    0x3E, 0xFF,              # ld a,$FF
    0x32, 0x00, 0x40,        # ld ($4000),a      ink byte
    0x3E, 0x38,              # ld a,$38
    0x32, 0x00, 0x58,        # ld ($5800),a      white paper, black ink
    0x3E, 0xA5,              # ld a,$A5
    0x32, 0x00, 0x80,        # ld ($8000),a
    0x01, 0xFD, 0x7F,        # ld bc,$7FFD
    0x3E, 0x07,              # ld a,7
    0xED, 0x79,              # out (c),a         bank 7 at $C000
    0x3E, 0x77,              # ld a,$77
    0x32, 0x00, 0xC0,        # ld ($C000),a
    0xAF,                    # xor a
    0xED, 0x79,              # out (c),a         bank 0 at $C000
    0x3E, 0x66,              # ld a,$66
    0x32, 0x00, 0xC0,        # ld ($C000),a
    0x01, 0xFD, 0xFF,        # ld bc,$FFFD
    0x3E, 0x08,              # ld a,8
    0xED, 0x79,              # out (c),a         AY register 8
    0x06, 0xBF,              # ld b,$BF
    0x3E, 0x0F,              # ld a,$0F
    0xED, 0x79,              # out (c),a
    0x06, 0xFF,              # ld b,$FF
    0xED, 0x78,              # in a,(c)
    0x32, 0x05, 0x80,        # ld ($8005),a
    0xED, 0x56,              # im 1
    0xFB,                    # ei
    0x76,                    # halt               frame interrupt
    0xF3,                    # di
    0x06, 0x00, 0x10, 0xFE,  # ld b,0 / djnz $    3330 T
    0x06, 0x00, 0x10, 0xFE,
    0x06, 0x00, 0x10, 0xFE,
    0x06, 0x00, 0x10, 0xFE,
    0x21, 0x00, 0x48,        # ld hl,$4800
    0x06, 0x40,              # ld b,64
    0x77,                    # ld (hl),a          contended
    0x2C,                    # inc l
    0x10, 0xFC,              # djnz -4
    0x3E, 0xC3,              # ld a,$C3
    0x32, 0x06, 0x80,        # ld ($8006),a       timing marker
])
TAPE_BYTES = bytes([0xA5, 0x3C, 0x81, 0x7E])


def loader(edge: int) -> bytes:
    """The turbo loader; edge is the address of EDGE."""
    call = bytes([0xCD, edge & 0xFF, edge >> 8])
    return bytes([
        0xDB, 0xFE,          # in a,($FE)
        0xE6, 0x40,          # and $40
        0x4F,                # ld c,a             C: EAR level
        0x16, 0x00,          # waitpilot: ld d,0
    ]) + call + bytes([      # pilot: call edge   B: pulse width
        0x78,                # ld a,b
        0xFE, 20,            # cp 20
        0x38, 0x0A,          # jr c,notpilot
        0xFE, 33,            # cp 33
        0x30, 0xF2,          # jr nc,waitpilot
        0x14,                # inc d
        0x20, 0xF1,          # jr nz,pilot
        0x15,                # dec d
        0x18, 0xEE,          # jr pilot
        0x7A,                # notpilot: ld a,d
        0xFE, 64,            # cp 64
        0x38, 0xE7,          # jr c,waitpilot
    ]) + call + bytes([      # second sync pulse
        0xDD, 0x21, 0x00, 0x81,  # ld ix,$8100
        0x11, len(TAPE_BYTES), 0x00,  # ld de,4
        0x2E, 0x08,          # byte: ld l,8
    ]) + call + bytes([      # bit: call edge     first half of the bit
        0x78,                # ld a,b
        0xFE, 11,            # cp 11
        0x3F,                # ccf                carry: long pulse, a 1
        0xCB, 0x14,          # rl h
    ]) + call + bytes([      # second half
        0x2D,                # dec l
        0x20, 0xF1,          # jr nz,bit
        0xDD, 0x74, 0x00,    # ld (ix+0),h
        0xDD, 0x23,          # inc ix
        0x1B,                # dec de
        0x7A,                # ld a,d
        0xB3,                # or e
        0x20, 0xE5,          # jr nz,byte
    ])


# EDGE: B counts reads of port $FE until EAR (bit 6) changes, 43 T a read
# (more when contended); 0 after 256 reads.
EDGE = bytes([
    0x06, 0x00,              # ld b,0
    0x04,                    # e1: inc b
    0xC8,                    # ret z
    0xDB, 0xFE,              # in a,($FE)
    0xA9,                    # xor c
    0xE6, 0x40,              # and $40
    0x28, 0xF7,              # jr z,e1
    0x79,                    # ld a,c
    0xEE, 0x40,              # xor $40
    0x4F,                    # ld c,a
    0xC9,                    # ret
])
LOOP = ORG + len(SETUP) + len(loader(0))
MAIN = bytes([
    0x01, 0xFE, 0xFD,        # ld bc,$FDFE
    0xED, 0x78,              # in a,(c)
    0x32, 0x01, 0x80,        # ld ($8001),a
    0x01, 0x1F, 0x00,        # ld bc,$001F
    0xED, 0x78,              # in a,(c)
    0x32, 0x02, 0x80,        # ld ($8002),a
    0x01, 0xE0, 0x80,        # ld bc,$80E0       A15 set: no $7FFD alias
    0xED, 0x78,              # in a,(c)
    0x32, 0x03, 0x80,        # ld ($8003),a
    0x01, 0xE1, 0x80,        # ld bc,$80E1
    0x3E, 0x5A,              # ld a,$5A
    0xED, 0x79,              # out (c),a
    0xED, 0x78,              # in a,(c)
    0x32, 0x04, 0x80,        # ld ($8004),a
    0xC3, LOOP & 0xFF, LOOP >> 8,  # jp LOOP
])
PROGRAM = SETUP + loader(LOOP + len(MAIN)) + MAIN + EDGE



def pzx_block(tag: bytes, body: bytes) -> bytes:
    return tag + len(body).to_bytes(4, "little") + body


def u16s(*values: int) -> bytes:
    return b"".join(value.to_bytes(2, "little") for value in values)


# A turbo block for that loader: 301 pilot pulses of 1200 T and two 400 T
# sync pulses (so the data starts high, as a ROM block's does), four bytes of
# two 350 or 700 T pulses per bit with a 945 T tail, then a second of silence.
TAPE = (pzx_block(b"PZXT", bytes([1, 0]))
        + pzx_block(b"PULS", u16s(0x8000 | 301, 1200, 400, 400))
        + pzx_block(b"DATA", (0x80000000 | 8 * len(TAPE_BYTES)).to_bytes(4, "little") + u16s(945)
                    + bytes([2, 2]) + u16s(350, 350, 700, 700) + TAPE_BYTES)
        + pzx_block(b"PAUS", (3500000).to_bytes(4, "little")))


def page() -> bytes:
    image = bytearray(16384)
    image[0x0000:0x0003] = bytes([0xC3, ORG & 0xFF, ORG >> 8])   # jp $0100
    image[0x0038:0x003A] = bytes([0xFB, 0xC9])                   # ei / ret
    image[ORG:ORG + len(PROGRAM)] = PROGRAM
    return bytes(image)


# The Pentagon's TR-DOS page: select 48 BASIC, then JP $0000 from $5B00.
DOS_EXIT = bytes([
    0xF3,                    # di
    0x01, 0xFD, 0x7F,        # ld bc,$7FFD
    0x3E, 0x10,              # ld a,$10
    0xED, 0x79,              # out (c),a         48 BASIC
    0x3E, 0xC3,              # ld a,$C3
    0x32, 0x00, 0x5B,        # ld ($5B00),a
    0xAF,                    # xor a
    0x32, 0x01, 0x5B,        # ld ($5B01),a
    0x32, 0x02, 0x5B,        # ld ($5B02),a      jp $0000
    0xC3, 0x00, 0x5B,        # jp $5B00
])


def dos_page() -> bytes:
    return DOS_EXIT + bytes(16384 - len(DOS_EXIT))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    for size in (16, 32, 48, 64):
        image = page() * 2 + dos_page() if size == 48 else page() * (size // 16)
        suffix = "" if size == 16 else f"-{size}k"
        (args.output_dir / f"firmware{suffix}.hex").write_text(
            "\n".join(f"{byte:02x}" for byte in image) + "\n", encoding="ascii")
        (args.output_dir / f"firmware{suffix}.rom").write_bytes(image)
    (args.output_dir / "tape.pzx").write_bytes(TAPE)


if __name__ == "__main__":
    main()
