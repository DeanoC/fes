"""Walk actual linker-produced EL3 tables, including permissions and holes."""
import copy
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'platform/embedded/firmware'))
import nano_firmware_mmu as mmu
import nano_firmware_ram as ram

SCRIPT = '''
ENTRY(bl31_entrypoint)
MEMORY { RAM (rwx) : ORIGIN = 0x80000000, LENGTH = 0x200000
         PRIVATE (rw) : ORIGIN = 0x50000, LENGTH = 0x10000 }
SECTIONS {
    .ro : {
        __BL31_START__ = .;
        *(.text) *(.rodata)
        . = ALIGN(0x1000);
        __RO_END__ = .;
    } >RAM
    __RW_START__ = . ;
    .data : { __DATA_START__ = .; *(.data) __DATA_END__ = .; } >RAM
    . = ALIGN(0x1000);
    __BL31_END__ = .;
    .stacks (NOLOAD) : { __NOBITS_START__ = .; . += 0x8000; } >PRIVATE
    .bss (NOLOAD) : { *(.bss) __NOBITS_END__ = .; } >PRIVATE
    /DISCARD/ : { *(.note*) *(.eh_frame*) }
}
'''


def walk(image, bounds, address):
    """Independent three-level 4 KiB table walk of the linked bytes."""
    if not 0 <= address < 1 << 32:
        return None
    table = bounds['tables']['root']
    for level, shift in ((1, 30), (2, 21), (3, 12)):
        raw, _ = mmu.file_bytes(image, table + ((address >> shift) & 511) * 8, 8)
        descriptor = int.from_bytes(raw, 'little')
        kind = descriptor & 3
        if not kind:
            return None
        if level < 3 and kind == 3:
            table = descriptor & 0x0000fffffffff000
            continue
        if kind != (3 if level == 3 else 1):
            raise ValueError('invalid descriptor type')
        return dict(pa=(descriptor & (0x0000ffffffffffff & -(1 << shift))) | (address & ((1 << shift) - 1)),
                    writable=not bool(descriptor & (1 << 7)),
                    executable=not bool(descriptor & (1 << 54)),
                    ns=bool(descriptor & (1 << 5)),
                    attr=(descriptor >> 2) & 7, share=(descriptor >> 8) & 3,
                    accessed=bool(descriptor & (1 << 10)),
                    ap_res1=bool(descriptor & (1 << 6)))
    raise AssertionError('walk did not terminate')


class NanoFirmwareMMUTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which('cc') or not shutil.which('ld'):
            raise unittest.SkipTest('host compiler and GNU linker required')
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        directory = Path(cls.temp.name)
        source = directory / 'image.S'
        source.write_text('''.global bl31_entrypoint
.text
bl31_entrypoint: .byte 0
.space 0x1200
.data
.long 0x12345678
.bss
.space 0x100
.section .note.GNU-stack,"",@progbits
''')
        script = directory / 'image.ld'
        script.write_text(mmu.inject_linker(SCRIPT))
        for command in (['cc', '-c', str(source), '-o', str(directory / 'image.o')],
                        ['ld', '-T', str(script), str(directory / 'image.o'), '-o', str(directory / 'image.elf')]):
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            if result.returncode:
                raise AssertionError(result.stderr)
        cls.image = (directory / 'image.elf').read_bytes()
        cls.symbols = ram.symbols(cls.image)
        cls.bounds = mmu.layout(cls.symbols)

    def test_linker_final_bytes_match_independent_expected_tables(self):
        hashes = mmu.verify_table_bytes(self.image, self.bounds)
        self.assertEqual(set(hashes), set(mmu.NAMES))
        self.assertEqual(self.bounds['tables_end'] - self.bounds['tables_start'], 24576)
        self.assertEqual(self.bounds['data_start'], self.bounds['tables_end'])
        self.assertEqual(self.symbols['__NOBITS_END__'][0], 0x58100)

    def assert_access(self, address, *, writable, executable, ns, attr):
        value = walk(self.image, self.bounds, address)
        self.assertIsNotNone(value, hex(address))
        self.assertEqual(value, dict(pa=address, writable=writable, executable=executable,
                                    ns=ns, attr=attr, share=3 if attr == 0 else 2,
                                    accessed=True, ap_res1=True), hex(address))

    def test_code_tables_writable_state_and_stack_are_separate(self):
        for address in (mmu.DDR_BASE, self.bounds['ro_end'] - 1):
            self.assert_access(address, writable=False, executable=True, ns=False, attr=0)
        for address in (self.bounds['tables_start'], self.bounds['tables_end'] - 1):
            self.assert_access(address, writable=False, executable=False, ns=False, attr=0)
        for address in (self.bounds['data_start'], self.bounds['data_end'] - 1,
                        ram.BASE, ram.STACK_END, ram.LIMIT - 1):
            self.assert_access(address, writable=True, executable=False, ns=False, attr=0)
        self.assertIsNone(walk(self.image, self.bounds, self.bounds['image_end']))
        self.assertIsNone(walk(self.image, self.bounds, 0x801fffff))

    def test_shared_dma_rom_reader_and_usb_buffers_remain_noncacheable(self):
        for address in (0x80200000, 0x84000000, 0x94800000, 0x94ffffff,
                        0x95000000, 0x957fffff, 0xbff00008, 0xbfffffff):
            self.assert_access(address, writable=True, executable=False, ns=True, attr=2)
        for address in (0, ram.BASE - 1, ram.LIMIT, 0x7ffff):
            self.assert_access(address, writable=True, executable=False, ns=False, attr=2)
        self.assertIsNone(walk(self.image, self.bounds, 0x80000))

    def test_mmio_remains_device_and_security_regions_do_not_merge(self):
        for address in (0x10c02100, 0x10d11000, 0x16000000, 0x20010400, 0x3fffffff):
            self.assert_access(address, writable=True, executable=False, ns=True, attr=1)
        for address in (0x18000000, 0x1c000100, 0x1d000000, 0x1d0fffff):
            self.assert_access(address, writable=True, executable=False, ns=False, attr=1)
        for address in (0x1d100000, 0x1fffffff, 0x40000000, 0x7fffffff,
                        0xc0000000, 0xffffffff, 0x100000000, -1):
            self.assertIsNone(walk(self.image, self.bounds, address), hex(address))

    def test_every_linked_leaf_is_identity_and_never_writable_executable(self):
        # Enumerate leaf entries instead of sampling: all reachable mappings,
        # table pointers, AF/AP RES1, NS and cacheable range coverage are checked.
        pending = [(self.bounds['tables']['root'], 1, 0)]
        visited = set()
        while pending:
            table, level, base = pending.pop()
            self.assertNotIn(table, visited)
            visited.add(table)
            raw, _ = mmu.file_bytes(self.image, table, 4096)
            for index, descriptor in enumerate(struct.unpack('<512Q', raw)):
                if not descriptor:
                    continue
                shift = {1: 30, 2: 21, 3: 12}[level]
                address = base + (index << shift)
                self.assertLess(address, 1 << 32)
                if level < 3 and descriptor & 3 == 3:
                    self.assertEqual(descriptor & ~0x0000fffffffff003, 0)
                    pending.append((descriptor & 0x0000fffffffff000, level + 1, address))
                    continue
                value = walk(self.image, self.bounds, address)
                self.assertEqual(value['pa'], address)
                self.assertFalse(value['writable'] and value['executable'])
                self.assertTrue(value['accessed'] and value['ap_res1'])
                if value['attr'] == 0:
                    self.assertFalse(value['ns'])
                    self.assertTrue(ram.BASE <= address < ram.LIMIT or
                                    mmu.DDR_BASE <= address < self.bounds['image_end'])
        self.assertEqual(visited, set(self.bounds['tables'].values()))

    def test_corrupted_descriptors_pointers_permissions_or_storage_are_rejected(self):
        for name, address in self.bounds['tables'].items():
            corrupt = bytearray(self.image)
            # Find the file offset through the real unique table bytes.
            page, _ = mmu.file_bytes(self.image, address, mmu.PAGE)
            offset = self.image.find(page)
            self.assertEqual(offset, self.image.rfind(page))
            corrupt[offset] ^= 0x20
            with self.subTest(table=name), self.assertRaises(ValueError):
                mmu.verify_table_bytes(corrupt, self.bounds)
        corrupt = bytearray(self.image)
        index = mmu.table_section(self.image, self.bounds)
        shoff = struct.unpack_from('<Q', corrupt, 40)[0]
        struct.pack_into('<Q', corrupt, shoff + index * 64 + 8, 3)
        with self.assertRaises(ValueError):
            mmu.verify_table_bytes(corrupt, self.bounds)

    def test_boundary_drift_overflow_missing_tables_and_repeated_install_reject(self):
        for symbol, value in (('__NOBITS_START__', 0x4f000), ('__NOBITS_END__', 0x60001),
                              ('__BL31_END__', 0x80201000), ('__DATA_START__', self.bounds['tables_start']),
                              ('__RO_END__', self.bounds['ro_end'] + 1),
                              ('fes_nano_mmu_root', self.bounds['tables_start'] + 8)):
            symbols = copy.deepcopy(self.symbols)
            symbols[symbol] = value, 0
            with self.subTest(symbol=symbol), self.assertRaises(ValueError):
                mmu.layout(symbols)
        missing = dict(self.symbols)
        del missing['fes_nano_mmu_gic']
        with self.assertRaises(ValueError):
            mmu.layout(missing)
        for text in (SCRIPT.replace('__RO_END__', '__OLD_END__'),
                     SCRIPT + '    __RW_START__ = . ;', mmu.inject_linker(SCRIPT)):
            with self.assertRaises(ValueError):
                mmu.inject_linker(text)

    def test_load_container_flags_do_not_replace_table_mapping_permissions(self):
        changed = bytearray(self.image)
        phoff = struct.unpack_from('<Q', changed, 32)[0]
        count = struct.unpack_from('<H', changed, 56)[0]
        for index in range(count):
            pos = phoff + index * 56
            kind, _, _, va, _, _, memsz, _ = struct.unpack_from('<IIQQQQQQ', changed, pos)
            if kind == 1 and va <= self.bounds['tables_start'] < va + memsz:
                struct.pack_into('<I', changed, pos + 4, 7)
        # The pinned vendor's .ro has WAX section flags and shares an RWX load
        # container with data. Our separate section and runtime leaves stay RO.
        mmu.verify_table_bytes(changed, self.bounds)
        self.assertFalse(walk(changed, self.bounds, self.bounds['tables_start'])['writable'])

    def test_missing_truncated_aliased_or_nonidentity_load_storage_rejects(self):
        phoff = struct.unpack_from('<Q', self.image, 32)[0]
        count = struct.unpack_from('<H', self.image, 56)[0]
        target = self.bounds['tables_start']
        positions = [phoff + i * 56 for i in range(count)]
        pos = next(p for p in positions if
                   (lambda row: row[0] == 1 and row[3] <= target < row[3] + row[6])(
                       struct.unpack_from('<IIQQQQQQ', self.image, p)))
        for offset, fmt, value in ((0, '<I', 0), (4, '<I', 0), (24, '<Q', target + 4096),
                                   (32, '<Q', 0)):
            corrupt = bytearray(self.image)
            struct.pack_into(fmt, corrupt, pos + offset, value)
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                mmu.verify_table_bytes(corrupt, self.bounds)
        other = next(p for p in positions if p != pos)
        corrupt = bytearray(self.image)
        corrupt[other:other + 56] = corrupt[pos:pos + 56]
        with self.assertRaises(ValueError):
            mmu.verify_table_bytes(corrupt, self.bounds)
        for damaged in (self.image[:40], self.image[:phoff + 8]):
            with self.assertRaises(ValueError):
                mmu.verify_table_bytes(damaged, self.bounds)

    def test_host_link_does_not_qualify_as_a_cold_aarch64_image(self):
        with self.assertRaises(ValueError):
            mmu.verify_image(self.image, '42' * 32)
        self.assertFalse(mmu.CONTRACT['runtime_enabled'] or mmu.CONTRACT['hardware_authority'])

    def test_mair_keeps_device_completion_strict_and_shared_memory_uncached(self):
        self.assertEqual(mmu.MAIR & 255, 0xff)       # Private Normal WB/WA
        self.assertEqual((mmu.MAIR >> 8) & 255, 0)   # Device-nGnRnE, no early ACK
        self.assertEqual((mmu.MAIR >> 16) & 255, 0x44)  # Shared Normal NC
        self.assertEqual(mmu.MAIR >> 24, 0)


if __name__ == '__main__':
    unittest.main()
