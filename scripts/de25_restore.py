#!/usr/bin/env python3
"""Compare a captured DE25 restore state with its serial-bound baseline."""

import argparse
import json
from pathlib import Path
import sys


def read_json(path):
    with Path(path).open(encoding='utf-8') as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f'{path}: expected a JSON object')
    return value


def verify(baseline, before, after):
    required = ('kit_id', 'serial', 'target_id', 'image_sha256', 'kit_sw', 'boot_id', 'baseline_state_sha256')
    if baseline.get('schema') != 1 or any(not baseline.get(key) for key in required):
        raise ValueError('invalid DE25 restore baseline')
    checks = {}
    for label, state in (('before', before), ('after', after)):
        checks[f'{label}_kit'] = state.get('kit') == baseline['kit_id']
        checks[f'{label}_serial'] = state.get('serial') == baseline['serial']
        checks[f'{label}_target_id'] = state.get('target_id') == baseline['target_id']
        checks[f'{label}_boot_id'] = state.get('boot_id') == baseline['boot_id']
        health = state.get('host_health') or {}
        target = health.get('target') or {}
        artifacts = target.get('artifacts') or {}
        software = target.get('software') or {}
        checks[f'{label}_ready'] = (state.get('agent_health') or {}).get('ready') is True
        checks[f'{label}_image'] = artifacts.get('image_sha256') == baseline['image_sha256']
        checks[f'{label}_bundle'] = software.get('active_bundle') == baseline['kit_sw']
        checks[f'{label}_runtime_idle'] = (state.get('agent_status') or {}).get('state') == 'idle'
        checks[f'{label}_lease_free'] = (state.get('lease') or {}).get('state') == 'free'
    checks['native_state_matches_baseline'] = before.get('native_sha256') == baseline['baseline_state_sha256']
    checks['native_state_unchanged'] = before.get('native_readonly') == after.get('native_readonly')
    checks['native_state_hash_unchanged'] = before.get('native_sha256') == after.get('native_sha256')
    return {'passed': all(checks.values()), 'kit_id': baseline['kit_id'], 'serial': baseline['serial'],
            'checks': checks}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--baseline', required=True)
    parser.add_argument('--before', required=True)
    parser.add_argument('--after', required=True)
    args = parser.parse_args(argv)
    try:
        result = verify(read_json(args.baseline), read_json(args.before), read_json(args.after))
    except (OSError, json.JSONDecodeError, ValueError) as error:
        print(f'DE25 restore verification error: {error}', file=sys.stderr)
        return 2
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
