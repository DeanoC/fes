#!/usr/bin/env python3
"""rung_summary.py NAME=PROJECT...: method.md's per-seed quantities, as JSON on stdout.

PROJECT is a fitted Quartus Pro project directory (golden_top.qsf, output_files/, and
paths/ from paths.tcl). For each:

- seed, alms (`ALMs needed [=A-B+C]`), system_alms (the `|system|` row, pc386_system),
  ram_blocks, dsp_blocks, netlist_sha256 (fes_mailbox/pc386_system.v);
- pll: the machine PLL's VCO, M, N and output counters as the fit report gives them;
- timing: every row of golden_top.sta.summary (analysis, clock or target, corner, slack);
- worst: the least slack of each analysis, with where;
- passes: method.md's pass rule (fits within 46,800 ALMs, every summary slack >= 0);
- fmax_machine: the Fmax Summary's machine-clock row (the analyser's estimate);
- paths: classify(paths/) (see below), when paths/ exists.

Path classification. Endpoints are owned by: cpu (system|cpu_*), machine (system|machine_*,
system|pixel_*, system|frame_*), anonymous (system|_N_ and any other system| name), shell
(outside system|). A path is `scaled` when launch and latch are the machine clock (its setup
relationship is the machine period), `fixed` when it crosses clocks with a relationship equal
to a max-delay bound of the SDC (10.000 ns), and `edge` otherwise (a cross-clock relationship
from the two clocks' edges).

Resolved owners (added after the method, labelled as such in the record): an anonymous register
`_N_` is often a register Yosys merged, followed in the netlist by `assign {names} = _N_;`.
resolve() maps it to cpu when every name it drives starts with cpu_, to machine when every name
starts with machine_/pixel_/frame_, and leaves it anonymous otherwise. `resolved_pairs` is the
machine_pairs count with those owners.
"""
import collections
import hashlib
import json
import re
import sys
from pathlib import Path

MACHINE = 'fes_machine_pll|machine_pll_outclk0'
PREFIX = 'soc_inst|subsys_periph|fes_scanout|fes_scanout|'
FIXED_BOUNDS = {10.0}
TARGET_2X_NS = 7.979  # C0 = 25, the fast clock of the 62.667 / 125.333 MHz pair


def owner(node):
    if PREFIX + 'system|' not in node:
        return 'shell'
    name = node.split(PREFIX + 'system|', 1)[1]
    if name.startswith('cpu_'):
        return 'cpu'
    if name.startswith(('machine_', 'pixel_', 'frame_')):
        return 'machine'
    return 'anonymous'


def resolver(netlist):
    """_N_ -> 'cpu' | 'machine' for anonymous registers whose assign names only that owner."""
    text = netlist.read_text(errors='replace')
    owners = {}
    for m in re.finditer(r'^\s*assign \{?([^=;]*?)\}? = (_\d+_);', text, re.M):
        names = [n.strip().split('[')[0] for n in m.group(1).split(',') if n.strip()]
        kinds = {'cpu' if n.startswith('cpu_') else 'machine' if n.startswith(('machine_', 'pixel_', 'frame_'))
                 else 'other' for n in names}
        if len(kinds) == 1 and 'other' not in kinds:
            owners[m.group(2)] = kinds.pop()
    return owners


def resolved(node, owners):
    o = owner(node)
    if o != 'anonymous':
        return o
    name = node.split(PREFIX + 'system|', 1)[1]
    base = re.match(r'(_\d+_)', name)
    return owners.get(base.group(1), 'anonymous') if base else 'anonymous'


def rows(path):
    """report_timing -detail summary rows: slack, from, to, launch, latch, relationship, skew, delay."""
    out = []
    for line in path.read_text(errors='replace').splitlines():
        cells = [c.strip() for c in line.split(';')]
        if len(cells) == 10 and re.fullmatch(r'-?\d+\.\d+', cells[1]):
            out.append({'slack': float(cells[1]), 'from': cells[2], 'to': cells[3], 'launch': cells[4],
                        'latch': cells[5], 'relationship': float(cells[6]), 'skew': float(cells[7]),
                        'delay': float(cells[8])})
    return out


def kind(row):
    if row['launch'] == row['latch'] == MACHINE:
        return 'scaled'
    if row['launch'] != row['latch'] and row['relationship'] in FIXED_BOUNDS:
        return 'fixed'
    return 'edge'


def short(row):
    return {'slack': row['slack'], 'from': row['from'].replace(PREFIX, ''), 'to': row['to'].replace(PREFIX, ''),
            'launch': row['launch'], 'latch': row['latch'], 'relationship': row['relationship'],
            'skew': row['skew'], 'delay': row['delay'], 'kind': kind(row),
            'owners': owner(row['from']) + '->' + owner(row['to'])}  # unresolved


def classify(paths, owners):
    result = {}
    for report in sorted(paths.glob('machine-setup-*.rpt')):
        corner = report.stem[len('machine-setup-'):]
        machine = rows(report)
        pairs = collections.defaultdict(lambda: {'count': 0, 'negative': 0, 'worst': None})
        for r in machine:
            p = pairs[owner(r['from']) + '->' + owner(r['to'])]
            p['count'] += 1
            p['negative'] += r['slack'] < 0
            p['worst'] = r['slack'] if p['worst'] is None else min(p['worst'], r['slack'])
        rpairs = collections.Counter()
        rworst = {}
        for r in machine:
            k = resolved(r['from'], owners) + '->' + resolved(r['to'], owners)
            rpairs[k] += 1
            rworst[k] = min(rworst.get(k, r['slack']), r['slack'])
        # The CPU boundary at 2x: a machine-clock path with exactly one end in the CPU, its slack
        # with the relationship cut from the machine period to TARGET_2X_NS (all else equal).
        # Owners as resolved (resolver()), so a merged CPU register counts as the CPU's.
        cpu = lambda n: resolved(n, owners) == 'cpu'
        boundary = [r for r in machine if cpu(r['from']) != cpu(r['to'])]
        at_2x = sorted(r['slack'] - (r['relationship'] - TARGET_2X_NS) for r in boundary)
        cpu_internal = sorted(r['slack'] - (r['relationship'] - TARGET_2X_NS)
                              for r in machine if cpu(r['from']) and cpu(r['to']))
        cross = []
        for name in ('cross-in-setup', 'cross-out-setup'):
            f = paths / f'{name}-{corner}.rpt'
            if f.exists():
                cross += rows(f)
        by_kind = collections.defaultdict(list)
        for r in cross:
            by_kind[kind(r)].append(r['slack'])
        negative = {}
        for check in ('setup', 'hold', 'recovery', 'removal'):
            f = paths / f'negative-{check}-{corner}.rpt'
            neg = rows(f) if f.exists() else []
            groups = collections.Counter(f"{kind(r)} {r['launch']} -> {r['latch']} {short(r)['owners']}" for r in neg)
            rgroups = collections.Counter(f"{resolved(r['from'], owners)}->{resolved(r['to'], owners)}" for r in neg)
            negative[check] = {'count': len(neg), 'groups': dict(groups.most_common()),
                               'resolved_owner_pairs': dict(rgroups.most_common()),
                               'worst': [short(r) for r in sorted(neg, key=lambda r: r['slack'])[:10]]}
        result[corner] = {
            'machine_paths_reported': len(machine),
            'machine_worst': [short(r) for r in machine[:15]],
            'machine_pairs': dict(sorted(pairs.items())),
            'resolved_pairs': {k: {'count': rpairs[k], 'worst': rworst[k]} for k in sorted(rpairs)},
            'cpu_boundary_endpoints': len(boundary),
            'cpu_boundary_at_2x': {'worst': at_2x[:1], 'negative': sum(s < 0 for s in at_2x),
                                   'worst_paths': [short(r) for r in sorted(
                                       boundary, key=lambda r: r['slack'] - r['relationship'])[:10]]},
            'cpu_internal_at_2x': {'worst': cpu_internal[:1], 'negative': sum(s < 0 for s in cpu_internal)},
            'cross_clock': {k: {'count': len(v), 'worst': min(v)} for k, v in sorted(by_kind.items())},
            'cross_edge_worst': [short(r) for r in sorted((r for r in cross if kind(r) == 'edge'),
                                                          key=lambda r: r['slack'])[:5]],
            'negative': negative,
        }
    return result


def pll(fit):
    block = fit.split('; fes_machine_pll|machine_pll|tennm_ph2_iopll')[-1]
    get = lambda label: re.findall(rf'-- {label}\s*;\s*([^;]+?)\s*;', block)
    return {'vco': get('IOPLL VCO Frequency')[0], 'm': get('M Counter')[0], 'n': get('N Counter')[0],
            'outputs': list(zip(get('Output Clock Frequency')[:2], get('C Counter')[:2]))}


def summary(project):
    out = project / 'output_files'
    fit = (out / 'golden_top.fit.rpt').read_text(errors='replace')
    alms = float(re.search(r'; ALMs needed \[=A-B\+C\]\s*;\s*([\d.,]+) / 46800', fit).group(1).replace(',', ''))
    system_alms = float(re.search(r'^;\s+\|system\|\s+;\s*([\d.]+) \(', fit, re.M).group(1))
    fsum = (out / 'golden_top.fit.summary').read_text()
    text = (out / 'golden_top.sta.summary').read_text()
    found = re.findall(r"Type\s*: (.+?) '(.+?)'\nSlack : (-?[\d.]+)(?:\nTNS\s*: -?[\d.]+)?(?:\nCorner: (.+))?", text)
    if not found or len(found) != text.count('\nType'):
        raise ValueError(f'{project}: {len(found)} timing summary rows parsed of {text.count(chr(10) + "Type")}')
    timing, worst = [], {}
    for analysis, clock, slack, corner in found:
        analysis, corner = analysis.strip(), (corner or '').strip()
        if analysis.endswith(' Max Skew'):
            corner, analysis = analysis[:-len(' Max Skew')], 'Max Skew'
        row = {'analysis': analysis, 'clock': clock, 'corner': corner, 'slack': float(slack)}
        timing.append(row)
        if analysis not in worst or row['slack'] < worst[analysis]['slack']:
            worst[analysis] = row
    sta = (out / 'golden_top.sta.rpt').read_text(errors='replace')
    fmax = re.search(r'^; ([\d.]+ MHz)\s*; ([\d.]+ MHz)\s*; ' + re.escape(MACHINE) + r'\s*;', sta, re.M)
    result = {
        'seed': int(re.search(r'set_global_assignment -name SEED (\d+)', (project / 'golden_top.qsf').read_text()).group(1)),
        'alms': alms, 'system_alms': system_alms,
        'ram_blocks': int(re.search(r'Total RAM Blocks : (\d+)', fsum).group(1)),
        'dsp_blocks': int(re.search(r'Total DSP Blocks : (\d+)', fsum).group(1)),
        'netlist_sha256': hashlib.sha256((project / 'fes_mailbox/pc386_system.v').read_bytes()).hexdigest(),
        'pll': pll(fit), 'fmax_machine': fmax.groups() if fmax else None,
        'timing': timing, 'worst': worst,
        'passes': alms <= 46800 and all(r['slack'] >= 0 for r in timing),
    }
    if (project / 'paths').is_dir():
        result['paths'] = classify(project / 'paths', resolver(project / 'fes_mailbox/pc386_system.v'))
    return result


if __name__ == '__main__':
    print(json.dumps({name: summary(Path(path)) for name, path in (a.split('=', 1) for a in sys.argv[1:])},
                     indent=1, sort_keys=True))
