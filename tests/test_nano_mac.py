"""Portable private MAC lease and frozen C/Go wire checks, no kit access."""
import json
from pathlib import Path
import re
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'

class NanoMACTests(unittest.TestCase):
    def test_ownership_generation_and_failure_retention(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-mac-') as temp:
            binary=Path(temp)/'mac'
            subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                '-fno-sanitize-recover=all','-I'+str(FW),str(FW/'nano_mac.c'),
                str(FW/'tests/nano_mac_test.c'),'-o',str(binary)],check=True,capture_output=True,text=True,timeout=30)
            r=subprocess.run([str(binary)],check=True,capture_output=True,text=True,timeout=10)
            self.assertIn('PASS MAC private generation, ownership, drain and failure retention',r.stdout)

    def test_wire_matches_frozen_oracle(self):
        oracle=json.loads((ROOT/'sources/mister-packages/testdata/oracles/fes-embedded-mac.json').read_text())
        go=(ROOT/'platform/embedded/tamago/embeddedcontrol/generated/fes_embedded_mac.go').read_text()
        values={k:int(v,16) for k,v in re.findall(r'(FesEmbedded\w+)\s+uint32\s+=\s+0x([0-9a-f]+)',go)}
        self.assertEqual(values,oracle['constants'])
        self.assertEqual(values['FesEmbeddedMacCall'],0xc2000305)

if __name__=='__main__':unittest.main()
