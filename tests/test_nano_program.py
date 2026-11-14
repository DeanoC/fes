"""The real secure descriptor and configuration/bridge engines without hardware."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FIRMWARE = ROOT / 'platform/embedded/firmware'


class NanoProgramTests(unittest.TestCase):
    def test_per_load_descriptors_and_different_programs_cross_owned_returns(self):
        with tempfile.TemporaryDirectory() as temporary:
            binary = Path(temporary) / 'program-binding'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror',
                       '-fsanitize=undefined', '-fno-sanitize-recover=all',
                       '-I' + str(FIRMWARE),
                       *(str(FIRMWARE / name) for name in
                         ('nano_program.c', 'nano_configuration.c', 'nano_bridges.c',
                          'tests/nano_program_test.c')), '-o', str(binary)]
            build = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(build.returncode, 0, build.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
            self.assertIn('PASS 36 per-load descriptor', result.stdout)
            self.assertIn('PASS 18 ROM descriptor', result.stdout)
            self.assertIn('PASS 5 mixed menu/computer configuration cycles and 20 role', result.stdout)


if __name__ == '__main__':
    unittest.main()
