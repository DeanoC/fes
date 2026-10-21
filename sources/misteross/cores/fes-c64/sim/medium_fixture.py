#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Original varied D64 images and independent Python G64 expectations."""
import argparse
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "scripts"))
from c64_gcr import g64


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    args.output_dir.mkdir(parents=True, exist_ok=True)
    for index, seed in enumerate((0x3D, 0xA7), 1):
        # Vary every sector and both nibbles, including the BAM disk IDs.
        image = bytes(((i * 17 + seed) ^ (i >> 8)) & 255 for i in range(174848))
        name = args.output_dir / f"disk{index}"
        name.with_suffix(".d64").write_bytes(image)
        name.with_suffix(".hex").write_text("".join(f"{byte:02x}\n" for byte in image))
        name.with_suffix(".g64").write_bytes(g64(image))
    # The mailbox test starts with a zeroed store and supplies every real byte
    # through the transport, including writes spanning odd chunk boundaries.
    (args.output_dir / "empty.hex").write_text("00\n" * 174848)


if __name__ == "__main__":
    main()
