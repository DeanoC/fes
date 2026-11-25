#!/usr/bin/env python3
"""Summarize the four registered DX builds and their retained LOADED route reports."""

import argparse
import json
import math
import re
import sys
from fractions import Fraction
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / '2026-11-24-de25-486sx-fmax'))
from rung_summary import summary

CORNERS = {
    '4_slow_fix4_100c': 'Slow fix4 100C Model',
    '4_slow_fix4_0c': 'Slow fix4 0C Model',
    '4_slow_fix4a_0c': 'Slow fix4a 0C Model',
    'MIN_fast_fix4_0c': 'Fast fix4 0C Model',
}
VCO_HZ = Fraction(50_000_000 * 188, 3)
CORNER_LINE = re.compile(r'CORNER \{([^}]+)\} (setup|hold|recovery|removal|mpw) (\d+) (-?\d+\.\d+)')


def corner_slacks(path):
    expected = {(corner, analysis) for corner in CORNERS.values()
                for analysis in ('setup', 'hold', 'recovery', 'removal', 'mpw')}
    slacks = {}
    for line in path.read_text().splitlines():
        if not line.startswith('CORNER '):
            continue
        match = CORNER_LINE.fullmatch(line)
        if match is None:
            raise ValueError(f'invalid corner report row: {line}')
        corner, analysis, count, slack = match.groups()
        if (corner, analysis) in slacks or not int(count):
            raise ValueError(f'duplicate corner or no paths: {line}')
        slacks[corner, analysis] = float(slack)
    if slacks.keys() != expected:
        raise ValueError('incomplete per-analysis corner coverage')
    return {corner: {analysis: slack for (name, analysis), slack in slacks.items() if name == corner}
            for corner in CORNERS.values()}


def loaded_routes(path):
    routes = {}
    for line in path.read_text().splitlines():
        corner, delay, skew = line.split('\t')
        delay = float(delay.removeprefix('data_delay='))
        skew = float(skew.removeprefix('skew='))
        if corner not in CORNERS or corner in routes or not all(map(math.isfinite, (delay, skew))) or delay < 0:
            raise ValueError(f'invalid LOADED route at {corner}')
        routes[corner] = {'data_ns': delay, 'skew_ns': skew, 'total_ns': delay + abs(skew)}
    if routes.keys() != CORNERS.keys():
        raise ValueError('incomplete LOADED corner coverage')
    return routes


def seed_result(output, logs, seed, divider):
    record = {'seed': seed, 'passes': False, 'failures': []}
    failures = record['failures']
    try:
        exit_code = int((logs / 'producer.exit').read_text())
        record['producer_exit'] = exit_code
        build = json.loads((output / 'build-inputs.json').read_text())
        record['revision'] = build['revision']
        fit = summary(output / 'signoff/combined/hps/project')
        clock = VCO_HZ / divider
        parameters = build['parameters']
        pixel_time = Fraction(3 * 1_000_000_000, parameters['pixel_clock_hz'])
        loaded_budget = min(3.9, float(5 * 1_000_000_000 / clock - pixel_time - 10))
        record.update(fit=fit, loaded_budget_ns=loaded_budget)
        if (parameters['model'], parameters['seed'], parameters['machine_clock_hz'], fit['seed']) != (
            '486dx', seed, round(clock), seed
        ):
            failures.append('wrong model, clock or seed')
        pll = fit['pll']
        if (pll['m'], pll['n'], [counter for _, counter in pll['outputs']]) != ('188', '3', [str(divider), '255']):
            failures.append('wrong fitted PLL counters')
        if not fit['passes']:
            failures.append('negative timing slack or fitter area over budget')
        timing_path = output / 'signoff-timing.json'
        timing = json.loads(timing_path.read_text()) if timing_path.is_file() else {}
        record['unreviewed_failed_rules'] = timing.get('unreviewed_failed_rules')
        if not timing.get('package_timing_admission') or exit_code != 0:
            failures.append('registered producer did not admit the package')
        else:
            record['package'] = json.loads((output / 'build-summary.json').read_text())
        slacks = corner_slacks(logs / 'corners.log')
        record['corner_slacks_ns'] = slacks
        if any(slack < 0 for analyses in slacks.values() for slack in analyses.values()):
            failures.append('negative per-corner slack')
        if (logs / 'report.exit').read_text().strip() != '0':
            failures.append('LOADED route report failed')
        routes = loaded_routes(logs / 'loaded.tsv')
        record['loaded_routes'] = routes
        if any(route['total_ns'] > loaded_budget for route in routes.values()):
            failures.append('LOADED route exceeds the re-arm budget')
    except (OSError, ValueError, KeyError, IndexError, AttributeError) as error:
        failures.append(f'incomplete evidence: {error}')
    record['passes'] = not failures
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('divider', type=int)
    parser.add_argument('build_directory', type=Path)
    parser.add_argument('log_directory', type=Path)
    args = parser.parse_args()
    seeds = [seed_result(args.build_directory / f's{seed}', args.log_directory / f's{seed}', seed, args.divider)
             for seed in range(1, 5)]
    revisions = {result.get('revision') for result in seeds}
    passed = len(revisions) == 1 and all(result['passes'] for result in seeds)
    print(json.dumps({'c0': args.divider, 'passes': passed, 'seeds': seeds}, indent=2, sort_keys=True))
    return 0 if passed else 1


if __name__ == '__main__':
    sys.exit(main())
