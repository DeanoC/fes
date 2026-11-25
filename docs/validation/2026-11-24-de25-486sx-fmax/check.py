#!/usr/bin/env python3
"""check.py FITS_DIR LABEL=PROJECT...: the method's per-fit invariants, one line each, PASS/FAIL.

From FITS_DIR's JSON: every fit's PLL is M 188, N 3, C1 255 and C0 its rung's (control 51); every
seed of a rung compiled one netlist. From each PROJECT's paths/cross-*-setup-*.rpt: no setup path
between the machine clock and the audio master (machine_pll_outclk1) has an edge relationship, that
is every one is a fixed bound (method.md's ratio check), and the range of those paths' slack.
"""
import collections
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from rung_summary import rows, kind  # noqa: E402

AUDIO = 'fes_machine_pll|machine_pll_outclk1'


def main(fits, *projects):
    ok = True
    nets = collections.defaultdict(set)
    for f in sorted(Path(fits).glob('*.json')):
        rung, seed = f.stem.rsplit('-s', 1)
        d = next(iter(json.loads(f.read_text()).values()))
        c0 = 51 if rung == 'control' else int(rung[1:])
        pll = d['pll']
        good = (pll['m'], pll['n'], pll['outputs'][0][1], pll['outputs'][1][1]) == ('188', '3', str(c0), '255')
        ok &= good
        print(f"{'PASS' if good else 'FAIL'} pll {f.stem}: M {pll['m']} N {pll['n']} C0 {pll['outputs'][0][1]} C1 {pll['outputs'][1][1]}")
        nets[rung].add(d['netlist_sha256'])
    for rung, s in sorted(nets.items()):
        ok &= len(s) == 1
        print(f"{'PASS' if len(s) == 1 else 'FAIL'} netlist {rung}: {len(s)} distinct")
    for arg in projects:
        label, project = arg.split('=', 1)
        audio = [r for p in Path(project, 'paths').glob('cross-*-setup-*.rpt') for r in rows(p)
                 if AUDIO in (r['launch'], r['latch'])]
        edge = [r for r in audio if kind(r) != 'fixed']
        ok &= bool(audio) and not edge
        print(f"{'PASS' if audio and not edge else 'FAIL'} audio {label}: {len(audio)} machine<->audio setup paths over "
              f"every corner, {len(edge)} not a fixed bound, slack {min(r['slack'] for r in audio):+.3f} to "
              f"{max(r['slack'] for r in audio):+.3f}")
    print('ALL PASS' if ok else 'SOME FAIL')
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
