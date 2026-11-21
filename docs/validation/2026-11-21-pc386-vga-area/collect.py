#!/usr/bin/env python3
"""pc386 VGA area (method.md): collect the DE25 board pair.

Per seed: whole-design ALMs needed, M20K, MLAB memory ALMs, DSP, the machine
netlist's SHA-256, and the worst slack per timing analysis over every clock and
corner, from each project's golden_top.fit.rpt and golden_top.sta.summary.
Adapted from #2047's area386_collect.py (same report parsing).

    collect.py ROOT > pair.json     # ROOT holds board-{base,arm}-out and board-{base,arm}-s{2,3,4}
"""
import hashlib
import json
import re
import statistics
import sys
from pathlib import Path


ANALYSES = ('Setup', 'Hold', 'Recovery', 'Removal', 'Minimum Pulse Width')


def entity(rpt):
    lines = rpt.read_text().split('\n')
    i = [k for k, l in enumerate(lines) if l.startswith('; Fitter Resource Utilization by Entity')][0]
    header = [c.strip() for c in lines[i + 2].split(';')]
    col = {h: n for n, h in enumerate(header)}
    out, path = {}, []
    for line in lines[i + 4:]:
        if line.startswith('+'):
            break
        cells = line.split(';')
        name = cells[1]
        depth = (len(name) - len(name.lstrip())) // 3
        path = path[:depth] + [name.strip().strip('|')]
        value = lambda h: float(cells[col[h]].split()[0])
        out['|'.join(path)] = {'alm': value('ALMs needed [=A-B+C]'), 'm20k': value('M20Ks'),
                               'dsp': value('DSP Blocks needed [=A-B]'), 'mem_alms': value('ALMs used for memory'),
                               'regs': value('Dedicated Logic Registers')}
    return out


def sta(summary):
    # The five analyses the method gates on, by exact type: "Setup Data Delay" and
    # "Net Delay" are other checks and must not be read as setup.
    worst = {}
    for m in re.finditer(r"Type  : ([A-Za-z ]+?) '([^']*)'\nSlack : (-?[\d.]+)", summary.read_text()):
        if m.group(1) in ANALYSES:
            worst[m.group(1)] = min(worst.get(m.group(1), 1e9), float(m.group(3)))
    return worst


def board(project):
    out = project / 'output_files'
    rows = entity(out / 'golden_top.fit.rpt')
    top = rows[[k for k in rows if k.count('|') == 0][0]]
    return {'alms': top['alm'], 'm20k': top['m20k'], 'dsp': top['dsp'], 'mem_alms': top['mem_alms'],
            'regs': top['regs'],
            'netlist_sha256': hashlib.sha256((project / 'fes_mailbox/pc386_system.v').read_bytes()).hexdigest(),
            'ram_blocks': int(re.search(r'Total RAM Blocks : (\d+)', (out / 'golden_top.fit.summary').read_text()).group(1)),
            'timing': sta(out / 'golden_top.sta.summary')}


def main():
    root = Path(sys.argv[1])
    result = {}
    for arm in ('base', 'arm'):
        seeds = {}
        for seed in (1, 2, 3, 4):
            project = root / (f'board-{arm}-out/combined/hps/project' if seed == 1 else f'board-{arm}-s{seed}/project')
            if (project / 'output_files/golden_top.sta.summary').exists():
                seeds[f's{seed}'] = board(project)
        alms = [s['alms'] for s in seeds.values()]
        result[arm] = {'seeds': seeds}
        if alms:
            result[arm]['median_alms'] = statistics.median(alms)
            result[arm]['spread_alms'] = max(alms) - min(alms)
    if all('median_alms' in result[a] for a in ('base', 'arm')):
        saving = result['base']['median_alms'] - result['arm']['median_alms']
        larger = max(result['base']['spread_alms'], result['arm']['spread_alms'])
        result['saving_alms'] = saving
        result['measured'] = saving > larger
    json.dump(result, sys.stdout, indent=1, sort_keys=True)
    print()


if __name__ == '__main__':
    main()
