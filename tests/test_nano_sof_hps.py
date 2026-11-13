"""Bounded, file-only Nano HPS extraction and package rejection checks."""
import gzip
import importlib.util
from pathlib import Path
import struct
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('nano_sof_hps', ROOT / 'platform/embedded/nano/sof_hps.py')
sof = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sof)


def record(kind, address=0, data=b''):
    raw = bytes([len(data)]) + address.to_bytes(2, 'big') + bytes([kind]) + data
    return b':' + (raw + bytes([-sum(raw) & 255])).hex().encode() + b'\n'


def hex_file(data):
    return b''.join(record(0, i, data[i:i + 16]) for i in range(0, len(data), 16)) + record(1)


def hps(data, split=16):
    result = b'\x32\x12' + struct.pack('<I', (len(data) + split - 1) // split)
    for start in range(0, len(data), split):
        block = data[start:start + split]
        result += b'\x34\x12' + struct.pack('<I', start) + b'\x12' + struct.pack('<I', len(block))
        result += b''.join(b'\x11' + bytes([value]) for value in block)
    return result


def rows(data):
    return [(2, b'nano-device\0'), (58, b'preserved FPGA and boot settings'),
            (62, hps(data)), (65, bytes(40)), (8, bytes(2))]


def pack(entries):
    raw = b'SOF\0' + struct.pack('<II', 0, len(entries))
    raw += b''.join(struct.pack('<HI', tag, len(data)) + data for tag, data in entries)
    return gzip.compress(raw, mtime=0)


class SofHpsTests(unittest.TestCase):
    data = bytes(range(33))

    def test_complete_identity_and_preserved_sections(self):
        reference = pack(rows(b'original'))
        result = sof.inspect(pack(rows(self.data)), hex_file(self.data), reference)
        self.assertEqual(result['spl_bytes'], 33)
        self.assertEqual(result['chunks'], [{'address': 0, 'size': 16}, {'address': 16, 'size': 16}, {'address': 32, 'size': 1}])
        self.assertEqual(result['preserved_sections'], 2)
        self.assertFalse(result['security_hash_and_configuration_crc_verified'])
        self.assertFalse(result['hardware_programming_or_boot_acceptance'])

    def test_changed_payload_or_other_reference_section_rejects(self):
        reference = pack(rows(b'original'))
        with self.assertRaisesRegex(ValueError, 'payload differs'):
            sof.inspect(pack(rows(self.data)), hex_file(self.data[:-1] + b'\xff'), reference)
        for index in (0, 1):
            changed = rows(self.data)
            tag, body = changed[index]
            changed[index] = (tag, body + b'changed')
            with self.assertRaisesRegex(ValueError, 'reference FPGA/configuration'):
                sof.inspect(pack(changed), hex_file(self.data), reference)
        changed = rows(self.data)
        changed[2], changed[3] = changed[3], changed[2]
        with self.assertRaisesRegex(ValueError, 'reference FPGA/configuration'):
            sof.inspect(pack(changed), hex_file(self.data), reference)

    def test_gzip_truncation_crc_trailing_and_resource_bounds(self):
        valid = pack(rows(self.data))
        bad_crc = bytearray(valid); bad_crc[-8] ^= 1
        for value in (b'', b'SOF\0', bytes(bad_crc), valid + valid, valid + b'junk'):
            with self.subTest(value=value[:10]), self.assertRaises(ValueError):
                sof.sections(value)
        for end in range(len(valid)):
            with self.assertRaises(ValueError):
                sof.sections(valid[:end])
        with patch.object(sof, 'MAX_FILE', len(valid) - 1), self.assertRaises(ValueError):
            sof.sections(valid)
        with patch.object(sof, 'MAX_RAW', 64), self.assertRaises(ValueError):
            sof.sections(valid)

    def test_section_count_lengths_duplicates_and_envelope(self):
        for raw in (b'SOF\0', b'SOF\0' + struct.pack('<II', 0, 65),
                    b'SOF\0' + struct.pack('<II', 1, 1),
                    b'SOF\0' + struct.pack('<IIHI', 0, 1, 62, 0xffffffff)):
            with self.assertRaises(ValueError):
                sof.sections(gzip.compress(raw))
        valid = rows(self.data)
        variants = [valid + [(62, hps(self.data))], valid + [(8, bytes(2))],
                    valid[:-1], valid[:-2] + [(65, bytes(39)), valid[-1]],
                    valid[:-1] + [(8, bytes(3))], [valid[-1]] + valid[:-1]]
        for entries in variants:
            with self.assertRaises(ValueError):
                sof.sections(pack(entries))
        with self.assertRaisesRegex(ValueError, 'trailing SOF'):
            sof.sections(gzip.compress(gzip.decompress(pack(valid)) + b'extra'))

    def test_hps_count_markers_addresses_and_sizes_reject(self):
        good = hps(self.data)
        for offset in (2, 6, 7, 8, 12, 13, 17):
            bad = bytearray(good); bad[offset] ^= 1
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                sof.decode_hps(bytes(bad))
        for end in range(len(good)):
            with self.assertRaises(ValueError):
                sof.decode_hps(good[:end])
        # A self-consistent shorter image still cannot replace the sealed one.
        partial = rows(self.data[:16])
        with self.assertRaisesRegex(ValueError, 'payload differs'):
            sof.inspect(pack(partial), hex_file(self.data), pack(rows(b'original')))
        oversized = b'\x32\x12\x03\0\0\0\x34\x12' + bytes(4) + b'\x12' + struct.pack('<I', 65537)
        with self.assertRaises(ValueError):
            sof.decode_hps(oversized)

    def test_hex_checksums_order_bounds_and_unsupported_records(self):
        valid = hex_file(self.data)
        variants = [b'', valid[:-12], valid + record(1), record(1),
                    record(0, 1, b'a') + record(1), record(0, 0, b'a') * 2 + record(1),
                    record(6) + record(1), record(5, data=b'\0\0\0\x01') + valid,
                    record(4, data=b'\0\x04') + record(0, 0, b'a') + record(1),
                    valid.replace(b':10', b':11', 1), b':0100000000FE\n' + record(1),
                    b':0100000000F\n' + record(1)]
        for data in variants:
            with self.subTest(data=data[:30]), self.assertRaises(ValueError):
                sof.decode_hex(data)
        self.assertEqual(sof.decode_hex(record(5, data=bytes(4)) + valid), self.data)

    def test_real_chunk_boundary_and_both_extended_address_modes(self):
        data = bytes(range(256)) * 256 + b'last'
        prefix = b''.join(record(0, i, data[i:i + 16]) for i in range(0, 65536, 16))
        for kind, upper in ((2, 0x1000), (4, 1)):
            encoded = prefix + record(kind, data=upper.to_bytes(2, 'big')) + record(0, 0, b'last') + record(1)
            self.assertEqual(sof.decode_hex(encoded), data)
        self.assertEqual(sof.decode_hps(hps(data, 65536))[0], data)
        for count in range(1, 5):
            data = b'x' * (65536 * (count - 1) + 1)
            self.assertEqual(sof.decode_hps(hps(data, 65536))[0], data)


if __name__ == '__main__':
    unittest.main()
