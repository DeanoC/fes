#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""Independent reference renderer for the FES Apple II HDMI picture.

It reimplements the video rules documented in rtl/apple2_video.v from the
screen memory the open diagnostic writes, and emits complete 1280x720 PPM
frames for the machine simulation to compare byte for byte. Text frames that
contain flashing characters are written twice (`NAME.ppm` with flashing
characters normal, `NAME-flash.ppm` inverted).
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import firmware  # noqa: E402
from font import table as font_table  # noqa: E402

WIDTH, HEIGHT = 1280, 720
LEFT, TOP = 80, 72
PALETTE = (0x000000, 0xE31E60, 0x604EBD, 0xFF44FD, 0x00A360, 0x9C9C9C, 0x14CFFD, 0xD0C3FF,
           0x607203, 0xFF6A3C, 0x9C9C9C, 0xFFA0D0, 0x14F53C, 0xD0DD8D, 0x72FFD0, 0xFFFFFF)
FONT = font_table()


class Memory:
    def __init__(self) -> None:
        self.ram = bytearray(0xC000)

    def text_row(self, row: int, page2: bool = False) -> int:
        return (0x0800 if page2 else 0x0400) + (row & 7) * 128 + (row >> 3) * 40

    def put_text(self, col: int, row: int, codes: list[int]) -> None:
        for code in codes:
            self.ram[self.text_row(row) + col] = code
            col += 1
            if col == 40:
                col, row = 0, row + 1


def render(mem: Memory, text: bool, mixed: bool, hires: bool, flash: bool) -> bytes:
    frame = bytearray(WIDTH * HEIGHT * 3)
    for ay in range(192):
        line_text = text or (mixed and ay >= 160)
        row = ay >> 3
        base_text = mem.text_row(row)
        base_hgr = 0x2000 + (ay & 7) * 1024 + ((ay >> 3) & 7) * 128 + (ay >> 6) * 40
        pixels: list[int] = []
        window = [0, 0, 0, 0]
        prev_dot6 = 0
        for col in range(40):
            if line_text:
                code = mem.ram[base_text + col]
                glyph = FONT[(code & 0x3F) * 8 + (ay & 7)]
                inverse = code < 0x40 or (code < 0x80 and flash)
                for dot in range(7):
                    on = ((glyph >> dot) & 1) ^ inverse
                    pixels.extend([0xFFFFFF if on else 0] * 4)
            elif not hires:
                value = mem.ram[base_text + col]
                nibble = value >> 4 if ay & 4 else value & 15
                pixels.extend([PALETTE[nibble]] * 28)
            else:
                value = mem.ram[base_hgr + col]
                for half in range(14):
                    if not value & 0x80:
                        dot = (value >> (half >> 1)) & 1
                    elif half == 0:
                        dot = prev_dot6
                    else:
                        dot = (value >> ((half - 1) >> 1)) & 1
                    phase = (((col & 1) << 1) + half) & 3
                    window[phase] = dot
                    colour = PALETTE[window[0] | window[1] << 1 | window[2] << 2 | window[3] << 3]
                    pixels.extend([colour] * 2)
                prev_dot6 = (value >> 6) & 1
        for sub in range(3):
            y = TOP + ay * 3 + sub
            offset = (y * WIDTH + LEFT) * 3
            for x, colour in enumerate(pixels):
                frame[offset + x * 3:offset + x * 3 + 3] = colour.to_bytes(3, "big")
    return bytes(frame)


def diagnostic_screens() -> dict[str, tuple[Memory, bool, bool, bool, bool]]:
    """Screen memory after each machine-simulation step."""
    mem = Memory()

    def textscr() -> None:
        for row in range(24):
            mem.put_text(0, row, [0xA0] * 40)
        for col, row, text, mode in firmware.BANNER:
            mem.put_text(col, row, [firmware.screen_code(c, mode) for c in text])
        mem.put_text(0, firmware.CHARSET_ROW, [0x80 | i for i in range(64)] + list(range(64)))

    def message(col: int, row: int, text: str) -> None:
        mem.put_text(col, row, [firmware.screen_code(c) for c in text])

    def lores() -> None:
        for row in range(24):
            for col in range(40):
                mem.ram[mem.text_row(row) + col] = firmware.lores_byte(col, row)

    screens: dict[str, tuple[Memory, bool, bool, bool, bool]] = {}

    def snapshot(name: str, text: bool, mixed: bool, hires: bool, flashes: bool) -> None:
        copy = Memory()
        copy.ram[:] = mem.ram
        screens[name] = (copy, text, mixed, hires, flashes)

    textscr()
    message(0, firmware.CHARSET_ROW + 5, "RAM OK")
    message(0, firmware.CHARSET_ROW + 6, "LANGUAGE CARD OK")
    message(0, firmware.CHARSET_ROW + 8, "KEYS T L H M B")
    snapshot("text", True, False, False, True)
    lores()
    snapshot("lores", False, False, False, False)
    for line in range(192):
        base = firmware.hires_row_base(line)
        for col in range(40):
            mem.ram[base + col] = firmware.hires_byte(col)
    snapshot("hires", False, False, True, False)
    lores()
    for row in range(firmware.MESSAGE_ROW, 24):
        mem.put_text(0, row, [0xA0] * 40)
    message(0, firmware.MESSAGE_ROW + 1, firmware.MIXED_TEXT)
    snapshot("mixed", False, True, False, False)
    textscr()
    snapshot("text2", True, False, False, True)
    message(0, firmware.MESSAGE_ROW, "A")
    message(0, 22, firmware.DISK_PASS)
    snapshot("disk", True, False, False, True)
    return screens


def write_ppm(path: Path, frame: bytes) -> None:
    path.write_bytes(f"P6\n{WIDTH} {HEIGHT}\n255\n".encode() + frame)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    for name, (mem, text, mixed, hires, flashes) in diagnostic_screens().items():
        write_ppm(args.output_dir / f"{name}.ppm", render(mem, text, mixed, hires, False))
        if flashes:
            write_ppm(args.output_dir / f"{name}-flash.ppm", render(mem, text, mixed, hires, True))


if __name__ == "__main__":
    main()
