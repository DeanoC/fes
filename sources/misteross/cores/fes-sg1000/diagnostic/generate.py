#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""BIOS-free SG-1000 Graphics I diagnostic; see LICENSE for source/output.

The annotated instruction emitter below is the cartridge source. It needs only
Python's standard library, not an assembler or downloaded ROM. The image is
entered at 0x0000 (SG-1000 has no Coleco-style BIOS shim).
"""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path


def cartridge(controllers: bool = False) -> bytes:
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
        out(0xBF, 0x40 | (value >> 8))

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
        jr_nz(loop)

    emit(0xF3)  # DI
    emit(0x31, 0x00, 0xC4)  # LD SP,C400 (top of the 1 KiB RAM mirror)
    emit(0xDB, 0xBF)  # IN A,(BF): clear the control latch and VBlank
    for index, value in enumerate((0x00, 0x80, 0x00, 0x80, 0x01, 0x36, 0x03, 0x01)):
        register(index, value)

    address(0x0000)
    emit(0x01, 0x00, 0x40)  # LD BC,4000: clear 16 KiB VRAM
    clear = len(code)
    emit(0xAF, 0xD3, 0xBE, 0x0B, 0x78, 0xB1)
    jr_nz(clear)

    # Graphics I shares a color byte across each group of eight names.
    # Border, green, red and blank therefore use names 0, 8, 16 and 24.
    square = bytes((0, 0x7E, 0x7E, 0x7E, 0x7E, 0x7E, 0x7E, 0))
    dynamic = controllers
    copy(0x0800, bytes([0xFF] * 8))
    copy(0x0840, bytes([0xFF] * 8) if dynamic else square)
    copy(0x0880, bytes([0xFF] * 8) if dynamic else square)
    copy(0x08C0, bytes(8))
    copy(0x2000, bytes((0x21, 0x21, 0x61, 0x11)))
    copy(0x1B00, bytes((0xD0,)))
    names = bytes(
        0 if col in (0, 31) or row in (0, 23) else 24 if dynamic
        else 8 + 8 * ((col + row) & 1)
        for row in range(24) for col in range(32)
    )
    copy(0x0000, names)
    register(1, 0xC0)  # Graphics I, 16 KiB, display on, interrupts off
    emit(0x3E, 0xA5, 0x32, 0x00, 0xC0)  # LD A,A5; LD (C000),A
    if not dynamic:
        emit(0xDB, 0xDC, 0x32, 0x01, 0xC0)  # IN A,(DC); LD (C001),A
        emit(0x76, 0x18, 0xFD)  # HALT; JR back
    else:
        emit(0xAF, 0x32, 0x01, 0xC0, 0x32, 0x02, 0xC0)
        poll = len(code)
        for bank, port in enumerate((0xDC, 0xDD)):
            emit(0xDB, port, 0x5F)  # IN A,(port); LD E,A
            emit(0x3A, 0x01 + bank, 0xC0, 0xBB)  # LD A,(cache); CP E
            emit(0xCA, 0, 0)  # JP Z,next bank
            skip = len(code) - 2
            emit(0x7B, 0x32, 0x01 + bank, 0xC0)  # LD A,E; LD (cache),A
            for bit in range(8):
                emit(0x7B, 0xE6, 1 << bit)
                emit(0x20, 0x04, 0x3E, 0x10, 0x18, 0x02, 0x3E, 0x08)
                # Active-low: pressed is red, released is green.
                emit(0x57)  # LD D,A
                for row in (4, 5) if bank == 0 else (8, 9):
                    address(row * 32 + 4 + 3 * bit)
                    emit(0x7A, 0xD3, 0xBE, 0x7A, 0xD3, 0xBE)
            code[skip:skip + 2] = len(code).to_bytes(2, "little")
        emit(0xC3, poll & 0xFF, poll >> 8)  # JP poll; never depends on interrupts

    for pointer, data in tables:
        location = len(code)
        code[pointer : pointer + 2] = location.to_bytes(2, "little")
        code.extend(data)
    assert 1 <= len(code) <= 16384
    return bytes(code)


def controller_ports(matrix: int) -> tuple[int, int]:
    dc = 0xFF
    dd = 0xFF
    for port_bit, matrix_bit in ((0, 0), (1, 2), (2, 3), (3, 1),
                                 (4, 4), (5, 5), (6, 7), (7, 8)):
        if not matrix & (1 << matrix_bit):
            dc &= ~(1 << port_bit)
    for port_bit, matrix_bit in ((0, 6), (1, 9)):
        if not matrix & (1 << matrix_bit):
            dd &= ~(1 << port_bit)
    return dc, dd


def preview(controllers: bool = False, matrix: int = 0xFFFFFFFFFF) -> bytes:
    """720p PPM reference for the shared Coleco 720p shell, including read latency."""
    rows = bytearray()
    for y in range(720):
        for x in range(1280):
            rgb = (0, 0, 0)
            if 384 <= x < 896 and 168 <= y < 552:
                lx, ly = max(0, x - 385) // 2, (y - 168) // 2
                col, row = lx // 8, ly // 8
                if col in (0, 31) or row in (0, 23):
                    rgb = (33, 200, 66)
                elif controllers:
                    for bank, value in enumerate(controller_ports(matrix)):
                        first_row = 4 if bank == 0 else 8
                        if first_row <= row <= first_row + 1:
                            for bit in range(8):
                                if 4 + 3 * bit <= col <= 5 + 3 * bit:
                                    rgb = (212, 82, 77) if not value & (1 << bit) else (33, 200, 66)
                elif lx % 8 not in (0, 7) and ly % 8 not in (0, 7):
                    rgb = (33, 200, 66) if (col + row) % 2 == 0 else (212, 82, 77)
            rows.extend(rgb)
    return b"P6\n1280 720\n255\n" + rows


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="raw cartridge path")
    parser.add_argument("--preview", type=Path, help="optional expected 1280x720 PPM")
    parser.add_argument("--pad-to", type=int, help="pad with FF to this raw size, at most 16384")
    parser.add_argument("--controllers", action="store_true",
                        help="poll and display raw SG-1000 DC/DD controller ports")
    def matrix_value(value: str) -> int:
        try:
            return int(value, 0)
        except ValueError:
            return int(value, 16)
    parser.add_argument("--matrix", type=matrix_value, default=0xFFFFFFFFFF,
                        help="preview-only active-low 40-bit keyboard matrix")
    args = parser.parse_args()
    if args.preview is not None and args.preview.resolve() == args.output.resolve():
        parser.error("cartridge and preview must have different paths")
    if not 0 <= args.matrix <= 0xFFFFFFFFFF:
        parser.error("--matrix must fit 40 bits")
    if not args.controllers and args.matrix != 0xFFFFFFFFFF:
        parser.error("--matrix requires --controllers")
    data = cartridge(args.controllers)
    if args.pad_to is not None:
        if not len(data) <= args.pad_to <= 16384:
            parser.error(f"--pad-to must be {len(data)}..16384")
        data += bytes([0xFF]) * (args.pad_to - len(data))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(data)
    if args.preview is not None:
        args.preview.parent.mkdir(parents=True, exist_ok=True)
        args.preview.write_bytes(preview(args.controllers, args.matrix))
    print(
        f"{args.output}: {len(data)} bytes, entry 0x0000, "
        f"sha256 {hashlib.sha256(data).hexdigest()}"
    )


if __name__ == "__main__":
    main()
