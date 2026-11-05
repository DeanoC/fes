#!/usr/bin/env python3
"""Report a window of machine_tb --cpu-profile: where fes_i386_cpu's clocks went.

machine_tb --cpu rtl --cpu-profile F appends a snapshot of its counters to F
at the handover (RTL-BEGIN), every --cpu-profile-every seconds and at the
end (cores/fes-pc386/sim/cpu_profile.h). A window is the difference between
the snapshot at or before FROM and the one at or after TO (simulated
seconds; the defaults are the first and last). It prints the window's
clocks by cause, its instruction classes and its hottest sites; --json
writes the same numbers.

Causes (each system clock the RTL ran is in exactly one):
  retired uops          issued uops that WB committed
  redirect              taken-branch refill (with any L1I miss on the way)
                        and the wrong-path uops a redirect flushed; the
                        class table splits the redirects by branch class
                        ("next": the clocks from a taken instruction to its
                        target's retirement)
  fetch and L1I         front end empty, an L1I miss or not
  L1D and port 0        a load frozen on an L1D miss to port 0
  microcode             the microcode ROM read, the temp register and string
                        set-up waits of a sequence
  interlock             AGI (a load's or a late result into an address; a
                        data load never waits, as it is at M1-M2 before EX),
                        store-to-load, flags after a shift, ESP and segment
                        writes
  multiply and divide   frozen behind the multiplier or divider
  I/O and VGA           I/O in WB and parked on its answer, INTA, I/O
                        serialisation, MMIO (VGA) loads and a store buffer
                        full behind them
  halt                  HLT, halted or parked asleep
  other                 D stop, the governor, replay serialisation, a bench
                        reset at a stop, anything else

  python3 scripts/pc386_cpu_profile.py cpu.prof
  python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.7 --to 32.3 --json window.json
"""
from __future__ import annotations

import argparse
from collections import defaultdict
import json
from pathlib import Path
import sys

CAUSES = {
    'retired uops': [],  # issued less wrong-path, computed
    'redirect: refill': ['refill', 'refill_l1i'],
    'redirect: wrong-path uops': [],  # computed
    'fetch and L1I': ['fe_l1i', 'fe_empty'],
    'L1D and port 0': ['frz_l1d'],
    'microcode': ['urom', 'temp', 'str'],
    'interlock: AGI': ['agi'],
    'interlock: store-to-load': ['st_ld'],
    'interlock: flags': ['flag'],
    'interlock: ESP': ['sp'],
    'interlock: segment': ['seg'],
    'multiply and divide': ['frz_md'],
    'I/O and VGA: I/O': ['frz_io', 'parked_io', 'frz_inta', 'io_serial'],
    'I/O and VGA: MMIO load': ['frz_mmio'],
    'I/O and VGA: store buffer full': ['frz_store'],
    'halt': ['halt', 'parked_halt'],
    'other': ['d_stop', 'gov', 'replay_serial', 'other_wait', 'reset'],
}
ISSUE = ('issue_first', 'issue_more')
BRANCHES = ('jcc', 'loop', 'jcxz', 'jmp', 'call', 'ret', 'jmp_ind', 'call_ind', 'far_call', 'far_jmp', 'retf',
            'int', 'iret', 'delivery')


class ProfileError(ValueError):
    pass


def load(lines) -> dict[float, dict]:
    """The snapshots by time: counters, buckets, classes and that interval's sites."""
    snaps: dict[float, dict] = defaultdict(lambda: {'t': None, 'b': None, 'c': {}, 's': {}})
    for line in lines:
        if not line.strip():
            continue
        kind, rest = line.split(' ', 1)
        f = rest.split()
        snap = snaps[float(f[0])]
        if kind == 'T':
            snap['t'] = {k: int(v) for k, v in (x.split('=') for x in f[1:])}
        elif kind == 'B':
            snap['b'] = {k: int(v) for k, v in (x.split('=') for x in f[1:])}
        elif kind == 'C':
            snap['c'][f[1]] = {k: int(v) for k, v in (x.split('=') for x in f[2:])}
        elif kind == 'S':
            kv = dict(x.split('=') for x in f[3:])
            snap['s'][f[1]] = [int(kv['n']), int(kv['cycles']), f[2]]
        else:
            raise ProfileError(f'unknown line kind {kind!r}')
    for at, snap in snaps.items():
        if snap['t'] is None or snap['b'] is None:
            raise ProfileError(f'snapshot {at} is incomplete')
    return dict(snaps)


def window(snaps: dict[float, dict], lo: float | None = None, hi: float | None = None) -> dict:
    times = sorted(snaps)
    if len(times) < 2:
        raise ProfileError('a window needs two snapshots')
    a = times[0] if lo is None else max((x for x in times if x <= lo + 1e-9), default=None)
    z = times[-1] if hi is None else min((x for x in times if x >= hi - 1e-9), default=None)
    if a is None or z is None or z <= a:
        raise ProfileError(f'no window from {lo} to {hi} in snapshots {times[0]}..{times[-1]}')
    sub = lambda x, y: {k: y[k] - x.get(k, 0) for k in y}  # noqa: E731
    first, last = snaps[a], snaps[z]
    t, b = sub(first['t'], last['t']), sub(first['b'], last['b'])
    classes = {k: sub(first['c'].get(k, {}), v) for k, v in last['c'].items()}
    sites: dict[str, list] = defaultdict(lambda: [0, 0, ''])
    for at in times:
        if a < at <= z:
            for addr, (n, cycles, cls) in snaps[at]['s'].items():
                sites[addr][0] += n
                sites[addr][1] += cycles
                sites[addr][2] = cls
    clocks = sum(b.values())
    if clocks != t['sampled']:
        raise ProfileError(f'buckets hold {clocks} clocks, {t["sampled"]} sampled')
    issued = sum(b[k] for k in ISSUE)
    wrong = issued - t['commits']
    causes = {}
    for name, keys in CAUSES.items():
        causes[name] = sum(b[k] for k in keys)
    causes['retired uops'] = t['commits']
    causes['redirect: wrong-path uops'] = wrong
    taken = {k: v for k, v in classes.items() if k in BRANCHES and v.get('taken')}
    return {'from': a, 'to': z, 'from_cycle': first['t']['cycles'], 'cycles': t['cycles'], 'rtl_clocks': clocks, 'retired': t['retired'],
            'pm_retired': t['pm_retired'], 'commits': t['commits'], 'issued': issued, 'wrong_path_uops': wrong,
            'redirects': t['redirects'], 'frames': t['frames'], 'polls_3da': t['polls_3da'],
            'l1d_waits': t['l1d_waits'], 'l1i_waits': t['l1i_waits'], 'prefixes': t['prefixes'],
            'cpi': t['cycles'] / max(t['retired'], 1), 'causes': causes, 'buckets': b, 'classes': classes,
            'redirects_by_class': {k: {'taken': v['taken'], 'clocks': v['next'],
                                       'per_taken': round(v['next'] / v['taken'], 2)} for k, v in taken.items()},
            'sites': sorted(([k, *v] for k, v in sites.items()), key=lambda s: -s[2])[:25]}


def report(w: dict) -> str:
    out = [f'window {w["from"]}..{w["to"]}: {w["cycles"]:,} clocks ({w["rtl_clocks"]:,} on the RTL), '
           f'{w["retired"]:,} retired, CPI {w["cpi"]:.3f}, {w["frames"]} frames'
           + (f', {w["cycles"] / w["frames"]:,.0f} clocks a frame' if w['frames'] else ''),
           f'issued {w["issued"]:,} uops, committed {w["commits"]:,}, wrong path {w["wrong_path_uops"]:,}; '
           f'{w["redirects"]:,} redirects, {w["l1d_waits"]:,} L1D and {w["l1i_waits"]:,} L1I misses',
           f'{"cause":32s} {"clocks":>14s} {"%":>6s} {"per instr":>9s}']
    clocks = max(w['rtl_clocks'], 1)
    for name, v in w['causes'].items():
        out.append(f'{name:32s} {v:14,d} {100 * v / clocks:6.2f} {v / max(w["retired"], 1):9.4f}')
    out.append(f'\n{"redirects":10s} {"taken":>11s} {"clocks":>13s} {"per taken":>9s}')
    for k, v in sorted(w['redirects_by_class'].items(), key=lambda kv: -kv[1]['clocks']):
        out.append(f'{k:10s} {v["taken"]:11,d} {v["clocks"]:13,d} {v["per_taken"]:9.2f}')
    out.append(f'\n{"class":10s} {"n":>11s} {"taken":>11s} {"own":>13s} {"next":>13s}')
    for k, v in sorted(w['classes'].items(), key=lambda kv: -(kv[1]['own'] + kv[1]['next'])):
        if v['n']:
            out.append(f'{k:10s} {v["n"]:11,d} {v["taken"]:11,d} {v["own"]:13,d} {v["next"]:13,d}')
    out.append('\ntop sites')
    for addr, n, cycles, cls in w['sites']:
        out.append(f'  {addr} {cls:9s} n={n:,} cycles={cycles:,} ({100 * cycles / max(w["cycles"], 1):.2f}%) '
                   f'{cycles / max(n, 1):.2f} each')
    return '\n'.join(out)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('profile', type=Path)
    parser.add_argument('--from', dest='lo', type=float, help='window start, simulated seconds (default first)')
    parser.add_argument('--to', dest='hi', type=float, help='window end, simulated seconds (default last)')
    parser.add_argument('--json', type=Path, help='write the window here')
    args = parser.parse_args(argv)
    try:
        w = window(load(args.profile.read_text().splitlines()), args.lo, args.hi)
    except (OSError, ProfileError) as error:
        print('cpu profile:', error, file=sys.stderr)
        return 2
    print(report(w))
    if args.json:
        args.json.write_text(json.dumps(w, indent=1) + '\n')
    return 0


if __name__ == '__main__':
    sys.exit(main())
