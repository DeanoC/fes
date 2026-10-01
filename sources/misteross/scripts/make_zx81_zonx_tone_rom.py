#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Write an open 8 KiB Zon X AY diagnostic, without Sinclair BASIC.

OUT (C),A programs the original CF/0F write-only board. Each repeat contains
mute, isolated A/B/C (~400/600/800 Hz), mixed tones, noise, falling/rising/
triangle/held envelopes, envelope retrigger and final mute. A phase marker
at 4000 is for CPU simulation only. No display file is generated.
"""
from pathlib import Path
import argparse

PHASES = ('mute', 'tone-a', 'tone-b', 'tone-c', 'tones-abc', 'noise',
          'decay', 'rise', 'triangle', 'hold-high', 'retrigger', 'mute-final')
TONE_PERIODS = (254, 169, 127)  # 1.625 MHz / (16*period)


def make_rom(*, fast: bool = False) -> bytes:
    code = bytearray([0xf3, 0x31, 0x00, 0x44])  # DI; LD SP,4400
    labels = {}; fixes = []
    def mark(name): labels[name] = len(code)
    def emit(*values): code.extend(values)
    def write(reg, value):
        emit(0x01, 0xcf, 0x00, 0x3e, reg, 0xed, 0x79,
             0x0e, 0x0f, 0x3e, value, 0xed, 0x79)
    def call(name):
        emit(0xcd); fixes.append((len(code), name, False)); emit(0, 0)
    def relative(opcode, name):
        emit(opcode); fixes.append((len(code), name, True)); emit(0)
    def marker(name):
        emit(0x3e, PHASES.index(name), 0x32, 0x00, 0x40)
    def phase(name):
        marker(name); call('delay')
    def mute():
        for reg in (8, 9, 10): write(reg, 0)
    mark('repeat')
    for reg in range(14): write(reg, 0)
    phase('mute')
    for channel, period in enumerate(TONE_PERIODS):
        write(2*channel, period & 255); write(2*channel+1, period >> 8)
    write(7, 0x38)
    for channel in range(3):
        write(8+channel, 15); phase('tone-' + 'abc'[channel]); write(8+channel, 0)
    for channel in range(3): write(8+channel, 15)
    phase('tones-abc'); mute()
    write(6, 17); write(7, 0x37); write(8, 15); phase('noise')
    write(7, 0x3e); write(8, 16)
    period = 64 if fast else 4096
    write(11, period & 255); write(12, period >> 8)
    for shape, name in ((0, 'decay'), (4, 'rise'), (10, 'triangle'), (13, 'hold-high')):
        write(13, shape); phase(name)
    # Start decay, let it advance, then repeat the unchanged R13 value.
    marker('retrigger')
    write(13, 0); call('delay-half'); write(13, 0); call('delay')
    mute(); phase('mute-final')
    emit(0xc3); fixes.append((len(code), 'repeat', False)); emit(0, 0)
    mark('delay-half'); emit(0x06, 1)
    emit(0x11, 0xff, 0x03 if fast else 0xff)
    mark('half-inner'); emit(0x1b, 0x7a, 0xb3)
    relative(0x20, 'half-inner'); emit(0xc9)
    mark('delay'); emit(0x06, 1 if fast else 2)
    mark('outer'); emit(0x11, 0xff, 0x07 if fast else 0xff)
    mark('inner'); emit(0x1b, 0x7a, 0xb3)
    relative(0x20, 'inner'); relative(0x10, 'outer'); emit(0xc9)
    for offset, label, rel in fixes:
        address = labels[label]
        if rel:
            delta = address - offset - 1
            if not -128 <= delta <= 127: raise ValueError('relative jump out of range')
            code[offset] = delta & 255
        else: code[offset:offset+2] = address.to_bytes(2, 'little')
    if len(code) > 8192: raise ValueError('diagnostic exceeds sealed ROM requirement')
    return bytes(code) + bytes([0xff]) * (8192 - len(code))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('output', type=Path)
    parser.add_argument('--fast', action='store_true', help='short phases for CPU simulation')
    parser.add_argument('--hex', action='store_true', help='Verilog memory initialization instead of raw ROM')
    args = parser.parse_args(); args.output.parent.mkdir(parents=True, exist_ok=True)
    data = make_rom(fast=args.fast)
    if args.hex: args.output.write_text(''.join(f'{byte:02x}\n' for byte in data))
    else: args.output.write_bytes(data)
