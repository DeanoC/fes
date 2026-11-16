"""Inspect the observed Nano SOF/HPS literal format without Quartus.

This proves embedded SPL byte identity and preservation of the reference's
other sections. It does not verify SDM commands, SOF security/hash fields,
configuration CRC, device programming or boot. Unknown formats reject.
"""
import hashlib
import re
import struct
import zlib

MAX_FILE = 8 << 20
MAX_RAW = 32 << 20
MAX_SPL = 0x40000
HPS = 62
GENERATED = {8, HPS, 65}


def sections(data):
    if not isinstance(data, bytes) or not 0 < len(data) <= MAX_FILE or data[:2] != b'\x1f\x8b':
        raise ValueError('requires bounded gzip SOF')
    decoder = zlib.decompressobj(31)
    try:
        raw = decoder.decompress(data, MAX_RAW + 1)
    except zlib.error as error:
        raise ValueError('invalid gzip SOF') from error
    if len(raw) > MAX_RAW or not decoder.eof or decoder.unused_data or decoder.unconsumed_tail:
        raise ValueError('oversize, incomplete or trailing gzip SOF')
    if len(raw) < 12:
        raise ValueError('short SOF header')
    magic, version, count = struct.unpack_from('<4sII', raw)
    if magic != b'SOF\0' or version != 0 or not 0 < count <= 64:
        raise ValueError('unsupported SOF header')
    result, offset = [], 12
    for _ in range(count):
        if len(raw) - offset < 6:
            raise ValueError('short SOF section header')
        tag, size = struct.unpack_from('<HI', raw, offset)
        offset += 6
        if size > len(raw) - offset:
            raise ValueError('short SOF section body')
        result.append((tag, raw[offset:offset + size]))
        offset += size
    if offset != len(raw):
        raise ValueError('trailing SOF data')
    for tag, size in ((8, 2), (65, 40)):
        rows = [body for number, body in result if number == tag]
        if len(rows) != 1 or len(rows[0]) != size:
            raise ValueError('unsupported SOF checksum/hash envelope')
    if result[-1][0] != 8 or sum(tag == HPS for tag, _ in result) != 1:
        raise ValueError('requires one HPS section and terminal checksum')
    return result


def decode_hps(data):
    if not 6 < len(data) <= 2 * MAX_SPL + 50 or data[:2] != b'\x32\x12':
        raise ValueError('unsupported HPS literal format')
    count = struct.unpack_from('<I', data, 2)[0]
    if not 0 < count <= 4:
        raise ValueError('unsupported HPS chunk count')
    result, chunks, offset = bytearray(), [], 6
    while offset < len(data):
        if len(chunks) >= 4 or len(data) - offset < 11 or data[offset:offset + 2] != b'\x34\x12' or data[offset + 6] != 0x12:
            raise ValueError('unsupported HPS chunk header')
        address = struct.unpack_from('<I', data, offset + 2)[0]
        size = struct.unpack_from('<I', data, offset + 7)[0]
        offset += 11
        if address != len(result) or not 0 < size <= 65536 or size > MAX_SPL - len(result) or size * 2 > len(data) - offset:
            raise ValueError('HPS chunk address/size differs')
        literals = data[offset:offset + size * 2]
        if literals[::2] != b'\x11' * size:
            raise ValueError('unsupported HPS literal marker')
        result.extend(literals[1::2])
        chunks.append({'address': address, 'size': size})
        offset += size * 2
    if not result or len(chunks) != count:
        raise ValueError('HPS chunk count differs')
    return bytes(result), chunks


def decode_hex(data):
    if not isinstance(data, bytes) or not 0 < len(data) <= 1 << 20:
        raise ValueError('requires bounded Intel HEX')
    result, upper, eof, start = bytearray(), 0, False, False
    for line in data.splitlines():
        if eof or not re.fullmatch(rb':[0-9a-fA-F]{10,520}', line):
            raise ValueError('invalid Intel HEX line/order')
        try:
            record = bytes.fromhex(line[1:].decode('ascii'))
        except ValueError as error:
            raise ValueError('invalid Intel HEX encoding') from error
        if len(record) != record[0] + 5 or sum(record) & 255:
            raise ValueError('Intel HEX length/checksum differs')
        address, kind, payload = int.from_bytes(record[1:3], 'big'), record[3], record[4:-1]
        if kind == 0:
            if not payload or upper + address != len(result) or len(payload) > MAX_SPL - len(result):
                raise ValueError('Intel HEX must cover contiguous OCRAM from zero')
            result.extend(payload)
        elif kind in (2, 4) and address == 0 and len(payload) == 2:
            upper = int.from_bytes(payload, 'big') << (4 if kind == 2 else 16)
        elif kind == 5 and address == 0 and payload == bytes(4) and not start:
            start = True
        elif kind == 1 and address == 0 and not payload:
            eof = True
        else:
            raise ValueError('unsupported Intel HEX record')
    if not eof or not result:
        raise ValueError('missing Intel HEX data/EOF')
    return bytes(result)


def inspect(candidate, expected_hex, reference):
    observed, original = sections(candidate), sections(reference)
    if [tag for tag, _ in observed] != [tag for tag, _ in original] or [
            (tag, body) for tag, body in observed if tag not in GENERATED] != [
            (tag, body) for tag, body in original if tag not in GENERATED]:
        raise ValueError('changed reference FPGA/configuration sections')
    blob, chunks = decode_hps(next(body for tag, body in observed if tag == HPS))
    expected = decode_hex(expected_hex)
    if blob != expected:
        raise ValueError('embedded SPL payload differs from sealed Intel HEX')
    return {'format': 'nano-sof-gzip-hps-literals', 'spl_bytes': len(blob),
            'spl_binary_sha256': hashlib.sha256(blob).hexdigest(),
            'spl_hex_sha256': hashlib.sha256(expected_hex).hexdigest(), 'chunks': chunks,
            'preserved_sections': sum(tag not in GENERATED for tag, _ in observed),
            'security_hash_and_configuration_crc_verified': False,
            'hardware_programming_or_boot_acceptance': False}
