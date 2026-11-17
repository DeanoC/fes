#!/usr/bin/env python3
import hashlib,json,os,shutil,subprocess
from pathlib import Path
p=Path(__file__).resolve().parent; w=p/'run'; d=w/'old-off';d.mkdir(exist_ok=False)
root=Path('/home/deano/.codex/worktrees/d31c/fes/out/dev/1902-wide-zero-v11-off');manifest=root/'manifest.json';m=json.loads(manifest.read_text());exe=next((root/'build').rglob('Vfes_i386_cpu'));sha=lambda f:hashlib.sha256(f.read_bytes()).hexdigest()
assert m['parameters']['GOV_REFERENCE_DIAGNOSTIC']==0 and sha(exe)==m['binary_sha256']
for f,h in m['sources'].items():assert Path(f).is_file() and sha(Path(f))==h
binary=d/'Vfes_i386_cpu';shutil.copyfile(exe,binary);binary.chmod(0o555);shutil.copyfile(manifest,d/'producer.json');(d/'producer.json').chmod(0o444)
prog=w/'stackwidth.bin';before={str(f):sha(f) for f in (binary,d/'producer.json',prog)}
env=dict(os.environ,FES_I386_MODE='governor',FES_I386_PROGRAM=str(prog),FES_I386_PRESET='1',FES_I386_FREQUENCY='0',FES_I386_TURBO='0',FES_I386_IRQ_PERIOD='0',FES_I386_LATENCY='0',FES_I386_CYCLE_LIMIT='10000000')
r=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=180);(d/'stdout.log').write_text(r.stdout);(d/'stderr.log').write_text(r.stderr);assert before=={str(f):sha(f) for f in (binary,d/'producer.json',prog)}
assert r.returncode==0 and 'MARK 8f 0 ' in r.stdout and 'halted=1' in r.stdout
other=(w/'off/stdout.log').read_text(); lines=lambda s:[x for x in s.splitlines() if x.startswith(('MARK ','STATE ','GOVERNOR '))]
a,b=lines(r.stdout),lines(other);(d/'comparison.json').write_text(json.dumps({'inputs':before,'old':a,'new':b,'equal':a==b},indent=2)+'\n');assert a==b,'default-off execution differed';print('default-off old vs new: all marks/cycles/charges, final architectural state and governor summary identical')
