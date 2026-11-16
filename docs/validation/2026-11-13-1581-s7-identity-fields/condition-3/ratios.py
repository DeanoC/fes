"""Print a failed core's condition-3 per-seed timing from the producer's ranking.

The ratios in the DeanoC/fes#1806 discussion were first quoted from a file that
existed only on the build host, which makes them unauditable however accurate
they are -- the defect DeanoC/fes#1862 had to retract six numbers for. This
reads the committed `coleco_v2.qor-ranking.json`, so every figure in the record
is derived from a primary artefact rather than transcribed by hand.

    python3 ratios.py [coleco_v2|c64]

A `worst_ratio` of exactly 0.0 is not a near-miss: it is a seed whose router hit
the recipe's 600 s budget and was killed (exit 124), so it produced no placement
to time. c64 has seven of those and coleco_v2 none, which is why the two
failures are not the same failure.
"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))


def rows(node):
    """Every (seed, weight, worst_ratio) the ranking records, at any depth."""
    found = []
    if isinstance(node, dict):
        if 'seed' in node and 'worst_ratio' in node:
            found.append((node['seed'], node.get('weight'), node['worst_ratio']))
        for value in node.values():
            found += rows(value)
    elif isinstance(node, list):
        for value in node:
            found += rows(value)
    return found


def main():
    core = sys.argv[1] if len(sys.argv) > 1 else 'coleco_v2'
    with open(os.path.join(HERE, f'{core}.qor-ranking.json')) as handle:
        ranking = json.load(handle)
    print(f'{core}:')
    measured = sorted(set(rows(ranking)), key=lambda row: -row[2])
    print(f'{len(measured)} seeds, best first')
    for seed, weight, ratio in measured:
        if ratio == 0.0:
            verdict = 'NO PLACEMENT (router hit the 600 s budget)'
        elif ratio >= 1.0:
            verdict = 'met'
        else:
            verdict = 'MISS'
        print(f'  seed {seed:>2}  weight {weight}  worst_ratio {ratio:.10f}  {verdict}')
    timed = [r for r in measured if r[2] > 0.0]
    best = measured[0]
    print(f'best: seed {best[0]} at {best[2]:.4f}; '
          f'{len([r for r in measured if r[2] >= 1.0])} of {len(measured)} met timing; '
          f'{len(measured) - len(timed)} produced no placement at all')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
