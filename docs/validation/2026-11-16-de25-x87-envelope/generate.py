#!/usr/bin/env python3
"""Emit the record's results tables from the run reports.

Record prose is generated from the data rather than typed, because a typed
figure drifts from the run that produced it. What this cannot do is decide
what the numbers mean; the prose around the tables is written by hand and
says what is measured and what is inferred.

  python3 docs/validation/2026-11-16-de25-x87-envelope/generate.py \
      --evidence <dir of the sim reports> --fits <dir of the fit records>
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path

OPS = ('ADD', 'SUB', 'MUL', 'DIV', 'SQRT', 'CMP', 'UCMP', '(reserved)')
# req_op 5 and 6 are zero by construction: the envelope answers FCOM and
# FUCOM in A_SETUP, from the operand classes, and never calls the backend.
BY_CONSTRUCTION = {5, 6, 7}


def load(path: Path):
    return json.loads(path.read_text()) if path.is_file() else None


def unit_table(evidence: Path) -> str:
    rows = []
    for name, label in (('unit-de10.json', 'DE10 sequenced arithmetic, RTL'),
                        ('unit-de25.json', 'DE25 `fes_x87_core`, RTL'),
                        ('unit-de10-netlist.json', "DE10, the seal's Yosys netlist")):
        r = load(evidence / name)
        if not r:
            continue
        rows.append(f"| {label} | {r['passed']} | {r['failed']} |")
    if not rows:
        return '_no unit-bench reports found_'
    return ('| Configuration | Cases passed | Failed |\n| --- | ---: | ---: |\n'
            + '\n'.join(rows))


def backend_counts(evidence: Path) -> str:
    r = load(evidence / 'lockstep-de25.json')
    if not r:
        return '_no DE25 lockstep report found_'
    suites = {k: v for k, v in r.items() if isinstance(v, dict) and 'summary' in v}
    offered = [0] * 8
    taken = [0] * 8
    for suite in suites.values():
        s = suite['summary']
        for n in range(8):
            offered[n] += s.get('x87_backend_offered', [0] * 8)[n]
            taken[n] += s.get('x87_backend_taken', [0] * 8)[n]
    out = ['| `req_op` | Operation | Offered | Taken | Reading |',
           '| ---: | --- | ---: | ---: | --- |']
    for n, op in enumerate(OPS):
        if taken[n]:
            reading = 'the gate reached this operation'
        elif n in BY_CONSTRUCTION:
            reading = 'zero **by construction** (see the text)'
        else:
            reading = '**a hole in the gate**'
        out.append(f'| {n} | {op} | {offered[n]:,} | {taken[n]:,} | {reading} |')
    out.append('')
    out.append(f'Totals: **{sum(offered):,} offered, {sum(taken):,} taken** across '
               f'{len(suites)} suites.')
    if sum(offered) != sum(taken):
        out.append('Offered and taken differ, which means the backend refused an '
                   'offer: with one ESC instruction in flight that should be '
                   'impossible, so it is reported rather than explained away.')
    return '\n'.join(out)


def lockstep_table(evidence: Path) -> str:
    rows = []
    for name, label in (('lockstep-de25.json', 'DE25'), ('lockstep-de10.json', 'DE10')):
        r = load(evidence / name)
        if not r:
            continue
        for suite, body in r.items():
            if not isinstance(body, dict) or 'summary' not in body:
                continue
            s = body['summary']
            rows.append(f"| {label} | {suite} | {s['passed']} | {s['failed']} | "
                        f"{s.get('x87_compared', 0)} | `{s['trace_hash']}` |")
    if not rows:
        return '_no lockstep reports found_'
    return ('| Backend | Suite | Passed | Failed | x87 states compared | Trace hash |\n'
            '| --- | --- | ---: | ---: | ---: | --- |\n' + '\n'.join(rows))


def hash_compare(evidence: Path) -> str:
    a, b = load(evidence / 'lockstep-de10.json'), load(evidence / 'lockstep-de25.json')
    if not (a and b):
        return '_both lockstep reports are needed for this comparison_'
    lines = []
    for suite in sorted(set(a) & set(b)):
        if not isinstance(a[suite], dict) or 'summary' not in a[suite]:
            continue
        ha, hb = a[suite]['summary']['trace_hash'], b[suite]['summary']['trace_hash']
        lines.append(f"| {suite} | `{ha}` | `{hb}` | {'same' if ha == hb else '**differ**'} |")
    if not lines:
        return '_no comparable suites_'
    return ('| Suite | DE10 | DE25 | |\n| --- | --- | --- | --- |\n' + '\n'.join(lines))


FORMS = ('fadd', 'fsub', 'fmul', 'fdiv', 'fsqrt', 'fscale', 'frndint', 'fprem')
SEQUENCER_BOTH = ('fscale', 'frndint', 'fprem')


def latency_table(evidence: Path) -> str:
    """Clocks from the instruction's commit to `busy_o` falling, which is what
    the next ESC waits for, at each precision. FSCALE, FRNDINT and FPREM stay
    on the sequencer in BOTH configurations, so their rows are a positive
    control: a difference there would mean the harness was measuring something
    other than the backend."""
    a, b = load(evidence / 'unit-de10.json'), load(evidence / 'unit-de25.json')
    if not (a and b and a.get('latency_clocks') and b.get('latency_clocks')):
        return '_both unit-bench reports with latency lines are needed_'
    la, lb = a['latency_clocks'], b['latency_clocks']
    out = ['| Form | PC | DE10 sequencer | DE25 backend | Difference | |',
           '| --- | ---: | ---: | ---: | ---: | --- |']
    for form in FORMS:
        for bits in (24, 53, 64):
            key = f'{form}.pc{bits}'
            if key not in la or key not in lb:
                continue
            d = lb[key] - la[key]
            if form in SEQUENCER_BOTH:
                note = 'positive control: must be 0' if d == 0 else '**control broken**'
            elif d < 0:
                note = '**the backend is faster**'
            elif d == 0:
                note = 'equal'
            else:
                note = ''
            out.append(f'| {form.upper()} | {bits} | {la[key]} | {lb[key]} | {d:+d} | {note} |')
    return '\n'.join(out)


def fit_table(fits: Path) -> str:
    rows = []
    for record in sorted(fits.glob('*/build.json')):
        r = load(record)
        if not r:
            continue
        res = r.get('resources', {})
        def used(k):
            v = res.get(k, {}).get('used')
            return '-' if v is None else f'{int(v):,}'
        def pct(k):
            v, total = res.get(k, {}).get('used'), res.get(k, {}).get('available')
            return '-' if not (v and total) else f'{100.0 * v / total:.1f}%'
        corners = r.get('timing', {}).get('corners', [])
        worst = min((c.get('setup') for c in corners if c.get('setup') is not None), default=None)
        rows.append(f"| {record.parent.name} | {r['x87_backend']} | {r['seed']} | "
                    f"{r['period_ns']:.3f} | {r['status']} | {used('alms')} | {pct('alms')} | "
                    f"{used('registers')} | {used('dsp_blocks')} | {used('m20k_blocks')} | "
                    f"{'-' if worst is None else f'{worst:+.3f} ns'} |")
    if not rows:
        return '_no fit records found_'
    return ('| Run | Backend | Seed | Period (ns) | Status | ALMs | of device | Registers | DSP | M20K | Worst setup |\n'
            '| --- | --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n'
            + '\n'.join(rows))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--fits', type=Path, required=True)
    args = parser.parse_args()
    print('### The unit bench, against the host\'s own x87\n')
    print(unit_table(args.evidence))
    print('\n### What the backend was actually asked to do\n')
    print(backend_counts(args.evidence))
    print('\n### Lockstep against the Bochs oracle\n')
    print(lockstep_table(args.evidence))
    print('\n### Trace hashes, DE10 against DE25\n')
    print('`trace_hash` is folded from EIP, the eight GPRs and EFLAGS at every')
    print('retirement (`hash_state`). It covers the **integer architectural**')
    print('trace: not cycle timing, and **not the x87 register stack**. So')
    print('identical hashes say the two configurations are indistinguishable to')
    print('the rest of the CPU -- which is what rule 5 asks -- and say nothing')
    print('about whether the arithmetic agrees. That evidence is the x87 state')
    print('comparison in the table above, and the unit bench.\n')
    print(hash_compare(args.evidence))
    print('\n### Per-form latency, in CPU clocks\n')
    print(latency_table(args.evidence))
    print('\n### The Agilex 5 fit\n')
    print(fit_table(args.fits))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
