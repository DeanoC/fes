"""Check the request service's serial evidence; no device access or admission."""
import json
import re
from configuration_trace import extract as extract_configuration
from native_trace import RULES, DECIMAL, decode_native_boot

PREFIX=b'FES_NATIVE_PROGRAM_REPLY '
SCOPE='nano-contained-program-service-v1'
MAX_BYTES=1<<20


def _unique(pairs):
    result={}
    for key,value in pairs:
        if key in result:raise ValueError('duplicate JSON reply field')
        result[key]=value
    return result


def require(condition,message):
    if not condition:raise ValueError(message)


def number(value,maximum):return type(value) is int and 0<=value<=maximum


def response(data):
    """Accept one complete terminal reply and preserve all preceding evidence."""
    require(isinstance(data,(bytes,bytearray)) and 0<len(data)<=MAX_BYTES,'bounded service bytes required')
    data=bytes(data);require(data.endswith(b'\n'),'incomplete service reply')
    rows=data.splitlines(keepends=True);indices=[i for i,row in enumerate(rows) if row.startswith(PREFIX)]
    require(len(indices)==1 and indices[0]==len(rows)-1,'missing, duplicate or nonterminal service reply')
    prefix=b''.join(rows[:-1]);text=prefix.decode('utf-8')
    require(not any(r.startswith(('panic:','fatal error:')) or 'FES_' in r and re.search(r'\b(?:FAIL|FAILURE|REJECT|EXCEPTION)\b',r) for r in text.splitlines()),'native failure before service reply')
    r=json.loads(rows[-1][len(PREFIX):],object_pairs_hook=_unique)
    fields={'scope','op','ok','state','owner','generation','held'}
    optional={'package_id','build_id','core_id','programs','activity','error','rom_receipt_sha256'}
    require(isinstance(r,dict) and fields<=r.keys() and not r.keys()-fields-optional,'service reply fields differ')
    require(r['scope']==SCOPE and isinstance(r['op'],str) and 0<len(r['op'])<=32,'service reply scope/operation differs')
    require(type(r['ok']) is type(r['held']) is bool and number(r['generation'],(1<<64)-1),'invalid service flags/generation')
    require(r['state'] in ('ready','loading','running','returning','parked','reboot_required'),'unknown service state')
    require(isinstance(r['owner'],str) and re.fullmatch('[0-9a-f]{16}',r['owner']) and int(r['owner'],16)>0,'invalid boot owner')
    identity={key:r[key] for key in ('package_id','build_id','core_id') if key in r}
    require(not identity or len(identity)==3,'partial program identity')
    if identity:
        require(all(isinstance(identity[k],str) and re.fullmatch('[0-9a-f]{'+str(n)+'}',identity[k]) and int(identity[k],16) for k,n in (('package_id',64),('build_id',32))) and
                isinstance(identity['core_id'],str) and 0<len(identity['core_id'])<=128,'invalid program identity')
    if r['state'] in ('ready','parked'):require(not identity,'inactive service carries program identity')
    if r['state']=='running':require(len(identity)==3,'running service lacks identity')
    if 'rom_receipt_sha256' in r:
        receipt=r['rom_receipt_sha256']
        require(r['state'] not in ('ready','parked') and len(identity)==3 and isinstance(receipt,str) and
                re.fullmatch('[0-9a-f]{64}',receipt) and int(receipt,16)>0,'invalid retained ROM receipt')
    require(('error' not in r) if r['ok'] else isinstance(r.get('error'),str) and 0<len(r['error'])<=1024,'service error flag differs')
    if 'programs' in r:
        require(r['op']=='status' and isinstance(r['programs'],list) and 0<len(r['programs'])<=32,'unexpected catalogue')
        for i,row in enumerate(r['programs']):
            require(isinstance(row,dict) and set(row)=={'index','package_id'} and type(row['index']) is int and row['index']==i and isinstance(row['package_id'],str) and re.fullmatch('[0-9a-f]{64}',row['package_id']),'invalid catalogue row')
    if 'activity' in r:
        v=r['activity'];require(r['op']=='activity' and isinstance(v,dict) and set(v)=={'Cycles','Address'} and number(v['Cycles'],0xffffffff) and number(v['Address'],0xffff),'invalid fetch observation')
    return r,prefix


def startup(data,build_id,owner_seed,atf,package,identities):
    r,prefix=response(data)
    # The service replaces the diagnostic's final Go/package report. Decode
    # only the actual common SPL/EL3/loader/EL1 prefix; never invent a marker.
    boot=decode_native_boot(prefix,build_id,owner_seed,package=package,atf=atf)
    require(not boot['errors'] and boot['loader_ready'] and boot['last_checkpoint']=='el1','matched service boot prefix rejected')
    seal=next(row['values'] for row in boot['records'] if row['stage']=='spl_handoff')
    require(r['ok'] and r['op']=='status' and r['state']=='ready' and r['generation']==0 and r['held'] and int(r['owner'],16)==seal['owner'],'initial service state differs from matched boot')
    require(r.get('programs')==[{'index':i,'package_id':identity['package_id']} for i,identity in enumerate(identities)],'initial catalogue differs from freshly verified SD choices')
    require('activity' not in r,'unexpected startup activity')
    return {'boot':boot,'reply':r,'owner':seal['owner'],'scope':'matched native startup and cached application Ready; no runtime admission'}


def _records(data):
    rules=dict(RULES);records=[]
    for i,raw in enumerate(data.splitlines(),1):
        line=raw.decode('utf-8').rstrip('\r')
        if not line.strip():continue
        matches=[(name,p.fullmatch(line)) for name,p in RULES]
        matches=[(name,m) for name,m in matches if m]
        require(len(matches)==1,'unexpected service firmware observation: '+line[:120])
        name,m=matches[0]
        v={k:int(value,10 if k in DECIMAL or name in ('master_stop','ddr_firewall') and k=='generation' else 16)
           for k,value in m.groupdict().items()}
        records.append({'stage':name,'line':i,'values':v})
    return records


def opening(data,configuration,owner,generation,previous):
    parsed=extract_configuration(data,configuration,generation=generation,previous=previous,rearm=previous!=0,
                                 require_activation=True,require_programmed=False,expected_owner=owner)
    require(not parsed['errors'] and parsed['complete'],'incomplete or invalid configuration transfer')
    timing=parsed['timing']
    require(timing is not None and 0<timing['interval_ticks']<20*400000000,'configuration exceeded the original20s bound')
    require(any(row['stage']=='configuration_mailbox' for row in parsed['records']) and any(row['stage']=='configuration_engine' for row in parsed['records']),'missing configuration queue observations')
    require(parsed['records'] and parsed['records'][-1]['stage']=='configuration_complete','configuration did not finish before opening')
    rows=_records(b'\n'.join(parsed['parts']))
    expected=['fpga_query','fpga_status','smmu','ddr_firewall']+['ddr_region']*8+['ddr_firewall_end']
    require([row['stage'] for row in rows]==expected,'control opening observations missing or reordered')
    # C control observations must follow completion in the actual transcript.
    require(not any(raw.strip() for raw in parsed['parts'][:parsed['records'][-1]['line']]),'control opening preceded transfer completion')
    values=[r['values'] for r in rows];query,status,smmu,firewall=values[:4]
    require(query=={'command':4,'words':6} if 'command' in query else query=={},'invalid readiness query')
    ready=status['state']==0 and bool(status['pins']&(1<<31)) and status['soft']&3==3 and not status['soft']&8
    require(status['status']==0 and status['words']==6 and status['ready']==1 and ready and all(0<=status[k]<=0xffffffff for k in ('state','version','pins','soft','error_location','error_details')),'invalid SDM readiness')
    secure=bool(smmu['s_idr1']&(1<<31))
    identified=bool(smmu['idr0'] and smmu['idr0']!=0xffffffff and all(smmu[k]!=0xffffffff for k in ('idr1','aidr','s_idr1')))
    settled=smmu['cr0']==smmu['ack'] and not smmu['gbpa']&(1<<31) and (not secure or smmu['s_cr0']==smmu['s_ack'] and not smmu['s_gbpa']&(1<<31) and not smmu['s_init'])
    clear=identified and settled and not any(smmu[k] for k in ('cr0','gbpa','agbpa')) and (not secure or not any(smmu[k] for k in ('s_cr0','s_gbpa','s_agbpa')))
    require(smmu['status']==0 and smmu['reads']==(26 if secure else 16) and smmu['identified']==int(identified) and smmu['settled']==int(settled) and smmu['bypass_controls_clear']==int(clear),'SMMU interpretation differs')
    require(secure or not any(smmu[k] for k in ('s_cr0','s_ack','s_init','s_gbpa','s_agbpa')),'absent secure SMMU bank was accessed')
    require(firewall==dict(status=0,owner=owner,generation=generation,reset=15,mpfe_reset=0,reads=66,enable=firewall['enable'],base=0x95000000,bytes=8192),'firewall scope differs')
    regions=values[4:12];known=not firewall['enable']&~255;covered=False
    for i,v in enumerate(regions):
        require(v['index']==i,'firewall region order differs')
        first=v['base']|(v['baseext']<<32);last=v['limit']|(v['limitext']<<32);enabled=bool(firewall['enable']&(1<<i))
        known=known and all(0<=v[k]<=0xffffffff for k in ('base','baseext','limit','limitext')) and not (v['base']&0xffff or v['limit']&0xffff!=0xffff or (v['baseext']|v['limitext'])&~255 or enabled and first>last)
        covered=covered or enabled and first<=0x95000000 and last>=0x95001fff
    covered=bool(known and covered)
    require(values[-1]=={'layout_known':int(known),'window_covered':int(covered)},'firewall interpretation differs')
    return {'configuration_timing':timing,'configuration_records':parsed['records'],'control_records':rows,
            'configuration_generation':generation,'runtime_admission':False,'physical_ddr_drain_qualified':False}


def closed(data,owner,generation):
    records=_records(data)
    require(len(records)==1 and records[0]['stage']=='master_stop','missing or repeated master stop')
    value=records[0]['values']
    require(value==dict(status=0,owner=owner,generation=generation+1,reset=13,reader_reads=88,reader_polls=1,disable=0,probe_polls=2,cancel=0,complete=1),'master stop differs from owned held-data recipe')
    return dict(value,physical_ddr_drain_qualified=False)
