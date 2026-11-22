#!/usr/bin/env python3
"""Collect the DE25 board pair (method.md): per arm and seed, whole-design ALMs needed, RAM and
DSP blocks, the worst slack of each analysis type over all clocks, the machine clock's setup
slack and any negative slack; then each arm's median and spread and the saving.

  pair_collect.py DIR > collect.json

DIR holds board-<arm>-out/combined/hps/project (seed 1) and board-<arm>-s<N>/project (seeds 2-4)
for the arms base and cut, as ~/fes-scratch/audio-area/board-pair.sh writes them.
"""
import json, re, statistics, sys
from pathlib import Path

ROOT = Path(sys.argv[1])
MACHINE = 'fes_machine_pll|machine_pll_outclk0'


def project(arm: str, seed: int) -> Path:
    if seed == 1:
        return ROOT / f'board-{arm}-out/combined/hps/project'
    return ROOT / f'board-{arm}-s{seed}/project'


def fit(path: Path) -> dict:
    out = path / 'output_files'
    place = (out / 'golden_top.fit.place.rpt').read_text(errors='replace')
    summary = (out / 'golden_top.fit.summary').read_text(errors='replace')
    alms = float(re.search(r'; ALMs needed \[=A-B\+C\]\s*;\s*([\d,.]+)', place).group(1).replace(',', ''))
    ram = int(re.search(r'Total RAM Blocks : (\d+)', summary).group(1))
    dsp = int(re.search(r'Total DSP Blocks : (\d+)', summary).group(1))
    seed = re.search(r'set_global_assignment -name SEED (\d+)', (path / 'golden_top.qsf').read_text()).group(1)
    worst, machine, negative = {}, None, []
    sta = (out / 'golden_top.sta.summary').read_text(errors='replace')
    for kind, clock, slack in re.findall(r"Type\s*:\s*([A-Za-z ]+?) '([^']*)'\nSlack\s*:\s*(-?[\d.]+)", sta):
        slack = float(slack)
        worst[kind] = min(worst.get(kind, slack), slack)
        if kind == 'Setup' and clock == MACHINE:
            machine = slack if machine is None else min(machine, slack)
        if slack < 0:
            negative.append([kind, clock, slack])
    return {'seed': int(seed), 'alms_needed': alms, 'ram_blocks': ram, 'dsp_blocks': dsp,
            'worst_slack_ns': worst, 'machine_setup_ns': machine, 'negative': negative}


result = {}
for arm in ('base', 'cut'):
    seeds = []
    for seed in (1, 2, 3, 4):
        try:
            seeds.append(fit(project(arm, seed)))
        except (OSError, AttributeError) as error:
            seeds.append({'seed': seed, 'missing': repr(error)[:200]})
    alms = [s['alms_needed'] for s in seeds if 'alms_needed' in s]
    result[arm] = {'seeds': seeds, 'median': statistics.median(alms) if alms else None,
                   'spread': max(alms) - min(alms) if alms else None, 'complete': len(alms) == 4}
if result['base']['complete'] and result['cut']['complete']:
    saving = result['base']['median'] - result['cut']['median']
    spread = max(result['base']['spread'], result['cut']['spread'])
    result['saving'] = saving
    result['measured'] = saving > spread
    result['band_pass'] = result['cut']['median'] <= result['base']['median']
    result['timing_pass'] = all(not s['negative'] for s in result['cut']['seeds'])
print(json.dumps(result, indent=1))
