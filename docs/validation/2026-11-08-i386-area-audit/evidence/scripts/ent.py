import sys,re
def parse(p):
    L=open(p,errors='replace').read().split('\n')
    i=next(k for k,l in enumerate(L) if l.startswith('; Fitter Resource Utilization by Entity'))
    hdr=[c.strip() for c in L[i+2].split(';')[1:-1]]
    rows=[]
    for l in L[i+4:]:
        if l.startswith('+'): break
        c=l.split(';')[1:-1]
        if len(c)!=len(hdr): continue
        name=c[0]; depth=(len(name)-len(name.lstrip(' |')))//3
        d=dict(zip(hdr,[x.strip() for x in c]))
        d['depth']=depth; rows.append(d)
    return hdr,rows
if __name__=='__main__':
    hdr,rows=parse(sys.argv[1]); print(hdr)
    md=int(sys.argv[2]) if len(sys.argv)>2 else 3
    for r in rows:
        if r['depth']<=md: print('  '*r['depth']+r['Compilation Hierarchy Node'].lstrip(' |'), r['ALMs needed [=A-B+C]'], r['ALMs used for memory'], r['Dedicated Logic Registers'], r['M10Ks'], r['DSP Blocks'], r['Entity Name'],sep='\t')
