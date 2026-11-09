#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Summarize machine_tb --census files (cores/fes-pc386/sim/census.h) into one
JSON row per run, for the i386 Phase 5 records.

Each run is TITLE=DIR, where DIR holds the census.txt and report.json that
`sim_fes_pc386.py --suite game --instruction-census` writes
(build/sim/fes-pc386/game-<id>/). The rows hold counts only: no instruction
bytes, frames or game data. Keys are sorted, so two records diff line by line.

    python3 scripts/pc386_census_summary.py --out census.jsonl \\
        jumpbump=build/sim/fes-pc386/game-jumpbump descent=...
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from pathlib import Path

# MOV to and from the system registers, by the census's sysreg stems.
SYSREG = {'0F20': ('cr', 'read'), '0F22': ('cr', 'write'), '0F21': ('dr', 'read'), '0F23': ('dr', 'write'),
          '0F24': ('tr', 'read'), '0F26': ('tr', 'write')}
STEM = re.compile(r'^(?:67)?(?:66)?(0F2[0-6])\.(\d)(?::|$)')


def load(path: Path) -> dict[str, int]:
    """census.txt's counts by key."""
    counts = {}
    for line in path.read_text().splitlines():
        parts = line.rsplit(' ', 2)
        if len(parts) == 3:
            counts[parts[0]] = int(parts[1])
    return counts


def per_100(walks: int, accesses: int) -> float | None:
    return round(100 * walks / accesses, 3) if accesses else None


def summarize(counts: dict[str, int]) -> dict:
    def total(pattern: str) -> int:
        return sum(n for k, n in counts.items() if re.search(pattern, k))

    def keyed(prefix: str) -> dict[str, int]:
        return {k[len(prefix):]: n for k, n in sorted(counts.items()) if k.startswith(prefix)}

    sysreg: dict[str, Counter] = {}
    for key, n in counts.items():
        if m := STEM.match(key):
            kind, direction = SYSREG[m.group(1)]
            sysreg.setdefault(f'{kind}{m.group(2)}', Counter())[direction] += n
    sources: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('ts:'):
            source = key.split(':')[1]
            if source in ('exception', 'intr', 'nmi', 'int'):
                source += ':' + key.split(':')[2]
            sources[source] += n
    entries: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('v86:enter:'):
            entries[key.split(':')[2]] += n
    pf: Counter = Counter()
    for key, n in counts.items():
        if key.startswith('event:exception:14:') and (m := re.search(r':err=([0-9a-f]+)', key)):
            pf[m.group(1)] += n
    tlb = {}
    for name in sorted({k.split(':')[1] for k in counts if k.startswith('tlb:')}):
        accesses, walks = counts.get(f'tlb:{name}:accesses', 0), counts.get(f'tlb:{name}:walks', 0)
        tlb[name] = {'accesses': accesses, 'walks': walks, 'walks_per_100': per_100(walks, accesses)}
    return {
        'paged_steps': counts.get('paging:steps', 0),
        'page_cross': counts.get('paging:page-cross', 0),
        'pg_toggles': total(r'^cr0\.pg:'),
        'sysreg': {k: dict(sorted(v.items())) for k, v in sorted(sysreg.items())},
        'clts': total(r'^(?:67)?(?:66)?0F06(:|$)'),
        'lmsw': total(r'^(?:67)?(?:66)?0F01\.6(:|$)'),
        'invlpg': total(r'^(?:67)?(?:66)?0F01\.7(:|$)'),
        'dr7': keyed('dr7:'),
        'db': total(r'^event:exception:1:'),
        'nm': total(r'^event:exception:7:'),
        'pf_by_error': dict(sorted(pf.items())),
        'task_switches': dict(sorted(sources.items())),
        'task_switch_keys': keyed('ts:'),
        'tss_iomap': keyed('tss-iomap:'),
        'ltr': total(r'^tr:.*:(?:67)?(?:66)?0F00\.3:'),
        'v86_steps': keyed('v86:steps:'),
        'v86_entries': dict(sorted(entries.items())),
        'v86_sensitive': keyed('v86:sens:'),
        'v86_events': {k: n for k, n in sorted(counts.items()) if k.startswith('event:') and ':v86:' in k},
        'io_bitmap': keyed('io-bitmap:'),
        'pm3_steps': total(r':pm3$'),
        'tlb': tlb,
    }


def run(title: str, directory: Path) -> dict:
    census = directory / 'census.txt'
    if not census.exists():
        return {'title': title, 'missing': True}
    row = {'title': title, **summarize(load(census))}
    report = directory / 'report.json'
    if report.exists():
        end = json.loads(report.read_text()).get('end') or {}
        row['end'], row['seconds'] = end.get('reason'), end.get('seconds')
    return row


def line(row: dict) -> str:
    if row.get('missing'):
        return f"{row['title']:14} MISSING"
    tlb = row['tlb']
    rate = lambda name: tlb.get(name, {}).get('walks_per_100')
    return (f"{row['title']:14} end={row.get('end')} paged={row['paged_steps']} "
            f"ts={sum(row['task_switches'].values())} v86={sum(row['v86_steps'].values())} "
            f"cr3w={row['sysreg'].get('cr3', {}).get('write', 0)} pf={sum(row['pf_by_error'].values())} "
            f"dtlb16={rate('dtlb16')} itlb8={rate('itlb8')}")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('runs', nargs='+', metavar='TITLE=DIR', help='a run: its title and its output directory')
    parser.add_argument('--out', type=Path, help='write the JSON rows here (default: stdout)')
    args = parser.parse_args(argv)
    rows = []
    for spec in args.runs:
        title, sep, directory = spec.partition('=')
        if not sep:
            parser.error(f'{spec}: expected TITLE=DIR')
        rows.append(run(title, Path(directory)))
    text = ''.join(json.dumps(r, sort_keys=True) + '\n' for r in rows)
    if args.out:
        args.out.write_text(text)
        for r in rows:
            print(line(r))
    else:
        sys.stdout.write(text)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
