# The error bound E and the step count k are RTL CONSTANTS, so they must not
# be fitted to the cases that then verify them -- that makes "zero mismatches"
# true by construction. Here they are fixed with margin and validated on
# independent cases, and the script reports the observed margin so the
# headroom is visible rather than assumed.
#
# Constants chosen from the earlier measurements, rounded up to a power of two:
#   divide  PC 64: measured max |T-qhat| = 49  -> E = 128, k = 8
#   divide  PC 24/53: measured 1               -> E = 4,   k = 3
#   sqrt    PC 64: measured 21                 -> E = 64,  k = 7
#   sqrt    PC 24/53: measured 1               -> E = 4,   k = 3
import math, random, sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
M64 = (1 << 64) - 1
RCP = [min(1023, int(1.0 / (0.5 + (i + 0.5) / 256.0) * 512.0 + 0.5)) for i in range(128)]
RSQ = [0] * 256
for p in range(2):
    for i in range(128):
        a = (1.0 + (i + 0.5) / 128.0) * (2.0 if p else 1.0)
        RSQ[p * 128 + i] = min(2047, int(1.0 / math.sqrt(a) * 1024.0 + 0.5))

def mulhi(x, y): return ((x * y) >> 64) & M64

DIV = {41: (2, 4, 3), 12: (3, 4, 3), 0: (3, 128, 8)}      # drop -> iters, E, k
SQR = {41: (2, 4, 3), 12: (3, 4, 3), 0: (3, 64, 7)}

def div_check(A, B, drop):
    iters, E, k = DIV[drop]
    s = 1 if A < B else 0
    sh = 65 + s - drop
    Rs = (RCP[(B >> 56) & 0x7F] << 54) & M64
    N = mulhi(A, Rs); D = mulhi(B, Rs)
    for _ in range(iters):
        f = ((1 << 64) - D) & M64
        if f == 0: f = M64
        N = (mulhi(N, f) << 1) & M64
        D = (mulhi(D, f) << 1) & M64
    qhat = (N << (65 + s - 63)) >> drop
    num = A << sh
    T, r = divmod(num, B)
    margin = abs(T - qhat)
    C = max(0, qhat - E)
    rem = num - C * B
    if rem < 0: return False, margin, 0
    width = rem.bit_length()          # the REGISTER width: the initial bound
    for i in range(k - 1, -1, -1):
        t = rem - (B << i)
        if t >= 0: rem = t; C += (1 << i)
    return (C == T and (rem != 0) == (r != 0)), margin, width

class F:
    __slots__ = ('s', 'e')
    def __init__(s_, sig, exp):
        if sig:
            n = sig.bit_length()
            if n > 64: sig >>= (n - 64); exp += (n - 64)
            elif n < 64: sig <<= (64 - n); exp -= (64 - n)
        s_.s = sig & M64; s_.e = exp
    def mul(a, b): return F(((a.s * b.s) >> 64), a.e + b.e + 1)

def sub(a, b):
    sh = a.e - b.e
    if sh >= 64: return F(a.s, a.e)
    bs = b.s >> sh if sh >= 0 else b.s
    as_ = a.s if sh >= 0 else a.s >> (-sh)
    e = a.e if sh >= 0 else b.e
    d = as_ - bs
    return F(d, e) if d > 0 else F(0, 0)

def sqrt_check(A, p, drop):
    iters, E, k = SQR[drop]
    a = F(A, p); y = F(RSQ[p * 128 + ((A >> 56) & 0x7F)], 53)
    three = F(3 << 62, 1)
    for _ in range(iters):
        ay2 = a.mul(y).mul(y)
        y = y.mul(sub(three, ay2)); y = F(y.s, y.e - 1)
    q = a.mul(y)
    s = 67 + p
    rad = (A << s) >> (2 * drop)
    T = math.isqrt(rad)
    qa = (q.s << (q.e + 2)) >> drop if q.e + 2 >= 0 else (q.s >> (-(q.e + 2))) >> drop
    margin = abs(T - qa)
    C = max(0, qa - E)
    rem = rad - C * C
    if rem < 0: return False, margin, 0
    width = rem.bit_length()          # the REGISTER width: the initial bound
    for i in range(k - 1, -1, -1):
        t = rem - ((C << (i + 1)) + (1 << (2 * i)))
        if t >= 0: rem = t; C += (1 << i)
    lost = (A << s) & ((1 << (2 * drop)) - 1)
    nz = (rem != 0) or (lost != 0)
    return (C == T and nz == ((A << s) - (C << drop) ** 2 != 0)), margin, width

N = int(sys.argv[1]) if len(sys.argv) > 1 else 300000
rnd = random.Random(20261115)      # a seed NOT used by any fitting run
print(f"independent validation, {N} random pairs per precision, constants fixed\n")
for label, drop in (("PC 24", 41), ("PC 53", 12), ("PC 64", 0)):
    it, E, k = DIV[drop]
    bad = 0; wm = 0; wr = 0
    for _ in range(N):
        A = rnd.getrandbits(64) | (1 << 63); B = rnd.getrandbits(64) | (1 << 63)
        ok, m, rb = div_check(A, B, drop)
        if not ok: bad += 1
        wm = max(wm, m); wr = max(wr, rb)
    print(f"FDIV  {label}: iters={it} E={E} k={k}  {bad} mismatches  "
          f"worst |T-qhat|={wm} (headroom {E - wm})  widest remainder register={wr} bits")
for label, drop in (("PC 24", 41), ("PC 53", 12), ("PC 64", 0)):
    it, E, k = SQR[drop]
    bad = 0; wm = 0; wr = 0
    for _ in range(N):
        A = rnd.getrandbits(64) | (1 << 63); p = rnd.getrandbits(1)
        ok, m, rb = sqrt_check(A, p, drop)
        if not ok: bad += 1
        wm = max(wm, m); wr = max(wr, rb)
    print(f"FSQRT {label}: iters={it} E={E} k={k}  {bad} mismatches  "
          f"worst |T-qhat|={wm} (headroom {E - wm})  widest remainder register={wr} bits")
