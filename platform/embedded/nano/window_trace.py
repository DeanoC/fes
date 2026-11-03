"""Audit source-bound native full-window sessions; grant no admission."""
import re

from hps_context import validate_window_read

H=r'[0-9a-f]{1,16}'
D=r'[0-9]{1,20}'
P=r'(?:NOTICE:[ \t]+)?'
PATTERNS={
    'scope':re.compile(P+rf'FES_NANO_WINDOW_SCOPE phase=(?P<phase>control|data|close) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H})'),
    'gate':re.compile(P+rf'FES_NANO_WINDOW_GATE status=(?P<status>-?{D}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) bytes=(?P<bytes>{D}) permit=(?P<permit>owned-read|denied)'),
    'master':re.compile(P+rf'FES_NANO_MASTER_STOP status=(?P<status>-?{D}) owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) reader_reads=(?P<reader_reads>{D}) reader_polls=(?P<reader_polls>{D}) disable=(?P<disable>[01]) probe_polls=(?P<probe_polls>{D}) cancel=(?P<cancel>[01]) complete=(?P<complete>[01]) data=active'),
    'closure':re.compile(P+rf'FES_NANO_WINDOW_CLOSURE owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) hardware_drained=(?P<hardware_drained>{H}) data_remained_contained=(?P<data_remained_contained>[01]) producers_quiescent=(?P<producers_quiescent>[01]) fabric_acknowledged=(?P<fabric_acknowledged>[01])'),
    'receipt':re.compile(rf'FES_NATIVE_FPGA_WINDOW_READ PASS package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) source_generation=(?P<source_generation>{D}) closed_generation=(?P<closed_generation>{D}) reader_generation=(?P<reader_generation>{D}) cookie=(?P<cookie>{D}) bytes=(?P<bytes>{D}) crc32=(?P<crc32>[0-9a-f]{{8}}) frame_sha256=(?P<frame_sha256>[0-9a-f]{{64}}) access_map_sha256=(?P<access_map_sha256>[0-9a-f]{{64}}) data_closed=1 memory_retained=1 runtime_admission=unavailable'),
    'frame':re.compile(rf'FES_NATIVE_FPGA_WINDOW_PRESENT FRAME index=(?P<index>{D}) package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) source_generation=(?P<source_generation>{D}) reader_generation=(?P<reader_generation>{D}) cookie=(?P<cookie>{D}) bytes=(?P<bytes>{D}) published_crc32=(?P<published_crc32>[0-9a-f]{{8}}) read_crc32=(?P<read_crc32>[0-9a-f]{{8}}) frame_sha256=(?P<frame_sha256>[0-9a-f]{{64}}) drain_generation=(?P<drain_generation>{D}) drain_cookie=(?P<drain_cookie>{D}) memory_retained=1'),
    'reader_drain':re.compile(P+rf'FES_NANO_WINDOW_READER_DRAIN owner=(?P<owner>{H}) generation=(?P<generation>{D}) reset=(?P<reset>{H}) reader_generation=(?P<reader_generation>{D}) request_generation=(?P<request_generation>{D}) cookie=(?P<cookie>{D}) ack_generation=(?P<ack_generation>{D}) ack_cookie=(?P<ack_cookie>{D}) status=(?P<status>{H}) pending=(?P<pending>{H})'),
    'present_receipt':re.compile(rf'FES_NATIVE_FPGA_WINDOW_PRESENT PASS package_id=(?P<package_id>[0-9a-f]{{64}}) build_id=(?P<build_id>[0-9a-f]{{32}}) owner=(?P<owner>{H}) source_generation=(?P<source_generation>{D}) closed_generation=(?P<closed_generation>{D}) frames=(?P<frames>{D}) last_reader_generation=(?P<last_reader_generation>{D}) last_cookie=(?P<last_cookie>{D}) bytes=(?P<bytes>{D}) crc32=(?P<crc32>[0-9a-f]{{8}}) frame_sha256=(?P<frame_sha256>[0-9a-f]{{64}}) access_map_sha256=(?P<access_map_sha256>[0-9a-f]{{64}}) data_closed=1 memory_retained=1 runtime_admission=unavailable'),
}
HEX={'owner','reset','crc32','hardware_drained','idr0','idr1','aidr','s_idr1','cr0','ack','gbpa','agbpa',
     's_cr0','s_ack','s_init','s_gbpa','s_agbpa','enable','base','baseext','limit','limitext','published_crc32','read_crc32','pending'}
TEXT={'permit','phase','package_id','build_id','frame_sha256','access_map_sha256'}


class _Incomplete(Exception):pass
class _Invalid(Exception):pass


def checked_contract(identity,memory,contract):
    if (not isinstance(identity,dict) or set(identity)!={'sha256','package_id','build_id','control_abi','cpu_base','map_sha256'} or
            type(identity.get('control_abi')) is not int or identity['control_abi']!=0x10203042 or
            type(identity.get('cpu_base')) is not int or identity['cpu_base']!=0x20010400 or
            any(not isinstance(identity.get(k),str) or not re.fullmatch('[0-9a-f]{'+str(n)+'}',identity[k]) or not int(identity[k],16)
                for k,n in (('sha256',64),('package_id',64),('build_id',32),('map_sha256',64)))):
        raise ValueError('explicit sealed window identity required')
    context=dict(programming_profile='de25-embedded-hps-first-window-diagnostic-v1',
                 control_abi=0x10203042,package_id=identity['package_id'],build_id=identity['build_id'])
    return validate_window_read(context,memory,identity['map_sha256'],contract)


def validate(data,*,owner,source_generation,identity,memory,contract,publication,rules,present=False):
    """Check two fresh full-window gates, real master-stop and native return.

    The receipt is the matched native driver's checked frame and owned-closure
    observation. This audit does not invent paired CSR or electrical evidence.
    """
    from hps_context import digest
    if type(present) is not bool:raise ValueError('window presentation selection must be explicit boolean')
    checked_contract(identity,memory,contract)
    if (type(owner) is not int or not 0<owner<1<<64 or type(source_generation) is not int or
            not 0<source_generation<(1<<64)-3):
        raise ValueError('bounded owned window source required')
    result=dict(complete=False,errors=[],records=[],runtime_admission=False,memory_released=False,
                core_load_acceptance=False,hdmi_qualified=False)
    raw=data.encode() if isinstance(data,str) else bytes(data)
    if len(raw)>1<<20:
        result['errors'].append('window capture exceeds byte bound');return result
    rows=raw.decode(errors='replace').split('\n')[:-1];cursor=0
    def require(ok,reason):
        if not ok:
            result['errors'].append(reason);raise _Invalid()
    def take(kind):
        nonlocal cursor
        while cursor<len(rows) and not rows[cursor].strip():cursor+=1
        if cursor==len(rows):raise _Incomplete()
        match=(PATTERNS.get(kind) or re.compile(rules[kind])).fullmatch(rows[cursor].rstrip('\r'))
        cursor+=1
        require(match is not None,'missing, reordered or malformed window '+kind+' at suffix line '+str(cursor))
        value={k:v if k in TEXT else int(v,16 if k in HEX or kind=='reader_drain' and k=='status' else 10) for k,v in match.groupdict().items()}
        result['records'].append(dict(stage='window_'+kind,suffix_line=cursor,values=value))
        return value
    def owned(v,generation,reset):
        require(v['owner']==owner and v['generation']==generation and v['reset']==reset,
                'window event changed owner, generation or reset scope')
    def scope(phase,generation,reset):
        v=take('scope');owned(v,generation,reset);require(v['phase']==phase,'window scope phase differs')
    try:
        scope('control',source_generation+1,13)
        for _ in range(2):
            translation=take('smmu')
            require(translation==dict(status=0,reads=26,idr0=0x080f7e3f,idr1=0x0e739d18,aidr=1,s_idr1=0x80000018,
                cr0=0,ack=0,gbpa=0,agbpa=0,s_cr0=0,s_ack=0,s_init=0,s_gbpa=0,s_agbpa=0,
                identified=1,settled=1,bypass_controls_clear=1),'window use-point translation is not the sealed bypass route')
            firewall=take('ddr_firewall');owned(firewall,source_generation+1,13)
            require(firewall['status']==0 and firewall['reads']==66 and firewall['mpfe_reset']==0 and
                    firewall['base']==memory['bus_base'] and firewall['bytes']==memory['bytes'] and not firewall['enable']&~255,
                    'window use-point firewall observation differs')
            covered=False
            for i in range(8):
                region=take('ddr_region')
                require(region['index']==i and region['base']&65535==0 and region['limit']&65535==65535 and
                        max(region['base'],region['limit'])<=0xffffffff and
                        max(region['baseext'],region['limitext'])<=255,'window firewall region layout is unknown')
                low=region['baseext']<<32|region['base'];high=region['limitext']<<32|region['limit']
                require(not firewall['enable']>>i&1 or low<=high,'enabled window firewall region is inverted')
                covered|=bool(firewall['enable']>>i&1 and low<=memory['bus_base'] and high>=memory['bus_base']+memory['bytes']-1)
            require(take('ddr_firewall_end')==dict(layout_known=1,window_covered=1) and covered,
                    'firewall does not cover the complete window')
            gate=take('gate');owned(gate,source_generation+1,13)
            require(gate['status']==0 and gate['bytes']==memory['bytes'] and gate['permit']=='owned-read','window gate denied access')
        scope('data',source_generation+2,5)
        frames=[]
        if present:
            for index in range(1,13):
                frame=take('frame')
                require(frame['index']==index and frame['package_id']==identity['package_id'] and
                    frame['build_id']==identity['build_id'] and frame['owner']==owner and
                    frame['source_generation']==source_generation and frame['reader_generation']==index+1 and
                    frame['cookie']==1 and frame['bytes']==memory['frame_bytes'] and
                    frame['published_crc32']==frame['read_crc32'] and int(frame['frame_sha256'],16)>0 and
                    frame['drain_generation']==index+1 and frame['drain_cookie']==index+1,
                    'window presentation changed publication, reader or local drain')
                frames.append(frame)
            require(len({f['read_crc32'] for f in frames})==12 and len({f['frame_sha256'] for f in frames})==12,
                    'window presentations did not change pixels')
        scope('close',source_generation+2,5)
        stop=take('master');owned(stop,source_generation+2,5)
        if present:
            require(stop['status']==0 and stop['complete']==1 and stop['disable']==0 and
                    stop['reader_polls']==1 and stop['reader_reads']==88 and stop['probe_polls']==2 and stop['cancel']==0,
                    'window presentation return lacks the already-drained complete master stop')
            drain=take('reader_drain');owned(drain,source_generation+2,5)
            require(drain['reader_generation']==drain['request_generation']==drain['ack_generation']==13 and
                    drain['cookie']==drain['ack_cookie']==13 and drain['status']==4 and drain['pending']==0,
                    'window firmware did not confirm the final local reader drain')
        else:
            require(stop['status']==0 and stop['complete']==1 and stop['disable']==1 and
                    2<=stop['reader_polls']<=128 and stop['reader_reads']==44*(stop['reader_polls']+1) and
                    2<=stop['probe_polls']<=128,'window return lacks the bounded complete all-master stop')
        closure=take('closure');owned(closure,source_generation+3,15)
        require(closure['hardware_drained']==8 and closure['data_remained_contained']==0 and
                closure['producers_quiescent']==1 and closure['fabric_acknowledged']==0,
                'window firmware closure lacks the exact active DDR drain')
        require(publication is not None,'window read lacks a complete pinned publication')
        if present:
            receipt=take('present_receipt')
            require(receipt==dict(package_id=identity['package_id'],build_id=identity['build_id'],owner=owner,
                source_generation=source_generation,closed_generation=source_generation+3,frames=12,
                last_reader_generation=13,last_cookie=1,bytes=memory['frame_bytes'],crc32=frames[-1]['read_crc32'],
                frame_sha256=frames[-1]['frame_sha256'],access_map_sha256=digest(contract)),
                'window native presentation return differs from its final frame')
            result.update(presentations=frames,reader_drain=drain)
        else:
            receipt=take('receipt')
            require(receipt==dict(package_id=identity['package_id'],build_id=identity['build_id'],owner=owner,
                source_generation=source_generation,closed_generation=source_generation+3,reader_generation=1,cookie=1,
                bytes=memory['frame_bytes'],crc32=publication['crc32'],frame_sha256=publication['frame_sha256'],
                access_map_sha256=digest(contract)),'window native frame or owned-return receipt differs')
        require(all(not line.strip() for line in rows[cursor:]),'window capture contains extra or replayed events')
        result.update(complete=raw.endswith(b'\n'),master_stop=stop,closure=closure,receipt=receipt)
    except (_Incomplete,_Invalid):pass
    return result


def decode(data,*,identity,memory,contract,base_decode,rules,present=False):
    checked_contract(identity,memory,contract)
    if not isinstance(data,(str,bytes,bytearray,memoryview)):
        raise TypeError('serial bytes or text required')
    raw=data.encode() if isinstance(data,str) else bytes(data)
    if len(raw)>1<<20:
        return dict(complete=False,loader_ready=False,records=[],errors=['window capture exceeds byte bound'],core_load_acceptance=False)
    rows=raw.splitlines(keepends=True);split=None
    for i,line in enumerate(rows):
        match=PATTERNS['scope'].fullmatch(line.decode(errors='replace').rstrip('\r\n'))
        if line.endswith(b'\n') and match and match['phase']=='control':
            split=i;break
    prefix=raw if split is None else b''.join(rows[:split])
    report=base_decode(prefix)
    publication=report.get('window_memory_observation')
    source=next((r['values'] for r in report['records'] if r['stage']=='hps_context'),None)
    observed=validate(b'' if split is None else b''.join(rows[split:]),owner=source['owner'] if source else 1,
        source_generation=4,identity=identity,memory=memory,contract=contract,publication=publication,rules=rules,present=present)
    report['errors'].extend(observed['errors'])
    prefix_complete=bool(report['complete'] and report['last_checkpoint']=='ddr_firewall_end' and
                         report.get('configuration_transfer',{}).get('complete') and source and publication)
    if split is not None and not prefix_complete:report['errors'].append('window access preceded a complete owned boot/configuration prefix')
    report['complete']=bool(split is not None and prefix_complete and observed['complete'] and not report['errors'])
    report['window_read']=observed
    report['classification']='serial-observed native '+('repeated presentation' if present else 'full-frame read')+' and owned firmware return; no display or runtime admission'
    report['core_load_acceptance']=False
    if observed['complete']:report['last_checkpoint']='window_present_closed' if present else 'window_read_closed'
    return report
