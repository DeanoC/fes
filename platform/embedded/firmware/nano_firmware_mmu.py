"""Experimental, immutable EL3 tables; emission/inspection grants no boot access.

The linker resolves code/data boundaries and table pointers in the final image.
Tables occupy file-backed DDR inside the existing 2 MiB BL31 exclusion, rather
than consuming the small OCRAM stack/BSS reservation. Nothing here writes system
registers or changes the currently selected cold profile.
"""
import argparse
import hashlib
import json
from pathlib import Path
import struct

import nano_firmware_ram as ram

PAGE = 0x1000
DDR_BASE, BL31_LIMIT, DDR_LIMIT = 0x80000000, 0x80200000, 0xc0000000
NAMES = ('root', 'low', 'ocram', 'gic', 'ddr', 'firmware')
PREFIX = 'fes_nano_mmu_'
TABLE_BYTES = len(NAMES) * PAGE
# Match the pinned TF-A v2 EL3 descriptor conventions, including AP[1] RES1.
AF, AP_RES1, RO, NS, XN = 1 << 10, 1 << 6, 1 << 7, 1 << 5, 1 << 54
WB = AF | AP_RES1 | (3 << 8)       # Attr0: WB/WA, inner shareable
NC = AF | AP_RES1 | (2 << 8) | 8   # Attr2: Normal NC, outer shareable
DEVICE = AF | AP_RES1 | (2 << 8) | 4
MAIR = 0x4400ff                    # Attr2=NC, Attr1=nGnRnE, Attr0=WB/WA
TCR = (1 << 31) | (1 << 23) | (3 << 12) | (1 << 10) | (1 << 8) | 32
CONTRACT = dict(format=1, scope='experimental-el3-static-private-cache-v1',
                granule=PAGE, va_bits=32, pa_bits=32, mair=MAIR, tcr=TCR,
                table_bytes=TABLE_BYTES, ddr_limit=DDR_LIMIT,
                firmware_limit=BL31_LIMIT, runtime_enabled=False,
                hardware_authority=False)


def _leaf(address, attrs, level):
    return address | attrs | (3 if level == 3 else 1)


def _fixed(name, index):
    """Return (PA, attributes, level), a table name, or an unmapped entry."""
    if name == 'root':
        return {0: 'low', 2: 'ddr'}.get(index)
    if name == 'low':
        address = index << 21
        if index == 0:
            return 'ocram'
        if address == 0x1d000000:
            return 'gic'
        if 0x10000000 <= address < 0x18000000 or 0x20000000 <= address < 0x40000000:
            return address, DEVICE | NS | XN, 2
        if 0x18000000 <= address < 0x1d000000:
            return address, DEVICE | XN, 2
    if name == 'ocram' and index < 0x80000 // PAGE:
        address = index * PAGE
        return address, (WB if ram.BASE <= address < ram.LIMIT else NC) | XN, 3
    if name == 'gic' and index < 0x100000 // PAGE:
        return 0x1d000000 + index * PAGE, DEVICE | XN, 3
    if name == 'ddr':
        if index == 0:
            return 'firmware'
        return DDR_BASE + (index << 21), NC | NS | XN, 2
    return None


def layout(symbols):
    """Validate final linker boundaries before interpreting any table bytes."""
    required = ('__BL31_START__', '__RO_END__', '__DATA_START__', '__DATA_END__',
                '__BL31_END__', '__NOBITS_START__', '__NOBITS_END__',
                '__FES_MMU_START__', '__FES_MMU_END__')
    try:
        values = {name: symbols[name][0] for name in required}
        tables = {name: symbols[PREFIX + name][0] for name in NAMES}
    except KeyError as exc:
        raise ValueError('missing final EL3 mapping symbol') from exc
    ro, start, end, data, data_end, image_end = (values[name] for name in
        ('__RO_END__', '__FES_MMU_START__', '__FES_MMU_END__',
         '__DATA_START__', '__DATA_END__', '__BL31_END__'))
    if (values['__BL31_START__'] != DDR_BASE or not DDR_BASE < ro == start < end == data < data_end <= image_end <= BL31_LIMIT or
            any(value % PAGE for value in (ro, start, end, data, image_end)) or
            end - start != TABLE_BYTES or
            tables != {name: start + i * PAGE for i, name in enumerate(NAMES)} or
            values['__NOBITS_START__'] != ram.BASE or
            not ram.STACK_END < values['__NOBITS_END__'] <= ram.LIMIT):
        raise ValueError('EL3 tables/code/data exceed or change their private reservations')
    return dict(ro_end=ro, tables_start=start, tables_end=end,
                data_start=data, data_end=data_end, image_end=image_end,
                tables=tables)


def expected_tables(bounds):
    result = {}
    for name in NAMES:
        entries = []
        for index in range(512):
            entry = _fixed(name, index)
            if name == 'firmware':
                address = DDR_BASE + index * PAGE
                if address < bounds['ro_end']:
                    entry = address, WB | RO, 3
                elif bounds['tables_start'] <= address < bounds['tables_end']:
                    entry = address, WB | RO | XN, 3
                elif bounds['data_start'] <= address < ((bounds['data_end'] + PAGE - 1) & -PAGE):
                    entry = address, WB | XN, 3
            entries.append(bounds['tables'][entry] | 3 if isinstance(entry, str) else
                           _leaf(*entry) if entry is not None else 0)
        result[name] = struct.pack('<512Q', *entries)
    return result


def linker_fragment():
    lines = ['/* Experimental static EL3 tables. No register writes or boot grant. */',
             '    .fes_mmu_tables (READONLY) : ALIGN(0x1000) {',
             '        __FES_MMU_START__ = .;']
    for name in NAMES:
        lines.append('        ' + PREFIX + name + ' = .;')
        for index in range(512):
            entry = _fixed(name, index)
            if name == 'firmware':
                address = DDR_BASE + index * PAGE
                expression = ('({a} < __RO_END__) ? {code} : '
                    '({a} >= __FES_MMU_START__ && {a} < __FES_MMU_END__) ? {table} : '
                    '({a} >= __DATA_START__ && {a} < ALIGN(__DATA_END__, 0x1000)) ? {data} : 0').format(
                        a=hex(address), code=hex(_leaf(address, WB | RO, 3)),
                        table=hex(_leaf(address, WB | RO | XN, 3)),
                        data=hex(_leaf(address, WB | XN, 3)))
            elif isinstance(entry, str):
                expression = '(' + PREFIX + entry + ' | 3)'
            else:
                expression = hex(_leaf(*entry)) if entry is not None else '0'
            lines.append('        QUAD(' + expression + ');')
    lines += ['        __FES_MMU_END__ = .;', '    } >RAM',
              '    ASSERT(__FES_MMU_START__ == __RO_END__, "EL3 table alignment changed")',
              '    ASSERT(__FES_MMU_END__ <= 0x80200000, "EL3 table reservation exceeded")']
    return '\n'.join(lines) + '\n'


def inject_linker(text):
    anchor = '    __RW_START__ = . ;'
    if (text.count(anchor) != 1 or '__FES_MMU_' in text or
            '__RO_END__' not in text or '__NOBITS_START__' not in text):
        raise ValueError('expected the separate-NOBITS, combined-RO BL31 linker script')
    return text.replace(anchor, linker_fragment() + '\n' + anchor)


def file_bytes(image, address, size):
    """Extract one uniquely file-backed identity range, rejecting overlap."""
    if (len(image) < 64 or image[:7] != b'\x7fELF\x02\x01\x01' or
            struct.unpack_from('<H', image, 16)[0] != 2):
        raise ValueError('static ELF64 little-endian image required')
    offset = struct.unpack_from('<Q', image, 32)[0]
    stride, count = struct.unpack_from('<HH', image, 54)
    if stride != 56 or not count or offset + stride * count > len(image):
        raise ValueError('invalid EL3 load headers')
    matches = []
    for i in range(count):
        kind, flags, pos, va, pa, filesz, memsz, _ = struct.unpack_from('<IIQQQQQQ', image, offset + i * stride)
        if kind != 1 or not (address < va + memsz and va < address + size):
            continue
        if (va != pa or not (va <= address and address + size <= va + filesz) or
                filesz > memsz or pos + filesz > len(image) or flags & ~7 or not flags & 4):
            raise ValueError('EL3 table must be uniquely file-backed at its physical address')
        matches.append((image[pos + address - va:pos + address - va + size], flags))
    if len(matches) != 1:
        raise ValueError('EL3 table has missing or overlapping load storage')
    return matches[0]


def table_section(image, bounds):
    # The vendor combines .ro (SHF_WRITE|SHF_EXECINSTR) and .data into a single
    # RWX load segment. Runtime permissions come from our descriptors, not that
    # container flag. Require the new section itself to be alloc/read-only,
    # with a unique file-backed identity mapping checked independently below.
    if len(image) < 64:
        raise ValueError('missing ELF section table')
    offset = struct.unpack_from('<Q', image, 40)[0]
    stride, count, strings_index = struct.unpack_from('<HHH', image, 58)
    if stride != 64 or not 0 < count <= 4096 or strings_index >= count or offset + count * stride > len(image):
        raise ValueError('invalid ELF section table')
    sections = [struct.unpack_from('<IIQQQQIIQQ', image, offset + i * stride) for i in range(count)]
    names = sections[strings_index]
    if names[1] != 3 or names[4] + names[5] > len(image):
        raise ValueError('invalid ELF section names')
    strings = image[names[4]:names[4] + names[5]]
    found = []
    for index, section in enumerate(sections):
        end = strings.find(b'\0', section[0])
        if section[0] >= len(strings) or end < 0:
            raise ValueError('invalid ELF section name')
        if strings[section[0]:end] == b'.fes_mmu_tables':
            found.append((index, section))
    if len(found) != 1:
        raise ValueError('missing or duplicate EL3 table section')
    index, section = found[0]
    if (section[1:4] != (1, 2, bounds['tables_start']) or section[5] != TABLE_BYTES or
            section[8] != PAGE or section[4] + section[5] > len(image)):
        raise ValueError('EL3 table section is not exact read-only allocated file storage')
    actual, _ = file_bytes(image, bounds['tables_start'], TABLE_BYTES)
    if actual != image[section[4]:section[4] + section[5]]:
        raise ValueError('EL3 table section and load storage differ')
    return index


def verify_table_bytes(image, bounds):
    table_section(image, bounds)
    hashes = {}
    for name, expected in expected_tables(bounds).items():
        actual, _ = file_bytes(image, bounds['tables'][name], PAGE)
        if actual != expected:
            raise ValueError('EL3 ' + name + ' table permissions or descriptors differ')
        hashes[name] = hashlib.sha256(actual).hexdigest()
    return hashes


def verify_image(image, chain):
    # Reuse all existing segment, stack/BSS and source-chain checks. The new
    # descriptor proof does not replace any of the selected cold-image gates.
    from build_terasic import bl31_load_segments
    bl31_load_segments(image, firmware_ram=ram.CONTRACT, cold_chain=chain)
    bounds = layout(ram.symbols(image))
    hashes = verify_table_bytes(image, bounds)
    return dict(CONTRACT, status='static-table-layout-verified', layout=bounds,
                table_sha256=hashes, image_sha256=hashlib.sha256(image).hexdigest(),
                chain_sha256=chain, core_load_accepted=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest='operation', required=True)
    emit = sub.add_parser('emit', help='prepare a private linker-script copy; never edit a retained build')
    emit.add_argument('--linker', type=Path, required=True)
    emit.add_argument('--output', type=Path, required=True)
    check = sub.add_parser('verify', help='inspect linked bytes; no boot or hardware authority')
    check.add_argument('--elf', type=Path, required=True)
    check.add_argument('--cold-chain', required=True)
    check.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    data = (inject_linker(args.linker.read_text()) if args.operation == 'emit' else
            json.dumps(verify_image(args.elf.read_bytes(), args.cold_chain), sort_keys=True, indent=2) + '\n')
    with args.output.open('x') as stream:
        stream.write(data)


if __name__ == '__main__':
    main()
