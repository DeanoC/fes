"""Bounded same-client retired-status reads at the actual register boundary."""
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'

class NanoRetiredStatusTests(unittest.TestCase):
    def test_fixed_query_ring_wrap_bounds_and_failure_paths(self):
        reference=Path(os.environ.get('FES_NANO_TFA_ROOT',str(ROOT/'out/terasic-git-restore-tfa')))
        header=reference/'plat/intel/soc/common/include/socfpga_mailbox.h'
        if not header.is_file():self.skipTest('reviewed vendor mailbox header required')
        self.assertEqual(hashlib.sha256(header.read_bytes()).hexdigest(),'a7a3e8dccc604fb6f9285572d5977bc00f36ba56417e1308cf0c8b16989db24f')
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);inc=base/'include';(inc/'lib').mkdir(parents=True)
            (inc/'lib/utils_def.h').write_text('#define BIT(n) (1U << (n))\n#define U(n) n##U\n')
            (inc/'socfpga_mailbox.h').write_bytes(header.read_bytes())
            binary=base/'status'
            result=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=address,undefined',
                '-fno-sanitize-recover=all','-I'+str(inc),'-I'+str(FW),str(FW/'tests/nano_mailbox_status_test.c'),'-o',str(binary)],capture_output=True,text=True,timeout=30)
            self.assertEqual(result.returncode,0,result.stderr)
            for mode in range(20):
                for initial in range(16):
                    with self.subTest(mode=mode,initial=initial):
                        result=subprocess.run([str(binary),str(mode),str(initial)],capture_output=True,text=True,timeout=5)
                        self.assertEqual(result.returncode,0,result.stderr)
                        self.assertIn('PASS retired-status',result.stdout)
