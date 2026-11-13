"""Verify retained continuation admission, separately from native activation."""
import importlib.util,re
from pathlib import Path
_spec=importlib.util.spec_from_file_location('retained_continuation_queue',Path(__file__).with_name('queue_observation.py'))
_queue=importlib.util.module_from_spec(_spec);_spec.loader.exec_module(_queue)
FIELDS='build owner epoch generation ready elapsed_us retained reuse activated'.split()
def require(ok,message):
 if not ok:raise ValueError(message)
def decode(raw,*,boot,build,configuration):
 require(type(raw) is bytes and 0<len(raw)<=1<<20 and raw.endswith(b'\n') and b'\0' not in raw,'complete bounded continuation capture required')
 lines=[re.sub(rb'^NOTICE:[ \t]+',b'',v) for v in raw.splitlines()]
 rows=[i for i,v in enumerate(lines) if b'FES_NANO_INPUT_CONTINUE' in v]
 require(len(rows)==1 and rows[0]==len(lines)-1,'one final continuation record required')
 terminal=lines[-1];require(len(terminal)<=512 and terminal.startswith(b'FES_NANO_INPUT_CONTINUE '),'bounded continuation marker required')
 pairs=[p.split('=',1) for p in terminal.decode('ascii').split()[1:]]
 require(all(len(p)==2 for p in pairs) and [p[0] for p in pairs]==FIELDS,'complete ordered continuation fields required')
 c={}
 for key,value in pairs:
  pattern='[0-9a-f]{16}' if key=='build' else '[0-9a-f]{1,16}' if key in ('owner','epoch') else '[0-9]{1,20}'
  require(re.fullmatch(pattern,value) is not None,'bounded continuation field required')
  c[key]=value if key=='build' else int(value,16 if key in ('owner','epoch') else 10)
  if key!='build':require(0<=c[key]<1<<64,'continuation integer overflow')
 q=_queue.decode(raw,boot=boot,build=build,configuration=configuration,_terminal=terminal)
 r=q['retirement_observation']['result'];errors=list(q['errors'])
 for key in ('build','owner','epoch','generation'):
  if c[key]!=r[key]:errors.append('continuation/retirement '+key+' differs')
 if (c['ready'],c['retained'],c['reuse'],c['activated'])!=(1,1,0,0):errors.append('continuation authority differs')
 if not r['elapsed_us']<=c['elapsed_us']<1000000:errors.append('continuation original retirement deadline differs')
 passed=q['passed'] and q['retirement_verified'] and not errors
 return dict(passed=passed,continuation_verified=passed,continuation=c,retirement=q,errors=errors,
  native_activation_verified=False,memory_reuse_authorized=False,ordinary_runtime_activation=False)
