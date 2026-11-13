import base64,hashlib,json,subprocess
from pathlib import Path
w=Path(__file__).resolve().parent;root=w.parents[1]
artifacts=w
head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip()
assert head==json.loads((w/'source.json').read_text())['head'] and not subprocess.check_output(['git','status','--porcelain'],cwd=root)
u=json.loads((artifacts/'native-uboot/result.json').read_text());cold=u['native_handoff']['cold_boot_profile']
sdkroot=w/'native-sdk'
sdk=json.loads((sdkroot/'result.json').read_text());composition=json.loads((artifacts/'native-composition/result.json').read_text())
assert cold['secure_capture_trace'] is True
assert cold['hps_ddr_read'] is None and sdk['hps_ddr_read'] is None and not composition['bounded_ddr_read']
assert cold['hps_computer']==sdk['hps_computer']
assert cold['hps_computer_sha256']==sdk['hps_computer_sha256']
assert cold['hps_computer']['hps_master'] is False
header=artifacts/'native-bl31/source/plat/intel/soc/common/include/fes_nano_cold_profile.h'
assert '#define FES_NANO_DDR_READ_DIAGNOSTIC 0' in header.read_text()
assert '#define FES_NANO_COLD_COMPUTER 1' in header.read_text()
assert (artifacts/'native-bl31/source/plat/intel/soc/common/include/nano_computer_control.inc').read_bytes()==(root/'platform/embedded/firmware/nano_computer_control.inc').read_bytes()
elf=artifacts/'native-bl31/source/build/agilex5/release/bl31/bl31.elf'
assert elf.is_file()
nm=root/'out/bootchain-tools/uboot-bin/aarch64-linux-gnu-nm'
raw=subprocess.check_output([str(nm),'--defined-only',str(elf)],text=True)
(w/'bl31-selected-symbols.log').write_text(raw)
symbols={line.split()[2]:line.split()[1] for line in raw.splitlines() if len(line.split())==3}
for name in ('fes_nano_fabric_stop','fes_nano_cold_control_target','fes_nano_platform_identity_setup'):assert symbols[name]=='T',name
assert symbols['fes_nano_computer_phase']=='b'
paths=[header,elf,sdkroot/'fpgaload.elf',artifacts/'native-composition/native-hps-first.sof',artifacts/'native-composition/u-boot.itb']
# Check the actual pre-relocation executable bytes, not just source text.
import re,struct,sys
sys.path.insert(0,str(root/'platform/embedded/firmware'))
import prepare_nano_owned
source=artifacts/'native-bl31/source'
reference=root/'out/terasic-git-restore-tfa'
pin='580dcb5c4e10aa7daff2bf69d493c076a6d3e665'
def original(name):
    return subprocess.check_output(['git','-C',str(reference),'show',pin+':'+name])
def sha(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
assembly={}
for dep in sorted((source/'build/agilex5/release/bl31').glob('*.d')):
    text=dep.read_text().replace('\\\n',' ')
    for name in set(re.findall(r'(?:^|\s)([^\s:]+\.S)(?:\s|$)',text)):
        p=source/name
        assert p.read_bytes()==original(name),name
        assembly[name]=sha(p)
assert 'bl31/aarch64/bl31_entrypoint.S' in assembly
old=root/'out/nano-ddr-cycle-20261102/native-bl31/source/build/agilex5/release/bl31/bl31_entrypoint.o'
new=source/'build/agilex5/release/bl31/bl31_entrypoint.o'
def elf_sections(path):
    data=path.read_bytes()
    assert data[:6]==b'\x7fELF\x02\x01'
    h=struct.unpack_from('<16sHHIQQQIHHHHHH',data)
    sections=[struct.unpack_from('<IIQQQQIIQQ',data,h[6]+i*h[11]) for i in range(h[12])]
    strings=sections[h[13]]
    names=data[strings[4]:strings[4]+strings[5]]
    def name(offset):return names[offset:names.index(0,offset)].decode()
    return {name(row[0]):data[row[4]:row[4]+row[5]] for row in sections if row[2]&4}
a,b=elf_sections(old),elf_sections(new)
assert a and a==b,'vendor entry executable object bytes changed'
# Remove only the exact additions and recover the complete pinned vendor setup.
setup=source/prepare_nano_owned.SETUP
vendor=original(prepare_nano_owned.SETUP).decode()
owned='#include "nano_bridge_platform.h"\n'+vendor.replace('\tsocfpga_delay_timer_init();\n','\tsocfpga_delay_timer_init();\n\tfes_nano_bridge_platform_setup();\n')
assert setup.read_text()==prepare_nano_owned.cold_setup_source(owned)
assert original(prepare_nano_owned.SETUP).count(b'\tsocfpga_delay_timer_init();\n')==1
paths += [setup,new,old]+[source/name for name in assembly]
bl31=json.loads((artifacts/'native-bl31/result.json').read_text())
assert all(lo>=0x80000000 and hi<=0x82000000 for lo,hi in bl31['bl31_load_segments'])
binary=source/'build/agilex5/release/bl31.bin'
for artifact in (binary,elf):
    for phase in ('before','after'):
        assert ('FES_NANO_EL3_CAPTURE phase='+phase+' pointer=%llx cookie=%llx').encode() in artifact.read_bytes()
paths.append(binary)
report={'status':'passed','source_commit':head,'classification':'selected AArch64 diagnostic firmware and native image; no hardware acceptance',
 'secure_capture_trace':True,'secure_capture_markers_in_actual_binary':True,'vendor_setup_reconstructed_exactly':True,'vendor_assembly_unchanged':assembly,
 'vendor_entry_executable_sections_unchanged':{name:hashlib.sha256(data).hexdigest() for name,data in a.items()},
 'vendor_entry_object_old_sha256':sha(old),'vendor_entry_object_new_sha256':sha(new),
 'bl31_bin_size':bl31['bl31_bin_size'],'bl31_load_segments':bl31['bl31_load_segments'],
 'layout_scope':'linked firmware and addresses changed; executable entry object compared before relocation; no startup cause inferred',
 'hps_computer_sha256':cold['hps_computer_sha256'],'computer_control_selected':True,'bounded_ddr_read_selected':False,'package_id':cold['hps_context']['package_id'],'build_id':cold['hps_context']['build_id'],
 'strong_symbols':{n:symbols[n] for n in ('fes_nano_fabric_stop','fes_nano_cold_control_target','fes_nano_platform_identity_setup')},
 'inputs':{str(p):sha(p) for p in paths},'runtime_admission':False}

print('PASS matched computer-control selection, strong firmware symbols, pinned assembly and unchanged entry executable object sections')

# Check the compiled SPL guard and the actual branch-side SHA API calls.
atf=u['native_handoff']['atf_handoff']
assert cold['spl_atf_trace'] is True
assert atf['bytes']==bl31['bl31_bin_size'] and atf['sha256']==bl31['bl31_bin_sha256']
spl_elf=artifacts/'native-uboot/build/spl/u-boot-spl'
spl_header=artifacts/'native-uboot/source/arch/arm/mach-socfpga/fes_nano_atf_seal.h'
assert ('#define FES_NANO_ATF_SIZE '+str(atf['bytes'])+'UL') in spl_header.read_text()
expected='{'+','.join('0x'+atf['sha256'][i:i+2] for i in range(0,64,2))+'}'
assert '#define FES_NANO_ATF_SHA '+expected in spl_header.read_text()
raw_spl=subprocess.check_output([str(nm),'--defined-only',str(spl_elf)],text=True)
assert any(line.split()[1:]==['T','fes_nano_spl_atf_trace'] for line in raw_spl.splitlines() if len(line.split())==3)
objdump=root/'out/bootchain-tools/uboot-bin/aarch64-linux-gnu-objdump'
disassembly=subprocess.check_output([str(objdump),'--disassemble=fes_nano_spl_atf_trace',str(spl_elf)],text=True)
(w/'spl-atf-disassembly.log').write_text(disassembly)
callees=re.findall(r'\bbl\s+[0-9a-f]+ <([^>+]+)',disassembly)
assert {'sha256_starts','sha256_update','sha256_finish'}<=set(callees),callees
assert not any('watchdog' in name or name in ('sha256_csum_wd','schedule') for name in callees)
# Compare the complete linked early startup intervals against the stopped
# early-capture candidate, separately from changing cold identity constants.
previous=root/'out/nano-el3-capture-20261102/native-bl31/source/build/agilex5/release/bl31/bl31.elf'
def named_symbols(path):
    raw=subprocess.check_output([str(nm),'-S','--defined-only',str(path)],text=True)
    return {row[3]:(int(row[0],16),int(row[1],16)) for line in raw.splitlines() if len(row:=line.split())==4}
def linked_bytes(path,address,size):
    data=path.read_bytes();h=struct.unpack_from('<16sHHIQQQIHHHHHH',data)
    for i in range(h[10]):
        ph=struct.unpack_from('<IIQQQQQQ',data,h[5]+i*h[9])
        if ph[0]==1 and ph[3]<=address and address+size<=ph[3]+ph[5]:
            offset=ph[2]+address-ph[3];return data[offset:offset+size]
    raise AssertionError('executable range is not in a file-backed load segment')
old_symbols,new_symbols=named_symbols(previous),named_symbols(elf)
comparisons={}
for name in ('bl31_entrypoint','bl31_setup','bl31_early_platform_setup2','console_16550_register','console_set_scope','fes_nano_cold_capture'):
    old_address,old_size=old_symbols[name];address,size=new_symbols[name]
    before=linked_bytes(previous,old_address,old_size);after=linked_bytes(elf,address,size)
    comparisons[name]={'old_address':old_address,'address':address,'old_size':old_size,'size':size,
        'instruction_bytes_unchanged':before==after,'address_unchanged':address==old_address,
        'sha256':hashlib.sha256(after).hexdigest()}
report.update(native_build_source_commit=head,spl_atf_trace=True,atf_handoff=atf,spl_atf_direct_callees=callees,
              startup_comparison=comparisons,startup_comparison_scope='linked entry through capture; cold identity constants may change elsewhere')
for path in (spl_elf,spl_header,previous):report['inputs'][str(path)]=sha(path)

print('PASS compiled fixed BL31 seal, strong SPL guard and direct raw SHA calls')
print(json.dumps(comparisons,indent=2))

assert atf['format']==2 and atf['scope']=='loaded-bl31-handoff-v2'
from relocations import check as check_relocations
names=tuple(comparisons)
report['startup_relocation_proof']=check_relocations(previous.parent,elf.parent,previous,elf,names)
report['startup_comparison_scope']='old exact linked-byte comparison retained; exact object code and relocation targets verified for changed layout'
report['startup_relocation_checker_sha256']=sha(w/'relocations.py')


def linked_string(address):
    if address is None:return None
    data=spl_elf.read_bytes();h=struct.unpack_from('<16sHHIQQQIHHHHHH',data)
    for i in range(h[10]):
        ph=struct.unpack_from('<IIQQQQQQ',data,h[5]+i*h[9])
        if ph[0]==1 and ph[3]<=address<ph[3]+ph[5]:
            offset=ph[2]+address-ph[3];end=data.find(b'\0',offset,min(len(data),offset+512))
            if end<0:return None
            try:return data[offset:end].decode('ascii')
            except UnicodeDecodeError:return None
    return None

def instructions(name):
    raw=subprocess.check_output([str(objdump),'--disassemble='+name,str(spl_elf)],text=True)
    path=w/('linked-'+name+'.txt');path.write_text(raw);report['inputs'][str(path)]=sha(path)
    rows=[];registers={}
    for line in raw.splitlines():
        match=re.fullmatch(r'\s*([0-9a-f]+):\s+([0-9a-f]{8})\s+(.+)',line)
        if not match:continue
        text=match[3];assembly=text.split('//',1)[0].strip();parts=assembly.split(None,1)
        mnemonic=parts[0];operands=parts[1] if len(parts)>1 else ''
        row={'address':int(match[1],16),'word':match[2],'instruction':text}
        if mnemonic in ('adr','adrp'):
            m=re.match(r'(x\d+),\s*([0-9a-f]+)',operands)
            if m:registers[m[1]]=int(m[2],16)
        elif mnemonic=='add':
            m=re.fullmatch(r'(x\d+),\s*(x\d+),\s*#(0x[0-9a-f]+|[0-9]+)',operands)
            if m and m[2] in registers:registers[m[1]]=registers[m[2]]+int(m[3],0)
            elif re.match(r'x\d+,',operands):registers.pop(operands.split(',')[0],None)
        elif mnemonic=='mov':
            m=re.fullmatch(r'(x\d+),\s*(x\d+)',operands)
            if m and m[2] in registers:registers[m[1]]=registers[m[2]]
            elif re.match(r'x\d+,',operands):registers.pop(operands.split(',')[0],None)
        elif mnemonic in ('ldr','ldp','mrs'):
            for reg in re.findall(r'\b[wx]\d+\b',operands.split('[',1)[0]):registers.pop('x'+reg[1:],None)
        elif mnemonic=='bl':
            m=re.search(r'<([^>+]+)',operands)
            row.update(callee=m[1] if m else None,string_argument=linked_string(registers.get('x0')))
            for reg in range(19):registers.pop('x'+str(reg),None)
        rows.append(row)
    assert rows;return rows

def call(rows,text):
    found=[r for r in rows if r.get('string_argument')==text]
    assert len(found)==1,(text,found)
    assert found[0]['callee'].startswith(('fes_nano_spl_cold_puts','fes_nano_spl_cold_printf','fes_nano_spl_cold_write'))
    return found[0]

handoff=instructions('fes_nano_spl_handoff');guard=instructions('fes_nano_spl_atf_trace');caller=instructions('spl_invoke_atf')
before=call(handoff,'FES_NANO_SPL BARRIER phase=before\n')
after=call(handoff,'FES_NANO_SPL BARRIER phase=after\n')
barriers=[r for r in handoff if r['word']=='d5033f9f'];assert len(barriers)==1
barrier=barriers[0];assert before['address']<barrier['address']<after['address']<handoff[-1]['address']
entry=call(guard,'FES_NANO_SPL ATF_STEP phase=entry\n')
hash_before=call(guard,'FES_NANO_SPL ATF_STEP phase=hash-before\n')
hash_after=call(guard,'FES_NANO_SPL ATF_STEP phase=hash-after\n')
current_el=next(r for r in guard if re.search(r'\bmrs\s+[^,]+,\s*currentel',r['instruction']))
assert entry['address']<current_el['address']<hash_before['address']
hash_calls=[r for r in guard if r.get('callee') in ('sha256_starts','sha256_update','sha256_finish')]
assert [r['callee'] for r in hash_calls]==['sha256_starts','sha256_update','sha256_finish']
assert hash_before['address']<hash_calls[0]['address']<hash_calls[1]['address']<hash_calls[2]['address']<hash_after['address']
final=[r for r in guard if str(r.get('string_argument','')).startswith('FES_NANO_SPL ATF target=')]
assert len(final)==1 and final[0]['address']>hash_after['address']
branches=[r for r in caller if r['instruction'].startswith('blr\t')];assert len(branches)==1
handoff_call=next(r for r in caller if r.get('callee')=='fes_nano_spl_handoff')
guard_call=next(r for r in caller if r.get('callee')=='fes_nano_spl_atf_trace')
assert handoff_call['address']<guard_call['address']<branches[0]['address']
report['spl_checkpoint_order']={'barrier_before':before,'dsb_sy':barrier,'barrier_after':after,'guard_entry':entry,
    'current_el':current_el,'hash_before':hash_before,'hash_calls':hash_calls,'hash_after':hash_after,'final_atf':final[0],
    'caller_handoff':handoff_call,'caller_guard':guard_call,'caller_bl31_branch':branches[0]}
report['spl_checkpoint_scope']='actual linked call/literal order; runtime progress remains unqualified'

print('PASS actual SPL checkpoint literals and linked barrier/guard/raw-hash/caller order; vendor startup instructions unchanged')

# Inspect every linked callee reachable from the contained output helpers.
# No serial-uclass, scheduler, timer, watchdog or indirect callback is permitted.
all_text=subprocess.check_output([str(objdump),'-d',str(spl_elf)],text=True)
(w/'spl-all-disassembly.log').write_text(all_text)
functions={};current=None
for line in all_text.splitlines():
    match=re.fullmatch(r'([0-9a-f]+) <([^>]+)>:',line)
    if match:current=match[2];functions.setdefault(current,[]);continue
    if current:
        match=re.fullmatch(r'\s*([0-9a-f]+):\s+([0-9a-f]{8})\s+(.+)',line)
        if match:functions[current].append({'address':int(match[1],16),'instruction':match[3]})
roots=sorted(name for name in functions if name.startswith('fes_nano_spl_cold_'))
assert roots and any(name.startswith('fes_nano_spl_cold_printf') for name in roots)
pending=list(roots);reachable={};indirect_branches=[]
forbidden={'printf','vprintf','puts','putc','serial_puts','hang','schedule','udelay','__udelay','mdelay'}
while pending:
    name=pending.pop()
    if name in reachable:continue
    assert name in functions,name
    assert name not in forbidden and not any(token in name.lower() for token in ('watchdog','cyclic','ns16550','serial_','timer_get','wdt_')),name
    calls=set()
    for row in functions[name]:
        instruction=row['instruction']
        assert not re.match(r'blr\b',instruction),('indirect callback',name,row)
        if re.match(r'br\b',instruction):indirect_branches.append({'function':name,**row})
        match=re.match(r'(?:bl|b)\s+[0-9a-f]+ <([^>+]+)',instruction)
        if match and match[1]!=name:calls.add(match[1])
    reachable[name]=sorted(calls);pending.extend(calls)
    assert len(reachable)<=256
local_switches=[]
for branch in indirect_branches:
    assert branch['function']=='vsnprintf_internal' and branch['instruction']=='br\tx1',branch
    rows=functions[branch['function']];index=next(i for i,r in enumerate(rows) if r['address']==branch['address'])
    context=rows[index-7:index+1]
    patterns=(r'sub\s+w1, w1, #0x63',r'cmp\s+w1, #0x15',
        r'b.hi\s+[0-9a-f]+ <vsnprintf_internal\+0x[0-9a-f]+>.*',
        r'ldr\s+x2, \[sp, #120\]',r'ldrh\s+w1, \[x2, w1, uxtw #1\]',
        r'adr\s+x2, ([0-9a-f]+) <vsnprintf_internal\+0x[0-9a-f]+>',
        r'add\s+x1, x2, w1, sxth #2',r'br\s+x1')
    assert len(context)==len(patterns)
    for row,pattern in zip(context,patterns):assert re.fullmatch(pattern,row['instruction']),row
    target_base=int(re.fullmatch(patterns[5],context[5]['instruction'])[1],16)
    stores=[i for i,row in enumerate(rows) if re.fullmatch(r'str\s+x0, \[sp, #120\]',row['instruction'])]
    assert len(stores)==1
    store=stores[0]
    page=re.fullmatch(r'adrp\s+x25, ([0-9a-f]+) <[^>]+>',rows[store-2]['instruction'])
    offset=re.fullmatch(r'add\s+x0, x25, #(0x[0-9a-f]+)',rows[store-1]['instruction'])
    assert page and offset
    table=int(page[1],16)+int(offset[1],16)
    values=linked_bytes(spl_elf,table,22*2)
    destinations=[target_base+4*delta for delta in struct.unpack('<22h',values)]
    start,size=named_symbols(spl_elf)['vsnprintf_internal']
    assert all(start<=address<start+size and address%4==0 for address in destinations)
    assert all(address in {row['address'] for row in rows} for address in destinations)
    local_switches.append({'branch_address':branch['address'],'table_address':table,'entries':22,
        'table_sha256':hashlib.sha256(values).hexdigest(),'destinations':destinations,
        'function_start':start,'function_size':size,'all_destinations_inside_formatter':True,
        'index_bound':21,'context':context})
inc=artifacts/'native-uboot/source/arch/arm/mach-socfpga/fes_nano_spl_console.inc'
assert inc.read_bytes()==(root/'platform/embedded/nano/spl_console.inc').read_bytes()
dts=artifacts/'native-uboot/source/arch/arm/dts/socfpga_agilex5.dtsi'
assert dts.read_bytes()==subprocess.check_output(['git','-C',str(root/'out/terasic-git-restore-uboot'),'show',
    'd0f8813fd6bf8191d49c8b8d2035ffd2ae10c413:arch/arm/dts/socfpga_agilex5.dtsi'])
body=re.search(r'\buart1: serial1@10c02100 \{(.*?)\n\s*\};',dts.read_text(),re.S).group(1)
assert 'reg = <0x10c02100 0x100>;' in body and 'reg-shift = <2>;' in body and 'reg-io-width = <4>;' in body
report['contained_console']={'scope':'bounded-uart1-v1','roots':roots,'reachable_callees':reachable,
    'local_format_switches':local_switches,'indirect_callback_calls_absent':True,'scheduler_and_device_callback_paths_absent':True,
    'uart_base':0x10c02100,'uart_lsr':0x10c02114,'register_shift':2,'register_io_bytes':4,
    'poll_cap_per_wait':100000,'message_buffer_bytes':384,'source_sha256':sha(inc),
    'vendor_uart_register_map_sha256':sha(dts)}
for path in (inc,dts,w/'spl-all-disassembly.log'):report['inputs'][str(path)]=sha(path)
(w/'selection-proof.json').write_text(json.dumps(report,indent=2)+'\n')
print('PASS complete linked contained-console call graph: no scheduler/device services or indirect callback calls; all22 switch destinations remain in formatter; pinned UART map verified')
