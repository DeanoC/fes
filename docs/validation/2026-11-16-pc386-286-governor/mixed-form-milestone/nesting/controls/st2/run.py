#!/usr/bin/env python3
import hashlib,json,os,shutil,subprocess
from pathlib import Path
p=Path(__file__).resolve().parent;root=p.parent;sha=lambda q:hashlib.sha256(q.read_bytes()).hexdigest()
cases=json.loads((p/'cases.json').read_text());allresults={}
for ver in ('v3','v5'):
 src=root/f'1902-mixed-candidate-{ver}';manifest=src/'manifest.json';m=json.loads(manifest.read_text());exe=src/'frozen-candidate/Vfes_i386_cpu';assert sha(exe)==m['binary_sha256']
 for f,h in m['sources'].items():assert Path(f).is_file() and sha(Path(f))==h
 d=p/ver;d.mkdir(exist_ok=False);paths=[]
 for path,name in ((exe,'Vfes_i386_cpu'),(manifest,'producer.json'),(p/'controls.bin','controls.bin'),(p/'controls.asm','controls.asm'),(p/'cases.json','cases.json')):
  q=d/name;shutil.copyfile(path,q);q.chmod(0o555 if name=='Vfes_i386_cpu' else 0o444);paths.append(q)
 before={str(x):sha(x) for x in paths};env=dict(os.environ,FES_I386_MODE='governor',FES_I386_PROGRAM=str(d/'controls.bin'),FES_I386_PRESET='1',FES_I386_FREQUENCY='0',FES_I386_TURBO='0',FES_I386_LATENCY='0',FES_I386_IRQ_PERIOD='0',FES_I386_CYCLE_LIMIT='10000000')
 r=subprocess.run([str(d/'Vfes_i386_cpu')],env=env,text=True,capture_output=True,timeout=180);(d/'stdout.log').write_text(r.stdout);(d/'stderr.log').write_text(r.stderr);assert before=={str(x):sha(x) for x in paths}
 marks={};end=[];state=[];summary=[]
 for line in r.stdout.splitlines():
  x=line.split()
  if len(x)==5 and x[0]=='MARK':
   if x[1]=='80': k=int(x[2]);assert k not in marks;marks[k]=(int(x[3]),int(x[4]))
   if x[1]=='8f':end.append(int(x[2]))
  if line.startswith('STATE '):state.append(line)
  if line.startswith('{'):summary.append(json.loads(line))
 assert r.returncode==0 and end==[0] and len(summary)==1 and not summary[0]['failed'] and 'halted=1' in r.stdout and set(marks)==set(range(2*len(cases))) and len(state)==1, f'{ver}: guest control failed, see stdout'
 charge={c[0]:marks[2*i+1][1]-marks[2*i][1] for i,c in enumerate(cases)}
 allresults[ver]={'hashes':before,'state':state[0],'charge':charge,'marks':marks}
assert allresults['v3']['state']==allresults['v5']['state']
for name,seg,bp,sp,prefix,eligible in cases:
 a=allresults['v3']['charge'][name];b=allresults['v5']['charge'][name]
 if eligible:assert a!=b, f'{name}: expected eligible change'
 else:assert a==b, f'{name}: unsupported context changed {a} -> {b}'
(p/'results.json').write_text(json.dumps(allresults,indent=2)+'\n');print(json.dumps({k:v['charge'] for k,v in allresults.items()},indent=2))
