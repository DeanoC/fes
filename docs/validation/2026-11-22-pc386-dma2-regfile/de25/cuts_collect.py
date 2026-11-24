#!/usr/bin/env python3
"""Collect the DE25 shell-cut fits: per arm and seed, whole-design ALMs needed, M20K, MLAB memory
ALMs ([d] ALMs used for memory), MLAB bits, DSP blocks needed and the worst slack of each analysis
type (setup, hold, recovery, removal, minimum pulse width) over every clock; then each arm's median
and spread, and each cut's saving against the base with the measured rule (saving > larger spread).

  cuts_collect.py DIR BASE ARM [ARM ...] > collect.json

DIR holds board-<tag>-out/combined/hps/project (seed 1) and board-<tag>-s<N>/project (seeds 2-4),
as board-pair.sh writes them.
"""
import json, re, statistics, sys
from pathlib import Path

ROOT = Path(sys.argv[1])
BASE, ARMS = sys.argv[2], sys.argv[3:]
KINDS = ('Setup', 'Hold', 'Recovery', 'Removal', 'Minimum Pulse Width')


def num(text, pattern):
    return float(re.search(pattern, text).group(1).replace(',', ''))


def project(tag, seed):
    return ROOT / (f'board-{tag}-out/combined/hps/project' if seed == 1 else f'board-{tag}-s{seed}/project')


def fit(path):
    out = path / 'output_files'
    place = (out / 'golden_top.fit.place.rpt').read_text(errors='replace')
    summary = (out / 'golden_top.fit.summary').read_text(errors='replace')
    sta = (out / 'golden_top.sta.summary').read_text(errors='replace')
    worst, negative = {}, []
    for kind, clock, slack in re.findall(r"Type\s*:\s*([A-Za-z ]+?) '([^']*)'\nSlack\s*:\s*(-?[\d.]+)", sta):
        if kind not in KINDS:
            continue
        slack = float(slack)
        worst[kind] = min(worst.get(kind, slack), slack)
        if slack < 0:
            negative.append([kind, clock, slack])
    return {
        'seed': int(re.search(r'set_global_assignment -name SEED (\d+)', (path / 'golden_top.qsf').read_text()).group(1)),
        'alms_needed': num(place, r'; ALMs needed \[=A-B\+C\]\s*;\s*([\d,.]+)'),
        'memory_alms': num(place, r'\[d\] ALMs used for memory \(up to half of total ALMs\)\s*;\s*([\d,]+)'),
        'mlab_bits': num(place, r'; Total MLAB memory bits\s*;\s*([\d,]+)'),
        'm20k': int(num(place, r'; M20K blocks\s*;\s*([\d,]+)')),
        'dsp_blocks': int(re.search(r'Total DSP Blocks : (\d+)', summary).group(1)),
        'worst_slack_ns': worst, 'all_kinds': sorted(worst) == sorted(KINDS), 'negative': negative,
    }


result = {'base': BASE, 'arms': {}}
for tag in [BASE] + ARMS:
    seeds = []
    for seed in (1, 2, 3, 4):
        try:
            seeds.append(fit(project(tag, seed)))
        except (OSError, AttributeError) as error:
            seeds.append({'seed': seed, 'missing': repr(error)[:200]})
    alms = [s['alms_needed'] for s in seeds if 'alms_needed' in s]
    result['arms'][tag] = {'seeds': seeds, 'complete': len(alms) == 4,
                           'median': statistics.median(alms) if alms else None,
                           'spread': max(alms) - min(alms) if alms else None,
                           'closed': all('negative' in s and not s['negative'] and s['all_kinds'] for s in seeds)}
base = result['arms'][BASE]
for tag in ARMS:
    arm = result['arms'][tag]
    if base['complete'] and arm['complete']:
        saving = base['median'] - arm['median']
        spread = max(base['spread'], arm['spread'])
        arm['saving'], arm['larger_spread'], arm['measured'] = saving, spread, saving > spread
json.dump(result, sys.stdout, indent=1)
print()
