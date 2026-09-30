#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Write an open 8 KiB ZX81 firmware diagnostic: Zon X channel A tone/mute.

Runs without Sinclair BASIC. Programs DF/0F via OUT (C),A with B=0, then
alternates volume 15/0 approximately every 2.1 seconds. The bounded cart uses
an eight-bit period of 255 and /16 prescale: 52.224 MHz / (16*256*2) = 6375 Hz.
No display file is generated; a blank active HDMI frame is expected.
"""
from pathlib import Path
import argparse

def make_rom() -> bytes:
    code = bytearray([0xf3, 0x31, 0x00, 0x44])  # DI; LD SP,4400
    labels = {}; fixes = []
    def mark(name): labels[name] = len(code)
    def emit(*values): code.extend(values)
    def write(reg, value):
        emit(0x01, 0xdf, 0x00, 0x3e, reg, 0xed, 0x79,
             0x0e, 0x0f, 0x3e, value, 0xed, 0x79)
    def call(name):
        emit(0xcd); fixes.append((len(code), name, False)); emit(0, 0)
    def relative(opcode, name):
        emit(opcode); fixes.append((len(code), name, True)); emit(0)
    write(0, 15); write(1, 15); write(7, 0x3e)
    mark('repeat'); write(8, 15); call('delay'); write(8, 0); call('delay')
    emit(0xc3); fixes.append((len(code), 'repeat', False)); emit(0, 0)
    mark('delay'); emit(0x06, 4)  # LD B,4
    mark('outer'); emit(0x11, 0xff, 0xff)  # LD DE,ffff
    mark('inner'); emit(0x1b, 0x7a, 0xb3)  # DEC DE; LD A,D; OR E
    relative(0x20, 'inner'); relative(0x10, 'outer'); emit(0xc9)
    for offset, label, rel in fixes:
        address = labels[label]
        if rel:
            delta = address - offset - 1
            if not -128 <= delta <= 127: raise ValueError('relative jump out of range')
            code[offset] = delta & 255
        else: code[offset:offset+2] = address.to_bytes(2, 'little')
    return bytes(code) + bytes([0xff]) * (8192 - len(code))

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', type=Path)
    args = parser.parse_args(); args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_bytes(make_rom())
