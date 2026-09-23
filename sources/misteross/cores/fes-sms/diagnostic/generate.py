#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""BIOS-free Master System Mode 4 diagnostic; see LICENSE for source/output.

The annotated instruction emitter below is the cartridge source. It needs only
Python's standard library, not an assembler or downloaded ROM. Reset enters
0x0000, then jumps to code at 0x4000 so a 32 KiB image actually executes in
the upper half. After the Mode 4 picture it programs an SN76489 square wave
on ports 0x7E/0x7F. Default output HALTs after the RAM signature (simulation).
--interactive keeps the controller poll loop for HIL display+USB+HDMI-audio
checks.
"""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path


UPPER = 0x4000
CART_MAX = 32768
# Distinctive upper-half payload. First plus-tile byte, also written to C002.
UPPER_MARK = 0x18
PLUS = bytes((0x18, 0x18, 0x7E, 0x7E, 0x18, 0x18, 0x00, 0x00))
HALT_TAIL = bytes((0x76, 0x18, 0xFD))  # HALT; JR back


def cartridge(interactive: bool = False) -> bytes:
    image = bytearray([0xFF] * UPPER)
    # DI; LD SP,DFF0; JP 4000
    image[0:7] = bytes((0xF3, 0x31, 0xF0, 0xDF, 0xC3, 0x00, 0x40))

    code = bytearray()
    tables: list[tuple[int, bytes]] = []
    marker_ptr = -1

    def pc() -> int:
        return UPPER + len(code)

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

    def cram_address(value: int) -> None:
        out(0xBF, value & 0x1F)
        out(0xBF, 0xC0)

    def jr_nz(target: int) -> None:
        displacement = target - (pc() + 2)
        assert -128 <= displacement <= 127
        emit(0x20, displacement & 0xFF)

    def djnz(target: int) -> None:
        displacement = target - (pc() + 2)
        assert -128 <= displacement <= 127
        emit(0x10, displacement & 0xFF)

    def copy(destination: int, data: bytes) -> None:
        address(destination)
        emit(0x21, 0, 0)  # LD HL,table (fixed up after code)
        tables.append((len(code) - 2, data))
        emit(0x11, len(data) & 0xFF, len(data) >> 8)  # LD DE,length
        loop = pc()
        emit(0x7E, 0xD3, 0xBE, 0x23, 0x1B, 0x7A, 0xB3)
        jr_nz(loop)

    def paint_bits(vram: int, source: int, count: int) -> None:
        # Name 1 = pressed (bit 0), name 2 = released. Reloads A after address().
        address(vram)
        emit(0x3A, source & 0xFF, source >> 8)  # LD A,(source)
        emit(0x4F, 0x06, count)  # LD C,A; LD B,count
        loop = pc()
        emit(0x79, 0xE6, 1)  # LD A,C; AND 1
        emit(0x20, 4)  # JR NZ, released
        emit(0x3E, 1, 0x18, 2)  # LD A,1; JR paint
        emit(0x3E, 2)  # released: LD A,2
        emit(0xD3, 0xBE, 0xAF, 0xD3, 0xBE)  # paint tile then zero attribute
        emit(0xCB, 0x39)  # SRL C
        djnz(loop)

    def mode4_tile(rows: tuple[tuple[int, ...], ...]) -> bytes:
        assert len(rows) == 8 and all(len(row) == 8 for row in rows)
        encoded = bytearray()
        for row in rows:
            for plane in range(4):
                value = 0
                for pixel, color in enumerate(row):
                    value |= ((color >> plane) & 1) << (7 - pixel)
                encoded.append(value)
        return bytes(encoded)

    emit(0xDB, 0xBF)  # IN A,(BF): clear the control latch and VBlank
    for index, value in enumerate(
        (0x04, 0x00, 0x0E, 0x00, 0x00, 0x7E, 0x00, 0x00, 0x00, 0x00, 0xFF)
    ):
        register(index, value)

    address(0x0000)
    emit(0x01, 0x00, 0x40)  # LD BC,4000: clear 16 KiB VRAM
    clear = pc()
    emit(0xAF, 0xD3, 0xBE, 0x0B, 0x78, 0xB1)
    jr_nz(clear)

    blank = tuple(tuple(0 for _ in range(8)) for _ in range(8))
    square1 = tuple(
        tuple(1 if x not in (0, 7) and y not in (0, 7) else 0 for x in range(8))
        for y in range(8)
    )
    square2 = tuple(
        tuple(2 if x not in (0, 7) and y not in (0, 7) else 0 for x in range(8))
        for y in range(8)
    )
    plus = tuple(
        tuple(3 if PLUS[y] & (0x80 >> x) else 0 for x in range(8))
        for y in range(8)
    )
    copy(0x0000, b"".join(mode4_tile(tile) for tile in (blank, square1, square2, plus)))
    cram_address(0)
    palette = bytes((0x00, 0x07, 0x1C, 0x3F) + (0x00,) * 12 +
                    (0x00, 0x34, 0x0F, 0x3F) + (0x00,) * 12)
    emit(0x21, 0, 0)
    tables.append((len(code) - 2, palette))
    emit(0x11, len(palette), 0x00)
    palette_loop = pc()
    emit(0x7E, 0xD3, 0xBE, 0x23, 0x1B, 0x7A, 0xB3)
    jr_nz(palette_loop)
    names = bytearray()
    for row in range(24):
        for col in range(32):
            tile = 3 if col in (0, 31) or row in (0, 23) else 1 + ((col + row) & 1)
            names.extend((tile, 0))
    for row in (11, 12):
        for col in (15, 16):
            attribute = ((col - 15) << 1) | ((row - 11) << 2)
            if row == 12 and col == 16:
                attribute |= 0x18
            offset = (row * 32 + col) * 2
            names[offset : offset + 2] = bytes((3, attribute))
    copy(0x3800, bytes(names))
    copy(0x3F00, bytes((0xD0,)))
    register(1, 0x40)
    emit(0x3E, 0xA5, 0x32, 0x00, 0xC0)  # LD A,A5; LD (C000),A
    emit(0x3A, 0, 0)  # LD A,(upper mark)
    marker_ptr = len(code) - 2
    emit(0x32, 0x02, 0xC0)  # LD (C002),A
    emit(0xDB, 0xDC, 0x32, 0x01, 0xC0)  # IN A,(DC); LD (C001),A
    # SN76489 tone 0 period 256 (~400 Hz), max volume; other channels silent.
    out(0x7F, 0x80)
    out(0x7E, 0x10)
    out(0x7F, 0x90)
    out(0x7F, 0xBF)
    out(0x7F, 0xDF)
    out(0x7F, 0xFF)
    if interactive:
        poll = pc()
        emit(0xDB, 0xDC, 0x32, 0x01, 0xC0)
        paint_bits(0x3948, 0xC001, 8)
        emit(0xDB, 0xDD, 0x32, 0x03, 0xC0)
        paint_bits(0x39C8, 0xC003, 2)
        emit(0xC3, poll & 0xFF, poll >> 8)
    else:
        emit(*HALT_TAIL)

    for pointer, data in tables:
        location = pc()
        code[pointer : pointer + 2] = location.to_bytes(2, "little")
        code.extend(data)
    marker_at = pc()
    code[marker_ptr : marker_ptr + 2] = marker_at.to_bytes(2, "little")
    code.append(UPPER_MARK)

    image.extend(code)
    assert UPPER < len(image) <= CART_MAX
    assert image[UPPER] == 0xDB
    return bytes(image)


def _cram_rgb(value: int) -> tuple[int, int, int]:
    red = value & 3
    green = (value >> 2) & 3
    blue = (value >> 4) & 3

    def expand(channel: int) -> int:
        return (channel << 6) | (channel << 4) | (channel << 2) | channel

    return (expand(red), expand(green), expand(blue))


_PALETTE0 = (0x00, 0x07, 0x1C, 0x3F)
_PALETTE1 = (0x00, 0x34, 0x0F, 0x3F)


def _plus_color(px: int, py: int, hflip: bool, vflip: bool) -> int:
    sample_x = 7 - px if hflip else px
    sample_y = 7 - py if vflip else py
    return 3 if PLUS[sample_y] & (0x80 >> sample_x) else 0


def preview(interactive: bool = False) -> bytes:
    """720p PPM reference for the SMS 720p shell, including read latency."""
    backdrop = _cram_rgb(_PALETTE1[0])
    rows = bytearray()
    for y in range(720):
        for x in range(1280):
            rgb = (0, 0, 0)
            if 384 <= x < 896 and 168 <= y < 552:
                lx, ly = max(0, x - 385) // 2, (y - 168) // 2
                col, row = lx // 8, ly // 8
                px, py = lx % 8, ly % 8
                palette = _PALETTE0
                color = 0
                if col in (0, 31) or row in (0, 23):
                    color = _plus_color(px, py, False, False)
                elif col in (15, 16) and row in (11, 12):
                    color = _plus_color(px, py, col == 16, row == 12)
                    if row == 12 and col == 16:
                        palette = _PALETTE1
                elif interactive and row == 5 and 4 <= col <= 11:
                    color = 2
                elif interactive and row == 7 and 4 <= col <= 5:
                    color = 2
                elif px not in (0, 7) and py not in (0, 7):
                    color = 1 + ((col + row) & 1)
                rgb = backdrop if color == 0 else _cram_rgb(palette[color])
            rows.extend(rgb)
    return b"P6\n1280 720\n255\n" + bytes(rows)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="raw cartridge path")
    parser.add_argument("--preview", type=Path, help="optional expected 1280x720 PPM")
    parser.add_argument("--hex-output", type=Path, help="optional Verilog byte memory file")
    parser.add_argument(
        "--pad-to",
        type=int,
        help=f"pad with FF to this raw size, at most {CART_MAX}",
    )
    parser.add_argument(
        "--interactive",
        action="store_true",
        help="HIL ROM: keep the controller poll loop (do not HALT forever)",
    )
    args = parser.parse_args()
    if args.preview is not None and args.preview.resolve() == args.output.resolve():
        parser.error("cartridge and preview must have different paths")
    data = cartridge(args.interactive)
    if args.pad_to is not None:
        if not len(data) <= args.pad_to <= CART_MAX:
            parser.error(f"--pad-to must be {len(data)}..{CART_MAX}")
        data += bytes([0xFF]) * (args.pad_to - len(data))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(data)
    if args.hex_output is not None:
        args.hex_output.parent.mkdir(parents=True, exist_ok=True)
        args.hex_output.write_text("".join(f"{byte:02x}\n" for byte in data))
    if args.preview is not None:
        args.preview.parent.mkdir(parents=True, exist_ok=True)
        args.preview.write_bytes(preview(args.interactive))
    kind = "HIL interactive" if args.interactive else "sim HALT"
    print(
        f"{args.output}: {len(data)} bytes, {kind}, entry 0x0000 JP 0x4000, "
        f"sha256 {hashlib.sha256(data).hexdigest()}"
    )


if __name__ == "__main__":
    main()
