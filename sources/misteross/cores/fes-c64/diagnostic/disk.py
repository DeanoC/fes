#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Synthetic 35-track D64 for the open Commodore 64 diagnostic.

One PRG, named BOOT, lives at track 1 sector 0. Its data bytes are the load
address $0801 followed by 11 22 33 44. The directory is the standard first
sector at track 18 sector 1. Nothing here is copied from a Commodore disk.
"""

from __future__ import annotations

import argparse
from pathlib import Path

DISK_BYTES = 174848
NAME = b"BOOT"


def track_starts() -> list[int]:
    offset = 0
    starts = []
    for track in range(1, 36):
        starts.append(offset)
        if track <= 17:
            count = 21
        elif track <= 24:
            count = 19
        elif track <= 30:
            count = 18
        else:
            count = 17
        offset += count * 256
    if offset != DISK_BYTES:
        raise RuntimeError(f"D64 geometry summed to {offset}")
    return starts


def build() -> bytes:
    image = bytearray(DISK_BYTES)
    starts = track_starts()
    bam = starts[17]
    image[bam] = 18
    image[bam + 1] = 1
    image[bam + 2] = 0x41
    directory = starts[17] + 256
    image[directory + 0] = 0
    image[directory + 1] = 0xFF
    image[directory + 2] = 0x82
    image[directory + 3] = 1
    image[directory + 4] = 0
    image[directory + 5:directory + 21] = NAME + bytes([0xA0]) * (16 - len(NAME))
    image[directory + 30] = 1
    image[directory + 31] = 0
    sector = starts[0]
    payload = bytes((0x01, 0x08, 0x11, 0x22, 0x33, 0x44))
    image[sector] = 0
    image[sector + 1] = 1 + len(payload)  # last used byte index
    image[sector + 2:sector + 2 + len(payload)] = payload
    return bytes(image)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--hex-output", type=Path, required=True)
    args = parser.parse_args()
    image = build()
    args.output.write_bytes(image)
    args.hex_output.write_text("".join(f"{byte:02X}\n" for byte in image))


if __name__ == "__main__":
    main()
