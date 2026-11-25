#!/usr/bin/env python3
"""rung_table.py REPO FITS_DIR: the record's rung and CPU-boundary tables, as markdown.

REPO is a checkout holding the rung branches (deano/de25-486sx-fmax-cNN). For each rung: its
commit, the SHA-256 of `git diff <method commit> <rung>` (what make_rung.py changed) and, from
FITS_DIR's seed-1 JSON, the netlist the fit compiled. The boundary table is each seed 1's
CPU-boundary projection (rung_summary.py: paths with exactly one resolved-CPU end, slack with the
relationship cut to 7.979 ns), Slow fix4 0C, for the rungs slower than 125.333 MHz only: at and above
it the board fit itself runs every path at that period or shorter, so the projection says nothing.
"""
import hashlib
import json
import subprocess
import sys
from fractions import Fraction
from pathlib import Path

METHOD = 'c1ca3e31cbee9cac123c929e90f9c8eaeb3fadfd'
VCO = Fraction(50_000_000 * 188, 3)
RUNGS = (42, 37, 34, 32, 30, 28, 27, 26, 25, 24, 22, 20, 18, 16)


def git(repo, *args):
    return subprocess.run(('git', '-C', repo) + args, check=True, capture_output=True).stdout


def main(repo, fits):
    fits = Path(fits)
    seed1 = {}
    for c in (51,) + RUNGS:
        f = fits / (('control' if c == 51 else f'c{c}') + '-s1.json')
        if f.exists():
            seed1[c] = next(iter(json.loads(f.read_text()).values()))
    print('| C0 | MHz | Commit | `make_rung.py` diff SHA-256 | Netlist SHA-256 |')
    print('| ---: | ---: | --- | --- | --- |')
    for c in (51,) + RUNGS:
        if c not in seed1:
            continue
        if c == 51:
            commit, digest = f'`{METHOD[:9]}` (method)', '—'
        else:
            rev = git(repo, 'rev-parse', f'deano/de25-486sx-fmax-c{c}').decode().strip()
            diff = git(repo, 'diff', METHOD, rev)
            commit, digest = f'`{rev[:9]}`', f'`{hashlib.sha256(diff).hexdigest()[:8]}…`'
        print(f"| {c} | {float(VCO / c / 1_000_000):.3f} | {commit} | {digest} | `{seed1[c]['netlist_sha256'][:8]}…` |")
    print()
    print('| C0 | MHz | CPU-boundary endpoints reported | Worst boundary slack at 7.979 ns | Boundary endpoints negative | '
          'Worst CPU-internal slack at 7.979 ns | CPU-internal endpoints negative |')
    print('| ---: | ---: | ---: | ---: | ---: | ---: | ---: |')
    for c, d in seed1.items():
        if c <= 25:
            continue
        p = d['paths']['4_slow_fix4_0c']
        b, i = p['cpu_boundary_at_2x'], p['cpu_internal_at_2x']
        fmt = lambda v: f'{v[0]:+.3f}' if v else '—'
        print(f"| {c} | {float(VCO / c / 1_000_000):.3f} | {p['cpu_boundary_endpoints']} | {fmt(b['worst'])} | "
              f"{b['negative']} | {fmt(i['worst'])} | {i['negative']} |")


if __name__ == '__main__':
    main(*sys.argv[1:])
