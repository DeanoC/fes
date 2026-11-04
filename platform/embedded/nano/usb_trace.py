"""Observe the selected USB-only native diagnostic, without device authority.

The caller supplies identities from the checked matched chain. A complete
capture proves reported initialization and stop; input evidence is reported
separately and never grants ordinary runtime or core admission.
"""
import importlib.util
from pathlib import Path
import re

from native_trace import MAX_BYTES, decode_native_boot

READY = 'FES_NATIVE_USB READY transport=pio input=boot-keyboard data=held'
PASS = 'FES_NATIVE_USB PASS scope=usb-only-pio'
REPORT = re.compile(r'FES_NATIVE_USB REPORT ([0-9a-f]{16})')
STOP = re.compile(r'FES_NATIVE_USB STOP owner=([0-9a-f]{1,16}) generation=([0-9]{1,20}) data=held')


def startup_checkpoints(data):
    # usbcheck has no main/SD/FAT checkpoints. Both early BSP observations
    # still have to follow all seven entry-loader observations.
    spec = importlib.util.spec_from_file_location('fes_usb_startup', Path(__file__).resolve().parents[1]/'entry/startup.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.decode(data, through='go_vectors')


def decode(data, build_id, owner_seed, *, hps_context, hps_io, atf=None):
    """Decode partial UART reads; only newline-terminated records count."""
    if not isinstance(data, (str, bytes, bytearray, memoryview)):
        raise TypeError('serial bytes or text required')
    raw = data.encode() if isinstance(data, str) else bytes(data)
    result = dict(complete=False, errors=[], records=[], boot=None, startup=None,
                  ready=False, stopped=False, reports=[], key_presses=0, key_releases=0,
                  input_observed=False, core_load_acceptance=False)
    if len(raw) > MAX_BYTES:
        result['errors'].append('USB capture exceeds byte bound')
        return result
    lines = raw.decode(errors='replace').split('\n')
    tail = lines.pop()
    first = next((i for i, line in enumerate(lines) if 'FES_NATIVE_USB' in line), len(lines))
    # Preserve the boot decoder's explicit bare-prompt handling before USB
    # starts. Incomplete USB records still cannot enter the event loop below.
    prefix = raw if first == len(lines) else '\n'.join(lines[:first])+ ('\n' if first else '')
    boot = result['boot'] = decode_native_boot(prefix, build_id, owner_seed, hps_context=hps_context,
                                              hps_io=hps_io, atf=atf, usb_control=True)
    startup = result['startup'] = startup_checkpoints(prefix)
    result['errors'].extend(boot['errors'])
    result['errors'].extend(startup['errors'])
    source = next((row['values'] for row in boot['records'] if row['stage']=='hps_context'), None)
    state = 'ready'
    keys = set()

    def require(condition, message):
        if not condition:
            result['errors'].append(message)

    for number, line in enumerate(lines[first:], first+1):
        line = line.rstrip('\r')
        if not line.strip() or line.strip() == 'NOTICE:':
            continue
        report, stop = REPORT.fullmatch(line), STOP.fullmatch(line)
        if line == READY:
            require(state == 'ready', 'duplicate or out-of-order USB Ready')
            require(boot['complete'] and startup['complete'], 'USB Ready precedes matched boot or CPU startup')
            state = 'reports'
            result['ready'] = not result['errors']
            stage = 'ready'
        elif report:
            require(state == 'reports', 'USB report outside active diagnostic')
            value = bytes.fromhex(report[1])
            usages = [v for v in value[2:] if v]
            valid = all(v > 3 and not 0xe0 <= v <= 0xe7 for v in usages) and len(set(usages)) == len(usages)
            require(valid, 'malformed or rollover USB keyboard report')
            if valid and state == 'reports':
                current = set(usages) | {0xe0+b for b in range(8) if value[0] & 1<<b}
                result['key_presses'] += len(current-keys)
                result['key_releases'] += len(keys-current)
                keys = current
            result['reports'].append(report[1])
            stage = 'report'
        elif stop:
            require(state == 'reports', 'duplicate or out-of-order USB Stop')
            require(source is not None and int(stop[1],16) == source['owner'] and int(stop[2]) == 3,
                    'USB Stop owner/generation differs')
            state = 'pass'
            result['stopped'] = not result['errors']
            stage = 'stop'
        elif line == PASS:
            require(state == 'pass', 'USB Pass lacks checked stop')
            state = 'done'
            stage = 'pass'
        else:
            result['errors'].append(f'failure, malformed or foreign USB evidence at line {number}: {line[:160]}')
            continue
        result['records'].append(dict(stage=stage, line=number))
    result['complete'] = state == 'done' and boot['complete'] and startup['complete'] and not tail and not result['errors']
    result['input_observed'] = bool(result['complete'] and result['key_presses'])
    return result
