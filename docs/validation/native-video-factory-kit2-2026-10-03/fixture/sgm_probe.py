#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2026 FES contributors
"""Original BIOS-free SGM cartridge probe for the development v2 Coleco shell.

The probe runs before the existing Graphics I checkerboard. Any failed RAM or
AY readback loops before video initialization, so a pass frame cannot appear.
The cartridge also leaves AY and SN tones running for an audio capture.
"""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path

from generate import cartridge as graphics_cartridge


def cartridge() -> bytes:
    rom = bytearray(graphics_cartridge())
    # Preserve all of the graphics cartridge's absolute table addresses. Its
    # first four bytes are DI; LD SP,6400. A new entry trampoline executes
    # those instructions before the checks and returns at the next opcode.
    assert rom[:4] == bytes((0xF3, 0x31, 0x00, 0x64))
    entry = 0x8000 + len(rom)
    rom[:3] = bytes((0xC3, entry & 0xFF, entry >> 8))
    probe = bytearray()

    def emit(*values: int) -> None:
        probe.extend(values)

    def out(port: int, value: int) -> None:
        emit(0x3E, value, 0xD3, port)

    def reject_if_not(value: int) -> None:
        emit(0xFE, value)  # CP value
        here = entry + len(probe)
        emit(0xC2, here & 0xFF, here >> 8)  # JP NZ,self

    def check_memory(address: int, value: int) -> None:
        emit(0x3A, address & 0xFF, address >> 8)  # LD A,(address)
        reject_if_not(value)

    def write_memory(address: int, value: int) -> None:
        emit(0x3E, value, 0x32, address & 0xFF, address >> 8)
        check_memory(address, value)

    def ay_register(index: int, value: int) -> None:
        out(0x50, index)
        out(0x51, value)
        emit(0xDB, 0x52)  # IN A,(52)
        reject_if_not(value)

    emit(0xF3, 0x31, 0x00, 0x64)  # DI; LD SP,6400
    write_memory(0x6000, 0x66)  # physical console-RAM sentinel
    out(0x53, 1)  # enable $2000-$7FFF expansion RAM
    for address, value in ((0x2000, 0xA5), (0x5FFF, 0x5A),
                           (0x6000, 0xC3), (0x7FFF, 0x3C)):
        write_memory(address, value)
    out(0x53, 0)
    check_memory(0x6000, 0x66)  # SGM writes must not mirror into console RAM
    out(0x7F, 0)  # enable $0000-$1FFF expansion RAM
    for address, value in ((0x0000, 0x11), (0x1FFF, 0x22)):
        write_memory(address, value)
    out(0x7F, 2)

    # Tone A near 440 Hz; register reads test the direct AY response edge.
    for index, value in ((0, 0xFE), (1, 0), (7, 0x3E), (8, 0x0F)):
        ay_register(index, value)
    for value in (0x84, 0x20, 0x90):
        out(0xE0, value)  # SN tone A and maximum channel volume
    emit(0xC3, 0x04, 0x80)  # resume original graphics cartridge after its prologue
    rom.extend(probe)
    assert len(rom) <= 16384
    return bytes(rom)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    payload = cartridge()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(payload)
    print(f"{args.output}: {len(payload)} bytes, sha256 {hashlib.sha256(payload).hexdigest()}")


if __name__ == "__main__":
    main()
