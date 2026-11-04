"""Independent walks require only owned normal pages and one UART device page."""
import ctypes
import importlib.util
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
ENTRY = ROOT/'platform/embedded/entry'
TABLE_BYTES = 22*4096
PA = 0xfffffff000
XN = 1 << 54


def walk(raw, base, va):
    def at(table, index):
        offset = table-base+index*8
        if table%4096 or not 0 <= offset <= len(raw)-8:
            raise AssertionError('table pointer left the owned table allocation')
        return struct.unpack_from('<Q',raw,offset)[0]
    root = at(base,va>>30)
    if not root&1:return None
    if root&3 != 3:raise AssertionError('root must be a table')
    leaf = at(root&PA,(va>>21)&511)
    if not leaf&1:return None
    if leaf&3 == 1:
        return ((leaf&PA)&~((2<<20)-1))|(va&((2<<20)-1)),leaf
    if leaf&3 != 3:raise AssertionError('invalid middle descriptor')
    page = at(leaf&PA,(va>>12)&511)
    if not page&1:return None
    if page&3 != 3:raise AssertionError('invalid page descriptor')
    return (page&PA)|(va&4095),page


class EntryMapTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory(prefix='entry-map-',dir=ROOT/'out')
        path = Path(cls.tmp.name)/'map.so'
        subprocess.run([shutil.which('cc'),'-std=c11','-O2','-Wall','-Wextra','-Werror',
                        '-shared','-fPIC','-ffreestanding','-fno-builtin','-fsanitize=undefined',
                        '-fno-sanitize-recover=all','-I'+str(ENTRY),str(ENTRY/'map.c'),
                        str(ENTRY/'memory.c'),'-o',str(path)],check=True)
        cls.library = ctypes.CDLL(str(path))
        cls.plan = cls.library.fes_entry_map_plan
        cls.plan.argtypes = [ctypes.c_void_p]+[ctypes.c_uint64]*6
        cls.plan.restype = ctypes.c_int

    @classmethod
    def tearDownClass(cls):cls.tmp.cleanup()

    def generate(self, args, expected=1):
        guarded = ctypes.create_string_buffer(b'\xa5'*(TABLE_BYTES+64))
        before = guarded.raw
        self.assertEqual(self.plan(ctypes.byref(guarded,32),*args),expected)
        if not expected:self.assertEqual(guarded.raw,before)
        self.assertEqual(guarded.raw[:32],before[:32])
        self.assertEqual(guarded.raw[32+TABLE_BYTES:],before[32+TABLE_BYTES:])
        return guarded.raw[32:32+TABLE_BYTES]

    def test_exact_identity_attributes_execution_and_invalid_guards(self):
        for ram, image, size, uart in ((0x40000000,0x58080000,12<<20,0x09000000),
                                        (0x84000000,0x96080000,7<<20,0x10c02100),
                                        (0x84000000,0x96080000,33<<20,0x10c02100)):
            args=(image+0x10000,ram,ram+(256<<20),image,image+size,uart)
            raw=self.generate(args);base=args[0];device_page=uart&~4095
            def check(address):
                expected = 'ram' if ram <= address < ram+(256<<20) else 'loader' if image <= address < image+size else 'uart' if device_page <= address < device_page+4096 else None
                found=walk(raw,base,address)
                if expected is None:self.assertIsNone(found,hex(address));return
                self.assertIsNotNone(found,hex(address));physical,desc=found
                self.assertEqual(physical,address)
                self.assertTrue(desc&(1<<10),'access flag must already be set')
                self.assertEqual((desc>>6)&3,0,'privileged read/write only')
                if expected=='uart':
                    self.assertEqual((desc>>2)&7,0);self.assertEqual((desc>>8)&3,2);self.assertTrue(desc&XN)
                else:
                    self.assertEqual((desc>>2)&7,1);self.assertEqual((desc>>8)&3,3)
                    self.assertEqual(bool(desc&XN),expected=='ram')
            # Every possible 2MiB boundary in the 4GiB input address space,
            # every loader page, all table pages and exact neighboring guards.
            for block in range(0,1<<32,2<<20):
                check(block);check(block+(2<<20)-1)
            for page in range(image,image+size,4096):check(page);check(page+4095)
            for page in range(base,base+TABLE_BYTES,4096):check(page)
            for address in (0,uart,device_page-1,device_page+4096,ram-1,ram,ram+(256<<20)-1,
                            ram+(256<<20),image-1,image,image+size-1,image+size):check(address)

    def test_invalid_geometry_never_publishes_partial_tables(self):
        valid=(0x96090000,0x84000000,0x94000000,0x96080000,0x96880000,0x10c02100)
        for index,values in ((0,(0,valid[0]+1,valid[3]-4096,valid[4]-4096,2**64-4096)),
                             (1,(0,valid[1]+4096,valid[2],0x3fe00000)),
                             (2,(0,valid[1],valid[2]-4096,0xc0200000,2**32+0x200000)),
                             (3,(0,valid[3]+1,valid[1],0x58080000)),
                             (4,(valid[3],valid[4]+1,valid[3]+(33<<20)+4096,0xc0001000)),
                             (5,(0,0x9000001,0x9000f80,0x84000000,2**64-4))):
            for value in values:
                args=list(valid);args[index]=value
                with self.subTest(index=index,value=hex(value)):self.generate(args,0)
        self.assertEqual(self.plan(None,*valid),0)

    def test_python_model_uses_the_actual_pinned_c_planner(self):
        spec=importlib.util.spec_from_file_location('test_cache_model',ENTRY/'cache_model.py')
        model=importlib.util.module_from_spec(spec);spec.loader.exec_module(model)
        args=(0x96090000,0x84000000,0x94000000,0x96080000,0x96880000,0x10c02100)
        self.assertEqual(model.TABLE_BYTES,TABLE_BYTES)
        self.assertEqual(model.tables(Path(self.tmp.name)/'map.so',*args),self.generate(args))
