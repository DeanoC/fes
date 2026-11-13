"""Observe one source-qualified full-input terminal, never activation authority."""
import re,importlib.util
from pathlib import Path
_spec=importlib.util.spec_from_file_location("full_input_bound_remap",Path(__file__).with_name("remap_trace.py"));_remap=importlib.util.module_from_spec(_spec);_spec.loader.exec_module(_remap)
PARK=b'FES_NANO_INPUT_PARK retained=1 resume=0 activated=0'
FIELDS=('build status detail proof owner epoch generation bytes accepted posted acknowledged completed posts acks buffers pending outstanding configured failed first_error service_error source_failed scope_reason bad_offset bad_value arm_writes elapsed_us sha384 retained activated').split()
HEX={'owner','epoch','bad_offset','bad_value'}
SIGNED={'first_error','service_error'}
TRANSFER=re.compile(r'FES_NANO_CORE_TRANSFER phase=(arm|start|write|poll|status|complete|FAIL) owner=([0-9a-f]+) generation=([0-9]+) completed=([0-9]+) pending=([0-9]+) total=([0-9]+) base=94800000 counter=([0-9a-f]+) hash_ticks=([0-9a-f]+)')
ENGINE=re.compile(r'FES_NANO_CORE_ENGINE credits=([0-9]+) block_bytes=([0-9]+) next_job=0 owner=([0-9a-f]+) generation=4')
START=re.compile(r'FES_NANO_START phase=([0-9]+) status=(-?[0-9]+)')
PHASES=(0,1,2,3,4,12,13,14,5,6,7,8,9,10,11)
def require(ok,message):
 if not ok:raise ValueError(message)
def decode(raw,*,boot,build,configuration):
 require(type(raw) is bytes and 0<len(raw)<=1<<20 and raw.endswith(b'\n') and b'\0' not in raw,'complete bounded input capture required')
 require(boot.get('loader_ready') is True and boot.get('errors')==[],'independently checked native boot required')
 cap=boot.get('capability_observation') or {};require(cap.get('status')==0 and cap.get('reads')==26,'complete boot capability observation required')
 owner,epoch=cap.get('owner'),cap.get('epoch');require(all(type(v) is int and 0<v<1<<64 for v in (owner,epoch)),'checked owner/epoch required')
 require(type(configuration) is dict and type(configuration.get('core_size')) is int and type(configuration.get('chunk_bytes')) is int and re.fullmatch('[0-9a-f]{96}',configuration.get('core_sha384','')) is not None,'checked complete MENU configuration required')
 total,chunk=configuration['core_size'],configuration['chunk_bytes'];require(64<total<=0x7ff000 and total%4==0 and 64<=chunk<=0xffff0 and chunk%64==0,'bounded exact source layout required')
 lines=[re.sub(rb'^NOTICE:[ \t]+',b'',v) for v in raw.splitlines()]
 require(lines[-1]==PARK and sum(v==PARK for v in lines)==1,'one last terminal input Park required')
 forbidden=(b'FES_NATIVE_PROGRAM_REPLY ',b'FES_NATIVE_FPGA_PROGRAMMED ',b'FES_NANO_FABRIC_ACTIVATION ',b'FES_NATIVE_INPUT_ACTION ',b'FES_NANO_FAULT64 ')
 require(not any(any(t in v for t in forbidden) for v in lines),'unexpected application/activation/other diagnostic')
 rows=[(i,v) for i,v in enumerate(lines) if b'FES_NANO_INPUT_RESULT' in v];require(len(rows)==1,'one terminal input result required')
 at,line=rows[0];text=line.decode('ascii');require(text.startswith('FES_NANO_INPUT_RESULT '),'malformed result prefix')
 tokens=text.split()[1:];require(len(tokens)==len(FIELDS),'complete result field count required');pairs=[v.split('=',1) for v in tokens];require(all(len(v)==2 for v in pairs) and [v[0] for v in pairs]==FIELDS,'exact ordered result fields required')
 r={}
 for key,value in pairs:
  if key=='build':require(re.fullmatch('[0-9a-f]{16}',value) is not None,'bounded build tag required');r[key]=value
  elif key=='sha384':require(re.fullmatch('[0-9a-f]{96}',value) is not None,'full source digest required');r[key]=value
  else:
   pattern='[0-9a-f]{1,16}' if key in HEX else '-?[0-9]{1,11}' if key in SIGNED else '[0-9]{1,20}'
   require(re.fullmatch(pattern,value) is not None,'bounded numeric field '+key);r[key]=int(value,16 if key in HEX else 10)
   require(-(1<<31)<=r[key]<(1<<64),'numeric field overflow')
 errors=[]
 def check(v,message):
  if not v:errors.append(message)
 check(r['build']==build,'source-bound build differs');check((r['owner'],r['epoch'],r['generation'])==(owner,epoch,4),'boot owner/epoch/generation differs')
 check(all(r[k]==v for k,v in dict(status=0,detail=0,proof=1,configured=1,failed=0,first_error=0,service_error=0,source_failed=0,scope_reason=0,bad_offset=0,bad_value=0,arm_writes=12,retained=1,activated=0,pending=0,outstanding=0).items()),'terminal firmware result not qualified')
 check(all(r[k]==total for k in ('bytes','accepted','posted','acknowledged','completed')),'exact full-byte prefixes differ')
 check(r['sha384']==configuration['core_sha384'],'source SHA384 differs');check(0<r['elapsed_us']<20000000,'transfer deadline differs')
 check(r['buffers']==(total+chunk-1)//chunk,'completed source buffer count differs')
 if errors:return dict(passed=False,result=r,errors=errors,source_flow_verified=False,DMA_authority=False,ordinary_runtime_activation=False,buffer_reuse_authority=False)
 starts=[];transfers=[];engines=[]
 for i,v in enumerate(lines[:at]):
  if b'FES_NANO_START' in v:
   m=START.fullmatch(v.decode('ascii'));require(m is not None,'malformed START record');starts.append((i,int(m[1]),int(m[2])))
  if b'FES_NANO_CORE_TRANSFER' in v:
   m=TRANSFER.fullmatch(v.decode('ascii'));require(m is not None,'malformed transfer record');transfers.append((i,m))
  if b'FES_NANO_CORE_ENGINE' in v:
   m=ENGINE.fullmatch(v.decode('ascii'));require(m is not None,'malformed engine record');engines.append((i,m))
 require(tuple(v[1] for v in starts)==PHASES and all(v[2] in ((2,3) if v[1]==7 else (0,)) for v in starts),'exact successful START sequence required')
 require(len(engines)==1,'one initial engine required');engine_pos,engine=engines[0];credits,block=int(engine[1]),int(engine[2]);require(0<credits<=14 and block>0 and block%4==0 and int(engine[3],16)==owner,'negotiated engine differs')
 _,remap=_remap.extract(raw,required=True)
 require(remap['vendor_operand_subtraction_matches'] and remap['submission_starts_at_transformed_pointer'] and remap['stream_identity_stable'],'vendor source pointer/remap trace differs')
 values=[v['values'] for v in remap['records']];request=values[4]
 require(request['address']==0x14800000 and request['bytes']==min(total,chunk,block) and request['bypass']==0 and request['ar']==request['aw']==0x80000000 and remap['mailbox_status']==0,'first negotiated DATA intent/result differs')
 require(all(v['bypass']==0 for v in values[:5]) and values[0]['ar']==values[0]['aw']==0 and all(v['ar']==v['aw']==0x80000000 for v in values[1:5]),'unexpected remap transition')
 expected_posts=sum((min(chunk,total-offset)+block-1)//block for offset in range(0,total,chunk))
 require(r['posts']==r['acks']==expected_posts,'exact submitted/acknowledged block count differs')
 require(len(transfers)>=5 and [m[1] for _,m in transfers[:2]]==['arm','start'] and transfers[-1][1][1]=='status','complete transfer phase sequence required')
 previous_completed=previous_submitted=0
 for i,m in transfers:
  phase=m[1];completed,pending=int(m[4]),int(m[5]);require(int(m[2],16)==owner and int(m[3])==4 and int(m[6])==total,'transfer source identity differs')
  require(previous_completed<=completed<=total and previous_submitted<=completed+pending<=total and pending<=chunk*4,'transfer byte prefixes differ')
  require((completed==total or completed%chunk==0) and (completed+pending==total or pending%chunk==0),'transfer chunk boundaries differ')
  require(phase not in ('complete','FAIL'),'outer completion/activation or failure in successful trace')
  if phase in ('arm','start'):require(completed==pending==0,'bytes before START')
  elif phase in ('write','poll'):require(pending>0,'missing pending transfer bytes')
  else:require(phase=='status' and completed==total and pending==0,'incomplete final status')
  previous_completed,previous_submitted=completed,completed+pending
 require(transfers[1][0]<starts[0][0]<starts[-1][0]<engine_pos<transfers[2][0] and transfers[-1][0]<at<len(lines)-1,'START/transfer/result order differs')
 require(not any(b'FES_NANO_' in v or b'FES_NATIVE_' in v for v in lines[at+1:-1]),'late native activity after terminal result')
 return dict(passed=True,result=r,errors=[],source_flow_verified=True,negotiated_credits=credits,negotiated_block_bytes=block,recorded_DATA_requests=r['posts'],recorded_DATA_bytes=total,matching_source_digest=r['sha384'],remap_observation=remap,DMA_authority=False,ordinary_runtime_activation=False,buffer_reuse_authority=False,physical_isolation_qualification=False)
