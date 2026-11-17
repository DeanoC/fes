#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""The x87 transcendentals' microprogram, its constants and its exact model
(#1958; docs/validation/2026-11-17-de25-x87-transcendentals).

FSIN, FCOS, FSINCOS, FPTAN, FPATAN, F2XM1, FYL2X and FYL2XP1 are computed on
the DE25 arithmetic backend as short sequences of its own add, subtract,
multiply and divide over constants in memory. This file is the one place
those sequences are written. From it come:

  - the microprogram and constant ROMs `fes_x87_trans` reads (--emit);
  - an integer model that runs the same microprogram with exactly rounded
    operations (`run`). The RTL must equal it bit for bit, which is an exact
    check with no tolerance;
  - the model's error against the mathematical result, in units of the last
    place (--truth), and its distance from a host's own x87 over a dump made
    by `x87_trans_probe --host-dump` (--hosts).

What follows the 387 (rules.md): the operand ranges, the special cases the
unit answers before this is entered, extended precision whatever PC says,
the final rounding under RC, and the trigonometric reduction by a pi/4 of 67
significant bits. The kernels themselves are table-and-polynomial, not the
387's CORDIC, whose microcode is not published.

Every intermediate operation is one of the backend's, rounded to nearest
even at 64 bits, on operands kept inside the normal range; only an
instruction's last operation is rounded under the control word, by the
unit's own A_RND, which is also where overflow, underflow and the inexact
flag come from. So the engine has no exception logic of its own.

Nothing here is ported from Bochs or from any other implementation: the
series are the textbook Taylor series and the constants are computed below
from integer arithmetic.
"""
from __future__ import annotations

import argparse
from fractions import Fraction
import sys

# ---- High-precision arithmetic: fixed point, PREC fractional bits. ----
PREC = 320
ONE = 1 << PREC


def fx(value: Fraction) -> int:
    return (value.numerator << PREC) // value.denominator


def frac(value: int) -> Fraction:
    return Fraction(value, ONE)


def _atan_inverse(n: int) -> int:
    """atan(1/n), by its series."""
    total, term, k, n2 = 0, ONE // n, 0, n * n
    while term:
        total += term // (2 * k + 1) if k % 2 == 0 else -(term // (2 * k + 1))
        term //= n2
        k += 1
    return total


PI = 4 * (4 * _atan_inverse(5) - _atan_inverse(239))   # Machin


def fx_mul(a: int, b: int) -> int:
    """Truncated toward zero, so a series' terms reach zero from either side."""
    product = a * b
    return product >> PREC if product >= 0 else -(-product >> PREC)


def tdiv(a: int, b: int) -> int:
    return a // b if a >= 0 else -(-a // b)


def fx_sin_cos(r: int) -> tuple[int, int]:
    """sin and cos of |r| <= 1, plain series."""
    r2 = fx_mul(r, r)
    sin, term, k = 0, r, 1
    while term:
        sin += term
        term = -tdiv(fx_mul(term, r2), (k + 1) * (k + 2))
        k += 2
    cos, term, k = 0, ONE, 0
    while term:
        cos += term
        term = -tdiv(fx_mul(term, r2), (k + 1) * (k + 2))
        k += 2
    return sin, cos


def fx_exp(u: int) -> int:
    """e^u for |u| <= 1."""
    total, term, k = 0, ONE, 0
    while term:
        total += term
        k += 1
        term = tdiv(fx_mul(term, u), k)
    return total


def fx_ln(m: int) -> int:
    """ln m for m in (1/4, 4), as 2 atanh((m - 1)/(m + 1))."""
    z = ((m - ONE) << PREC) // (m + ONE)
    z2 = fx_mul(z, z)
    total, term, k = 0, z, 1
    while term:
        total += tdiv(term, k)
        term = fx_mul(term, z2)
        k += 2
    return 2 * total


LN2 = fx_ln(2 * ONE)


def fx_sqrt(v: int) -> int:
    import math
    return math.isqrt(v << PREC)


def fx_atan(t: int) -> int:
    """atan t for 0 <= t <= 1: three halvings of the angle, then the series."""
    for _ in range(3):
        t = (t << PREC) // (ONE + fx_sqrt(ONE + fx_mul(t, t)))
    t2 = fx_mul(t, t)
    total, term, k = 0, t, 1
    while term:
        total += term // k if k % 4 == 1 else -(term // k)
        term = fx_mul(term, t2)
        k += 2
    return 8 * total


# ---- The extended format. ----
# A value is (sign, exponent, significand): (-1)^sign * significand *
# 2^(exponent - 63), the significand's bit 63 set. Zero is (sign, None, 0).
BIAS = 16383
E_MIN = -16382


def value(f) -> Fraction:
    sign, exponent, significand = f
    if exponent is None:
        return Fraction(0)
    v = Fraction(significand) * Fraction(2) ** (exponent - 63)
    return -v if sign else v


def backend(op: str, a, b):
    """One backend operation as the engine uses it: rounded to nearest even
    at 64 bits, and a zero result signed as fes_x87_fast and fes_x87_slow
    sign it under round to nearest -- a product's or a quotient's by its
    operands' signs, a sum of two zeros by their common sign, and any other
    zero sum, an exact cancellation included, positive."""
    if op in ('MUL', 'DIV'):
        if a[1] is None or (op == 'MUL' and b[1] is None):
            return (a[0] ^ b[0], None, 0)
        return rn(value(a) * value(b) if op == 'MUL' else value(a) / value(b))
    sign_b = b[0] ^ (op == 'SUB')
    if op == 'RSUB':
        a, b, sign_b = b, a, a[0] ^ 1
    if a[1] is None and b[1] is None:
        return (a[0] if a[0] == sign_b else 0, None, 0)
    return rn(value(a) + value((sign_b, b[1], b[2])))


def rn(v: Fraction):
    """Round to nearest even at 64 bits, with no exponent limit."""
    if v == 0:
        return (0, None, 0)
    sign, v = (1, -v) if v < 0 else (0, v)
    exponent = v.numerator.bit_length() - v.denominator.bit_length()
    if Fraction(2) ** exponent > v:
        exponent -= 1
    scaled = v / Fraction(2) ** (exponent - 63)
    significand = scaled.numerator // scaled.denominator
    rest = scaled - significand
    if rest > Fraction(1, 2) or rest == Fraction(1, 2) and significand & 1:
        significand += 1
    if significand >> 64:
        significand >>= 1
        exponent += 1
    return (sign, exponent, significand)


def from_bits(sign_exponent: int, significand: int):
    """An 80-bit value as the engine sees an operand: normalised, with the
    exponent a denormal really has."""
    sign, field = sign_exponent >> 15, sign_exponent & 0x7fff
    if significand == 0:
        return (sign, None, 0)
    exponent = (field if field else 1) - BIAS
    shift = 64 - significand.bit_length()
    return (sign, exponent - shift, significand << shift)


def to_bits(f) -> tuple[int, int]:
    sign, exponent, significand = f
    if exponent is None:
        return sign << 15, 0
    assert E_MIN <= exponent <= 16383, f
    return sign << 15 | (exponent + BIAS), significand


def final_round(v: Fraction, rc: int) -> tuple[int, int, int]:
    """The unit's last rounding, as A_RND does it with every exception
    masked: (sign and exponent field, significand, flags) with flags bit 5
    PE, bit 4 UE, bit 3 OE. rc: 0 nearest, 1 down, 2 up, 3 toward zero."""
    if v == 0:
        return 0, 0, 0
    sign, mag = (1, -v) if v < 0 else (0, v)
    exponent = mag.numerator.bit_length() - mag.denominator.bit_length()
    if Fraction(2) ** exponent > mag:
        exponent -= 1
    quantum = Fraction(2) ** (max(exponent, E_MIN) - 63)
    scaled = mag / quantum
    whole = scaled.numerator // scaled.denominator
    rest = scaled - whole
    up = (rest > Fraction(1, 2) or rest == Fraction(1, 2) and whole & 1) if rc == 0 else \
         (rest != 0 and ((rc == 1 and sign) or (rc == 2 and not sign)))
    whole += up
    flags = 0x20 if rest else 0
    exponent = max(exponent, E_MIN)
    if whole >> 64:
        whole >>= 1
        exponent += 1
    if exponent > 16383:
        infinite = rc == 0 or (rc == 1 and sign) or (rc == 2 and not sign)
        return (sign << 15 | 0x7fff, 1 << 63, 0x28) if infinite else (sign << 15 | 0x7ffe, (1 << 64) - 1, 0x28)
    if not whole >> 63:                       # a denormal, or zero
        return sign << 15, whole, flags | (0x10 if rest else 0)
    return sign << 15 | (exponent + BIAS), whole, flags


def hi21(f):
    """A value with all but the top 21 bits of its significand cleared."""
    return f if f[1] is None else (f[0], f[1], f[2] & ~((1 << 43) - 1))


def lo43(f):
    """What hi21 cleared, as a value: exact."""
    low = 0 if f[1] is None else f[2] & ((1 << 43) - 1)
    if not low:
        return (f[0], None, 0)
    shift = 64 - low.bit_length()
    return (f[0], f[1] - shift, low << shift)


# ---- The constants. ----
class Rom:
    def __init__(self):
        self.words: list[tuple[int, int]] = []    # (sign and exponent field, significand)
        self.names: dict[str, int] = {}

    def add(self, name: str, v: Fraction) -> int:
        """A constant, rounded to nearest."""
        return self.put(name, rn(v))

    def put(self, name: str, f) -> int:
        assert name not in self.names, name
        self.names[name] = len(self.words)
        self.words.append(to_bits(f))
        return self.names[name]

    def pair(self, name: str, v: Fraction) -> None:
        """A constant as a rounded head and the rounded rest."""
        head = rn(v)
        self.put(name + '_h', head)
        self.add(name + '_l', v - value(head))

    def get(self, index: int):
        sign_exponent, significand = self.words[index]
        return from_bits(sign_exponent, significand)


# pi/4 to 67 significant bits, the 387's reduction constant (rules.md G2):
# P = PM * 2^-67, truncated. The bits after the 67th are 001..., so rounding
# gives the same number, and so does a pi of 66 bits.
PM = (PI >> 2) >> (PREC - 67)
assert PM.bit_length() == 67

TRIG_LO, TRIG_HI = 4, 25          # sin and cos table: c = j/32
EXP_LO, EXP_HI = 4, 32            # 2^x table: c = j/32, both signs
LOG_LO, LOG_HI = 45, 91           # log2 table: c = j/64
ATAN_LO, ATAN_HI = 2, 32          # atan table: c = j/32


def build_rom() -> Rom:
    rom = Rom()
    factorial = [1]
    for k in range(1, 24):
        factorial.append(factorial[-1] * k)
    rom.put('zero', (0, None, 0))
    rom.add('one', Fraction(1))
    rom.put('one_up', (0, 0, (1 << 63) + 1))          # the value after 1: |x| < it is |x| <= 1
    rom.add('eighth', Fraction(1, 8))
    rom.add('sixteenth', Fraction(1, 16))
    rom.add('c3_128', Fraction(3, 128))
    rom.add('tiny', Fraction(1, 2 ** 80))
    rom.pair('pi', frac(PI))
    rom.pair('pi2', frac(PI) / 2)
    rom.pair('ln2', frac(LN2))
    rom.pair('k', 1 / frac(LN2))                      # log2 e
    k_head = rom.get(rom.names['k_h'])
    rom.put('k_a', hi21(k_head))                       # ... its top 21 bits, and the rest of the head
    rom.add('k_b', value(k_head) - value(hi21(k_head)))
    # sin: x^3 (s3 + z (s5 + ...)), z = x^2; cos - 1: z (c2 + z (c4 + ...)).
    for k in range(3, 15, 2):
        rom.add(f's{k}', Fraction((-1) ** (k // 2), factorial[k]))
    for k in range(2, 14, 2):
        rom.add(f'c{k}', Fraction((-1) ** (k // 2), factorial[k]))
    # atan x - x: x^3 (a3 + z (a5 + ...)).
    for k in range(3, 17, 2):
        rom.add(f'a{k}', Fraction((-1) ** (k // 2), k))
    # (2^x - 1)/x = g1 + x (g2 + x (g3 + ...)), g_k = ln2^k / k!.
    power = Fraction(1)
    for k in range(1, 14):
        power *= frac(LN2)
        if k > 1:
            rom.add(f'g{k}', power / factorial[k])
    # e^u - 1 - u = u^2 (e2 + u (e3 + ...)).
    for k in range(2, 9):
        rom.add(f'e{k}', Fraction(1, factorial[k]))
    # log2(1 + u) = u (k + u (k2 + u (k3 + ...))), k_n = (-1)^(n+1) / (n ln 2).
    for k in range(2, 13):
        rom.add(f'k{k}', Fraction((-1) ** (k + 1), k) / frac(LN2))
    # The tables, one array a field so an index is a base plus j.
    def table(prefix: str, low: int, high: int, values, paired: bool) -> None:
        entries = {n: values(n) for n in range(low, high + 1)}
        if not paired:
            for n, v in entries.items():
                rom.add(f'{prefix}{n}', v)
            return
        heads = {n: rn(v) for n, v in entries.items()}
        for n in entries:
            rom.put(f'{prefix}{n}_h', heads[n])
        for n, v in entries.items():
            rom.add(f'{prefix}{n}_l', v - value(heads[n]))

    def angle(n: int) -> tuple[int, int]:
        return fx_sin_cos(fx(Fraction(n, 32)))

    table('tc', TRIG_LO, TRIG_HI, lambda n: Fraction(n, 32), False)
    table('ts', TRIG_LO, TRIG_HI, lambda n: frac(angle(n)[0]), True)
    table('tk', TRIG_LO, TRIG_HI, lambda n: frac(angle(n)[1]), True)
    table('xc', EXP_LO, EXP_HI, lambda n: Fraction(n, 32), False)
    for sign, tag in ((1, 'p'), (-1, 'n')):
        def power(n: int, sign=sign) -> Fraction:
            return frac(fx_exp(sign * fx_mul(fx(Fraction(n, 32)), LN2)))
        table(f'x{tag}t', EXP_LO, EXP_HI, power, False)
        table(f'x{tag}m', EXP_LO, EXP_HI, lambda n, power=power: power(n) - 1, True)
    table('lc', LOG_LO, LOG_HI, lambda n: Fraction(n, 64), False)
    table('li', LOG_LO, LOG_HI, lambda n: Fraction(64, n), False)
    table('ll', LOG_LO, LOG_HI, lambda n: frac(fx_ln(fx(Fraction(n, 64)))) / frac(LN2), True)
    table('ac', ATAN_LO, ATAN_HI, lambda n: Fraction(n, 32), False)
    table('aa', ATAN_LO, ATAN_HI, lambda n: frac(fx_atan(fx(Fraction(n, 32)))), True)
    return rom


# ---- The microprogram. ----
# A word is (op, dst, a, b, arg). `a` is always a temporary. `b` is a
# temporary ('t', n), a constant ('c', index) or a table entry ('j', base):
# the constant at base + J. The operations:
#   ADD SUB RSUB MUL DIV   dst <- a op b, on the backend, rounded to nearest.
#                          RSUB is b - a.
#   MOV NEG ABS            dst <- b, -b, |b|
#   SCALE                  dst <- b * 2^E
#   HI LO                  dst <- b's top 21 significand bits, and the rest of b: b = HI + LO exactly
#   ITOF                   dst <- E as a value (E not zero)
#   TIDX                   J <- round(|b| * 2^arg)
#   LSPL                   dst <- the significand of b (or of the x operand,
#                          arg 1) as a value in (sqrt(1/2), sqrt 2], E <- its
#                          exponent, flag M1 <- the value is exactly 1
#   SETE                   E <- arg: an expression of the operands' exponents
#   CMP                    flag arg <- |a| < |b|
#   TRED                   the trigonometric reduction: T4, T5 <- the reduced
#                          argument's head and rest, and the octant flags
#   BR  arg=(flag, value, label)   branch when the flag has that value
#   JMP arg=label; CALL arg=label; RET
#   FIN arg=(kind, use_e, negate_flag, second): the last operation, whose
#       unrounded result the unit rounds. kind: ADD SUB MUL DIV on (a, b);
#       MOV: a itself; BELOW, ABOVE: a, an eighth of a unit short of or past
#       it. use_e adds E to the result's exponent. negate_flag names a flag
#       that flips the sign. second is the temporary holding the other
#       result of a two-result form, or None.
FLAGS = ('SWAP', 'SNEG', 'CNEG', 'TNEG', 'XS', 'YS', 'T32', 'T68', 'C', 'EZ', 'EBIG', 'ESMALL', 'YGTX', 'M1',
         'J64')
T_X, T_Y, T_MX, T_MY = 0, 1, 2, 3        # x, y, and each one's significand at exponent 0, signed


class Program:
    def __init__(self, rom: Rom):
        self.rom = rom
        self.words: list[list] = []
        self.labels: dict[str, int] = {}
        self.entries: dict[str, int] = {}

    def label(self, name: str) -> None:
        assert name not in self.labels, name
        self.labels[name] = len(self.words)

    def entry(self, name: str) -> None:
        self.entries[name] = len(self.words)

    def _b(self, b):
        if isinstance(b, int):
            return ('t', b)
        if b.startswith('@'):                         # a table: the constant at base + J
            name, low = b[1:].split(':')
            return ('j', self.rom.names[name] - int(low))
        return ('c', self.rom.names[b])

    def op(self, op: str, dst: int = 0, a: int = 0, b=0, arg=None) -> None:
        self.words.append([op, dst, a, self._b(b), arg])

    # Shorthand.
    def add(self, d, a, b): self.op('ADD', d, a, b)
    def sub(self, d, a, b): self.op('SUB', d, a, b)
    def rsub(self, d, a, b): self.op('RSUB', d, a, b)
    def mul(self, d, a, b): self.op('MUL', d, a, b)
    def div(self, d, a, b): self.op('DIV', d, a, b)
    def mov(self, d, b): self.op('MOV', d, 0, b)
    def br(self, flag, wanted, label): self.op('BR', arg=(flag, wanted, label))
    def jmp(self, label): self.op('JMP', arg=label)
    def call(self, label): self.op('CALL', arg=label)
    def ret(self): self.op('RET')

    def fin(self, kind, a=0, b=0, use_e=False, negate=None, second=None):
        self.op('FIN', 0, a, b, (kind, use_e, negate, second))

    def horner(self, acc: int, z: int, names: list[str]) -> None:
        """acc <- names[0] + z (names[1] + z (... names[-1]))."""
        self.mul(acc, z, names[-1])
        for name in reversed(names[1:-1]):
            self.add(acc, acc, name)
            self.mul(acc, acc, z)
        self.add(acc, acc, names[0])

    def resolve(self) -> None:
        for word in self.words:
            if word[0] in ('JMP', 'CALL'):
                word[4] = self.labels[word[4]]
            elif word[0] == 'BR':
                flag, wanted, label = word[4]
                word[4] = (FLAGS.index(flag), wanted, self.labels[label])
            elif word[0] == 'FIN':
                kind, use_e, negate, second = word[4]
                word[4] = (kind, use_e, None if negate is None else FLAGS.index(negate), second)
            elif word[0] == 'CMP':
                word[4] = FLAGS.index(word[4])


def build_program(rom: Rom) -> Program:
    p = Program(rom)
    S_NAMES = [f's{k}' for k in range(3, 15, 2)]
    C_NAMES = [f'c{k}' for k in range(2, 14, 2)]

    # ---- sin and cos of the reduced argument in T4 (head) and T5 (rest). ----
    # Each leaves its result as T8 + T9, the head exact and the rest small,
    # so the caller's last addition is the only rounding that matters.
    def table_setup():
        """J, then d in T6, z = d^2 in T7, sin d - d in T10, cos d - 1 in T11."""
        p.op('TIDX', 0, 0, 4, 5)
        p.sub(6, 4, f'@tc{TRIG_LO}:{TRIG_LO}')
        p.add(6, 6, 5)
        p.mul(7, 6, 6)
        p.horner(10, 7, S_NAMES[:4])
        p.mul(10, 10, 7)
        p.mul(10, 10, 6)
        p.horner(11, 7, C_NAMES[:4])
        p.mul(11, 11, 7)

    def sin_small():
        p.mul(7, 4, 4)
        p.horner(9, 7, S_NAMES)
        p.mul(9, 9, 7)
        p.mul(9, 9, 4)
        p.mov(8, 4)

    def cos_small():
        p.mul(7, 4, 4)
        p.horner(9, 7, C_NAMES)
        p.mul(9, 9, 7)
        p.mov(8, 'one')

    def sin_table():
        """sin(c + d) = S + [Sl + C d + C (sin d - d) + S (cos d - 1)]."""
        p.mul(12, 10, f'@tk{TRIG_LO}_h:{TRIG_LO}')
        p.mul(13, 11, f'@ts{TRIG_LO}_h:{TRIG_LO}')
        p.add(12, 12, 13)
        p.mul(13, 6, f'@tk{TRIG_LO}_h:{TRIG_LO}')
        p.add(12, 12, 13)
        p.add(9, 12, f'@ts{TRIG_LO}_l:{TRIG_LO}')
        p.mov(8, f'@ts{TRIG_LO}_h:{TRIG_LO}')

    def cos_table(head: int, rest: int):
        """cos(c + d) = C + [Cl - S d - S (sin d - d) + C (cos d - 1)]."""
        p.mul(12, 10, f'@ts{TRIG_LO}_h:{TRIG_LO}')
        p.mul(13, 11, f'@tk{TRIG_LO}_h:{TRIG_LO}')
        p.sub(12, 13, 12)
        p.mul(13, 6, f'@ts{TRIG_LO}_h:{TRIG_LO}')
        p.sub(12, 12, 13)
        p.add(rest, 12, f'@tk{TRIG_LO}_l:{TRIG_LO}')
        p.mov(head, f'@tk{TRIG_LO}_h:{TRIG_LO}')

    p.label('sin_kernel')
    p.op('CMP', 0, 4, 'eighth', 'C')
    p.br('C', 0, 'sin_kernel_table')
    sin_small()
    p.ret()
    p.label('sin_kernel_table')
    table_setup()
    sin_table()
    p.ret()

    p.label('cos_kernel')
    p.op('CMP', 0, 4, 'eighth', 'C')
    p.br('C', 0, 'cos_kernel_table')
    cos_small()
    p.ret()
    p.label('cos_kernel_table')
    table_setup()
    cos_table(8, 9)
    p.ret()

    # Both: the sine in T8 + T9 and the cosine in T14 + T15.
    p.label('sincos_kernel')
    p.op('CMP', 0, 4, 'eighth', 'C')
    p.br('C', 0, 'sincos_kernel_table')
    cos_small()
    p.mov(14, 8)
    p.mov(15, 9)
    sin_small()
    p.ret()
    p.label('sincos_kernel_table')
    table_setup()
    cos_table(14, 15)
    sin_table()
    p.ret()

    # ---- FSIN, FCOS: one kernel, chosen by the octant. ----
    p.entry('FSIN')
    p.br('T32', 0, 'fsin_reduce')
    p.op('SETE', arg='XE')
    p.fin('BELOW', a=T_MX, use_e=True)                  # sin x = x - x^3/6: just short of x
    p.label('fsin_reduce')
    p.op('TRED')
    p.br('SWAP', 1, 'fsin_cos')
    p.call('sin_kernel')
    p.fin('ADD', 8, 9, negate='SNEG')
    p.label('fsin_cos')
    p.call('cos_kernel')
    p.fin('ADD', 8, 9, negate='SNEG')

    p.entry('FCOS')
    p.br('T32', 0, 'fcos_reduce')
    p.mov(8, 'one')
    p.fin('BELOW', a=8)                                 # cos x = 1 - x^2/2
    p.label('fcos_reduce')
    p.op('TRED')
    p.br('SWAP', 1, 'fcos_sin')
    p.call('cos_kernel')
    p.fin('ADD', 8, 9, negate='CNEG')
    p.label('fcos_sin')
    p.call('sin_kernel')
    p.fin('ADD', 8, 9, negate='CNEG')

    # ---- FSINCOS: the sine is the last result, which replaces ST; the ----
    # cosine, rounded to nearest, is the other, which the unit pushes.
    p.entry('FSINCOS')
    p.br('T32', 0, 'fsincos_reduce')
    p.mov(8, 'one')
    p.op('SETE', arg='XE')
    p.fin('BELOW', a=T_MX, use_e=True, second=8)        # sin x just short of x, cos x = 1
    p.label('fsincos_reduce')
    p.op('TRED')
    p.call('sincos_kernel')
    p.br('SWAP', 1, 'fsincos_swap')
    p.add(12, 14, 15)                                   # the cosine
    p.br('CNEG', 0, 'fsincos_a')
    p.op('NEG', 12, 0, 12)
    p.label('fsincos_a')
    p.fin('ADD', 8, 9, negate='SNEG', second=12)
    p.label('fsincos_swap')
    p.add(12, 8, 9)                                     # the cosine is the sine kernel's
    p.br('CNEG', 0, 'fsincos_b')
    p.op('NEG', 12, 0, 12)
    p.label('fsincos_b')
    p.fin('ADD', 14, 15, negate='SNEG', second=12)

    # ---- FPTAN: sine over cosine; the unit pushes the 1.0. ----
    p.entry('FPTAN')
    p.br('T32', 0, 'fptan_reduce')
    p.op('SETE', arg='XE')
    p.fin('ABOVE', a=T_MX, use_e=True)                  # tan x = x + x^3/3
    p.label('fptan_reduce')
    p.op('TRED')
    p.call('sincos_kernel')
    p.add(12, 8, 9)
    p.add(13, 14, 15)
    p.br('SWAP', 1, 'fptan_swap')
    p.fin('DIV', 12, 13, negate='TNEG')
    p.label('fptan_swap')
    p.fin('DIV', 13, 12, negate='TNEG')

    # ---- F2XM1. ----
    G_NAMES = [f'g{k}' for k in range(2, 14)]
    E_NAMES = [f'e{k}' for k in range(2, 9)]
    p.entry('F2XM1')
    p.br('T68', 0, 'f2xm1_normal')
    p.op('SETE', arg='XE')
    p.fin('MUL', T_MX, 'ln2_h', use_e=True)             # x ln 2, E = x's exponent
    p.label('f2xm1_normal')
    p.op('CMP', 0, T_X, 'one_up', 'C')                  # |x| <= 1: inside the 387's range
    p.br('C', 1, 'f2xm1_range')
    p.fin('MOV', a=T_X)                                 # undefined: the operand, as the hosts leave it
    p.label('f2xm1_range')
    p.op('CMP', 0, T_X, 'eighth', 'C')
    p.br('C', 0, 'f2xm1_table')
    p.horner(9, T_X, G_NAMES)                           # x (g1 + x H): g1 is ln 2, head and rest
    p.mul(9, 9, T_X)
    p.add(9, 9, 'ln2_l')
    p.add(9, 9, 'ln2_h')
    p.fin('MUL', T_X, 9)
    p.label('f2xm1_table')
    p.op('TIDX', 0, 0, T_X, 5)
    p.op('ABS', 6, 0, T_X)
    p.sub(6, 6, f'@xc{EXP_LO}:{EXP_LO}')                # |x| - c, exact
    p.br('XS', 1, 'f2xm1_negative')
    p.call('f2xm1_e')
    p.mul(12, 12, f'@xpt{EXP_LO}:{EXP_LO}')
    p.add(9, 12, f'@xpm{EXP_LO}_l:{EXP_LO}')
    p.mov(8, f'@xpm{EXP_LO}_h:{EXP_LO}')
    p.fin('ADD', 8, 9)
    p.label('f2xm1_negative')
    p.op('NEG', 6, 0, 6)
    p.call('f2xm1_e')
    p.mul(12, 12, f'@xnt{EXP_LO}:{EXP_LO}')
    p.add(9, 12, f'@xnm{EXP_LO}_l:{EXP_LO}')
    p.mov(8, f'@xnm{EXP_LO}_h:{EXP_LO}')
    p.fin('ADD', 8, 9)
    p.label('f2xm1_e')                                  # T12 <- 2^d - 1, d in T6
    p.mul(7, 6, 'ln2_h')
    p.horner(12, 7, E_NAMES)
    p.mul(12, 12, 7)
    p.mul(12, 12, 7)
    p.add(12, 12, 7)
    p.ret()

    # ---- FYL2X and FYL2XP1: y log2. ----
    # log2 m = L + log2(1 + u), and log2(1 + u) = u k + u (kl + u H(u)).
    # The product with y is where the error would double, so it is never
    # taken of a rounded logarithm. Away from 1 the logarithm is split into
    # its rounded value G and exactly what the rounding dropped, and the
    # result is y G + y (rest). Beside 1 with no exponent, where u k is the
    # whole result, y, u and k are each split at 21 bits so that the leading
    # product of three is exact.
    K_NAMES = [f'k{k}' for k in range(2, 13)]

    p.label('log_tail')                                 # T10 <- kl + u H(u), u in T6
    p.horner(10, 6, K_NAMES)
    p.mul(10, 10, 6)
    p.add(10, 10, 'k_l')
    p.ret()

    p.label('log_split')                                # y u k, exactly at the head; T10 = kl + u H
    p.op('HI', 11, 0, T_MY)
    p.op('LO', 12, 0, T_MY)
    p.op('HI', 13, 0, 6)
    p.op('LO', 14, 0, 6)
    p.mul(9, 6, 'k_h')                                  # u k, rounded: only for the small terms
    p.mul(15, 11, 13)                                   # yh uh, exact
    p.mul(8, 15, 'k_a')                                 # ... times k's head, exact
    p.mul(15, 15, 'k_b')
    p.mul(9, 9, 12)                                     # yl (u k)
    p.add(9, 9, 15)
    p.mul(15, 14, 'k_h')                                # yh (ul k)
    p.mul(15, 15, 11)
    p.add(9, 9, 15)
    p.mul(15, 6, 10)                                    # y u (kl + u H)
    p.mul(15, 15, T_MY)
    p.add(9, 9, 15)
    p.fin('ADD', 8, 9, use_e=True)

    p.label('log_finish')                               # log2 = T8 (head, may be zero) + T9, plus E
    p.br('EZ', 1, 'log_round')
    p.op('ITOF', 11)                                    # the exponent, and what adding the head drops
    p.add(12, 11, 8)
    p.sub(13, 11, 12)
    p.add(13, 13, 8)
    p.add(9, 9, 13)
    p.mov(8, 12)
    p.label('log_round')
    p.add(12, 8, 9)                                     # G, and the rest: (head - G) + rest
    p.sub(13, 8, 12)
    p.add(13, 13, 9)
    p.op('SETE', arg='YE')
    p.mul(8, 12, T_MY)
    p.mul(9, 13, T_MY)
    p.fin('ADD', 8, 9, use_e=True)

    p.label('log_m')                                    # m in T4, its rest in T5, exponent in E
    p.sub(6, 4, 'one')
    p.add(6, 6, 5)
    p.op('CMP', 0, 6, 'c3_128', 'C')
    p.br('C', 0, 'log_table')
    p.call('log_tail')
    p.br('EZ', 0, 'log_near_exponent')
    p.op('SETE', arg='YE')
    p.jmp('log_split')
    p.label('log_near_exponent')
    p.mul(9, 6, 10)
    p.mul(8, 6, 'k_h')
    p.add(9, 9, 8)
    p.mov(8, 'zero')
    p.jmp('log_finish')
    p.label('log_table')
    p.op('TIDX', 0, 0, 4, 6)
    p.sub(6, 4, f'@lc{LOG_LO}:{LOG_LO}')
    p.add(6, 6, 5)
    p.mul(6, 6, f'@li{LOG_LO}:{LOG_LO}')
    p.call('log_tail')
    p.mul(9, 6, 10)
    p.mul(8, 6, 'k_h')
    p.add(9, 9, 8)
    p.add(9, 9, f'@ll{LOG_LO}_l:{LOG_LO}')
    p.mov(8, f'@ll{LOG_LO}_h:{LOG_LO}')
    p.jmp('log_finish')

    p.entry('FYL2X')
    p.op('LSPL', 4, 0, 0, 1)
    p.mov(5, 'zero')
    p.br('M1', 0, 'log_m')
    p.op('ITOF', 8)                                     # a power of two: y times its exponent
    p.op('SETE', arg='YE')
    p.fin('MUL', T_MY, 8, use_e=True)

    p.entry('FYL2XP1')
    p.br('T68', 0, 'fyl2xp1_normal')
    p.mov(6, T_MX)                                      # y x log2 e, on the significands
    p.mov(10, 'k_l')
    p.op('SETE', arg='XE+YE')
    p.jmp('log_split')
    p.label('fyl2xp1_normal')
    p.op('SETE', arg='ZERO')
    p.op('CMP', 0, T_X, 'c3_128', 'C')
    p.br('C', 0, 'fyl2xp1_sum')
    p.mov(6, T_X)
    p.call('log_tail')
    p.op('SETE', arg='YE')
    p.jmp('log_split')
    p.label('fyl2xp1_sum')
    p.add(4, T_X, 'one')                                # 1 + x, and what the rounding dropped
    p.sub(5, 4, 'one')
    p.sub(5, T_X, 5)
    p.op('LSPL', 4, 0, 4, 0)
    p.op('SCALE', 5, 0, 5, 'NEG')                       # the rest, at the same scale as the head
    p.jmp('log_m')

    # ---- FPATAN. ----
    A_NAMES = [f'a{k}' for k in range(3, 17, 2)]
    p.label('atan_kernel')                              # t in T4, in [2^-70, 1]; atan t = T8 + T9
    p.op('CMP', 0, 4, 'sixteenth', 'C')
    p.br('C', 0, 'atan_table')
    p.mul(7, 4, 4)
    p.horner(9, 7, A_NAMES)
    p.mul(9, 9, 7)
    p.mul(9, 9, 4)
    p.mov(8, 4)
    p.ret()
    p.label('atan_table')
    p.op('TIDX', 0, 0, 4, 5)
    p.sub(6, 4, f'@ac{ATAN_LO}:{ATAN_LO}')              # d = (t - c)/(1 + t c)
    p.mul(7, 4, f'@ac{ATAN_LO}:{ATAN_LO}')
    p.add(7, 7, 'one')
    p.div(6, 6, 7)
    p.mul(7, 6, 6)
    p.horner(9, 7, A_NAMES[:4])
    p.mul(9, 9, 7)
    p.mul(9, 9, 6)
    p.add(9, 9, 6)
    p.add(9, 9, f'@aa{ATAN_LO}_l:{ATAN_LO}')
    p.mov(8, f'@aa{ATAN_LO}_h:{ATAN_LO}')
    p.ret()

    p.entry('FPATAN')
    p.op('ABS', 12, 0, T_MX)
    p.op('ABS', 13, 0, T_MY)
    p.br('YGTX', 1, 'fpatan_steep')
    p.op('SETE', arg='YE-XE')                           # |y| <= |x|: the angle is atan(|y|/|x|)
    p.br('ESMALL', 0, 'fpatan_flat_ratio')
    p.br('XS', 1, 'fpatan_flat_tiny_left')
    p.fin('DIV', 13, 12, use_e=True, negate='YS')       # the angle is the quotient, to its last place
    p.label('fpatan_flat_tiny_left')
    p.mov(8, 'tiny')
    p.mov(9, 'tiny')
    p.jmp('fpatan_flat_left')
    p.label('fpatan_flat_ratio')
    p.div(4, 13, 12)
    p.op('SCALE', 4, 0, 4)
    p.call('atan_kernel')
    p.br('XS', 1, 'fpatan_flat_left')
    p.fin('ADD', 8, 9, negate='YS')
    p.label('fpatan_flat_left')                         # x < 0: pi minus the angle
    p.rsub(9, 9, 'pi_l')
    p.rsub(8, 8, 'pi_h')
    p.fin('ADD', 8, 9, negate='YS')
    p.label('fpatan_steep')
    p.op('SETE', arg='XE-YE')                           # |y| > |x|: pi/2 minus or plus atan(|x|/|y|)
    p.br('ESMALL', 0, 'fpatan_steep_ratio')
    p.mov(8, 'tiny')
    p.mov(9, 'tiny')
    p.jmp('fpatan_steep_join')
    p.label('fpatan_steep_ratio')
    p.div(4, 12, 13)
    p.op('SCALE', 4, 0, 4)
    p.call('atan_kernel')
    p.label('fpatan_steep_join')
    p.br('XS', 1, 'fpatan_steep_left')
    p.rsub(9, 9, 'pi2_l')
    p.rsub(8, 8, 'pi2_h')
    p.fin('ADD', 8, 9, negate='YS')
    p.label('fpatan_steep_left')
    p.add(9, 9, 'pi2_l')
    p.add(8, 8, 'pi2_h')
    p.fin('ADD', 8, 9, negate='YS')

    p.resolve()
    return p


# ---- The model: the engine, run on exact arithmetic. ----
SQRT2_SIG = 0xb504f333f9de6484        # sqrt 2's significand, truncated


class Result:
    """What the engine hands the unit: the last operation's exact value
    before rounding, and the other result of a two-result form."""

    def __init__(self, exact: Fraction, second, steps: int, backend_ops: int):
        self.exact, self.second, self.steps, self.backend_ops = exact, second, steps, backend_ops


def run(program: Program, name: str, x, y=None) -> Result:
    """Run one instruction's microprogram on finite, non-zero operands, as
    the unit enters it. x and y are (sign, exponent, significand) values;
    for a trigonometric form |x| < 2^63."""
    rom = program.rom
    xs, xe, xm = x
    ys, ye, ym = y if y is not None else (0, 0, 1 << 63)
    T: list = [(0, None, 0)] * 16
    T[T_X], T[T_MX] = x, (xs, 0, xm)
    T[T_Y], T[T_MY] = (ys, ye, ym), (ys, 0, ym)
    flag = dict.fromkeys(FLAGS, 0)
    flag.update(XS=xs, YS=ys, T32=int(xe < -32), T68=int(xe < -68),
                YGTX=int((ye, ym) > (xe, xm)))
    E, J, pc, link, steps, issued = 0, 0, program.entries[name], None, 0, 0

    def set_e(v: int) -> int:
        flag['EZ'], flag['EBIG'], flag['ESMALL'] = int(v == 0), int(v > 70), int(v < -70)
        return v

    while True:
        op, dst, a, b, arg = program.words[pc]
        pc += 1
        steps += 1
        assert steps < 2000
        kind, ref = b
        if kind == 't':
            B = T[ref]
        elif kind == 'c':
            B = rom.get(ref)
        else:
            B = rom.get(ref + J)
        A = T[a]
        if op in ('ADD', 'SUB', 'RSUB', 'MUL', 'DIV'):
            issued += 1
            T[dst] = backend(op, A, B)
            assert T[dst][1] is None or E_MIN <= T[dst][1] <= 16383, (name, pc, T[dst])
        elif op == 'MOV':
            T[dst] = B
        elif op == 'NEG':
            T[dst] = (B[0] ^ 1, B[1], B[2])
        elif op == 'ABS':
            T[dst] = (0, B[1], B[2])
        elif op == 'HI':
            T[dst] = hi21(B)
        elif op == 'LO':
            T[dst] = lo43(B)
        elif op == 'SCALE':
            shift = -E if arg == 'NEG' else E
            T[dst] = B if B[1] is None else (B[0], B[1] + shift, B[2])
        elif op == 'ITOF':
            T[dst] = rn(Fraction(E))
        elif op == 'TIDX':
            J = 0 if B[1] is None else int(abs(value(B)) * (1 << arg) + Fraction(1, 2))
            assert J < 256
            flag['J64'] = int(J == 64)
        elif op == 'LSPL':
            s, e, m = x if arg else B
            if m > SQRT2_SIG:
                e += 1
                T[dst] = (0, -1, m)
            else:
                T[dst] = (0, 0, m)
            E = set_e(e)
            flag['M1'] = int(m == 1 << 63)
        elif op == 'SETE':
            E = set_e({'XE': xe, 'YE': ye, 'YE-XE': ye - xe, 'XE-YE': xe - ye, 'XE+YE': xe + ye, 'ZERO': 0}[arg])
        elif op == 'CMP':
            flag[FLAGS[arg]] = int(abs(value(A)) < abs(value(B)))
        elif op == 'TRED':
            # |x| = q P + r exactly, P = PM 2^-67; an odd octant takes P - r.
            if xe >= -1:
                n = xm << (xe + 4)
                q, r = divmod(n, PM)
                if q & 1:
                    r = PM - r
                octant = q & 7
                width = r.bit_length()
                if width <= 64:
                    T[4], T[5] = (0, width - 68, r << (64 - width)), (0, None, 0)
                else:
                    low = r & ((1 << (width - 64)) - 1)
                    T[4] = (0, width - 68, r >> (width - 64))
                    T[5] = rn(Fraction(low, 1 << 67))
            else:
                octant = 0
                T[4], T[5] = (0, xe, xm), (0, None, 0)
            flag['SWAP'] = (octant + 1) >> 1 & 1
            flag['SNEG'] = (octant >> 2) ^ xs
            flag['CNEG'] = (octant + 2) >> 2 & 1
            flag['TNEG'] = flag['SNEG'] ^ flag['CNEG']
        elif op == 'BR':
            which, wanted, target = arg
            if flag[FLAGS[which]] == wanted:
                pc = target
        elif op == 'JMP':
            pc = arg
        elif op == 'CALL':
            assert link is None
            link, pc = pc, arg
        elif op == 'RET':
            pc, link = link, None
        elif op == 'FIN':
            what, use_e, negate, second = arg
            va, vb = value(A), value(B)
            if what in ('ADD', 'SUB', 'MUL', 'DIV'):
                issued += 1
                exact = va + vb if what == 'ADD' else va - vb if what == 'SUB' else va * vb if what == 'MUL' \
                    else va / vb
            elif what == 'MOV':
                exact = va
            else:
                unit = Fraction(2) ** (A[1] - 63)
                step = unit / 8 if what == 'ABOVE' else -unit / 8
                exact = va + (step if va > 0 else -step)
            if use_e:
                exact *= Fraction(2) ** E
            if negate is not None and flag[FLAGS[negate]]:
                exact = -exact
            other = None if second is None else x if second == 'X' else T[second]
            return Result(exact, other, steps, issued)
        else:
            raise AssertionError(op)



# ---- The mathematical result, for the model's own error. ----
def truth(name: str, x, y=None) -> Fraction:
    """The function each instruction computes, to far beyond the format:
    for the trigonometric forms, the function of rounded period (rules.md
    G2), x scaled by (pi/4)/P before the true sine and cosine."""
    vx = value(x)
    if name in ('FSIN', 'FCOS', 'FPTAN', 'FSINCOS'):
        period = Fraction(PM, 1 << 67)
        q, r = divmod(abs(vx), period)
        angle = fx(r * frac(PI) / 4 / period)
        octant = int(q) & 7
        if octant & 1:
            angle = PI // 4 - angle
        sin, cos = fx_sin_cos(angle)
        if (octant + 1) >> 1 & 1:
            sin, cos = cos, sin
        if octant >> 2:
            sin = -sin
        if (octant + 2) >> 2 & 1:
            cos = -cos
        if vx < 0:
            sin = -sin
        return {'FSIN': frac(sin), 'FCOS': frac(cos), 'FSINCOS': frac(sin),
                'FPTAN': Fraction(sin, cos)}[name]
    if name == 'F2XM1':
        return frac(fx_exp(fx_mul(fx(vx), LN2))) - 1
    vy = value(y)
    if name in ('FYL2X', 'FYL2XP1'):
        v = vx + 1 if name == 'FYL2XP1' else vx
        if name == 'FYL2XP1' and abs(vx) < Fraction(1, 1 << 40):
            # log2(1 + x) for a tiny x, by its series: the fixed point above cannot hold 1 + x.
            return vy * vx * (1 - vx / 2 + vx * vx / 3) / frac(LN2)
        e = v.numerator.bit_length() - v.denominator.bit_length()
        m = v / Fraction(2) ** e
        return vy * (e + frac(fx_ln(fx(m))) / frac(LN2))
    # FPATAN
    ax, ay = abs(vx), abs(vy)
    if ay <= ax:
        ratio = ay / ax
        angle = frac(fx_atan(fx(ratio))) if ratio > Fraction(1, 1 << 100) else ratio
        if vx < 0:
            angle = frac(PI) - angle
    else:
        ratio = ax / ay
        inner = frac(fx_atan(fx(ratio))) if ratio > Fraction(1, 1 << 100) else ratio
        angle = frac(PI) / 2 - inner if vx > 0 else frac(PI) / 2 + inner
    return -angle if vy < 0 else angle


def ulps(exact: Fraction, reference: Fraction) -> Fraction:
    """exact - reference in units of the reference's last place at 64 bits."""
    if reference == 0:
        return Fraction(0)
    mag = abs(reference)
    e = mag.numerator.bit_length() - mag.denominator.bit_length()
    if Fraction(2) ** e > mag:
        e -= 1
    return (exact - reference) / Fraction(2) ** (max(e, E_MIN) - 63)


def ordinal(sign_exponent: int, significand: int):
    field = sign_exponent & 0x7fff
    if field == 0x7fff or significand == 0 or bool(significand >> 63) != bool(field):
        return None
    return sign_exponent >> 15, significand if field == 0 else ((field - 1) << 63) + significand


TWO = ('FYL2X', 'FPATAN', 'FYL2XP1')


def compare_with_host(program: Program, dump: str, limit: int, with_truth: bool) -> dict:
    """The model against one host's dump, per instruction and operand class:
    the whole histogram of the distance between the model's result and the
    host's, in representable values, and the model's own error against the
    mathematical result, before and after its last rounding."""
    import hashlib
    seen: dict = {}
    rows: dict = {}
    header = ''
    for line in open(dump):
        if line.startswith('#'):
            header = line[1:].strip()
            continue
        name, cls, ta, tb, r0, r1, _ = line.split()
        key = (name, int(cls))
        seen[key] = seen.get(key, 0) + 1
        if seen[key] > limit:
            continue
        a = from_bits(int(ta[:4], 16), int(ta[4:], 16))
        b = from_bits(int(tb[:4], 16), int(tb[4:], 16))
        # The dump's first operand is the one loaded first: ST(1) of a two-operand form.
        x, y = (b, a) if name in TWO else (a, None)
        row = rows.setdefault(key, {'rows': 0, 'outside_engine': 0, 'compared': 0, 'not_a_distance': 0,
                                    'host_distance': {}, 'worst_host_distance': 0, 'backend_operations_most': 0,
                                    'steps_most': 0, 'error_before_rounding': Fraction(0),
                                    'error_after_rounding': Fraction(0)})
        row['rows'] += 1
        special = (x[1] is None or (y is not None and y[1] is None)
                   or any(int(t[:4], 16) & 0x7fff == 0x7fff for t in (ta, tb))
                   or (name == 'FYL2X' and x[0]) or (name == 'FYL2XP1' and value(x) <= -1)
                   or (name == 'F2XM1' and abs(value(x)) > 1)
                   or (name == 'FYL2X' and x[2] == 1 << 63 and x[1] == 0))
        if special:
            # An operand the unit answers without the engine, or one outside
            # the instruction's range: not this model's to compute.
            row['outside_engine'] += 1
            continue
        result = run(program, name, x, y)
        got = final_round(result.exact, 0)
        host = r1 if name in ('FPTAN', 'FSINCOS') else r0   # ST(1): the tangent, the sine
        host_bits = (int(host[:4], 16), int(host[4:], 16))
        g, h = ordinal(got[0], got[1]), ordinal(*host_bits)
        row['compared'] += 1
        row['backend_operations_most'] = max(row['backend_operations_most'], result.backend_ops)
        row['steps_most'] = max(row['steps_most'], result.steps)
        if g is None or h is None or g[0] != h[0]:
            if (got[0], got[1]) == host_bits:
                row['host_distance'][0] = row['host_distance'].get(0, 0) + 1
            else:
                row['not_a_distance'] += 1
        else:
            distance = abs(g[1] - h[1])
            row['host_distance'][distance] = row['host_distance'].get(distance, 0) + 1
            row['worst_host_distance'] = max(row['worst_host_distance'], distance)
        if with_truth:
            exact = truth(name, x, y)
            row['error_before_rounding'] = max(row['error_before_rounding'], abs(ulps(result.exact, exact)))
            rounded = value(from_bits(got[0], got[1]))
            row['error_after_rounding'] = max(row['error_after_rounding'], abs(ulps(rounded, exact)))
    classes = []
    for (name, cls), row in sorted(rows.items()):
        row['host_distance'] = {str(k): v for k, v in sorted(row['host_distance'].items())}
        for key in ('error_before_rounding', 'error_after_rounding'):
            row[key] = round(float(row[key]), 4) if with_truth else None
        classes.append({'instruction': name, 'class': cls, **row})
    return {'what': "the microprogram's integer model against a host's own x87 and against the mathematical "
                    'result; no RTL',
            'dump': {'header': header, 'sha256': hashlib.sha256(open(dump, 'rb').read()).hexdigest()},
            'rows_per_class_limit': limit, 'microprogram_words': len(program.words),
            'constants': len(program.rom.words),
            'model_sha256': hashlib.sha256(open(__file__, 'rb').read()).hexdigest(), 'classes': classes}


def compare_specials(program: Program, path: str) -> dict:
    """The model against a reference's `x87_trans_special` lines: every case
    the engine is entered for with the exceptions masked -- denormals, the
    range edges, results that overflow or underflow, each rounding mode --
    comparing the value, and PE, UE and OE exactly. The model does not have
    the unmasked overflow and underflow results, which are the unit's A_RND
    and not the engine's, so the unmasked lines are not compared here."""
    import re
    kinds: dict = {}
    examples: dict = {}
    for line in open(path):
        head, tail = line.rstrip('\n').split(' | ')
        name, cw_text = head.split()[:2]
        cw = int(cw_text, 16)
        m = re.match(r'a=(\w+) b=(\w+) stack=(\d) sw=(\w+) tw=(\w+) st0=(\w+) st1=(\w+)', tail)
        ta, tb, stack, sw = m[1], m[2], int(m[3]), int(m[4], 16)
        if stack != 0 or cw & 0x3f != 0x3f:
            continue
        def unsupported(t):
            field, sig = int(t[:4], 16) & 0x7fff, int(t[4:], 16)
            return field == 0x7fff or (field != 0 and not sig >> 63) or sig == 0
        if unsupported(tb) or (name in TWO and unsupported(ta)):
            continue
        x = from_bits(int(tb[:4], 16), int(tb[4:], 16))
        y = from_bits(int(ta[:4], 16), int(ta[4:], 16)) if name in TWO else None
        if name in ('FSIN', 'FCOS', 'FSINCOS', 'FPTAN') and x[1] >= 63:
            continue
        if (name == 'FYL2X' and (x[0] or (x[1] == 0 and x[2] == 1 << 63))) or \
           (name == 'FYL2XP1' and value(x) <= -1):
            continue
        result = run(program, name, x, y)
        got = final_round(result.exact, cw >> 10 & 3)
        host = m[7] if name in ('FPTAN', 'FSINCOS') else m[6]
        host_bits = (int(host[:4], 16), int(host[4:], 16))
        g, h = ordinal(got[0], got[1]), ordinal(*host_bits)
        if (got[0], got[1]) == host_bits:
            distance = 0
        elif g is None or h is None or g[0] != h[0]:
            distance = 'not a distance'
        else:
            distance = abs(g[1] - h[1])
        # The instruction is inexact whatever its last operation was (rules.md
        # G6), so PE is always set, and with it UE where the result is tiny.
        mine = got[2] | 0x20 | (0x10 if not got[0] & 0x7fff else 0)
        flags = 'flags same' if mine == sw & 0x38 else f'flags {mine:02x} against {sw & 0x38:02x}'
        key = f'{name}: distance {distance if distance in (0, 1, 2, "not a distance") else "over 2"}, {flags}'
        kinds[key] = kinds.get(key, 0) + 1
        if distance not in (0, 1) or flags != 'flags same':
            examples.setdefault(key, []).append(f'{head} model {got[0]:04x}{got[1]:016x} host {host}')
    return {'kinds': dict(sorted(kinds.items())), 'examples': examples}


# ---- The ROMs fes_x87_trans reads. ----
OPCODES = ('NOP', 'ADD', 'SUB', 'RSUB', 'MUL', 'DIV', 'MOV', 'NEG', 'ABS', 'HI', 'LO', 'SCALE', 'ITOF', 'TIDX',
           'LSPL', 'SETE', 'CMP', 'TRED', 'BR', 'JMP', 'CALL', 'RET', 'FIN')
FIN_KINDS = ('ADD', 'SUB', 'MUL', 'DIV', 'MOV', 'BELOW', 'ABOVE')
SETE_KINDS = ('XE', 'YE', 'YE-XE', 'XE-YE', 'XE+YE', 'ZERO')
INSTRUCTIONS = ('FSIN', 'FCOS', 'FSINCOS', 'FPTAN', 'F2XM1', 'FYL2X', 'FYL2XP1', 'FPATAN')
WORD_BITS, PROGRAM_DEPTH, CONSTANT_DEPTH = 40, 512, 1024


def encode(word) -> int:
    """A microprogram word: op [4:0], dst [8:5], a [12:9], b's mode [14:13]
    (0 a temporary, 1 a constant, 2 a constant at base + J) and address
    [24:15]; for FIN, add E [25], the negating flag [30:27] and whether there
    is one [26], and the other result [32:31] (0 none, 1 a temporary, 2 the x
    operand) and its temporary [36:33]."""
    op, dst, a, (mode, ref), arg = word
    use_e = neg_valid = neg_flag = sec_mode = sec_temp = 0
    if op == 'TIDX':
        dst = arg
    elif op == 'LSPL':
        a = arg
    elif op == 'SETE':
        a = SETE_KINDS.index(arg)
    elif op == 'SCALE':
        a = int(arg == 'NEG')
    elif op == 'CMP':
        assert FLAGS[arg] == 'C'
    elif op == 'BR':
        a, dst, ref, mode = arg[0], arg[1], arg[2], 't'
    elif op in ('JMP', 'CALL'):
        ref, mode = arg, 't'
    elif op == 'FIN':
        kind, use_e, negate, second = arg
        dst = FIN_KINDS.index(kind)
        if negate is not None:
            neg_valid, neg_flag = 1, negate
        if second == 'X':
            sec_mode = 2
        elif second is not None:
            sec_mode, sec_temp = 1, second
    assert 0 <= dst < 16 and 0 <= a < 16 and 0 <= ref < 1024, word
    return (OPCODES.index(op) | dst << 5 | a << 9 | {'t': 0, 'c': 1, 'j': 2}[mode] << 13 | ref << 15
            | int(use_e) << 25 | neg_valid << 26 | neg_flag << 27 | sec_mode << 31 | sec_temp << 33)


def emit(program: Program) -> dict[str, str]:
    """The generated SystemVerilog, by file name."""
    assert len(program.words) <= PROGRAM_DEPTH and len(program.rom.words) <= CONSTANT_DEPTH
    head = ['// SPDX-License-Identifier: MIT',
            '// Generated by scripts/x87_trans_ucode.py; do not edit.', '//']
    package = head + [
        '// The x87 transcendentals\' microprogram: its operations, its flags, each',
        '// instruction\'s entry, and the reduction constant. The microprogram and what',
        '// each word means are in scripts/x87_trans_ucode.py, which also holds the',
        '// exact model the engine is checked against.',
        'package fes_x87_trans_pkg;',
        '    // Every name is listed, whether or not the engine reads it.',
        '    /* verilator lint_off UNUSEDPARAM */']
    package += [f'    localparam logic [4:0] U_{name} = 5\'d{n};' for n, name in enumerate(OPCODES)]
    package += [f'    localparam logic [2:0] UF_{name} = 3\'d{n};' for n, name in enumerate(FIN_KINDS)]
    package += [f'    localparam logic [3:0] FL_{name} = 4\'d{n};' for n, name in enumerate(FLAGS)]
    package += [f'    localparam logic [2:0] TR_{name} = 3\'d{n};' for n, name in enumerate(INSTRUCTIONS)]
    package += ['    // Each instruction\'s entry, indexed by TR_*.',
                '    function automatic logic [8:0] entry_of(input logic [2:0] instruction);',
                '        case (instruction)']
    package += [f'            3\'d{n}: entry_of = 9\'d{program.entries[name]};' for n, name in enumerate(INSTRUCTIONS)]
    package += ['            default: entry_of = 9\'d0;', '        endcase', '    endfunction',
                '    // pi/4 to 67 significant bits, the 387\'s reduction constant, times 2^67.',
                f'    localparam logic [66:0] PM = 67\'h{PM:017x};',
                '    // sqrt 2\'s significand, truncated: a significand above it is halved.',
                f'    localparam logic [63:0] SQRT2 = 64\'h{SQRT2_SIG:016x};',
                '    /* verilator lint_on UNUSEDPARAM */',
                'endpackage', '']
    rom = head + [
        '// The engine\'s two memories, each read with a registered output: the',
        f'// microprogram, {len(program.words)} words of {WORD_BITS} bits, and the constants, {len(program.rom.words)} values of 80.',
        'module fes_x87_trans_rom (',
        '    input  logic        clk,',
        '    input  logic [8:0]  pc,',
        '    output logic [39:0] word,',
        '    input  logic [9:0]  address,',
        '    output logic [79:0] constant',
        ');',
        f'    (* ramstyle = "M20K" *) logic [{WORD_BITS - 1}:0] program_rom [0:{PROGRAM_DEPTH - 1}];',
        f'    (* ramstyle = "M20K" *) logic [79:0] constant_rom [0:{CONSTANT_DEPTH - 1}];',
        '    initial begin']
    rom += [f'        program_rom[{n}] = 40\'h{(encode(program.words[n]) if n < len(program.words) else 0):010x};'
            for n in range(PROGRAM_DEPTH)]
    rom += [f'        constant_rom[{n}] = 80\'h'
            + (f'{program.rom.words[n][0]:04x}{program.rom.words[n][1]:016x}' if n < len(program.rom.words)
               else '0' * 20) + ';' for n in range(CONSTANT_DEPTH)]
    rom += ['    end', '    always_ff @(posedge clk) begin', '        word <= program_rom[pc];',
            '        constant <= constant_rom[address];', '    end', 'endmodule', '']
    return {'fes_x87_trans_pkg.sv': '\n'.join(package), 'fes_x87_trans_rom.sv': '\n'.join(rom)}


def raw_of(exact: Fraction) -> tuple[int, int, int, int]:
    """A value as the engine hands it to the unit: (zero, sign, the biased
    exponent as 18 signed bits, the 67-bit significand, whose low three bits
    are the guard, the round and the sticky)."""
    if exact == 0:
        return 1, 0, 0, 0
    sign, mag = (1, -exact) if exact < 0 else (0, exact)
    exponent = mag.numerator.bit_length() - mag.denominator.bit_length()
    if Fraction(2) ** exponent > mag:
        exponent -= 1
    scaled = mag / Fraction(2) ** (exponent - 66)
    whole = scaled.numerator // scaled.denominator
    if scaled != whole:
        whole |= 1
    return 0, sign, (exponent + BIAS) & 0x3ffff, whole


def vectors(program: Program, dump: str, per_class: int) -> list[str]:
    """Engine vectors from a host dump's operands: the instruction, x and y as
    the unit normalises them, and what the engine must hand back."""
    lines, seen = [], {}
    for line in open(dump):
        if line.startswith('#'):
            continue
        name, cls, ta, tb = line.split()[:4]
        seen[(name, cls)] = seen.get((name, cls), 0) + 1
        if seen[(name, cls)] > per_class:
            continue
        a = from_bits(int(ta[:4], 16), int(ta[4:], 16))
        b = from_bits(int(tb[:4], 16), int(tb[4:], 16))
        x, y = (b, a) if name in TWO else (a, (0, 0, 1 << 63))
        if (x[1] is None or y[1] is None or any(int(t[:4], 16) & 0x7fff == 0x7fff for t in (ta, tb))
                or (name == 'FYL2X' and (x[0] or (x[2] == 1 << 63 and x[1] == 0)))
                or (name == 'FYL2XP1' and value(x) <= -1)):
            continue
        result = run(program, name, x, y)
        zero, sign, exponent, significand = raw_of(result.exact)
        second = result.second
        mode = 0 if second is None else 2 if second is x else 1
        bits = to_bits(second) if mode == 1 else (0, 0)
        lines.append(f'{INSTRUCTIONS.index(name)} {x[0]} {x[1] & 0x3ffff:05x} {x[2]:016x} '
                     f'{y[0]} {y[1] & 0x3ffff:05x} {y[2]:016x} '
                     f'{zero} {sign} {exponent:05x} {significand:017x} {mode} {bits[0]:04x}{bits[1]:016x}')
    return lines


def unit_vectors(program: Program, per_class: int, seed: int) -> list[str]:
    """Instruction-level vectors for the unit bench, from operands of this
    script's own: the instruction, the control word (every exception masked,
    each rounding mode), x and y as 80-bit values, and the model's result,
    rounded as the unit's A_RND rounds it, with PE, UE and OE.
    "<instruction> <cw> <x> <y> <ST(0)> <ST(1) or -> <flags>"."""
    import random
    rng = random.Random(seed)

    def operand(low: int, high: int, sign: int = 0):
        exponent = rng.randint(low, high)
        negative = sign < 0 or (sign == 0 and rng.random() < 0.5)
        return from_bits((0x8000 if negative else 0) | max(exponent + BIAS, 0),
                         (rng.getrandbits(64) | 1 << 63) >> max(0, -(exponent + BIAS) + 1))

    def bits(f) -> str:
        sign, exponent, significand = f
        if exponent < E_MIN:                           # a denormal, back in its own encoding
            return f'{sign << 15:04x}{significand >> (E_MIN - exponent):016x}'
        return f'{sign << 15 | (exponent + BIAS):04x}{significand:016x}'

    classes = {
        'FSIN': [(-8, -1), (0, 9), (10, 62), (-70, -9), (-16440, -16300)],
        'F2XM1': [(-8, -1), (-70, -9), (-16440, -16300), (0, 3)],
    }
    for name in ('FCOS', 'FSINCOS', 'FPTAN'):
        classes[name] = classes['FSIN']
    lines = []
    for name in INSTRUCTIONS:
        for index in range(per_class * 4):
            rc = index & 3
            y = (0, 0, 1 << 63)
            if name in classes:
                low, high = classes[name][(index >> 2) % len(classes[name])]
                x = operand(low, high)
            elif name == 'FYL2X':
                kind = (index >> 2) % 4
                y = operand(-4, 3) if kind < 3 else operand(-16440, 16383)
                x = operand(-4, 3, 1) if kind == 0 else operand(-16440, 16383, 1) if kind == 1 else \
                    (0, rng.randint(-40, 40), 1 << 63) if kind == 2 else operand(-1, 0, 1)
                if x[2] == 1 << 63 and x[1] == 0:
                    continue                           # exactly 1: the unit's, not the engine's
            elif name == 'FYL2XP1':
                kind = (index >> 2) % 4
                y = operand(-4, 3) if kind < 3 else operand(-16440, 16383)
                x = operand(-8, -2) if kind == 0 else operand(-70, -9) if kind == 1 else \
                    operand(-16440, -16300) if kind == 2 else operand(-2, 40, 1)
            else:
                kind = (index >> 2) % 4
                y = operand(-4, 3) if kind == 0 else operand(-70, -9) if kind == 1 else operand(-16440, 16383)
                x = operand(-4, 3) if kind < 2 else operand(-16440, 16383)
                if kind == 3:
                    x = (rng.getrandbits(1), y[1], y[2])   # |y| = |x|
            result = run(program, name, x, y)
            sign_exponent, significand, flags = final_round(result.exact, rc)
            flags |= 0x20 | (0x10 if not sign_exponent & 0x7fff and significand else 0)
            final = f'{sign_exponent:04x}{significand:016x}'
            if name == 'FSINCOS':
                st0, st1 = bits(result.second), final
            elif name == 'FPTAN':
                st0, st1 = '3fff8000000000000000', final
            else:
                st0, st1 = final, '-'
            lines.append(f'{INSTRUCTIONS.index(name)} {0x037f | rc << 10:04x} {bits(x)} {bits(y)} {st0} {st1} '
                         f'{flags:02x}')
    return lines


def main() -> int:
    import json
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--hosts', help='a `x87_trans_probe --host-dump` file to compare the model with')
    parser.add_argument('--limit', type=int, default=2000, help='rows a class from the dump (default 2000)')
    parser.add_argument('--truth', action='store_true', help="with --hosts: also the model's error against the "
                                                             'mathematical result')
    parser.add_argument('--report', help='with --hosts: write the comparison here as JSON')
    parser.add_argument('--specials', help='a `x87_trans_special` output to compare the model with')
    parser.add_argument('--emit', action='store_true', help="write the engine's generated SystemVerilog")
    parser.add_argument('--check', action='store_true', help='fail if the checked-in generated files differ')
    parser.add_argument('--vectors', help='with --hosts: write engine vectors here, --limit rows a class')
    parser.add_argument('--unit-vectors', help='write instruction-level vectors for the unit bench here, '
                                               '--limit rows a class and rounding mode')
    parser.add_argument('--seed', type=int, default=1958)
    args = parser.parse_args()
    rom = build_rom()
    program = build_program(rom)
    print(f'{len(program.words)} microprogram words, {len(rom.words)} constants', file=sys.stderr)
    if args.emit or args.check:
        from pathlib import Path
        rtl = Path(__file__).resolve().parents[1] / 'cores/fes-common/rtl/i386'
        stale = []
        for name, text in emit(program).items():
            if args.check:
                if not (rtl / name).is_file() or (rtl / name).read_text() != text:
                    stale.append(name)
            else:
                (rtl / name).write_text(text)
        if stale:
            print('stale generated files: ' + ', '.join(stale), file=sys.stderr)
            return 1
    if args.unit_vectors:
        with open(args.unit_vectors, 'w') as out:
            out.write('\n'.join(unit_vectors(program, args.limit, args.seed)) + '\n')
        return 0
    if args.vectors:
        with open(args.vectors, 'w') as out:
            out.write('\n'.join(vectors(program, args.hosts, args.limit)) + '\n')
        return 0
    if args.specials:
        result = compare_specials(program, args.specials)
        for kind, count in result['kinds'].items():
            print(f'{count:5} {kind}')
            for example in result['examples'].get(kind, [])[:4]:
                print('        ' + example)
    if not args.hosts:
        return 0
    report = compare_with_host(program, args.hosts, args.limit, args.truth)
    if args.report:
        with open(args.report, 'w') as out:
            out.write(json.dumps(report, indent=2) + '\n')
    for row in report['classes']:
        hist = ' '.join(f'{k}:{v}' for k, v in row['host_distance'].items())
        print(f'{row["instruction"]:8} class {row["class"]:>2} compared={row["compared"]:5} '
              f'outside={row["outside_engine"]:4} ops<={row["backend_operations_most"]:3} host-distance {hist} '
              f'not-a-distance={row["not_a_distance"]}'
              + (f' error<={row["error_before_rounding"]}/{row["error_after_rounding"]}ulp' if args.truth else ''))
    return 0


if __name__ == '__main__':
    sys.exit(main())
