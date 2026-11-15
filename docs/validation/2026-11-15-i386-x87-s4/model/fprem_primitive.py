# Does FPREM's partial step run on the divide tail's primitive?
#
# An earlier version of this check compared Python's divmod against its own
# remainder, which is true by definition and proved nothing. This one runs
# the ACTUAL datapath -- the Goldschmidt estimate, the fixed error bound and
# the restoring tail -- at the step's variable scale n = (expDiff & 31) | 32
# (Bochs cpu/fpu/fprem.cc at the pinned revision, and the host, agree), and
# counts the multiplier passes the variable shift costs.
import random, sys, os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
M64 = (1 << 64) - 1
RCP = [min(1023, int(1.0 / (0.5 + (i + 0.5) / 256.0) * 512.0 + 0.5)) for i in range(128)]
def mulhi(x, y): return ((x * y) >> 64) & M64

E_CONST, K_CONST, ITERS = 128, 8, 3      # the divide's PC 64 constants, reused

def prem_step(A, expDiff, B, near):
    """One FPREM/FPREM1 step on the divide tail's primitive.
    Returns (quotient, exact remainder, partial, multiplier_passes, q_bits)."""
    partial = expDiff >= 64
    n = ((expDiff & 31) | 32) if partial else expDiff
    passes = 0
    # the dividend aligned by n: a multiply-based shift, one pass (slice 3's)
    num = A << n
    passes += 1 if n else 0
    # Goldschmidt estimate of num/B, on the shared multiplier
    Rs = (RCP[(B >> 56) & 0x7F] << 54) & M64
    # num is up to 64+n bits; estimate the quotient's top 64 bits from its
    # leading 64, which is the same normalise-then-multiply the divide does
    nb = num.bit_length()
    lead = (num >> (nb - 64)) if nb > 64 else (num << (64 - nb))
    N = mulhi(lead, Rs); D = mulhi(B, Rs); passes += 2
    for _ in range(ITERS):
        f = ((1 << 64) - D) & M64
        if f == 0: f = M64
        N = (mulhi(N, f) << 1) & M64
        D = (mulhi(D, f) << 1) & M64
        passes += 2
    # place the estimate at the quotient's scale
    qbits = nb - B.bit_length() + 1
    qhat = (N >> (64 - qbits)) if qbits <= 64 else (N << (qbits - 64))
    C = max(0, qhat - E_CONST)
    rem = num - C * B                      # one 64x64 product (C fits 64 bits)
    passes += 1
    if rem < 0:
        return None, None, partial, passes, qbits
    for i in range(K_CONST - 1, -1, -1):
        t = rem - (B << i)
        if t >= 0: rem = t; C += (1 << i)
    if near and not partial:
        # FPREM1 rounds the quotient to nearest, which is one more compare
        if 2 * rem > B or (2 * rem == B and (C & 1)):
            C += 1; rem -= B
    return C, rem, partial, passes, qbits

rnd = random.Random(20261116)
bad = 0; n_cases = 0; worst_q = 0; worst_passes = 0; worst_rem = 0
for _ in range(60000):
    A = rnd.getrandbits(64) | (1 << 63)
    B = rnd.getrandbits(64) | (1 << 63)
    expDiff = rnd.randrange(0, 400)
    for near in (False, True):
        q, rem, partial, passes, qbits = prem_step(A, expDiff, B, near)
        n_cases += 1
        if q is None: bad += 1; continue
        n = ((expDiff & 31) | 32) if partial else expDiff
        # the reference, computed independently of the datapath
        num = A << n
        tq, tr = divmod(num, B)
        if near and not partial and (2 * tr > B or (2 * tr == B and (tq & 1))):
            tq += 1; tr -= B
        if q != tq or rem != tr: bad += 1
        worst_q = max(worst_q, q.bit_length())
        worst_passes = max(worst_passes, passes)
        worst_rem = max(worst_rem, abs(rem).bit_length())
print(f"{n_cases} step evaluations, exponent differences 0-399, both roundings")
print(f"  mismatches against independent exact arithmetic: {bad}")
print(f"  widest quotient: {worst_q} bits   widest remainder: {worst_rem} bits")
print(f"  worst multiplier passes for one step: {worst_passes}")
print("  the partial step runs on the divide tail's primitive"
      if bad == 0 else "  THE PRIMITIVE DOES NOT CARRY")
