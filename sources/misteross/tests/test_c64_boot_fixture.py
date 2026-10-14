# SPDX-License-Identifier: MIT
"""The PRG fixture rejects unsafe inputs before opening firmware or a model."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HELPER=Path(__file__).resolve().parents[1]/'scripts/c64_boot_observe.py'

class PrgFixtureInputTests(unittest.TestCase):
    def reject(self,blob,extra,message):
        with tempfile.TemporaryDirectory() as directory:
            temp=Path(directory)
            prg=temp/'fixture.prg';prg.write_bytes(blob)
            args=[sys.executable,str(HELPER),'--basic',str(temp/'missing-basic'),
                  '--kernal',str(temp/'missing-kernal'),'--chargen',str(temp/'missing-chargen'),
                  '--disk',str(temp/'missing-disk'),'--output',str(temp/'output'),
                  '--prg-fixture',str(prg)]
            result=subprocess.run(args+extra,text=True,capture_output=True)
            self.assertEqual(result.returncode,2,result.stderr)
            self.assertIn(message,result.stderr)
            self.assertFalse((temp/'output').exists())

    def test_basic_load_address_is_required(self):
        self.reject(b'\x00\x08\x00\x00',[], 'requires BASIC load address')

    def test_program_must_fit_basic_ram(self):
        for blob in [b'\x01\x08',b'\x01\x08'+bytes(0xa000-0x0801+1)]:
            with self.subTest(length=len(blob)):
                self.reject(blob,[], 'requires BASIC load address')

    def test_endpoint_must_follow_installation(self):
        self.reject(b'\x01\x08\x00\x00',['--cycles',str(3*52224000)],
                    'budget/checkpoints must follow')

    def test_checkpoint_must_follow_installation(self):
        self.reject(b'\x01\x08\x00\x00',
                    ['--cycles',str(8*52224000),'--save-state','unused.state',
                     '--save-at-cycles',str(2*52224000)],'budget/checkpoints must follow')

if __name__=='__main__':unittest.main()
