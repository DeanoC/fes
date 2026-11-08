import sys, collections
N=480*270
path=sys.argv[1]; f0=int(sys.argv[2]); f1=int(sys.argv[3])
counts=[]
with open(path,'rb') as f:
    f.seek(f0*N); prev=None
    for i in range(f0,f1):
        fr=f.read(N)[:480*216][::5]   # playfield only (status bar excluded)
        counts.append(0 if prev is None else sum(1 for a,b in zip(fr,prev) if abs(a-b)>4))
        prev=fr
print('changed-pixel percentiles', sorted(counts)[len(counts)//2], sorted(counts)[int(len(counts)*.9)], max(counts))
open(sys.argv[4],'w').write('\n'.join(map(str,counts)))
