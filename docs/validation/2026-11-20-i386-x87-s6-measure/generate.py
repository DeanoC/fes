#!/usr/bin/env python3
"""Write results.md for x87 s6 step 6.0 from the committed fit evidence (method.md).

  python3 generate.py            # rewrites results.md
  python3 generate.py --check    # fails if results.md is stale

Reads fits/<name>/fit.json and timing.tsv, and entities.tsv for 6.0b. Every figure
in results.md comes from those files. The bands are method.md's, applied here
mechanically.
"""
import argparse
import csv
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
FITS = HERE / 'fits'
LINE_92 = 43_056            # 92% of the documented 46,800 ALMs (method.md, band 4)
DEVICE_ALMS = 46_800
GATE_PCT = 5.0              # 6.0b's flat-against-hierarchy gate
P1959 = [42_943, 42_956, 42_986, 43_010]   # #1959's four seeds (its fits.md), for the uncontrolled delta
RUNGS = [('a-s1', 61.44), ('c-75', 75), ('c-90', 90), ('c-100', 100), ('c-110', 110), ('c-120', 120)]


def load(name: str) -> dict | None:
    d = FITS / name
    if not (d / 'fit.json').is_file():
        return None
    fit = json.loads((d / 'fit.json').read_text())
    rows = list(csv.DictReader((d / 'timing.tsv').open(), delimiter='\t'))

    def worst(suffix):
        values = [float(r['worst_slack_ns']) for r in rows
                  if r['check'].endswith(suffix) and r['worst_slack_ns'] not in ('', 'None')]
        return min(values) if values else None
    timing = fit['timing']
    r = fit['resources']
    setup_rows = [r for r in rows if r['check'] == 'clk_clock_setup' and r['worst_slack_ns'] not in ('', 'None')]
    path = min(setup_rows, key=lambda r: float(r['worst_slack_ns'])) if setup_rows else None
    return {'name': name, 'fit': fit['status'],
            'clk_path': (path['from'], path['to'], path['corner']) if path else None,
            'timing': timing['status'] if isinstance(timing, dict) else timing,
            'alms': r['alms']['used'], 'pct': r['alms']['percent_of_device'],
            'labs': r['labs']['used'], 'dsp': r['dsp_blocks']['used'], 'm20k': r['ram_blocks']['used'],
            'regs': r['registers']['used'], 'setup': worst('setup'), 'hold': worst('hold'),
            'period': fit['period_ns'], 'seed': fit['seed']}


def ns(x):
    return '—' if x is None else f'{x:+.3f} ns'


def table(rows):
    out = ['| Fit | Seed | MHz | Fit | Timing, all corners | ALMs needed | of device | LABs touched | DSP | M20K | Registers | Worst setup | Worst hold |',
           '| --- | ---: | ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |']
    for r in rows:
        out.append(f"| {r['name']} | {r['seed']} | {1000 / r['period']:.2f} | {r['fit']} | {r['timing']} | "
                   f"{r['alms']:,.0f} | {r['pct']:.2f}% | {r['labs']:,.0f} | {r['dsp']:.0f} | {r['m20k']:.0f} | "
                   f"{r['regs']:,.0f} | {ns(r['setup'])} | {ns(r['hold'])} |")
    return out


def band(rows) -> str:
    if any(r['fit'] != 'pass' or r['alms'] > DEVICE_ALMS for r in rows):
        return 'band 1: does not fit'
    if any(r['timing'] != 'pass' for r in rows):
        return 'band 3: fits but does not pass timing'
    if any(r['alms'] >= LINE_92 for r in rows):
        return 'band 4: not established (ALMs needed at or above 92%, 43,056, on at least one seed)'
    return 'band 2: fits and passes timing at 61.44 MHz'


def part_a(lines):
    rows = [r for r in (load(f'a-s{s}') for s in (1, 2, 3, 4)) if r]
    lines += ['## 6.0a: current main against the working line', '']
    lines += table(rows) + ['']
    if len(rows) < 4:
        lines += [f'{len(rows)} of 4 seeds recorded. No band is judged on fewer than four.', '']
        return rows
    alms = [r['alms'] for r in rows]
    med, spread = statistics.median(alms), max(alms) - min(alms)
    over = [str(r['seed']) for r in rows if r['alms'] >= LINE_92]
    lines += [f'**{band(rows)}.** ALMs needed range from {min(alms):,.0f} to {max(alms):,.0f}, with median {med:,.1f} '
              f'and spread {spread:,.0f}. Seeds at or above the line: {", ".join(over) or "none"}.', '']
    m59, s59 = statistics.median(P1959), max(P1959) - min(P1959)
    lines += [f'**Uncontrolled, against #1959** (median {m59:,.1f}, spread {s59}): the medians differ by '
              f'**{med - m59:+,.1f} ALMs**. The larger of the two spreads is {max(spread, s59):,.0f}. '
              "The base moved as well as the x87, so no PR's cost is read from this.", '']
    return rows


def part_b(lines, flat):
    lines += ['## 6.0b: where the ALMs are', '']
    hier = load('b-hier')
    if not hier:
        lines += ['Not recorded yet.', '']
        return
    lines += table([hier]) + ['']
    if flat:
        gap = 100.0 * (hier['alms'] - flat['alms']) / flat['alms']
        within = abs(gap) <= GATE_PCT
        lines += [f"Against 6.0a seed 1's flattened {flat['alms']:,.0f} ALMs, the hierarchy fit differs by "
                  f"**{gap:+.2f}%**, {'within' if within else 'outside'} the ±{GATE_PCT:.0f}% gate. So the table below is "
                  + ('**the ranking**.' if within else '**indicative**, and ranks nothing on its own.'), '']
    ent = HERE / 'entities.tsv'
    if ent.is_file():
        rows = [r for r in csv.DictReader(ent.open(), delimiter='\t') if r['depth'] in ('1', '2', '3')]
        rows.sort(key=lambda r: -float(r['alms_total']))
        lines += ['The table lists every entity at depth 1 to 3 with at least 100 ALMs, counting its '
                  'sub-entities, largest first. "Own" is the entity without its sub-entities. Each figure '
                  'bounds what removing that entity could save; it is not a measured saving.', '',
                  '| Depth | Entity | ALMs | Own | Registers | M20K | DSP |',
                  '| ---: | --- | ---: | ---: | ---: | --- | ---: |']
        for r in rows:
            if float(r['alms_total']) >= 100:
                lines.append(f"| {r['depth']} | `{r['path'].removeprefix('<top>/')}` | {float(r['alms_total']):,.1f} | "
                             f"{float(r['alms_own']):,.1f} | {int(r['registers_total']):,} | {r['m20k']} | {r['dsp']} |")
        lines.append('')


def part_c(lines):
    lines += ['## 6.0c: the clock ladder, seed 1', '']
    rows = [r for r in (load(n) for n, _ in RUNGS) if r]
    lines += table(rows) + ['']
    lines += ["Each rung's worst `clk` setup path, endpoints as the fitter names them:", '',
              '| Rung | From | To | Corner |', '| ---: | --- | --- | --- |']
    for r in rows:
        if r['clk_path']:
            f, to, corner = r['clk_path']
            lines.append(f"| {1000 / r['period']:.2f} | `{f}` | `{to}` | {corner} |")
    lines.append('')
    passing = []
    for name, mhz in RUNGS:
        r = load(name)
        if r is None or r['fit'] != 'pass' or r['timing'] != 'pass':
            break
        passing.append(mhz)
    above = [m for (n, m) in RUNGS[len(passing) + 1:] if (r := load(n)) and r['fit'] == 'pass' and r['timing'] == 'pass']
    if passing:
        lines += [f'The highest rung at which seed 1 passes, with every lower rung also passing, is '
                  f'**{passing[-1]:g} MHz**. A closure is claimed only after seeds 2 to 4 pass at that '
                  'rung (method.md).', '']
    if above:
        lines += [f'Seed 1 also passes at {", ".join(f"{m:g}" for m in above)} MHz, above a rung that '
                  'misses. That is reported here and is not a closure.', '']
    seeds = [r for r in (load(f'd-{m}-s{s}') for _, m in RUNGS[1:] for s in (2, 3, 4)) if r]
    if seeds:
        lines += ['### Closure seeds', ''] + table(seeds) + ['']


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    lines = ['# x87 s6 step 6.0: results', '',
             '<!-- generated by generate.py from fits/ and entities.tsv; do not edit -->', '',
             'The bands and rules are in [method.md](method.md). The pixel clock is 74.25 MHz throughout; '
             'MHz in the tables is the system clock.', '']
    a = part_a(lines)
    part_b(lines, next((r for r in a if r['seed'] == 1), None))
    part_c(lines)
    text = '\n'.join(lines).rstrip() + '\n'
    out = HERE / 'results.md'
    if args.check:
        if not out.is_file() or out.read_text() != text:
            print('results.md is stale; run generate.py', file=sys.stderr)
            return 1
        return 0
    out.write_text(text)
    return 0


if __name__ == '__main__':
    sys.exit(main())
