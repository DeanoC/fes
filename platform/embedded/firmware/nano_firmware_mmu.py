"""Experimental, immutable EL3 tables; emission/inspection grants no boot access.

The linker resolves code/data boundaries and table pointers in the final image.
Tables occupy file-backed DDR inside the existing 2 MiB BL31 exclusion, rather
than consuming the small OCRAM stack/BSS reservation. Nothing here writes system
registers or changes the currently selected cold profile.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import struct

import nano_firmware_ram as ram
import nano_secure_mmio

PAGE = 0x1000
DDR_BASE, BL31_LIMIT, DDR_LIMIT = 0x80000000, 0x80200000, 0xc0000000
NAMES = ('root', 'low', 'ocram', 'gic', 'ddr', 'firmware')
PREFIX = 'fes_nano_mmu_'
TABLE_BYTES = len(NAMES) * PAGE
HERE = Path(__file__).resolve().parent
_status_spec=importlib.util.spec_from_file_location('fes_mmu_status',HERE.parent/'nano/mmu_status.py')
mmu_status=importlib.util.module_from_spec(_status_spec)
_status_spec.loader.exec_module(mmu_status)
_logging_spec=importlib.util.spec_from_file_location('fes_mmu_logging',HERE.parent/'nano/mmu_logging.py')
mmu_logging=importlib.util.module_from_spec(_logging_spec)
_logging_spec.loader.exec_module(mmu_logging)
_views_spec=importlib.util.spec_from_file_location('fes_mmu_mailbox_views',HERE.parent/'nano/mmu_mailbox_view.py')
mmu_mailbox_view=importlib.util.module_from_spec(_views_spec)
_views_spec.loader.exec_module(mmu_mailbox_view)
# Match the pinned TF-A v2 EL3 descriptor conventions, including AP[1] RES1.
AF, AP_RES1, RO, NS, XN = 1 << 10, 1 << 6, 1 << 7, 1 << 5, 1 << 54
WB = AF | AP_RES1 | (3 << 8)       # Attr0: WB/WA, inner shareable
NC = AF | AP_RES1 | (2 << 8) | 8   # Attr2: Normal NC, outer shareable
DEVICE = AF | AP_RES1 | (2 << 8) | 4
MAIR = 0x4400ff                    # Attr2=NC, Attr1=nGnRnE, Attr0=WB/WA
TCR = (1 << 31) | (1 << 23) | (3 << 12) | (1 << 10) | (1 << 8) | 32
CONTRACT = dict(format=6, scope='experimental-el3-static-secure-control-v6',
                granule=PAGE, va_bits=32, pa_bits=32, mair=MAIR, tcr=TCR,
                table_bytes=TABLE_BYTES, ddr_limit=DDR_LIMIT,
                firmware_limit=BL31_LIMIT, runtime_enabled=False,
                hardware_authority=False)
RUNTIME_CONTRACT = dict(format=11, scope='matched-cold-el3-secure-control-v11',
    mair=MAIR, tcr=TCR, table_bytes=TABLE_BYTES, granule=PAGE, va_bits=32, pa_bits=32,
    memory_probe='private-line-clean-invalidate-reread-v1',
    memory_types='secure-ocram-normal-nc-private-ddr-wb-v1',
    witness_storage='initialized-private-ddr-data-v1',
    enable_phase='after-startup-cache-maintenance-before-el3-exit',
    single_cpu='a55-cpu0-release-locked',
    boot_steps='admit-register-barrier-private-clean-v1',
    fault_capture='bounded-register-only-sctlr-transition-v2',
    status_inventory=mmu_status.SHA, mailbox_views=mmu_mailbox_view.SHA,
    control_address_selection=nano_secure_mmio.MODE,
    decode_logging=mmu_logging.SHA,
    vendor_cache_policy='restore-caller-scr-and-return-v1', hardware_authority=False)
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
    anchor = '*(SORT_BY_ALIGNMENT(.data*))'
    if common.count(anchor) != 1:
        raise ValueError('MMU witness initialized data context changed')
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
    (include/'nano_mmu_status.inc').write_text(mmu_status.assembly())
    files.append(str((include/'nano_mmu_status.inc').relative_to(source)))
    (include/'nano_mmu_mailbox.inc').write_text(mmu_mailbox_view.assembly())
    files.append(str((include/'nano_mmu_mailbox.inc').relative_to(source)))
    for name in ('nano_mmu.h','nano_mmu_platform.h','nano_mmu_memory.h','nano_mmu_transition.h','nano_mmu_logging.inc'):
        (include/name).write_bytes((HERE/name).read_bytes());files.append(str((include/name).relative_to(source)))
    for name in ('nano_mmu.c','nano_mmu_platform.c','nano_mmu_memory.c','nano_mmu_transition.S'):
        path=source/'plat/intel/soc/common'/name;path.write_bytes((HERE/name).read_bytes())
        files.append(str(path.relative_to(source)))
    make=source/'plat/intel/soc/agilex5/platform.mk'
    make.write_text(make.read_text()+'\nBL31_SOURCES += plat/intel/soc/common/nano_mmu.c plat/intel/soc/common/nano_mmu_platform.c plat/intel/soc/common/nano_mmu_memory.c plat/intel/soc/common/nano_mmu_transition.S\n')
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
    if (size!=64 or address&63 or not proof['layout']['data_start']<=address<=proof['layout']['data_end']-size or
            file_bytes(image,address,size)[0]!=bytes(64) or 'fes_nano_mmu_platform_boot' not in symbols):
        raise ValueError('MMU witness must be initialized private cached DDR data')
    helper,helper_size=symbols.get('invalidate_cache_low_el',(0,0))
    if helper_size!=len(VENDOR_CACHE_CODE) or helper&3 or not DDR_BASE<=helper<helper+helper_size<=proof['layout']['ro_end']:
        raise ValueError('selected lower-EL cache helper is missing/outside executable firmware')
    code,_=file_bytes(image,helper,helper_size)
    if code!=VENDOR_CACHE_CODE:
        raise ValueError('selected lower-EL cache helper does not restore SCR and return')
    transition=verify_transition(image,symbols,proof['layout'])
    if transition.get('hex_formatter') is None:raise ValueError('selected MMU profile lacks its fixed UART formatter')
    transition['mailbox_views']=mmu_mailbox_view.verify(image,symbols,proof['layout'],file_bytes)
    if any(address<transition[name]+8 and transition[name]<address+size for name in ('saved_vbar','logging_state')):
        raise ValueError('MMU witness overlaps private transition lifetime state')
    return dict(proof, selected_policy=dict(RUNTIME_CONTRACT), witness=address, witness_bytes=size,
                transition_diagnostic=transition,
                startup_cache_helper=dict(address=helper,bytes=helper_size,sha256=hashlib.sha256(code).hexdigest()))


def verify_transition(image, symbols, bounds):
    """Check final leaf code, vector routes, exact writes and finite UART bounds."""
    if len(image)<20 or struct.unpack_from('<H',image,18)[0]!=183:
        raise ValueError('MMU transition diagnostic requires an AArch64 ELF')
    prefix='fes_nano_mmu_transition_'
    logging_state,state_size=symbols.get('fes_nano_mmu_logging_state',(0,0))
    saved_vbar=symbols.get(prefix+'saved_vbar',(0,0))[0]
    if (state_size!=8 or logging_state&7 or not bounds['data_start']<=logging_state<logging_state+8<=bounds['data_end'] or
            abs(logging_state-saved_vbar)<8 or file_bytes(image,logging_state,8)[0]!=bytes(8)):
        raise ValueError('MMU logging requires distinct initialized private lifetime state')
    log_sites={};log_writes={}
    def constant(register,value,wide=True):
        return [(0xd2800000 if wide else 0x52800000)|((value&65535)<<5)|register,
                (0xf2a00000 if wide else 0x72a00000)|((value>>16)<<5)|register]
    for parent,names in (('write',('arm',)),('restore',('restore',)),('capture',('capture',))):
        base,length=symbols.get(prefix+parent,(0,0));allowed={}
        for name in names:
            label='fes_nano_mmu_log_store_'+name
            address,size=symbols.get(label,(0,0));value=8 if name=='arm' else 0
            expected=constant(0,0x1c000100)+[0x52800001|(value<<5),0xb9000001,0xd5033f9f]
            if (size!=16 or not base<=address<address+20<=base+length or
                    file_bytes(image,address,20)[0]!=struct.pack('<5I',*expected)):
                raise ValueError('MMU logging control address/value/barrier differs')
            allowed[address+12]=0xb9000001
            log_sites[label]=dict(address=address,bytes=size,value=value)
        states=(('arm_before',1),('armed',2)) if parent=='write' else ((parent+'_restoring',3),(parent+'_retired',4))
        for name,value in states:
            label='fes_nano_mmu_log_state_'+name;address,size=symbols.get(label,(0,0))
            delta=logging_state-address
            adr=0x10000005|((delta&3)<<29)|(((delta>>2)&0x7ffff)<<5)
            expected=[adr,0x52800001|(value<<5),0xb90000a1,0xd5033f9f]
            if (size!=12 or not -(1<<20)<=delta<(1<<20) or not base<=address<address+16<=base+length or
                    file_bytes(image,address,16)[0]!=struct.pack('<4I',*expected)):
                raise ValueError('MMU logging state target/value/barrier differs')
            allowed[address+8]=0xb90000a1
            log_sites[label]=dict(address=address,bytes=size,value=value)
        log_writes[parent]=allowed
    formatter=mmu_mailbox_view.verify_hex(image,symbols,bounds,file_bytes)
    result={}
    for name in ('write','restore','capture','vectors','park_vectors','park'):
        address,size=symbols.get(prefix+name,(0,0))
        if address&3 or size&3 or not 0<size<=16384 or not DDR_BASE<=address<address+size<=bounds['ro_end']:
            raise ValueError('MMU transition diagnostic code outside executable firmware')
        code,_=file_bytes(image,address,size)
        if formatter is not None and address<formatter['address']+96 and formatter['address']<address+size:
            raise ValueError('MMU UART formatter overlaps a transition function')
        result[name]=dict(address=address,bytes=size,sha256=hashlib.sha256(code).hexdigest())
        if name in ('vectors','park_vectors'):
            target=symbols.get(prefix+('capture' if name=='vectors' else 'park'),(0,0))[0]
            if address&2047 or size!=2048:
                raise ValueError('MMU transition vector alignment or size differs')
            for i in range(16):
                branch=0x14000000|(((target-address-i*128)//4)&0x3ffffff)
                if code[i*128:(i+1)*128]!=struct.pack('<I',branch)+bytes(124):
                    raise ValueError('MMU transition vector route or padding differs')
        elif name=='park':
            if code!=struct.pack('<II',0xd503205f,0x17ffffff):
                raise ValueError('MMU transition failure must park permanently')
        else:
            words=struct.unpack('<'+str(size//4)+'I',code)
            calls=[]
            for word_index,word in enumerate(words):
                if word&0xfc000000==0x94000000:
                    mmu_mailbox_view.verify_hex_call(words,word_index,address+word_index*4,formatter)
                    calls.append(address+word_index*4)
                memory=((word>>25)&15) in (4,6,12,14)
                stack_change=(word&0x1f000000)==0x11000000 and not(word&(1<<29)) and word&31==31
                if (memory and (word>>5)&31==31 or stack_change or
                        word&0xfffffc1f==0xd63f0000 or
                        word==0xd69f03e0 or name=='capture' and word&0xfffffc1f in (0xd65f0000,0xd61f0000)):
                    raise ValueError('MMU transition diagnostic must remain stack-free and leaf')
            if formatter is not None:formatter['calls'][name]=calls
            if name=='capture':
                park=symbols.get(prefix+'park',(0,0))[0]
                if words[-1]!=0x14000000|(((park-address-size+4)//4)&0x3ffffff):
                    raise ValueError('MMU transition fault capture must end in permanent park')
            # Exact production sink, finite per-character polling and per-line budget.
            for register,value,wide in ((3,0x412fd050,True),(3,0x81000000,True),
                                        (10,0x10c02114,True),(13,0x10c02100,True),(11,100000,False)):
                op=(0xd2800000 if wide else 0x52800000)|((value&65535)<<5)|register
                high=(0xf2a00000 if wide else 0x72a00000)|((value>>16)<<5)|register
                if struct.pack('<II',op,high) not in code:
                    raise ValueError('MMU transition UART sink or polling bound differs')
            init,init_size=symbols.get('fes_nano_mmu_uart_init_'+name,(0,0))
            expected=constant(10,0x10c02114)+constant(13,0x10c02100)
            if (init_size!=16 or not address<=init<init+16<=address+size or
                    file_bytes(image,init,16)[0]!=struct.pack('<4I',*expected)):
                raise ValueError('MMU transition must initialize its fixed UART registers')
            if struct.pack('<I',0x52800000|(384<<5)|17) not in code:
                raise ValueError('MMU transition UART line budget is missing')
            if name in ('write','capture'):
                # Every status MMIO read is an exact reviewed address followed
                # by a 32-bit read; no acknowledgement or memory-port alias.
                actual=[]
                for i,word in enumerate(words):
                    if word==0xb9400000:
                        if i<2 or words[i-2]&0xffe0001f!=0xd2800000 or words[i-1]&0xffe0001f!=0xf2a00000:
                            raise ValueError('MMU status read lacks its fixed address')
                        actual.append(((words[i-2]>>5)&65535)|(((words[i-1]>>5)&65535)<<16))
                if actual!=[address for _,address in mmu_status.MMIO]:
                    raise ValueError('MMU status MMIO read inventory differs')
            if name=='capture' and struct.pack('<I',0xd538411e) not in code:
                raise ValueError('MMU fault capture must snapshot interrupted SP_EL0')
            for index,word in enumerate(words):
                if ((word>>25)&15) in (4,6,12,14) and not(word&(1<<22)):
                    if (word!=0xb90001a9 and not(name=='write' and word==0xf9000041) and
                            log_writes[name].get(address+index*4)!=word):
                        raise ValueError('MMU diagnostic writes outside its fixed UART/one-use slot')
                if word&0xfff00000==0xd5100000:
                    allowed={'write':{0xd51ec001,0xd51e1000},'restore':{0xd51ec001},'capture':{0xd51ec008}}
                    if word not in allowed[name]:raise ValueError('MMU diagnostic has an unreviewed system-register write')
    for name,expected in (('sctlr',struct.pack('<II',0xd51e1000,0xd5033fdf)),
                          ('restore_vbar',struct.pack('<III',0xd51ec001,0xd5033fdf,0xd53ec003))):
        address,size=symbols.get(prefix+name,(0,0));parent=result['write' if name=='sctlr' else 'restore']
        if size!=len(expected) or not parent['address']<=address<address+size<=parent['address']+parent['bytes'] or file_bytes(image,address,size)[0]!=expected:
            raise ValueError('MMU transition register write/barrier/readback differs')
    runtime=symbols.get('runtime_exceptions',(0,0))[0]
    saved,size=symbols.get(prefix+'saved_vbar',(0,0))
    if (runtime&2047 or not DDR_BASE<=runtime<runtime+2048<=bounds['ro_end'] or
            saved&7 or size!=8 or not bounds['data_start']<=saved<saved+size<=bounds['data_end'] or
            file_bytes(image,saved,size)[0]!=bytes(8)):
        raise ValueError('MMU transition original vector or one-use saved state differs')
    return dict(result,hex_formatter=formatter,runtime_vectors=runtime,saved_vbar=saved,saved_bytes=size,status_inventory=mmu_status.SHA,
                decode_logging=mmu_logging.SHA,logging_state=logging_state,logging_state_bytes=state_size,logging_sites=log_sites,
                uart_data=0x10c02100,uart_status=0x10c02114,uart_polls=100000,uart_bytes=384)


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
    if name == 'ocram' and index == mmu_mailbox_view.ALIAS // PAGE:
        return mmu_mailbox_view.PHYSICAL, DEVICE | XN, 3
    if name == 'ocram' and index == nano_secure_mmio.SYSMGR_ALIAS // PAGE:
        return nano_secure_mmio.SYSMGR_PHYSICAL, DEVICE | XN, 3
    if name == 'ocram' and index == nano_secure_mmio.TCU_ALIAS // PAGE:
        return nano_secure_mmio.TCU_PHYSICAL, DEVICE | RO | XN, 3
    if name == 'ocram' and index < 0x80000 // PAGE:
        address = index * PAGE
        return address, NC | XN, 3
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
