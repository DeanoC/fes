"""The secure read-only probe closes control before returning any identity."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'


class NanoIdentityTests(unittest.TestCase):
    def test_owned_identity_success_failures_and_containment(self):
        with tempfile.TemporaryDirectory() as temp:
            binary=Path(temp)/'identity'
            command=['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                     '-fno-sanitize-recover=all','-I'+str(FW),
                     *(str(FW/name) for name in ('nano_bridges.c','nano_identity.c','tests/nano_identity_test.c')),
                     '-o',str(binary)]
            subprocess.run(command,check=True,capture_output=True,text=True)
            result=subprocess.run([str(binary)],check=True,capture_output=True,text=True,timeout=10)
            self.assertIn('PASS 57 identity/control containment cases',result.stdout)
