"""Decode CPU startup checkpoints, independently of native ownership receipts."""
import re

PHASES = ('loader_begin', 'decode_begin', 'inflate_end', 'checksum_end', 'decode_end', 'copy_end', 'handoff',
          'go_hwinit', 'go_vectors', 'go_main', 'verifier_ready', 'sd_open_begin',
          'sd_open_end', 'fat_mount_end', 'retain_begin', 'retain_end')
PATTERN = re.compile(r'FES_BOOT_STEP phase=(?P<phase>[a-z_]{1,24}) '
                     r'ticks=(?P<ticks>[0-9a-f]{16}) hz=(?P<hz>[0-9a-f]{16}) '
                     r'el=(?P<el>[12]) sctlr=(?P<sctlr>[0-9a-f]{16})')
MAX_BYTES = 2 * 1024 * 1024


def decode(data, through='retain_end'):
    """Retain ordered partial progress. No checkpoint grants boot admission.

    Durations include trace output and intermediate work. Equal ticks are
    permitted at coarse counter frequencies; counter regression, resets and
    changed frequency cannot establish elapsed time.
    """
    if through not in PHASES:
        raise ValueError('unknown final startup checkpoint')
    report = dict(records=[], errors=[], complete=False, rejected=False,
                  last_checkpoint=None, timings_us={}, core_load_acceptance=False)
    raw = data.encode() if isinstance(data, str) else bytes(data)
    if len(raw) > MAX_BYTES:
        report['errors'].append('startup capture exceeds byte bound')
        return report
    expected = PHASES[:PHASES.index(through) + 1]
    for number, line in enumerate(raw.decode(errors='replace').split('\n')[:-1], 1):
        line = line.rstrip('\r')
        if not line.startswith('FES_BOOT_STEP'):
            continue
        match = PATTERN.fullmatch(line)
        if not match:
            report['errors'].append(f'malformed startup checkpoint at line {number}')
            continue
        row = {key: value if key == 'phase' else int(value, 10 if key == 'el' else 16)
               for key, value in match.groupdict().items()}
        phase = row['phase']
        records = report['records']
        index = len(records)
        rejection = phase == 'rejected'
        if (report['rejected'] or
                (rejection and (not records or records[-1]['phase'] not in
                               ('loader_begin', 'decode_begin', 'inflate_end', 'checksum_end', 'decode_end'))) or
                (not rejection and (index >= len(expected) or phase != expected[index]))):
            report['errors'].append(f'out-of-order or duplicate startup checkpoint at line {number}')
        state = 0 if phase in ('loader_begin', 'handoff', 'rejected') else 0x1005
        level = 2 if phase in PHASES[:7] or rejection else 1
        if row['sctlr'] & 0x1005 != state or row['el'] != level:
            report['errors'].append(f'wrong startup CPU/cache state at line {number}')
        if not row['hz'] or (records and (row['hz'] != records[0]['hz'] or row['ticks'] < records[-1]['ticks'])):
            report['errors'].append(f'incoherent startup counter at line {number}')
        records.append(dict(line=number, **row))
        report['rejected'] |= rejection
        if len(records) > len(PHASES) + 1:
            report['errors'].append('too many startup checkpoints')
            break
    records = report['records']
    report['last_checkpoint'] = records[-1]['phase'] if records else None
    if records and not report['errors']:
        first = records[0]
        report['timings_us'] = {row['phase']: (row['ticks'] - first['ticks']) * 1000000 // first['hz']
                                for row in records}
        report['complete'] = not report['rejected'] and tuple(row['phase'] for row in records) == expected
    return report
