#!/usr/bin/env python3
"""Shared disk-record byte vector; patterned payload avoids a binary fixture."""
import argparse
import hashlib
import json
from pathlib import Path
import struct

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "testdata/media-data-v1/atari-st.json"

def fixture(size=737280):
    core, game, base, unit = "fes.atari-st", "atari-st-desktop", "a" * 64, 0
    layout = b"fes.atari-st-floppy.image"
    payload = bytes((at * 17 + (at >> 9)) & 255 for at in range(size))
    sha = lambda data: hashlib.sha256(data).digest()
    header = (b"FESDISK1" + sha(core.encode()) + sha(game.encode()) + bytes.fromhex(base)
              + struct.pack("<HHHHI", unit, 1, 0 if size == 737280 else 1, len(layout), len(payload)) + layout)
    checksum = sha(header + payload)
    record = header + payload + checksum
    namespace = b"fes-media-data-v1\0" + core.encode() + b"\0" + game.encode() + b"\0" + str(unit).encode() + b"\0" + base.encode()
    return {"format": 1, "core_id": core, "game_id": game, "base_media_id": base,
            "unit": unit, "layout_id": layout.decode(), "layout_major": 1, "layout_minor": 0 if size == 737280 else 1,
            "payload_size": len(payload), "payload_pattern": "(offset * 17 + (offset >> 9)) & 255",
            "payload_sha256": sha(payload).hex(), "header_hex": header.hex(),
            "checksum_hex": checksum.hex(), "record_size": len(record),
            "revision": sha(record).hex(), "namespace": sha(namespace).hex()}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for path, size in [(OUTPUT, 737280), (OUTPUT.with_name("atari-st-geometry.json"), 839680)]:
        data = json.dumps(fixture(size), indent=2) + "\n"
        if args.check:
            if not path.is_file() or path.read_text() != data:
                parser.exit(1, "media-data fixture is stale\n")
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(data)

if __name__ == "__main__":
    main()
