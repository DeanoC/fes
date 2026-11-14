"""Audit fixed-window request replies and real firmware evidence; no I/O/grants."""
import json
import re

from configuration_trace import extract
from native_trace import RULES, decode_native_configuration
from service_trace import _unique, number, require
from window_trace import PATTERNS, HEX, TEXT, checked_contract, check_route

PREFIX=b'FES_NATIVE_WINDOW_REPLY '
SCOPE='nano-contained-window-service-v1'
MAX_BYTES=1<<20


def quiet(data):return all(line.strip() in (b'',b'NOTICE:') for line in data.splitlines())


def response(data):
    require(isinstance(data,(bytes,bytearray)) and 0<len(data)<=MAX_BYTES,'bounded window service bytes required')
    data=bytes(data);require(data.endswith(b'\n'),'incomplete window service reply')
    lines=data.splitlines(keepends=True)
    require(sum(line.startswith(PREFIX) for line in lines)==1 and lines[-1].startswith(PREFIX),'one terminal window reply required')
    prefix=b''.join(lines[:-1]);text=prefix.decode('utf-8')
    require('FES_NATIVE_WINDOW_REPLY' not in text and 'FES_NATIVE_PROGRAM_REPLY' not in text,'foreign or malformed service reply')
    require(not any(line.startswith(('panic:','fatal error:')) or 'FES_' in line and re.search(r'\b(?:FAIL|FAILURE|REJECT|EXCEPTION)\b',line) for line in text.splitlines()),'native failure before reply')
    def constant(value):raise ValueError('non-finite JSON value: '+value)
    r=json.loads(lines[-1][len(PREFIX):],object_pairs_hook=_unique,parse_constant=constant)
    base={'scope','op','ok','state','owner','generation','frames','package_id'}
    require(type(r) is dict and base<=r.keys() and not r.keys()-base-{'last_frame','last_return','storage','error'},'window reply fields differ')
    require(r['scope']==SCOPE and type(r['op']) is str and 0<len(r['op'])<=32,'window scope/operation differs')
    require(type(r['ok']) is bool and number(r['generation'],(1<<64)-1) and number(r['frames'],(1<<64)-2),'window flags/counts differ')
    require(r['state'] in ('ready','running','parked','loading','returning','reboot_required'),'unknown window service state')
    for key,length in (('owner',16),('package_id',64)):
        require(type(r[key]) is str and re.fullmatch('[0-9a-f]{'+str(length)+'}',r[key]) and int(r[key],16)>0,'invalid window '+key)
    require(('error' not in r) if r['ok'] else type(r.get('error')) is str and 0<len(r['error'])<=1024,'window error flag differs')
    if r['state']=='reboot_required':
        require(not r['ok'] and not {'last_frame','last_return','storage'}&r.keys(),'retired service returned success observations')
        return r,prefix
    f=r.get('last_frame');s=r.get('storage')
    require(type(f) is dict and set(f)=={'source_generation','reader_generation','cookie','bytes','published_crc32','read_crc32','frame_sha256','drain_generation','drain_cookie'},'incomplete window frame receipt')
    for key in ('source_generation','reader_generation','drain_generation','drain_cookie'):
        require(number(f[key],(1<<64)-1) and f[key]>0,'invalid window frame '+key)
    require(type(f['cookie']) is int and f['cookie']==1 and type(f['bytes']) is int and f['bytes']==3686400,'window frame extent/cookie differs')
    require(all(number(f[key],0xffffffff) for key in ('published_crc32','read_crc32')) and f['published_crc32']==f['read_crc32'],'window CRC differs')
    require(type(f['frame_sha256']) is str and re.fullmatch('[0-9a-f]{64}',f['frame_sha256']) and int(f['frame_sha256'],16)>0,'invalid window frame hash')
    require(r['frames']>0 and f['reader_generation']==f['drain_generation']==f['drain_cookie']==r['frames']+1,'window frame lacks matching local drain')
    require(type(s) is dict and set(s)=={'generation','active','prepared'} and number(s['generation'],(1<<64)-1) and s['generation']>0 and type(s['active']) is type(s['prepared']) is bool,'invalid cached SD receipt')
    closed=r.get('last_return')
    if closed is not None:
        require(type(closed) is dict and set(closed)=={'source_generation','closed_generation','frames','reset','masters_contained'},'invalid window return receipt')
        require(all(number(closed[k],(1<<64)-1) and closed[k]>0 for k in ('source_generation','closed_generation','frames')) and
                closed['closed_generation']==closed['source_generation']+3 and type(closed['reset']) is int and closed['reset']==15 and closed['masters_contained'] is True,'window return lacks closed ownership')
    if r['state']=='running':
        require(s==dict(generation=f['source_generation'],active=False,prepared=False),'running window lost SD containment')
        if closed:require(closed['closed_generation']+3==f['source_generation'],'running window borrowed a stale return')
    if r['state'] in ('ready','parked'):
        require(closed is not None and closed['frames']==r['frames'] and closed['source_generation']==f['source_generation'],'inactive window lost its frame return')
        require(s==dict(generation=closed['closed_generation']+2 if r['state']=='ready' else f['source_generation'],active=r['state']=='ready',prepared=r['state']=='ready'),'window SD return differs')
    return r,prefix


class _Rows:
    def __init__(self,data):
        self.lines=[line.decode('utf-8').rstrip('\r') for line in data.split(b'\n')[:-1] if line.strip() not in (b'',b'NOTICE:')]
        self.index=0;self.records=[]
    def take(self,kind):
        require(self.index<len(self.lines),'missing window service '+kind)
        pattern=PATTERNS.get(kind) or dict(RULES)[kind]
        m=pattern.fullmatch(self.lines[self.index]);self.index+=1
        require(m is not None,'unexpected/reordered window service '+kind)
        v={k:value if k in TEXT else int(value,16 if k in HEX or kind=='reader_drain' and k=='status' else 10) for k,value in m.groupdict().items()}
        self.records.append(dict(stage=kind,values=v));return v
    def end(self):require(self.index==len(self.lines),'extra window firmware observations')


def _opening(data,owner,generation,memory,prepare):
    cursor=_Rows(data)
    if prepare:
        query=cursor.take('fpga_query');require(query in ({},{'command':4,'words':6}),'invalid readiness query')
        status=cursor.take('fpga_status')
        require(status['status']==0 and status['words']==6 and status['ready']==1 and status['state']==0 and status['pins']&(1<<31) and status['soft']&3==3 and not status['soft']&8 and
                all(number(status[k],0xffffffff) for k in ('state','version','pins','soft','error_location','error_details')),'window configuration is not ready')
        check_route(cursor.take,require,owner=owner,generation=generation,reset=15,memory=memory,with_gate=False)
    require(cursor.take('scope')==dict(phase='control',owner=owner,generation=generation+1,reset=13),'window control ownership differs')
    for _ in range(2):check_route(cursor.take,require,owner=owner,generation=generation+1,reset=13,memory=memory,with_gate=True)
    require(cursor.take('scope')==dict(phase='data',owner=owner,generation=generation+2,reset=5),'window data ownership differs')
    cursor.end();return cursor.records


def _configuration(data,configuration,owner,generation,previous,programmed):
    p=extract(data,configuration,generation=generation,previous=previous,rearm=previous!=0,require_activation=True,require_programmed=programmed,expected_owner=owner)
    require(p['complete'] and not p['errors'],'window configuration transfer rejected: '+str(p['errors']))
    require(p['activation'] is not None and (p['programmed'] is not None)==programmed,'missing or unexpected programmed/activation receipt')
    t=p['timing'];require(t is not None and 0<=t['interval_ticks']<20*400000000,'configuration exceeded20s or lacks timing')
    engines=[row['values'] for row in p['records'] if row['stage']=='configuration_engine']
    mailboxes=[row['values'] for row in p['records'] if row['stage']=='configuration_mailbox']
    require(len(engines)==1 and engines[0]['credits']==4 and engines[0]['block_bytes']==0xffff0 and
            {row['phase'] for row in mailboxes}=={1,2,3},'configuration queue/engine observations absent or changed')
    return p


def startup(data,build_id,owner_seed,*,identity,configuration,memory,contract,cpu_mapping,atf=None):
    checked_contract(identity,memory,contract)
    r,prefix=response(data)
    require(r['ok'] and r['op']=='status' and r['state']=='running' and r['generation']==1 and r['frames']==1 and 'last_return' not in r,'initial service state differs')
    require(r['package_id']==identity['package_id'] and r['last_frame']['source_generation']==4,'initial service identity differs')
    lines=prefix.splitlines(keepends=True)
    split=next((i for i,line in enumerate(lines) if PATTERNS['scope'].fullmatch(line.decode().rstrip('\r\n'))),None)
    require(split is not None,'initial window scope absent')
    cold=b''.join(lines[:split])
    boot=decode_native_configuration(cold,build_id,owner_seed,fpga_identity=identity,configuration=configuration,from_storage=True,
        reader_memory=memory,cpu_mapping=cpu_mapping,atf=atf,_window_prefix=True)
    require(boot['complete'] and not boot['errors'] and boot['last_checkpoint']=='ddr_firewall_end','initial boot/configuration prefix rejected: '+str(boot['errors']))
    hps=next(row['values'] for row in boot['records'] if row['stage']=='hps_context')
    require(int(r['owner'],16)==hps['owner'],'service owner differs from verified boot')
    _configuration(cold,configuration,hps['owner'],4,0,True)
    opened=_opening(b''.join(lines[split:]),hps['owner'],4,memory,False)
    return dict(reply=r,boot=boot,opening=opened,owner=hps['owner'],runtime_admission=False)


def transition(data,previous,op,*,identity,configuration,memory):
    r,prefix=response(data)
    require(r['ok'] and r['op']==op and r['owner']==previous['owner'] and r['package_id']==identity['package_id']==previous['package_id'],'window response failed or changed owner/identity/operation')
    old=previous['last_frame'];source=old['source_generation'];owner=int(r['owner'],16)
    result=dict(reply=r,records=[],runtime_admission=False)
    if op=='status':
        require(r==dict(previous,op='status') and quiet(prefix),'status changed cached evidence or touched firmware')
    elif op=='present':
        require(previous['state']=='running' and r['state']=='running' and r['generation']==previous['generation'] and r['frames']==previous['frames']+1,'presentation changed session/count')
        require(r['last_frame']['source_generation']==source and r['storage']==previous['storage'] and r.get('last_return')==previous.get('last_return'),'presentation changed ownership')
        require(r['last_frame']['frame_sha256']!=old['frame_sha256'] and r['last_frame']['read_crc32']!=old['read_crc32'] and quiet(prefix),'presentation is unchanged or contains unexpected firmware')
    elif op in ('return','park'):
        require(previous['state']=='running' and r['state']==('ready' if op=='return' else 'parked') and r['generation']==previous['generation']+1 and r['frames']==previous['frames'] and r['last_frame']==old,'return changed session/frame')
        require(r['last_return']==dict(source_generation=source,closed_generation=source+3,frames=previous['frames'],reset=15,masters_contained=True),'return closure differs')
        cursor=_Rows(prefix)
        require(cursor.take('scope')==dict(phase='close',owner=owner,generation=source+2,reset=5),'return scope differs')
        require(cursor.take('master')==dict(status=0,owner=owner,generation=source+2,reset=5,reader_reads=88,reader_polls=1,disable=0,probe_polls=2,cancel=0,complete=1),'owned master stop differs')
        reader=old['reader_generation'];cookie=old['drain_cookie']
        require(cursor.take('reader_drain')==dict(owner=owner,generation=source+2,reset=5,reader_generation=reader,request_generation=reader,cookie=cookie,ack_generation=reader,ack_cookie=cookie,status=4,pending=0),'firmware reader drain differs')
        require(cursor.take('closure')==dict(owner=owner,generation=source+3,reset=15,hardware_drained=8,data_remained_contained=0,producers_quiescent=1,fabric_acknowledged=0),'independent hardware closure differs')
        cursor.end();result['records']=cursor.records
    elif op=='launch':
        require(previous['state']=='ready' and r['state']=='running' and r['generation']==previous['generation']+1 and r['frames']==1 and r.get('last_return')==previous['last_return'],'launch borrowed a stale return')
        source=previous['last_return']['closed_generation']+3
        require(r['last_frame']['source_generation']==source,'launch lost fresh SD containment')
        p=_configuration(prefix,configuration,owner,source,previous['last_return']['source_generation'],False)
        require(not any(part.strip() not in (b'',b'NOTICE:') for part in p['parts'][:p['records'][-1]['line']]),'window opened before complete configuration')
        result['configuration']={k:v for k,v in p.items() if k!='parts'}
        result['records']=_opening(b'\n'.join(p['parts']),owner,source,memory,True)
    else:raise ValueError('unsupported qualified window operation')
    return result
