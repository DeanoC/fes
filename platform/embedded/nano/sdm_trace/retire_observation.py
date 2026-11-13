"""Require a single source-bound retirement after the full input receipt."""
import importlib.util,re
from pathlib import Path
_s=importlib.util.spec_from_file_location('bound_retirement_input',Path(__file__).with_name('input_observation.py'))
_input=importlib.util.module_from_spec(_s);_s.loader.exec_module(_input)
FIELDS='build owner epoch generation bytes sha384 status phase retired first_error writes producer consumer control abort elapsed_us bad_offset bad_value retained reuse activated'.split()
HEX={'owner','epoch','abort','bad_offset','bad_value'}
SIGNED={'status','first_error'}
def require(v,message):
 if not v:raise ValueError(message)
def decode(raw,*,boot,build,configuration,_terminal=None):
 require(type(raw) is bytes and 0<len(raw)<=1<<20 and raw.endswith(b'\n') and b'\0' not in raw,'complete bounded retirement capture required')
 parts=raw.splitlines(keepends=True);lines=[re.sub(rb'^NOTICE:[ \t]+',b'',v.rstrip(b'\r\n')) for v in parts]
 rows=[(i,v) for i,v in enumerate(lines) if b'FES_NANO_RETIRE_RESULT' in v];require(len(rows)==1,'one complete retirement result required')
 at,line=rows[0];require(at>=1 and lines[at-1].startswith(b'FES_NANO_INPUT_RESULT ') and at==len(lines)-2,'retirement must immediately follow source result before final Park')
 require(line.startswith(b'FES_NANO_RETIRE_RESULT '),'malformed retirement prefix')
 tokens=line.decode('ascii').split()[1:];pairs=[v.split('=',1) for v in tokens]
 require(all(len(p)==2 for p in pairs) and [p[0] for p in pairs]==FIELDS,'complete ordered retirement fields required')
 r={}
 for key,value in pairs:
  if key in ('build','sha384'):
   require(re.fullmatch('[0-9a-f]{'+str(16 if key=='build' else 96)+'}',value) is not None,'bounded retirement digest/build required');r[key]=value
  else:
   pattern='[0-9a-f]{1,16}' if key in HEX else '-?[0-9]{1,11}' if key in SIGNED else '[0-9]{1,20}'
   require(re.fullmatch(pattern,value) is not None,'bounded retirement field '+key);r[key]=int(value,16 if key in HEX else 10)
   require(-(1<<31)<=r[key]<(1<<64),'retirement integer overflow')
 source=_input.decode(b''.join(parts[:at]+parts[at+1:]),boot=boot,build=build,configuration=configuration,_terminal=_terminal)
 errors=list(source['errors']);s=source['result']
 for k in ('build','owner','epoch','generation','bytes','sha384'):
  if r[k]!=s[k]:errors.append('retirement/source '+k+' differs')
 expected=dict(status=0,phase=6,retired=1,first_error=0,writes=5,producer=12,consumer=12,control=0,abort=0x100000,bad_offset=0,bad_value=0,retained=1,reuse=0,activated=0)
 if any(r[k]!=v for k,v in expected.items()):errors.append('retirement control/result not qualified')
 if not 0<r['elapsed_us']<1000000:errors.append('retirement stage deadline differs')
 passed=source['passed'] and not errors
 return dict(passed=passed,errors=errors,source_flow_verified=source['source_flow_verified'],retirement_verified=passed,
  input_observation=source,result=r,recorded_DATA_requests=source.get('recorded_DATA_requests',0),recorded_DATA_bytes=source.get('recorded_DATA_bytes',0),
  ordinary_runtime_activation=False,buffer_reuse_authority=False,DMA_authority=False,independent_interconnect_drain=False)
