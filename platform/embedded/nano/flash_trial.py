"""One-use admission for an explicitly approved Nano boot-flash experiment.

Importing this module opens no device. Callers own the claimed private JTAG
server, exact clock/USB/source checks, process deadlines and host cleanup.
Programming and boot acceptance are separate receipts. No failure triggers an
automatic retry, alternative device index, power cycle or factory restoration.
"""
import copy
import hashlib
import json
from pathlib import Path
import re
import struct
import threading

from jtag_programming import ProgramAttempt, _persist, _cable

ARRAY_BYTES = 16 << 20
PART = 'A5EB013BB23B'
FLASH = 'MT25QU128'
ROLES = ('native', 'factory-recovery')
_REVERSE = bytes(int(f'{value:08b}'[::-1], 2) for value in range(256))


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()).hexdigest()


def _hex(value, length):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{'+str(length)+'}', value) is not None


def validate_image(image):
    """Pin the observed vendor JIC envelope and all main-array bytes by SHA256."""
    if not isinstance(image, dict) or set(image) != {'path', 'sha256', 'array_sha256'}:
        raise ValueError('complete pinned JIC identity required')
    if not isinstance(image['path'], str):
        raise ValueError('JIC path must be text')
    path = Path(image['path'])
    if (not path.is_absolute() or '..' in path.parts or path.suffix != '.jic' or
            any(p.is_symlink() for p in (path, *path.parents)) or
            not re.fullmatch(r'[A-Za-z0-9_./-]+', str(path)) or
            not _hex(image['sha256'], 64) or not _hex(image['array_sha256'], 64)):
        raise ValueError('unambiguous absolute JIC path and digests required')
    if not path.is_file() or not ARRAY_BYTES < path.stat().st_size <= ARRAY_BYTES+65536:
        raise ValueError('bounded full-array JIC required')
    raw = path.read_bytes()
    if hashlib.sha256(raw).hexdigest() != image['sha256']:
        raise ValueError('JIC identity changed')
    if raw[:12] != b'JIC\0'+struct.pack('<II', 0, 8):
        raise ValueError('observed eight-section JIC required')
    offset, sections = 12, []
    for expected in (1, 2, 3, 27, 28, 30, 26, 8):
        if len(raw)-offset < 6:
            raise ValueError('short JIC section')
        tag, size = struct.unpack_from('<HI', raw, offset)
        offset += 6
        if tag != expected or size > len(raw)-offset:
            raise ValueError('unexpected JIC section')
        sections.append(raw[offset:offset+size])
        offset += size
    if (offset != len(raw) or sections[1] != (PART+'\0').encode() or
            sections[3] != (FLASH+'\0').encode() or len(sections[-1]) != 2 or
            len(sections[4]) != ARRAY_BYTES+12):
        raise ValueError('JIC target, geometry or envelope differs')
    array = sections[4][12:].translate(_REVERSE)
    if hashlib.sha256(array).hexdigest() != image['array_sha256']:
        raise ValueError('full JIC array identity differs')
    return dict(jic_sha256=image['sha256'], array_sha256=image['array_sha256'],
                bytes=ARRAY_BYTES, part=PART, flash=FLASH,
                outer_checksum_independently_verified=False)


def validate_plan(plan):
    if (not isinstance(plan, dict) or set(plan) != {'format', 'serial', 'source_commit', 'backup_sha256', 'images'} or
            type(plan['format']) is not int or plan['format'] != 1 or plan['serial'] != 'TRWU8CBL' or
            not _hex(plan['source_commit'], 40) or not _hex(plan['backup_sha256'], 64) or
            not isinstance(plan['images'], dict) or set(plan['images']) != set(ROLES)):
        raise ValueError('fixed claimed-kit flash plan required')
    for image in plan['images'].values():
        validate_image(image)
    if (plan['images']['factory-recovery']['array_sha256'] != plan['backup_sha256'] or
            plan['images']['native']['sha256'] == plan['images']['factory-recovery']['sha256']):
        raise ValueError('distinct native image and exact backup restore required')
    return digest(plan)


def validate_approval(approval, plan_sha256, role):
    if (role not in ROLES or not isinstance(approval, dict) or
            set(approval) != {'format', 'source', 'serial', 'plan_sha256', 'roles', 'user_text'} or
            type(approval['format']) is not int or approval['format'] != 1 or
            approval['source'] != 'direct-user-message' or approval['serial'] != 'TRWU8CBL' or
            approval['plan_sha256'] != plan_sha256 or
            not isinstance(approval['roles'], list) or any(not isinstance(value, str) for value in approval['roles']) or
            len(set(approval['roles'])) != len(approval['roles']) or
            any(value not in ROLES for value in approval['roles']) or role not in approval['roles'] or
            not isinstance(approval['user_text'], str) or not approval['user_text'].strip()):
        raise ValueError('explicit user approval for this exact flash plan and role required')
    # The operator records authorization only after a direct human instruction.
    # This data format does not infer consent from a goal, plan or kit claim.
    return digest(approval)


def command(cable, image):
    validate_image(image)
    if not _cable(cable):
        raise ValueError('exact private top cable required')
    return ['-c', cable, '-m', 'jtag', '-o', 'IPV;'+image['path']+';'+PART+'@1']


def programmer_success(result):
    if not isinstance(result, dict) or set(result) != {'returncode', 'output'}:
        raise ValueError('bounded programmer result required')
    raw = result['output']
    if type(result['returncode']) is not int or result['returncode'] != 0 or not isinstance(raw, bytes) or not 0 < len(raw) <= 8 << 20:
        raise ValueError('programmer command failed or output incomplete')
    text = raw.decode('ascii')
    if (not text.endswith('\n') or text.count('Quartus Prime Programmer was successful. 0 errors, 0 warnings') != 1 or
            re.search(r'(?mi)^\s*(?:Error|Critical Warning|Warning)(?:\s|\()', text)):
        raise ValueError('exact program/verify success absent')
    return hashlib.sha256(raw).hexdigest()


class FlashTrial:
    def __init__(self, directory, plan, approval, observe, scan, program):
        self.directory, self.plan, self.approval = Path(directory), copy.deepcopy(plan), copy.deepcopy(approval)
        self.observe, self.scan, self.program = observe, scan, program
        self._lock = threading.Lock()

    def _result(self, role):
        return self.directory/(role+'.result.json')

    def record_boot_failure(self, evidence):
        """No unstarted/pending power-cycle observation can trigger restoration."""
        result = json.loads(self._result('native').read_text())
        if (result.get('pass') is not True or result.get('programmer_reported_success') is not True or
                result.get('plan_sha256') != digest(self.plan) or
                not isinstance(evidence, dict) or set(evidence) != {'format', 'phase', 'trace_sha256', 'boot_started', 'failed'} or
                type(evidence['format']) is not int or evidence['format'] != 1 or evidence['phase'] != 'native-power-on' or
                evidence['boot_started'] is not True or evidence['failed'] is not True or
                not _hex(evidence['trace_sha256'], 64)):
            raise ValueError('observed failed native boot required; pending power cycle is not failure')
        _persist(self.directory/'native-boot-failure.json', dict(evidence, native_result_sha256=digest(result), plan_sha256=digest(self.plan)))

    def _recovery_allowed(self, plan_sha256):
        result = json.loads(self._result('native').read_text())
        if result.get('plan_sha256') != plan_sha256 or result.get('write_attempted') is not True:
            raise ValueError('recovery requires a prior native write attempt')
        if result.get('pass') is False:
            return
        failed = json.loads((self.directory/'native-boot-failure.json').read_text())
        if (failed.get('plan_sha256') != plan_sha256 or failed.get('native_result_sha256') != digest(result) or
                failed.get('boot_started') is not True or failed.get('failed') is not True):
            raise ValueError('source-bound failed native boot required for restoration')

    def run(self, role):
        if not self._lock.acquire(blocking=False):
            raise ValueError('flash trial action already active')
        write_attempted, receipt = False, None
        try:
            # Approval is checked before any device callback or durable action.
            plan_sha256 = digest(self.plan)
            approval_sha256 = validate_approval(self.approval, plan_sha256, role)
            validate_plan(self.plan)
            if role == 'factory-recovery':
                self._recovery_allowed(plan_sha256)
            image = self.plan['images'][role]
            request = dict(format=1, role=role, plan_sha256=plan_sha256, approval_sha256=approval_sha256,
                           image_sha256=image['sha256'], nonvolatile=True, state='reserved')
            _persist(self.directory/(role+'.request.json'), request)
            receipt = dict(request, pass_=False, write_attempted=False, programmer_reported_success=False,
                           power_on_boot_qualified=False, factory_restore_boot_qualified=False)
            receipt['pass'] = receipt.pop('pass_')

            def observed():
                if validate_plan(self.plan) != plan_sha256:
                    raise ValueError('approved flash plan changed')
                identity = self.observe()
                if not isinstance(identity, dict) or identity.get('source_commit') != self.plan['source_commit']:
                    raise ValueError('operator source changed')
                return identity

            def committed(cable):
                nonlocal write_attempted
                args = command(cable, image)
                if validate_approval(self.approval, plan_sha256, role) != approval_sha256:
                    raise ValueError('flash approval changed during admission')
                _persist(self.directory/(role+'.write-started.json'), dict(request, state='consumed-before-nonvolatile-write', arguments=args))
                write_attempted = True
                result = self.program(role, cable, args)
                receipt['programmer_output_sha256'] = programmer_success(result)
                receipt['programmer_reported_success'] = True
                return result

            gate = ProgramAttempt(self.directory/(role+'.live-scan.json'), observed, self.scan, committed)
            grant, _ = gate.run('native' if role == 'native' else 'recovery', image['sha256'])
            receipt['live_grant'] = grant
            if observed() != grant['transport']:
                raise ValueError('transport changed during programming')
            receipt['pass'] = True
            return receipt
        except BaseException as error:
            if receipt is not None:
                receipt['error'] = str(error)
            raise
        finally:
            try:
                if receipt is not None:
                    receipt['write_attempted'] = write_attempted
                    _persist(self._result(role), receipt)
            finally:
                self._lock.release()
