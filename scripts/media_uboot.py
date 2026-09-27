#!/usr/bin/env python3
"""Derive the FES U-Boot from the locked MiSTer uboot.img.

The upstream file is four 64 KiB copies of the Cyclone V SPL (each with its
own Altera "AS01" header) followed, at 0x40000, by U-Boot proper as a legacy
mkimage image: a 64-byte big-endian header (magic 0x27051956, header CRC-32,
time, data size, load, entry, data CRC-32, OS/arch/type/compression, name)
and then the data. U-Boot's compiled-in default environment lives in that
data as NUL-separated strings with no CRC of its own.

FES boots one idle core, FAT /idle.rbf. The only change is the same-length
default-environment value core=menu.rbf -> core=idle.rbf. The data CRC and
then the header CRC are recomputed; the SPL copies, timestamp and every other
byte are unchanged. The upstream and derived identities are pinned in
boot-media.lock.toml.
"""
import argparse
import hashlib
from pathlib import Path
import struct
import sys
import zlib


SPL_SIZE = 4 * 64 * 1024
HEADER_SIZE = 64
MAGIC = 0x27051956
UPSTREAM_VALUE = b"\0core=menu.rbf\0"
DERIVED_VALUE = b"\0core=idle.rbf\0"


def _header(data):
    if len(data) < SPL_SIZE + HEADER_SIZE:
        raise ValueError("U-Boot image is too short for the SPL and legacy header")
    header = bytearray(data[SPL_SIZE:SPL_SIZE + HEADER_SIZE])
    magic, header_crc, _time, size = struct.unpack_from(">4I", header)
    data_crc = struct.unpack_from(">I", header, 24)[0]
    if magic != MAGIC:
        raise ValueError("U-Boot legacy image magic differs")
    if SPL_SIZE + HEADER_SIZE + size != len(data):
        raise ValueError("U-Boot legacy image size differs from file")
    struct.pack_into(">I", header, 4, 0)
    if zlib.crc32(header) != header_crc:
        raise ValueError("U-Boot legacy header CRC differs")
    if zlib.crc32(data[SPL_SIZE + HEADER_SIZE:]) != data_crc:
        raise ValueError("U-Boot legacy data CRC differs")
    return size


def check(data):
    """Validate the SPL-prefixed legacy image checksums; return the data size."""
    return _header(bytes(data))


def derive(upstream):
    """Return the FES U-Boot bytes for exact locked upstream bytes."""
    upstream = bytes(upstream)
    _header(upstream)
    if upstream.count(UPSTREAM_VALUE) != 1 or upstream.count(b"menu.rbf") != 1:
        raise ValueError("upstream U-Boot must contain core=menu.rbf exactly once")
    if b"idle.rbf" in upstream:
        raise ValueError("upstream U-Boot already names idle.rbf")
    offset = upstream.index(UPSTREAM_VALUE)
    if offset < SPL_SIZE + HEADER_SIZE:
        raise ValueError("core=menu.rbf is outside the U-Boot legacy image data")
    image = bytearray(upstream)
    image[offset:offset + len(DERIVED_VALUE)] = DERIVED_VALUE
    payload = SPL_SIZE + HEADER_SIZE
    struct.pack_into(">I", image, SPL_SIZE + 24, zlib.crc32(image[payload:]))
    struct.pack_into(">I", image, SPL_SIZE + 4, 0)
    struct.pack_into(">I", image, SPL_SIZE + 4, zlib.crc32(image[SPL_SIZE:payload]))
    _header(image)
    return bytes(image)


def derive_locked(upstream, lock):
    """Derive from bytes matching lock.uboot_upstream and verify lock.uboot."""
    upstream = bytes(upstream)
    source = lock.uboot_upstream
    if len(upstream) != source.size or hashlib.sha256(upstream).hexdigest() != source.sha256:
        raise ValueError("upstream uboot.img digest or size differs from boot-media lock")
    derived = derive(upstream)
    if len(derived) != lock.uboot.size or hashlib.sha256(derived).hexdigest() != lock.uboot.sha256:
        raise ValueError("derived U-Boot digest or size differs from boot-media lock")
    for value in lock.environment:
        if value.encode() not in derived:
            raise ValueError("U-Boot environment string differs from boot-media lock: " + value)
    return derived


def main():
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    from media_inputs import MediaLock
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--lock", type=Path, default=Path(__file__).resolve().parents[1] / "boot-media.lock.toml")
    parser.add_argument("upstream", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    try:
        derived = derive_locked(args.upstream.read_bytes(), MediaLock.load(args.lock))
        if args.output.exists() or args.output.is_symlink():
            raise ValueError("output already exists")
        args.output.write_bytes(derived)
    except (OSError, ValueError) as error:
        parser.exit(1, f"media_uboot: {error}\n")
    print(hashlib.sha256(derived).hexdigest(), len(derived))


if __name__ == "__main__":
    main()
