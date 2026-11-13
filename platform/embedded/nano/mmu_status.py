"""Fixed read-only memory-error inventory for the selected MMU diagnostic.

Reads use the same reviewed register addresses as the early cold fault probe.
The optional RAS section observes the current selector only; it never writes it.
Completion is diagnostic evidence and grants no boot or device authority.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import re

_spec=importlib.util.spec_from_file_location('fes_mmu_status_map',Path(__file__).with_name('fault_status.py'))
_map=importlib.util.module_from_spec(_spec);_spec.loader.exec_module(_map)
_NAMES=('dsu_detect','dsu_interrupt','dsu_status_before','dsu_location0','dsu_location1','dsu_status_after',
        'ocram_detect','ocram_interrupt','ocram_status_before','ocram_location0','ocram_location1','ocram_status_after',
        'ecc_config','ecc_initialized','ecc_pending','ecc_decoder')
_addresses=dict(_map.MMIO)
MMIO=tuple((name,_addresses[name]) for name in _NAMES)
RAS=(('pfr0','id_aa64pfr0_el1'),('midr','midr_el1'),('disr','disr_el1'),('count','erridr_el1'),
     ('selector_before','errselr_el1'),('features','erxfr_el1'),('control','erxctlr_el1'),
     ('status_before','erxstatus_el1'),('misc','erxmisc0_el1'),('status_after','erxstatus_el1'),
     ('selector_after','errselr_el1'))
PROBES=tuple((name,'mmio',address) for name,address in MMIO)+tuple((name,'cpu',reg) for name,reg in RAS)
CONTRACT=dict(format=1,scope='bounded-mmu-memory-status-readonly-v1',probes=[list(x) for x in PROBES],
              ras='reviewed-a55-current-selector-readonly-v1',uart_polls=100000,uart_bytes=384,
              phases=['before','fault'],hardware_authority=False)
SHA=hashlib.sha256(json.dumps(CONTRACT,sort_keys=True,separators=(',',':')).encode()).hexdigest()


def assembly(test_addresses=None):
    """Canonical source; fixtures may substitute aligned read targets only."""
    if test_addresses is not None and (not isinstance(test_addresses,dict) or any(
            type(i) is not int or not 0<=i<len(PROBES) or type(address) is not int or
            address&7 or not 0<=address<1<<32 for i,address in test_addresses.items())):
        raise ValueError('invalid MMU status fixture read map')
    rows=['/* Fixed read-only inventory: no status acknowledgements or selector writes. */',
          '.macro mmu_inventory phase']
    for i,(_,kind,target) in enumerate(PROBES):
        rows+=['mmu_cpu fes_nano_mmu_transition_park','mov w17,#384',
               'mmu_text .Lstatus_\\phase\\()_'+str(i)]
        if test_addresses is not None and i in test_addresses:
            rows+=['mmu_constant x0,'+hex(test_addresses[i]),'ldr '+('w0' if kind=='mmio' else 'x0')+',[x0]']
        elif kind=='mmio':rows+=['mmu_constant x0,'+hex(target),'ldr w0,[x0]']
        else:rows+=['mrs x0,'+target]
        rows+=['mmu_hex x0','mmu_text .Lnewline']
        r=i-len(MMIO)
        if r==0:rows+=['ubfx x2,x0,#28,#4','cmp x2,#1','b.ne .Lstatus_feature_\\@']
        elif r==1:rows+=['mmu_constant x2,0x412fd050','cmp x0,x2','b.ne .Lstatus_cpu_\\@']
        elif r==3:rows+=['cmp x0,#2','b.ne .Lstatus_count_\\@']
        elif r==4:rows+=['cmp x0,#2','b.hs .Lstatus_selector_\\@','mov x6,x0']
        elif r==10:rows+=['cmp x0,x6','b.ne .Lstatus_changed_\\@']
    rows+=['mov w17,#384','mmu_text .Lstatus_\\phase\\()_complete','b .Lstatus_done_\\@']
    for reason in ('feature','cpu','count','selector','changed'):
        rows+=['.Lstatus_'+reason+'_\\@:','mov w17,#384',
               'mmu_text .Lstatus_\\phase\\()_skip_'+reason,'b .Lstatus_done_\\@']
    rows+=['.Lstatus_done_\\@:','.endm','.pushsection .rodata.fes_mmu_status,"a"']
    for phase in ('before','fault'):
        for i in range(len(PROBES)):
            rows+=['.Lstatus_'+phase+'_'+str(i)+': .asciz "FES_NANO_EL3_MMU_STATUS phase='+phase+' index='+format(i,'02d')+' value="']
        rows+=['.Lstatus_'+phase+'_complete: .asciz "FES_NANO_EL3_MMU_STATUS phase='+phase+' END ras=complete\\r\\n"']
        for reason in ('feature','cpu','count','selector','changed'):
            rows+=['.Lstatus_'+phase+'_skip_'+reason+': .asciz "FES_NANO_EL3_MMU_STATUS phase='+phase+' END ras=skip reason='+reason+'\\r\\n"']
    return '\n'.join(rows+['.popsection'])+'\n'


ROW=re.compile(r'FES_NANO_EL3_MMU_STATUS phase=(before|fault) index=([0-9]{2}) value=([0-9a-f]{16})?')
END=re.compile(r'FES_NANO_EL3_MMU_STATUS phase=(before|fault) END ras=(complete|skip)(?: reason=(feature|cpu|count|selector|changed))?')


def state():return dict(records=[],pending=None,complete=False,valid=True,ras=None,reason=None,hardware_authority=False)


def observe(s,line,number,phase):
    def bad():
        s['valid']=s['complete']=False
        return 'invalid/out-of-order MMU memory-status diagnostic'
    if not s['valid'] or s['complete']:return bad()
    match=ROW.fullmatch(line)
    if match:
        index=int(match[2]);value=match[3]
        if match[1]!=phase or index!=len(s['records']) or index>=len(PROBES):return bad()
        if value is None:
            if s['pending'] is not None:return bad()
            s['pending']=dict(index=index,line=number);return None
        if s['pending'] is not None:return bad()  # A split physical record cannot become a second complete row.
        s['records'].append(dict(index=index,name=PROBES[index][0],value=int(value,16),line=number));return None
    match=END.fullmatch(line)
    if not match or match[1]!=phase or s['pending'] is not None:return bad()
    kind,reason=match.group(2,3);n=len(s['records']);values={r['index']:r['value'] for r in s['records']};base=len(MMIO)
    if kind=='complete':
        if reason is not None or n!=len(PROBES):return bad()
        valid=values[base]>>28&15==1 and values[base+1]==0x412fd050 and values[base+3]==2 and values[base+4]<2 and values[base+10]==values[base+4]
    else:
        guards=dict(feature=(base+1,lambda:values[base]>>28&15!=1),cpu=(base+2,lambda:values[base+1]!=0x412fd050),
                    count=(base+4,lambda:values[base+3]!=2),selector=(base+5,lambda:values[base+4]>=2),
                    changed=(base+11,lambda:values[base+10]!=values[base+4]))
        if reason not in guards or n!=guards[reason][0]:return bad()
        valid=guards[reason][1]()
        # Every earlier identity/record-layout gate must have passed.
        if n>base+1:valid=valid and values[base]>>28&15==1
        if n>base+2:valid=valid and values[base+1]==0x412fd050
        if n>base+4:valid=valid and values[base+3]==2
        if n>base+5:valid=valid and values[base+4]<2
    if not valid:return bad()
    s.update(complete=True,ras=kind,reason=reason);return None
