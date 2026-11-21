import json,sys
def flat(o,p=''):
    if isinstance(o,dict):
        for k,v in o.items(): yield from flat(v,f'{p}/{k}')
    elif isinstance(o,list):
        for i,v in enumerate(o): yield from flat(v,f'{p}[{i}]')
    else: yield p,o
a=dict(flat(json.load(open(sys.argv[1])))); b=dict(flat(json.load(open(sys.argv[2]))))
diff=[k for k in sorted(set(a)|set(b)) if a.get(k)!=b.get(k)]
import collections
c=collections.Counter(k.split('/')[1].split('[')[0] for k in diff)
print(len(a),len(b),len(diff),dict(c))
for k in diff[:int(sys.argv[3]) if len(sys.argv)>3 else 15]: print(k, str(a.get(k))[:70], '|', str(b.get(k))[:70])
