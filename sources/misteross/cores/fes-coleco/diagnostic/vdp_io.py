#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""BIOS-free CPU VDP I/O/NMI diagnostic; raw entry 8000, NMI handler 8066.

RAM: 6000=00 running/A5 pass/E1..E7 failure, 6001=NMI count,
6002/3=handler status reads, 6004=handler error, 6010..14=data reads.
Pass: green one-tile border, black interior. Failure: same border with an
orange interior. CPU paints either result and only then HALTs. Timeout is
never success. Source and generated output use diagnostic/LICENSE (MIT).
"""
import argparse
import hashlib
from pathlib import Path


def cartridge():
    code = bytearray()
    labels = {}
    fixups = []

    def emit(*values):
        code.extend(values)

    def label(name):
        labels[name] = 0x8000 + len(code)

    def jump(opcode, name):
        emit(opcode, 0, 0)
        fixups.append((len(code)-2, name))

    def out(port, value):
        emit(0x3e, value, 0xd3, port)

    def store(address):
        emit(0x32, address & 255, address >> 8)

    def load(address):
        emit(0x3a, address & 255, address >> 8)

    def register(index, value):
        out(0xbf, value)
        out(0xbf, 0x80 | index)

    def address(value, write=True):
        out(0xbf, value & 255)
        out(0xbf, (0x40 if write else 0) | (value >> 8))

    def countdown(name, count):
        emit(0x01, count & 255, count >> 8)  # LD BC,count
        label(name)

    def next_count(name):
        emit(0x0b, 0x78, 0xb1)  # DEC BC; LD A,B; OR C
        jump(0xc2, name)

    jump(0xc3, "start")
    code.extend(bytes(0x66-len(code)))
    label("nmi")
    emit(0xf5, 0xdb, 0xbf)  # PUSH AF; IN A,(BF), snapshot then clear
    store(0x6002)
    emit(0xe6, 0x80)
    jump(0xca, "handler_error")
    emit(0xdb, 0xbf)
    store(0x6003)
    emit(0xe6, 0x80)
    jump(0xca, "handler_count")
    label("handler_error")
    emit(0x3e, 1)
    store(0x6004)
    label("handler_count")
    load(0x6001)
    emit(0x3c)  # INC A
    store(0x6001)
    emit(0xf1, 0xed, 0x45)  # POP AF; RETN

    label("start")
    emit(0xf3, 0x31, 0x00, 0x64, 0xaf)  # DI; SP=6400; XOR A
    for ram in range(0x6000, 0x6005):
        store(ram)
    emit(0xdb, 0xbf)
    register(1, 0x80)  # NMI disabled throughout initial read/status checks
    address(0x1234)
    for value in (0x19, 0xa6, 0x73):
        out(0xbe, value)
    address(0x1234, False)  # read-address command must prefetch first byte
    for index, value in enumerate((0x19, 0xa6, 0x73)):
        emit(0xdb, 0xbe)
        store(0x6010 + index)
        emit(0xfe, value)
        jump(0xc2, "fail1")
    address(0x3fff)
    out(0xbe, 0x42)
    out(0xbe, 0xbd)  # auto-increment wraps to 0000
    address(0x3fff, False)
    for index, value in enumerate((0x42, 0xbd)):
        emit(0xdb, 0xbe)
        store(0x6013 + index)
        emit(0xfe, value)
        jump(0xc2, "fail2")

    countdown("wait_status", 65535)
    emit(0xdb, 0xbf, 0xe6, 0x80)
    jump(0xc2, "status_seen")
    next_count("wait_status")
    jump(0xc3, "fail3")
    label("status_seen")
    emit(0xdb, 0xbf, 0xe6, 0x80)
    jump(0xc2, "fail4")
    # >2 natural raster frames at production clock enables, with IRQ off.
    # Do not read status during the delay: leave a pending VBlank to enable.
    # 6144 * 24 Z80 T-states * 16 system clocks is about 2.20 frames.
    # Starting just after VBlank leaves ~0.8 frame before the next VBlank;
    # the first-NMI timeout below is <0.1 frame, excluding a late-edge pass.
    countdown("pending_delay", 6144)
    next_count("pending_delay")
    load(0x6001)
    emit(0xb7)  # OR A
    jump(0xc2, "fail5")
    register(1, 0xa0)  # pending status must assert NMI immediately on enable
    # <one frame from enable, so waiting for a later VBlank cannot pass.
    countdown("first_nmi", 128)
    load(0x6001)
    emit(0xb7)
    jump(0xc2, "first_seen")
    next_count("first_nmi")
    jump(0xc3, "fail5")
    label("first_seen")
    countdown("second_nmi", 8192)
    load(0x6001)
    emit(0xfe, 2)
    jump(0xd2, "second_seen")  # JP NC: count >=2
    next_count("second_nmi")
    jump(0xc3, "fail6")
    label("second_seen")
    register(1, 0x80)
    load(0x6001)
    emit(0xfe, 2)
    jump(0xc2, "fail7")
    load(0x6004)
    emit(0xb7)
    jump(0xc2, "fail7")
    emit(0x1e, 0xa5, 0x16, 0)  # E=result; D=interior pattern (black)
    jump(0xc3, "paint")
    for reason in range(1, 8):
        label(f"fail{reason}")
        emit(0x1e, 0xe0 + reason, 0x16, 0xff)  # orange interior
        jump(0xc3, "paint")

    label("paint")
    register(1, 0x80)
    emit(0xdb, 0xbf)
    # Every location consulted by the final raster is initialized by CPU OUT.
    for index, value in enumerate((0, 0x80, 0, 0x80, 1, 0x36, 3, 1)):
        register(index, value)
    address(0x0800)
    for _ in range(8):
        out(0xbe, 0xff)
    for _ in range(8):
        emit(0x7a, 0xd3, 0xbe)  # LD A,D; OUT interior pattern
    address(0x2000)
    out(0xbe, 0xf1)
    out(0xbe, 0x01)
    address(0x1b00)
    out(0xbe, 0xd0)
    address(0)
    for row in range(24):
        for col in range(32):
            out(0xbe, 0 if row in (0, 23) or col in (0, 31) else 1)
    register(1, 0xc0)  # display on, NMI off
    emit(0x7b)  # publish only after complete picture
    store(0x6000)
    emit(0x76)
    jump(0xc3, "halt")
    # Re-enter HALT if an unexpected interrupt ever wakes it.
    labels["halt"] = 0x8000 + len(code) - 4
    for offset, name in fixups:
        code[offset:offset+2] = labels[name].to_bytes(2, "little")
    if len(code) % 2 == 0:
        emit(0xff)
    assert len(code) <= 16384 and labels["nmi"] == 0x8066
    return bytes(code)


def preview():
    pixels = bytearray()
    for y in range(720):
        for x in range(1280):
            border = False
            if 384 <= x < 896 and 168 <= y < 552:
                # Preserve the established one-pixel framebuffer latency.
                col = max(0, x-385) // 16
                row = (y-168) // 16
                border = col in (0, 31) or row in (0, 23)
            pixels.extend(b"\x00\xff\x40" if border else b"\x00\x00\x00")
    return b"P6\n1280 720\n255\n" + pixels


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--pad-to", type=int)
    parser.add_argument("--preview", type=Path, help="720p reference for PASS only")
    args = parser.parse_args()
    data = cartridge()
    if args.preview is not None and args.preview.resolve() == args.output.resolve():
        parser.error("cartridge and preview must have different paths")
    if args.pad_to is not None:
        if not len(data) <= args.pad_to <= 16384:
            parser.error(f"--pad-to must be {len(data)}..16384")
        data += b"\xff" * (args.pad_to-len(data))
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(data)
    if args.preview is not None:
        args.preview.parent.mkdir(parents=True, exist_ok=True)
        args.preview.write_bytes(preview())
    print(f"{args.output}: {len(data)} bytes sha256 {hashlib.sha256(data).hexdigest()}")


if __name__ == "__main__":
    main()
