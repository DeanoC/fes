#!/usr/bin/env python3
"""Read Quartus Pro's "Fitter Resource Utilization by Entity" table (method.md, 6.0b).

  python3 entities.py <fit.place.rpt> <out.tsv> [--depth N]

Writes one row per hierarchy node down to --depth (default 3): depth, path,
ALMs needed (with the node's own figure), registers, M20K and DSP, each as
Quartus prints it. Nothing is estimated or apportioned here.
"""
import argparse
import re
import sys
from pathlib import Path

TITLE = '; Fitter Resource Utilization by Entity'
PAIR = re.compile(r'^\s*([0-9.]+)\s*\(([0-9.]+)\)\s*$')


def pair(cell: str) -> tuple[float, float]:
    match = PAIR.match(cell)
    if not match:
        raise ValueError(f'not "total (own)": {cell!r}')
    return float(match.group(1)), float(match.group(2))


def rows(text: str):
    lines = text.splitlines()
    start = next(i for i, line in enumerate(lines) if line.startswith(TITLE))
    header = [c.strip() for c in lines[start + 2].split(';')[1:-1]]
    want = {'ALMs needed [=A-B+C]': 'alms', 'Dedicated Logic Registers': 'registers',
            'M20Ks': 'm20k', 'DSP Blocks needed [=A-B]': 'dsp', 'Full Hierarchy Name': 'full'}
    index = {want[h]: i for i, h in enumerate(header) if h in want}
    if len(index) != len(want):
        raise ValueError(f'columns missing: {sorted(set(want) - set(header))}')
    stack: list[str] = []
    for line in lines[start + 4:]:
        if line.startswith('+'):
            break
        cells = line.split(';')[1:-1]
        name_cell = cells[0]
        depth = (len(name_cell) - len(name_cell.lstrip(' '))) // 3
        name = name_cell.strip().strip('|') or '<top>'
        del stack[depth:]
        stack.append(name)
        yield depth, '/'.join(stack), {k: cells[i].strip() for k, i in index.items()}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('report', type=Path)
    parser.add_argument('out', type=Path)
    parser.add_argument('--depth', type=int, default=3)
    args = parser.parse_args()
    out = ['depth\tpath\talms_total\talms_own\tregisters_total\tregisters_own\tm20k\tdsp']
    for depth, path, cell in rows(args.report.read_text(errors='replace')):
        if depth > args.depth:
            continue
        alms, own = pair(cell['alms'])
        regs, regs_own = pair(cell['registers'])
        out.append(f'{depth}\t{path}\t{alms:.1f}\t{own:.1f}\t{regs:.0f}\t{regs_own:.0f}\t{cell["m20k"]}\t{cell["dsp"]}')
    args.out.write_text('\n'.join(out) + '\n')
    return 0


if __name__ == '__main__':
    sys.exit(main())
