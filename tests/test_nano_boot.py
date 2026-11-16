"""Secure-only boot consumption and complete master-stop receipts, off-board."""
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FW = ROOT / 'platform/embedded/firmware'


class NanoBootTests(unittest.TestCase):
    def test_owned_boot_and_master_failures(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-boot-') as temp:
            binary = Path(temp) / 'boot'
            command = ['cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-fsanitize=undefined',
                       '-fno-sanitize-recover=all', '-I' + str(FW),
                       *(str(FW / name) for name in ('nano_bridges.c', 'nano_bridge_sip.c', 'nano_boot.c', 'tests/nano_boot_test.c')),
                       '-o', str(binary)]
            subprocess.run(command, check=True, capture_output=True, text=True)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('PASS 26 boot/master ownership cases', result.stdout)

    def test_reset_masks_match_existing_pinned_serial_handoff(self):
        old = (ROOT / 'platform/embedded/nano/handoff.h').read_text()
        new = (FW / 'nano_boot.h').read_text()
        for before, after in (('FES_NANO_PER0_FUNCTION', 'FES_NANO_MASTER_FUNCTION'),
                              ('FES_NANO_PER0_OCP', 'FES_NANO_MASTER_OCP')):
            a = re.search(r'#define ' + before + r' (0x[0-9a-f]+)U', old).group(1)
            b = re.search(r'#define ' + after + r' (0x[0-9a-f]+)U', new).group(1)
            self.assertEqual(a, b)


if __name__ == '__main__':
    unittest.main()
