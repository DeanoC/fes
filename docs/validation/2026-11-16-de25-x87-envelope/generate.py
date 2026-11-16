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
                        ('unit-de10-netlist.json', "DE10, the seal's Yosys netlist"),
                        ('unit-de25-netlist.json',
                         "DE25, the seal's Yosys netlist — 240 failures before the packed array")):
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


def lockstep_whole(evidence: Path) -> str:
    """The run as a whole, not only the eight directed suites. The corpus and
    the random programs are the bulk of a lockstep run, and a table of the
    directed suites alone understates what passed.

    `default_suite` is **false** for a DE25 run by design: `backend` is a
    suite argument, so a report from a differently parameterised CPU cannot
    be accepted by the pc386 sealer's --lockstep-report as covering the DE10
    seal."""
    rows = []
    for name, label in (('lockstep-baseline.json', 'baseline at the merge commit'),
                        ('lockstep-de10.json', 'DE10 sequenced arithmetic'),
                        ('lockstep-de25.json', 'DE25 `fes_x87_core`'),
                        ('lockstep-de10-netlist.json', "DE10, the seal's netlist"),
                        ('lockstep-de25-netlist.json', "DE25, the seal's netlist")):
        r = load(evidence / name)
        if not r:
            continue
        run = r.get('run', {})
        ct = (r.get('corpus') or {}).get('totals', {})
        rt = (r.get('random') or {}).get('totals', {}) or (r.get('random') or {})
        rows.append(
            f"| {label} | {'**pass**' if run.get('passed') else '**FAIL**'} | "
            f"{str(run.get('default_suite')).lower()} | "
            f"{ct.get('tests', '-')} | {ct.get('passed', '-')} | {ct.get('failed', '-')} | "
            f"{rt.get('programs', rt.get('runs', '-'))} | {rt.get('failed', '-')} |")
    if not rows:
        return '_no lockstep reports found_'
    return ('| Run | Verdict | `default_suite` | Corpus tests | passed | failed | Random programs | failed |\n'
            '| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |\n' + '\n'.join(rows))


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
    for record in sorted(list(fits.glob('*/build.json')) + [f for f in fits.glob('unit-*.json')
                         if not f.name.endswith('.inputs.json')]):
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
        # Slack lives under corners[].analyses.<kind>.worst_slack_ns. Reading
        # a wrong key here printed '-' silently for the first three seeds,
        # which is why the generator now fails on a missing one rather than
        # dashing it out.
        corners = r.get('timing', {}).get('corners', [])
        slacks = [c['analyses']['setup']['worst_slack_ns'] for c in corners
                  if c.get('analyses', {}).get('setup', {}).get('worst_slack_ns') is not None]
        if corners and not slacks:
            raise SystemExit(f'{record}: timing record has corners but no setup slack; '
                             'the key this generator reads has moved')
        worst = min(slacks, default=None)
        holds = [c['analyses']['hold']['worst_slack_ns'] for c in corners
                 if c.get('analyses', {}).get('hold', {}).get('worst_slack_ns') is not None]
        worst_hold = min(holds, default=None)
        # The label carries X87_BACKEND and x87_present, not just the run
        # name. The sibling machine measurement produces a similar-looking
        # ALM delta at X87_BACKEND=0, and a reader who meets two unlabelled
        # deltas will average them.
        backend_n = 1 if r['x87_backend'] == 'de25' else 0
        kind = r.get('x87_present')
        label = (f"`{r.get('top', '?')}`, X87_BACKEND={backend_n}"
                 + (f", x87_present={kind}" if kind is not None else "")
                 + f", {r.get('virtual_ports', '?')} virtual ports")
        rows.append(f"| {record.stem.replace('.inputs', '')} | {label} | {r['seed']} | "
                    f"{r['period_ns']:.3f} | {r['status']} | {used('alms')} | {pct('alms')} | "
                    f"{used('labs')} | {pct('labs')} | "
                    f"{used('registers')} | {used('dsp_blocks')} | {used('m20k_blocks')} | "
                    f"{'-' if worst is None else f'{worst:+.3f} ns'} | "
                    f"{'-' if worst_hold is None else f'{worst_hold:+.3f} ns'} |")
    if not rows:
        return '_no fit records found_'
    return ('| Run | Top and parameters | Seed | Period (ns) | Status | ALMs | of device | LABs | of device | Registers | DSP | M20K | Worst setup | Worst hold |\n'
            '| --- | --- | ---: | ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n'
            + '\n'.join(rows))


def exclusion_table(fits: Path) -> str:
    """What each fit kept, by exact register leaf name. The DE10 arithmetic is
    inline in the sequencer's always_ff, so no entity table can show its
    absence; this is the evidence instead, and the positive controls are what
    make an absence mean absent rather than renamed."""
    rows = []
    for record in sorted(list(fits.glob('*/build.json')) + [f for f in fits.glob('unit-*.json')
                         if not f.name.endswith('.inputs.json')]):
        r = load(record)
        if not r or 'exclusion' not in r:
            continue
        e = r['exclusion']
        absent = e.get('absent_expected', {})
        controls = e.get('positive_controls', {})
        rows.append(f"| {record.stem} | "
                    f"{', '.join(sorted(n for n, v in absent.items() if v)) or 'none — as required'} | "
                    f"{', '.join(sorted(n for n, v in controls.items() if v)) or '**none — result void**'} | "
                    f"{', '.join(r.get('ram_blocks') or []) or '-'} | "
                    f"{e.get('registers', '-')} |")
    if not rows:
        return '_no fit carries an exclusion record_'
    return ('| Run | Sequenced-arithmetic registers still present | Positive controls found | Memories the fit kept | Registers in fit |\n'
            '| --- | --- | --- | --- | ---: |\n' + '\n'.join(rows))


def rtl_agreement(fits: Path) -> str:
    """Every fit must have been taken from the same RTL. The fit script and
    the timing gate are exempt: they are harness, and editing them between
    seeds does not change the design. This exists because faults were injected
    into this tree while fits were queued, and a timeline argument is not a
    check."""
    HARNESS = {'x87_envelope_fit.py', 'x87_envelope_timing.tcl'}
    seen = {}
    for inputs in sorted(fits.glob('unit-*.inputs.json')):
        d = load(inputs)
        if not d:
            continue
        design = {k: v for k, v in d['source_sha256'].items() if k not in HARNESS}
        seen[inputs.stem.replace('.inputs', '')] = design
    if not seen:
        return '_no fit inputs found_'
    first = next(iter(seen.values()))
    bad = {name: d for name, d in seen.items() if d != first}
    if bad:
        raise SystemExit('fits were taken from different RTL, so their figures cannot be '
                         f'tabled together: {sorted(bad)}')
    return (f'All {len(seen)} fits were taken from the same RTL '
            f'({len(first)} design files, hashes equal across every fit; the fit script and '
            'the timing gate are exempt as harness).')


OPEN = '<!-- generated by generate.py; do not edit between the markers -->'
CLOSE = '<!-- end generated -->'


def emit(args) -> None:
    """Print every table. `main` either shows this or writes it into the
    record between its markers, so that refilling the record is a command and
    the tables cannot drift from the reports they came from."""
    print("### The unit bench, against the host's own x87\n")
    print(unit_table(args.evidence))
    print('\n### What the backend was actually asked to do\n')
    print(backend_counts(args.evidence))
    print('\n### Lockstep against the Bochs oracle\n')
    print(lockstep_whole(args.evidence))
    print()
    print('`default_suite` is **false** for a DE25 run by design: `backend` is a suite')
    print('argument, so a report from a differently parameterised CPU cannot be')
    print("accepted by the pc386 sealer's --lockstep-report as covering the DE10 seal.")
    print('\nThe directed suites, which are what reach the x87 at all:\n')
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
    print(rtl_agreement(args.fits) + '\n')
    print('Worst setup and worst hold are the minimum across all four published')
    print('operating conditions. **Hold is non-negative but not strictly positive**:')
    print('two seeds reach exactly 0.000 ns at their worst corner, which is a pass')
    print('and is also as close to the edge as a pass can be. Every ALM figure is')
    print("the fit's own total and INCLUDES its virtual-pin envelope's cells; they")
    print('are not subtracted, following the sibling Agilex 5 record so that the')
    print('two read the same way. The **LAB columns are LABs TOUCHED, not')
    print('capacity**: a LAB counts as used when one of its ten ALM slots is used,')
    print("so the percentage runs ahead of occupancy. Quartus's own capacity row is")
    print('ALMs needed.\n')
    print(fit_table(args.fits))
    print('\n### What the selection left out\n')
    print(exclusion_table(args.fits))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--fits', type=Path, required=True)
    parser.add_argument('--write', type=Path,
                        help='rewrite this record between its generated markers, instead of '
                             'printing; the markers make refilling a command rather than a '
                             'manual edit, so the tables cannot drift from the reports')
    args = parser.parse_args()
    if args.write:
        import io, contextlib
        buffer = io.StringIO()
        with contextlib.redirect_stdout(buffer):
            emit(args)
        text = args.write.read_text()
        head, _, rest = text.partition(OPEN)
        _, _, tail = rest.partition(CLOSE)
        if not rest or not tail:
            raise SystemExit(f'{args.write} has no generated markers')
        args.write.write_text(head + OPEN + '\n\n' + buffer.getvalue().rstrip()
                              + '\n\n' + CLOSE + tail)
        print(f'rewrote {args.write} between its generated markers')
    

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--fits', type=Path, required=True)
    parser.add_argument('--write', type=Path,
                        help='rewrite this record between its generated markers instead of '
                             'printing')
    args = parser.parse_args()
    if not args.write:
        emit(args)
        return 0
    import contextlib
    import io
    buffer = io.StringIO()
    with contextlib.redirect_stdout(buffer):
        emit(args)
    text = args.write.read_text()
    head, marker, rest = text.partition(OPEN)
    _, close, tail = rest.partition(CLOSE)
    if not marker or not close:
        raise SystemExit(f'{args.write} has no generated markers')
    args.write.write_text(head + OPEN + '\n\n' + buffer.getvalue().rstrip()
                          + '\n\n' + CLOSE + tail)
    print(f'rewrote {args.write} between its generated markers')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
