# FSQRT's per-precision cost, with the PC target scaled correctly.
#
# An earlier version of this model took the PC target as isqrt(A<<s) >> drop
# and measured the remainder against the FULL-width square, which inflated
# the remainder to 110 bits. The correct target is
#     T_pc = isqrt((A<<s) >> 2*drop)
# since isqrt(n) >> d is not isqrt(n >> 2d) in general, and the sticky bit is
# (remainder != 0) OR (the radicand bits shifted out != 0).
import math, random, sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from sqrt_exactness import F, hw_sqrt, M64        # the datapath model

PCD = {"PC 24": 41, "PC 53": 12, "PC 64": 0}

def cases():
    rnd = random.Random(11); sc = []
    for p in (0, 1):
        sc += [(1 << 63, p), (M64, p), ((1 << 63) | 1, p), (M64 - 1, p),
               (0xC000000000000000, p)]
        for i in range(128):
            b = (1 << 63) | (i << 56)
            sc += [(b, p), (b | ((1 << 56) - 1), p)]
        for _ in range(1500):
            r = rnd.getrandbits(33) | (1 << 32); v = r * r
            v >>= max(0, v.bit_length() - 64)
            for d in (-1, 0, 1): sc.append((((v | (1 << 63)) + d) & M64, p))
        sc += [(rnd.getrandbits(64) | (1 << 63), p) for _ in range(15000)]
    return sc

sc = cases()
print(f"FSQRT, {len(sc)} cases, PC target scaled as isqrt(radicand >> 2*drop)\n")
print(f"{'precision':9} {'iters':>5} {'muls':>5} {'steps k':>8} {'rem bits':>9} {'exact':>6}")
for name, drop in PCD.items():
    for it in (1, 2, 3, 4):
        hi = -1 << 60; lo = 1 << 60; al = {}
        for A, p in sc:
            q, nm = hw_sqrt(A, p, it)
            s = 67 + p
            rad = (A << s) >> (2 * drop)          # the radicand at this precision
            T = math.isqrt(rad)
            qa = (q.s << (q.e + 2)) >> drop if q.e + 2 >= 0 else (q.s >> (-(q.e + 2))) >> drop
            al[(A, p)] = (qa, nm, rad, T)
            d = T - qa; hi = max(hi, d); lo = min(lo, d)
        E = max(abs(hi), abs(lo)) + 1
        k = max(1, (2 * E - 1).bit_length())
        bad = 0; mr = 0; nm = 0
        for A, p in sc:
            qa, nm, rad, T = al[(A, p)]
            C = max(0, qa - E)
            rem = rad - C * C
            mr = max(mr, rem.bit_length())
            if rem < 0: bad += 1; continue
            for i in range(k - 1, -1, -1):
                t = rem - ((C << (i + 1)) + (1 << (2 * i)))
                if t >= 0: rem = t; C += (1 << i)
            s = 67 + p
            lost = (A << s) & ((1 << (2 * drop)) - 1)
            nz = (rem != 0) or (lost != 0)
            if C != T or nz != ((A << s) - (C << drop) ** 2 != 0): bad += 1
        print(f"{name:9} {it:5} {nm:5} {k:8} {mr:9} {'yes' if bad == 0 else f'NO({bad})':>6}")
    print()
