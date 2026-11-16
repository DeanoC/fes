from pathlib import Path
import hashlib,json,os,subprocess
p=Path('/home/deano/.codex/worktrees/d31c/fes/out/dev'); prog=p/'1902-memory-model-v3/modes/rtl.bin'; results={}
for preset,freq,turbo in [(0,0,0),(1,0,0),(3,0,0),(1,0,1)]:
    case=f'{preset}-{freq}-{turbo}'; results[case]={}
    for side in ('before','after'):
        build = '1902-memory-off-before' if side == 'before' else '1902-memory-off-final'
        binary=p/build/'build/rtl-GOV_D3072-GOV_N5-GOV_REFERENCE_DIAGNOSTIC0/Vfes_i386_cpu'
        env={**os.environ,'FES_I386_MODE':'governor','FES_I386_LATENCY':'0','FES_I386_PROGRAM':str(prog),'FES_I386_PRESET':str(preset),'FES_I386_FREQUENCY':str(freq),'FES_I386_TURBO':str(turbo),'FES_I386_IRQ_PERIOD':'0'}
        r=subprocess.run([str(binary)],env=env,capture_output=True,text=True,timeout=120)
        if r.returncode not in (0,1) or 'halted=1' not in r.stdout: raise ValueError(f'{case} {side} failed: {r.returncode} {r.stderr}')
        (p/f'1902-memory-model-v3/off-{case}-{side}.log').write_text(r.stdout+'\nSTDERR\n'+r.stderr)
        results[case][side]=hashlib.sha256(r.stdout.encode()).hexdigest()
    results[case]['identical']=results[case]['before']==results[case]['after']
    if not results[case]['identical']:raise ValueError(f'{case} differs')
(p/'1902-memory-model-v3/default-off-comparison.json').write_text(json.dumps({'program_sha256':hashlib.sha256(prog.read_bytes()).hexdigest(),'results':results},indent=2)+'\n')
print(json.dumps(results,indent=2))
