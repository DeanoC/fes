"""Private selected OCRAM layout for the matched SPL/BL31/native cold chain."""
import hashlib
import importlib.util
import json
import re
from pathlib import Path
import struct
import nano_firmware_runtime

HERE = Path(__file__).resolve().parent
_status_spec = importlib.util.spec_from_file_location('fes_early_fault_status', HERE.parent/'nano/fault_status.py')
fault_status = importlib.util.module_from_spec(_status_spec)
_status_spec.loader.exec_module(fault_status)
BASE, LIMIT, STACK_END = 0x50000, 0x60000, 0x58000
COOKIE = 0x464e52414d000001
ENTRY = 'bl31/aarch64/bl31_entrypoint.S'
ENTRY_SHA = '2cb7c9d1d7845f8d1338acb77b074da3975e17342127fff42d5ff37509f0efde'
COMMON = 'include/arch/aarch64/el3_common_macros.S'
COMMON_SHA = '731852658339cd149d0fd08762f0a70c9d5adba0336b402ce22532c98feebde6'
CACHE = 'lib/aarch64/cache_helpers.S'
CACHE_SHA = '675fe70fd4919347c9ac8fd3ffd5b3f09ec53f948e926ce51cf342083a68b8e6'
VENDOR_PHASES = ('enter',) + tuple(name+'-'+edge for name in
    ('reset','cpu-data','rw-cache','nobits-cache','bss','stack','setup') for edge in ('before','after'))
PLATFORM = 'plat/intel/soc/agilex5/include/socfpga_plat_def.h'
INCLUDE = 'plat/intel/soc/common/include'
CONTRACT = dict(format=2, scope='matched-spl-el3-ocram-nobits-v2', base=BASE,
                limit=LIMIT, stack_end=STACK_END, cookie=COOKIE,
                cache_policy='owned-cold-all-setway-before-nobits-v1')
# Assembly has no C type environment. The selected C translation unit must
# independently check every emitted wire constant against the real producer.
ENTRY_CONSTANTS = {
    'BASE': (BASE, 'FES_NANO_FW_RAM_BASE'),
    'LIMIT': (LIMIT, 'FES_NANO_FW_RAM_LIMIT'),
    'COLD_MAGIC': (0x464e4332, 'FES_NANO_COLD_MAGIC'),
    'COLD_VERSION': (2, 'FES_NANO_COLD_VERSION'),
    'COLD_BYTES': (224, 'sizeof(struct fes_nano_cold_record)'),
    'COLD_FLAGS': (15, 'FES_NANO_COLD_FLAGS'),
    'COLD_COOKIE': (0x464e433200000002, 'FES_NANO_COLD_COOKIE'),
    'BOOT_MAGIC': (0x464e4231, 'FES_NANO_BOOT_MAGIC'),
    'BOOT_VERSION': (1, 'FES_NANO_BOOT_VERSION'),
    'BOOT_BYTES': (80, 'sizeof(struct fes_nano_boot_record)'),
    'HISTORY_NEVER_ENABLED': (1, 'FES_NANO_BOOT_NEVER_ENABLED'),
    'FABRIC_ABSENT': (1, 'FES_NANO_FABRIC_ABSENT'),
    'PRODUCERS_ALL': (0x3ff, 'FES_NANO_PRODUCERS_ALL'),
    'MASTER_RESET': (0xff21bfbf, 'FES_NANO_MASTER_RESET'),
    'COOKIE': (COOKIE, 'FES_NANO_FW_RAM_COOKIE'),
    'UART_DATA': (0x10c02100, 'FES_NANO_FW_RAM_UART_DATA'),
    'UART_STATUS': (0x10c02114, 'FES_NANO_FW_RAM_UART_STATUS'),
    'UART_POLLS': (100000, 'FES_NANO_FW_RAM_UART_POLLS'),
    'UART_BYTES': (384, 'FES_NANO_FW_RAM_UART_BYTES'),
}


def selected(profile, enabled):
    if type(enabled) is not bool:
        raise ValueError('explicit boolean firmware RAM selection required')
    if not enabled:
        return None
    mapping = profile.get('cpu_mapping')
    rom = profile.get('hps_rom')
    if (profile.get('usb_pio') is not True or profile.get('usb_shared') is not True or
            profile.get('sd_pio') is not True or not isinstance(rom, dict) or
            rom.get('bytes') not in (4096, 8192) or not isinstance(mapping, dict) or
            mapping.get('format') != 2 or mapping.get('scope') != 'pinned-tamago-el1-resources-excluded-v2' or
            mapping.get('firmware_base') != BASE or mapping.get('firmware_bytes') != LIMIT-BASE):
        raise ValueError('OCRAM firmware requires the matched shared-ROM and native exclusion profile')
    return dict(CONTRACT)


def checked(profile):
    value = profile.get('firmware_ram') if profile else None
    if value is None:
        return None
    expected = selected(profile, True)
    if json.dumps(value, sort_keys=True) != json.dumps(expected, sort_keys=True):
        raise ValueError('firmware RAM reservation differs from the fixed matched layout')
    return expected


def chain_bytes(chain):
    if not isinstance(chain, str) or len(chain) != 64 or any(c not in '0123456789abcdef' for c in chain) or int(chain, 16) == 0:
        raise ValueError('exact nonzero cold chain identity required')
    return bytes.fromhex(chain)


def assembly_header(chain):
    data = chain_bytes(chain)
    return ('/* Source-bound private EL3 entry constants, not an NS API. */\n'
            '#define FES_RAM_CHAIN_BYTES ' + ','.join(hex(v) for v in data) + '\n'
            '#define FES_RAM_OWNER_SEED ' + hex(int(chain[:16],16)|1) + '\n' +
            ''.join(('#ifndef FES_RAM_'+name+'\n' if name in ('UART_DATA','UART_STATUS') else '') +
                    '#define FES_RAM_'+name+' '+hex(value)+'\n' +
                    ('#endif\n' if name in ('UART_DATA','UART_STATUS') else '')
                    for name,(value,_) in ENTRY_CONSTANTS.items()) +
            '#ifndef FES_RAM_RSTMGR\n#define FES_RAM_RSTMGR 0x10d11000\n#endif\n'
            '#ifndef FES_RAM_PWRMGR\n#define FES_RAM_PWRMGR 0x10d14000\n#endif\n')


def entry_assertions():
    return ''.join('_Static_assert(FES_RAM_'+name+' == ('+expression+'), '
                   '"early EL3 '+name+' differs from C producer");\n'
                   for name,(_,expression) in ENTRY_CONSTANTS.items())


def support_assembly(chain):
    chain_bytes(chain)
    return '''
.pushsection .rodata.fes_ram_chain,"a"
.balign 8
.global fes_nano_ram_expected_chain
.type fes_nano_ram_expected_chain, %object
fes_nano_ram_expected_chain:
.byte FES_RAM_CHAIN_BYTES
.size fes_nano_ram_expected_chain, 32
.popsection
.pushsection .data.fes_ram_attempt,"aw"
.balign 8
.global fes_nano_ram_attempt
.type fes_nano_ram_attempt, %object
fes_nano_ram_attempt:
.quad 0
.size fes_nano_ram_attempt, 8
.popsection
.pushsection .rodata.fes_ram_trace,"a"
fes_nano_ram_trace_entry: .asciz "FES_NANO_RAM ENTRY\\r\\n"
fes_nano_ram_trace_pass: .asciz "FES_NANO_RAM PASS\\r\\n"
fes_nano_ram_trace_reject: .asciz "FES_NANO_RAM REJECT phase="
fes_nano_ram_trace_newline: .asciz "\\r\\n"
.popsection
''' + '.pushsection .rodata.fes_ram_vendor,"a"\n' + ''.join(
        'fes_nano_ram_vendor_'+phase.replace('-','_')+': .asciz "FES_NANO_RAM VENDOR phase='+phase+'\\r\\n"\n'
        for phase in VENDOR_PHASES) + '.popsection\n'


def vendor_macro(text, name):
    matches = re.findall(r'(?ms)^[ \t]*\.macro '+re.escape(name)+r'\b.*?^[ \t]*\.endm\n', text)
    if len(matches) != 1:
        raise ValueError('vendor startup macro context changed: '+name)
    return matches[0]


def vendor_call(call, phase):
    return ('\tfes_ram_uart_text fes_nano_ram_vendor_'+phase.replace('-','_')+'_before\n' + call +
            '\tfes_ram_uart_text fes_nano_ram_vendor_'+phase.replace('-','_')+'_after\n')


def vendor_source(text):
    """Instrument source-pinned cold macro copies; vendor originals stay intact.

    Trace expands only at call boundaries. x8..x14/NZCV are dead there in this
    pinned cold entry; arguments x0..x7 and retained x18..x30 are preserved.
    No diagnostic stack or candidate-memory access is introduced.
    """
    if hashlib.sha256(text.encode()).hexdigest() != COMMON_SHA:
        raise ValueError('unreviewed vendor startup macro source')
    arch = vendor_macro(text, 'el3_arch_init_common')
    entry = vendor_macro(text, 'el3_entrypoint_common')
    for original, phase in (('\tbl\tinit_cpu_data_ptr\n','cpu-data'),):
        if arch.count(original) != 1:raise ValueError('vendor CPU-data context changed')
        arch = arch.replace(original, vendor_call(original, phase))
    arch = arch.replace('.macro el3_arch_init_common', '.macro fes_ram_el3_arch_init_common', 1)
    for original, phase in (('\tbl\treset_handler\n','reset'),('\tbl\tplat_set_my_stack\n','stack')):
        if entry.count(original) != 1:raise ValueError('vendor call context changed: '+phase)
        entry = entry.replace(original, vendor_call(original, phase))
    call = '\t\tbl\tinv_dcache_range\n'
    parts = entry.split(call)
    if len(parts) != 4:raise ValueError('vendor cache context changed')
    nobits = ('\tfes_decode_setup\n' +
              vendor_call('\tfes_ram_nobits_cache\n\tfes_decode_restore\n', 'nobits-cache'))
    entry = (parts[0]+vendor_call(call,'rw-cache')+parts[1]+nobits+
             parts[2]+call+parts[3])
    call = '\t\tbl\tzeromem\n'
    parts = entry.split(call)
    if len(parts) != 3:raise ValueError('vendor zeroization context changed')
    entry = parts[0]+vendor_call(call,'bss')+parts[1]+call+parts[2]
    if entry.count('\tel3_arch_init_common\n') != 1:raise ValueError('vendor architecture context changed')
    entry = entry.replace('\tel3_arch_init_common\n','\tfes_ram_el3_arch_init_common\n')
    entry = entry.replace('.macro el3_entrypoint_common','.macro fes_ram_el3_entrypoint_common',1)
    return '/* Source-pinned cold-entry diagnostics; original vendor macros remain untouched. */\n'+arch+'\n'+entry


def entry_source(text, chain):
    if hashlib.sha256(text.encode()).hexdigest() != ENTRY_SHA:
        raise ValueError('unreviewed BL31 entry source')
    anchor = 'func bl31_entrypoint\n'
    if len(re.findall(r'(?m)^func bl31_entrypoint$',text)) != 1:
        raise ValueError('BL31 first-instruction context changed')
    before, end, after = text.partition('endfunc bl31_entrypoint\n')
    if not end or before.count('\tel3_entrypoint_common') != 2:
        raise ValueError('BL31 cold startup context changed')
    stash = '\tmov\tx23, x3\n'
    call = '\tbl\tbl31_setup\n'
    if before.count(stash) != 1 or before.count(call) != 1:
        raise ValueError('BL31 retained arguments/setup context changed')
    vectors = '\t\t_exception_vectors=runtime_exceptions'
    restore = '\tmov\tx0, x20\n'
    if before.count(vectors) != 2 or before.count(restore) != 1:
        raise ValueError('BL31 cold exception vector context changed')
    before = before.replace(vectors,'\t\t_exception_vectors=fes_nano_ram_fault_vectors')
    before = before.replace(restore,'\tadr\tx8, runtime_exceptions\n\tmsr\tvbar_el3, x8\n\tisb\n'+restore)
    before = before.replace('\tel3_entrypoint_common','\tfes_ram_el3_entrypoint_common')
    before = before.replace(stash,stash+'\tfes_decode_init\n\tfes_ram_uart_text fes_nano_ram_vendor_enter\n')
    before = before.replace(call,vendor_call(call,'setup'))
    text = before+end+after
    return ('#include "fes_nano_ram_entry.h"\n' +
            text.replace(anchor, anchor + '#include "nano_firmware_decode.inc"\n#include "nano_firmware_cache.inc"\n#include "nano_vendor_entry.inc"\n#include "nano_firmware_entry.inc"\n',1) +
            support_assembly(chain) + '#include "nano_firmware_fault.inc"\n')


def install(source, profile):
    if checked(profile) is None:
        return None
    chain = profile['chain_sha256']
    if hashlib.sha256((source / CACHE).read_bytes()).hexdigest() != CACHE_SHA:
        raise ValueError('unreviewed cold set/way cache helper source')
    include = source / INCLUDE
    (include / 'nano_firmware_status.inc').write_text(fault_status.assembly())
    (include / 'nano_vendor_entry.inc').write_text(vendor_source((source / COMMON).read_text()))
    (source / ENTRY).write_text(entry_source((source / ENTRY).read_text(), chain))
    (include / 'fes_nano_ram_entry.h').write_text(assembly_header(chain))
    for name in ('nano_firmware_entry.inc', 'nano_firmware_fault.inc', 'nano_firmware_decode.inc', 'nano_firmware_cache.inc', 'nano_firmware_ram.h'):
        (include / name).write_bytes((HERE / name).read_bytes())
    platform = source / PLATFORM
    text = platform.read_text()
    if ('#define BL31_NOBITS_BASE' in text or '#define BL31_NOBITS_LIMIT' in text or
            '#define OCRAM_BASE' not in text):
        raise ValueError('Agilex5 separate memory context changed')
    platform.write_text(text + '\n/* Selected matched SPL/EL3 reservation. */\n'
        '#define BL31_NOBITS_BASE 0x00050000\n#define BL31_NOBITS_LIMIT 0x00060000\n')
    c = source / 'plat/intel/soc/common/nano_cold_platform.c'
    c.write_text('#include "fes_nano_ram_entry.h"\n#include "nano_firmware_ram.h"\n' +
                 entry_assertions() + c.read_text())
    runtime_sources = nano_firmware_runtime.install(source)
    names = [ENTRY, COMMON, CACHE, PLATFORM, INCLUDE+'/fes_nano_ram_entry.h', INCLUDE+'/nano_vendor_entry.inc', INCLUDE+'/nano_firmware_entry.inc', INCLUDE+'/nano_firmware_fault.inc', INCLUDE+'/nano_firmware_decode.inc', INCLUDE+'/nano_firmware_cache.inc', INCLUDE+'/nano_firmware_status.inc',
             INCLUDE+'/nano_firmware_ram.h', 'plat/intel/soc/common/nano_cold_platform.c']
    return dict(contract=dict(CONTRACT), chain_sha256=chain,
                sources={name:hashlib.sha256((source/name).read_bytes()).hexdigest() for name in dict.fromkeys(names+runtime_sources)})


def verify_installed(source, binding):
    if binding is not None and any(hashlib.sha256((source/name).read_bytes()).hexdigest() != value
                                   for name,value in binding['sources'].items()):
        raise ValueError('selected firmware RAM entry sources changed during compilation')


def symbols(image):
    """Read the actual static ELF symbol table; no subprocess or trusted receipt."""
    offset = struct.unpack_from('<Q', image, 40)[0]
    size, count = struct.unpack_from('<HH', image, 58)
    if size != 64 or not 0 < count <= 4096 or offset+size*count > len(image):
        raise ValueError('selected BL31 requires its complete ELF section table')
    sections = [struct.unpack_from('<IIQQQQIIQQ', image, offset+i*size) for i in range(count)]
    found = {}
    for section in sections:
        if section[1] != 2:
            continue
        start, length, link, stride = section[4], section[5], section[6], section[9]
        if stride != 24 or length%24 or start+length > len(image) or link >= count:
            raise ValueError('invalid selected BL31 symbol table')
        strings = sections[link]
        if strings[1] != 3 or strings[4]+strings[5] > len(image):
            raise ValueError('invalid selected BL31 symbol names')
        names = image[strings[4]:strings[4]+strings[5]]
        for position in range(start, start+length, 24):
            name, info, other, index, value, length = struct.unpack_from('<IBBHQQ', image, position)
            if not index or name >= len(names) or info >> 4 not in (1, 2):
                continue
            end = names.find(b'\0', name)
            if end < 0:
                raise ValueError('unterminated BL31 symbol name')
            decoded = names[name:end].decode('ascii')
            if decoded in found and found[decoded] != (value, length):
                raise ValueError('ambiguous selected BL31 symbol')
            found[decoded] = (value, length)
    return found


def validate_elf(image, segments, chain):
    table = symbols(image)
    required = ('bl31_entrypoint','__NOBITS_START__','__NOBITS_END__','__STACKS_START__',
                '__STACKS_END__','__BSS_START__','__BSS_END__','fes_nano_ram_attempt','fes_nano_ram_expected_chain')
    if any(name not in table for name in required):
        raise ValueError('missing selected BL31 entry/reservation symbols')
    v = {name: table[name][0] for name in required}
    if (v['bl31_entrypoint'] != 0x80000000 or v['__NOBITS_START__'] != BASE or
            v['__STACKS_START__'] != BASE or v['__STACKS_END__'] != STACK_END or
            v['__BSS_START__'] != STACK_END or not STACK_END < v['__BSS_END__'] <= v['__NOBITS_END__'] <= LIMIT):
        raise ValueError('selected BL31 symbol layout exceeds the exact reservation')
    low = [s for s in segments if s[0] < 0x80000000]
    if low != [(BASE, v['__NOBITS_END__'])]:
        raise ValueError('selected BL31 NOBITS segment differs from its symbols')
    phoff = struct.unpack_from('<Q', image, 32)[0]
    count = struct.unpack_from('<H', image, 56)[0]
    for name, expected in [('fes_nano_ram_attempt',bytes(8)),('fes_nano_ram_expected_chain',chain_bytes(chain))]:
        address, length = table[name]
        if length != len(expected) or not 0x80000000 <= address < 0x80200000-length:
            raise ValueError('selected BL31 entry binding is outside file-backed DDR')
        matches = []
        for i in range(count):
            kind, flags, offset, va, pa, filesz, memsz, align = struct.unpack_from('<IIQQQQQQ',image,phoff+i*56)
            if kind == 1 and va <= address and address+length <= va+filesz:
                matches.append(image[offset+address-va:offset+address-va+length])
        if matches != [expected]:
            raise ValueError('selected BL31 initialized latch/chain bytes differ')
