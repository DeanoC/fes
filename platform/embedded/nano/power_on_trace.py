"""Additional power-on evidence, composed with the complete native boot decoder.

Reset STAT alone is insufficient: its last-POR flag is sticky. Require the
independent SDM boot kind (POR=0) and an operator-confirmed cycle after capture
was armed. This helper neither opens UART/JTAG nor authorizes restoration.
"""
import hashlib
import re

MAX_BYTES = 1 << 20
_BEGIN = re.compile(rb'(?m)^FES_NANO_SPL BEGIN cause=([0-9a-f]{8}) per0=[0-9a-f]{8} bridge=[0-9a-f]{8} cpus=[0-9a-f]{8} release=[0-9a-f]{8} kind=([0-7]) valid=([01])\r?$')
_HANDOFF = re.compile(rb'(?m)^FES_ENTRY EL1 HANDOFF\r?\n')


def observe(raw, *, cycle_confirmed, native_boot):
    """Pending is not failure; POR scope alone is not a complete loader pass."""
    if (not isinstance(raw, bytes) or len(raw) > MAX_BYTES or type(cycle_confirmed) is not bool or
            not isinstance(native_boot, dict) or type(native_boot.get('loader_ready')) is not bool or
            not isinstance(native_boot.get('errors'), list)):
        raise ValueError('bounded trace, cycle confirmation and complete decoder report required')
    result = dict(format=1, phase='native-power-on', trace_sha256=hashlib.sha256(raw).hexdigest(),
                  boot_started=bool(raw.strip()) and cycle_confirmed, failed=False, power_on_boot_qualified=False,
                  status='pending-power-cycle', errors=[])
    if not cycle_confirmed:
        return result
    matches = list(_BEGIN.finditer(raw))
    if len(matches) != 1:
        result['errors'].append('exactly one complete SPL beginning required')
    elif matches[0].groups()[1:] != (b'0', b'1'):
        result['errors'].append('positive POR boot kind required; JTAG/reset boot is not power-on proof')
    # native_trace checks register containment, paired ownership, secure handoff,
    # selected build/package and device context. Never substitute a ready string.
    if not native_boot['loader_ready'] or native_boot['errors'] or len(_HANDOFF.findall(raw)) != 1:
        result['errors'].append('complete matched native loader proof required')
    result.update(boot_started=True, failed=bool(result['errors']),
                  status='failed' if result['errors'] else 'passed',
                  power_on_boot_qualified=not result['errors'])
    return result
