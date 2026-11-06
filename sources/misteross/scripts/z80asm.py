#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Small two-pass Z80 assembler for the open ZX81 ROM.

Syntax: one statement per line, `label:` definitions, `.local` labels scoped to
the previous global label, `name equ expr`, `org expr`, `db`, `dw`, `ds`,
`dz "TEXT"` (ZX81 character codes, or ZX80 after `charset zx80`; last byte
with bit 7 set), `assert expr`
and `include "file"`. Expressions use Python operators on
integers plus `$` for the current address and 'c' character constants.
Overlapping `org` blocks are an error, so fixed ROM entry points cannot be
overwritten by floating code. A `jr` whose target is out of range is
assembled as the equivalent `jp`.

Code between `float` and `endfloat` has no fixed address: after each pass
the sections are placed into the bytes below `floatlimit` that no fixed (org)
code uses. Two layouts are tried and the one that fits, and then is smallest,
is kept: largest first and best fit; or the sections joined by a `jr` in
source order through the largest gaps (so the `jr` stays short), with the
rest filling the holes left, smallest hole first, each with the sections
that fill it best. A source with `floatorder` tries a third layout: the
sections in the order (found by a seeded annealing search) that leaves the
fewest `jr` out of range, through the gaps in address order; and a `jr` out
of range goes by way of a direct jump to the same place within its reach
before it becomes a `jp`. A section must
not fall through into the next one. Between `autofloat` and `endautofloat` a
new section starts at every global label that follows an unconditional
jump or return (a label after data continues the same table).
"""
from __future__ import annotations
import argparse
import bisect
import math
import random
import re
import sys
from pathlib import Path

R8 = {'b': 0, 'c': 1, 'd': 2, 'e': 3, 'h': 4, 'l': 5, 'a': 7}
RP = {'bc': 0, 'de': 1, 'hl': 2, 'sp': 3}
RPAF = {'bc': 0, 'de': 1, 'hl': 2, 'af': 3}
CC = {'nz': 0, 'z': 1, 'nc': 2, 'c': 3, 'po': 4, 'pe': 5, 'p': 6, 'm': 7}
ALU = {'add': 0, 'adc': 1, 'sub': 2, 'sbc': 3, 'and': 4, 'xor': 5, 'or': 6, 'cp': 7}
ROT = {'rlc': 0, 'rrc': 1, 'rl': 2, 'rr': 3, 'sla': 4, 'sra': 5, 'sll': 6, 'srl': 7}
SIMPLE = {
    'nop': [0x00], 'rlca': [0x07], 'rrca': [0x0f], 'rla': [0x17], 'rra': [0x1f],
    'daa': [0x27], 'cpl': [0x2f], 'scf': [0x37], 'ccf': [0x3f], 'halt': [0x76],
    'exx': [0xd9], 'di': [0xf3], 'ei': [0xfb], 'neg': [0xed, 0x44],
    'retn': [0xed, 0x45], 'reti': [0xed, 0x4d], 'rrd': [0xed, 0x67], 'rld': [0xed, 0x6f],
    'ldi': [0xed, 0xa0], 'cpi': [0xed, 0xa1], 'ini': [0xed, 0xa2], 'outi': [0xed, 0xa3],
    'ldd': [0xed, 0xa8], 'cpd': [0xed, 0xa9], 'ind': [0xed, 0xaa], 'outd': [0xed, 0xab],
    'ldir': [0xed, 0xb0], 'cpir': [0xed, 0xb1], 'inir': [0xed, 0xb2], 'otir': [0xed, 0xb3],
    'lddr': [0xed, 0xb8], 'cpdr': [0xed, 0xb9], 'indr': [0xed, 0xba], 'otdr': [0xed, 0xbb],
}
# How many operands each instruction takes.
OPERANDS = {'im': (1,), 'rst': (1,), 'ex': (2,), 'push': (1,), 'pop': (1,), 'inc': (1,),
            'dec': (1,), 'djnz': (1,), 'ret': (0, 1), 'jp': (1, 2), 'call': (1, 2),
            'jr': (1, 2), 'bit': (2,), 'set': (2,), 'res': (2,), 'in': (2,), 'out': (2,),
            'ld': (2,)}
OPERANDS.update({m: (1, 2) for m in ALU})
OPERANDS.update({m: (1,) for m in ROT})


ZX81_TEXT = {' ': 0, '"': 11, '£': 12, '$': 13, ':': 14, '?': 15, '(': 16,
             ')': 17, '>': 18, '<': 19, '=': 20, '+': 21, '-': 22, '*': 23,
             '/': 24, ';': 25, ',': 26, '.': 27}
ZX81_TEXT.update({str(d): 28 + d for d in range(10)})
ZX81_TEXT.update({chr(65 + i): 38 + i for i in range(26)})
ZX80_TEXT = {' ': 0, '"': 1, '£': 12, '$': 13, ':': 14, '?': 15, '(': 16,
             ')': 17, '-': 18, '+': 19, '*': 20, '/': 21, '=': 22, '>': 23,
             '<': 24, ';': 25, ',': 26, '.': 27}
ZX80_TEXT.update({str(d): 28 + d for d in range(10)})
ZX80_TEXT.update({chr(65 + i): 38 + i for i in range(26)})
CHARSETS = {'zx81': ZX81_TEXT, 'zx80': ZX80_TEXT}


class AsmError(Exception):
    pass


def split_operands(text: str) -> list[str]:
    parts, depth, quote, current = [], 0, None, ''
    for ch in text:
        if quote:
            current += ch
            if ch == quote:
                quote = None
        elif ch in '"\'':
            quote = ch; current += ch
        elif ch == '(':
            depth += 1; current += ch
        elif ch == ')':
            depth -= 1; current += ch
        elif ch == ',' and depth == 0:
            parts.append(current.strip()); current = ''
        else:
            current += ch
    if current.strip():
        parts.append(current.strip())
    return parts


def strip_comment(line: str) -> str:
    quote = None
    for i, ch in enumerate(line):
        if quote:
            if ch == quote:
                quote = None
        elif ch in '"\'':
            # 'af'' is the only apostrophe that is not a quote.
            if ch == "'" and line[max(0, i-2):i].lower() == 'af':
                continue
            quote = ch
        elif ch == ';':
            return line[:i]
    return line


class Assembler:
    def __init__(self, include_dirs=()):
        self.symbols: dict[str, int] = {}
        self.final_pass = False
        self.include_dirs = [Path(d) for d in include_dirs]
        self.long_jumps: set[str] = set()
        self.float_at: dict[int, int] = {}     # section index -> address
        self.float_size: dict[int, int] = {}
        self.float_limit = None
        self.float_limit_override = None
        self.where = ''
        self.grew = False
        self.chaining = False
        self.chains, self.jumps = {}, {}

    # -- expressions -------------------------------------------------------
    def value(self, expr: str) -> int:
        expr = expr.strip()
        def char(m):
            return str(ord(m.group(1)))
        text = re.sub(r"'(.)'", char, expr)
        text = re.sub(r'\$([0-9a-fA-F]+)\b', lambda m: str(int(m.group(1), 16)), text)
        text = re.sub(r'\b([0-9][0-9a-fA-F]*)h\b', lambda m: str(int(m.group(1), 16)), text)
        text = re.sub(r'%([01]+)\b', lambda m: str(int(m.group(1), 2)), text)
        text = text.replace('$', str(self.pc))
        def name(m):
            word = m.group(0)
            if word[0].isdigit():
                return word
            key = self.scoped(word)
            if key in self.symbols:
                return str(self.symbols[key])
            if self.final_pass:
                raise AsmError(f'undefined symbol {word}')
            return '0'
        text = re.sub(r'\.?[A-Za-z_][A-Za-z0-9_.]*|\b0x[0-9a-fA-F]+\b|\b\d+\b', name, text)
        try:
            return int(eval(text, {'__builtins__': {}}, {}))
        except Exception as exc:
            raise AsmError(f'bad expression {expr!r}: {exc}') from None

    def scoped(self, word: str) -> str:
        return self.scope + word if word.startswith('.') else word

    # -- operand classification -------------------------------------------
    def indexed(self, op: str):
        """(ix+d) / (iy+d) -> (prefix, displacement)."""
        m = re.fullmatch(r'\(\s*(ix|iy)\s*(?:([+-])(.*))?\)', op, re.I)
        if not m:
            return None
        prefix = 0xdd if m.group(1).lower() == 'ix' else 0xfd
        disp = 0
        if m.group(2):
            disp = self.value(m.group(2) + m.group(3))   # the sign is the first term's
        if self.final_pass and not -128 <= disp <= 127:
            raise AsmError('index displacement out of range')
        return prefix, disp & 0xff

    @staticmethod
    def memory(op: str):
        if op.startswith('(') and op.endswith(')'):
            depth = 0
            for i, ch in enumerate(op):
                depth += ch == '('
                depth -= ch == ')'
                if depth == 0 and i != len(op) - 1:
                    return None
            return op[1:-1].strip()
        return None

    def r8(self, op: str):
        """Return (prefix, code, displacement) for an 8-bit register operand."""
        low = op.lower()
        if low in R8:
            return None, R8[low], None
        if low == '(hl)':
            return None, 6, None
        if low in ('ixh', 'ixl', 'iyh', 'iyl'):
            return (0xdd if low[1] == 'x' else 0xfd), (4 if low[2] == 'h' else 5), None
        ix = self.indexed(op)
        if ix:
            return ix[0], 6, ix[1]
        return None

    # -- emission ----------------------------------------------------------
    def word(self, value):
        if self.final_pass and not -0x8000 <= value <= 0xffff:
            raise AsmError(f'word out of range: {value:#x}')
        value &= 0xffff
        return [value & 0xff, value >> 8]

    def byte(self, expr):
        value = self.value(expr)
        if self.final_pass and not -128 <= value <= 255:
            raise AsmError(f'byte out of range: {expr}')
        return value & 0xff

    def relative(self, expr, length):
        target = self.value(expr)
        offset = target - (self.pc + length)
        if self.final_pass and not -128 <= offset <= 127:
            raise AsmError(f'relative jump out of range ({offset})')
        return offset & 0xff

    def encode(self, mnemonic: str, ops: list[str]) -> list[int]:
        m = mnemonic.lower()
        lows = [o.lower().replace(' ', '') for o in ops]
        if m in SIMPLE and not ops:
            return list(SIMPLE[m])
        if m in OPERANDS and len(ops) not in OPERANDS[m]:
            raise AsmError(f'{m} takes {" or ".join(map(str, OPERANDS[m]))} operands')
        if m == 'im':
            return [0xed, {0: 0x46, 1: 0x56, 2: 0x5e}[self.value(ops[0])]]
        if m == 'rst':
            v = self.value(ops[0])
            if self.final_pass and v not in range(0, 0x40, 8):
                raise AsmError(f'rst needs 0x00 to 0x38 in steps of 8: {ops[0]}')
            return [0xc7 | v & 0x38]
        if m == 'ex':
            pair = tuple(lows)
            table = {('de', 'hl'): [0xeb], ('af', "af'"): [0x08], ('(sp)', 'hl'): [0xe3],
                     ('(sp)', 'ix'): [0xdd, 0xe3], ('(sp)', 'iy'): [0xfd, 0xe3]}
            return table[pair]
        if m in ('push', 'pop'):
            base = 0xc5 if m == 'push' else 0xc1
            if lows[0] in ('ix', 'iy'):
                return [0xdd if lows[0] == 'ix' else 0xfd, base | 0x20]
            return [base | RPAF[lows[0]] << 4]
        if m in ('jp', 'call', 'jr', 'djnz', 'ret'):
            return self.flow(m, ops, lows)
        if m in ALU:
            return self.alu(m, ops, lows)
        if m in ('inc', 'dec'):
            if lows[0] in RP:
                return [(0x03 if m == 'inc' else 0x0b) | RP[lows[0]] << 4]
            if lows[0] in ('ix', 'iy'):
                return [0xdd if lows[0] == 'ix' else 0xfd, 0x23 if m == 'inc' else 0x2b]
            pre, code, disp = self.r8(ops[0])
            out = [(0x04 if m == 'inc' else 0x05) | code << 3]
            return self.prefixed(pre, out, disp)
        if m in ROT or m in ('bit', 'set', 'res'):
            if m in ROT:
                operand, base = ops[0], ROT[m] << 3
            else:
                bit = self.value(ops[0])
                if self.final_pass and not 0 <= bit <= 7:
                    raise AsmError(f'bit number out of range: {ops[0]}')
                bit &= 7
                operand = ops[1]
                base = {'bit': 0x40, 'res': 0x80, 'set': 0xc0}[m] | bit << 3
            pre, code, disp = self.r8(operand)
            if pre is not None:
                return [pre, 0xcb, disp, base | code]
            return [0xcb, base | code]
        if m == 'in':
            if lows[1] == '(c)':
                return [0xed, 0x40 | R8[lows[0]] << 3]
            if lows[0] != 'a':
                raise AsmError('in r,(n) needs A')
            return [0xdb, self.byte(self.port(ops[1]))]
        if m == 'out':
            if lows[0] == '(c)':
                return [0xed, 0x41 | R8[lows[1]] << 3]
            if lows[1] != 'a':
                raise AsmError('out (n),r needs A')
            return [0xd3, self.byte(self.port(ops[0]))]
        if m == 'ld':
            return self.load(ops, lows)
        raise AsmError(f'unknown instruction {mnemonic} {", ".join(ops)}')

    def port(self, op):
        port = self.memory(op)
        if port is None:
            raise AsmError(f'port {op} needs brackets')
        return port

    def prefixed(self, pre, out, disp):
        if pre is None:
            return out
        if disp is None:
            return [pre] + out
        return [pre, out[0], disp] + out[1:]

    def flow(self, m, ops, lows):
        if m == 'ret':
            return [0xc0 | CC[lows[0]] << 3] if ops else [0xc9]
        if m == 'djnz':
            if self.in_float is not None:
                index = self.in_float[0]
                self.jr_sites.append((index, self.pc - self.float_pc[index], self.value(ops[0]), 1000))
            return [0x10, self.relative(ops[0], 2)]
        if m == 'jr':
            if len(ops) == 2 and lows[0] not in ('nz', 'z', 'nc', 'c'):
                raise AsmError(f'jr cannot test {ops[0]}')
            target = ops[-1]
            if self.in_float is not None:
                index = self.in_float[0]
                self.jr_edges.add((index, self.value(target)))
                self.jr_sites.append((index, self.pc - self.float_pc[index], self.value(target), 1))
            to = self.value(target)
            op = 0x20 | CC[lows[0]] << 3 if len(ops) == 2 else 0x18
            if self.where in self.chains:
                via = self.chains[self.where][0]
                if -128 <= via - (self.pc + 2) <= 127 and via in self.jumps_before.get(to, ()):
                    return [op, (via - (self.pc + 2)) & 0xff]
                del self.chains[self.where]             # lost: a jp from now on
                self.long_jumps.add(self.where)
                self.grew = True
            if self.where not in self.long_jumps:
                offset = to - (self.pc + 2)
                if -128 <= offset <= 127 or not self.symbols_settled:
                    if len(ops) == 1:
                        self.jumps.setdefault(to, set()).add(self.pc)
                    return [op, self.relative(target, 2)]
                for via in sorted(self.jumps_before.get(to, ()) if self.chaining else ()):
                    if via != self.pc and -128 <= via - (self.pc + 2) <= 127:
                        self.chains[self.where] = (via, to)     # by way of a jump to the same place
                        self.grew = True
                        return [op, (via - (self.pc + 2)) & 0xff]
                self.long_jumps.add(self.where)
                self.grew = True
            if len(ops) == 1:
                self.jumps.setdefault(to, set()).add(self.pc)
            if len(ops) == 2:
                return [0xc2 | CC[lows[0]] << 3] + self.word(to)
            return [0xc3] + self.word(to)
        if m == 'jp' and lows[0] in ('(hl)', '(ix)', '(iy)'):
            return {'(hl)': [0xe9], '(ix)': [0xdd, 0xe9], '(iy)': [0xfd, 0xe9]}[lows[0]]
        if len(ops) == 2:
            base = 0xc2 if m == 'jp' else 0xc4
            return [base | CC[lows[0]] << 3] + self.word(self.value(ops[1]))
        if m == 'jp':
            self.jumps.setdefault(self.value(ops[0]), set()).add(self.pc)
        return [0xc3 if m == 'jp' else 0xcd] + self.word(self.value(ops[0]))

    def alu(self, m, ops, lows):
        if len(ops) == 2 and lows[0] in ('hl', 'ix', 'iy') and m in ('add', 'adc', 'sbc'):
            if m == 'add':
                if lows[0] == 'hl':
                    return [0x09 | RP[lows[1]] << 4]
                pre = 0xdd if lows[0] == 'ix' else 0xfd
                reg = {'bc': 0, 'de': 1, lows[0]: 2, 'sp': 3}[lows[1]]
                return [pre, 0x09 | reg << 4]
            if lows[0] != 'hl':
                raise AsmError(f'{m} {ops[0]},{ops[1]} cannot be encoded')   # only add takes IX, IY
            return [0xed, (0x4a if m == 'adc' else 0x42) | RP[lows[1]] << 4]
        operand = ops[-1]
        if len(ops) == 2 and lows[0] != 'a':
            raise AsmError(f'{m} needs A')
        reg = self.r8(operand)
        if reg:
            pre, code, disp = reg
            return self.prefixed(pre, [0x80 | ALU[m] << 3 | code], disp)
        return [0xc6 | ALU[m] << 3, self.byte(operand)]

    def load(self, ops, lows):
        dst, src = ops
        ld, ls = lows
        special = {('a', 'i'): [0xed, 0x57], ('a', 'r'): [0xed, 0x5f],
                   ('i', 'a'): [0xed, 0x47], ('r', 'a'): [0xed, 0x4f],
                   ('sp', 'hl'): [0xf9], ('sp', 'ix'): [0xdd, 0xf9], ('sp', 'iy'): [0xfd, 0xf9],
                   ('a', '(bc)'): [0x0a], ('a', '(de)'): [0x1a],
                   ('(bc)', 'a'): [0x02], ('(de)', 'a'): [0x12]}
        if (ld, ls) in special:
            return special[(ld, ls)]
        d8, s8 = self.r8(dst), self.r8(src)
        if d8 and s8:
            (pd, cd, dd), (ps, cs, ds_) = d8, s8
            # ld (hl),(hl) is HALT, and an index prefix turns H, L and (HL) in
            # the same instruction into its halves or (IX+d), so ld h,ixl,
            # ld ixh,(ix+1) and ld ixh,iyl do not exist either.
            if cd == cs == 6:
                bad = True
            elif pd is not None and ps is not None:
                bad = pd != ps or 6 in (cd, cs)
            elif pd is not None:
                bad = cd != 6 and cs in (4, 5, 6)
            elif ps is not None:
                bad = cs != 6 and cd in (4, 5, 6)
            else:
                bad = False
            if bad:
                raise AsmError(f'ld {dst},{src} cannot be encoded')
            pre = pd if pd is not None else ps
            disp = dd if dd is not None else ds_
            return self.prefixed(pre, [0x40 | cd << 3 | cs], disp)
        if d8 and not self.memory(src):
            pd, cd, dd = d8
            out = self.prefixed(pd, [0x06 | cd << 3], dd)
            return out + [self.byte(src)]
        if ld == 'a' and self.memory(src):
            return [0x3a] + self.word(self.value(self.memory(src)))
        if ls == 'a' and self.memory(dst):
            return [0x32] + self.word(self.value(self.memory(dst)))
        if ld in RP or ld in ('ix', 'iy'):
            pre = [] if ld in RP else [0xdd if ld == 'ix' else 0xfd]
            mem = self.memory(src)
            if mem is not None:
                address = self.word(self.value(mem))
                if ld in ('hl', 'ix', 'iy'):
                    return pre + [0x2a] + address
                return [0xed, 0x4b | RP[ld] << 4] + address
            code = 2 if ld in ('ix', 'iy') else RP[ld]
            return pre + [0x01 | code << 4] + self.word(self.value(src))
        mem = self.memory(dst)
        if mem is not None and (ls in RP or ls in ('ix', 'iy')):
            address = self.word(self.value(mem))
            if ls in ('hl', 'ix', 'iy'):
                pre = [] if ls == 'hl' else [0xdd if ls == 'ix' else 0xfd]
                return pre + [0x22] + address
            return [0xed, 0x43 | RP[ls] << 4] + address
        raise AsmError(f'unknown load ld {dst},{src}')

    # -- passes ------------------------------------------------------------
    def read(self, path: Path, out: list):
        for number, raw in enumerate(path.read_text().splitlines(), 1):
            line = strip_comment(raw).rstrip()
            m = re.match(r'\s*include\s+"([^"]+)"', line, re.I)
            if m:
                for folder in (path.parent, *self.include_dirs):
                    if (folder / m.group(1)).is_file():
                        self.read(folder / m.group(1), out)
                        break
                else:
                    raise AsmError(f'{path.name}:{number}: cannot find {m.group(1)}')
            else:
                out.append((path.name, number, line))

    def assemble(self, source: Path, size: int, fill: int = 0xff):
        lines = []
        self.read(source, lines)
        best = None
        self.order, self.order_runs = None, 0
        ordered = any(strip_comment(line).strip().lower() == 'floatorder' for _, _, line in lines)
        self.chaining = ordered
        for strategy in ('size', 'linked') + (('order',) if ordered else ()):
            self.strategy = strategy
            self.float_at = {}
            self.float_size = {}
            self.symbols = {}
            biggest = {}
            for _ in range(30):
                self.relax(lines, size, fill)
                for i, n in self.float_size.items():
                    biggest[i] = max(n, biggest.get(i, 0))
                placed = dict(self.float_at)
                actual = dict(self.float_size)
                if strategy != 'order':                 # (which packs them as they are)
                    self.float_size = dict(biggest)      # room for the largest seen
                self.place_floats(size)
                self.float_size = actual
                if placed == self.float_at and self.consistent(size):
                    break
            else:
                continue
            over = sum(n for i, n in self.float_size.items() if self.float_at.get(i, size) >= size)
            total = sum(self.float_size.values())
            if best is None or (over, total) < best[0]:
                best = ((over, total), strategy, dict(self.float_at), dict(self.float_size))
        if best is None:
            raise AsmError('layout did not settle')
        (over, total), self.strategy, self.float_at, self.float_size = best
        self.relax(lines, size, fill)
        if over:
            raise AsmError(f'floating code does not fit: {total} bytes in '
                           f'{len(self.float_size)} sections, {over} bytes left over')
        self.final_pass = True
        image = self._pass(lines, size, fill)
        for via, to in self.chains.values():
            if via not in self.jumps.get(to, ()):
                raise AsmError('a chained jr lost the jump it goes by')
        return image

    def consistent(self, size):
        """No two placed sections (or fixed code) share a byte."""
        used = [False] * 0x30000
        for a, owner in enumerate(self.fixed_owner):
            used[a] = owner is not None
        for i, at in self.float_at.items():
            if at >= size:
                continue
            for a in range(at, at + self.float_size[i]):
                if used[a]:
                    return False
                used[a] = True
        return True

    def relax(self, lines, size, fill):
        """Fresh jump relaxation for the current placement."""
        self.long_jumps = set()
        self.chains = {}                        # jr site -> (the jump it goes by, its target)
        self.jumps = {}
        self.symbols_settled = False
        for passes in range(40):
            before = dict(self.symbols)
            self.grew = False
            self.final_pass = False
            self._pass(lines, size, fill)
            if passes > 1 and before == self.symbols and not self.grew:
                return
            self.symbols_settled = passes >= 1
        raise AsmError('jump relaxation did not settle')

    def place_floats(self, size):
        if not self.float_size:
            return
        limit = self.float_limit if self.float_limit is not None else size
        free, start = [], None
        for a in range(limit + 1):
            if a < limit and self.fixed_owner[a] is None:
                if start is None:
                    start = a
            elif start is not None:
                free.append([start, a])
                start = None
        sizes = self.float_size
        placement = {}
        if self.strategy == 'order':
            left = self.place_order(free, placement)
        elif self.strategy == 'size':
            for index in sorted(sizes, key=lambda i: (-sizes[i], i)):
                fits = [g for g in free if g[1] - g[0] >= sizes[index]]
                if fits:
                    gap = min(fits, key=lambda g: g[1] - g[0])
                    placement[index] = gap[0]
                    gap[0] += sizes[index]
            left = [i for i in sizes if i not in placement]
        else:
            left = self.place_linked(free, placement)
        scratch = 0x10000
        for index in sorted(left):
            placement[index] = scratch          # does not fit
            scratch += sizes[index]
        self.float_at = placement

    def place_order(self, free, placement):
        """The 'order' layout; returns the sections that did not fit."""
        sizes = self.float_size
        self.order_runs += 1
        if self.order_runs <= 2:                # again once the sizes are known
            starts = sorted((at, i) for i, at in self.float_pc.items())
            addresses = [at for at, _ in starts]

            def locate(address):
                k = bisect.bisect_right(addresses, address) - 1
                if k >= 0:
                    at, j = starts[k]
                    if address < at + sizes.get(j, 0):
                        return j, address - at
                return None, address
            edges = [(i, offset) + locate(target) + (weight,)
                     for i, offset, target, weight in self.jr_sites]
            self.order = anneal_order(self.order or sorted(sizes), sizes, edges, free)
        _, at, left = order_layout(self.order, sizes, free)
        placement.update(at)
        return left

    def place_linked(self, free, placement):
        """The 'linked' layout; returns the sections that did not fit."""
        sizes = self.float_size
        starts = sorted((at, i) for i, at in self.float_pc.items())
        addresses = [at for at, _ in starts]
        linked = set()
        for i, target in self.jr_edges:
            k = bisect.bisect_right(addresses, target) - 1
            if k >= 0:
                at, j = starts[k]
                if j != i and target < at + sizes[j]:
                    linked |= {i, j}
        gaps = sorted(free, key=lambda g: (g[0] - g[1], g[0]))     # largest first
        k, left = 0, []
        for i in sorted(linked):
            while k < len(gaps) and gaps[k][1] - gaps[k][0] < sizes[i]:
                k += 1
            if k == len(gaps):
                left.append(i)
                continue
            placement[i] = gaps[k][0]
            gaps[k][0] += sizes[i]
        left += [i for i in sizes if i not in linked]
        left.sort(key=lambda i: (-sizes[i], i))
        for gap in sorted(free, key=lambda g: (g[1] - g[0], g[0])):
            room = gap[1] - gap[0]
            best = {0: ()}                      # bytes filled -> sections
            for i in left:
                for filled, chosen in list(best.items()):
                    total = filled + sizes[i]
                    if total <= room and total not in best:
                        best[total] = chosen + (i,)
            for i in best[max(best)]:
                placement[i] = gap[0]
                gap[0] += sizes[i]
                left.remove(i)
        return left

    def _pass(self, lines, size, fill):
        if True:
            self.image_size = size
            image = bytearray([fill]) * 0x30000
            owner = [None] * 0x30000
            self.fixed_owner = [None] * size
            self.in_float = None
            self.float_count = 0
            self.float_pc = {}                  # section index -> address this pass
            self.jr_edges = set()               # (section, jr target address)
            self.jr_sites = []                  # (section, offset, target, cost if out of range)
            self.jumps_before, self.jumps = self.jumps, {}  # target -> addresses of direct jumps
            self.scratch_next = 0x10000
            self.charset_name, self.charset = 'zx81', ZX81_TEXT
            self.auto = False
            self.flow_ended = True
            self.pc = 0
            self.scope = ''
            for name, number, line in lines:
                try:
                    self.where = f'{name}:{number}'
                    self.statement(line, image, owner, self.where)
                except AsmError as exc:
                    raise AsmError(f'{name}:{number}: {exc}\n    {line.strip()}') from None
                except (KeyError, IndexError, TypeError, ValueError) as exc:
                    raise AsmError(f'{name}:{number}: cannot assemble ({exc})\n    {line.strip()}') from None
        if self.final_pass:
            for index, at in self.float_at.items():
                if at >= size:
                    total = sum(self.float_size.values())
                    over = sum(n for i, n in self.float_size.items() if self.float_at[i] >= size)
                    raise AsmError(f'floating code does not fit: {total} bytes in '
                                   f'{len(self.float_size)} sections, {over} bytes left over')
        return bytes(image[:size])

    def define(self, label, value):
        key = self.scoped(label)
        if self.final_pass and key in self.symbols and self.symbols[key] != value:
            raise AsmError(f'{label} moved between passes')
        if not self.final_pass or key not in self.symbols:
            self.symbols[key] = value

    def statement(self, line, image, owner, where):
        m = re.match(r'\s*(\.?[A-Za-z_][A-Za-z0-9_.]*)\s*:', line)
        if m and self.auto and not m.group(1).startswith('.') and self.flow_ended:
            if self.in_float is not None:
                self.statement('endfloat', image, owner, where)
            self.statement('float', image, owner, where)
            self.flow_ended = False     # a second label joins this section
        if m:
            label = m.group(1)
            if not label.startswith('.'):
                self.scope = label
            self.define(label, self.pc)
            line = line[m.end():]
        line = line.strip()
        if not line:
            return
        m = re.match(r'(\.?[A-Za-z_][A-Za-z0-9_.]*)\s+equ\s+(.*)', line, re.I)
        if m:
            self.define(m.group(1), self.value(m.group(2)))
            return
        parts = line.split(None, 1)
        mnemonic = parts[0].lower()
        rest = parts[1] if len(parts) > 1 else ''
        ops = split_operands(rest)
        if mnemonic == 'autofloat':
            self.auto = True
            self.flow_ended = True
            return
        if mnemonic == 'endautofloat':
            self.auto = False
            if self.in_float is not None:
                self.statement('endfloat', image, owner, where)
            return
        if mnemonic == 'floatorder':
            return
        if mnemonic not in ('float', 'endfloat', 'assert', 'error', 'floatlimit'):
            low = [o.lower().replace(' ', '') for o in ops]
            self.flow_ended = (
                mnemonic in ('ret', 'reti', 'retn') and not ops
                or mnemonic == 'jr' and len(ops) == 1
                or mnemonic == 'jp' and (len(ops) == 1))
        if mnemonic == 'org':
            if self.in_float is not None:
                raise AsmError('org inside float')
            self.pc = self.value(rest)
            return
        if mnemonic == 'floatlimit':
            if self.float_limit_override is None:
                self.float_limit = self.value(rest)
            return
        if mnemonic == 'float':
            if self.in_float is not None:
                raise AsmError('nested float')
            index = self.float_count
            self.float_count += 1
            self.in_float = (index, self.pc)
            if index in self.float_at:
                self.pc = self.float_at[index]
            else:
                self.pc = self.scratch_next
            self.float_start = self.pc
            self.float_pc[index] = self.pc
            return
        if mnemonic == 'endfloat':
            index, saved = self.in_float
            if self.pc == self.float_start:
                raise AsmError('empty floating section: its labels would have no place')
            self.float_size[index] = self.pc - self.float_start
            if index not in self.float_at:
                self.scratch_next = self.pc
            self.in_float = None
            self.pc = saved
            return
        if mnemonic == 'charset':
            if rest.strip() not in CHARSETS:
                raise AsmError(f'unknown charset {rest.strip()}')
            self.charset_name = rest.strip()
            self.charset = CHARSETS[self.charset_name]
            return
        if mnemonic == 'assert':
            if self.final_pass and not self.value(rest):
                raise AsmError(f'assertion failed: {rest}')
            return
        if mnemonic == 'error':
            if self.final_pass:
                raise AsmError(rest)
            return
        if mnemonic in ('db', 'defb', 'defm'):
            data = []
            for op in ops:
                if op.startswith('"'):
                    data.extend(op[1:-1].encode('latin-1'))
                else:
                    data.append(self.byte(op))
        elif mnemonic == 'dz':
            text = rest.strip()
            if len(text) < 3 or text[0] != '"' or text[-1] != '"':
                raise AsmError('dz needs a quoted string')
            try:
                data = [self.charset[ch] for ch in text[1:-1]]
            except KeyError as exc:
                raise AsmError(f'no {self.charset_name} character for {exc}') from None
            data[-1] |= 0x80
        elif mnemonic in ('dw', 'defw'):
            data = []
            for op in ops:
                data.extend(self.word(self.value(op)))
        elif mnemonic in ('ds', 'defs'):
            count = self.value(ops[0])
            data = [self.byte(ops[1]) if len(ops) > 1 else 0] * count
        else:
            data = self.encode(mnemonic, ops)
        for i, b in enumerate(data):
            address = self.pc + i
            if self.final_pass and not 0 <= address < self.image_size:
                raise AsmError(f'address {address:#06x} outside image')
            if not 0 <= address < len(image):
                raise AsmError(f'address {address:#06x} outside image')
            if self.in_float is None and address < self.image_size:
                self.fixed_owner[address] = where
            if self.final_pass and owner[address] is not None:
                raise AsmError(f'{address:#06x} already written by {owner[address]}')
            image[address] = b
            owner[address] = where
        self.pc += len(data)


def order_layout(order, sizes, free):
    """Each section in this order at the end of the first gap it fits in:
    (bytes that do not fit, the addresses, the sections left over)."""
    ends = [start for start, _ in free]
    at, left = {}, []
    for i in order:
        for k, (_, end) in enumerate(free):
            if ends[k] + sizes[i] <= end:
                at[i] = ends[k]
                ends[k] += sizes[i]
                break
        else:
            left.append(i)
    return sum(sizes[i] for i in left), at, left


def anneal_order(sections, sizes, edges, free, steps=60000, seed=1):
    """An order of the sections that fits and leaves few jr out of range."""
    def cost(order):
        over, at, _ = order_layout(order, sizes, free)
        far = 0
        for i, offset, j, target, weight in edges:
            if i not in at or j is not None and j not in at:
                far += weight
                continue
            to = at[j] + target if j is not None else target
            if not -128 <= to - (at[i] + offset + 2) <= 127:
                far += weight
        return over * 1000 + far
    rng = random.Random(seed)
    order = list(sections)
    now = cost(order)
    best, best_order, heat = now, order[:], 2.0
    cool = (0.05 / heat) ** (1 / steps)
    for _ in range(steps):
        trial = order[:]
        a, b = sorted(rng.sample(range(len(trial)), 2))
        if rng.random() < 0.5:
            trial[a], trial[b] = trial[b], trial[a]
        else:
            trial.insert(b, trial.pop(a))
        c = cost(trial)
        if c <= now or rng.random() < math.exp((now - c) / heat):
            order, now = trial, c
            if c < best:
                best, best_order = c, order[:]
        heat *= cool
    return best_order


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('source', type=Path)
    parser.add_argument('output', type=Path)
    parser.add_argument('--size', type=lambda s: int(s, 0), default=8192)
    parser.add_argument('--symbols', type=Path)
    parser.add_argument('-I', dest='include', action='append', default=[])
    parser.add_argument('--float-limit', type=lambda s: int(s, 0))
    args = parser.parse_args()
    asm = Assembler(args.include)
    if args.float_limit is not None:
        asm.float_limit = asm.float_limit_override = args.float_limit
    try:
        image = asm.assemble(args.source, args.size)
    except AsmError as exc:
        print(exc, file=sys.stderr)
        sys.exit(1)
    args.output.write_bytes(image)
    if args.symbols:
        args.symbols.write_text(''.join(f'{v:04x} {k}\n' for k, v in sorted(asm.symbols.items(), key=lambda kv: kv[1])))


if __name__ == '__main__':
    main()
