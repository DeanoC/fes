#!/usr/bin/env python3
"""Run an immutable prior benchmark stimulus against this private candidate."""
import hashlib,json,os,shutil,subprocess,sys,time
from pathlib import Path
P=Path(__file__).resolve().parent
fixture=Path(sys.argv[1]).resolve()
run=P/sys.argv[2]
run.mkdir(exist_ok=False)
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
candidate_manifest=json.loads((P/'manifest.json').read_text())
fixture_manifest=json.loads((fixture/'manifest.json').read_text())
cpu=P/'build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC1/Vfes_i386_cpu'
assert sha(cpu)==candidate_manifest['binary_sha256']
for path,digest in candidate_manifest['sources'].items():assert sha(Path(path))==digest,path
assert sha(fixture/'rtl.bin')==fixture_manifest['files'][str(fixture/'rtl.bin')]
shutil.copy2(cpu,run/'Vfes_i386_cpu');shutil.copyfile(fixture/'rtl.bin',run/'input.bin')
(run/'Vfes_i386_cpu').chmod(0o555);(run/'input.bin').chmod(0o444)
env=dict(os.environ,FES_I386_MODE='governor',FES_I386_LATENCY='0',FES_I386_PROGRAM=str(run/'input.bin'),FES_I386_PRESET='1',FES_I386_FREQUENCY='0',FES_I386_TURBO='0',FES_I386_IRQ_PERIOD='0')
identity={'candidate_manifest':sha(P/'manifest.json'),'fixture_manifest':sha(fixture/'manifest.json'),'cpu':sha(run/'Vfes_i386_cpu'),'input':sha(run/'input.bin'),'runner':sha(Path(__file__)),'settings':{k:v for k,v in env.items() if k.startswith('FES_I386_')}}
(run/'before.json').write_text(json.dumps(identity,indent=2)+'\n')
with (run/'stdout').open('w') as out,(run/'stderr').open('w') as err:
 proc=subprocess.Popen([str(run/'Vfes_i386_cpu')],env=env,stdout=out,stderr=err); start=time.monotonic()
 while proc.poll() is None:
  elapsed=time.monotonic()-start
  if elapsed>180:proc.kill();proc.wait();raise SystemExit('candidate run timed out')
  print(f'{run.name} CPU heartbeat {elapsed:.0f}s',flush=True);time.sleep(10)
after={'cpu':sha(run/'Vfes_i386_cpu'),'input':sha(run/'input.bin'),'candidate_manifest':sha(P/'manifest.json'),'fixture_manifest':sha(fixture/'manifest.json')}
(run/'after.json').write_text(json.dumps(after,indent=2)+'\n')
assert all(after[k]==identity[k] for k in after)
text=(run/'stdout').read_text();marks={}
for line in text.splitlines():
 fields=line.split()
 if len(fields)==5 and fields[0]=='MARK' and int(fields[1],16)==0x80:
  k=int(fields[2]);assert k not in marks;marks[k]=tuple(map(int,fields[3:]))
errors=[line for line in text.splitlines() if line.startswith('MARK ') and int(line.split()[1],16)==0x8e]
assert proc.returncode==0 and not errors and 'halted=1' in text and set(marks)==set(range(2*len(fixture_manifest['cases']))),(proc.returncode,len(marks),errors)
result={c['name']:{'charged_per_iteration':(marks[2*i+1][1]-marks[2*i][1])/1000,'elapsed_cycles':marks[2*i+1][0]-marks[2*i][0]} for i,c in enumerate(fixture_manifest['cases'])}
(run/'prediction.json').write_text(json.dumps(result,indent=2)+'\n')
print(f'{run.name}: {len(result)} cases pass',flush=True)
