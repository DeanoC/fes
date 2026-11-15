"""Observe the fixed DDR read/drain recipe; never grant runtime admission."""
import re
import struct
from hps_context import reader_pattern,digest

H=r'[0-9a-f]{1,16}'
D=r'[0-9]{1,20}'
PREFIX=r'(?:NOTICE:[ \t]+)?'
CONTROL=re.compile(PREFIX+rf'FES_NANO_DDR_CONTROL phase=(?P<phase>1[4-7]) owner=(?P<owner>{H}) generation=(?P<generation>[0-9]{{1,20}}) reset=(?P<reset>{H})')
PATTERNS={
    'control':CONTROL,
    'gate':re.compile(PREFIX+rf'FES_NANO_DDR_GATE status=(?P<status>-?{D}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) pattern_words=(?P<pattern_words>{D}) permit=bounded-read'),
    'phase':re.compile(PREFIX+rf'FES_NANO_DDR_READ phase=(?P<phase>[1-8]) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) reads=(?P<reads>{D}) writes=(?P<writes>{D}) polls=(?P<polls>{D})'),
    'access':re.compile(PREFIX+rf'FES_NANO_DDR_ACCESS phase=(?P<phase>before|after) ordinal=(?P<ordinal>{D}) offset=(?P<offset>{H}) value=(?P<value>{H})'),
    'write':re.compile(PREFIX+rf'FES_NANO_DDR_WRITE phase=(?P<phase>before|after) offset=(?P<offset>{H}) value=(?P<value>{H})'),
    'result':re.compile(PREFIX+rf'FES_NANO_DDR_RESULT status=(?P<status>-?{D}) bytes=(?P<bytes>{D}) checksum=(?P<checksum>{H}) '+
        ' '.join(r'data'+str(i)+rf'=(?P<data{i}>{H})' for i in range(4))+rf' generation=(?P<generation>{D}) cookie=(?P<cookie>{D})'),
    'master':re.compile(PREFIX+rf'FES_NANO_MASTER_STOP status=(?P<status>-?{D}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) reader_reads=(?P<reader_reads>{D}) reader_polls=(?P<reader_polls>{D}) disable=(?P<disable>[01]) probe_polls=(?P<probe_polls>{D}) cancel=(?P<cancel>[01]) complete=(?P<complete>[01]) data=active'),
    'closure':re.compile(PREFIX+rf'FES_NANO_DDR_CLOSURE owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) hardware_drained=(?P<hardware_drained>{H}) data_remained_contained=(?P<data_remained_contained>[01]) producers_quiescent=(?P<producers_quiescent>[01]) fabric_acknowledged=(?P<fabric_acknowledged>[01])'),
    'receipt':re.compile(rf'FES_NATIVE_FPGA_DDR_READER PASS owner=(?P<owner>{H}) generation=(?P<generation>{D}) reader_generation=1 cookie=7 bytes=32 data_closed=1 runtime_programming=unavailable'),
}
HEX={'owner','reset','offset','value','checksum','data0','data1','data2','data3','hardware_drained',
     'idr0','idr1','aidr','s_idr1','cr0','ack','gbpa','agbpa','s_cr0','s_ack','s_init','s_gbpa','s_agbpa',
     'enable','base','baseext','limit','limitext','abi'}
TEXT={'package_id','build_id','map_sha256'}

class _Incomplete(Exception):pass
class _Invalid(Exception):pass


def validate(data,*,owner,source_generation,identity,memory,contract,rules):
    """Validate actual paired MMIO, fresh gates, endpoint and hardware closure.

    A serial prefix stays incomplete. No synthetic successful observation is
    inserted into another decoder; all returned records keep their actual state.
    """
    fields={'format','scope','package_id','build_id','reader_memory_sha256','control_map_sha256',
            'read_bytes','open_mask','close_mask','adapter_source_sha256','adapter_generated_sha256','arcache','arprot','aruser'}
    if (not isinstance(contract,dict) or set(contract)!=fields or
        contract['scope']!='sealed-cold-ddr-read-v1' or contract['package_id']!=identity['package_id'] or
        contract['build_id']!=identity['build_id'] or contract['reader_memory_sha256']!=digest(memory) or
        contract['control_map_sha256']!=identity['map_sha256'] or
        any(type(contract[k]) is not int or contract[k]!=v for k,v in
            {'format':1,'read_bytes':32,'open_mask':8,'close_mask':10,'arcache':2,'arprot':3,'aruser':224}.items()) or
        any(not isinstance(contract[k],str) or not re.fullmatch('[0-9a-f]{64}',contract[k]) or not int(contract[k],16)
            for k in ('adapter_source_sha256','adapter_generated_sha256')) or
        type(owner) is not int or not 0<owner<1<<64 or type(source_generation) is not int or
        not 0<source_generation<(1<<64)-3 or identity['control_abi']!=0x10203041):
        raise ValueError('independently checked sealed DDR read contract and owner required')
    result={'complete':False,'errors':[],'records':[],'reads':0,'writes':0,'polls':0,
            'data_remained_contained':False,'runtime_admission':False,'core_load_acceptance':False}
    raw=data.encode() if isinstance(data,str) else bytes(data)
    if len(raw)>1<<20:result['errors'].append('DDR capture exceeds byte bound');return result
    rows=raw.decode(errors='replace').split('\n')[:-1];cursor=0
    def require(ok,message):
        if not ok:result['errors'].append(message);raise _Invalid()
    def take(kind,phase=None):
        nonlocal cursor
        while cursor<len(rows) and not rows[cursor].strip():cursor+=1
        if cursor==len(rows):raise _Incomplete()
        line=rows[cursor].rstrip('\r');cursor+=1
        pattern=PATTERNS.get(kind) or re.compile(rules[kind])
        match=pattern.fullmatch(line)
        require(match is not None,'missing, reordered or malformed DDR '+kind+' at suffix line '+str(cursor))
        values={k:v if k in TEXT or k=='phase' and v in ('before','after','open','read','close','closed')
                else int(v,16 if k in HEX else 10) for k,v in match.groupdict().items()}
        require(phase is None or values['phase']==phase,'DDR phase differs: '+kind)
        result['records'].append({'stage':'ddr_'+kind,'suffix_line':cursor,'values':values})
        return values
    def owned(v,generation,reset):
        require(v['owner']==owner and v['generation']==generation and v['reset']==reset,'DDR event lost owned bridge scope')
    def control(phase,delta,reset):owned(take('control',phase),source_generation+delta,reset)
    def gate():
        v=take('smmu')
        require(v=={'status':0,'reads':26,'idr0':0x080f7e3f,'idr1':0x0e739d18,'aidr':1,'s_idr1':0x80000018,
            'cr0':0,'ack':0,'gbpa':0,'agbpa':0,'s_cr0':0,'s_ack':0,'s_init':0,'s_gbpa':0,'s_agbpa':0,
            'identified':1,'settled':1,'bypass_controls_clear':1},'DDR use-point translation is unqualified')
        v=take('ddr_firewall');owned(v,source_generation+1,13)
        require(v['status']==0 and v['reads']==66 and v['mpfe_reset']==0 and v['base']==0x95000000 and v['bytes']==8192,
                'DDR use-point firewall observation failed')
        enable=v['enable'];covered=False
        require(not enable&~255,'DDR firewall enable layout is unknown')
        for i in range(8):
            v=take('ddr_region')
            require(v['index']==i and v['base']&65535==0 and v['limit']&65535==65535 and
                    v['base']<=0xffffffff and v['limit']<=0xffffffff and
                    v['baseext']<=255 and v['limitext']<=255,'DDR firewall region layout differs')
            low=v['baseext']<<32|v['base'];high=v['limitext']<<32|v['limit']
            require(not enable>>i&1 or low<=high,'enabled DDR firewall region is inverted')
            if enable>>i&1 and low<=0x95000000 and high>=0x95001fff:covered=True
        require(take('ddr_firewall_end')=={'layout_known':1,'window_covered':1} and covered,'DDR read window is not covered')
        v=take('gate');owned(v,source_generation+1,13)
        require(v['status']==0 and v['pattern_words']==2048,'DDR fixed pattern gate rejected')
    def phase(n):
        v=take('phase',n);owned(v,source_generation+2,5)
        require(all(v[k]==result[k] for k in ('reads','writes','polls')),'DDR progress counters differ')
    def read(offset):
        a,b=take('access','before'),take('access','after');result['reads']+=1
        require(a==dict(phase='before',ordinal=result['reads'],offset=offset,value=0) and
                b['ordinal']==result['reads'] and b['offset']==offset and 0<=b['value']<=0xffffffff,
                'DDR read pair, ordinal, offset or value differs')
        return b['value']
    def write(offset,value):
        for side in ('before','after'):
            require(take('write',side)==dict(phase=side,offset=offset,value=value),'DDR command changed or replayed')
        result['writes']+=1
    def state(status,pending,ack):
        offsets=(0x38,0x3c,0x1c,0x20,0x40,0x44,0x48,0x4c,0x24,0x28,0x2c,0x30)
        for _ in range(128):
            result['polls']+=1
            first=[read(o) for o in offsets];v=[read(o) for o in offsets]
            if first!=v:continue
            require(v[0] in (1,2,4) and not v[1]&~0x10007 and v[1]&7<=1 and
                    v[2:4]==[1,0] and v[5]==v[7]==v[9]==v[11]==0 and v[8]==1 and v[10]==(7 if ack else 0),
                    'DDR generation, pending state or requested cookie differs')
            if not ack:require(v[4]==v[6]==0 and v[0]!=4,'DDR acknowledged before disable')
            elif v[4] or v[6] or v[0]==4:
                require(v[:8]==[4,0,1,0,1,0,7,0],'DDR final ACK has wrong generation or cookie')
            if v[:8]==[status,pending,1,0,ack,0,7 if ack else 0,0]:return
            require(v[0]==(1 if status==1 else 2),'DDR state transition differs')
        require(False,'DDR state exceeded bounded poll count')
    try:
        control(14,1,13);gate();control(15,1,13);gate();control(16,2,5);phase(1)
        offsets=(0,4,8,12,16,20,24,28,32,36,40,44,48,56,60,64,68,72,76,0xb0,0xb4,0xb8,0x94,0x98,0xa0,0xa4,0xa8,0xac)
        build=tuple((int(identity['build_id'],16)>>(32*i))&0xffffffff for i in range(4))
        initial=(0x46455345,0x10000,0x10203041,*build,*([0]*12),0x95000000,8192,1,*([0]*6))
        for _ in range(2):
            require([read(o) for o in offsets]==list(initial),'DDR initial identity, window or counters differ')
        phase(2);write(0x24,1);write(0x28,0);write(0x34,1);state(1,0,0)
        phase(3);write(0x84,0x95000000);write(0x88,1);write(0x80,8);state(1,1,0)
        phase(4);phase(5);write(0x2c,7);write(0x30,0);write(0x34,2);phase(6);state(4,0,1);phase(7)
        words=struct.unpack('<8I',reader_pattern()[:32]);checksum=0
        for w in words:checksum^=w
        require(read(0x94)==32 and read(0x98)==checksum and [read(0xa0+4*i) for i in range(4)]==list(words[4:]),
                'DDR returned bytes, checksum or final beat differs')
        state(4,0,1);phase(8)
        require(take('result')==dict(status=0,bytes=32,checksum=checksum,**{'data'+str(i):words[i+4] for i in range(4)},generation=1,cookie=7),
                'DDR helper result differs from its readback')
        control(17,2,5)
        offsets=(0,4,8,12,16,20,24,28,32,64,68,72,76,56,60)
        expected=(0x46455345,0x10000,0x10203041,*build,1,0,1,0,7,0,4,0)
        for ordinal in range(31,61):
            offset=offsets[(ordinal-31)%15];value=expected[(ordinal-31)%15]
            for side in ('before','after'):
                require(take('identity_access',side)==dict(phase=side,ordinal=ordinal,offset=offset,value=value if side=='after' else 0),
                        'DDR final live identity differs')
        v=take('identity_transition','close');owned(v,source_generation+2,5)
        require(v['base']==identity['cpu_base'],'DDR close changed control aperture')
        v=take('master');owned(v,source_generation+2,5)
        require(all(v[k]==x for k,x in {'status':0,'reader_reads':88,'reader_polls':1,'disable':0,'probe_polls':2,'cancel':0,'complete':1}.items()),
                'DDR all-master stop differs')
        v=take('closure');owned(v,source_generation+3,15)
        require(v['hardware_drained']==8 and v['data_remained_contained']==0 and v['producers_quiescent']==1 and v['fabric_acknowledged']==0,
                'DDR hardware closure receipt differs')
        v=take('identity_transition','closed');owned(v,source_generation+3,15)
        require(v['base']==identity['cpu_base'],'DDR closure changed control aperture')
        v=take('fpga_identity')
        require(v==dict(package_id=identity['package_id'],build_id=identity['build_id'],abi=0x10203041,base=identity['cpu_base'],
                map_sha256=identity['map_sha256'],owner=owner,generation=source_generation+3),'DDR native identity differs')
        require(take('receipt')==dict(owner=owner,generation=source_generation+3),'DDR native completion lost ownership')
        require(all(not line.strip() for line in rows[cursor:]),'DDR capture has extra or repeated events')
        result['complete']=raw.endswith(b'\n')
    except (_Incomplete,_Invalid):pass
    return result


def decode(data,build_id,owner_seed,*,identity,memory,contract,base_decode,rules):
    if not isinstance(data,(str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw=data.encode() if isinstance(data,str) else bytes(data)
    if len(raw)>1<<20:return base_decode(raw)
    lines=raw.splitlines(keepends=True);split=None
    for index,line in enumerate(lines):
        match=CONTROL.fullmatch(line.decode(errors='replace').rstrip('\r\n'))
        if match and match['phase']=='14':split=index;break
    prefix=raw if split is None else b''.join(lines[:split])
    report=base_decode(prefix)
    seal=next((r['values'] for r in report['records'] if r['stage']=='spl_handoff'),None)
    observed=validate(b'' if split is None else b''.join(lines[split:]),owner=seal['owner'] if seal else owner_seed,
                      source_generation=4,identity=identity,memory=memory,contract=contract,rules=rules)
    report['errors'].extend(observed['errors'])
    if split is None and report['complete']:report['errors'].append('active DDR diagnostic ended in the contained-only profile')
    prefix_complete=report['last_checkpoint']=='identity_access_30_after'
    if split is not None and not prefix_complete:report['errors'].append('DDR read preceded complete initial live identity')
    report['complete']=bool(split is not None and prefix_complete and seal and report.get('configuration_transfer',{}).get('complete') and
                            observed['complete'] and not report['errors'])
    report['ddr_read']=observed
    report['core_load_acceptance']=False
    report['classification']='serial-observed fixed DDR read, master stop and bridge drain; no runtime admission'
    if observed['complete']:report['last_checkpoint']='ddr_read_closed'
    return report
