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


def cartridge() -> bytes:
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
    copy(0x0800, bytes([0xFF] * 8) + square + square)
    # This reduced VDP uses color_base + name (not the full TMS9918 palette):
    # nonzero high nibble -> green; zero high nibble -> orange; unset bit -> black.
    copy(0x2000, bytes((0xF1, 0xF1, 0x01)))
    copy(0x1B00, bytes((0xD0,)))  # Sprite-list terminator for future VDP expansion
    names = bytes(
        0 if col in (0, 31) or row in (0, 23) else 1 + ((col + row) & 1)
        for row in range(24) for col in range(32)
    )
    copy(0x0000, names)
    register(1, 0xC0)  # Graphics I, 16 KiB, display on, interrupts off
    emit(0x76, 0x18, 0xFD, 0x00)  # HALT; JR back; unreachable NOP makes an odd media tail

    for pointer, data in tables:
        location = 0x8000 + len(code)
        code[pointer:pointer + 2] = location.to_bytes(2, "little")
        code.extend(data)
    assert 1 <= len(code) <= 16384
    return bytes(code)


def preview() -> bytes:
    """720p PPM reference for the current shell, including its read latency.

    The C++ board test derives its pixel oracle separately from the tile layout.
    This optional artifact is for the human hardware-capture comparison.
    """
    rows = bytearray()
    for y in range(720):
        for x in range(1280):
            rgb = (0, 0, 0)
            if 384 <= x < 896 and 168 <= y < 552:
                lx, ly = max(0, x - 385) // 2, (y - 168) // 2
                col, row = lx // 8, ly // 8
                if col in (0, 31) or row in (0, 23):
                    rgb = (0, 255, 64)
                elif lx % 8 not in (0, 7) and ly % 8 not in (0, 7):
                    rgb = (0, 255, 64) if (col + row) % 2 == 0 else (255, 64, 0)
            rows.extend(rgb)
    return b"P6\n1280 720\n255\n" + rows


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="raw cartridge path")
    parser.add_argument("--preview", type=Path, help="optional expected 1280x720 PPM")
    parser.add_argument("--pad-to", type=int, help="pad with FF to this raw size, at most 16384")
    args = parser.parse_args()
    if args.preview is not None and args.preview.resolve() == args.output.resolve():
        parser.error("cartridge and preview must have different paths")
    data = cartridge()
    if args.pad_to is not None:
        if not len(data) <= args.pad_to <= 16384:
            parser.error(f"--pad-to must be {len(data)}..16384")
        data += bytes([0xFF]) * (args.pad_to - len(data))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(data)
    if args.preview is not None:
        args.preview.parent.mkdir(parents=True, exist_ok=True)
        args.preview.write_bytes(preview())
    print(f"{args.output}: {len(data)} bytes, entry 0x8000, sha256 {hashlib.sha256(data).hexdigest()}")


if __name__ == "__main__":
    main()
