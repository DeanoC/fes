"""Bounded read-only settings snapshot in a freshly admitted recovery session.

This grants no boot/card admission or write authority. The caller supplies and
retires the existing UploadSession. Missing settings are reported, never seeded
or replaced implicitly; an existing file can be preserved before a trial.
"""
import json
import re

from service_trace import require
from upload import UploadSession, destination, payload_limit, sha


def snapshot_preferences(session, receipt):
    require(isinstance(session, UploadSession), 'owned recovery session required')
    target = destination('preferences')
    with session._operation(receipt):
        receipt.update(read_only=True, destination=target, present=False,
                       cid_verified=False, activation=False)
        def command(value, timeout=15):
            return session._command(receipt, value, timeout)
        session._admit_card(command, receipt)
        listing = command('fatls mmc 0:1 FES', 30)
        require(len(re.findall(r'(?m)^\s*\d+ file\(s\), \d+ dir\(s\)\s*\r?$', listing)) == 1,
                'complete FAT directory listing required')
        candidates = [line for line in listing.splitlines() if re.search(r'(?i)\bprefs\.json\s*$', line)]
        require(len(candidates) <= 1, 'ambiguous preferences file')
        payload = None
        if candidates:
            match = re.fullmatch(r'\s*([0-9]+)\s+prefs\.json\s*', candidates[0], re.I)
            require(match is not None, 'invalid preferences directory member')
            size = int(match[1])
            require(0 < size <= payload_limit('preferences'), 'preferences length exceeds bound')
            # An explicit load count contains even a changed directory entry.
            output = command('fatload mmc 0:1 83000000 '+target+' 10000', 60)
            require(session._byte_count(output, size, 'read'), 'preferences read length changed')
            session.memory.loaded(0x83000000, size)
            data = bytearray()
            for offset in range(0, size, 1024):
                count = min(1024, size-offset)
                output = command(f'md.b {0x83000000+offset:x} {count:x}', 15)
                rows = re.findall(r'(?m)^([0-9a-fA-F]{8}): ((?:[0-9a-fA-F]{2} ){1,16})[^\r\n]*\r?$', output)
                for address, values in rows:
                    chunk = bytes.fromhex(values)
                    require(int(address,16) == 0x83000000+len(data) and
                            0 < len(chunk) <= min(16, size-len(data)),
                            'preferences memory dump address/length changed')
                    data.extend(chunk)
                require(len(data) == offset+count, 'preferences memory dump incomplete')
            payload = bytes(data)
            def invalid_constant(_):
                raise ValueError('nonfinite preferences value')
            require(type(json.loads(payload.decode('utf-8'), parse_constant=invalid_constant)) is dict,
                    'preferences must be a JSON object')
            session._digest(command, 0x83000000, size, sha(payload))
            receipt.update(present=True, payload_bytes=size, payload_sha256=sha(payload))
        session._protected_files(command, 0x82000000)
        session._observe_memory(command, receipt, 'complete')
        receipt.update(status='verified', fresh_readback=True,
                       boot_files_unchanged=True, recovery_manifest_unchanged=True)
        return payload
