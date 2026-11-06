#!/usr/bin/env python3
"""Exercise a bound PC/XT disk in two *already owned* kit sessions.

The owner retains the kit queue lock and target lease throughout both phases.
This diagnostic never claims a kit, launches/stops a session, or restores it.
It sends one explicit Save in the write phase; a failed/ambiguous operation is
never retried. It is a physical diagnostic, not hil-queue acceptance.
"""
import argparse
import hashlib
import json
import math
import os
import re
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from urllib.parse import urlsplit

if __package__:
    from . import pcxt_dos_application_acceptance as app
    from . import pcxt_health_evidence as health_evidence
else:
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    import pcxt_dos_application_acceptance as app
    import pcxt_health_evidence as health_evidence

pa = app.pa
DISK_SHA = app.SCENARIOS['com']['floppy']
MARKER = 'FES PC/XT DOS BOOT'
GAME_PATTERN = re.compile(r'fpga-pcxt-persist-test-[0-9a-f]{16}-[0-9a-f]{12}\Z')
HEX64 = re.compile(r'[0-9a-f]{64}\Z')
HEALTH_UNSAFE = False


class PhaseDeadline:
    def __init__(self,end): self.end=end
    def remaining(self,reserve=0):
        left=self.end-time.monotonic()-reserve
        if left<=0: raise pa.AcceptanceError('floppy phase source check deadline expired')
        return left


def bounded_source_command(argv, deadline, *, cwd=None):
    """Bound and reap an owned Git/GitHub process group without pipe hangs."""
    deadline.remaining(1)
    with tempfile.TemporaryFile() as output:
        proc=subprocess.Popen(argv,cwd=cwd,stdout=output,stderr=subprocess.DEVNULL,start_new_session=True)
        try:
            # Keep the leader unreaped until its entire group is killed. This
            # prevents PID/group reuse when an exited leader leaves children.
            end=time.monotonic()+min(20,deadline.remaining(1))
            while True:
                observed=os.waitid(os.P_PID,proc.pid,os.WEXITED|os.WNOHANG|os.WNOWAIT)
                if observed is not None:break
                left=min(end-time.monotonic(),deadline.remaining(1))
                if left<=0:raise pa.AcceptanceError('source process deadline expired')
                time.sleep(min(.05,left))
        finally:
            try:os.killpg(proc.pid,signal.SIGKILL)
            except ProcessLookupError:pass
            try:proc.wait(timeout=5)
            except subprocess.TimeoutExpired:raise pa.AcceptanceError('source process cleanup failed') from None
        try:os.killpg(proc.pid,0)
        except ProcessLookupError:pass
        else:raise pa.AcceptanceError('source process group remains; identity unsafe')
        if proc.returncode:raise pa.AcceptanceError('source process failed')
        deadline.remaining(1)
        output.seek(0);result=output.read(16*1024*1024+1)
        if len(result)>16*1024*1024:raise pa.AcceptanceError('source process output too large')
        return result


def require_pr_head(args, deadline=None):
    """Verify an explicitly named open PR and its exact local source checkout."""
    try:
        deadline = deadline or PhaseDeadline(time.monotonic()+21)
        repo, number, base, commit = args.source_repository, args.source_pr, args.source_base, args.source_commit
        components = repo.split('/') if isinstance(repo, str) else []
        if (len(components) != 2 or any(not re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', part) or part in ('.', '..') for part in components)
                or type(number) is not int or number <= 0
                or not isinstance(base, str) or not re.fullmatch(r'[A-Za-z0-9_./-]{1,200}', base)
                or not isinstance(commit, str) or not re.fullmatch(r'[0-9a-f]{40}', commit)):
            raise ValueError('invalid source identity')
        root = Path(__file__).resolve().parent.parent
        current = bounded_source_command(['git', 'rev-parse', 'HEAD'], deadline, cwd=root).decode('ascii').strip()
        if current != commit:
            raise ValueError('local source changed')
        paths = ['scripts/pcxt_floppy_persistence_acceptance.py', 'scripts/pcxt_health_evidence.py',
                 'scripts/pcxt_dos_application_acceptance.py', 'scripts/pcxt_dos_hil_acceptance.py',
                 'scripts/package_acceptance.py', 'scripts/diagnostic_identity.py', 'scripts/input_diagnostic.py',
                 'sources/misteross/scripts/core_package.py', 'sources/misteross/scripts/rfc3986_validator.py',
                 'sources/misteross/cores/fes-pcxt/firmware/hardware-dos.json',
                 'sources/misteross/cores/fes-pcxt/sim/bios.lock']
        for path in paths:
            committed = bounded_source_command(['git', 'show', f'{commit}:{path}'], deadline, cwd=root)
            if (root / path).read_bytes() != committed:
                raise ValueError('diagnostic source changed')
        result = bounded_source_command(['gh', 'api', f'repos/{repo}/pulls/{number}'], deadline)
        value = json.loads(result)
        head = value.get('head') if isinstance(value, dict) else None
        head_repo = head.get('repo') if isinstance(head, dict) else None
        base_value = value.get('base') if isinstance(value, dict) else None
        base_repo = base_value.get('repo') if isinstance(base_value, dict) else None
        if (not isinstance(value, dict) or value.get('state') != 'open' or
                not isinstance(head, dict) or head.get('sha') != commit or
                not isinstance(head_repo, dict) or not isinstance(head_repo.get('full_name'), str) or not head_repo['full_name'] or
                not isinstance(base_value, dict) or base_value.get('ref') != base or
                not isinstance(base_repo, dict) or base_repo.get('full_name') != repo):
            raise ValueError('PR head changed')
        deadline.remaining(1)
    except (OSError, ValueError, TypeError, subprocess.CalledProcessError, subprocess.TimeoutExpired, json.JSONDecodeError):
        raise pa.AcceptanceError('cannot verify the requested open PR and exact local source') from None


def running_private_host(pid, origin, executable):
    """Prove the selected loopback listener is held by this exact binary."""
    ep = urlsplit(origin)
    if ep.scheme != 'http' or ep.hostname != '127.0.0.1' or not ep.port or ep.path not in ('', '/') or ep.query or ep.fragment or ep.username:
        raise pa.AcceptanceError('host origin must be an exact loopback HTTP listener')
    if isinstance(pid, bool) or not isinstance(pid, int) or pid <= 0:
        raise pa.AcceptanceError('invalid private host PID')
    proc = Path('/proc')/str(pid)
    exe = Path(executable)
    try:
        if proc.stat().st_uid != os.getuid():
            raise pa.AcceptanceError('private host has a different owner')
        actual, expected = (proc/'exe').stat(), exe.stat()
        if (actual.st_dev, actual.st_ino) != (expected.st_dev, expected.st_ino):
            raise pa.AcceptanceError('listening host process is not the receipted binary')
        matches = []
        for name in ('tcp', 'tcp6'):
            for line in (Path('/proc/net')/name).read_text().splitlines()[1:]:
                fields = line.split()
                if len(fields) < 10 or fields[3] != '0A':
                    continue
                address, port = fields[1].split(':')
                if int(port,16) == ep.port:
                    matches.append((name, address, fields[9]))
        if len(matches) != 1 or matches[0][:2] != ('tcp', '0100007F'):
            raise pa.AcceptanceError('host port is not a single IPv4 loopback listener')
        sockets = set()
        for fd in (proc/'fd').iterdir():
            try:
                value = os.readlink(fd)
            except OSError:
                continue
            if value.startswith('socket:[') and value.endswith(']'):
                sockets.add(value[8:-1])
        if matches[0][2] not in sockets:
            raise pa.AcceptanceError('selected private host does not own the listening socket')
    except (OSError, ValueError, IndexError):
        raise pa.AcceptanceError('cannot establish private host process and listener identity') from None
    return {'pid': pid, 'listener': origin, 'socket_inode': matches[0][2], 'executable_inode': expected.st_ino}


def unit_binding(session, owned, expected_revision=None):
    p = session.get('core_package', {})
    app.unit(session, 'ready')
    abi = p['abi']
    unit = p['media_units'][0]
    unit_interface = unit['interface']
    if (any(type(abi.get(k)) is not int for k in ('major', 'minor')) or
            any(type(unit.get(k)) is not int for k in ('unit', 'min_bytes', 'max_bytes', 'chunk_bytes')) or
            any(type(unit_interface.get(k)) is not int for k in ('major', 'minor'))):
        raise pa.AcceptanceError('writable media contract uses invalid numeric types')
    interfaces = p.get('active_interfaces', [])
    needed = {'fes.media.pcxt-floppy', 'fes.media.pcxt-floppy-write'}
    actual = {i.get('id') for i in interfaces if isinstance(i, dict) and type(i.get('major')) is int and type(i.get('minor')) is int
              and i.get('major') == 1 and i.get('minor') == 0}
    if not needed.issubset(actual) or p.get('persistence_mode') != 'persistent':
        raise pa.AcceptanceError('active PC/XT media does not advertise a durable writable disk')
    u = p['media_units'][0]
    if 'saveable' in u and u['saveable'] is not True:
        raise pa.AcceptanceError('active disk does not admit save')
    bind = u.get('persistence')
    if (not isinstance(bind, dict) or bind.get('mode') != 'persistent' or
            bind.get('game_id') != owned.game_id or not GAME_PATTERN.fullmatch(owned.game_id) or
            bind.get('base_media_id') != DISK_SHA):
        raise pa.AcceptanceError('disk does not use the isolated expected library binding')
    revision = bind.get('revision')
    if revision != 'absent' and (not isinstance(revision, str) or not HEX64.fullmatch(revision)):
        raise pa.AcceptanceError('disk revision is malformed')
    if expected_revision is not None and revision != expected_revision:
        raise pa.AcceptanceError('disk revision does not match the expected phase')
    return revision


def previous_receipt(path, expected_sha, args, owned, current_provenance):
    raw, digest = pa._read_archive(path, maximum=1024*1024)
    if digest != expected_sha:
        raise pa.AcceptanceError('write-phase receipt hash differs')
    try:
        rec = json.loads(raw)
        sess = rec['session']
        if (rec['classification'] != 'physical diagnostic: saved; restart pending' or
                rec['scenario'] != 'com' or rec['source_commit'] != args.source_commit or
                rec['archive_sha256'] != args.archive_sha256 or rec['package_id'] != args.package_id or
                rec['rbf_sha256'] != args.rbf_sha256 or rec['bundle_id'] != args.bundle_id or
                rec['base_image_sha256'] != args.image_sha256 or rec['target_id'] != args.target_id or
                rec['boot_id'] != args.expected_boot or rec['disk']['floppy_sha256'] != DISK_SHA or
                rec['provenance'] != current_provenance or
                sess['target'] != owned.target_name or sess['target_id'] != owned.target_id or
                sess['game_id'] != owned.game_id or sess['package_id'] != owned.package_id or
                type(sess['generation']) is not int or sess['generation'] == owned.generation or
                not isinstance(sess['id'], str) or not sess['id'] or sess['id'] == owned.session_id or
                not isinstance(sess['flight_id'], str) or not sess['flight_id'] or sess['flight_id'] == owned.flight_id or
                not isinstance(rec['saved_revision'], str) or not HEX64.fullmatch(rec['saved_revision'])):
            raise ValueError('write receipt does not bind a distinct restart')
    except (KeyError, TypeError, ValueError, json.JSONDecodeError):
        raise pa.AcceptanceError('write-phase receipt does not bind this exact restart') from None
    return rec, digest


def usages(text):
    table = {chr(ord('A')+i): 4+i for i in range(26)}
    table.update({' ':44, '.':55, '\n':40})
    try:
        return tuple(table[c] for c in text)
    except KeyError:
        raise ValueError('unsupported DOS command character') from None


def output_proof(lines, command, marker, *, frame=None, anchor=None):
    """Require the typed command, file contents and returned prompt in order."""
    upper = [line.upper().strip() for line in lines]
    for row in range(len(lines) - 2):
        if upper[row] != 'A:\\>' + command or upper[row + 1] != marker:
            continue
        prompt_row = row + 2
        plain = upper[prompt_row] == 'A:\\>'
        cursor = (lines[prompt_row].upper().rstrip() == 'A:\\>?' and
                  underline_cursor(frame, anchor, prompt_row, 4))
        if plain or cursor:
            return {'command_row': row, 'marker': marker, 'marker_row': row + 1,
                    'prompt_row': prompt_row, 'screen': lines}
    return None


def wait_for_output(runner, owned, args, rec, font, anchor, dimensions, command, marker, phase):
    deadline = time.monotonic() + 120
    while True:
        n = len(rec['captures'])
        frame, info = app.capture_owned(runner, owned, args, DISK_SHA,
                                        args.evidence/f'{phase}-{n:03}.png', phase)
        rec['captures'].append(info)
        lines = app.read_anchored(frame, font, anchor, dimensions)
        proof = output_proof(lines, command, marker, frame=frame, anchor=anchor)
        if proof is not None:
            return proof
        if time.monotonic() >= deadline:
            raise pa.AcceptanceError(phase+' did not reach the required command, output and prompt')
        time.sleep(1)


def underline_cursor(frame, anchor, row, column):
    """Require the exact two-scanline cursor in the OCR's question-mark cell."""
    if frame is None or not isinstance(anchor,dict) or not (0<=row<25 and 0<=column<80):
        return False
    try:
        width,height,rgb=frame
        if width<=0 or height<=0 or len(rgb)!=width*height*3:return False
        fg,bg=anchor['foreground_rgb'],anchor['background_rgb']
        dx,dy=anchor['alignment_dx'],anchor['alignment_dy']
        observed=0
        # Match the existing anchored OCR's sampling and color classification.
        for gy in range(8):
            for gx in range(8):
                sx,sy=column*8+gx,row*8+gy
                x=round((160+(sx+.5)*1.5)*width/1280)+dx
                y=round((sy+.5)*height/200)+dy
                if not (0<=x<width and 0<=y<height):return False
                value=app.visual._pixel(rgb,width,x,y)
                df=sum((a-b)**2 for a,b in zip(value,fg))
                db=sum((a-b)**2 for a,b in zip(value,bg))
                observed=(observed<<1)|(df<db)
        return observed==0xffff
    except (KeyError,TypeError,ValueError,IndexError):
        return False


def copy_output_proof(lines, *, frame=None, anchor=None):
    """Require the exact typed command, FreeCOM mapping, and later prompt."""
    upper = [line.upper().strip() for line in lines]
    command = re.compile(r'A:\\>COPY FESXT\.TXT FESSAVE\.TXT')
    mapping = re.compile(r'FESXT\.TXT\s+=>{1,2}\s+FESSAVE\.TXT')
    for command_row, line in enumerate(upper):
        if not command.fullmatch(line):
            continue
        for mapping_row in range(command_row + 1, len(lines)):
            if not mapping.fullmatch(upper[mapping_row]):
                continue
            for prompt_row in range(mapping_row + 1, len(lines)):
                plain = upper[prompt_row] == 'A:\\>'
                # A CGA underline cursor is not a ROM glyph; the anchored OCR
                # emits '?' in its cell. Never accept a literal '?' glyph.
                cursor = (lines[prompt_row].upper().rstrip() == 'A:\\>?' and
                          underline_cursor(frame,anchor,prompt_row,4))
                if plain or cursor:
                    return {'command_row': command_row, 'mapping_row': mapping_row,
                            'prompt_row': prompt_row, 'screen': lines}
    return None


def wait_for_copy(runner, owned, args, rec, font, anchor, dimensions):
    deadline = time.monotonic() + 120
    while True:
        n = len(rec['captures'])
        frame, info = app.capture_owned(runner, owned, args, DISK_SHA,
                                        args.evidence/f'copy-{n:03}.png', 'copy')
        rec['captures'].append(info)
        lines = app.read_anchored(frame, font, anchor, dimensions)
        proof = copy_output_proof(lines,frame=frame,anchor=anchor)
        if proof is not None:
            return proof
        if time.monotonic() >= deadline:
            raise pa.AcceptanceError('copy did not reach the required command, mapping and later prompt')
        time.sleep(1)


def save_response_observation(value, owned):
    """Project bounded, untrusted response evidence before identity validation.

    This observation is not a save receipt and must never authorize restart.
    It intentionally retains no free-form text from the response.
    """
    if not isinstance(value, dict):
        return {'trust': 'unverified', 'response_object': False}
    package = value.get('core_package')
    package = package if isinstance(package, dict) else {}
    units = package.get('media_units')
    try:
        app.unit(value, 'ready')
        unit = units[0]
        abi, interface = package['abi'], unit['interface']
        unit_identity_matches = all(type(v) is int for v in (
            unit['unit'], unit['min_bytes'], unit['max_bytes'], unit['chunk_bytes'],
            abi['major'], abi['minor'], interface['major'], interface['minor']))
    except (pa.AcceptanceError, AttributeError, TypeError, KeyError, IndexError):
        unit_identity_matches = False
    first = units[0] if unit_identity_matches else {}
    binding = first.get('persistence')
    binding = binding if isinstance(binding, dict) else {}
    execution = value.get('execution')
    revision = binding.get('revision')
    result = {
        'trust': 'unverified', 'response_object': True,
        'active': value.get('state') == 'active',
        'execution': execution if execution in ('fpga_native', 'fpga_development') else 'other_or_missing',
        'session_id_matches': value.get('id') == owned.session_id,
        'target_id_matches': value.get('target_id') == owned.target_id,
        'target_matches': value.get('target') == owned.target_name,
        'flight_id_matches': value.get('flight_id') == owned.flight_id,
        'game_id_matches': value.get('game_id') == owned.game_id,
        'package_id_matches': package.get('package_id') == owned.package_id,
        'generation_matches': type(package.get('generation')) is int and package['generation'] == owned.generation,
        'unit_identity_matches': unit_identity_matches,
        'binding_game_matches': binding.get('game_id') == owned.game_id,
        'binding_base_matches': binding.get('base_media_id') == DISK_SHA,
        'revision_kind': 'absent' if revision == 'absent' else
                         'sha256' if isinstance(revision, str) and HEX64.fullmatch(revision) else 'other_or_missing',
    }
    if result['revision_kind'] == 'sha256':
        result['unverified_revision'] = revision
    return result


def save_once(runner, owned, args, rec):
    require_pr_head(args,PhaseDeadline(args.phase_deadline_monotonic))
    app.validate_active(runner, owned, args, DISK_SHA)
    before = runner.session()
    unit_binding(before, owned, 'absent')
    headers = {'Accept':'application/json', 'X-FogCast-Session-ID':owned.session_id,
               'X-FogCast-Package-ID':owned.package_id, 'X-FogCast-Core-Generation':str(owned.generation),
               'X-FogCast-Target':owned.target_name, 'X-FogCast-Target-ID':owned.target_id,
               'X-FogCast-Media-Unit':'0'}
    rec['save_started_monotonic_ns'] = time.monotonic_ns()
    old_timeout = runner.api.timeout
    runner.api.timeout = max(old_timeout, 150)
    try:
        after = runner.api.request('POST', '/api/v1/session/disk/save', b'', headers)
    finally:
        runner.api.timeout = old_timeout
        rec['save_finished_monotonic_ns'] = time.monotonic_ns()
    rec['save_response_observation'] = save_response_observation(after, owned)
    try:
        response_identity = runner.session_identity(after, 'save response')
    except pa.AcceptanceError:
        # The generic runner may include an untrusted observed target in its
        # exception. The bounded observation above contains the useful facts.
        raise pa.AcceptanceError('Save response failed owned session identity validation') from None
    if response_identity != owned:
        raise pa.AcceptanceError('Save response changed the owned session identity')
    revision = unit_binding(after, owned)
    if revision == 'absent':
        raise pa.AcceptanceError('Save did not report a durable revision')
    app.validate_active(runner, owned, args, DISK_SHA)
    if unit_binding(runner.session(), owned, revision) != revision:
        raise pa.AcceptanceError('live session did not retain the saved revision')
    rec['save_response'] = after
    return revision


def run_phase(runner, owned, args, rec, input_session_id, font):
    require_pr_head(args,PhaseDeadline(args.phase_deadline_monotonic))
    disk = app.verify_disk(args.disk, 'com')
    if disk['floppy_sha256'] != DISK_SHA:
        raise pa.AcceptanceError('unexpected immutable disk image')
    # The known base fixture has FESXT.TXT and no FESSAVE.TXT. A distinct
    # game ID with revision absent is required so this cannot overwrite a save.
    if args.phase == 'write':
        unit_binding(runner.session(), owned, 'absent')
    else:
        prior, digest = previous_receipt(args.write_receipt, args.write_receipt_sha256, args, owned, rec['provenance'])
        unit_binding(runner.session(), owned, prior['saved_revision'])
        rec['write_receipt_sha256'] = digest
        rec['saved_revision'] = prior['saved_revision']
    before, info = app.capture_owned(runner, owned, args, DISK_SHA,
                                     args.evidence/'before.png', 'fresh DOS prompt')
    rec['captures'].append(info)
    anchor = app.visual.find_prompt(*before, font)
    lines = app.read_anchored(before, font, anchor, before[:2])
    if any(MARKER in line.upper() or 'FESSAVE.TXT' in line.upper() for line in lines):
        raise pa.AcceptanceError('fresh screen already contains a test marker')
    if args.phase == 'write':
        app.type_keys(runner, owned, args, DISK_SHA, input_session_id, rec['hid_events'], usages('TYPE FESXT.TXT\n'))
        rec['read_only_control'] = wait_for_output(runner, owned, args, rec, font, anchor, before[:2], 'TYPE FESXT.TXT', MARKER, 'control')
        unit_binding(runner.session(), owned, 'absent')
        app.type_keys(runner, owned, args, DISK_SHA, input_session_id, rec['hid_events'], usages('COPY FESXT.TXT FESSAVE.TXT\n'))
        rec['guest_copy'] = wait_for_copy(runner, owned, args, rec, font, anchor, before[:2])
        rec['saved_revision'] = save_once(runner, owned, args, rec)
        rec['classification'] = 'physical diagnostic: saved; restart pending'
    else:
        app.type_keys(runner, owned, args, DISK_SHA, input_session_id, rec['hid_events'], usages('TYPE FESSAVE.TXT\n'))
        rec['restored_file'] = wait_for_output(runner, owned, args, rec, font, anchor, before[:2], 'TYPE FESSAVE.TXT', MARKER, 'restored')
        unit_binding(runner.session(), owned, rec['saved_revision'])
        rec['classification'] = 'physical diagnostic: DOS saved file restored after restart'
    require_pr_head(args,PhaseDeadline(args.phase_deadline_monotonic))
    rec['limitations'] = ['physical diagnostic only; not hil-queue acceptance',
                          'caller owns Stop, restart, restoration and kit release',
                          'does not establish whole-board timing or general DOS compatibility']


def check_matching_sources(provenance, source_commit):
    if (provenance['host']['source_commit'] != source_commit or
            provenance['software']['source_commit'] != source_commit):
        raise pa.AcceptanceError('host, software and package source commits differ')


def guarded_observation(original_validate_once, runner, owned, args, disk_sha, observer=None):
    """Recheck private host and writable binding on both owned session snapshots."""
    def validate_media(foreground, targeted):
        def check():
            foreground_revision = unit_binding(foreground, owned)
            target_revision = unit_binding(targeted, owned)
            if foreground_revision != target_revision:
                raise pa.AcceptanceError('writable disk revision changed between owned session observations')
        if observer is None:
            check()
        else:
            observer.run_stage('media_binding', check)
    if observer is None:
        running_private_host(args.host_pid, args.host_api, args.host_binary)
        return original_validate_once(runner, owned, args, disk_sha, session_guard=validate_media)
    previous = observer.tracing
    try:
        observer.tracing = True
        observer.run_stage('host_guard', lambda: running_private_host(args.host_pid, args.host_api, args.host_binary))
        return health_evidence.traced_app_validation(
            app, observer, lambda: original_validate_once(runner, owned, args, disk_sha, session_guard=validate_media))
    finally:
        observer.tracing = previous


def main(argv=None):
    global HEALTH_UNSAFE
    HEALTH_UNSAFE = False
    original_parser, original_source = app.parser, app.source_provenance
    original_validate_once, original_run_com = app._validate_active_once, app.run_com
    original_runner, original_validate = app.pa.Runner, app.validate_active
    def parser():
        p = original_parser()
        p.add_argument('--phase', required=True, choices=('write', 'verify'))
        p.add_argument('--source-repository', required=True, help='GitHub owner/repository containing the PR head')
        p.add_argument('--source-pr', required=True, type=int, help='open pull request number')
        p.add_argument('--source-base', required=True, help='expected pull request base branch')
        p.add_argument('--phase-deadline-monotonic', required=True, type=float)
        p.add_argument('--host-pid', required=True, type=int)
        p.add_argument('--write-receipt', type=Path)
        p.add_argument('--write-receipt-sha256')
        return p
    def source(args, font):
        result, host_raw, software_raw = original_source(args, font)
        check_matching_sources(result, args.source_commit)
        result['diagnostic_file_sha256']['scripts/pcxt_floppy_persistence_acceptance.py'] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        result['diagnostic_file_sha256']['scripts/pcxt_health_evidence.py'] = hashlib.sha256(Path(health_evidence.__file__).read_bytes()).hexdigest()
        return result, host_raw, software_raw
    def guarded(runner, owned, args, disk_sha):
        return guarded_observation(original_validate_once, runner, owned, args, disk_sha, observer)
    def scenario(runner, owned, args, rec, input_session_id, font):
        if not math.isfinite(args.phase_deadline_monotonic) or args.phase_deadline_monotonic <= time.monotonic()+1:
            raise pa.AcceptanceError('valid floppy phase deadline required')
        require_pr_head(args, PhaseDeadline(args.phase_deadline_monotonic))
        if args.scenario != 'com':
            raise pa.AcceptanceError('only the pinned COM base image is admitted')
        if args.phase == 'write' and (args.write_receipt is not None or args.write_receipt_sha256 is not None):
            raise pa.AcceptanceError('write phase cannot take a prior receipt')
        if args.phase == 'verify' and (args.write_receipt is None or not isinstance(args.write_receipt_sha256,str) or not HEX64.fullmatch(args.write_receipt_sha256)):
            raise pa.AcceptanceError('verify requires a hashed write-phase receipt')
        archive, _ = pa._read_archive(args.archive)
        manifest = app.inspect_archive(archive, args)
        interfaces = manifest.get('interfaces', [])
        for name in ('fes.media.pcxt-floppy', 'fes.media.pcxt-floppy-write'):
            if not any(i.get('id') == name and i.get('major') == 1 and i.get('minor') == 0 and i.get('required') is True for i in interfaces if isinstance(i, dict)):
                raise pa.AcceptanceError('archive lacks the writable PC/XT floppy contract')
        return run_phase(runner, owned, args, rec, input_session_id, font)
    parsed=parser().parse_args(argv)
    observer=health_evidence.HealthEvidence(parsed.evidence,parsed.target_id,parsed.expected_boot,parsed.bundle_id,parsed.image_sha256)
    class ObservedRunner(original_runner):
        def __init__(self,options):
            super().__init__(options)
            observer.install(self.api)
    def observed_validate(*args, **kwargs):
        global HEALTH_UNSAFE
        try:
            # The existing validation forks. Buffer fixed stage events in a
            # bounded pipe and persist them only after the child is reaped.
            observer.begin_trace()
            try:
                return original_validate(*args, **kwargs)
            finally:
                active_error = sys.exception()
                try:
                    observer.end_trace()
                except BaseException:
                    HEALTH_UNSAFE = True
                    if active_error is None:
                        raise
                    active_error.add_note('stage evidence collection failed; claim must be retained')
        except BaseException:
            # A child-local evidence failure cannot update the parent latch.
            HEALTH_UNSAFE=True
            raise
    app.parser, app.source_provenance, app._validate_active_once, app.run_com = parser, source, guarded, scenario
    app.pa.Runner,app.validate_active=ObservedRunner,observed_validate
    try:
        app.install_signal_handler()
        app.main(argv)
    finally:
        app.pa.Runner,app.validate_active=original_runner,original_validate
        app.parser, app.source_provenance, app._validate_active_once, app.run_com = original_parser, original_source, original_validate_once, original_run_com
        active_error=sys.exception()
        try: observer.close()
        except BaseException:
            HEALTH_UNSAFE=True
            if active_error is None: raise
            active_error.add_note('health evidence close failed; claim must be retained')
        if observer.compromised: HEALTH_UNSAFE=True


def cli():
    try:
        main()
    except BaseException as error:
        if HEALTH_UNSAFE: raise SystemExit(74) from error
        if isinstance(error,(pa.AcceptanceError,ValueError,OSError,json.JSONDecodeError)):
            raise SystemExit(str(error)) from error
        raise


if __name__ == '__main__':
    cli()
