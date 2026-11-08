"""Fixed-window descriptor service with real C engine and injected ownership/SHA supplier."""
from pathlib import Path
import hashlib
import struct
import subprocess
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoTapeServiceTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='fes-nano-tape-service-')
        cls.binary = Path(cls.temp.name) / 'service'
        subprocess.run(['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                        '-fsanitize=undefined', '-fno-sanitize-recover=all',
                        '-I' + str(FW), str(FW / 'nano_tape.c'),
                        str(FW / 'nano_tape_service.c'),
                        str(FW / 'tests/nano_tape_service_test.c'), '-o', str(cls.binary)],
                       check=True, capture_output=True, timeout=30)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def check(self, mode, size=917, extra=()):
        result = subprocess.run([str(self.binary), str(mode), str(size), *map(str, extra)],
                                capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('PASS tape service', result.stdout)

    def test_full_transfers(self):
        for size in (1, 2, 511, 512, 513, 917, 1025, 16375):
            with self.subTest(size=size):
                self.check(0, size)

    def test_service_failures_and_original_deadline(self):
        for mode in range(101, 114):
            with self.subTest(mode=mode):
                self.check(mode)

    def test_every_header_byte_is_checked(self):
        for offset in range(128):
            with self.subTest(offset=offset):
                self.check(120 + offset)

    def test_engine_failures_remain_retired(self):
        # These faults are injected at the actual endpoint/ownership callbacks.
        for mode in (1, 2, 3, 4, 5, 6, 7, 8, 10, 11, 12, 13, 14, 15, 16,
                     17, 18, 19, 20, 21, 23, 24, 25, 26):
            with self.subTest(mode=mode):
                self.check(mode)

    def test_external_fixed_descriptor_bytes(self):
        image = bytes((i * 37 + 11) & 255 for i in range(917))
        digest = hashlib.sha384(image).digest()
        header = bytearray(128)
        struct.pack_into('<IIIIQQII', header, 0, 0x31505446, 1, 128, len(image),
                         17, 9, 0x82000000, zlib.crc32(image))
        header[40:56] = bytes.fromhex('0123456789abcdef1032547698badcfe')
        header[56:104] = digest
        paths = [Path(self.temp.name) / name for name in ('header', 'image', 'digest')]
        for path, data in zip(paths, (header, image, digest)):
            path.write_bytes(data)
        self.check(0, len(image), paths)


if __name__ == '__main__':
    unittest.main()
