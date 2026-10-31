#!/usr/bin/env python3
"""Shared disk-record byte vector; patterned payload avoids a binary fixture."""
import argparse
import hashlib
import json
from pathlib import Path
import struct
import zlib

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

DELTA_LAYOUT = b"fes.ata-disk.delta"
CHUNK = 65536


def delta_chunk(index):
    """Patterned 64 KiB chunk data; the last chunk of a short image is short."""
    return bytes((index * 31 + at * 13 + (at >> 8)) & 255 for at in range(CHUNK))


def delta_fixture():
    core, game, unit = "fes.pc386", "ultima-vi", 0
    base_size = 2 * 1024 * 1024 + 512 * 3  # not a whole number of chunks
    base = bytes((at * 7 + (at >> 11)) & 255 for at in range(base_size))
    sha = lambda data: hashlib.sha256(data).digest()
    base_id = sha(base).hex()
    chunks = (0, 5, 31, 32)  # 32 is the 1536-byte tail chunk
    data = {}
    for index in chunks:
        length = min(CHUNK, base_size - index * CHUNK)
        data[index] = delta_chunk(index)[:length]
    header = (b"FESDELT1" + sha(core.encode()) + sha(game.encode()) + bytes.fromhex(base_id)
              + struct.pack("<HHHHIII", unit, 1, 0, len(DELTA_LAYOUT), base_size, CHUNK, len(chunks)) + DELTA_LAYOUT)
    entries = b"".join(struct.pack("<I", index) + sha(data[index]) for index in chunks)
    manifest = header + entries
    record = manifest + sha(manifest)
    image = bytearray(base)
    for index in chunks:
        image[index * CHUNK:index * CHUNK + len(data[index])] = data[index]
    namespace = b"fes-media-data-v1\0" + core.encode() + b"\0" + game.encode() + b"\0" + str(unit).encode() + b"\0" + base_id.encode()

    def changed(at, value):
        return manifest[:at] + value + manifest[at + len(value):]

    def sealed(body):
        return body + sha(body)

    def entry_list(pairs):
        body = header[:120] + struct.pack("<I", len(pairs)) + DELTA_LAYOUT
        return sealed(body + b"".join(struct.pack("<I", i) + sha(data.get(i, b"")) for i in pairs))

    invalid = {
        "st-envelope": b"FESDISK1" + record[8:],
        "bad-checksum": record[:-1] + bytes([record[-1] ^ 1]),
        "wrong-unit": sealed(changed(104, struct.pack("<H", 1))),
        "wrong-major": sealed(changed(106, struct.pack("<H", 2))),
        "wrong-minor": sealed(changed(108, struct.pack("<H", 1))),
        "base-not-whole-sectors": sealed(changed(112, struct.pack("<I", base_size + 1))),
        "base-under-minimum": sealed(changed(112, struct.pack("<I", 1024 * 1024 - 512))),
        "wrong-chunk-size": sealed(changed(116, struct.pack("<I", 32768))),
        "descending-indices": entry_list((5, 0)),
        "duplicate-index": entry_list((5, 5)),
        "index-past-base": entry_list((33,)),
        "count-mismatch": sealed(changed(120, struct.pack("<I", len(chunks) + 1))),
        "truncated": record[:-1],
        "trailing-byte": record + b"\x00",
    }
    return {"format": 1, "core_id": core, "game_id": game, "base_media_id": base_id, "unit": unit,
            "layout_id": DELTA_LAYOUT.decode(), "layout_major": 1, "layout_minor": 0,
            "base_size": base_size, "base_pattern": "(offset * 7 + (offset >> 11)) & 255",
            "chunk_size": CHUNK, "chunk_pattern": "(index * 31 + offset * 13 + (offset >> 8)) & 255",
            "chunks": [{"index": i, "bytes": len(data[i]), "sha256": sha(data[i]).hex(),
                        "file": "chunk-" + sha(data[i]).hex()} for i in chunks],
            "record_hex": record.hex(), "record_size": len(record), "revision": sha(record).hex(),
            "namespace": sha(namespace).hex(), "restored_sha256": sha(bytes(image)).hex(),
            "restored_crc32": zlib.crc32(bytes(image)),
            "invalid": [{"name": name, "hex": value.hex()} for name, value in invalid.items()]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    for path, value in [(OUTPUT, lambda: fixture(737280)),
                        (OUTPUT.with_name("atari-st-geometry.json"), lambda: fixture(839680)),
                        (OUTPUT.with_name("ata-disk-delta.json"), delta_fixture)]:
        data = json.dumps(value(), indent=2) + "\n"
        if args.check:
            if not path.is_file() or path.read_text() != data:
                parser.exit(1, "media-data fixture is stale\n")
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(data)

if __name__ == "__main__":
    main()
