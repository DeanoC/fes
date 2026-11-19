#!/usr/bin/env python3
"""The reach rule of method.md, computed from kit-scores.json and nothing else.

usage: python3 -I reach.py kit-scores.json reach.json

Each benchmark gives a time-like value T: 1/fps for 3DBench, 3DBench 1.0c and PCPBench, TopBench's
"Total" in microseconds, DOOM's realtics. A preset the core keeps up with throughout runs in
T(f) = A/f + B: A the work the 486 table charges, B the time that does not scale with the CPU
frequency (ISA I/O and VGA memory time, which the governor charges as fixed time, and the timer).

  fit        A and B from 25 MHz (preset 6, word 1 = 250) and 33.3 MHz (preset 6).
  tolerance  tol(f) = 2% of the prediction + the prediction's worst-case error from each fit point's
             half display quantum + the measured value's half quantum at f.
  hold-out   20 MHz (preset 6, word 1 = 200) must be within tol(20) of the prediction.
  repeat     preset 7 and preset 6 at word 1 = 667 (identical governor settings, governor bench) must be
             within 2% plus both half quanta of each other.
  A > 0      the fit must slow down with frequency.

If a benchmark fails the hold-out, the repeat or A > 0, or misses any of those four scores, its verdicts
at 50 and 66.7 MHz are "not measurable". Otherwise, at 50 MHz (preset 6, word 1 = 500) and 66.7 MHz
(preset 7):
  reached       T(f) - prediction <= tol(f)
  not reached   T(f) - prediction >  tol(f)
A preset is "not reached" if any benchmark says not reached; "reached" if every benchmark says
reached; otherwise "not measurable". 25 and 33.3 MHz are the calibration points and get no verdict of
their own beyond the hold-out.
"""
import json
import sys

BENCH = {  # key: (kit-scores field, kind, display quantum)
    '3dbench': ('3dbench_fps', 'fps', 0.1),
    '3dbench2': ('3dbench2_fps', 'fps', 0.1),
    'pcpbench': ('pcpbench', 'fps', 0.1),
    'topbench': ('topbench_total_us', 'time', 1),
    'doom_max': ('doom_max_realtics', 'time', 1),
    'doom_min': ('doom_min_realtics', 'time', 1),
}
MHZ = {'6:200': 20.0, '6:250': 25.0, '6:0': 100 / 3, '6:500': 50.0, '7:0': 200 / 3, '6:667': 66.7}
SLACK = 0.02


def as_time(value, kind, quantum):
    """T and its half-quantum error bound."""
    if value is None:
        return None, None
    if kind == 'fps':
        return 1 / value, (quantum / 2) / value ** 2
    return float(value), quantum / 2


def fit(t1, t2, f1, f2):
    a = (t1 - t2) / (1 / f1 - 1 / f2)
    return a, t1 - a / f1


def predict(points, f):
    """Prediction at f, and its worst case over each fit point moved by its half quantum."""
    (t1, e1, f1), (t2, e2, f2) = points
    a, b = fit(t1, t2, f1, f2)
    centre = a / f + b
    worst = 0.0
    for s1 in (-1, 1):
        for s2 in (-1, 1):
            a2, b2 = fit(t1 + s1 * e1, t2 + s2 * e2, f1, f2)
            worst = max(worst, abs(a2 / f + b2 - centre))
    return centre, worst, a, b


def main() -> int:
    scores = json.load(open(sys.argv[1]))['presets']
    out = {'rule': 'method.md, "Reach"', 'benchmarks': {}, 'presets': {}}
    for name, (field, kind, q) in BENCH.items():
        t = {k: as_time((scores.get(k) or {}).get(field), kind, q) for k in MHZ}
        b = {'field': field, 'T': {k: v[0] for k, v in t.items()}}
        missing = [k for k in ('6:200', '6:250', '6:0', '7:0', '6:667') if t[k][0] is None]
        if missing:
            b['measurable'], b['why'] = False, f'missing {missing}'
        else:
            pts = ((t['6:250'][0], t['6:250'][1], 25.0), (t['6:0'][0], t['6:0'][1], 100 / 3))
            p20, w20, a, bb = predict(pts, 20.0)
            tol20 = SLACK * p20 + w20 + t['6:200'][1]
            r7, r667 = t['7:0'], t['6:667']
            repeat = abs(r7[0] - r667[0]) <= SLACK * r7[0] + r7[1] + r667[1]
            b.update({'A': a, 'B': bb, 'holdout': {'measured': t['6:200'][0], 'predicted': p20, 'tol': tol20,
                                                    'pass': abs(t['6:200'][0] - p20) <= tol20},
                      'repeat': {'7': r7[0], '6:667': r667[0], 'pass': repeat}})
            b['measurable'] = a > 0 and b['holdout']['pass'] and repeat
            if not b['measurable']:
                b['why'] = 'A <= 0' if a <= 0 else ('hold-out failed' if not b['holdout']['pass'] else 'repeat failed')
            b['verdicts'] = {}
            for k in ('6:500', '7:0'):
                if t[k][0] is None:
                    b['verdicts'][k] = {'verdict': 'not measurable', 'why': 'missing'}
                    continue
                p, w, _, _ = predict(pts, MHZ[k])
                tol = SLACK * p + w + t[k][1]
                excess = t[k][0] - p
                v = 'not measurable' if not b['measurable'] else ('reached' if excess <= tol else 'not reached')
                b['verdicts'][k] = {'measured': t[k][0], 'predicted': p, 'excess_pct': 100 * excess / p,
                                    'tol_pct': 100 * tol / p, 'verdict': v}
        out['benchmarks'][name] = b
    for k in ('6:500', '7:0'):
        vs = [b.get('verdicts', {}).get(k, {}).get('verdict', 'not measurable') for b in out['benchmarks'].values()]
        out['presets'][k] = {'verdict': 'not reached' if 'not reached' in vs else
                             ('reached' if all(v == 'reached' for v in vs) else 'not measurable'),
                             'by_benchmark': dict(zip(out['benchmarks'], vs))}
    json.dump(out, open(sys.argv[2], 'w'), indent=1)
    for k, v in out['presets'].items():
        print(k, v['verdict'], v['by_benchmark'])
    for name, b in out['benchmarks'].items():
        print(name, 'measurable' if b.get('measurable') else f'not measurable ({b.get("why")})')
    return 0


if __name__ == '__main__':
    sys.exit(main())
