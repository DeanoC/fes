import sys,re,collections
ent=sys.argv[2]; c=collections.Counter()
for l in open(sys.argv[1]):
    t,loc,n=l.rstrip('\n').split('\t',2)
    if t not in ('FF','LCELL_COMB'): continue
    parts=n.split('|')
    if len(parts)<2 or not parts[-2].endswith(':'+ent) : continue
    s=re.sub(r'~.*','',parts[-1]); s=re.sub(r'\[.*','',s); s=re.sub(r'\d+$','#',s)
    c[(t,s)]+=1
for (t,s),k in c.most_common(int(sys.argv[3])): print(k,t,s)
