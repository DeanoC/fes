from pathlib import Path
import hashlib,json,sys
out=Path(__file__).resolve().parent;root=out/'sources/misteross';sys.path.insert(0,str(root))
from scripts import test_fes_i386_lockstep as lock, i386_verilator
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
lock.RTL=root/'cores/fes-common/rtl/i386';lock.SIM=root/'cores/fes-common/sim/i386';lock.OUTPUT=out/'build'
sources=[lock.RTL/name for name in lock.CPU_RTL]+[lock.SIM/'lockstep_tb.cpp'];before={str(p):sha(p) for p in sources}
linked=sorted((Path('/home/deano/.codex/worktrees/d31c/fes/out/dev/pc386-clean-main/linked')).iterdir())
if len(linked)!=5 or [p.name[0] for p in linked]!=list('01234'):raise SystemExit('link inputs invalid')
tool=i386_verilator.locate(Path('/home/deano/fes/out/cache/misteross-toolchains'))
binary=lock.verilate(tool,linked,4,None,{'GOV_N':5,'GOV_D':3072,'GOV_REFERENCE_DIAGNOSTIC':1})
if before!={str(p):sha(p) for p in sources}:raise SystemExit('source changed during build')
(out/'binary-manifest.json').write_text(json.dumps({'sources':before,'binary_sha256':sha(binary),'source_manifest_sha256':sha(out/'source-manifest.json'),'verilator':tool.record(),'linked':{str(p):sha(p) for p in linked},'parameters':{'GOV_N':5,'GOV_D':3072,'GOV_REFERENCE_DIAGNOSTIC':1}},indent=2)+'\n')
print('combined private candidate',binary,'sha256',sha(binary),flush=True)
