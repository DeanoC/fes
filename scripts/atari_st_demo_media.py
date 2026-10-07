#!/usr/bin/env python3
"""Inspect classic MSA demo disks without changing their sector layout.

This is an offline compatibility diagnostic, not a runtime media importer.
The MSA layout is documented by Hatari in src/floppies/msa.c:
https://github.com/hatari/hatari/blob/main/src/floppies/msa.c
Only complete disks starting at track zero are accepted. No ROMs or demos
are distributed. Unsupported geometry is reported, never padded or truncated.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct

MAX_BYTES = 8 * 1024 * 1024
SUPPORTED = (80, 2, 9)


def decode_msa(data):
    if not 10 < len(data) <= MAX_BYTES:
        raise ValueError("MSA input must be bounded and contain track data")
    magic, sectors, sides, first, last = struct.unpack_from(">5H", data)
    if magic != 0x0E0F or not 1 <= sectors <= 56 or sides > 1 or first != 0 or last > 86:
        raise ValueError("invalid or partial MSA disk header")
    track_bytes = sectors * 512
    result = bytearray()
    position = 10
    for _ in range((last + 1) * (sides + 1)):
        if position + 2 > len(data):
            raise ValueError("missing MSA track length")
        length = struct.unpack_from(">H", data, position)[0]
        position += 2
        if not 0 < length <= track_bytes or position + length > len(data):
            raise ValueError("invalid or truncated MSA track")
        block = data[position:position + length]
        position += length
        if length == track_bytes:
            result.extend(block)
            continue
        track = bytearray()
        cursor = 0
        while cursor < length:
            value = block[cursor]
            cursor += 1
            count = 1
            if value == 0xE5:
                if cursor + 3 > length:
                    raise ValueError("truncated MSA run")
                value = block[cursor]
                count = struct.unpack_from(">H", block, cursor + 1)[0]
                cursor += 3
            if count == 0 or len(track) + count > track_bytes:
                raise ValueError("invalid MSA run length")
            track.extend(bytes([value]) * count)
        if len(track) != track_bytes:
            raise ValueError("MSA track expands to the wrong size")
        result.extend(track)
    if position != len(data):
        raise ValueError("unexpected data after final MSA track")
    geometry = (last + 1, sides + 1, sectors)
    return bytes(result), geometry


def inspect(data):
    raw, geometry = decode_msa(data)
    tracks, sides, sectors = geometry
    return {"format": "msa", "source_bytes": len(data),
            "source_sha256": hashlib.sha256(data).hexdigest(),
            "tracks": tracks, "sides": sides, "sectors_per_track": sectors,
            "sector_bytes": 512, "raw_bytes": len(raw),
            "raw_sha256": hashlib.sha256(raw).hexdigest(),
            "current_fes_geometry_supported": geometry == SUPPORTED,
            "current_fes_requires": {"format": "raw-st", "tracks": 80,
                                     "sides": 2, "sectors_per_track": 9,
                                     "bytes": 737280}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("msa", type=Path)
    args = parser.parse_args()
    with args.msa.open("rb") as stream:
        data = stream.read(MAX_BYTES + 1)
    try:
        report = inspect(data)
    except ValueError as exc:
        parser.error(str(exc))
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
