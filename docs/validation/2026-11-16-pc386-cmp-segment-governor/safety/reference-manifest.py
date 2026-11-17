#!/usr/bin/env python3
import hashlib,json,os,sys
from pathlib import Path
p=Path(__file__).resolve().parent; q=p/'reference-vm'; paths=[]
for d in ('app','roms'):
 paths.extend(x for x in (q/d).rglob('*') if x.is_file() or x.is_symlink())
for n in ('refbox.py','alsoft.conf','box.sh','vm/86box.cfg','vm/nvr/award286.nvr','readback-SURVEY.COM','readback-FAILCASE.COM','disk-identities.sha256','target-check.log','survey-write.log','failcase-write.log','result-absence.log'):
 paths.append(q/n)
for n in ('generate.py','reference.asm','SURVEY.COM','FAILCASE.COM','reference-failure.asm','rtl.asm','rtl.bin','rtl-failure.asm','rtl-failure.bin','manifest.json','frozen-inputs.sha256','frozen-prediction.json','frozen-prediction-identity.json','commands-frozen.jsonl','run-reference.sh','safe-disk-copy.py','safeguard-tests.json','audit-speed-graphics.py','reference-manifest.py'):
 paths.append(p/n)
r={}
for f in sorted(paths):
 item={'mode':oct(f.lstat().st_mode & 0o777)}
 if f.is_symlink():
  item['symlink']=os.readlink(f)
  if f.is_file():item['target_sha256']=hashlib.sha256(f.read_bytes()).hexdigest()
 else:item['sha256']=hashlib.sha256(f.read_bytes()).hexdigest()
 r[str(f.relative_to(p))]=item
out=q/sys.argv[1]; out.write_text(json.dumps(r,indent=2,sort_keys=True)+'\n'); print(len(r),'entries',hashlib.sha256(out.read_bytes()).hexdigest())
