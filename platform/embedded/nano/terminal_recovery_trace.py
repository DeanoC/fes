"""Read-only terminal reset telemetry. No event grants recovery or reuse.

The caller retains the original failed verdict and raw UART bytes. A request,
return or later SPL banner cannot replace independently checked fresh boot,
device/reset-bank observations and a new epoch admission.
"""
import hashlib
import re

PARK=b'FES_NANO_INPUT_PARK retained=1 resume=0 activated=0'
PREFIX=b'FES_NANO_RECOVERY_'


def observe(raw, *, build_id):
    if (type(raw) is not bytes or not 0<len(raw)<=1<<20 or b'\0' in raw or
            type(build_id) is not str or re.fullmatch('[0-9a-f]{16}',build_id) is None):
        raise ValueError('bounded raw UART and selected build required')
    lines=raw.splitlines(keepends=True)
    attempt=None;returned=None;park=None;incomplete=False
    for index,row in enumerate(lines):
        line=re.sub(rb'^NOTICE:[ \t]+',b'',row)
        complete=line.endswith(b'\n')
        line=line.rstrip(b'\r\n')
        if line==PARK:
            if not complete or park is not None:
                raise ValueError('one complete terminal Park required')
            park=index
        if PREFIX not in line:
            continue
        if not complete:
            if not line.startswith(PREFIX):
                raise ValueError('malformed partial recovery marker')
            incomplete=True
            continue
        expected=(b'FES_NANO_RECOVERY_ATTEMPT build='+build_id.encode()+
                  b' retained=1 reuse=0 activated=0')
        if line==expected:
            if attempt is not None or park is None or index!=park+1:
                raise ValueError('one recovery attempt immediately after terminal Park required')
            attempt=index
            continue
        match=re.fullmatch(rb'FES_NANO_RECOVERY_RETURN build=([0-9a-f]{16}) error=(-?[0-9]{1,11}) retained=1 resume=0 reuse=0 activated=0',line)
        if (match is None or match[1].decode()!=build_id or attempt is None or
                returned is not None or index!=attempt+1 or not -(1<<31)<=int(match[2])<0):
            raise ValueError('malformed, repeated or foreign recovery return')
        returned=int(match[2])
    return dict(status='observed-terminal-recovery-telemetry',build_id=build_id,
                wire_bytes=len(raw),wire_sha256=hashlib.sha256(raw).hexdigest(),
                terminal_park_seen=park is not None,attempt_observed=attempt is not None,
                entry_returned=returned is not None,return_error=returned,
                incomplete_marker=incomplete,request_publication_verified=False,reset_completed=False,fresh_boot_admitted=False,
                recovery_safe=False,source_reuse_authorized=False,ordinary_resume=False,
                hardware_acceptance=False)
