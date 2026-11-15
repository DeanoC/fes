# Slice 4 exactness model, corrected construction.
#
#   1. Goldschmidt on the shared multiplier, 64-bit truncated intermediates.
#   2. C0 = qhat - E, a guaranteed lower bound (E measured, not guessed).
#      The increment d = T - C0 lies in [0, 2E).
#   3. k = ceil(log2(2E)) restoring compare-subtract steps find d exactly and
#      leave the exact remainder, hence the TRUE sticky bit round_pc() needs.
import random
M64 = (1 << 64) - 1
RCP = [min(1023, int(1.0 / (0.5 + (i + 0.5) / 256.0) * 512.0 + 0.5)) for i in range(128)]
def mulhi(x, y): return ((x * y) >> 64) & M64

def qhat_of(A, B, iters, drop):
    s = 1 if A < B else 0
    sh = 65 + s
    Rs = (RCP[(B >> 56) & 0x7F] << 54) & M64
    N = mulhi(A, Rs); D = mulhi(B, Rs)
    for _ in range(iters):
        f = ((1 << 64) - D) & M64
        if f == 0: f = M64
        N = (mulhi(N, f) << 1) & M64
        D = (mulhi(D, f) << 1) & M64
    return (N << (sh - 63)) >> drop, sh - drop

def exact(A, B, drop):
    s = 1 if A < B else 0
    num = A << (65 + s - drop)
    return num // B, (num % B) != 0

def build_cases():
    cs = []
    ext = [1 << 63, M64, (1 << 63) | 1, M64 - 1, 0xC000000000000000,
           0xAAAAAAAAAAAAAAAA, 0x8000000000000003, 0xFFFFFFFFFFFFF800]
    cs += [(a, b) for a in ext for b in ext]
    for i in range(128):
        b0 = (1 << 63) | (i << 56)
        b1 = b0 | ((1 << 56) - 1)
        cs += [(1 << 63, b0), (M64, b0), (1 << 63, b1), (M64, b1), (b0, b1), (b1, b0)]
    rnd = random.Random(4)
    for _ in range(3000):
        for nb in (24, 53, 64):
            B = (rnd.getrandbits(64) | (1 << 63)) & ~((1 << (64 - nb)) - 1)
            q = rnd.getrandbits(nb) | (1 << (nb - 1))
            p = q * B
            for d in (-1, 0, 1):
                v = (p >> max(0, p.bit_length() - 64)) + d
                cs.append(((v | (1 << 63)) & M64, B))
    cs += [(rnd.getrandbits(64) | (1 << 63), rnd.getrandbits(64) | (1 << 63))
           for _ in range(40000)]
    return cs

cases = build_cases()
PCPOS = {"PC 24": 39, "PC 53": 10, "PC 64": -2}
print(f"{len(cases)} cases\n")
print(f"{'precision':9} {'iters':>5} {'max T-qhat':>11} {'E':>6} {'steps k':>8} "
      f"{'rem bits':>9} {'muls':>5} {'exact':>7}")
results = {}
for name, pos in PCPOS.items():
    drop = max(0, pos + 2)
    row = None
    for iters in (2, 3, 4):
        # measure the error bound over every case
        worst = -1 << 60; worstlo = 1 << 60
        for A, B in cases:
            q, shp = qhat_of(A, B, iters, drop)
            T, _ = exact(A, B, drop)
            worst = max(worst, T - q); worstlo = min(worstlo, T - q)
        E = max(abs(worst), abs(worstlo)) + 1
        k = max(1, (2 * E - 1).bit_length())
        # now verify exactness with C0 = qhat - E and k restoring steps
        bad = 0; maxrem = 0
        for A, B in cases:
            q, shp = qhat_of(A, B, iters, drop)
            C = q - E
            if C < 0: C = 0
            rem = (A << shp) - C * B
            maxrem = max(maxrem, rem.bit_length())
            if rem < 0: bad += 1; continue
            for i in range(k - 1, -1, -1):
                t = rem - (B << i)
                if t >= 0: rem = t; C |= 0; C += (1 << i)
            T, nz = exact(A, B, drop)
            if C != T or (rem != 0) != nz: bad += 1
        row = (iters, worst, E, k, maxrem, 2 + 2 * iters, bad)
        if bad == 0: break
    results[name] = row
    print(f"{name:9} {row[0]:5} {row[1]:11} {row[2]:6} {row[3]:8} {row[4]:9} "
          f"{row[5]:5} {'yes' if row[6]==0 else f'NO({row[6]})':>7}")
