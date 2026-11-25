#!/usr/bin/env python3
"""results.py FITS_DIR: the record's markdown tables from rung_summary.py's JSON files.

FITS_DIR holds <label>.json files, each rung_summary.py output for one seed, labelled
<rung>-s<seed> (control-s1, c42-s1, ...). Prints:
- one row per seed: machine clock (from the PLL report), ALMs, the worst machine-to-machine
  (period-scaled) setup slack over every corner from paths.tcl, the machine clock's summary
  setup and hold rows (which also hold fixed-bound crossings latched by it), the worst of every
  analysis over all clocks, and the pass rule;
- one row per seed: the machine clock's worst setup slack by owner pair (slow 0C corner), with
  anonymous registers resolved (rung_summary.resolver), the five tightest pairs.
"""
import json
import sys
from pathlib import Path

MACHINE = 'fes_machine_pll|machine_pll_outclk0'
ANALYSES = ('Setup', 'Hold', 'Recovery', 'Removal', 'Minimum Pulse Width', 'Max Skew', 'Net Delay',
            'Setup Data Delay')
ORDER = {'control': 51, **{f'c{c}': c for c in (42, 37, 34, 32, 30, 28, 27, 26, 25, 24, 22, 20, 18, 16)}}


def key(label):
    rung, seed = label.rsplit('-s', 1)
    return (-ORDER.get(rung, 0), int(seed))


def main(directory):
    seeds = {p.stem: next(iter(json.loads(p.read_text()).values())) for p in Path(directory).glob('*.json')}
    print('| Rung | MHz | Seed | ALMs | Machine→machine setup | Machine clock setup row | Machine clock hold row | '
          + ' | '.join(ANALYSES) + ' | Fmax est. | Pass |')
    print('| --- | ---: | ---: | ---: | ---: | ---: | ---: | ' + ' | '.join('---:' for _ in ANALYSES) + ' | ---: | --- |')
    for label in sorted(seeds, key=key):
        d = seeds[label]
        hz = int(d['pll']['outputs'][0][0].split()[0])
        machine = {a: min((r['slack'] for r in d['timing'] if r['clock'] == MACHINE and r['analysis'] == a),
                          default=None) for a in ('Setup', 'Hold')}
        scaled = min((c['machine_worst'][0]['slack'] for c in d.get('paths', {}).values() if c['machine_worst']),
                     default=None)
        cells = [f"{d['worst'][a]['slack']:+.3f}" if a in d['worst'] else '—' for a in ANALYSES]
        rung, seed = label.rsplit('-s', 1)
        print(f"| {rung} | {hz / 1e6:.3f} | {seed} | {d['alms']:,.1f} | "
              f"{'—' if scaled is None else format(scaled, '+.3f')} | {machine['Setup']:+.3f} | "
              f"{machine['Hold']:+.3f} | " + ' | '.join(cells) +
              f" | {d['fmax_machine'][0] if d['fmax_machine'] else '—'} | {'pass' if d['passes'] else '**FAIL**'} |")
    print()
    print('| Rung | Seed | Owner pair (resolved): worst machine-to-machine setup slack, Slow fix4 0C (endpoints reported) |')
    print('| --- | ---: | --- |')
    for label in sorted(seeds, key=key):
        c = seeds[label].get('paths', {}).get('4_slow_fix4_0c')
        if not c:
            continue
        pairs = sorted(c['resolved_pairs'].items(), key=lambda kv: kv[1]['worst'])[:5]
        rung, seed = label.rsplit('-s', 1)
        print(f"| {rung} | {seed} | " + '; '.join(f"{k} {v['worst']:+.3f} ({v['count']})" for k, v in pairs) + ' |')


if __name__ == '__main__':
    main(sys.argv[1])
