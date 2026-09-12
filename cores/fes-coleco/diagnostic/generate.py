#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""Original, BIOS-free Z80 Graphics I diagnostic; see LICENSE for source/output.

The annotated instruction emitter below is the cartridge source. It needs only
Python's standard library, not an assembler or downloaded ROM. All addresses are
absolute relative to the reset shim's JP 0x8000 entry point.
"""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path


def cartridge(interactive: bool = False, controllers: bool = False) -> bytes:
    dynamic = interactive or controllers
    code = bytearray()
    tables: list[tuple[int, bytes]] = []

    def emit(*values: int) -> None:
        code.extend(values)

    def out(port: int, value: int) -> None:
        emit(0x3E, value, 0xD3, port)  # LD A,value; OUT (port),A

    def register(index: int, value: int) -> None:
        out(0xBF, value)
        out(0xBF, 0x80 | index)

    def address(value: int) -> None:
        out(0xBF, value & 0xFF)
        out(0xBF, 0x40 | (value >> 8))  # VRAM write, low byte first

    def jr_nz(target: int) -> None:
        displacement = target - (len(code) + 2)
        assert -128 <= displacement <= 127
        emit(0x20, displacement & 0xFF)

    def copy(destination: int, data: bytes) -> None:
        address(destination)
        emit(0x21, 0, 0)  # LD HL,table (fixed up after code)
        tables.append((len(code) - 2, data))
        emit(0x11, len(data) & 0xFF, len(data) >> 8)  # LD DE,length
        loop = len(code)
        emit(0x7E, 0xD3, 0xBE, 0x23, 0x1B, 0x7A, 0xB3)
        # LD A,(HL); OUT (BE),A; INC HL; DEC DE; LD A,D; OR E
        jr_nz(loop)

    emit(0xF3)  # DI: no BIOS or interrupt service dependencies
    emit(0x31, 0x00, 0x64)  # LD SP,6400 (no CALL, PUSH or RAM reads)
    emit(0xDB, 0xBF)  # IN A,(BF): clear the control latch and VBlank
    for index, value in enumerate((0x00, 0x80, 0x00, 0x80, 0x01, 0x36, 0x03, 0x01)):
        register(index, value)

    address(0x0000)
    emit(0x01, 0x00, 0x40)  # LD BC,4000: clear ALL 16 KiB, not power-up RAM
    clear = len(code)
    emit(0xAF, 0xD3, 0xBE, 0x0B, 0x78, 0xB1)
    # XOR A; OUT (BE),A; DEC BC; LD A,B; OR C
    jr_nz(clear)

    # Name 0 = solid border; names 1/2 = inset 6x6 square within an 8x8 tile.
    square = bytes((0, 0x7E, 0x7E, 0x7E, 0x7E, 0x7E, 0x7E, 0))
    copy(0x0800, bytes([0xFF] * 24) + bytes(8) if dynamic
         else bytes([0xFF] * 8) + square + square)
    # This reduced VDP uses color_base + name (not the full TMS9918 palette):
    # nonzero high nibble -> green; zero high nibble -> orange; unset bit -> black.
    copy(0x2000, bytes((0xF1, 0xF1, 0x01, 0)) if dynamic
         else bytes((0xF1, 0xF1, 0x01)))
    copy(0x1B00, bytes((0xD0,)))  # Sprite-list terminator for future VDP expansion
    names = bytes(
        0 if col in (0, 31) or row in (0, 23) else 1 + ((col + row) & 1)
        for row in range(24) for col in range(32)
    )
    if dynamic:
        names = bytes(0 if col in (0, 31) or row in (0, 23) else 3
                      for row in range(24) for col in range(32))
    copy(0x0000, names)
    register(1, 0xC0)  # Graphics I, 16 KiB, display on, interrupts off
    if dynamic:
        # FF differs from every valid raw bus byte (bit7=0), and from every
        # normalized five-bit row. Every bank paints after reset/reload.
        emit(0x3E, 0xFF)
        for bank in range(4 if controllers else 2):
            emit(0x32, bank, 0x60)
        if interactive:
            out(0xC0, 0)  # shared joystick mode; OUT data is ignored
        poll = 0x8000 + len(code)
        for bank, port in enumerate((0xFC, 0xFC, 0xFF, 0xFF) if controllers else (0xFC, 0xFF)):
            if controllers:
                out(0x80 if bank & 1 else 0xC0, 0)
            emit(0xDB, port)
            if interactive:
                # Preserve directions and normalize bus fire1 bit6 to panel bit4.
                emit(0x57, 0xE6, 0x0F, 0x5F, 0x7A, 0x0F, 0x0F, 0xE6, 0x10, 0xB3)
                # LD D,A; AND 0F; LD E,A; LD A,D; RRCA twice; AND 10; OR E
            emit(0x5F)  # LD E,A: raw byte for controllers, normalized legacy row
            emit(0x3A, bank, 0x60, 0xBB)  # LD A,(6000+bank); CP E
            emit(0xCA, 0, 0)  # JP Z,next_bank (absolute fixup)
            skip = len(code) - 2
            emit(0x7B, 0x32, bank, 0x60)  # LD A,E; LD (cache),A
            for bit in range(8 if controllers else 5):
                emit(0x7B, 0xE6, 1 << bit)  # LD A,E; AND mask
                emit(0x28, 0x04, 0x3E, 0x02, 0x18, 0x02, 0x3E, 0x01)
                # JR Z,pressed; LD A,2 (orange); JR selected; LD A,1 (green)
                emit(0x57)  # LD D,A: preserve tile while setting VDP address
                first_row = 3 + 5 * bank if controllers else 5 + 10 * bank
                for row in range(first_row, first_row + (2 if controllers else 4)):
                    address(row * 32 + (4 + 3 * bit if controllers else 2 + 6 * bit))
                    emit(0x7A)  # LD A,D
                    for _ in range(2 if controllers else 4):
                        emit(0xD3, 0xBE)
            code[skip:skip + 2] = (0x8000 + len(code)).to_bytes(2, "little")
        emit(0xC3, poll & 0xFF, poll >> 8)  # JP poll; never depends on interrupts
    else:
        emit(0x76, 0x18, 0xFD, 0x00)  # HALT; JR back; unreachable NOP makes an odd media tail

    for pointer, data in tables:
        location = 0x8000 + len(code)
        code[pointer:pointer + 2] = location.to_bytes(2, "little")
        code.extend(data)
    if dynamic and len(code) % 2 == 0:
        emit(0xFF)  # unused odd tail exercises the single-byte GP transfer
    assert 1 <= len(code) <= 16384
    return bytes(code)


def preview(interactive: bool = False, row0: int = 31, row1: int = 31,
            controllers: bool = False, matrix: int = 0xFFFFFFFFFF) -> bytes:
    """720p PPM reference for the current shell, including its read latency.

    The C++ board test derives its pixel oracle separately from the tile layout.
    This optional artifact is for the human hardware-capture comparison.
    """
    banks = []
    for player in range(2):
        joy = (matrix >> (5 * player)) & 15
        fire1 = (matrix >> (4 + 5 * player)) & 1
        fire2 = (matrix >> (10 + player)) & 1
        keypad = 15
        for key, nibble in enumerate((10, 13, 7, 12, 2, 3, 14, 5, 1, 11, 9, 6)):
            if not matrix & (1 << (12 + 12 * player + key)):
                keypad = nibble
                break
        banks.extend((0x30 | (fire1 << 6) | joy, 0x30 | (fire2 << 6) | keypad))
    rows = bytearray()
    for y in range(720):
        for x in range(1280):
            rgb = (0, 0, 0)
            if 384 <= x < 896 and 168 <= y < 552:
                lx, ly = max(0, x - 385) // 2, (y - 168) // 2
                col, row = lx // 8, ly // 8
                if col in (0, 31) or row in (0, 23):
                    rgb = (0, 255, 64)
                elif controllers:
                    for bank, value in enumerate(banks):
                        for bit in range(8):
                            if (3 + 5 * bank <= row <= 4 + 5 * bank
                                    and 4 + 3 * bit <= col <= 5 + 3 * bit):
                                rgb = (255, 64, 0) if value & (1 << bit) else (0, 255, 64)
                elif interactive:
                    for player, bits in enumerate((row0, row1)):
                        for bit in range(5):
                            if (5 + 10 * player <= row < 9 + 10 * player
                                    and 2 + 6 * bit <= col < 6 + 6 * bit):
                                rgb = (255, 64, 0) if bits & (1 << bit) else (0, 255, 64)
                elif lx % 8 not in (0, 7) and ly % 8 not in (0, 7):
                    rgb = (0, 255, 64) if (col + row) % 2 == 0 else (255, 64, 0)
            rows.extend(rgb)
    return b"P6\n1280 720\n255\n" + rows


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="raw cartridge path")
    parser.add_argument("--preview", type=Path, help="optional expected 1280x720 PPM")
    parser.add_argument("--pad-to", type=int, help="pad with FF to this raw size, at most 16384")
    modes = parser.add_mutually_exclusive_group()
    modes.add_argument("--interactive", action="store_true", help="poll two joystick rows continuously")
    modes.add_argument("--controllers", action="store_true", help="display both players' raw joystick/keypad bytes")
    def matrix_value(value: str) -> int:
        try:
            return int(value, 0)
        except ValueError:
            return int(value, 16)
    parser.add_argument("--matrix", type=matrix_value, default=0xFFFFFFFFFF,
                        help="preview-only active-low 40-bit integer or hexadecimal matrix")
    parser.add_argument("--row0", type=int, default=31, help="preview-only active-low player 1 bits (0..31)")
    parser.add_argument("--row1", type=int, default=31, help="preview-only active-low player 2 bits (0..31)")
    args = parser.parse_args()
    if args.preview is not None and args.preview.resolve() == args.output.resolve():
        parser.error("cartridge and preview must have different paths")
    if not 0 <= args.row0 <= 31 or not 0 <= args.row1 <= 31:
        parser.error("--row0 and --row1 must be 0..31")
    if not args.interactive and (args.row0 != 31 or args.row1 != 31):
        parser.error("controller preview rows require --interactive")
    if not 0 <= args.matrix <= 0xFFFFFFFFFF:
        parser.error("--matrix must fit 40 bits")
    if not args.controllers and args.matrix != 0xFFFFFFFFFF:
        parser.error("--matrix requires --controllers")
    data = cartridge(args.interactive, args.controllers)
    if args.pad_to is not None:
        if not len(data) <= args.pad_to <= 16384:
            parser.error(f"--pad-to must be {len(data)}..16384")
        data += bytes([0xFF]) * (args.pad_to - len(data))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(data)
    if args.preview is not None:
        args.preview.parent.mkdir(parents=True, exist_ok=True)
        args.preview.write_bytes(preview(args.interactive, args.row0, args.row1,
                                        args.controllers, args.matrix))
    print(f"{args.output}: {len(data)} bytes, entry 0x8000, sha256 {hashlib.sha256(data).hexdigest()}")


if __name__ == "__main__":
    main()
