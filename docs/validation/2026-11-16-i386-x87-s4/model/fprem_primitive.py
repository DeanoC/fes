# Is FPREM's partial step the divide tail's primitive?
#
# A partial step consumes at most 64 quotient bits (measured on the host:
# model/fprem_host.c). So N = floor(A*2^64 / B) is at most 65 bits, N*B is
# exactly the shared multiplier's 128-bit product, and the remainder
# A*2^64 - N*B is exact and less than B. If that holds with no violations,
# the remainders need no divider of their own.
import random

M64 = (1 << 64) - 1

def prem_step(A, eA, B, eB, round_near):
    """One step. Returns (quotient, exact remainder, partial)."""
    d = eA - eB
    if d < 64:                               # completes: C2 clear
        num = A << d
        q, rem = divmod(num, B)
        if round_near and (2 * rem > B or (2 * rem == B and (q & 1))):
            q += 1; rem -= B                 # FPREM1 rounds the quotient to nearest
        return q, rem, False
    num = A << 64                            # partial: take 64 quotient bits
    q, rem = divmod(num, B)
    return q, rem, True

rnd = random.Random(21)
viol = 0; n = 0; worst_q = 0
for _ in range(20000):
    A = rnd.getrandbits(64) | (1 << 63)
    B = rnd.getrandbits(64) | (1 << 63)
    eA = rnd.randrange(0, 300)
    for near in (False, True):
        q, rem, partial = prem_step(A, eA, B, 0, near)
        n += 1
        if partial:
            worst_q = max(worst_q, q.bit_length())
            if q.bit_length() > 65: viol += 1
        if near:
            if abs(2 * rem) > B: viol += 1   # FPREM1's remainder is in [-B/2, B/2]
        elif rem < 0 or rem >= B: viol += 1
print(f"{n} cases, {viol} violations, widest partial quotient = {worst_q} bits")
print("the quotient fits the 64x64 product and the remainder is exact and in range"
      if viol == 0 else "VIOLATIONS -- the primitive does not carry")
