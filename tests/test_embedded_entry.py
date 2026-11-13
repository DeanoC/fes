"""The native entry admits complete bounded ELF plans before any target write."""
import ctypes
import importlib.util
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
ENTRY = ROOT / 'platform/embedded/entry'
spec = importlib.util.spec_from_file_location('embedded_entry_build', ENTRY / 'build.py')
builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(builder)


class Segment(ctypes.Structure):
    _fields_ = [(name, ctypes.c_uint64) for name in ('address', 'memory_size', 'file_offset', 'file_size')]


class Plan(ctypes.Structure):
    _fields_ = [('entry', ctypes.c_uint64), ('count', ctypes.c_uint32), ('segments', Segment * 16)]


def image():
    data = bytearray(0x3000)
    ident = b'\x7fELF\x02\x01\x01' + bytes(9)
    struct.pack_into('<16sHHIQQQIHHHHHH', data, 0, ident, 2, 183, 1, 0x40010000,
                     64, 0, 0, 64, 56, 2, 0, 0, 0)
    struct.pack_into('<IIQQQQQQ', data, 64, 1, 5, 0x1000, 0x40010000, 0x40010000, 64, 0x100, 0x1000)
    struct.pack_into('<IIQQQQQQ', data, 120, 1, 6, 0x2000, 0x40020000, 0x40020000, 32, 0x200, 0x1000)
    data[0x1000:0x1040] = bytes(range(64))
    data[0x2000:0x2020] = bytes(range(32))
    return data


class EntryPlanTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        lib = Path(cls.tmp.name) / 'entry.so'
        subprocess.run([shutil.which('cc') or 'cc', '-std=c11', '-Wall', '-Wextra', '-Werror', '-fPIC', '-shared',
                        '-fsanitize=undefined', '-fno-sanitize-recover=all', '-I', str(ENTRY),
                        str(ENTRY / 'elf_plan.c'), '-o', str(lib)], check=True)
        cls.library = ctypes.CDLL(str(lib))
        cls.plan = cls.library.fes_entry_plan_elf
        cls.plan.argtypes = [ctypes.POINTER(ctypes.c_uint8), ctypes.c_uint64, ctypes.c_uint64,
                             ctypes.c_uint64, ctypes.c_uint64, ctypes.c_uint64, ctypes.POINTER(Plan)]
        cls.plan.restype = ctypes.c_int

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def check(self, data, expected):
        source = (ctypes.c_uint8 * len(data)).from_buffer_copy(data)
        plan = Plan()
        ctypes.memset(ctypes.byref(plan), 0xa5, ctypes.sizeof(plan))
        before = bytes(plan)
        result = self.plan(source, len(data), 0x40000000, 0x50000000,
                           0x42080000, 0x42100000, ctypes.byref(plan))
        self.assertEqual(result, expected)
        if not expected:
            self.assertEqual(bytes(plan), before, 'rejected image published a partial load plan')
        return plan

    def test_valid_static_image_preserves_file_and_bss_extents(self):
        result = self.check(image(), 1)
        self.assertEqual((result.entry, result.count), (0x40010000, 2))
        self.assertEqual((result.segments[0].address, result.segments[0].file_offset,
                          result.segments[0].file_size, result.segments[0].memory_size),
                         (0x40010000, 0x1000, 64, 0x100))
        self.assertEqual((result.segments[1].address, result.segments[1].file_size,
                          result.segments[1].memory_size), (0x40020000, 32, 0x200))

    def test_rejects_invalid_metadata_and_unusable_entry(self):
        mutations = [
            (0, '<B', 0), (5, '<B', 2), (16, '<H', 3), (18, '<H', 62),
            (24, '<Q', 0x40010002), (24, '<Q', 0x40010080),
            (32, '<Q', 0xfffffffffffffff0), (54, '<H', 55), (56, '<H', 65),
            (64, '<I', 3), (64, '<I', 2),
        ]
        for offset, fmt, value in mutations:
            with self.subTest(offset=offset, value=value):
                data = image()
                struct.pack_into(fmt, data, offset, value)
                self.check(data, 0)

    def test_rejects_out_of_bounds_overlapping_or_malformed_segments(self):
        mutations = [
            (64 + 8, '<Q', 0xfffffffffffffff0),  # file offset overflow
            (64 + 24, '<Q', 0x40011000),         # virtual/physical mismatch
            (64 + 40, '<Q', 1),                  # file longer than memory
            (64 + 48, '<Q', 3),                  # invalid alignment
            (120 + 16, '<Q', 0x40000000),        # early runtime tables
            (120 + 16, '<Q', 0x42080000),        # live staging image
            (120 + 16, '<Q', 0x40010000),        # other load segment
            (120 + 16, '<Q', 0x4fff0000),        # initial Go stack
            (120 + 40, '<Q', 0xffffffffffffffff), # RAM range overflow
        ]
        for offset, fmt, value in mutations:
            with self.subTest(offset=offset, value=value):
                data = image()
                struct.pack_into(fmt, data, offset, value)
                if offset == 120 + 16:
                    struct.pack_into('<Q', data, 120 + 24, value)
                self.check(data, 0)


class StagingTests(unittest.TestCase):
    def test_staging_is_separate_from_go_and_declared_owners(self):
        exclusions = [(0x82000000, 0x92000000), (0x80000000, 0x82000000),
                      (0x94000000, 0x94200000)]
        builder.validate_staging(0x93080000, 0x93480000, 0x80000000, 0xc0000000, exclusions)
        for start, end in ((0x82080000, 0x82480000), (0x80080000, 0x80480000),
                           (0x93e80000, 0x94280000), (0xbfe80000, 0xc0280000)):
            with self.subTest(start=start), self.assertRaises(ValueError):
                builder.validate_staging(start, end, 0x80000000, 0xc0000000, exclusions)
        exclusions += [(0x94800000,0x95000000),(0x95000000,0x95200000)]
        builder.validate_staging(0x96080000,0x96480000,0x80000000,0xc0000000,exclusions)
        for start,end in ((0x94880000,0x94a80000),(0x95080000,0x95090000),(0x951ff000,0x95201000)):
            with self.subTest(cold_arena_start=start),self.assertRaises(ValueError):
                builder.validate_staging(start,end,0x80000000,0xc0000000,exclusions)


if __name__ == '__main__':
    unittest.main()
