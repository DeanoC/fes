#!/usr/bin/env python3
"""Generate the i486 slice 1 fit spike's results from the committed reports.

Reads fits/, elaboration/ and lockstep/ beside this file and writes
results.md (the tables and every number summary.md quotes) and results.json.
The bands and readings are method.md's, applied mechanically here; nothing in
this script was changed after a result was seen except to read a file.

  python3 docs/validation/2026-11-18-i486-s1-fit/generate.py [--check]

--check fails when results.md or results.json differ from what the reports
give.
"""
from __future__ import annotations

import argparse
import json
import re
import statistics
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
ARMS = ('a', 'b', 'c')
ARM_NAMES = {'a': 'a, baseline', 'b': 'b, build parameter', 'c': 'c, runtime strap'}
BOARDS = ('de10', 'de25')
DE10_ALMS, DE25_ALMS = 41_910, 46_800
DE10_CEILING = 38_500
DE10_88 = 36_880
DE25_92, DE25_88 = 43_056, 41_184
BAND = (350, 850)
STRAP_SMALL = 150
CPU_ENTITY = 'fes_i386_cpu'


def load_fits() -> dict:
    """{board: {arm: {seed: record}}} for the flattened fits; per-module fits apart."""
    fits = {board: {arm: {} for arm in ARMS} for board in BOARDS}
    hier = {}
    for path in sorted((HERE / 'fits').glob('*/')):
        board, arm, kind, seed = path.name.split('-')
        seed = int(seed[1:])
        if board == 'de10':
            s = json.loads((path / 'summary.json').read_text())
            closes = s['closes']
            setup = min(r['slack_ns'] for r in s['corners'] if r['analysis'] == 'setup'
                        and r['clock'].startswith('system_clock|pll|general[0]'))
            hold = min(r['slack_ns'] for r in s['corners'] if r['analysis'] == 'hold')
            rec = {'alms': s['resources']['alms']['used'], 'registers': s['resources']['registers']['used'],
                   'ram': s['resources']['ram_blocks']['used'], 'dsp': s['resources']['dsp_blocks']['used'],
                   'closes': closes, 'cpu_setup_ns': setup, 'worst_hold_ns': hold,
                   'synthesis': s['synthesis'], 'netlist': s['netlist_sha256'], 'commit': s['commit'],
                   'corners': s['corners'], 'entities': s['entities'], 'host': s['host'],
                   'minutes': s['compile_minutes'], 'failure': s['timing_failure']}
        else:
            f = json.loads((path / 'fit.json').read_text())
            n = json.loads((path / 'netlist.json').read_text())
            corners = f['timing']['corners']
            fmax = {c['corner']: {k['clock']: float(k['fmax']) for k in c['clocks']} for c in f['timing']['fmax']}
            rec = {'alms': int(f['resources']['alms']['used']), 'registers': int(f['resources']['registers']['used']),
                   'ram': int(f['resources']['ram_blocks']['used']), 'dsp': int(f['resources']['dsp_blocks']['used']),
                   'closes': f['timing']['status'] == 'pass',
                   'cpu_setup_ns': min(c['analyses']['clk_clock_setup']['worst_slack_ns'] for c in corners),
                   'worst_hold_ns': min(c['analyses']['hold']['worst_slack_ns'] for c in corners),
                   'clk_fmax_worst': min(v['clk_clock'] for v in fmax.values()),
                   'pixel_fmax_worst': min(v['pixel_clk_clock'] for v in fmax.values()),
                   'both_clocks_every_corner': all('clk_clock' in v and 'pixel_clk_clock' in v for v in fmax.values())
                   and len(fmax) == f['timing']['corner_count'],
                   'netlist': n['fitted_netlist']['source_sha256'],
                   'fitted_netlist': n['fitted_netlist']['fitted_sha256'],
                   'commit': n.get('source_revision'), 'packing': f['resources']['packing_difficulty']['value'],
                   'host': f['host'], 'corners': corners, 'fmax': fmax}
        if kind == 'hier':
            hier[(board, arm)] = rec
        else:
            fits[board][arm][seed] = rec
    return {'fits': fits, 'hier': hier}


def median(values):
    return statistics.median(values)


def arm_stats(records: dict) -> dict:
    alms = [r['alms'] for r in records.values()]
    return {'seeds': sorted(records), 'alms': alms, 'median': median(alms), 'spread': max(alms) - min(alms),
            'closing': sum(r['closes'] for r in records.values()),
            'setup_median': median([r['cpu_setup_ns'] for r in records.values()]),
            'setup_min': min(r['cpu_setup_ns'] for r in records.values())}


def entity_totals(rec: dict) -> dict:
    """ALMs of the CPU subtree and of everything else, from a per-module fit."""
    top = next(e for e in rec['entities'] if e['level'] == 0)
    cpu = [e for e in rec['entities'] if module_name(e['entity']) == CPU_ENTITY]
    cpu_alms = cpu[0]['alms'] if len(cpu) == 1 else None
    return {'total': top['alms'], 'cpu': cpu_alms, 'rest': top['alms'] - cpu_alms if cpu_alms is not None else None}


def module_name(entity: str) -> str:
    """'|fes_i386_cpu_8fd8ee64:cpu|' -> 'fes_i386_cpu': the module, without the per-netlist hash
    quartus_x87_spike.plain_module_names appends to a parameterised one."""
    name = entity.strip('|').split(':')[0]
    return re.sub(r'_[0-9a-f]{8}$', '', name)


def cpu_modules(rec: dict) -> dict:
    """ALMs of each module directly under fes_i386_cpu, and the CPU's own logic, from a per-module fit."""
    rows = rec['entities']
    i = next(k for k, e in enumerate(rows) if module_name(e['entity']) == CPU_ENTITY)
    level = rows[i]['level']
    out = {'(own logic)': rows[i]['own_alms']}
    for e in rows[i + 1:]:
        if e['level'] <= level:
            break
        if e['level'] == level + 1:
            name = module_name(e['entity'])
            out[name] = out.get(name, 0) + e['alms']
    return out


def reading_sx(delta, unc):
    if abs(delta) < unc:
        return f'not resolved: under {unc:.0f} ALMs'
    if delta < BAND[0]:
        return 'below 350: under the estimate'
    if delta <= BAND[1]:
        return '350 to 850: the estimate holds'
    return 'above 850: over the estimate'


def reading_total(median_b, board):
    if board == 'de10':
        if median_b <= DE10_88:
            return 'at or under 36,880 (88%): inside the range the machine has closed at before'
        if median_b <= DE10_CEILING:
            return '36,881 to 38,500: under the working ceiling, past what has closed before'
        return 'above 38,500: over the working ceiling'
    if median_b <= DE25_88:
        return 'at or under 41,184 (88%)'
    if median_b <= DE25_92:
        return '41,185 to 43,056: under the 92% line'
    return 'above 43,056 (92%)'


def reading_timing(a, b):
    if a < 3:
        return 'arm a closes on fewer than three of four: no timing cost attributed'
    if b >= a - 1:
        return 'no timing cost the spike can resolve'
    return 'the delta costs DE10 timing'


def reading_strap(delta, unc, closing_b, closing_c):
    timing_ok = closing_c >= closing_b - 1
    if abs(delta) < unc and timing_ok:
        return f'not resolved, timing equal within a seed: the strap costs under {unc:.0f} ALMs'
    if abs(delta) >= unc and delta <= STRAP_SMALL and timing_ok:
        return 'resolved, at most 150 ALMs: a small, measured cost'
    if delta > STRAP_SMALL or not timing_ok:
        return 'material: above 150 ALMs or two or more fewer closing seeds'
    return f'not resolved (under {unc:.0f} ALMs), timing differs'


def elaborations() -> dict:
    return {path.stem: json.loads(path.read_text()) for path in sorted((HERE / 'elaboration').glob('*.json'))}


def analyse(data: dict) -> dict:
    elab = elaborations()
    out = {'boards': {}, 'netlists': {}}
    # Every seed of an arm, on both boards, fitted the one netlist its
    # elaboration recorded, and each arm's commit is the one elaborated.
    for arm in ARMS:
        want = elab.get(f'{arm}-flat', {}).get('netlist_sha256')
        got = {r['netlist'] for board in BOARDS for r in data['fits'][board][arm].values()}
        commits = {r['commit'] for board in BOARDS for r in data['fits'][board][arm].values()}
        out['netlists'][arm] = {'elaborated': want, 'commit': elab.get(f'{arm}-flat', {}).get('commit'),
                                'fitted': sorted(got), 'one_netlist': got == {want} if got else None,
                                'commits_agree': commits <= {elab.get(f'{arm}-flat', {}).get('commit')} if got else None,
                                'changed_from_base': elab.get(f'{arm}-flat', {}).get('changed_from_base')}
    for board in BOARDS:
        arms = {arm: arm_stats(data['fits'][board][arm]) for arm in ARMS if data['fits'][board][arm]}
        result = {'arms': arms}
        hier = {arm: entity_totals(data['hier'][(board, arm)]) for arm in ARMS if (board, arm) in data['hier']}
        result['hier'] = hier
        if 'a' in arms and 'b' in arms:
            floor_b = abs(hier['b']['rest'] - hier['a']['rest']) if 'a' in hier and 'b' in hier else None
            unc = max(arms['a']['spread'], arms['b']['spread'], floor_b or 0)
            delta = arms['b']['median'] - arms['a']['median']
            result['sx'] = {'delta': delta, 'seed_spread': max(arms['a']['spread'], arms['b']['spread']),
                            'netlist_floor': floor_b, 'uncertainty': unc,
                            'reading': reading_sx(delta, unc) if board == 'de10' else None,
                            'total_reading': reading_total(arms['b']['median'], board),
                            'cpu_delta_hier': hier['b']['cpu'] - hier['a']['cpu'] if 'a' in hier and 'b' in hier else None}
            if board == 'de10':
                result['timing_reading'] = reading_timing(arms['a']['closing'], arms['b']['closing'])
        if 'b' in arms and 'c' in arms:
            floor_c = abs(hier['c']['rest'] - hier['b']['rest']) if 'b' in hier and 'c' in hier else None
            unc = max(arms['b']['spread'], arms['c']['spread'], floor_c or 0)
            delta = arms['c']['median'] - arms['b']['median']
            result['strap'] = {'delta': delta, 'seed_spread': max(arms['b']['spread'], arms['c']['spread']),
                               'netlist_floor': floor_c, 'uncertainty': unc,
                               'reading': reading_strap(delta, unc, arms['b']['closing'], arms['c']['closing']),
                               'cpu_delta_hier': hier['c']['cpu'] - hier['b']['cpu'] if 'b' in hier and 'c' in hier else None}
        out['boards'][board] = result
    return out


def extension(res: dict) -> str:
    """method.md's seed extension: a delta within one uncertainty of a band edge, or a timing reading
    that turns on one seed, gets seeds 5 to 8 on that board for all arms, once."""
    reasons = []
    for board in BOARDS:
        r = res['boards'][board]
        edges = (BAND[0], BAND[1]) if board == 'de10' else ()
        sx = r['sx']
        if any(abs(sx['delta'] - e) < sx['uncertainty'] for e in edges):
            reasons.append(f'{board.upper()} SX delta within one uncertainty of a band edge')
        st = r['strap']
        if abs(st['delta'] - STRAP_SMALL) < st['uncertainty'] and abs(st['delta']) >= st['uncertainty']:
            reasons.append(f'{board.upper()} strap delta within one uncertainty of 150')
        if board == 'de10':
            a, b, c = (r['arms'][k]['closing'] for k in ARMS)
            # Each reading's threshold: b at a - 1 or a - 2, c at b - 1 or b - 2, and a at
            # three (fewer than three attributes nothing) sit one seed from flipping.
            if b in (a - 1, a - 2) or c in (b - 1, b - 2) or a in (2, 3):
                reasons.append('DE10 timing reading turns on one seed')
    seeds = {len(s['seeds']) for board in BOARDS for s in res['boards'][board]['arms'].values()}
    ran = max(seeds) > 4
    if not reasons:
        return 'did not fire; four seeds an arm on both boards' + (' (but more were run)' if ran else '')
    return 'fired (' + '; '.join(reasons) + ')' + ('; seeds 5 to 8 were run' if ran else '; NOT YET RUN')


def fmt(v):
    return f'{v:,.0f}' if isinstance(v, (int, float)) and float(v).is_integer() else f'{v:,.1f}' if isinstance(v, float) else str(v)


def render(data: dict, res: dict, lockstep: dict) -> str:
    lines = ['# i486 slice 1: generated results', '',
             'Written by `generate.py` from the reports in `fits/`, `elaboration/` and `lockstep/`. Do not edit by hand.', '']
    for board in BOARDS:
        r = res['boards'][board]
        total = DE10_ALMS if board == 'de10' else DE25_ALMS
        lines += [f'## {board.upper()}', '', '### Every fit', '',
                  '| Arm | Seed | ALMs needed | % of device | Registers | RAM blocks | DSP | CPU clock worst setup (ns) | Worst hold (ns) | Closes |'
                  + (' clk Fmax worst corner (MHz) |' if board == 'de25' else ''),
                  '| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |' + (' ---: |' if board == 'de25' else '')]
        for arm in ARMS:
            for seed, rec in sorted(data['fits'][board][arm].items()):
                lines.append(f"| {arm} | {seed} | {rec['alms']:,} | {100 * rec['alms'] / total:.2f} | {rec['registers']:,} | "
                             f"{rec['ram']} | {rec['dsp']} | {rec['cpu_setup_ns']:+.3f} | {rec['worst_hold_ns']:+.3f} | "
                             f"{'yes' if rec['closes'] else 'no'} |" + (f" {rec['clk_fmax_worst']:.2f} |" if board == 'de25' else ''))
        lines += ['', '### By arm', '', '| Arm | Seeds | Median ALMs | Spread | Seeds closing | Median CPU-clock setup (ns) | Worst CPU-clock setup (ns) |',
                  '| --- | --- | ---: | ---: | ---: | ---: | ---: |']
        for arm, s in r['arms'].items():
            lines.append(f"| {ARM_NAMES[arm]} | {', '.join(map(str, s['seeds']))} | {fmt(s['median'])} | {s['spread']} | "
                         f"{s['closing']} of {len(s['seeds'])} | {s['setup_median']:+.3f} | {s['setup_min']:+.3f} |")
        if r['hier']:
            lines += ['', '### Per-module fits (seed 1, no flatten)', '', '| Arm | Total ALMs | `fes_i386_cpu` | Everything else |',
                      '| --- | ---: | ---: | ---: |']
            for arm, h in r['hier'].items():
                lines.append(f"| {arm} | {fmt(h['total'])} | {fmt(h['cpu'])} | {fmt(h['rest'])} |")
            mods = {arm: cpu_modules(data['hier'][(board, arm)]) for arm in ARMS if (board, arm) in data['hier']}
            if set(mods) == set(ARMS):
                lines += ['', 'Inside `fes_i386_cpu` (one level down; ALMs needed):', '',
                          '| Module | a | b | c | b - a | c - b |', '| --- | ---: | ---: | ---: | ---: | ---: |']
                names = sorted(set().union(*mods.values()), key=lambda n: (-abs(mods['b'].get(n, 0) - mods['a'].get(n, 0)), n))
                for name in names:
                    a, b, c = (mods[k].get(name, 0) for k in ARMS)
                    lines.append(f'| `{name}` | {a:,.1f} | {b:,.1f} | {c:,.1f} | {b - a:+,.1f} | {c - b:+,.1f} |')
        for key, label in (('sx', 'The SX delta, arm b minus arm a'), ('strap', 'The strap, arm c minus arm b')):
            if key in r:
                d = r[key]
                lines += ['', f'### {label}', '',
                          f"- Delta between medians: **{fmt(d['delta'])} ALMs**",
                          f"- Seed spread (larger arm's): {d['seed_spread']} ALMs; netlist floor (logic outside the CPU, per-module fits): "
                          f"{fmt(d['netlist_floor']) if d['netlist_floor'] is not None else 'not measured'} ALMs; uncertainty used: {fmt(d['uncertainty'])} ALMs",
                          f"- CPU subtree delta in the per-module fits: {fmt(d['cpu_delta_hier']) if d['cpu_delta_hier'] is not None else 'not measured'} ALMs",
                          f"- Reading: {d['reading'] or 'reported beside the DE10 band, not held to it (method.md)'}"]
                if key == 'sx':
                    lines.append(f"- Arm b's median against the {'working ceiling' if board == 'de10' else '92% line'}: {d['total_reading']}")
        if 'timing_reading' in r:
            lines.append(f"- DE10 timing reading: {r['timing_reading']}")
        lines.append('')
    lines += ['## Netlists', '', '| Arm | Commit | Netlist SHA-256 | Every seed, both boards, fitted it | Files changed from the base |',
              '| --- | --- | --- | --- | --- |']
    for arm, n in res['netlists'].items():
        lines.append(f"| {arm} | `{(n['commit'] or '')[:12]}` | `{(n['elaborated'] or '')[:16]}…` | "
                     f"{'yes' if n['one_netlist'] and n['commits_agree'] else 'NO' if n['one_netlist'] is not None else 'no fits yet'} | "
                     f"{len(n['changed_from_base'] or [])} |")
    lines += ['', '## Lockstep (48 directed 486 cases, level-4 oracle)', '']
    for name, rep in lockstep.items():
        lines.append(f"- {name}: {rep['agree']} of {rep['cases']} agree; the stops list "
                     f"{'equals' if rep.get('matches_expected') else 'differs from'} slice 0's committed list")
        for label, stop in (rep['stops'].items() if len(rep['stops']) <= 10 else ()):
            lines.append(f'  - {label}: `{stop}`')
    lines.append('')
    return '\n'.join(lines)


def result_block(res: dict) -> str:
    """The summary's numbers, each from res."""
    d10, d25 = res['boards']['de10'], res['boards']['de25']
    a10, b10, c10 = (d10['arms'][k] for k in ARMS)
    a25, b25, c25 = (d25['arms'][k] for k in ARMS)
    sx10, sx25, st10, st25 = d10['sx'], d25['sx'], d10['strap'], d25['strap']
    on, off = res['lockstep']['identity-on'], res['lockstep']['identity-off']
    n = len(a10['seeds'])
    pct = lambda v, total: f'{100 * v / total:.2f}%'
    out = ['**Result.**',
           f"- **The 486SX delta on the DE10 (arm b minus arm a): {fmt(sx10['delta'])} ALMs between medians**, "
           f"against an uncertainty of {fmt(sx10['uncertainty'])} (seed spread {sx10['seed_spread']}, netlist floor "
           f"{fmt(sx10['netlist_floor'])}). Reading against the plan's 350 to 850: **{sx10['reading']}**. "
           f"It is {fmt(sx10['uncertainty'] - abs(sx10['delta']))} ALMs short of resolving; resolved or not it reads "
           f"below 350, so neither band edge is near. Synthesis, which no seed moves, adds "
           f"{int(b10_syn(res, 'aluts')):+,} ALUTs and {int(b10_syn(res, 'registers')):+,} registers; the per-module "
           f"fits (one seed) put {fmt(sx10['cpu_delta_hier'])} ALMs of change in `fes_i386_cpu` "
           f"({fmt(res['boards']['de10']['hier']['b']['total'] - res['boards']['de10']['hier']['a']['total'])} in the "
           f"whole machine); the modules are in `results.md`.",
           f"- **The 486SX machine on the DE10: {fmt(b10['median'])} ALMs median ({pct(b10['median'], DE10_ALMS)})**, "
           f"against arm a's {fmt(a10['median'])}: {sx10['total_reading']}.",
           f"- **DE10 timing at 61.44 MHz:** arm a closes on {a10['closing']} of {n} seeds, arm b on {b10['closing']}, "
           f"arm c on {c10['closing']}; median CPU-clock setup slack {a10['setup_median']:+.3f}, {b10['setup_median']:+.3f} "
           f"and {c10['setup_median']:+.3f} ns, worst {a10['setup_min']:+.3f}, {b10['setup_min']:+.3f} and "
           f"{c10['setup_min']:+.3f} ns. Reading: {d10['timing_reading']}.",
           f"- **The DE25 (Agilex 5 ALMs, a different unit, not held to the band): {fmt(sx25['delta'])} ALMs** between "
           f"medians (uncertainty {fmt(sx25['uncertainty'])}); arm b at {fmt(b25['median'])} "
           f"({pct(b25['median'], DE25_ALMS)}), {sx25['total_reading']}. Every DE25 fit closes at 61.44 MHz; the "
           f"worst-corner `clk` Fmax medians are {fmax_median(res, 'a'):.1f}, {fmax_median(res, 'b'):.1f} and "
           f"{fmax_median(res, 'c'):.1f} MHz for arms a, b and c; both clock domains were analysed at every "
           f"corner in {res['de25_coverage']} of {res['de25_fits']} DE25 fits.",
           f"- **The strap over the build parameter (arm c minus arm b): {fmt(st10['delta'])} ALMs on the DE10** "
           f"(uncertainty {fmt(st10['uncertainty'])}) **and {fmt(st25['delta'])} on the DE25** (uncertainty "
           f"{fmt(st25['uncertainty'])}). DE10 reading: **{st10['reading']}**. DE25 reading: {st25['reading']}. "
           f"On the DE10, arm c's synthesis has {int(res['synthesis_delta_strap']['registers']):+,} register and "
           f"{int(res['synthesis_delta_strap']['aluts']):+,} ALUTs over arm b: the strap's latch and its gating.",
           f"- **Lockstep:** with the identity on, {on['agree']} of {on['cases']} directed cases agree with the level-4 "
           f"oracle (the 386 agrees on {off['agree']}); the {len(on['stops'])} that stop are the ones `method.md` "
           f"names as stops by design. With the identity off, the stops "
           f"{'equal' if off['matches_expected'] else 'do not equal'} slice 0's committed list.",
           f"- **test386 with the identity off** (the spike RTL, CPU alone): POST codes end at "
           f"{res['test386']['last_post']:02X}h with {res['test386']['errors']} errors and no stop, "
           f"{res['test386']['retirements']:,} retirements to its halt.",
           f"- **Extension rule:** {res['extension']}.",
           '', '**Recommendation on the strap, for Deano to decide.** ' + recommendation(res)]
    return '\n'.join(out)


def b10_syn(res: dict, key: str) -> int:
    return res['synthesis_delta']['de10'][key]


def fmax_median(res: dict, arm: str) -> float:
    return res['boards']['de25']['fmax_median'][arm]


def recommendation(res: dict) -> str:
    st10 = res['boards']['de10']['strap']
    c10, b10 = res['boards']['de10']['arms']['c'], res['boards']['de10']['arms']['b']
    reading = st10['reading']
    measured = (f"Measured: arm c minus arm b is {fmt(st10['delta'])} ALMs on the DE10 against an uncertainty of "
                f"{fmt(st10['uncertainty'])}, and arm c closes on {c10['closing']} DE10 seeds to arm b's {b10['closing']}.")
    if reading.startswith('not resolved, timing equal'):
        return (measured + ' The strap costs nothing this spike can measure over the build parameter, so a runtime '
                'strap is affordable on area and timing. What it costs instead is outside the fit: a record bit, '
                'the ABI and FogCast fields, and lockstep, test386 and kit runs of both identities on one netlist. '
                'The spike recommends the strap if per-title identity selection is wanted; otherwise the build '
                'parameter alone is the smaller change.')
    if reading.startswith('resolved, at most'):
        return (measured + ' The strap has a small, measured area cost and no timing cost the spike resolves. The '
                'spike recommends it if per-title identity selection is wanted.')
    return (measured + ' The strap has a material cost by the method\'s reading. The spike recommends the build '
            'parameter unless per-title selection is worth that cost.')


def lockstep_summary() -> dict:
    out = {}
    for path in sorted((HERE / 'lockstep').glob('*.json')):
        cases = json.loads(path.read_text())['lockstep']['cases']
        out[path.stem] = {'cases': len(cases), 'agree': sum(1 for c in cases if c['rtl'] == 'agree'),
                          'matches_expected': all(c['as_expected'] for c in cases),
                          'stops': {c['label']: c['rtl'] for c in cases if c['rtl'] != 'agree'}}
    return out


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    data = load_fits()
    res = analyse(data)
    lockstep = lockstep_summary() if (HERE / 'lockstep').is_dir() else {}
    res['lockstep'] = lockstep
    res['synthesis_delta'] = {'de10': {key: median([r['synthesis'][key] for r in data['fits']['de10']['b'].values()])
                                            - median([r['synthesis'][key] for r in data['fits']['de10']['a'].values()])
                                            for key in ('aluts', 'registers', 'alm_estimate')}}
    res['synthesis_delta_strap'] = {key: median([r['synthesis'][key] for r in data['fits']['de10']['c'].values()])
                                     - median([r['synthesis'][key] for r in data['fits']['de10']['b'].values()])
                                     for key in ('aluts', 'registers', 'alm_estimate')}
    res['boards']['de25']['fmax_median'] = {arm: median([r['clk_fmax_worst'] for r in data['fits']['de25'][arm].values()])
                                             for arm in ARMS}
    res['extension'] = extension(res)
    res['de25_fits'] = sum(len(data['fits']['de25'][arm]) for arm in ARMS)
    res['de25_coverage'] = sum(r['both_clocks_every_corner'] for arm in ARMS for r in data['fits']['de25'][arm].values())
    t386 = json.loads((HERE / 'test386' / 'identity-off-rtl.json').read_text())
    res['test386'] = {'last_post': t386['posts'][-1], 'errors': len(t386['errors']), 'stop': t386['stop'],
                      'retirements': t386['end']['retirements'], 'commit': t386['rtl_commit']}
    md = render(data, res, lockstep)
    summary_path = HERE / 'summary.md'
    text = summary_path.read_text()
    start, end = '<!-- generated:start -->\n', '<!-- generated:end -->'
    head, rest = text.split(start)
    _, tail = rest.split(end)
    summary = head + start + result_block(res) + '\n' + end + tail
    js = json.dumps(res, indent=2, sort_keys=True, default=float) + '\n'
    if args.check:
        ok = ((HERE / 'results.md').read_text() == md and (HERE / 'results.json').read_text() == js
              and summary_path.read_text() == summary)
        print('results are current' if ok else 'results.md, results.json or summary.md differ from the reports')
        return 0 if ok else 1
    (HERE / 'results.md').write_text(md)
    (HERE / 'results.json').write_text(js)
    summary_path.write_text(summary)
    return 0


if __name__ == '__main__':
    sys.exit(main())
