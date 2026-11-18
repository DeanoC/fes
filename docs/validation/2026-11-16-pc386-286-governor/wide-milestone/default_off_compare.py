from pathlib import Path
import hashlib,json,os,subprocess
p=Path('/home/deano/.codex/worktrees/d31c/fes/out/dev'); out=p/'1902-wide-zero-v11/default-off';out.mkdir(exist_ok=True)
programs={'forms':p/'1902-wide-holdout/rtl.bin','zero':p/'1902-zero-wide-holdout/rtl.bin','memory_gate':p/'1902-wide-zero-v11/memory-gate/probe.bin'};results={}
for program,prog in programs.items():
 for preset,freq,turbo in [(0,0,0),(1,0,0),(3,0,0),(1,0,1)]:
  case=f'{program}-{preset}-{freq}-{turbo}';results[case]={}
  for side,b in [('before',p/'1902-memory-off-final/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC0/Vfes_i386_cpu'),('after',p/'1902-wide-zero-v11-off/build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC0/Vfes_i386_cpu')]:
   env={**os.environ,'FES_I386_MODE':'governor','FES_I386_LATENCY':'0','FES_I386_PROGRAM':str(prog),'FES_I386_PRESET':str(preset),'FES_I386_FREQUENCY':str(freq),'FES_I386_TURBO':str(turbo),'FES_I386_IRQ_PERIOD':'0'}
   r=subprocess.run([str(b)],env=env,capture_output=True,text=True,timeout=120)
   if r.returncode or 'halted=1' not in r.stdout:raise ValueError(f'{case} {side} failed: {r.returncode} {r.stderr}')
   (out/f'{case}-{side}.log').write_text(r.stdout+'\nSTDERR\n'+r.stderr);results[case][side]=hashlib.sha256(r.stdout.encode()).hexdigest()
  results[case]['identical']=results[case]['before']==results[case]['after']
  if not results[case]['identical']:raise ValueError(f'{case} differs')
(out/'comparison.json').write_text(json.dumps({'programs':{k:hashlib.sha256(v.read_bytes()).hexdigest() for k,v in programs.items()},'results':results},indent=2)+'\n');print('12 default-off CPU mode/program pairs identical')
