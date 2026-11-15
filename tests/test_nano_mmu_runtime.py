"""Execute the real bounded EL3 profile transition with injectable registers."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
FW=ROOT/'platform/embedded/firmware'
sys.path.insert(0,str(FW))
import nano_firmware_mmu as mmu


class NanoMMURuntimeTests(unittest.TestCase):
    def test_actual_transition_and_live_scope_reject_partial_failed_or_changed_state(self):
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);binary=base/'mmu'
            source=base/'constants.c'
            source.write_text('#include "nano_mmu.h"\n'+''.join(
                '_Static_assert('+name+'=='+hex(value)+'ULL,"static/live policy differs");\n'
                for name,value in (('FES_NANO_MMU_MAIR',mmu.MAIR),('FES_NANO_MMU_TCR',mmu.TCR),
                                   ('FES_NANO_MMU_TABLE_BYTES',mmu.TABLE_BYTES))))
            built=subprocess.run(['cc','-std=c11','-O2','-Wall','-Wextra','-Werror',
                '-fsanitize=undefined','-fno-sanitize-recover=all','-I'+str(FW),
                str(FW/'nano_mmu.c'),str(FW/'nano_mmu_memory.c'),str(FW/'tests/nano_mmu_test.c'),str(source),'-o',str(binary)],
                capture_output=True,text=True,timeout=30)
            self.assertEqual(built.returncode,0,built.stderr)
            result=subprocess.run([binary],capture_output=True,text=True,timeout=10)
            self.assertEqual(result.returncode,0,result.stderr+result.stdout)
            self.assertIn('PASS one-attempt MMU enable',result.stdout)


if __name__=='__main__':unittest.main()
