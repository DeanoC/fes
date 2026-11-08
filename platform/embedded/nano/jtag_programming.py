"""One-use live JTAG admission for host bring-up image programming.

This does not load native emulation packages. The caller retains its board
claim, private server, clock policy, subprocess bounds and cleanup duties.
"""
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import threading

CLIENT = '/data/altera_pro/26.3/quartus/linux64/'
LIBRARIES = (CLIENT+'libjtag_client.so', CLIENT+'libjtag_client_dcrt.so')
BINDING = CLIENT+'libjtag_client_dcrt.so'


def _hex(value, length):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{'+str(length)+'}', value) is not None


def _cable(value):
    match = re.fullmatch(r'DE25-Nano on localhost:([0-9]{1,5}) \[3-11\.1-iface0\]', value) if isinstance(value, str) else None
    return match is not None and 1024 <= int(match[1]) <= 65535


def _identity(value):
    fields = {'serial', 'source_commit', 'claim_token', 'pid', 'start_ticks',
              'cable', 'config_sha256', 'clock', 'client_sha256', 'probe_sha256'}
    if not isinstance(value, dict) or set(value) != fields:
        raise ValueError('complete private transport identity required')
    if (value['serial'] != 'TRWU8CBL' or not _hex(value['source_commit'], 40) or
            not _hex(value['claim_token'], 16) or type(value['pid']) is not int or value['pid'] <= 0 or
            not isinstance(value['start_ticks'], str) or not re.fullmatch('[0-9]+', value['start_ticks']) or
            not _cable(value['cable']) or
            not _hex(value['config_sha256'], 64) or value['clock'] != '6M' or
            not _hex(value['probe_sha256'], 64) or not isinstance(value['client_sha256'], dict) or
            set(value['client_sha256']) != set(LIBRARIES) or
            any(not _hex(digest, 64) for digest in value['client_sha256'].values())):
        raise ValueError('private transport identity differs from the qualified top link')
    return value


def live_scan(raw, cable):
    """Require the exact successful vendor-client probe, not a device listing."""
    if not _cable(cable) or not isinstance(raw, bytes) or not 0 < len(raw) <= 65536:
        raise ValueError('bounded live scan transcript required')
    try:
        text = raw.decode('ascii')
    except UnicodeDecodeError as error:
        raise ValueError('non-ASCII live scan transcript') from error
    if not text.endswith('\n') or '\r' in text:
        raise ValueError('incomplete live scan transcript')
    rows = text[:-1].split('\n')
    paths = []
    while rows and rows[0].startswith('FES_JTAG_SCAN LIBRARY '):
        paths.append(rows.pop(0).removeprefix('FES_JTAG_SCAN LIBRARY '))
    expected = [
        'FES_JTAG_SCAN BINDING forced-scan '+BINDING,
        'FES_JTAG_SCAN CALL phase=find rc=0',
        'FES_JTAG_SCAN CALL phase=name rc=0',
        'FES_JTAG_SCAN CABLE '+cable,
        'FES_JTAG_SCAN CALL phase=lock rc=0',
        'FES_JTAG_SCAN CALL phase=clock rc=0',
        'FES_JTAG_SCAN CLOCK 6000000',
        'FES_JTAG_SCAN CALL phase=forced-scan rc=0',
        'FES_JTAG_SCAN CALL phase=cached-after rc=0',
        'FES_JTAG_SCAN DEVICE phase=cached-after count=1 id=4362C0DD mask=00000000 ir=10',
        'FES_JTAG_SCAN CALL phase=unlock rc=0',
        'FES_JTAG_SCAN RESULT pass=1 programming=0 cpu_reset=0',
    ]
    if not 2 <= len(paths) <= 16 or set(paths) != set(LIBRARIES) or rows != expected:
        raise ValueError('live vendor scan is absent, incomplete or failed')
    return hashlib.sha256(raw).hexdigest()


def _persist(path, record):
    with path.open('x') as stream:
        json.dump(record, stream, sort_keys=True)
        stream.write('\n')
        stream.flush()
        os.fsync(stream.fileno())
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


class ProgramAttempt:
    """A fresh scan and durable consumption precede exactly one program call.

    observe() must recheck the live claim/server/config/clock and return their
    complete identity. scan() runs the frozen vendor probe on that exact cable.
    program() retains the existing image validation and process deadline. A
    file shared by this operation prevents replay by a new Python instance.
    """
    def __init__(self, consumed, observe, scan, program):
        self.consumed = Path(consumed)
        self.observe, self.scan, self.program = observe, scan, program
        self.retired = False
        self._lock = threading.Lock()

    def run(self, phase, image_sha256):
        if not self._lock.acquire(blocking=False):
            raise ValueError('programming attempt is active')
        try:
            if self.retired:
                raise ValueError('programming attempt is retired')
            self.retired = True
            if phase not in ('recovery', 'native') or not _hex(image_sha256, 64):
                raise ValueError('fixed programming phase and image digest required')
            # Reserve durably before any scan. A new process cannot replay a
            # failed scan or uncertain attempt by creating another instance.
            _persist(self.consumed, dict(format=1, phase=phase, image_sha256=image_sha256,
                                         state='reserved-before-scan'))
            identity = copy.deepcopy(_identity(self.observe()))
            transcript_sha256 = live_scan(self.scan(identity['cable']), identity['cable'])
            if _identity(self.observe()) != identity:
                raise ValueError('transport changed during the live scan')
            record = dict(format=1, phase=phase, image_sha256=image_sha256,
                          transport=identity, live_scan_sha256=transcript_sha256,
                          state='consumed-before-programming')
            # Keep both immutable records, including after failed persistence
            # or programming. The reservation always prevents another attempt.
            _persist(self.consumed.with_name(self.consumed.name+'.scan.json'), record)
            if _identity(self.observe()) != identity:
                raise ValueError('transport changed before programming')
            return record, self.program(identity['cable'])
        finally:
            self._lock.release()
