"""How much timing headroom did each condition-3 build actually have?

"Eleven of twelve passed" hides the thing that matters for DeanoC/fes#1878: a
`BUILD_ID` move reshuffles placement, and a core passes or fails on how much
margin it had on its *tightest* clock. `coleco_v2` failed outright; this prints
how close the others came, from the producers' own timing blocks in
`clocks.json` (copied verbatim from each build-summary.json, not transcribed).

    python3 margins.py
"""
import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))

# Observed swing in achieved frequency on one core across two identities, in
# percentage points: ramtest's memory clock measured +3.67% here and +0.98% at
# DeanoC/fes#1881's head. Any core inside this band of its constraint can flip.
SWING_PP = 2.7


def tightest(timing):
    """The clock with the least achieved/constraint ratio, and that ratio."""
    best = None
    for name, entry in sorted(timing.items()):
        if not isinstance(entry, dict):
            continue
        achieved, constraint = entry.get('achieved_mhz'), entry.get('constraint_mhz')
        if not isinstance(achieved, (int, float)) or not isinstance(constraint, (int, float)):
            continue
        if constraint <= 0:
            continue
        ratio = achieved / constraint
        if best is None or ratio < best[2]:
            best = (name, entry.get('clock', name), ratio, achieved, constraint)
    return best


def main():
    with open(os.path.join(HERE, 'clocks.json')) as handle:
        cores = json.load(handle)
    rows = []
    for core, record in sorted(cores.items()):
        found = tightest(record.get('timing') or {})
        if found is None:
            print(f'{core:12} no timing block recorded')
            continue
        name, clock, ratio, achieved, constraint = found
        rows.append((ratio, core, name, clock, achieved, constraint))
    rows.sort()
    print(f'{"core":12} {"tightest clock":34} {"achieved":>10} {"constraint":>11}  margin')
    for ratio, core, name, clock, achieved, constraint in rows:
        flag = '  <-- inside the observed identity swing' if (ratio - 1) * 100 < SWING_PP else ''
        print(f'{core:12} {f"{name} ({clock})"[:34]:34} {achieved:>10.3f} {constraint:>11.3f}  '
              f'{(ratio - 1) * 100:+6.2f}%{flag}')
    print()
    print(f'coleco_v2    FAILED: no placement met timing, best seed 0.9796 '
          f'({-2.04:+.2f}% on its tightest clock)')
    print(f'cores within {SWING_PP}pp of their constraint can flip on the next identity move')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
