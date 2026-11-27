#!/usr/bin/env python3
"""Write a one-line auto-running BASIC tape for the Spectrum HIL (F3 D12).

    10 BORDER 1: PAUSE 0

A loaded tape turns the border blue and waits for a key, so an HDMI grab shows
that the ROM loaded it and the run went through the core's tape path. Written
here so the HIL carries no third-party tape.

usage: make_tap.py OUT.tap
"""
import struct
import sys


def number(n):
    # The ROM's 5-byte small-integer form after the digits.
    return bytes([0x0E, 0, 0, n & 0xFF, n >> 8, 0])


def block(flag, body):
    data = bytes([flag]) + body
    check = 0
    for b in data:
        check ^= b
    data += bytes([check])
    return struct.pack('<H', len(data)) + data


BORDER, PAUSE = 0xE7, 0xF2
line = bytes([BORDER]) + b'1' + number(1) + b':' + bytes([PAUSE]) + b'0' + number(0) + b'\r'
program = struct.pack('>H', 10) + struct.pack('<H', len(line)) + line
header = bytes([0]) + b'FES HIL   ' + struct.pack('<HHH', len(program), 10, len(program))
with open(sys.argv[1], 'wb') as f:
    f.write(block(0x00, header) + block(0xFF, program))
