"""Bounded initial loader-call receipts; observations never grant ownership."""
import re

PHASES = (
    'memory-before', 'memory-after', 'snapshot-before',
    'registers-before', 'registers-after',
    *(f'cpu{i}-{edge}' for i in range(4) for edge in ('before', 'after')),
    *(name+'-'+edge for name in ('reset', 'watchdog', 'uart')
      for edge in ('before', 'after')),
    'snapshot-after', 'cpu-check-before', 'cpu-check-after',
    'watchdog-stop-before', 'watchdog-stop-after', 'owned-before', 'owned-after',
)


def state():
    return dict(records=[], valid=True, complete=False,
                firmware_call_may_have_started=False)


def observe(progress, line, number, *, boot_seen, memory_seen, owned_seen,
            later_handoff_seen, prior_valid):
    def invalid():
        progress['valid'] = progress['complete'] = False
        return f'invalid initial loader progress at line {number}'
    if (not prior_valid or not boot_seen or later_handoff_seen or
            not progress['valid'] or progress['complete'] or
            len(progress['records']) >= len(PHASES)):
        return invalid()
    match = re.fullmatch(r'FES_NANO_LOADER phase=([a-z0-9-]+)', line)
    expected = PHASES[len(progress['records'])]
    if (match is None or match[1] != expected or
            memory_seen != (expected != 'memory-before') or
            owned_seen != (expected == 'owned-after')):
        return invalid()
    progress['records'].append(dict(phase=expected, line=number))
    if expected in ('cpu0-before', 'cpu1-before', 'cpu2-before',
                    'cpu3-before', 'owned-before'):
        progress['firmware_call_may_have_started'] = True
    progress['complete'] = len(progress['records']) == len(PHASES)
    return None
