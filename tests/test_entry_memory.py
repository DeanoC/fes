"""Exact copy/clear extents and CRC over every alignment, with no extra access."""
import ctypes
from pathlib import Path
import random
import shutil
import subprocess
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
ENTRY = ROOT / 'platform/embedded/entry'


class EntryMemoryTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory(prefix='entry-memory-', dir=ROOT / 'out')
        library = Path(cls.tmp.name) / 'memory.so'
        subprocess.run([shutil.which('cc'), '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror',
                        '-ffreestanding', '-fno-builtin', '-fsanitize=undefined', '-fno-sanitize-recover=all',
                        '-fPIC', '-shared', '-I'+str(ENTRY), str(ENTRY/'memory.c'), str(ENTRY/'crc32.c'),
                        '-o', str(library)], check=True)
        cls.lib = ctypes.CDLL(str(library))
        cls.copy = cls.lib.fes_entry_copy
        cls.copy.argtypes = [ctypes.c_void_p, ctypes.c_void_p, ctypes.c_uint64]
        cls.copy.restype = None
        cls.zero = cls.lib.fes_entry_zero
        cls.zero.argtypes = [ctypes.c_void_p, ctypes.c_uint64]
        cls.zero.restype = None
        cls.crc = cls.lib.fes_entry_crc32
        cls.crc.argtypes = [ctypes.c_void_p, ctypes.c_uint64, ctypes.POINTER(ctypes.c_uint32)]
        cls.crc.restype = ctypes.c_int

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def test_copy_and_zero_preserve_every_byte_outside_the_exact_extent(self):
        data = random.Random(25).randbytes(65536+128)
        for length in (*range(81), 127, 128, 129, 4095, 4096, 4097, 65535):
            for source_offset in range(8):
                source = ctypes.create_string_buffer(data)
                for destination_offset in range(8):
                    with self.subTest(length=length, source=source_offset, destination=destination_offset):
                        offset = 16+destination_offset
                        destination = ctypes.create_string_buffer(b'\xa5'*(length+64))
                        self.copy(ctypes.byref(destination,offset), ctypes.byref(source,source_offset),length)
                        expected = b'\xa5'*offset + data[source_offset:source_offset+length] + b'\xa5'*(len(destination)-offset-length-1)+b'\0'
                        self.assertEqual(destination.raw, expected)
                        self.zero(ctypes.byref(destination,offset), length)
                        self.assertEqual(destination.raw, b'\xa5'*offset + bytes(length) + expected[offset+length:])
                self.assertEqual(source.raw, data+b'\0')
        self.copy(None, None, 0)
        self.zero(None, 0)

    def test_crc_matches_ieee_oracle_for_byte_edges_and_bulk_lengths(self):
        data = random.Random(0x25).randbytes(65536+16)
        source = ctypes.create_string_buffer(data)
        for offset in range(16):
            for length in (*range(81), 255, 4095, 4096, 65535):
                checksum = ctypes.c_uint32(123)
                self.assertEqual(self.crc(ctypes.byref(source,offset), length, ctypes.byref(checksum)),1)
                self.assertEqual(checksum.value,zlib.crc32(data[offset:offset+length]))
        known = ctypes.create_string_buffer(b'123456789')
        result = ctypes.c_uint32()
        self.assertEqual(self.crc(known,9,ctypes.byref(result)),1)
        self.assertEqual(result.value,0xcbf43926)
        self.assertEqual(self.crc(None,0,ctypes.byref(result)),1)
        self.assertEqual(result.value,0)
        for pointer, length in ((None,1),(source,(32<<20)+1),(ctypes.c_void_p(2**64-4),8)):
            result.value = 123
            self.assertEqual(self.crc(pointer,length,ctypes.byref(result)),0)
            self.assertEqual(result.value,0)
        self.assertEqual(self.crc(source,8,None),0)

    def test_exact_allocations_under_address_and_alignment_sanitizers(self):
        source = Path(self.tmp.name)/'guards.c'
        source.write_text(r'''
#include <assert.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "memory.h"
int main(void) {
    for (unsigned n=0;n<260;n++) for (unsigned a=0;a<8;a++) for (unsigned b=0;b<8;b++) {
        /* The final byte is next to the allocator's poison redzone. */
        uint8_t *s=malloc(n+a+1),*d=malloc(n+b+1);assert(s && d);
        for (unsigned i=0;i<n+a+1;i++)s[i]=(i*17+3)&255;
        memset(d,0xa5,n+b+1);
        fes_entry_copy(d+b+1,s+a+1,n);
        for(unsigned i=0;i<b+1;i++)assert(d[i]==0xa5);
        assert(!memcmp(d+b+1,s+a+1,n));
        fes_entry_zero(d+b+1,n);
        for(unsigned i=0;i<b+1;i++)assert(d[i]==0xa5);
        for(unsigned i=0;i<n;i++)assert(!d[b+1+i]);
        free(s);free(d);
    }
    return 0;
}
''')
        binary = source.with_suffix('')
        subprocess.run([shutil.which('cc'), '-std=c11', '-O1', '-Wall', '-Wextra', '-Werror',
                        '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                        '-I'+str(ENTRY),str(source),str(ENTRY/'memory.c'),'-o',str(binary)],check=True)
        result = subprocess.run([str(binary)],capture_output=True,text=True,timeout=30)
        self.assertEqual(result.returncode,0,result.stdout+result.stderr)


if __name__ == '__main__':
    unittest.main()
