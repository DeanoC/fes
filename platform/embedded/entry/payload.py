"""Bounded, deterministic gzip encoding shared by entry producers and checks."""
import hashlib
import struct
import zlib

MAX_BYTES = 32 << 20
STACK_RESERVE = 0x20000
HEADER = b'\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff'
ENCODING = 'gzip-deflate-v1'


def encode(payload):
    if not 64 <= len(payload) <= MAX_BYTES:
        raise ValueError('entry ELF exceeds the existing32MiB payload bound')
    compressor = zlib.compressobj(6, zlib.DEFLATED, -15)
    body = compressor.compress(payload) + compressor.flush()
    packed = HEADER + body + struct.pack('<II', zlib.crc32(payload), len(payload))
    if len(packed) > MAX_BYTES:
        raise ValueError('compressed entry payload exceeds32MiB')
    return packed


def decode(packed):
    if not 18 < len(packed) <= MAX_BYTES or packed[:10] != HEADER:
        raise ValueError('entry requires one fixed-header gzip member')
    crc, size = struct.unpack_from('<II', packed, len(packed) - 8)
    if not 64 <= size <= MAX_BYTES:
        raise ValueError('decoded entry ELF exceeds32MiB')
    decoder = zlib.decompressobj(-15)
    try:
        output = decoder.decompress(packed[10:-8], size + 1)
    except zlib.error as error:
        raise ValueError('invalid entry DEFLATE stream') from error
    if (len(output) != size or not decoder.eof or decoder.unused_data or
            decoder.unconsumed_tail or zlib.crc32(output) != crc):
        raise ValueError('entry gzip length, checksum or stream boundary differs')
    return output


def workspace(ram_start, ram_end):
    if (type(ram_start) is not int or type(ram_end) is not int or ram_start < 0 or
            ram_end > (1 << 64) - 1 or ram_end - ram_start <= MAX_BYTES + STACK_RESERVE + 0x10000):
        raise ValueError('entry requires owned RAM for its bounded decode workspace')
    return ram_end - STACK_RESERVE - MAX_BYTES, MAX_BYTES


def checked(record, image, payload=None):
    offset, stored = record.get('payload_offset'), record.get('payload_stored_size')
    if (record.get('payload_encoding') != ENCODING or type(offset) is not int or
            type(stored) is not int or offset < 64 or not 18 < stored <= MAX_BYTES or
            offset > len(image) or stored > len(image) - offset):
        raise ValueError('entry compressed payload layout differs')
    packed = image[offset:offset + stored]
    if hashlib.sha256(packed).hexdigest() != record.get('payload_stored_sha256'):
        raise ValueError('entry stored payload digest differs')
    output = decode(packed)
    if (len(output) != record.get('payload_size') or type(record.get('payload_size')) is not int or
            hashlib.sha256(output).hexdigest() != record.get('payload_sha256') or
            payload is not None and payload != output):
        raise ValueError('entry decoded payload differs')
    base, capacity = workspace(record['ram_start'], record['ram_end'])
    if (type(record.get('decode_base')) is not int or record['decode_base'] != base or
            type(record.get('decode_capacity')) is not int or record['decode_capacity'] != capacity):
        raise ValueError('entry decode workspace differs from owned RAM')
    return output
