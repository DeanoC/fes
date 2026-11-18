#!/usr/bin/env python3
"""Run only after clearance: NASM + frozen whole-CPU comparison."""
import argparse, hashlib, json, os, re, shutil, stat, subprocess
from pathlib import Path
p=Path(__file__).resolve().parent
sha=lambda q:hashlib.sha256(Path(q).read_bytes()).hexdigest()
ap=argparse.ArgumentParser();ap.add_argument('--baseline-bin',required=True);ap.add_argument('--baseline-manifest',required=True);ap.add_argument('--candidate-bin',required=True);ap.add_argument('--candidate-manifest',required=True);ap.add_argument('--off-bin');ap.add_argument('--off-manifest');args=ap.parse_args()
if bool(args.off_bin)!=bool(args.off_manifest):raise SystemExit('supply both --off-bin and --off-manifest')
runs=[('baseline',args.baseline_bin,args.baseline_manifest,1),('candidate',args.candidate_bin,args.candidate_manifest,1)]
if args.off_bin:runs.append(('off',args.off_bin,args.off_manifest,0))
work=p/'run';work.mkdir(exist_ok=False)
source=work/'stackwidth.asm';shutil.copyfile(p/'stackwidth.asm',source);source.chmod(0o444)
nasm='/home/deano/fes-scratch/u6ref/tools/nasm/nasm';binpath=work/'stackwidth.bin'
subprocess.run([nasm,'-f','bin','-o',str(binpath),'-l',str(work/'stackwidth.lst'),str(source)],check=True,timeout=20);binpath.chmod(0o444)
results={}
for name,exe,manifest,enabled in runs:
 m=json.loads(Path(manifest).read_text()); assert m['binary_sha256']==sha(exe);assert m['parameters']['GOV_REFERENCE_DIAGNOSTIC']==enabled
 for f,h in m['sources'].items():assert Path(f).is_file() and sha(f)==h, f
 frozen=work/name;frozen.mkdir();saved=frozen/'Vfes_i386_cpu';shutil.copyfile(exe,saved);saved.chmod(0o555)
 manifest_copy=frozen/'producer.json';shutil.copyfile(manifest,manifest_copy);manifest_copy.chmod(0o444)
 before={str(q.relative_to(work)):sha(q) for q in (source,binpath,saved,manifest_copy)}
 env=dict(os.environ,FES_I386_MODE='governor',FES_I386_PROGRAM=str(binpath),FES_I386_PRESET='1',FES_I386_FREQUENCY='0',FES_I386_TURBO='0',FES_I386_IRQ_PERIOD='0',FES_I386_LATENCY='0',FES_I386_CYCLE_LIMIT='10000000')
 r=subprocess.run([str(saved)],env=env,text=True,capture_output=True,timeout=180);(frozen/'stdout.log').write_text(r.stdout);(frozen/'stderr.log').write_text(r.stderr)
 after={str(q.relative_to(work)):sha(q) for q in (source,binpath,saved,manifest_copy)};assert before==after
 marks={};end=[];state=[];summary=[]
 for line in r.stdout.splitlines():
  fields=line.split()
  if len(fields)==5 and fields[0]=='MARK':
   if fields[1]=='80':
    k=int(fields[2]);assert k not in marks; marks[k]=(int(fields[3]),int(fields[4]))
   if fields[1]=='8f':end.append(int(fields[2]))
  if line.startswith('STATE '):state.append(line)
  if line.startswith('{'):summary.append(json.loads(line))
 assert not r.returncode and len(summary)==1 and summary[0]['failed']==0 and end==[0] and set(marks)==set(range(8)) and len(state)==1 and 'halted=1' in r.stdout, f'{name}: guest/bench failure; see log'
 charge={label:marks[b][1]-marks[a][1] for label,a,b in [('enter32',0,1),('enter16',2,3),('leave32',4,5),('leave16',6,7)]}
 results[name]={'frozen_sha256':before,'marks':marks,'charge':charge,'state':state[0],'exit':r.returncode}
 (frozen/'result.json').write_text(json.dumps(results[name],indent=2)+'\n')
assert results['baseline']['state']==results['candidate']['state'], 'architectural state mismatch'
if 'off' in results:assert results['baseline']['state']==results['off']['state'], 'default-off architectural state mismatch'
for case in ('enter32','leave32'):
 assert results['baseline']['charge'][case]==results['candidate']['charge'][case], f'{case}: 32-bit-stack fallback changed'
assert any(results['baseline']['charge'][case]!=results['candidate']['charge'][case] for case in ('enter16','leave16')), '16-bit controls did not activate measured model'
(work/'results.json').write_text(json.dumps(results,indent=2)+'\n');print(json.dumps({name:r['charge'] for name,r in results.items()},indent=2),flush=True)
