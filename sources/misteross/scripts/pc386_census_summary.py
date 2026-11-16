#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Summarize machine_tb --census files (cores/fes-pc386/sim/census.h) into one
JSON row per run, for the i386 Phase 5 records.

Each run is TITLE=DIR, where DIR holds the census.txt and report.json that
`sim_fes_pc386.py --suite game --instruction-census` writes
(build/sim/fes-pc386/game-<id>/). The rows hold counts only: no instruction
bytes, frames or game data. Keys are sorted, so two records diff line by line.

    python3 scripts/pc386_census_summary.py --out census.jsonl \\
        jumpbump=build/sim/fes-pc386/game-jumpbump descent=...
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from pathlib import Path

# MOV to and from the system registers, by the census's sysreg stems.
SYSREG = {'0F20': ('cr', 'read'), '0F22': ('cr', 'write'), '0F21': ('dr', 'read'), '0F23': ('dr', 'write'),
          '0F24': ('tr', 'read'), '0F26': ('tr', 'write')}
STEM = re.compile(r'^(?:67)?(?:66)?(0F2[0-6])\.(\d)(?::|$)')


def load(path: Path) -> dict[str, int]:
    """census.txt's counts by key."""
    counts = {}
    for line in path.read_text().splitlines():
        parts = line.rsplit(' ', 2)
        if len(parts) == 3:
            counts[parts[0]] = int(parts[1])
    return counts


def per_100(walks: int, accesses: int) -> float | None:
    return round(100 * walks / accesses, 3) if accesses else None


def summarize(counts: dict[str, int]) -> dict:
    def total(pattern: str) -> int:
        return sum(n for k, n in counts.items() if re.search(pattern, k))

    def keyed(prefix: str) -> dict[str, int]:
        return {k[len(prefix):]: n for k, n in sorted(counts.items()) if k.startswith(prefix)}

    sysreg: dict[str, Counter] = {}
    for key, n in counts.items():
        if m := STEM.match(key):
            kind, direction = SYSREG[m.group(1)]
            sysreg.setdefault(f'{kind}{m.group(2)}', Counter())[direction] += n
    sources: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('ts:'):
            source = key.split(':')[1]
            if source in ('exception', 'intr', 'nmi', 'int'):
                source += ':' + key.split(':')[2]
            sources[source] += n
    entries: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('v86:enter:'):
            entries[key.split(':')[2]] += n
    pf: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('event:exception:14:') and (m := re.search(r':err=([0-9a-f]+)', key)):
            pf[m.group(1)] += n
    tlb = {}
    for name in sorted({k.split(':')[1] for k in counts if k.startswith('tlb:')}):
        accesses, walks = counts.get(f'tlb:{name}:accesses', 0), counts.get(f'tlb:{name}:walks', 0)
        tlb[name] = {'accesses': accesses, 'walks': walks, 'walks_per_100': per_100(walks, accesses)}
    return {
        'paged_steps': counts.get('paging:steps', 0),
        'page_cross': counts.get('paging:page-cross', 0),
        'pg_toggles': total(r'^cr0\.pg:'),
        'sysreg': {k: dict(sorted(v.items())) for k, v in sorted(sysreg.items())},
        'clts': total(r'^(?:67)?(?:66)?0F06(:|$)'),
        'lmsw': total(r'^(?:67)?(?:66)?0F01\.6(:|$)'),
        'invlpg': total(r'^(?:67)?(?:66)?0F01\.7(:|$)'),
        'dr7': keyed('dr7:'),
        'db': total(r'^event:exception:1:'),
        'nm': total(r'^event:exception:7:'),
        'pf_by_error': dict(sorted(pf.items())),
        'task_switches': dict(sorted(sources.items())),
        'task_switch_keys': keyed('ts:'),
        'tss_iomap': keyed('tss-iomap:'),
        'ltr': total(r'^tr:.*:(?:67)?(?:66)?0F00\.3:'),
        'v86_steps': keyed('v86:steps:'),
        'v86_entries': dict(sorted(entries.items())),
        'v86_sensitive': keyed('v86:sens:'),
        'v86_events': {k: n for k, n in sorted(counts.items()) if k.startswith('event:') and ':v86:' in k},
        'io_bitmap': keyed('io-bitmap:'),
        'pm3_steps': total(r':pm3$'),
        'tlb': tlb,
    }


# The x87 game census (docs/validation/2026-11-15-i386-x87-game-census/method.md).
# A form's name, its slice class and the operand it moves. The form is the
# census's: the opcode, ".m<reg>" for a memory form, the whole ModRM for the
# register forms that need it, else ".r<reg>".
#
# class: load, store, fxch, arith, compare, trans, control, image, simple.
# operand: real32, real64, real80, int16, int32, int64, bcd80, image, none.
X87_FORMS: dict[str, tuple[str, str]] = {
    # D8/DC: real arithmetic, memory (m32 / m64) and register.
    **{f'd8.m{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'real32') for r in range(8)},
    **{f'dc.m{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'real64') for r in range(8)},
    **{f'd8.r{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'none') for r in range(8)},
    **{f'dc.r{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'none') for r in range(8)},
    **{f'de.r{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'none') for r in range(8)},
    # DA/DE memory: integer arithmetic (m32 / m16).
    **{f'da.m{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'int32') for r in range(8)},
    **{f'de.m{r}': (('arith' if r in (0, 1, 4, 5, 6, 7) else 'compare'), 'int16') for r in range(8)},
    # D9 memory: FLD/FST/FSTP m32, FLDENV, FLDCW, FNSTENV, FNSTCW.
    'd9.m0': ('load', 'real32'), 'd9.m2': ('store', 'real32'), 'd9.m3': ('store', 'real32'),
    'd9.m4': ('control', 'image'), 'd9.m5': ('control', 'none'),
    'd9.m6': ('control', 'image'), 'd9.m7': ('control', 'none'),
    # DD memory: FLD/FST/FSTP m64, FRSTOR, FNSAVE, FNSTSW m16.
    'dd.m0': ('load', 'real64'), 'dd.m2': ('store', 'real64'), 'dd.m3': ('store', 'real64'),
    'dd.m4': ('control', 'image'), 'dd.m6': ('control', 'image'), 'dd.m7': ('control', 'none'),
    # DB memory: FILD/FIST/FISTP m32, FLD m80, FSTP m80.
    'db.m0': ('load', 'int32'), 'db.m2': ('store', 'int32'), 'db.m3': ('store', 'int32'),
    'db.m5': ('load', 'real80'), 'db.m7': ('store', 'real80'),
    # DF memory: FILD/FIST/FISTP m16, FBLD/FBSTP m80, FILD/FISTP m64.
    'df.m0': ('load', 'int16'), 'df.m2': ('store', 'int16'), 'df.m3': ('store', 'int16'),
    'df.m4': ('load', 'bcd80'), 'df.m5': ('load', 'int64'),
    'df.m6': ('store', 'bcd80'), 'df.m7': ('store', 'int64'),
    # D9 register forms.
    'd9.r0': ('load', 'none'), 'd9.r1': ('fxch', 'none'), 'd9d0': ('simple', 'none'),
    'd9e0': ('simple', 'none'), 'd9e1': ('simple', 'none'), 'd9e4': ('compare', 'none'),
    'd9e5': ('compare', 'none'), 'd9e8': ('load', 'none'), 'd9e9': ('load', 'none'),
    'd9ea': ('load', 'none'), 'd9eb': ('load', 'none'), 'd9ec': ('load', 'none'),
    'd9ed': ('load', 'none'), 'd9ee': ('load', 'none'),
    'd9f0': ('trans', 'none'), 'd9f1': ('trans', 'none'), 'd9f2': ('trans', 'none'),
    'd9f3': ('trans', 'none'), 'd9f4': ('simple', 'none'), 'd9f5': ('trans', 'none'),
    'd9f6': ('simple', 'none'), 'd9f7': ('simple', 'none'), 'd9f8': ('trans', 'none'),
    'd9f9': ('trans', 'none'), 'd9fa': ('arith', 'none'), 'd9fb': ('trans', 'none'),
    'd9fc': ('round', 'none'), 'd9fd': ('trans', 'none'), 'd9fe': ('trans', 'none'),
    'd9ff': ('trans', 'none'),
    # DB/DA/DE/DD register forms that carry their whole ModRM.
    'dbe0': ('control', 'none'), 'dbe1': ('control', 'none'), 'dbe2': ('control', 'none'),
    'dbe3': ('control', 'none'), 'dbe4': ('control', 'none'),
    'dae9': ('compare', 'none'), 'ded9': ('compare', 'none'), 'dfe0': ('control', 'none'),
    # DD register forms: /0 FFREE, /1 (FXCH alias), /2 FST ST(i), /3 FSTP ST(i),
    # and the whole ModRM for /4 and /5, which are FUCOM and FUCOMP ST(i).
    'dd.r0': ('simple', 'none'),
    # DD C8+i is the 8087/287/387's undocumented FXCH alias, so it belongs in
    # the fxch class like D9 C8+i: the probe-only rule tests that class, and a
    # run using the alias would otherwise be called probe-only wrongly.
    'dd.r1': ('fxch', 'none'),
    'dd.r2': ('store', 'none'), 'dd.r3': ('store', 'none'),
    **{f'dde{n:x}': ('compare', 'none') for n in range(16)},
    **{f'db.r{r}': ('simple', 'none') for r in range(8)},
    **{f'da.r{r}': ('compare', 'none') for r in range(8)},
    **{f'df.r{r}': ('simple', 'none') for r in range(8)},
}
# FXCH is d9.r1; the sqrt and the precision-controlled register forms above
# are named individually. Forms the table does not name are counted as
# 'unknown' and reported, never silently dropped.
# The classes precision control acts on: add, subtract, multiply, divide and
# square root. FRNDINT and the integer and real stores round under *rounding*
# control only and ignore precision control (387 PRM; the x87 plan record's
# "Accuracy" section), so FRNDINT has its own class, `round`, and is counted
# in the mix but not in the PC distribution.
# The encodings census.h can name that are NOT 387 instructions: reserved or
# undefined ModRM values inside the ESC groups. They are listed explicitly
# rather than left absent, so that
# test_the_form_inventory_covers_everything_the_census_can_emit can require
# every emittable form to be either a named instruction or listed here. A new
# instruction therefore has to be taken OUT of this list to be added, which is
# what makes the check able to catch an omission -- three review rounds found
# gaps in this inventory by inspection before it existed.
X87_RESERVED = (
    'd9.m1', 'd9d1', 'd9d2', 'd9d3', 'd9d4', 'd9d5', 'd9d6', 'd9d7',
    'd9d8', 'd9d9', 'd9da', 'd9db', 'd9dc', 'd9dd', 'd9de', 'd9df',
    'd9e2', 'd9e3', 'd9e6', 'd9e7', 'd9ef', 'dae8', 'daea', 'daeb',
    'daec', 'daed', 'daee', 'daef', 'db.m1', 'db.m4', 'db.m6', 'dbe5',
    'dbe6', 'dbe7', 'dd.m1', 'dd.m5', 'ddf0', 'ddf1', 'ddf2', 'ddf3',
    'ddf4', 'ddf5', 'ddf6', 'ddf7', 'ddf8', 'ddf9', 'ddfa', 'ddfb',
    'ddfc', 'ddfd', 'ddfe', 'ddff', 'ded8', 'deda', 'dedb', 'dedc',
    'dedd', 'dede', 'dedf', 'df.m1', 'dfe1', 'dfe2', 'dfe3', 'dfe4',
    'dfe5', 'dfe6', 'dfe7',
)

X87_FORMS.update({form: ('reserved', 'none') for form in X87_RESERVED})

X87_ARITH_PC = ('arith',)
PRECISION = {0: '24', 1: 'reserved', 2: '53', 3: '64'}
ROUNDING = {0: 'nearest', 1: 'down', 2: 'up', 3: 'zero'}


# Mnemonics for the forms, so a generated list can name instructions rather
# than encodings. A form with no entry keeps its encoding.
X87_MNEMONIC = {
    'd9f0': 'F2XM1', 'd9f1': 'FYL2X', 'd9f2': 'FPTAN', 'd9f3': 'FPATAN', 'd9f4': 'FXTRACT',
    'd9f5': 'FPREM1', 'd9f6': 'FDECSTP', 'd9f7': 'FINCSTP', 'd9f8': 'FPREM', 'd9f9': 'FYL2XP1',
    'd9fa': 'FSQRT', 'd9fb': 'FSINCOS', 'd9fc': 'FRNDINT', 'd9fd': 'FSCALE', 'd9fe': 'FSIN',
    'd9ff': 'FCOS', 'd9d0': 'FNOP', 'd9e0': 'FCHS', 'd9e1': 'FABS', 'd9e4': 'FTST', 'd9e5': 'FXAM',
    'd9e8': 'FLD1', 'd9e9': 'FLDL2T', 'd9ea': 'FLDL2E', 'd9eb': 'FLDPI', 'd9ec': 'FLDLG2',
    'd9ed': 'FLDLN2', 'd9ee': 'FLDZ', 'dbe2': 'FNCLEX', 'dbe3': 'FNINIT', 'dbe4': 'FSETPM',
    'dfe0': 'FNSTSW AX', 'ded9': 'FCOMPP', 'dae9': 'FUCOMPP',
    'd9.m0': 'FLD m32', 'd9.m2': 'FST m32', 'd9.m3': 'FSTP m32', 'd9.m4': 'FLDENV',
    'd9.m5': 'FLDCW', 'd9.m6': 'FNSTENV', 'd9.m7': 'FNSTCW', 'd9.r0': 'FLD ST(i)',
    'd9.r1': 'FXCH', 'dd.m0': 'FLD m64', 'dd.m2': 'FST m64', 'dd.m3': 'FSTP m64',
    'dd.m4': 'FRSTOR', 'dd.m6': 'FNSAVE', 'dd.m7': 'FNSTSW m16', 'dd.r0': 'FFREE',
    'dd.r1': 'FXCH (DD alias)',
    'dd.r2': 'FST ST(i)', 'dd.r3': 'FSTP ST(i)', 'db.m0': 'FILD m32', 'db.m2': 'FIST m32',
    'db.m3': 'FISTP m32', 'db.m5': 'FLD m80', 'db.m7': 'FSTP m80',
    'df.m0': 'FILD m16', 'df.m2': 'FIST m16', 'df.m3': 'FISTP m16', 'df.m4': 'FBLD',
    'df.m5': 'FILD m64', 'df.m6': 'FBSTP', 'df.m7': 'FISTP m64',
    'd8.m0': 'FADD m32', 'd8.m1': 'FMUL m32', 'd8.m2': 'FCOM m32', 'd8.m3': 'FCOMP m32', 'd8.m4': 'FSUB m32', 'd8.m5': 'FSUBR m32', 'd8.m6': 'FDIV m32', 'd8.m7': 'FDIVR m32',
    'dc.m0': 'FADD m64', 'dc.m1': 'FMUL m64', 'dc.m2': 'FCOM m64', 'dc.m3': 'FCOMP m64', 'dc.m4': 'FSUB m64', 'dc.m5': 'FSUBR m64', 'dc.m6': 'FDIV m64', 'dc.m7': 'FDIVR m64',
    'da.m0': 'FIADD m32', 'da.m1': 'FIMUL m32', 'da.m2': 'FICOM m32', 'da.m3': 'FICOMP m32', 'da.m4': 'FISUB m32', 'da.m5': 'FISUBR m32', 'da.m6': 'FIDIV m32', 'da.m7': 'FIDIVR m32',
    'de.m0': 'FIADD m16', 'de.m1': 'FIMUL m16', 'de.m2': 'FICOM m16', 'de.m3': 'FICOMP m16', 'de.m4': 'FISUB m16', 'de.m5': 'FISUBR m16', 'de.m6': 'FIDIV m16', 'de.m7': 'FIDIVR m16',
}


def x87_name(form: str) -> str:
    """'d9f0' -> 'F2XM1 (`d9f0`)'; an unnamed form keeps its encoding."""
    mnemonic = X87_MNEMONIC.get(form)
    return f'{mnemonic} (`{form}`)' if mnemonic else f'`{form}`'


def x87_form_of(key: str) -> str:
    """The form in an x87 key's field: 'D8.m0:pm3' -> 'D8.m0'."""
    return key.split(':')[0]


def x87_summary(counts: dict[str, int]) -> dict:
    """The x87 rows this record's tables are built from."""
    ops: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('x87op:'):
            ops[x87_form_of(key[len('x87op:'):])] += n
    by_class: Counter = Counter()
    by_operand: Counter = Counter()
    unknown: Counter = Counter()
    for form, n in ops.items():
        entry = X87_FORMS.get(form)
        if entry is None:
            unknown[form] += n
            continue
        cls, operand = entry
        by_class[cls] += n
        if operand != 'none':
            by_operand[operand] += n
    # Precision and rounding control, per form and over the arithmetic forms.
    pc_arith: Counter = Counter()
    rc_arith: Counter = Counter()
    pc_all: Counter = Counter()
    rc_all: Counter = Counter()
    for key, n in counts.items():
        if not key.startswith('x87cwform:'):
            continue
        rest = key[len('x87cwform:'):]
        form, _, cw_text = rest.rpartition(':')
        cw = int(cw_text, 16)
        pc, rc = PRECISION[(cw >> 8) & 3], ROUNDING[(cw >> 10) & 3]
        pc_all[pc] += n
        rc_all[rc] += n
        entry = X87_FORMS.get(x87_form_of(form))
        if entry and entry[0] in X87_ARITH_PC:
            pc_arith[pc] += n
            rc_arith[rc] += n
    sig: Counter = Counter()
    sig_conservative: Counter = Counter()
    sig_by_form: dict[str, Counter] = {}
    for key, n in counts.items():
        if key.startswith('x87sig:'):
            form, _, bucket = key[len('x87sig:'):].rpartition(':')
            sig[bucket] += n
            sig_by_form.setdefault(bucket, Counter())[form] += n
            # The register stores FST/FSTP ST(i) are read from the correct
            # side only in census files written after the popping-store fix,
            # and a census file cannot say which it is, so the verdict uses an
            # aggregate without them. It is conservative either way: their
            # values were produced by arithmetic or a load that is counted.
            if form not in ('dd.r2', 'dd.r3'):
                sig_conservative[bucket] += n
    masks: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('x87cwform:'):
            cw = int(key.rpartition(':')[2], 16)
            masks[f'{cw & 0x3f:02x}'] += n
    # Precision and rounding control by the census's arithmetic class, which
    # is where FSQRT is separable: x87cw:sqrt is FSQRT alone (d9fa).
    pc_by_class: dict[str, Counter] = {}
    rc_by_class: dict[str, Counter] = {}
    for key, n in counts.items():
        if key.startswith('x87cw:'):
            cls, _, cw_text = key[len('x87cw:'):].rpartition(':')
            cw = int(cw_text, 16)
            pc_by_class.setdefault(cls, Counter())[PRECISION[(cw >> 8) & 3]] += n
            rc_by_class.setdefault(cls, Counter())[ROUNDING[(cw >> 10) & 3]] += n
    # FRNDINT's own precision distribution, from its own rows. It ignores
    # precision control; the column exists to show that, and taking it from
    # the `other` class would mix in every load, store, compare and control
    # form the census puts there.
    frndint_pc: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('x87cwform:d9fc:'):
            frndint_pc[PRECISION[(int(key.rpartition(':')[2], 16) >> 8) & 3]] += n
    # The committed reliance rule is ES set *or* IRQ 13 delivered, so the
    # delivery is counted directly rather than inferred from an ES transition:
    # a title that restores state with ES already set takes the interrupt
    # without a fresh transition.
    version = counts.get('x87census:version', 1)
    irq13 = sum(n for k, n in counts.items() if k.startswith('x87irq13:'))
    # A superset of IRQ 13: any external interrupt delivered with a 387 error
    # pending, whatever the slave PIC's programmed vector base. Reliance is
    # read from this, which cannot miss a delivery.
    err_intr = sum(n for k, n in counts.items() if k.startswith('x87errintr:'))
    r_es = any(k.startswith('x87es:') for k in counts)
    pc_sites: dict[str, Counter] = {}
    for key, n in counts.items():
        if key.startswith('x87pcsite:'):
            pc, cs, page = key[len('x87pcsite:'):].split(':')
            pc_sites.setdefault(PRECISION[int(pc)], Counter())[f'{cs}:{page}'] += n
    cw_sets: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('x87cwset:'):
            cw_sets[key[len('x87cwset:'):]] += n
    cw_seconds: dict[str, set] = {}
    for key, n in counts.items():
        if key.startswith('x87cwsec:'):
            second, _, cw_text = key[len('x87cwsec:'):].rpartition(':')
            cw = int(cw_text, 16)
            cw_seconds.setdefault(second, set()).add(f'pc{PRECISION[(cw >> 8) & 3]}/{ROUNDING[(cw >> 10) & 3]}')
    retired = sum(ops.values())
    # The committed verdict
    # (docs/validation/2026-11-15-i386-x87-game-census/method.md): a title is
    # sufficient for a 24-bit significand only if every arithmetic ESC ran at
    # PC=24, no significand wider than 24 bits was observed, and no m80 real
    # operand was loaded or stored. Each failing reason is named with its
    # count, so the verdict is the script's output and not a reading of the
    # tables. Mind what it does not say: precision control narrows the
    # significand only, so "sufficient" means a 24-bit-significand unit with
    # the 387's 15-bit exponent, never an fp32 one.
    reasons = []
    for pc, n in sorted(pc_arith.items()):
        if pc != '24' and n:
            reasons.append(f'{n} arithmetic ESCs at PC={pc}')
    for bucket in ('le53', 'le64'):
        if sig_conservative.get(bucket):
            reasons.append(f'{sig_conservative[bucket]} steps left ST(0) with a significand in {bucket}')
    for form in ('db.m5', 'db.m7'):
        if ops.get(form):
            reasons.append(f'{ops[form]} {"FLD" if form == "db.m5" else "FSTP"} m80')
    # Probe only, mechanically, and this is the method's ORIGINAL committed
    # definition: zero load, store, FXCH, arithmetic, FRNDINT and
    # transcendental forms. FRNDINT is in the predicate because the
    # classification counted it as arithmetic when the rule was written
    # (9601ee0da); amendment 2 split it into `round` for being outside
    # precision control, and leaving it out here would let that
    # reclassification narrow the rule by a side effect. Amendment 1 had
    # widened it to "no memory operand,
    # no FXCH, no transcendental" so that the 287/387 infinity test (FLD1,
    # FLDZ, FDIVP, FCHS, FCOMPP), which is register-only, would still count as
    # a probe. That widening also admitted *unbounded* register-form
    # arithmetic, which would suppress the sufficiency verdict for a title
    # computing entirely from constants and registers. No title in the census
    # runs the infinity test -- DOOM retires 11 ESCs and none of them is
    # arithmetic -- so the narrower original rule classifies all nine
    # identically, and it is restored rather than patched.
    memory_fp = sum(n for form, n in ops.items()
                    if (X87_FORMS.get(form) or ('', 'none'))[1] in
                    ('real32', 'real64', 'real80', 'int16', 'int32', 'int64', 'bcd80'))
    computing = bool(by_class.get('load', 0) or by_class.get('store', 0) or by_class.get('fxch', 0)
                     or by_class.get('arith', 0) or by_class.get('round', 0) or by_class.get('trans', 0))
    return {
        'computes': computing,
        'memory_fp_ops': memory_fp,
        'calibration': {'x87op_total': retired,
                        'esc_stem_total': sum(n for k, n in counts.items()
                                              if re.match(r'^(?:67)?(?:66)?D[89A-F]\.\d(?::|$)', k)),
                        'faulted': sum(n for k, n in counts.items() if k.startswith('x87fault:')),
                        'held': sum(n for k, n in counts.items() if k.startswith('x87held:'))},
        'probe_only': not computing,
        'pc24_sufficient': (not reasons) if computing else None,
        'pc24_insufficient_because': reasons,
        'retired': retired,
        'by_form': dict(sorted(ops.items())),
        'by_class': dict(sorted(by_class.items())),
        'by_operand': dict(sorted(by_operand.items())),
        'unknown_forms': dict(sorted(unknown.items())),
        'pc_arith': dict(sorted(pc_arith.items())),
        'rc_arith': dict(sorted(rc_arith.items())),
        'pc_all': dict(sorted(pc_all.items())),
        'rc_all': dict(sorted(rc_all.items())),
        'significand': dict(sorted(sig.items())),
        'significand_forms': {b: dict(sorted(c.items())) for b, c in sorted(sig_by_form.items())},
        'exception_masks': dict(sorted(masks.items())),
        'pc_by_class': {c: dict(sorted(v.items())) for c, v in sorted(pc_by_class.items())},
        'rc_by_class': {c: dict(sorted(v.items())) for c, v in sorted(rc_by_class.items())},
        'fsqrt': ops.get('d9fa', 0),
        'frndint': ops.get('d9fc', 0),
        'frndint_by_pc': dict(sorted(frndint_pc.items())),
        'irq13': irq13,
        'x87_key_version': version,
        # Both from the key set's version, not from which keys happen to be
        # present: a post-fix run in which IRQ 13 never fires has no
        # x87irq13 key either, and would otherwise be indistinguishable from
        # a file written before the key existed.
        'irq13_recorded': version >= 2,
        'irq13_any_vector': version >= 3,
        'store_exponents_recorded': version >= 3,
        'register_store_widths_reliable': version >= 2,
        'store_exponent_range': {k[len('x87exp:'):]: n for k, n in sorted(counts.items())
                                 if k.startswith('x87exp:')},
        'significand_excluding_register_stores': dict(sorted(sig_conservative.items())),
        'interrupts_with_error_pending': err_intr,
        'interrupts_with_error_pending_by_vector': {k[len('x87errintr:'):]: n for k, n in sorted(counts.items())
                                                    if k.startswith('x87errintr:')},
        'relies_on_exceptions': bool(r_es or irq13 or err_intr),
        'fsqrt_by_pc': dict(sorted(pc_by_class.get('sqrt', Counter()).items())),
        'pc_sites': {p: dict(sorted(c.items())) for p, c in sorted(pc_sites.items())},
        'cw_writes': dict(sorted(cw_sets.items())),
        'cw_by_second': {k: sorted(v) for k, v in sorted(cw_seconds.items(), key=lambda kv: int(kv[0]))},
        'es_set': {k[len('x87es:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87es:')},
        'exceptions': {k[len('x87exc:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87exc:')},
        'faults': {k[len('x87fault:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87fault:')},
        'held': {k[len('x87held:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87held:')},
        'emulator_ints': {k[len('x87emuint:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87emuint:')},
        'coprocessor_io': {k[len('x87io:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87io:')},
        'cr0_changes': {k[len('x87cr0:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87cr0:')},
        'events': {k[len('x87ev:'):]: n for k, n in sorted(counts.items()) if k.startswith('x87ev:')},
        'fwait': sum(n for k, n in counts.items() if k.startswith('x87wait')),
        'stem_total': sum(n for k, n in counts.items()
                          if re.match(r'^(?:67)?(?:66)?D[89ABCDEF]\.\d(?::|$)', k)),
    }


def run(title: str, directory: Path) -> dict:
    census = directory / 'census.txt'
    if not census.exists():
        return {'title': title, 'missing': True}
    counts = load(census)
    row = {'title': title, **summarize(counts), 'x87': x87_summary(counts)}
    report = directory / 'report.json'
    if report.exists():
        end = json.loads(report.read_text()).get('end') or {}
        row['end'], row['seconds'] = end.get('reason'), end.get('seconds')
    return row


def line(row: dict) -> str:
    if row.get('missing'):
        return f"{row['title']:14} MISSING"
    tlb = row['tlb']
    rate = lambda name: tlb.get(name, {}).get('walks_per_100')
    return (f"{row['title']:14} end={row.get('end')} paged={row['paged_steps']} "
            f"ts={sum(row['task_switches'].values())} v86={sum(row['v86_steps'].values())} "
            f"cr3w={row['sysreg'].get('cr3', {}).get('write', 0)} pf={sum(row['pf_by_error'].values())} "
            f"dtlb16={rate('dtlb16')} itlb8={rate('itlb8')}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('runs', nargs='+', metavar='TITLE=DIR', help='a run: its title and its output directory')
    parser.add_argument('--out', type=Path, help='write the JSON rows here (default: stdout)')
    args = parser.parse_args(argv)
    rows = []
    for spec in args.runs:
        title, sep, directory = spec.partition('=')
        if not sep:
            parser.error(f'{spec}: expected TITLE=DIR')
        rows.append(run(title, Path(directory)))
    text = ''.join(json.dumps(r, sort_keys=True) + '\n' for r in rows)
    if args.out:
        args.out.write_text(text)
        for r in rows:
            print(line(r))
    else:
        sys.stdout.write(text)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
