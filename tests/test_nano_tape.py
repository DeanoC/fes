"""Unbound secure-side tape engine: real C, injected transport and clock."""
from pathlib import Path
import re
import subprocess
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoTapeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='fes-nano-tape-')
        cls.binary = Path(cls.temp.name) / 'tape'
        command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                   '-fsanitize=undefined', '-fno-sanitize-recover=all',
                   '-I' + str(FW), str(FW / 'nano_tape.c'),
                   str(FW / 'tests/nano_tape_test.c'), '-o', str(cls.binary)]
        subprocess.run(command, check=True, timeout=30, capture_output=True)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def check(self, mode, size):
        result = subprocess.run([str(self.binary), str(mode), str(size)],
                                capture_output=True, text=True, timeout=15)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('PASS secure tape', result.stdout)
        return result.stdout

    def test_full_media_transfer_odd_padding_chunk_edges_and_maximum(self):
        for size in (1, 2, 511, 512, 513, 917, 1025, 16375):
            with self.subTest(size=size):
                output = self.check(0, size)
                crc = int(re.search(r'crc=([0-9a-f]+)', output).group(1), 16)
                self.assertEqual(crc, zlib.crc32(bytes((i*37+11) & 255 for i in range(size))))

    def test_late_faults_never_publish_receipt_run_or_replay(self):
        for mode in range(1, 29):
            with self.subTest(mode=mode):
                self.check(mode, 917)


if __name__ == '__main__':
    unittest.main()
