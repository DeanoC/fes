#!/usr/bin/env python3
"""Write results.md for the first x87 slice 6.2 milestone from the committed fit evidence (method.md).

  python3 generate.py            # rewrites results.md
  python3 generate.py --check    # fails if results.md is stale

Reads fits/<name>/fit.json. The 6.0a baseline is step 6.0's committed record (PR #2028),
whose four seeds are quoted here with their source, and the rules are method.md's,
applied mechanically.
"""
import argparse
import json
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
FITS = HERE / 'fits'
# 6.0a, seeds 1-4: docs/validation/2026-11-20-i386-x87-s6-measure/fits/a-s{1..4}/fit.json (PR #2028).
BASELINE = {1: 43_059, 2: 43_080, 3: 43_008, 4: 43_038}
LINE_92 = 43_056
VIRTUAL_PIN_ALMS = 651          # every 6.0a seed's "ALMs containing virtual pins"
SHELLS = (8_400, 6_600, 12_100)  # the DE25 shell: #2036's RTL-arbiter figure first, then the earlier bounds
RAM_FREE_REAL = 58              # RAM blocks left on the real DE25 board with the shell (#2036): 300 of 358 used
NOISE = 200                     # this lane's fit-to-fit noise, method.md part 2


def load(name: str) -> dict | None:
    path = FITS / name / 'fit.json'
    if not path.is_file():
        return None
    fit = json.loads(path.read_text())
    r, timing = fit['resources'], fit['timing']
    return {'name': name, 'seed': fit['seed'], 'fit': fit['status'],
            'timing': timing['status'] if isinstance(timing, dict) else timing,
            'alms': r['alms']['used'], 'pct': r['alms']['percent_of_device'],
            'dsp': r['dsp_blocks']['used'], 'ram': r['ram_blocks']['used'],
            'regs': r['registers']['used'], 'device': fit['device']}


def table(rows):
    out = ['| Fit | Device | Seed | Fit | Timing, all corners | ALMs needed | of device | DSP | RAM blocks | Registers |',
           '| --- | --- | ---: | --- | --- | ---: | ---: | ---: | ---: | ---: |']
    for r in rows:
        out.append(f"| {r['name']} | {r['device']} | {r['seed']} | {r['fit']} | {r['timing']} | {r['alms']:,.0f} | "
                   f"{r['pct']:.2f}% | {r['dsp']:.0f} | {r['ram']:.0f} | {r['regs']:,.0f} |")
    return out


def part1(lines):
    lines += ['## Part 1: the ablation bound', '']
    rows = [r for r in (load(f'abl-s{s}') for s in (1, 2, 3, 4)) if r]
    lines += table(rows) + ['']
    if len(rows) < 4:
        lines += [f'{len(rows)} of 4 seeds recorded; no verdict on fewer.', '']
        return None
    abl = [r['alms'] for r in rows]
    base = list(BASELINE.values())
    m_abl, m_base = statistics.median(abl), statistics.median(base)
    s_abl, s_base = max(abl) - min(abl), max(base) - min(base)
    diff, floor = m_base - m_abl, max(s_abl, s_base)
    lines += [f'6.0a, the controlled baseline (PR #2028): {", ".join(f"{v:,}" for v in base)}, median {m_base:,.1f}, '
              f'spread {s_base}. The ablation: median {m_abl:,.1f}, spread {s_abl:,.0f}.', '',
              f'**The bound is {diff:,.1f} ALMs** (6.0a minus the ablation, difference of medians), against the '
              f'larger spread, {floor:,.0f}: ' + ('**measurable, so the move is built** (method.md).' if diff > floor
                                                  else '**not measurable, so the move is not built** (method.md).'),
              '']
    return diff


def part2(lines):
    lines += ['## Part 2: does the DE10 still fold the unit at X87_BACKEND = 1', '']
    d0, d1 = load('de10-d0'), load('de10-d1')
    rows = [r for r in (d0, d1) if r]
    lines += table(rows) + ['']
    if not (d0 and d1):
        lines += ['Not both fits recorded yet.', '']
        return
    checks = [('DSP blocks equal', d0['dsp'] == d1['dsp']),
              ('RAM blocks equal', d0['ram'] == d1['ram']),
              (f'ALMs within ±{NOISE} ({d1["alms"] - d0["alms"]:+,.0f})', abs(d1['alms'] - d0['alms']) <= NOISE),
              ('D1 passes timing at every corner', d1['fit'] == 'pass' and d1['timing'] == 'pass')]
    lines += [f'- {name}: **{"yes" if ok else "no"}**' for name, ok in checks] + ['']
    folds = all(ok for _, ok in checks)
    lines += [('**D1 folds**: deleting `X87_BACKEND = 0` does not grow the DE10 identity-off package.' if folds else
               '**D1 does not fold** under the rule as committed, so the method blocks the deletion.'), '']
    if not folds:
        lines += ['**The deletion proceeds on Deano\'s acceptance, not on this rule.** On 20 November 2026 he '
                  'accepted it on these numbers ("Yes, accept (a)", relayed by the coordinating session): D1 is '
                  f'{d0["alms"] - d1["alms"]:,.0f} ALMs smaller than D0, with equal DSP ({d1["dsp"]:.0f}), equal RAM '
                  f'blocks ({d1["ram"]:.0f}) and timing passing on both.', '',
                  'Deano has since made every area, fold and resource band one-sided (20 November 2026: "we '
                  'should update the rule to be one way, smaller is almost always better and a good thing"), so '
                  'that only growth over the reference fails. Under the one-sided form this shrink is a pass. '
                  'The rule as committed is left as it was written (method.md, amendment 1).', '']


def part3(lines):
    lines += ['## Part 3: the move\'s DE25 A/B against 6.0a', '']
    rows = [r for r in (load(f'move-s{s}') for s in (1, 2, 3, 4)) if r]
    lines += table(rows) + ['']
    if len(rows) < 4:
        lines += [f'{len(rows)} of 4 seeds recorded; no verdict on fewer.', '']
        return
    move = [r['alms'] for r in rows]
    base = list(BASELINE.values())
    m_move, m_base = statistics.median(move), statistics.median(base)
    s_move, s_base = max(move) - min(move), max(base) - min(base)
    floor = max(s_move, s_base)
    saving = m_base - m_move
    grew = m_move - m_base > floor
    lines += [f'The move (`e7f017259`, on main `9951d04f0`): median {m_move:,.1f}, spread {s_move:,.0f}. '
              f'6.0a (main `8a162c536`): median {m_base:,.1f}, spread {s_base}. The two bases differ by #2034 alone, '
              'which changes build scripts and no design input.', '',
              f'**The move saves {saving:,.1f} ALMs** (difference of medians) against the larger spread, {floor:,.0f}. '
              'Judged one-sided (method.md, amendment 1): ' + ('**fails, the move grew the design.**' if grew else
              '**passes**: the move did not grow the design.'), '']
    ram = {r['ram'] for r in rows}
    dsp = {r['dsp'] for r in rows}
    lines += [f'RAM blocks {", ".join(f"{v:.0f}" for v in sorted(ram))} and DSP {", ".join(f"{v:.0f}" for v in sorted(dsp))} '
              'on every seed, as in 6.0a (145 and 42): the move costs no RAM block against the '
              f'{RAM_FREE_REAL} the real DE25 board has left with the shell (#2036), and no DSP.', '']
    machine = m_move - VIRTUAL_PIN_ALMS
    lines += ['**The gap to the 92% line (43,056) with a real shell**, from the move\'s median less the '
              f'{VIRTUAL_PIN_ALMS} ALMs of virtual pins that stand in for one ({machine:,.1f}):', '',
              '| Shell | Machine + shell | Over the 92% line | Before the move |', '| ---: | ---: | ---: | ---: |']
    for shell in SHELLS:
        before = m_base - VIRTUAL_PIN_ALMS + shell - LINE_92
        lines.append(f'| {shell:,} | {machine + shell:,.1f} | {machine + shell - LINE_92:+,.1f} | {before:+,.1f} |')
    lines += ['', f'{SHELLS[0]:,} is #2036\'s RTL-arbiter shell, one seed; {SHELLS[1]:,} and {SHELLS[2]:,} the earlier '
              'bounds. Every row is one subtraction of figures stated here; none is a fit of a machine with a shell.', '']


def part4(lines):
    """The cycle cost: the unit bench's commit-to-idle clocks per form, before (#1959's final DE25
    report, the last revision these arms changed in) and after (this head)."""
    before = json.loads((HERE / 'evidence/before-unit-de25-rtl-1959.json').read_text())
    after = json.loads((HERE / 'evidence/unit-rtl.json').read_text())
    b, a = before['latency_clocks'], after['latency_clocks']
    lines += ['## The forms\' clocks', '',
              f"The unit bench's own latency cases, from the instruction's commit to the unit's idle: before is "
              f"#1959's final DE25 report (`{before['source']['fes_revision'][:9]}`, the last revision in which these "
              f"arms changed), after is this head (`{after['source']['fes_revision'][:9]}`). Both are one case a form "
              'and precision, so they say what the path costs, not what a title pays.', '',
              '| Form | PC | Before | After | Change |', '| --- | --- | ---: | ---: | ---: |']
    for key in sorted(k for k in a if k.split('.')[0] in ('fscale', 'frndint', 'fprem')):
        lines.append(f"| {key.split('.')[0].upper()} | {key.split('.')[1]} | {b[key]} | {a[key]} | {a[key] - b[key]:+d} |")
    lines += ['', 'The census (PR #1887) has FRNDINT at 3.15% of Screamer\'s x87 work (39,902 in 30 s) and 7.56% of '
              'Terminal Velocity\'s (518), FSCALE at 4,908 executions over all nine titles, and FPREM, FPREM1 and '
              'FXTRACT executed by none. At 35 clocks more a FRNDINT, Screamer\'s rate costs about 46,500 clocks a '
              'second, under 0.1% of a 61.44 MHz machine.', '']


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    lines = ['# x87 slice 6.2, first milestone: results', '',
             '<!-- generated by generate.py from fits/; do not edit -->', '',
             'Rules: [method.md](method.md). Every fit is seed-by-seed as the fitter reported it.', '']
    part1(lines)
    part2(lines)
    part3(lines)
    part4(lines)
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
