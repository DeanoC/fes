#!/usr/bin/env python3
"""Collapse ZXText2P's empty display for a 1 KiB ZX81 (MIT).
Copyright 2026 FES contributors; see ../licenses/guess-number-MIT.txt.
Usage: python3 compact-display.py path/to/guess-number.p
"""
import pathlib
import struct
import sys

path = pathlib.Path(sys.argv[1])
data = bytearray(path.read_bytes())
dfile = struct.unpack_from('<H', data, 3)[0] - 0x4009
variables = struct.unpack_from('<H', data, 7)[0] - 0x4009
assert data[dfile:variables] == bytes([0x76]) + (bytes(32) + bytes([0x76])) * 24
# Collapsed display: initial HALT followed by 24 empty rows, one HALT each.
data[dfile:variables] = bytes([0x76]) * 25
# VARS, E_LINE, CH_ADD, STKBOT and STKEND move; D_FILE/DF_CC stay put.
for offset in (7, 11, 13, 17, 19):
    pointer = struct.unpack_from('<H', data, offset)[0]
    struct.pack_into('<H', data, offset, pointer - 768)
path.write_bytes(data)
