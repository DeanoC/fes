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
_BANNER = re.compile(rb'(?m)^U-Boot SPL [^\r\n]*-fes-nano-([0-9a-f]{16}) [^\r\n]*\r?\n')


def split_capture(raw, build_id):
    """Separate an earlier boot's UART tail from one complete SPL beginning."""
    if not isinstance(raw, bytes) or len(raw) > MAX_BYTES or not isinstance(build_id, str) or not re.fullmatch('[0-9a-f]{16}', build_id):
        raise ValueError('bounded capture and exact SPL build ID required')
    banners = list(_BANNER.finditer(raw))
    beginnings = list(_BEGIN.finditer(raw))
    if (len(banners) != 1 or len(beginnings) != 1 or banners[0][1].decode('ascii') != build_id or
            beginnings[0].start() != banners[0].end()):
        raise ValueError('expected one matching SPL banner immediately before BEGIN')
    offset = banners[0].start()
    return raw[:offset], raw[offset:]


def observe(raw, *, build_id, cycle_confirmed, native_boot):
    """Decode the exact matching SPL segment; pending is not a boot failure."""
    if (not isinstance(raw, bytes) or len(raw) > MAX_BYTES or type(cycle_confirmed) is not bool or
            not isinstance(build_id, str) or not re.fullmatch('[0-9a-f]{16}', build_id) or
            not callable(native_boot)):
        raise ValueError('bounded trace, exact SPL build ID, cycle confirmation and native decoder required')
    result = dict(format=1, phase='native-power-on', trace_sha256=hashlib.sha256(raw).hexdigest(),
                  boot_started=False, failed=False, power_on_boot_qualified=False,
                  status='pending-power-cycle', errors=[])
    if not cycle_confirmed:
        return result

    # Split and decode one source segment here. Accepting a caller-supplied
    # decoder result would let a stale good report accompany unrelated bytes.
    _, boot_segment = split_capture(raw, build_id)
    native_report = native_boot(boot_segment)
    if (not isinstance(native_report, dict) or type(native_report.get('loader_ready')) is not bool or
            not isinstance(native_report.get('errors'), list)):
        raise ValueError('native decoder returned an incomplete report')

    matches = list(_BEGIN.finditer(boot_segment))
    positive_por = len(matches) == 1 and matches[0].groups()[1:] == (b'0', b'1')
    if len(matches) != 1:
        result['errors'].append('exactly one complete SPL beginning required')
    elif not positive_por:
        result['errors'].append('positive POR boot kind required; JTAG/reset boot is not power-on proof')
    # native_trace checks register containment, paired ownership, secure handoff,
    # selected build/package and device context. It receives these same bytes.
    if not native_report['loader_ready'] or native_report['errors'] or len(_HANDOFF.findall(boot_segment)) != 1:
        result['errors'].append('complete matched native loader proof required')
    result.update(boot_started=positive_por, failed=bool(result['errors']),
                  status='failed' if result['errors'] else 'passed',
                  power_on_boot_qualified=not result['errors'])
    return result
