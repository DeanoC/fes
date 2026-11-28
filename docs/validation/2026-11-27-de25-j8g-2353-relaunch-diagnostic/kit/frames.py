#!/usr/bin/env python3
"""run3 frame metrics: mean luma, non-black fraction, change vs previous (fraction of 160x90 px differing by >24)."""
import subprocess, sys, json
def gray(p):
    return subprocess.run(['ffmpeg','-loglevel','error','-i',p,'-vf','scale=160:90,format=gray','-f','rawvideo','-'],capture_output=True,check=True).stdout
prev=None; out=[]
for p in sys.argv[1:]:
    g=gray(p); n=len(g)
    row={'frame':p.rsplit('/',1)[-1],'mean_luma':round(sum(g)/n,2),'nonblack':round(sum(1 for x in g if x>16)/n,4)}
    if prev is not None and len(prev)==n: row['changed_vs_prev']=round(sum(1 for a,b in zip(prev,g) if abs(a-b)>24)/n,4)
    prev=g; out.append(row); print(json.dumps(row))
