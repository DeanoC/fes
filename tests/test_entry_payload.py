"""Exercise the freestanding gzip decoder and complete ELF exclusion plan."""
import ctypes
import hashlib
import importlib.util
import json
from pathlib import Path
import random
import shutil
import struct
import subprocess
import tempfile
import unittest
import zlib

ROOT = Path(__file__).resolve().parents[1]
ENTRY = ROOT / 'platform/embedded/entry'
spec = importlib.util.spec_from_file_location('fes_test_entry_payload', ENTRY / 'payload.py')
codec = importlib.util.module_from_spec(spec)
spec.loader.exec_module(codec)


class Segment(ctypes.Structure):
    _fields_ = [(name, ctypes.c_uint64) for name in ('address', 'memory_size', 'file_offset', 'file_size')]


class Plan(ctypes.Structure):
    _fields_ = [('entry', ctypes.c_uint64), ('count', ctypes.c_uint32), ('segments', Segment * 16)]


class EntryPayloadTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix='entry-payload-', dir=ROOT / 'out')
        cls.library_path = Path(cls.temporary.name) / 'payload.so'
        subprocess.run([shutil.which('cc'), '-std=c11', '-O2', '-Wall', '-Wextra', '-Werror',
                        '-ffreestanding', '-fno-builtin', '-shared', '-fPIC',
                        '-I'+str(ENTRY), '-I'+str(ENTRY/'vendor/puff'),
                        str(ENTRY/'payload.c'), str(ENTRY/'crc32.c'), str(ENTRY/'vendor/puff/puff.c'),
                        str(ENTRY/'elf_plan.c'), '-o', str(cls.library_path)], check=True)
        cls.library = ctypes.CDLL(str(cls.library_path))
        cls.unpack = cls.library.fes_entry_unpack
        cls.unpack.argtypes = [ctypes.c_void_p, ctypes.c_uint64, ctypes.c_void_p, ctypes.c_uint64,
                               ctypes.POINTER(ctypes.c_uint64)]
        cls.unpack.restype = ctypes.c_int
        cls.plan = cls.library.fes_entry_plan_decoded_elf
        cls.plan.argtypes = [ctypes.c_void_p, *([ctypes.c_uint64] * 7), ctypes.POINTER(Plan)]
        cls.plan.restype = ctypes.c_int

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def decode(self, packed, capacity):
        source = ctypes.create_string_buffer(packed)
        guarded = ctypes.create_string_buffer(b'\xa5'*16 + b'\x00'*capacity + b'\x5a'*16)
        written = ctypes.c_uint64(0x12345678)
        ok = self.unpack(source, len(packed), ctypes.byref(guarded, 16), capacity, ctypes.byref(written))
        self.assertEqual(guarded.raw[:16], b'\xa5'*16)
        self.assertEqual(guarded.raw[16+capacity:16+capacity+16], b'\x5a'*16)
        if not ok:
            self.assertEqual(written.value, 0, 'failed decoding supplied an output length')
        return ok, written.value, guarded.raw[16:16+capacity]

    def test_stream_types_and_lengths_match_python_and_c(self):
        randomizer = random.Random(0xDE25)
        for size in (64, 255, 4096, 65535, 1 << 20):
            for kind in ('random', 'repeated'):
                raw = randomizer.randbytes(size) if kind == 'random' else (b'FES-Tenfoot-'*((size+11)//12))[:size]
                for level, strategy in ((0, zlib.Z_DEFAULT_STRATEGY), (6, zlib.Z_DEFAULT_STRATEGY),
                                        (9, zlib.Z_FIXED), (1, zlib.Z_HUFFMAN_ONLY)):
                    with self.subTest(size=size, kind=kind, level=level, strategy=strategy):
                        compressor = zlib.compressobj(level, zlib.DEFLATED, -15, 8, strategy)
                        packed = codec.HEADER + compressor.compress(raw) + compressor.flush() + struct.pack('<II', zlib.crc32(raw), len(raw))
                        self.assertEqual(codec.decode(packed), raw)
                        ok, written, output = self.decode(packed, size)
                        self.assertEqual((ok, written, output), (1, size, raw))

    def test_truncated_streams_take_the_freestanding_error_return(self):
        raw = (b'bounded-native-entry' * 1024)
        packed = codec.encode(raw)
        for cut in range(len(packed)-18):
            damaged = packed[:10] + packed[10:10+cut] + packed[-8:]
            self.assertEqual(self.decode(damaged, len(raw))[0], 0, cut)
            with self.assertRaises(ValueError):
                codec.decode(damaged)

    def test_crc_length_header_trailing_members_and_output_bounds(self):
        raw = random.Random(25).randbytes(4096)
        packed = codec.encode(raw)
        variants = [b'', packed+packed, packed+b'x', packed[:10]+b'\x07'+packed[-8:]]
        for i in range(10):
            changed = bytearray(packed)
            changed[i] ^= 1
            variants.append(bytes(changed))
        for i in range(8):
            changed = bytearray(packed)
            changed[-1-i] ^= 1
            variants.append(bytes(changed))
        for size in (0, 63, len(raw)-1, len(raw)+1, codec.MAX_BYTES+1, 0xffffffff):
            changed = bytearray(packed)
            struct.pack_into('<I', changed, len(changed)-4, size)
            variants.append(bytes(changed))
        for changed in variants:
            self.assertEqual(self.decode(changed, len(raw))[0], 0)
            with self.assertRaises(ValueError):
                codec.decode(changed)
        for capacity in (0, 1, len(raw)-1):
            self.assertEqual(self.decode(packed, capacity)[0], 0)
        for _ in range(2):
            self.assertEqual(self.decode(packed, len(raw)), (1, len(raw), raw))

    def test_aliases_null_and_oversized_parameters_reject(self):
        packed = codec.encode(b'Z'*4096)
        buffer = ctypes.create_string_buffer(packed+b'\x00'*8192)
        written = ctypes.c_uint64(1)
        for source, size, target, capacity in ((None, len(packed), buffer, 4096),
                (buffer, len(packed), None, 4096),
                (buffer, len(packed), buffer, 4096),
                (buffer, len(packed), ctypes.byref(buffer, 16), 4096),
                (buffer, codec.MAX_BYTES+1, ctypes.byref(buffer, len(packed)), 4096),
                (buffer, len(packed), ctypes.byref(buffer, len(packed)), codec.MAX_BYTES+1)):
            written.value = 1
            self.assertEqual(self.unpack(source, size, target, capacity, ctypes.byref(written)), 0)
            self.assertEqual(written.value, 0)
        self.assertEqual(self.unpack(buffer, len(packed), buffer, 4096, None), 0)

    def test_producer_and_receipt_bind_stored_and_decoded_bytes(self):
        raw = b'ELF contents for bounded producer' * 128
        packed = codec.encode(raw)
        self.assertEqual(codec.encode(raw), packed)
        offset = 128
        image = b'\x00'*offset + packed + b'\x00'*32
        base, capacity = codec.workspace(0x84000000, 0x94000000)
        record = dict(payload_encoding=codec.ENCODING, payload_offset=offset,
                      payload_stored_size=len(packed), payload_stored_sha256=hashlib.sha256(packed).hexdigest(),
                      payload_size=len(raw), payload_sha256=hashlib.sha256(raw).hexdigest(),
                      ram_start=0x84000000, ram_end=0x94000000, decode_base=base, decode_capacity=capacity)
        self.assertEqual(codec.checked(record, image, raw), raw)
        for key, value in (('payload_encoding','none'), ('payload_stored_size',len(packed)+1),
                ('payload_stored_sha256','0'*64), ('payload_size',len(raw)-1),
                ('payload_sha256','0'*64), ('payload_offset',True), ('decode_base',base+4096),
                ('decode_capacity',capacity-4096)):
            changed = dict(record, **{key:value})
            with self.subTest(field=key), self.assertRaises(ValueError):
                codec.checked(changed, image, raw)
        with self.assertRaises(ValueError):
            codec.checked(record, image, raw[:-1])
        for raw in (b'', b'X'*63):
            with self.assertRaises(ValueError):
                codec.encode(raw)
        with self.assertRaises(ValueError):
            codec.workspace(0x40000000, 0x41000000)

    @staticmethod
    def elf():
        raw = bytearray(512)
        struct.pack_into('<16sHHIQQQIHHHHHH', raw, 0,
                b'\x7fELF\x02\x01\x01'+b'\x00'*9, 2, 183, 1, 0x84010000, 64,
                0, 0, 64, 56, 2, 0, 0, 0)
        for offset, flags, source, address, count in ((64,5,256,0x84010000,128),
                                                     (120,6,384,0x84200000,128)):
            struct.pack_into('<IIQQQQQQ', raw, offset, 1, flags, source, address, address, count, count, 1)
        return bytes(raw)

    def test_all_destinations_exclude_the_complete_decode_workspace(self):
        raw = self.elf()
        source = ctypes.create_string_buffer(raw)
        base, capacity = codec.workspace(0x84000000, 0x94000000)
        plan = Plan()
        self.assertEqual(self.plan(source, len(raw), 0x84000000, 0x94000000,
                                  0x96080000, 0x96880000, base, capacity, ctypes.byref(plan)), 1)
        self.assertEqual((plan.entry,plan.count), (0x84010000,2))
        for address in (base, base+capacity-4096):
            changed = bytearray(raw)
            struct.pack_into('<QQ', changed, 120+16, address, address)
            candidate = ctypes.create_string_buffer(bytes(changed))
            before = bytes(plan)
            self.assertEqual(self.plan(candidate, len(raw), 0x84000000, 0x94000000,
                                      0x96080000, 0x96880000, base, capacity, ctypes.byref(plan)), 0)
            self.assertEqual(bytes(plan), before, 'invalid scratch overlap published a partial load plan')
        for wrong_base, wrong_capacity in ((base+1,capacity), (base,capacity-1), (0xffffffffffffffff,capacity)):
            self.assertEqual(self.plan(source, len(raw), 0x84000000, 0x94000000,
                                      0x96080000, 0x96880000, wrong_base, wrong_capacity, ctypes.byref(plan)), 0)

    def test_decoder_mutations_under_address_and_undefined_sanitizers(self):
        source = Path(self.temporary.name) / 'sanitizer.c'
        binary = source.with_suffix('')
        sample = Path(self.temporary.name) / 'sanitizer.gz'
        raw = (random.Random(25).randbytes(512) * 8)
        sample.write_bytes(codec.encode(raw))
        source.write_text(r"""
#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include "payload.h"
int main(int argc,char **argv) {
    uint8_t input[16384],changed[16416],output[8256];
    assert(argc==2);FILE *f=fopen(argv[1],"rb");assert(f);
    size_t size=fread(input,1,sizeof(input),f);assert(feof(f) && !ferror(f));fclose(f);
    assert(size>18 && size<sizeof(input));
    for(unsigned i=0;i<1024;i++) {
        unsigned offset=i%16;memcpy(changed+offset,input,size);
        if(i)changed[offset+10+(i%(size-18))]^=1U<<(i%8);
        memset(output,0xa5,sizeof(output));uint64_t written=123;
        int ok=fes_entry_unpack(changed+offset,size,output+16+offset,4096,&written);
        assert((ok && written==4096) || (!ok && !written));
        for(unsigned j=0;j<16+offset;j++)assert(output[j]==0xa5);
        for(unsigned j=16+offset+4096;j<sizeof(output);j++)assert(output[j]==0xa5);
    }
    for(size_t cut=1;cut<size-18;cut++) {
        memcpy(changed,input,10+cut);memcpy(changed+10+cut,input+size-8,8);
        uint64_t written=123;
        assert(!fes_entry_unpack(changed,18+cut,output+16,4096,&written) && !written);
    }
    puts("ENTRY_PAYLOAD_SANITIZERS_PASS");return 0;
}
""")
        subprocess.run([shutil.which('cc'), '-std=c11', '-O1', '-Wall', '-Wextra', '-Werror',
                        '-fsanitize=address,undefined', '-fno-sanitize-recover=all',
                        '-I'+str(ENTRY), '-I'+str(ENTRY/'vendor/puff'), str(source),
                        str(ENTRY/'payload.c'), str(ENTRY/'crc32.c'), str(ENTRY/'vendor/puff/puff.c'), '-o', str(binary)], check=True)
        completed = subprocess.run([str(binary), str(sample)], capture_output=True, text=True, timeout=30)
        self.assertEqual(completed.returncode, 0, completed.stdout+completed.stderr)
        self.assertIn('ENTRY_PAYLOAD_SANITIZERS_PASS', completed.stdout)

    def test_vendor_bytes_match_the_pinned_licensed_source(self):
        pin = json.loads((ENTRY/'vendor/puff/upstream.json').read_text())
        self.assertEqual(pin['revision'], '51b7f2abdade71cd9bb0e7a373ef2610ec6f9daf')
        for name, digest in pin['sha256'].items():
            self.assertEqual(hashlib.sha256((ENTRY/'vendor/puff'/name).read_bytes()).hexdigest(), digest)


if __name__ == '__main__':
    unittest.main()
