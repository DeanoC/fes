#!/usr/bin/env python3
"""compare_fits.py ARM.json BASE.json: method.md's comparison of two fit_summary.py outputs.
Prints each arm's ALMs and machine (`system`) ALMs, median and spread, every seed's closure,
the worst slack of each analysis over all seeds, and the arm-minus-base differences with
whether each is measured (larger than the larger spread) or within the spread."""
import json
import statistics
import sys


def stats(fits, key):
    values = [fit[key] for fit in fits.values()]
    return statistics.median(values), round(max(values) - min(values), 1)


arm, base = (json.load(open(path)) for path in sys.argv[1:3])
for name, fits in (('arm', arm), ('base', base)):
    print(name, 'seeds', sorted(fit['seed'] for fit in fits.values()),
          'all close' if all(fit['closes'] for fit in fits.values()) else 'NOT ALL CLOSE',
          'netlists', sorted({fit['netlist_sha256'][:12] for fit in fits.values()}))
    for key in ('alms', 'system_alms'):
        median, spread = stats(fits, key)
        print(f'  {key}: {[fit[key] for fit in fits.values()]} median {median:.1f} spread {spread}')
    kinds = sorted({kind for fit in fits.values() for kind in fit['worst']})
    print('  worst:', ', '.join(f"{kind} {min(fit['worst'][kind]['slack'] for fit in fits.values()):+.3f}"
                                for kind in kinds))
for key in ('alms', 'system_alms'):
    (am, aspread), (bm, bspread) = stats(arm, key), stats(base, key)
    delta = am - bm
    print(f'{key}: {delta:+.1f} ({"measured" if abs(delta) > max(aspread, bspread) else "within the spread"};'
          f' larger spread {max(aspread, bspread)})')
median = stats(arm, 'alms')[0]
print(f'arm median against the 92% line 43,056: {43056 - median:+.1f} under; against the device 46,800: '
      f'{46800 - median:+.1f} under; largest seed {max(fit["alms"] for fit in arm.values())}')
