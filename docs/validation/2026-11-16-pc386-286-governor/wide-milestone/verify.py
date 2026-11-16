#!/usr/bin/env python3
"""Verify retained bytes, loop arithmetic, controls and prediction provenance.

This checks transcription and assembly, not the reference emulator's provenance
or hardware equivalence. Optionally verify final RTL with --source-root and
reassemble the source with --nasm.
"""
import argparse,gzip,hashlib,json,re,subprocess,tempfile
from pathlib import Path

HERE=Path(__file__).resolve().parent

def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def load(path): return json.loads((HERE/path).read_text())
def rows(group, prefix, control):
 data=[(HERE/group/f'{prefix}{i}.TXT').read_bytes() for i in range(1,4)]
 if len(set(data))!=1:raise SystemExit(f'{group}: reference repeats differ')
 text=data[0].decode('ascii')
 if control not in text.splitlines():raise SystemExit(f'{group}: missing {control}')
 return {k:int(v) for k,v in re.findall(r'^([A-Z_]+)=(\d+)\r?$',text,re.M)}
def charges(path,iterations=1000):
 marks={}
 for line in (HERE/path).read_text().splitlines():
  m=re.fullmatch(r'MARK 80 (\d+) (\d+) (\d+)',line)
  if m:
   k,_,c=map(int,m.groups())
   if k in marks:raise SystemExit(f'duplicate marker in {path}')
   marks[k]=c
 if len(marks)%2 or set(marks)!=set(range(len(marks))):raise SystemExit(f'incomplete markers in {path}')
 return [(marks[i+1]-marks[i])/iterations for i in range(0,len(marks),2)]
def clocks(n):return 12_500_000*64/(18.2065*1000*n)
def main():
 a=argparse.ArgumentParser(description=__doc__);a.add_argument('--nasm',type=Path);a.add_argument('--source-root',type=Path);a.add_argument('--check-patches',action='store_true');args=a.parse_args()
 for l in (HERE/'SHA256SUMS').read_text().splitlines():
  digest,name=l.split('  ',1)
  if sha(HERE/name)!=digest:raise SystemExit('hash mismatch: '+name)
 groups=[('training','WIDE','ODD_RAM_VERIFIED'),('forms-holdout','VERIFY','ODD_RAM_VERIFIED'),('zero-holdout','ZERO','ZERO_FLAGS_RAM_VERIFIED'),('zero-wide-holdout','ZW','ZERO_FLAGS_RAM_VERIFIED')]
 for group,prefix,control in groups:
  raw=rows(group,prefix,control);comp=load(group+'/comparison.json');pred=load(group+'/rtl.json');mark=charges(group+'/rtl.log')
  if list(raw)!=list(comp) or list(raw)!=list(pred) or len(mark)!=len(raw):raise SystemExit(group+': inconsistent rows')
  for (name,n),m in zip(raw.items(),mark):
   row=comp[name]; ref=row.get('reference',row.get('reference_whole_loop'))
   if row['blocks']!=n or abs(ref-clocks(n))>1e-8 or abs(pred[name]['charged']-m)>1e-8:raise SystemExit(group+': arithmetic mismatch '+name)
  if group!='training':
   fin=load(group+'/final.json');fm=charges(group+'/final.log');fc=load(group+'/final-comparison.json')
   if list(fin)!=list(raw) or len(fm)!=len(raw):raise SystemExit(group+': final rows differ')
   for (name,n),m in zip(raw.items(),fm):
    if abs(fin[name]['charged']-m)>1e-8 or abs(fc[name]['reference']-clocks(n))>1e-8 or abs(fc[name]['final']-m)>1e-8 or abs(fc[name]['delta']-(m-clocks(n)))>1e-8:raise SystemExit(group+': final mismatch '+name)
 strip=gzip.decompress((HERE/'zero-wide-holdout/speed-strips.gray.gz').read_bytes());g=load('zero-wide-holdout/speed-audit.json');n=g['frames'];unit=56*18;sample=(HERE/'zero-wide-holdout/speed-indicator.pgm').read_bytes().split(b'\n',3)[3]
 if g['video_geometry']['fps']!=4 or n!=1080 or len(strip)!=n*unit or len(sample)!=unit or any(strip[i*unit:(i+1)*unit]!=sample for i in range(n)):raise SystemExit('speed trace bitmap mismatch')
 for group,denom in [('memory-gate',100),('zero-mmio',1000)]:
  a=charges(group+'/before.log',denom);b=charges(group+'/after.log',denom);result=load(group+'/results.json')
  av=list(result['before'].values()) if isinstance(result['before'],dict) else result['before'];bv=list(result['after'].values()) if isinstance(result['after'],dict) else result['after']
  if a!=av or b!=bv:raise SystemExit(group+': marker mismatch')
 d=load('default-off/comparison.json')['results']
 if len(d)!=12:raise SystemExit('default-off: missing mode/program pairs')
 for case,r in d.items():
  for side in ('before','after'):
   text=(HERE/f'default-off/{case}-{side}.log').read_text().split('\nSTDERR\n',1)[0]
   if hashlib.sha256(text.encode()).hexdigest()!=r[side]:raise SystemExit(f'default-off output mismatch {case} {side}')
  if not r['identical'] or r['before']!=r['after']:raise SystemExit('default-off difference: '+case)
 pred=load('zero-wide-holdout/candidate-manifest.json');width=load('zero-wide-holdout/width-fix-manifest.json');final=load('final-candidate-manifest.json')
 if pred['binary_sha256']!=width['binary_sha256'] or any(x['parameters']['GOV_REFERENCE_DIAGNOSTIC']!=1 for x in (pred,width,final)):raise SystemExit('enabled candidate identity mismatch')
 if args.source_root:
  rtl=args.source_root/'sources/misteross/cores/fes-common/rtl/i386'
  for name in ('fes_i386_pkg.sv','fes_i386_qalign.sv','fes_i386_cpu.sv','fes_i386_governor.sv'):
   matches=[v for k,v in final['sources'].items() if k.endswith('/rtl/'+name)]
   if len(matches)!=1 or sha(rtl/name)!=matches[0]:raise SystemExit('final RTL mismatch: '+name)
 if args.check_patches:
  if not args.source_root:raise SystemExit('--check-patches requires --source-root')
  files=('fes_i386_pkg.sv','fes_i386_qalign.sv','fes_i386_cpu.sv','fes_i386_governor.sv')
  manifests=(('forms','forms-holdout/candidate-manifest.json'),('shift','forms-holdout/shift-manifest.json'),('zero','zero-holdout/candidate-manifest.json'),('pending','zero-wide-holdout/candidate-manifest.json'))
  for name,manifest in manifests:
   with tempfile.TemporaryDirectory(prefix='fes1902-patch-') as tmp:
    directory=Path(tmp)
    for file in files:
     src=f'22fdddd3704768946df40b6ff7333e9225486106:sources/misteross/cores/fes-common/rtl/i386/{file}'
     (directory/file).write_bytes(subprocess.check_output(['git','-C',str(args.source_root),'show',src]))
    subprocess.run(['patch','-s','-p1','-i',str(HERE/'source-patches'/f'{name}.patch')],cwd=directory,check=True)
    sources=load(manifest)['sources']
    for file in files:
     expected=[v for k,v in sources.items() if k.endswith('/rtl/'+file)]
     if len(expected)!=1 or sha(directory/file)!=expected[0]:raise SystemExit(f'patch source mismatch: {name} {file}')
 if args.nasm:
  expected={'training/reference-checked':'88c789f7207e7be6ef0d869983993cd6c5c7399580f071b956ed121873d7a384','training/rtl':'e9430bfdab2bcad35123817c4df25d5486578c33fcd0d849bd5133757d0302f0','forms-holdout/reference-checked':'53f47eea549d849a1b7a079b4788a5e71c5172faafabfbe9abb21751e808316b','forms-holdout/rtl':'28601707e72ec79458aa852eecd046ed2e4e17548231c423b0dcf051adb97721','zero-holdout/reference-checked':'8c523292fe255869b0657e6f7b539dfd53dd960c463b50208740e0667d713536','zero-holdout/rtl':'82cc9fd06a2ad9ab386d94bce6bd8e7caaa0d7caa1fafbc0143392eabb91a1cc','zero-wide-holdout/reference-checked':'6139dc3b3408bd276a361cba107c9cbf8d42ced8daf451e316f41c7af8b120e9','zero-wide-holdout/rtl':'b7c51e0bef6716dc17330230c7dc1c6d81395ba53bcc03a53367a2621cbe76e9','memory-gate/probe':'79177c0e6812f78a79a187c4aee0ebe4b704adac0208e7f413ff03d41cefa7bd','zero-mmio/probe':'eac65736f0a38708184763a8c0b12328d52801502de3644760a7583d546b3cf4'}
  with tempfile.TemporaryDirectory(prefix='fes1902-wide-') as temp:
   for name,digest in expected.items():
    output=Path(temp)/(name.replace('/','_')+'.bin');subprocess.run([str(args.nasm),'-f','bin','-o',str(output),str(HERE/(name+'.asm'))],check=True)
    if sha(output)!=digest:raise SystemExit('assembly mismatch: '+name)
 print('Retained hashes, controls, arithmetic, speed strips and optional source/assembly checks pass; reference provenance is not revalidated.')
if __name__=='__main__':main()
