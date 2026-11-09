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
RUNTIME_CONTRACT = dict(format=3, scope='matched-cold-el3-private-cache-v3',
    mair=MAIR, tcr=TCR, table_bytes=TABLE_BYTES, granule=PAGE, va_bits=32, pa_bits=32,
    memory_probe='private-line-clean-invalidate-reread-v1',
    enable_phase='after-startup-cache-maintenance-before-el3-exit',
    single_cpu='a55-cpu0-release-locked',
    boot_steps='admit-register-barrier-private-clean-v1',
    vendor_cache_policy='restore-caller-scr-and-return-v1', hardware_authority=False)
HERE = Path(__file__).resolve().parent
LD = 'bl31/bl31.ld.S'
LD_SHA = 'f6da064e38aa9b19dbab1d82dd427628ee38f83f6d12c0a9a68fd98a2e22177c'
COMMON_LD = 'include/common/bl_common.ld.h'
COMMON_LD_SHA = '713da3b695e23dabb3aa3bc4b453873b41dab466f137a4c6df368164784cc127'
VENDOR_CACHE = 'plat/intel/soc/agilex5/soc/agilex5_cache.S'
VENDOR_CACHE_SHA = 'f41d7e66d603aa9fb327bcbf653660592e865825f120e9a419abee87b339e360'
VENDOR_CACHE_BODY = '''func invalidate_cache_low_el
\tmrs\tx0,SCR_EL3
\torr\tx1,x0,#SCR_NS_BIT
\tmsr\tSCR_EL3, x1
\tisb
\ttlbi\tALLE2
\tdsb\tsy
\ttlbi\tALLE1
\tdsb\tsy
endfunc invalidate_cache_low_el'''
VENDOR_CACHE_CODE = struct.pack('<11I',
    0xd53e1100, 0xb2400001, 0xd51e1101, 0xd5033fdf,
    0xd50c871f, 0xd5033f9f, 0xd50c879f, 0xd5033f9f,
    0xd51e1100, 0xd5033fdf, 0xd65f03c0)


def restore_vendor_cache(text):
    if text.count(VENDOR_CACHE_BODY) != 1:
        raise ValueError('unreviewed lower-EL TLB invalidation helper')
    repaired = VENDOR_CACHE_BODY.replace('endfunc invalidate_cache_low_el',
        '\tmsr\tSCR_EL3, x0\n\tisb\n\tret\nendfunc invalidate_cache_low_el')
    return text.replace(VENDOR_CACHE_BODY, repaired)


def selected(profile, enabled):
    if type(enabled) is not bool:
        raise ValueError('explicit boolean firmware MMU selection required')
    if not enabled:
        return None
    if (ram.checked(profile) is None or profile.get('usb_shared') is not True or
            profile.get('initial_tape') is not True or profile.get('computer_reload') is not True or
            not isinstance(profile.get('hps_menu_program'), dict)):
        raise ValueError('firmware MMU requires the complete mixed ROM/USB/tape OCRAM profile')
    return dict(RUNTIME_CONTRACT)


def checked(profile):
    value = profile.get('firmware_mmu') if profile else None
    if value is None:
        return None
    expected = selected(profile, True)
    if json.dumps(value, sort_keys=True) != json.dumps(expected, sort_keys=True):
        raise ValueError('firmware MMU policy differs from its fixed producer')
    return expected


def install(source, profile, ram_binding=None):
    contract = checked(profile)
    if contract is None:
        return None
    if ram_binding is None:
        raise ValueError('MMU install requires the completed cold RAM binding')
    ram.verify_installed(source, ram_binding)
    for name, expected in ((LD, LD_SHA), (COMMON_LD, COMMON_LD_SHA),
                           (VENDOR_CACHE, VENDOR_CACHE_SHA)):
        if hashlib.sha256((source/name).read_bytes()).hexdigest() != expected:
            raise ValueError('unreviewed MMU linker source: '+name)
    ld = (source/LD).read_text()
    anchor = '    __RW_START__ = . ;'
    if ld.count(anchor) != 2:
        raise ValueError('MMU linker branch context changed')
    ld = ld.replace(anchor, '#include "nano_mmu_tables.ld.inc"\n'+anchor)
    common = (source/COMMON_LD).read_text()
    anchor = '*(SORT_BY_ALIGNMENT(.bss*))'
    if common.count(anchor) != 1:
        raise ValueError('MMU witness BSS context changed')
    common = common.replace(anchor, anchor+' *(.fes_mmu_witness)')
    entry_name = 'bl31/aarch64/bl31_entrypoint.S'
    entry = (source/entry_name).read_text()
    anchor = '\tfes_ram_uart_text fes_nano_ram_runtime_exit_before\n\tb\tel3_exit\n'
    if entry.count(anchor) != 1 or entry.index('fes_ram_bss_clean') > entry.index(anchor):
        raise ValueError('MMU enable requires the completed original cold exit maintenance')
    original_entry=entry
    entry = entry.replace(anchor, '\tfes_ram_uart_text fes_nano_ram_runtime_exit_before\n'
        '\tbl\tfes_nano_mmu_platform_boot\n\tcbnz\tx0, .Lfes_ram_park\n\tb\tel3_exit\n')
    include = source/'plat/intel/soc/common/include'
    cache = restore_vendor_cache((source/VENDOR_CACHE).read_text())
    files = [LD, COMMON_LD, VENDOR_CACHE, entry_name]
    (source/LD).write_text(ld);(source/COMMON_LD).write_text(common)
    (source/entry_name).write_text(entry)
    (source/VENDOR_CACHE).write_text(cache)
    (include/'nano_mmu_tables.ld.inc').write_text(linker_fragment())
    files.append(str((include/'nano_mmu_tables.ld.inc').relative_to(source)))
    for name in ('nano_mmu.h','nano_mmu_platform.h','nano_mmu_memory.h'):
        (include/name).write_bytes((HERE/name).read_bytes());files.append(str((include/name).relative_to(source)))
    for name in ('nano_mmu.c','nano_mmu_platform.c','nano_mmu_memory.c'):
        path=source/'plat/intel/soc/common'/name;path.write_bytes((HERE/name).read_bytes())
        files.append(str(path.relative_to(source)))
    make=source/'plat/intel/soc/agilex5/platform.mk'
    make.write_text(make.read_text()+'\nBL31_SOURCES += plat/intel/soc/common/nano_mmu.c plat/intel/soc/common/nano_mmu_platform.c plat/intel/soc/common/nano_mmu_memory.c\n')
    files.append(str(make.relative_to(source)))
    ram_binding['sources'][entry_name]=hashlib.sha256(entry.encode()).hexdigest()
    return dict(contract=contract, prior_ram_entry_sha256=hashlib.sha256(original_entry.encode()).hexdigest(),
                sources={name:hashlib.sha256((source/name).read_bytes()).hexdigest() for name in files})


def verify_installed(source, binding):
    if binding is not None and (binding.get('contract') != RUNTIME_CONTRACT or
            any(hashlib.sha256((source/name).read_bytes()).hexdigest()!=value for name,value in binding['sources'].items())):
        raise ValueError('installed MMU sources differ from the selected binding')


def verify_live_image(image, chain):
    proof=verify_image(image, chain)
    symbols=ram.symbols(image)
    address,size=symbols.get('fes_nano_mmu_witness',(0,0))
    if (size!=64 or address&63 or not ram.STACK_END<=address<=ram.LIMIT-size or
            not symbols['__BSS_START__'][0]<=address<address+size<=symbols['__BSS_END__'][0] or
            'fes_nano_mmu_platform_boot' not in symbols):
        raise ValueError('MMU private witness or late boot hook is missing/outside its reservation')
    helper,helper_size=symbols.get('invalidate_cache_low_el',(0,0))
    if helper_size!=len(VENDOR_CACHE_CODE) or helper&3 or not DDR_BASE<=helper<helper+helper_size<=proof['layout']['ro_end']:
        raise ValueError('selected lower-EL cache helper is missing/outside executable firmware')
    code,_=file_bytes(image,helper,helper_size)
    if code!=VENDOR_CACHE_CODE:
        raise ValueError('selected lower-EL cache helper does not restore SCR and return')
    return dict(proof, selected_policy=dict(RUNTIME_CONTRACT), witness=address, witness_bytes=size,
                startup_cache_helper=dict(address=helper,bytes=helper_size,sha256=hashlib.sha256(code).hexdigest()))


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
