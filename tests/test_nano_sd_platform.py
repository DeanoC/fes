"""Actual Nano SD platform callbacks with independent injected register events."""
from pathlib import Path
import subprocess
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
class NanoSDPlatformTests(unittest.TestCase):
    def test_real_callback_sequences_and_retained_failures(self):
        with tempfile.TemporaryDirectory(prefix='fes-nano-sd-platform-') as temp:
            binary=Path(temp)/'test'
            command=['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(FW)]
            command.extend(str(FW/name) for name in ('nano_cold.c','nano_bridges.c','nano_sd.c','nano_sd_platform.c','tests/nano_sd_platform_test.c'))
            command.extend(['-o',str(binary)])
            result=subprocess.run(command,capture_output=True,text=True,timeout=30)
            self.assertEqual(result.returncode,0,result.stderr)
            result=subprocess.run([str(binary)],capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertIn('PASS actual Nano SD platform callbacks',result.stdout)
if __name__=='__main__':unittest.main()
