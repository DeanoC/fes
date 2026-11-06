#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0-or-later
"""Replay the scripts recorded with the Sinclair ZX80 ROM (ref/*.txt) on the
FES ZX80 RTL running the open ROM, and report differences in program,
variables, edit line and screen by the same rules as compare.py. Then type
a few scripts as a USB keyboard does, SHIFT down before the key and up after
it (SHIFT~), and require the same result as SHIFT and the key together.

  rtl_compare.py PROBE [REF...]   PROBE is the bench build_probe.sh builds
"""
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
sys.path.insert(0, sys.path[0])
from compare import REF, blocks

FILES = ['p1', 'p2', 'p3', 'p4', 'p5', 'pb', 'pc', 'pd', 'pe', 'pf', 'ph', 'pr']
SHIFT_FIRST = [
    '1 0 F I SHIFT+L 1 SHIFT+4 5 NL 2 0 O I SHIFT+X NL 3 0 N I NL R NL',
    '1 0 O SHIFT+Y H I SHIFT+Y NL 2 0 O SHIFT+Y H SHIFT+X SHIFT+Y NL R NL',
]


def parse(text):
    got = {'screen': {}}
    for line in text.splitlines():
        if line.startswith(('prog:', 'vars:', 'eline:')):
            k, v = line.split(':', 1)
            got[k] = v.strip()
        elif line.startswith('  ') and '|' in line:
            n, rest = line.split('|', 1)
            got['screen'][int(n)] = rest[:-1]
    return got


def run(probe, script):
    out = subprocess.run([probe, 'probe', script], capture_output=True, text=True, timeout=1800)
    if out.returncode:
        raise RuntimeError(f'probe failed: {out.stderr.strip()}')
    return parse(out.stdout)


def check(probe, ref):
    try:
        got = run(probe, ref['script'])
        if 'shift_first' in ref:
            ref = {**run(probe, ref['shift_first']), 'script': ref['script']}
    except RuntimeError as exc:
        return [str(exc)]
    for d in (ref, got):
        d['eline'] = ' '.join(b for b in d.get('eline', '').split() if b != 'b0')
    diffs = []
    for k in ('prog', 'vars', 'eline'):
        if k == 'eline' and not ref.get(k):
            continue            # a report: this ROM leaves the old line
        if ref.get(k, '') != got.get(k, ''):
            diffs.append(f'{k}: want [{ref.get(k)}] got [{got.get(k)}]')
    for n in sorted(set(ref['screen']) | set(got['screen'])):
        if ref['screen'].get(n, '') != got['screen'].get(n, ''):
            diffs.append(f'row {n}: want |{ref["screen"].get(n, "")}| got |{got["screen"].get(n, "")}|')
    return diffs


def main():
    probe = sys.argv[1]
    refs = [(f, ref) for f in (sys.argv[2:] or FILES) for ref in blocks(REF + f + '.txt')]
    if not sys.argv[2:]:
        refs += [('shift-first', {'script': s.replace('SHIFT+', 'SHIFT~'), 'shift_first': s})
                 for s in SHIFT_FIRST]
    with ThreadPoolExecutor(max(1, min(8, os.cpu_count() or 1))) as pool:
        results = list(pool.map(lambda fr: check(probe, fr[1]), refs))
    bad = 0
    for (f, ref), diffs in zip(refs, results):
        if diffs:
            bad += 1
            print(f'{f}: {ref["script"]}')
            for d in diffs:
                print('   ' + d)
    print(f'RTL differences: {bad} of {len(refs)}')
    sys.exit(1 if bad else 0)


if __name__ == '__main__':
    main()
