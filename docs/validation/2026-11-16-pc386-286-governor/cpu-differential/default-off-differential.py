from pathlib import Path
import hashlib, json, subprocess, sys
sys.path.insert(0,str(Path('sources/misteross').resolve()))
from scripts import test_fes_i386_lockstep as lock, test_fes_i386_governor as bench, i386_verilator, fes_i386_oracle as oracle
p=Path('out/dev/lockstep-evidence').resolve(); root=Path('sources/misteross').resolve()
lock.OUTPUT=p/'default-off-build'
tool=i386_verilator.locate(Path('/home/deano/fes/out/cache/misteross-toolchains'))
binpath=lock.verilate(tool,oracle.link_inputs(4,False),4,None,{'GOV_N':5,'GOV_D':3072,'GOV_REFERENCE_DIAGNOSTIC':0})
program=p/'governor_bench.bin'
subprocess.run(['/home/deano/fes-scratch/u6ref/tools/nasm/nasm','-f','bin','-o',str(program),str(bench.PROGRAM)],check=True)
original=Path('/home/deano/.codex/worktrees/d31c/fes/out/dev/pc386-clean-main/build/rtl-GOV_D3072-GOV_N5/Vfes_i386_cpu')
sha=lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
print('candidate binary',sha(binpath),'main binary',sha(original),'program',sha(program),flush=True)
results={}
for preset,frequency,turbo in [(0,0,0),(1,0,0),(3,0,0),(1,0,1)]:
 key=f'{preset}-{frequency}-{turbo}'; print('running',key,flush=True)
 before=bench.run(original,program,preset,1000000,frequency,turbo)
 after=bench.run(binpath,program,preset,1000000,frequency,turbo)
 results[key]={'before':before,'after':after,'identical':before==after}
 print('result',key,'identical',before==after,flush=True)
(p/'default-off-differential.json').write_text(json.dumps({'main_sha256':sha(original),'candidate_sha256':sha(binpath),'program_sha256':sha(program),'parameters':{'GOV_N':5,'GOV_D':3072,'GOV_REFERENCE_DIAGNOSTIC':0},'irq_period':1000000,'results':results},indent=2)+'\n')
if not all(x['identical'] for x in results.values()): raise SystemExit(1)
