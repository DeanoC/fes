#!/usr/bin/env python3
"""Open 16 KiB ZX Spectrum diagnostic firmware and a 3-byte .tap image.

Nothing here is derived from a Sinclair ROM. The image paints a red border,
one black ink byte on a white attribute cell, then loops: read the A-row of
the keyboard, the Kempston port, probe socket 1's id, and write/read its
scratch register. Results land at $8000.
"""

from __future__ import annotations

import argparse
from pathlib import Path

# ld bc,$FDFE / in a,(c) / ld ($8001),a
# ld bc,$001F / in a,(c) / ld ($8002),a
# ld bc,$00E0 / in a,(c) / ld ($8003),a
# ld bc,$00E1 / ld a,$5A / out (c),a / in a,(c) / ld ($8004),a
# jp $0017
PROGRAM = bytes([
    0xF3,
    0x31, 0xFF, 0xFF,
    0x3E, 0x02,
    0xD3, 0xFE,
    0x3E, 0xFF,
    0x32, 0x00, 0x40,
    0x3E, 0x38,
    0x32, 0x00, 0x58,
    0x3E, 0xA5,
    0x32, 0x00, 0x80,
    0x01, 0xFE, 0xFD,
    0xED, 0x78,
    0x32, 0x01, 0x80,
    0x01, 0x1F, 0x00,
    0xED, 0x78,
    0x32, 0x02, 0x80,
    0x01, 0xE0, 0x00,
    0xED, 0x78,
    0x32, 0x03, 0x80,
    0x01, 0xE1, 0x00,
    0x3E, 0x5A,
    0xED, 0x79,
    0xED, 0x78,
    0x32, 0x04, 0x80,
    0xC3, 0x17, 0x00,
])

# One data block: length 1, flag/data $FF. The player uses the data pilot.
TAPE = bytes([0x01, 0x00, 0xFF])


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    image = PROGRAM + bytes(16384 - len(PROGRAM))
    (args.output_dir / "firmware.hex").write_text(
        "\n".join(f"{byte:02x}" for byte in image) + "\n", encoding="ascii")
    (args.output_dir / "firmware.rom").write_bytes(image)
    (args.output_dir / "tape.hex").write_text(
        "\n".join(f"{byte:02x}" for byte in TAPE) + "\n", encoding="ascii")
    (args.output_dir / "tape.tap").write_bytes(TAPE)


if __name__ == "__main__":
    main()
