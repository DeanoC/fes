"""Print coleco_v2's condition-3 per-seed timing from the producer's own ranking.

The ratios in the DeanoC/fes#1806 discussion were first quoted from a file that
existed only on the build host, which makes them unauditable however accurate
they are -- the defect DeanoC/fes#1862 had to retract six numbers for. This
reads the committed `coleco_v2.qor-ranking.json`, so every figure in the record
is derived from a primary artefact rather than transcribed by hand.

    python3 ratios.py
"""
import json
import os

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
    with open(os.path.join(HERE, 'coleco_v2.qor-ranking.json')) as handle:
        ranking = json.load(handle)
    measured = sorted(set(rows(ranking)), key=lambda row: -row[2])
    print(f'{len(measured)} placements, best first')
    for seed, weight, ratio in measured:
        met = 'met' if ratio >= 1.0 else 'MISS'
        print(f'  seed {seed:>2}  weight {weight}  worst_ratio {ratio:.10f}  {met}')
    best = measured[0]
    print(f'best: seed {best[0]} at {best[2]:.4f}; '
          f'{len([r for r in measured if r[2] >= 1.0])} of {len(measured)} met timing')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
