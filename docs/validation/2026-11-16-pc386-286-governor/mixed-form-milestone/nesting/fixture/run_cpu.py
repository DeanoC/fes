#!/usr/bin/env python3
import hashlib,json,os,subprocess
from pathlib import Path
p=Path(__file__).resolve().parent;f=p/'frozen-cpu';sha=lambda q:hashlib.sha256(q.read_bytes()).hexdigest();m=json.loads((f/'producer.json').read_text());exe=f/'Vfes_i386_cpu';prog=f/'rtl.bin';cases=json.loads((f/'cases.json').read_text());assert sha(exe)==m['binary_sha256'] and m['parameters']['GOV_REFERENCE_DIAGNOSTIC']==1
for path,h in m['sources'].items():assert Path(path).is_file() and sha(Path(path))==h
inputs=[exe,prog,f/'producer.json',f/'rtl.asm',f/'cases.json'];before={str(x):sha(x) for x in inputs};env=dict(os.environ,FES_I386_MODE='governor',FES_I386_LATENCY='0',FES_I386_PROGRAM=str(prog),FES_I386_PRESET='1',FES_I386_FREQUENCY='0',FES_I386_TURBO='0',FES_I386_IRQ_PERIOD='0',FES_I386_CYCLE_LIMIT='20000000')
r=subprocess.run([str(exe)],env=env,capture_output=True,text=True,timeout=180);(f/'cpu.log').write_text(r.stdout+'\nSTDERR\n'+r.stderr);after={str(x):sha(x) for x in inputs};assert before==after
marks={};end=[];s=[]
for l in r.stdout.splitlines():
 v=l.split()
 if len(v)==5 and v[0]=='MARK':
  if v[1]=='80':k=int(v[2]);assert k not in marks;marks[k]=(int(v[3]),int(v[4]))
  if v[1]=='8f':end.append(int(v[2]))
 if l.startswith('{'):s.append(json.loads(l))
assert r.returncode==0 and end==[0] and len(s)==1 and not s[0]['failed'] and 'halted=1' in r.stdout and set(marks)==set(range(2*len(cases)))
pred={case[0]:{'charged_per_loop':(marks[2*i+1][1]-marks[2*i][1])/1000,'cycles':marks[2*i+1][0]-marks[2*i][0]} for i,case in enumerate(cases)}
(f/'predictions.json').write_text(json.dumps(pred,indent=2)+'\n');(f/'run-manifest.json').write_text(json.dumps({'before':before,'after':after,'cases':len(cases),'env':{k:v for k,v in env.items() if k.startswith('FES_I386_')},'exit':r.returncode},indent=2)+'\n');print(json.dumps(pred,indent=2),flush=True)
