import re,sys,collections
def parse(p):
    out=collections.Counter(); cur=None; w=None
    for line in open(p):
        line=line.strip()
        if line.startswith('cell '): cur=line.split()[2]; w=None
        elif line.startswith('parameter \\WIDTH'): w=int(line.split()[-1].split("'")[-1],2) if "'" in line.split()[-1] else int(line.split()[-1])
        elif line.startswith('connect \\Q '):
            m=re.search(r'# \\Q (.*)$',line); name=m.group(1) if m else line.split()[2]
            name=re.sub(r' \[.*','',name)
            if name.startswith('\\_'): name='<anon>'
            out[name]+=w
    return out
a,b=parse(sys.argv[1]),parse(sys.argv[2])
for k in sorted(set(a)|set(b)):
    if a[k]!=b[k]: print(k,a[k],b[k])
print('total',sum(a.values()),sum(b.values()))
