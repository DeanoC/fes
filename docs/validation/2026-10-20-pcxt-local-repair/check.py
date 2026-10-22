#!/usr/bin/env python3
"""Validate the retained benchmark and terminal evidence without routing or hardware."""
from pathlib import Path
import json
import replay

here = Path(__file__).resolve().parent
fixture, control = replay.prepare('control')
_, swap = replay.prepare('swap')
changes = []

def compare(a, b, path=()):
    if isinstance(a, dict):
        assert a.keys() == b.keys(), path
        for key in a:
            compare(a[key], b[key], path + (key,))
    elif a != b:
        changes.append(path)

compare(control, swap)
assert len(changes) == 2 and all(path[-1] == 'NEXTPNR_BEL' for path in changes)
assert len(control['modules']['top']['cells']) == 10462
records = [json.loads(line) for line in (here / 'runs.jsonl').read_text().splitlines()]
routes = {row['label']: row for row in records if row.get('classification') == 'diagnostic-fixed-placement-replay'}
assert set(routes) == {'control', 'swap', 'swap-direct'}
for row in routes.values():
    assert row['status'] == 'failed' and row['returncode'] != 0
    assert row['nextpnr_sha256'] == fixture['nextpnr_sha256']
    assert row['placement_verification'] == {'all_input_cells_retained_at_input_bels': True, 'count': 10462}
    assert row['timing_summary']['final_analogue_model']
    assert row['fmax']['clocks[0]']['constraint'] == 100
    assert row['fmax']['clocks[0]']['achieved'] < 100
    for name, clock in row['timing_summary']['clocks'].items():
        assert clock['hold_wns_ns'] >= 0
        if name != 'clocks[0]':
            assert clock['setup_wns_ns'] >= 0
    assert row['timeout_seconds'] == 600
    assert '--timing-allow-fail' not in row['command']
    assert row['env'] == {'FES_GPU_LEASE_OWNER': 'pcxt-agent', 'GPU_LEASE_MAX_PER_OWNER': '2', 'NEXTPNR_REQUIRE_GPU': '1'}
assert routes['swap']['artifacts']['diagnostic.rbf'] == routes['swap-direct']['artifacts']['diagnostic.rbf']

def target_slack(row):
    return next(e['setup_slack_ns'] for e in row['selected_result_net']['endpoints']
                if e['cell'].endswith('.eu_alu_last_result_MISTRAL_FF_Q_5'))

assert target_slack(routes['control']) < 0 < target_slack(routes['swap-direct'])
assert target_slack(routes['swap-direct']) - target_slack(routes['control']) > 2
probe = next(r for r in records if r.get('record') == 'swap-final-preflight')
assert probe['returncode'] == 0 and probe['no_new_invalid_bels']
assert len(probe['baseline_invalid']) == len(probe['candidate_invalid']) == 26
assert next(r for r in records if r.get('record') == 'baseline-dump-v2')['same_rbf_as_parent']
assert sum(row['charged_seconds'] for row in routes.values()) <= 1800
print('PASS: checkpoint, exactly two BEL changes, 3 terminal routes, identical swap RBFs, target improvement, unchanged gates and evidence limits')
