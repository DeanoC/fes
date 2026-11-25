#!/usr/bin/env python3
"""Compare a captured DE25 restore state with its serial-bound baseline."""

import argparse
import hashlib
import json
from pathlib import Path
import sys


def read_json(path):
    with Path(path).open(encoding='utf-8') as handle:
        value = json.load(handle)
    if not isinstance(value, dict):
        raise ValueError(f'{path}: expected a JSON object')
    return value


def _stable_native(state):
    text = state.get('native_readonly')
    boot_id = state.get('boot_id')
    if not isinstance(text, str) or not isinstance(boot_id, str) or not boot_id or '\n' in boot_id or '\r' in boot_id:
        return None
    lines = text.splitlines(keepends=True)
    boot_lines = [line for line in lines if line.startswith('boot_id=')]
    if len(boot_lines) != 1 or boot_lines[0].rstrip('\r\n') != f'boot_id={boot_id}':
        return None
    return ''.join(line for line in lines if not line.startswith('boot_id='))


def _baseline_native_hash(state, baseline_boot_id):
    """Reconstruct the baseline bytes, changing only the checked boot ID."""
    if _stable_native(state) is None or not isinstance(baseline_boot_id, str) or not baseline_boot_id:
        return None
    if '\n' in baseline_boot_id or '\r' in baseline_boot_id:
        return None
    text = state['native_readonly']
    if hashlib.sha256(text.encode('utf-8')).hexdigest() != state.get('native_sha256'):
        return None
    lines = text.splitlines(keepends=True)
    reconstructed = ''.join(
        f'boot_id={baseline_boot_id}' + line[len(line.rstrip('\r\n')):]
        if line.startswith('boot_id=') else line
        for line in lines
    )
    return hashlib.sha256(reconstructed.encode('utf-8')).hexdigest()


def verify(baseline, before, after, recovery=None):
    required = ('kit_id', 'serial', 'target_id', 'image_sha256', 'kit_sw', 'boot_id', 'baseline_state_sha256')
    if baseline.get('schema') != 1 or any(not baseline.get(key) for key in required):
        raise ValueError('invalid DE25 restore baseline')
    checks = {}
    serial_reboot = recovery is not None
    if serial_reboot:
        serial_reboot = (
            recovery.get('schema') == 1 and recovery.get('serial') == baseline['serial'] and
            recovery.get('device') == '/dev/serial/by-id/usb-TERASIC_DE25-Nano_TRWU8J8G-if02-port0' and
            recovery.get('command') == 'serial-sysrq-break-b' and
            recovery.get('linux_boot_seen') is True and
            isinstance(before.get('boot_id'), str) and bool(before['boot_id']) and
            isinstance(after.get('boot_id'), str) and bool(after['boot_id']) and
            recovery.get('before_boot_id') == before['boot_id'] and
            recovery.get('after_boot_id') == after.get('boot_id') and
            recovery.get('after_boot_id') != recovery.get('before_boot_id') and
            recovery.get('target_id') == baseline['target_id']
        )
    for label, state in (('before', before), ('after', after)):
        checks[f'{label}_kit'] = state.get('kit') == baseline['kit_id']
        checks[f'{label}_serial'] = state.get('serial') == baseline['serial']
        checks[f'{label}_target_id'] = state.get('target_id') == baseline['target_id']
        checks[f'{label}_boot_id'] = (state.get('boot_id') == baseline['boot_id'] if recovery is None
                                      else serial_reboot)
        health = state.get('host_health') or {}
        target = health.get('target') or {}
        artifacts = target.get('artifacts') or {}
        software = target.get('software') or {}
        checks[f'{label}_ready'] = (state.get('agent_health') or {}).get('ready') is True
        checks[f'{label}_image'] = artifacts.get('image_sha256') == baseline['image_sha256']
        checks[f'{label}_bundle'] = software.get('active_bundle') == baseline['kit_sw']
        checks[f'{label}_runtime_idle'] = (state.get('agent_status') or {}).get('state') == 'idle'
        checks[f'{label}_lease_free'] = (state.get('lease') or {}).get('state') == 'free'
    if recovery is not None:
        checks['native_state_matches_baseline'] = (
            _baseline_native_hash(before, baseline['boot_id']) == baseline['baseline_state_sha256']
        )
        checks['serial_recovery_receipt'] = serial_reboot
        stable_before = _stable_native(before)
        stable_after = _stable_native(after)
        checks['native_state_unchanged'] = stable_before is not None and stable_before == stable_after
        checks['native_state_hash_unchanged'] = (
            checks['native_state_unchanged'] and
            _baseline_native_hash(after, baseline['boot_id']) == baseline['baseline_state_sha256']
        )
    else:
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
    parser.add_argument('--serial-recovery', help='receipt from a verified J8G serial SysRq reboot')
    args = parser.parse_args(argv)
    try:
        result = verify(read_json(args.baseline), read_json(args.before), read_json(args.after),
                        read_json(args.serial_recovery) if args.serial_recovery else None)
    except (OSError, json.JSONDecodeError, ValueError) as error:
        print(f'DE25 restore verification error: {error}', file=sys.stderr)
        return 2
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
