# FSQRT modelled in the (significand, exponent) form the RTL actually uses:
# every value is a 64-bit significand with bit63 set and an exponent, every
# multiply is the top 64 bits of the 128-bit product, every subtract is the
# adder. That is faithful to the datapath AND easier to keep scaled right.
import random, math
M64=(1<<64)-1
RSQ=[0]*256
for p in range(2):
    for i in range(128):
        a=(1.0+(i+0.5)/128.0)*(2.0 if p else 1.0)     # radicand in [1,4)
        RSQ[p*128+i]=min(2047,int(1.0/math.sqrt(a)*1024.0+0.5))

class F:                       # value = sig * 2^(exp-63), bit63 of sig set
    __slots__=('s','e')
    def __init__(s_,sig,exp):
        if sig:
            n=sig.bit_length()
            if n>64: sig>>=(n-64); exp+=(n-64)
            elif n<64: sig<<=(64-n); exp-=(64-n)
        s_.s=sig&M64; s_.e=exp
    def mul(a,b):              # the shared multiplier: top 64 bits
        return F(((a.s*b.s)>>64), a.e+b.e+1)
    def val(a): return a.s*2.0**(a.e-63)

def sub(a,b):                  # a - b on the adder, truncated to 64 bits
    sh=a.e-b.e
    if sh>=64: return F(a.s,a.e)
    bs=b.s>>sh if sh>=0 else b.s
    as_=a.s if sh>=0 else a.s>>(-sh)
    e=a.e if sh>=0 else b.e
    d=as_-bs
    if d<=0: return F(0,0)
    return F(d,e)

def hw_sqrt(A,p,iters):
    """A in [2^63,2^64) with exponent parity p. Returns the 66-bit candidate
    scale and the Goldschmidt estimate aligned to it."""
    a=F(A, 0+p)                                      # radicand in [1,4)
    y=F(RSQ[p*128+((A>>56)&0x7f)], 53)               # y ~ 1/sqrt(a): value = v/1024
    nmul=0
    three=F(3<<62, 1)                                # exactly 3.0
    for _ in range(iters):
        ay2=a.mul(y).mul(y); nmul+=2
        t=sub(three,ay2)                             # 3 - a*y^2
        y=y.mul(t); nmul+=1
        y=F(y.s, y.e-1)                              # /2
    q=a.mul(y); nmul+=1                              # sqrt(a) = a * (1/sqrt(a))
    return q,nmul

def test(cases,iters):
    worst=-1<<60; worstlo=1<<60; al={}
    for A,p in cases:
        q,nmul=hw_sqrt(A,p,iters)
        s=67+p
        T=math.isqrt(A<<s)
        # place q at T's scale: q ~ sqrt(a) in [1,2), T has 66 bits
        qa=q.s<<(q.e+2) if q.e+2>=0 else q.s>>(-(q.e+2))   # value*2^65, using the exponent
        al[(A,p)]=(qa,s,nmul)
        worst=max(worst,T-qa); worstlo=min(worstlo,T-qa)
    E=max(abs(worst),abs(worstlo))+1
    k=max(1,(2*E-1).bit_length())
    bad=0; mr=0; nm=0
    for A,p in cases:
        qa,s,nm=al[(A,p)]
        C=max(0,qa-E)
        rem=(A<<s)-C*C
        mr=max(mr,rem.bit_length())
        if rem<0: bad+=1; continue
        for i in range(k-1,-1,-1):
            t=rem-((C<<(i+1))+(1<<(2*i)))            # (C+2^i)^2-C^2: shifts only
            if t>=0: rem=t; C+=(1<<i)
        T=math.isqrt(A<<s)
        if C!=T or (rem!=0)!=((A<<s)-T*T!=0): bad+=1
    return worst,E,k,mr,nm,bad

if __name__ == '__main__':
    rnd=random.Random(11); sc=[]
    for p in (0,1):
        sc+=[(1<<63,p),(M64,p),((1<<63)|1,p),(M64-1,p),(0xC000000000000000,p)]
        for i in range(128):
            b=(1<<63)|(i<<56); sc+=[(b,p),(b|((1<<56)-1),p)]
        # Exact squares AT THE RADICAND'S OWN SCALE, and their neighbours. An
        # earlier version squared a 33-bit root and shifted the product right
        # to 64 bits, which discards non-zero low bits, so the centre was
        # usually NOT an exact square and the +-1 neighbours were not
        # square-adjacent. Codex found it on PR #1889. Here A is built so that
        # A << (67+p) is exactly T*T, and that is asserted before the
        # neighbours are added.
        made = 0
        while made < 1500:
            T = rnd.getrandbits(65) | (1 << 65)
            sq = T * T
            sh = 67 + p
            if sq & ((1 << sh) - 1):
                continue                 # not an exact square at this scale
            A = sq >> sh
            if A.bit_length() != 64:
                continue
            assert math.isqrt(A << sh) ** 2 == (A << sh)
            for d in (-1, 0, 1):
                v = (A + d) & M64
                if v >> 63:
                    sc.append((v, p))
            made += 1
        sc+=[(rnd.getrandbits(64)|(1<<63),p) for _ in range(15000)]
    print(f"FSQRT: {len(sc)} cases\n{'iters':>5} {'muls':>5} {'max T-qhat':>12} {'steps k':>8} {'rem bits':>9} {'exact':>6}")
    for it in (1,2,3,4):
        w,E,k,mr,nm,bad=test(sc,it)
        print(f"{it:5} {nm:5} {w:12} {k:8} {mr:9} {'yes' if bad==0 else f'NO({bad})':>6}")
