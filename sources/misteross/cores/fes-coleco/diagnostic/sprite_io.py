#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""BIOS-free CPU Graphics II sprite diagnostic; raw entry 8000.

The cartridge first places five 8x8 sprites on one line and polls the VDP
status until it observes collision plus the fifth-sprite index. It then
publishes a static 16x16 magnified picture that also exercises early-clock and
right-edge clipping. Source and generated output use diagnostic/LICENSE.
"""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path


def cartridge() -> bytes:
    code = bytearray()
    labels: dict[str, int] = {}
    fixups: list[tuple[int, str]] = []
    tables: list[tuple[int, bytes]] = []

    def emit(*values: int) -> None:
        code.extend(values)

    def label(name: str) -> None:
        labels[name] = 0x8000 + len(code)

    def jump(opcode: int, name: str) -> None:
        emit(opcode, 0, 0)
        fixups.append((len(code) - 2, name))

    def jr_nz(loop: int) -> None:
        displacement = loop - (len(code) + 2)
        assert -128 <= displacement <= 127
        emit(0x20, displacement & 0xff)

    def out(port: int, value: int) -> None:
        emit(0x3e, value, 0xd3, port)

    def store(address: int) -> None:
        emit(0x32, address & 0xff, address >> 8)

    def register(index: int, value: int) -> None:
        out(0xbf, value)
        out(0xbf, 0x80 | index)

    def address(value: int) -> None:
        out(0xbf, value & 0xff)
        out(0xbf, 0x40 | ((value >> 8) & 0x3f))

    def copy(destination: int, data: bytes) -> None:
        address(destination)
        emit(0x21, 0, 0)  # LD HL,table (fixed after code)
        tables.append((len(code) - 2, data))
        emit(0x11, len(data) & 0xff, len(data) >> 8)  # LD DE,length
        loop = len(code)
        emit(0x7e, 0xd3, 0xbe, 0x23, 0x1b, 0x7a, 0xb3)
        # LD A,(HL); OUT (BE),A; INC HL; DEC DE; LD A,D; OR E
        jr_nz(loop)

    def countdown(name: str, count: int) -> None:
        emit(0x01, count & 0xff, count >> 8)  # LD BC,count
        label(name)

    def next_count(name: str) -> None:
        emit(0x0b, 0x78, 0xb1)  # DEC BC; LD A,B; OR C
        jump(0xc2, name)       # JP NZ,name

    jump(0xc3, "start")
    code.extend(bytes(0x66 - len(code)))

    label("start")
    emit(0xf3, 0x31, 0x00, 0x64, 0xaf)  # DI; SP=6400; XOR A
    for address_value in range(0x6000, 0x6004):
        store(address_value)

    # Display is held off while the CPU creates deterministic VRAM contents.
    for index, value in enumerate((0x00, 0x80, 0x0f, 0x80, 0x02, 0x36, 0x01, 0x00)):
        register(index, value)

    address(0x0000)
    emit(0x01, 0x00, 0x40)  # LD BC,4000: clear all 16 KiB of VRAM
    clear_loop = len(code)
    emit(0xaf, 0xd3, 0xbe, 0x0b, 0x78, 0xb1)
    # XOR A; OUT (BE),A; DEC BC; LD A,B; OR C
    jr_nz(clear_loop)

    # Sprite pattern 3 is deliberately stored at 0800; 16x16 mode ignores
    # its low two name bits. Pattern row 0 has one set bit at its left edge.
    copy(0x0800, bytes((0x80, 0x00)))
    # Background name 0 is a solid green border; name 8 is black (a separate Graphics I color group).
    copy(0x1000, bytes((0xff,) * 8))
    copy(0x1040, bytes(8))
    copy(0x2000, bytes((0x21, 0x11)))
    names = bytes(
        0 if col in (0, 31) or row in (0, 23) else 8
        for row in range(24) for col in range(32)
    )
    copy(0x3c00, names)

    # Five visible 8x8 sprites: 0/1 overlap for collision, 4 is the first
    # suppressed sprite and must report index 4 in status bits 4..0.
    initial_sprites = bytes((
        40, 48, 0, 0x06,
        40, 48, 0, 0x02,
        40, 80, 0, 0x02,
        40, 112, 0, 0x06,
        40, 144, 0, 0x02,
        0xd0, 0, 0, 0,
    ))
    copy(0x1b00, initial_sprites)

    countdown("wait_status", 0xffff)
    emit(0xdb, 0xbf, 0x32, 0x02, 0x60)  # IN A,(BF); save raw status
    emit(0xe6, 0x60, 0xfe, 0x60)        # keep collision/fifth bits
    jump(0xc2, "status_retry")         # JP NZ,status_retry
    emit(0x3a, 0x02, 0x60)              # LD A,(6002); inspect fifth index
    emit(0xe6, 0x1f, 0xfe, 0x04)
    jump(0xca, "status_ok")            # JP Z,status_ok
    label("status_retry")
    next_count("wait_status")
    jump(0xc3, "fail")

    label("status_ok")
    emit(0x3e, 0xa5)
    store(0x6000)

    # Publish the final static picture after the status sample. The two first
    # sprites exercise early-clock and right-edge clipping; the third is green
    # at x=10 so the image exposes a non-red sprite as well.
    final_sprites = bytes((
        80, 220, 3, 0x86,
        100, 255, 3, 0x06,
        130, 10, 3, 0x02,
        0xd0, 0, 0, 0,
    ))
    copy(0x1b00, final_sprites)
    register(1, 0xc3)  # display on, 16 KiB, 16x16 sprites, magnified
    emit(0x76)          # HALT: the board test treats this as completion

    label("fail")
    emit(0x3e, 0xe1)
    store(0x6000)
    emit(0x76)

    for pointer, data in tables:
        location = 0x8000 + len(code)
        code[pointer:pointer + 2] = location.to_bytes(2, "little")
        code.extend(data)
    if len(code) % 2 == 0:
        emit(0xff)
    assert 1 <= len(code) <= 16384
    for pointer, name in fixups:
        code[pointer:pointer + 2] = labels[name].to_bytes(2, "little")
    return bytes(code)


def preview() -> bytes:
    """Return the expected fixed-720p image, including framebuffer read delay."""
    pixels = bytearray()
    for y in range(720):
        for x in range(1280):
            rgb = (0, 0, 0)
            if 384 <= x < 896 and 168 <= y < 552:
                logical_x = max(0, x - 385) // 2
                logical_y = (y - 168) // 2
                col, row = logical_x // 8, logical_y // 8
                if (logical_x, logical_y) in {
                    (188, 81), (189, 81), (188, 82), (189, 82),
                    (255, 101), (255, 102),
                }:
                    rgb = (212, 82, 77)  # sprite color 6 / pixel 6
                elif (logical_x, logical_y) in {
                    (10, 131), (11, 131), (10, 132), (11, 132),
                }:
                    rgb = (33, 200, 66)  # sprite color 2 / pixel 2
                elif col in (0, 31) or row in (0, 23):
                    rgb = (33, 200, 66)  # Graphics I pixel 2
            pixels.extend(rgb)
    return b"P6\n1280 720\n255\n" + pixels


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--pad-to", type=int)
    parser.add_argument("--preview", type=Path)
    args = parser.parse_args()
    if args.preview is not None and args.preview.resolve() == args.output.resolve():
        parser.error("cartridge and preview must have different paths")
    data = cartridge()
    if args.pad_to is not None:
        if not len(data) <= args.pad_to <= 16384:
            parser.error(f"--pad-to must be {len(data)}..16384")
        data += b"\xff" * (args.pad_to - len(data))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(data)
    if args.preview is not None:
        args.preview.parent.mkdir(parents=True, exist_ok=True)
        args.preview.write_bytes(preview())
    print(f"{args.output}: {len(data)} bytes sha256 {hashlib.sha256(data).hexdigest()}")


if __name__ == "__main__":
    main()
