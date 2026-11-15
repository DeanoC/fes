exec(open('sqrt2.py').read().split('def test(')[0])
import math,random
rnd=random.Random(11); sc=[]
for p in (0,1):
    sc+=[(1<<63,p),(M64,p),((1<<63)|1,p),(M64-1,p),(0xC000000000000000,p)]
    for i in range(128):
        b=(1<<63)|(i<<56); sc+=[(b,p),(b|((1<<56)-1),p)]
    for _ in range(1500):
        r=rnd.getrandbits(33)|(1<<32); v=r*r; v>>=max(0,v.bit_length()-64)
        for d in(-1,0,1): sc.append((((v|(1<<63))+d)&M64,p))
    sc+=[(rnd.getrandbits(64)|(1<<63),p) for _ in range(15000)]

# drop = low bits of the 66-bit target the PC does not need exact
PCD={"PC 24":41,"PC 53":12,"PC 64":0}
print(f"FSQRT, {len(sc)} cases\n{'precision':9} {'iters':>5} {'muls':>5} {'steps k':>8} {'rem bits':>9} {'exact':>6}")
for name,drop in PCD.items():
    chosen=None
    for it in (1,2,3,4):
        hi=-1<<60; lo=1<<60; al={}
        for A,p in sc:
            q,nm=hw_sqrt(A,p,it); s=67+p-drop
            T=math.isqrt((A<<(67+p))>>(2*drop)) if False else None
            # target at the PC position: T = isqrt(A<<(67+p)) >> drop, exactly
            Tf=math.isqrt(A<<(67+p))>>drop
            qa=(q.s<<(q.e+2))>>drop if q.e+2>=0 else (q.s>>(-(q.e+2)))>>drop
            al[(A,p)]=(qa,nm); d=Tf-qa; hi=max(hi,d); lo=min(lo,d)
        E=max(abs(hi),abs(lo))+1; k=max(1,(2*E-1).bit_length())
        bad=0; mr=0; nm=0
        for A,p in sc:
            qa,nm=al[(A,p)]
            # work at the PC's scale: C is the sqrt truncated at `drop`
            s=67+p
            C=max(0,qa-E)
            # exact remainder at this scale: A<<s - (C<<drop)^2, bounded
            full=C<<drop
            rem=(A<<s)-full*full
            mr=max(mr,rem.bit_length())
            if rem<0: bad+=1; continue
            for i in range(k-1,-1,-1):
                step=(1<<(i+drop))
                t=rem-((full<<1)*step+step*step)
                if t>=0: rem=t; full+=step; C+=(1<<i)
            Tf=math.isqrt(A<<s)>>drop
            nzf=(A<<s)-(Tf<<drop)**2!=0
            if C!=Tf or (rem!=0)!=nzf: bad+=1
        print(f"   {name} it={it} muls={nm} k={k} rem={mr} {'ok' if bad==0 else 'BAD'}")
        if bad==0 and chosen is None:
            chosen=(it,nm,k,mr)
    if chosen: print(f"{name:9} {chosen[0]:5} {chosen[1]:5} {chosen[2]:8} {chosen[3]:9} {'yes':>6}")
    else: print(f"{name:9} {'-':>5} {'-':>5} {'-':>8} {'-':>9} {'NO':>6}")
