import sys,re
sys.path.insert(0,'.')
from ent import parse
def num(s):
    m=re.match(r'([\d.]+) \(([\d.]+)\)',s); return (float(m.group(1)),float(m.group(2))) if m else (float(s or 0),float(s or 0))
def load(p):
    _,rows=parse(p); d={}
    for r in rows:
        fh=r['Full Hierarchy Name']
        fh=re.sub(r'\|[^|:]*:',lambda m:'|'+m.group(0).split(':')[-1]+':' ,fh)
        a=num(r['ALMs needed [=A-B+C]']); g=num(r['Dedicated Logic Registers']); lut=num(r['Combinational ALUTs'])
        d[fh]=dict(alm=a[0],self=a[1],reg=g[0],rself=g[1],lut=lut[0],m10k=int(r['M10Ks'] or 0),dsp=r['DSP Blocks'],mem=num(r['ALMs used for memory'])[0],depth=r['depth'],ent=r['Entity Name'])
    return d
b=load(sys.argv[1]); p=load(sys.argv[2]); md=int(sys.argv[3]); filt=sys.argv[4] if len(sys.argv)>4 else ''
keys=list(p.keys())+[k for k in b if k not in p]
print('depth\tentity\tinstance\tALM_base\tALM_pg\tdALM\tReg_base\tReg_pg\tdReg\tLUT_pg\tM10K_pg\tMLABmem_pg')
for k in keys:
    x=p.get(k) or b[k]
    if x['depth']>md or filt not in k: continue
    B=b.get(k,dict(alm=0,reg=0)); P=p.get(k,dict(alm=0,reg=0,lut=0,m10k=0,mem=0))
    inst=k.split('|')[-1].split(':')[-1] if '|' in k else k
    print(x['depth'],x['ent'],inst,round(B['alm']),round(P['alm']),round(P['alm']-B['alm']),int(B['reg']),int(P['reg']),int(P['reg']-B['reg']),int(P['lut']),P['m10k'],round(P['mem'],1),sep='\t')
