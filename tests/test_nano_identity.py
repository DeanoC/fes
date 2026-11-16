"""The secure read-only probe closes control before returning any identity."""
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'


class NanoIdentityTests(unittest.TestCase):
    def test_secure_reader_uses_actual_avalon_rtl_and_contains_failures(self):
        fpga=ROOT/'sources/misteross'
        with tempfile.TemporaryDirectory(prefix='fes-nano-reader-rtl-') as temp:
            base=Path(temp);objects=[]
            for name in ('nano_bridges.c','nano_identity.c'):
                obj=base/(name+'.o')
                result=subprocess.run(['cc','-std=c11','-Wall','-Wextra','-Werror',
                    '-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(FW),
                    '-c',str(FW/name),'-o',str(obj)],capture_output=True,text=True,timeout=30)
                self.assertEqual(result.returncode,0,result.stderr)
                objects.append(obj)
            command=['verilator','--cc','--exe','--build','-j','2','--top-module','fes_de25_embedded',
                     '-Wall','-Wno-UNUSEDSIGNAL','--Mdir',str(base/'obj'),
                     '-I'+str(fpga/'cores/fes-common/generated'),
                     "-DFES_DE25_COMPUTER_BUILD_ID=128'h00112233445566778899aabbccddeeff",
                     '-CFLAGS','-std=c++17 -O1 -UNDEBUG -fsanitize=undefined -fno-sanitize-recover=all -I'+str(FW),
                     '-LDFLAGS','-fsanitize=undefined '+' '.join(str(p) for p in objects),
                     str(fpga/'cores/fes-common/rtl/fes_embedded_control.sv'),
                     str(fpga/'prototypes/de25_board_control/fes_de25_embedded.sv'),
                     str(FW/'tests/nano_reader_rtl_test.cpp')]
            result=subprocess.run(command,capture_output=True,text=True,timeout=90)
            self.assertEqual(result.returncode,0,result.stdout+'\n'+result.stderr)
            result=subprocess.run([str(base/'obj/Vfes_de25_embedded')],capture_output=True,text=True,timeout=15)
            self.assertEqual(result.returncode,0,result.stdout+'\n'+result.stderr)
            self.assertRegex(result.stdout,r'PASS [1-9][0-9]+ secure reader/actual Avalon RTL cases; [1-9][0-9]+ successful reads; 12 one-shot writes')
            print(result.stdout.strip(),flush=True)

    def test_owned_identity_success_failures_and_containment(self):
        with tempfile.TemporaryDirectory() as temp:
            binary=Path(temp)/'identity'
            command=['cc','-std=c11','-Wall','-Wextra','-Werror','-fsanitize=undefined',
                     '-fno-sanitize-recover=all','-I'+str(FW),
                     *(str(FW/name) for name in ('nano_bridges.c','nano_identity.c','tests/nano_identity_test.c')),
                     '-o',str(binary)]
            subprocess.run(command,check=True,capture_output=True,text=True)
            result=subprocess.run([str(binary)],check=True,capture_output=True,text=True,timeout=10)
            self.assertIn('PASS 59 identity/control containment cases',result.stdout)
