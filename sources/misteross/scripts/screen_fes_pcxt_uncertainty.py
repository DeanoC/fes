#!/usr/bin/env python3
"""Screen a retained XT route against uncertainty from its Quartus clock reference.

This checks setup and hold headroom only. It never grants whole-board qualification or
changes the producer's clocks, constraints, reports or bitstream.
"""
from __future__ import annotations
import argparse
from collections import defaultdict
import hashlib
import json
import math
from pathlib import Path
import re

CORNERS = {'7_slow_1100mv_100c', '7_slow_1100mv_-40c',
           'MIN_fast_1100mv_100c', 'MIN_fast_1100mv_-40c'}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def finite(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise ValueError('expected a finite timing number')
    return float(value)


def clock_event(name):
    match = re.fullmatch(r'(memory_clock|video_clock)\|general\[([01])\]\.gpll~PLL_OUTPUT_COUNTER\|divclk( \(INVERTED\))?', name)
    if not match or (match[1] == 'video_clock' and match[2] != '0'):
        raise ValueError(f'unsupported Quartus clock identity: {name}')
    clock = 'clk_pixel' if match[1] == 'video_clock' else f'clocks[{match[2]}]'
    return f'{"negedge" if match[3] else "posedge"} {clock}'



def uncertainties(root, kind="setup"):
    """Retain clock edge identity and require all four corners per pair."""
    if kind not in ('setup', 'hold'):
        raise ValueError('unknown uncertainty check kind')
    values = defaultdict(dict)
    reports = {}
    if {p.name for p in root.iterdir() if p.is_dir()} != CORNERS:
        raise ValueError('expected the four complete Quartus operating corners')
    for corner in sorted(CORNERS):
        path = root / corner / f'internal-{kind}.rpt'
        reports[str(path.relative_to(root))] = sha(path)
        text = path.read_text()
        blocks = re.split(r'\nPath #\d+:', text)[1:]
        if not blocks:
            raise ValueError(f'no full {kind} paths in {path}')
        for block in blocks:
            def field(label):
                matches = re.findall(r'(?m)^; ' + label + r'\s*;([^\n;]+)', block)
                if len(matches) != 1:
                    raise ValueError(f'missing or repeated {label}')
                return matches[0].strip()
            pair = (clock_event(field('Launch Clock')), clock_event(field('Latch Clock')))
            increments = re.findall(r';\s*[-\d.]+\s*;\s*([-\d.]+)\s*;[^\n]*; clock uncertainty', block)
            if len(increments) != 1:
                raise ValueError(f'expected one explicit {kind} uncertainty increment')
            increment = finite(float(increments[0]))
            if (kind == 'setup' and increment > 0) or (kind == 'hold' and increment < 0):
                raise ValueError('uncertainty must reduce setup or increase hold required time')
            value = -increment if kind == 'setup' else increment
            previous = values[pair].get(corner)
            if previous is not None and previous != value:
                raise ValueError('ambiguous uncertainty for one clock pair/corner')
            values[pair][corner] = value
    for pair, corners in values.items():
        if set(corners) != CORNERS:
            raise ValueError(f'incomplete corner coverage for {pair}')
    return {pair: max(corners.values()) for pair, corners in values.items()}, reports


def screen(report, budgets, hold_budgets=None):
    hold_budgets = {} if hold_budgets is None else hold_budgets
    for value in list(budgets.values()) + list(hold_budgets.values()):
        if finite(value) < 0:
            raise ValueError('negative uncertainty budget')
    if report.get('timing_summary', {}).get('final_analogue_model') is not True:
        raise ValueError('expected a final analogue timing report')
    if set(report.get('fmax', {})) != {'clocks[0]', 'clocks[1]', 'clk_pixel'}:
        raise ValueError('unexpected native clock set')
    for clock, mhz in {'clocks[0]': 100, 'clocks[1]': 100, 'clk_pixel': 74.25}.items():
        # Native derived-frequency serialization differs by less than one ppm.
        if not math.isclose(finite(report['fmax'][clock]['constraint']), mhz, rel_tol=1e-6, abs_tol=1e-6):
            raise ValueError('unexpected native clock frequency')
    seen = set()
    groups = defaultdict(list)
    failures = []
    hold_failures = []
    unpaired = []
    uncovered = defaultdict(int)
    for net in report['detailed_net_timings']:
        for endpoint in net['endpoints']:
            identity = (net['driver'], net['port'], net['event'], net['net'],
                        endpoint['cell'], endpoint['port'], endpoint['event'])
            if identity in seen:
                raise ValueError('duplicate native timing identity')
            seen.add(identity)
            if not endpoint.get('setup_checked') or not endpoint.get('hold_checked'):
                unpaired.append(list(identity))
                continue
            setup = finite(endpoint['setup_slack_ns'])
            hold = finite(endpoint['hold_slack_ns'])
            pair = (net['event'], endpoint['event'])
            if pair not in budgets or pair not in hold_budgets:
                uncovered[pair] += 1
                continue
            adjusted = setup - budgets[pair]
            adjusted_hold = hold - hold_budgets[pair]
            groups[pair].append((adjusted, adjusted_hold))
            if adjusted < 0:
                failures.append({'identity': list(identity), 'native_setup_ns': setup,
                                 'uncertainty_ns': budgets[pair], 'adjusted_setup_ns': adjusted})
            if adjusted_hold < 0:
                hold_failures.append({'identity': list(identity), 'native_hold_ns': hold,
                                      'uncertainty_ns': hold_budgets[pair], 'adjusted_hold_ns': adjusted_hold})
    if not seen:
        raise ValueError('no native endpoints screened')
    failures.sort(key=lambda row: row['adjusted_setup_ns'])
    return {'native_endpoint_rows': len(seen),
            'clock_pairs': [{'launch': p[0], 'capture': p[1], 'rows': len(v),
                             'setup_uncertainty_ns': budgets[p], 'hold_uncertainty_ns': hold_budgets[p],
                             'adjusted_wns_ns': min(x[0] for x in v),
                             'adjusted_hold_wns_ns': min(x[1] for x in v)}
                            for p, v in sorted(groups.items())],
            'setup_failures': failures, 'hold_failures': hold_failures,
            'uncovered_clock_pairs': [{'launch': p[0], 'capture': p[1], 'rows': n}
                                      for p, n in sorted(uncovered.items())],
            'unpaired_native_rows': unpaired,
            'checked_setup_headroom_pass': bool(groups) and not failures,
            'checked_hold_headroom_pass': bool(groups) and not hold_failures,
            'all_checked_rows_have_uncertainty': not uncovered,
            'complete_native_uncertainty_coverage': not uncovered and not unpaired}


def run(diagnostic, comparison):
    receipt = json.loads((diagnostic / 'diagnostic.json').read_text())
    report = json.loads((diagnostic / 'timing.json').read_text())
    if receipt.get('status') != 'routed-diagnostic-unqualified' or receipt.get('working_tree_changes') != []:
        raise ValueError('expected a completed diagnostic from clean source')
    if receipt.get('timing') != report:
        raise ValueError('timing report differs from the producer receipt')
    rbf_hash = sha(diagnostic / 'diagnostic.rbf')
    if receipt.get('rbf_sha256') != rbf_hash:
        raise ValueError('producer RBF hash mismatch')
    branch = json.loads((comparison / 'branch-comparison.json').read_text())
    if (branch.get('native_rbf_sha256') != rbf_hash or
            branch.get('Quartus_rbf_sha256') != sha(comparison / 'output_files/top.rbf') or
            branch.get('native') != branch.get('Quartus') or
            len(branch.get('native', [])) != 10 or branch.get('equal') is not True):
        raise ValueError('expected a matching nine-edge reference for this source RBF')
    summary = json.loads((comparison / 'summary.json').read_text())
    for name, key in [('input-receipt.json', 'input_receipt_sha256'),
                      ('output_files/top.fit.summary', 'fit_summary_sha256'),
                      ('output_files/top.sta.summary', 'sta_summary_sha256'),
                      ('audit.tcl', 'audit_sha256')]:
        if summary.get(key) != sha(comparison / name):
            raise ValueError(f'changed Quartus reference evidence: {name}')
    fitted = (comparison / 'output_files/top.fit.summary').read_text()
    if 'Fitter Status : Successful' not in fitted or 'Timing Models : Final' not in fitted:
        raise ValueError('expected final fitted Quartus timing models')
    budgets, reports = uncertainties(comparison / 'timing-audit')
    hold_budgets, hold_reports = uncertainties(comparison / 'timing-audit', 'hold')
    reports.update(hold_reports)
    result = screen(report, budgets, hold_budgets)
    result.update({'schema': 1, 'classification': 'partial reference-based setup/hold uncertainty screen',
                   'source_revision': receipt['revision'], 'source_rbf_sha256': rbf_hash,
                   'native_report_sha256': sha(diagnostic / 'timing.json'),
                   'reference_rbf_sha256': branch['Quartus_rbf_sha256'],
                   'reference_report_hashes': reports,
                   'whole_board_qualified': False,
                   'limits': ['Quartus reference assumptions and different clock side branches remain',
                              'No external IO, reset/CDC or hardware qualification',
                              'This screen does not modify native timing or adopt a new physical model']})
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--diagnostic', type=Path, required=True)
    parser.add_argument('--quartus-comparison', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    result = run(args.diagnostic, args.quartus_comparison)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    return 0 if (result['checked_setup_headroom_pass'] and result['checked_hold_headroom_pass']
                 and result['complete_native_uncertainty_coverage']) else 1


if __name__ == '__main__':
    raise SystemExit(main())
