"""File-backed AArch64 LOAD/section/HEX binding, independent of debug symbols."""
from pathlib import Path
import struct
import sys
import unittest
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'platform/embedded/nano'))
from recovery_reset_probe import check_elf_image


class ResetLoadedELFTests(unittest.TestCase):
    def fixture(self):
        image=bytes(i%251 for i in range(0x180));elf=bytearray(0x500)
        elf[:16]=b'\x7fELF\x02\x01\x01'+bytes(9)
        struct.pack_into('<HHIQQQIHHHHHH',elf,16,2,183,1,0,64,128,0,64,56,1,64,3,0)
        struct.pack_into('<IIQQQQQQ',elf,64,1,7,0x300,0,0,0x180,0x180,0x100)
        struct.pack_into('<IIQQQQIIQQ',elf,192,0,1,6,0,0x300,0x100,0,0,8,0)
        struct.pack_into('<IIQQQQIIQQ',elf,256,0,1,3,0x100,0x400,0x80,0,0,8,0)
        elf[0x300:0x480]=image
        symbols=(' 1: 0000000000000010 32 FUNC GLOBAL DEFAULT 1 fes_recovery_reset_begin\n'
                 ' 2: 0000000000000060 32 FUNC GLOBAL DEFAULT 1 fes_recovery_reset_report\n'
                 ' 3: 0000000000000100 32 OBJECT LOCAL DEFAULT 2 fes_recovery_reset\n')
        return elf,image,symbols

    def test_loaded_code_and_initialized_state_are_bound_to_exact_hex(self):
        elf,image,symbols=self.fixture();r=check_elf_image(bytes(elf),image,symbols)
        self.assertEqual(set(r['symbols']),{'fes_recovery_reset_begin','fes_recovery_reset_report','fes_recovery_reset'})
        self.assertEqual(r['symbols']['fes_recovery_reset']['address'],0x100)

    def test_nonloaded_state_code_wrong_segment_and_byte_drift_reject(self):
        for offset,fmt,value in ((260,'I',8),(264,'Q',1),(68,'I',4),
                (96,'Q',0x100),(280,'Q',0x401),(196,'I',8),
                (88,'Q',4),(56,'H',0),(32,'Q',0xffff)):
            elf,image,symbols=self.fixture();struct.pack_into('<'+fmt,elf,offset,value)
            with self.subTest(offset=offset,value=value),self.assertRaises(ValueError):
                check_elf_image(bytes(elf),image,symbols)
        elf,image,symbols=self.fixture();elf[0x300+0x80]^=1
        with self.assertRaises(ValueError):check_elf_image(bytes(elf),image,symbols)
        elf,image,symbols=self.fixture()
        with self.assertRaises(ValueError):check_elf_image(bytes(elf),image[:-1],symbols)
