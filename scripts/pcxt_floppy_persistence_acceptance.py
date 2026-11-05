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


class SourceCleanupError(pa.AcceptanceError):
    # Callers write this into failed-phase evidence; wrappers must not turn an
    # unknown source subprocess into an ordinary, safely handed-back failure.
    source_cleanup_incomplete = True


def bounded_source_command(argv, deadline, *, cwd=None, maximum_seconds=20, safe_python=False, cache_prefix=None):
    """Bound and reap an owned Git/GitHub process group without pipe hangs."""
    if app._OBSERVER_GUARD_FD is None:
        return _bounded_source_command(argv, deadline, cwd=cwd, maximum_seconds=maximum_seconds,
                                       safe_python=safe_python, cache_prefix=cache_prefix)
    # The observer can be killed at its hard deadline while a source command
    # is in another process group. A small supervisor survives that death and
    # retains the guard fd until it has killed and reaped the command group.
    parent_pid = os.getpid()
    with tempfile.TemporaryFile() as result_file:
        pid = os.fork()
        if pid == 0:
            parent_dead = [False]
            def cancel(_signum, _frame):
                parent_dead[0] = True
            signal.signal(signal.SIGTERM, cancel)
            libc = app.ctypes.CDLL(None, use_errno=True)
            if libc.prctl(1, signal.SIGTERM, 0, 0, 0) != 0 or os.getppid() != parent_pid:
                os._exit(1)
            os.write(app._OBSERVER_GUARD_FD, b'S')
            cleanup_verified = [True]  # no command has started yet
            try:
                value = _bounded_source_command(argv, deadline, cwd=cwd, maximum_seconds=maximum_seconds,
                    safe_python=safe_python, cache_prefix=cache_prefix, cancelled=lambda: parent_dead[0],
                    started=lambda: cleanup_verified.__setitem__(0, False),
                    cleaned=lambda: cleanup_verified.__setitem__(0, True))
                if parent_dead[0]:
                    raise pa.AcceptanceError('source observer exited')
                if not cleanup_verified[0]:
                    raise SourceCleanupError('source process cleanup was not verified')
                result_file.write(value); result_file.flush()
            except BaseException:
                if cleanup_verified[0]:
                    os.write(app._OBSERVER_GUARD_FD, b'C')
                os._exit(1)
            os.write(app._OBSERVER_GUARD_FD, b'C')
            os._exit(0)
        _, status = os.waitpid(pid, 0)
        if status != 0:
            raise pa.AcceptanceError('source process failed or cleanup was unverified')
        result_file.seek(0)
        return result_file.read(16*1024*1024+1)


def _bounded_source_command(argv, deadline, *, cwd=None, maximum_seconds=20, safe_python=False,
                            cache_prefix=None, cancelled=lambda: False,
                            started=lambda: None, cleaned=lambda: None):
    deadline.remaining(1)
    with tempfile.TemporaryFile() as output:
        environment = dict(os.environ, GIT_NO_REPLACE_OBJECTS='1')
        if safe_python:
            environment.pop('PYTHONPATH',None); environment.pop('PYTHONHOME',None)
            environment['PYTHONNOUSERSITE']='1'
            environment['PYTHONDONTWRITEBYTECODE']='1'
            environment['PYTHONPYCACHEPREFIX']=str(cache_prefix)
        started()
        try:
            proc=subprocess.Popen(argv,cwd=cwd,stdout=output,stderr=subprocess.DEVNULL,start_new_session=True,env=environment)
        except BaseException as exc:
            # Popen can fail after forking but before returning the handle.
            raise SourceCleanupError('source process start cleanup unverified') from exc
        identity_lost = False
        try:
            # Keep the leader unreaped until its entire group is killed. This
            # prevents PID/group reuse when an exited leader leaves children.
            end=time.monotonic()+min(maximum_seconds,deadline.remaining(1))
            while True:
                if cancelled(): raise pa.AcceptanceError('source observer exited')
                try:
                    observed=os.waitid(os.P_PID,proc.pid,os.WEXITED|os.WNOHANG|os.WNOWAIT)
                except OSError as exc:
                    # Without an owned unreaped child, a numeric process group
                    # is no longer a safe signal target.
                    identity_lost = True
                    raise SourceCleanupError('source process ownership unverified; no signal sent') from exc
                if observed is not None:break
                left=min(end-time.monotonic(),deadline.remaining(1))
                if left<=0:raise pa.AcceptanceError('source process deadline expired')
                time.sleep(min(.05,left))
        finally:
            if not identity_lost:
                active_error = sys.exception()
                try:
                    try:os.killpg(proc.pid,signal.SIGKILL)
                    except ProcessLookupError:pass
                    proc.wait(timeout=5)
                    # A deadline or cancellation must not skip group proof.
                    try:os.killpg(proc.pid,0)
                    except ProcessLookupError:pass
                    else:raise RuntimeError('source process group remains')
                    cleaned()
                except BaseException as exc:
                    failure = SourceCleanupError('source process cleanup unverified')
                    if active_error is not None:
                        failure.add_note('source check also failed: '+repr(active_error))
                    raise failure from exc
        if proc.returncode:raise pa.AcceptanceError('source process failed')
        deadline.remaining(1)
        output.seek(0);result=output.read(16*1024*1024+1)
        if len(result)>16*1024*1024:raise pa.AcceptanceError('source process output too large')
        return result


SOURCE_PATHS = ['scripts/pcxt_floppy_persistence_acceptance.py', 'scripts/pcxt_health_evidence.py',
                'scripts/pcxt_dos_application_acceptance.py', 'scripts/pcxt_dos_hil_acceptance.py',
                'scripts/pcxt_digger_readiness.py', 'scripts/pcxt_gameplay_capture.py',
                'scripts/pcxt_gameplay_probe.py', 'scripts/pcxt_game_audio_capture.py',
                'scripts/pcxt_capture_fanout.py', 'scripts/pcxt_v4l2_capture.c',
                'scripts/package_acceptance.py', 'scripts/diagnostic_identity.py', 'scripts/input_diagnostic.py',
                'scripts/pcxt_selected_source.py', 'scripts/bundle.py', 'scripts/recipes.py',
                'scripts/artifact_cache.py', 'scripts/core_dev_accept.py', 'scripts/core_dev.py',
                'scripts/package_acceptance_isolated.py', 'scripts/module_sources.py', 'scripts/optional_cores.py',
                'sources/misteross/scripts/core_package.py', 'sources/misteross/scripts/rfc3986_validator.py',
                'sources/misteross/cores/fes-pcxt/firmware/hardware-dos.json',
                'sources/misteross/cores/fes-pcxt/sim/bios.lock']


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
        for path in SOURCE_PATHS:
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


def require_main_head(args, deadline=None):
    """Bind a committed local diagnostic to an independently built main head."""
    try:
        deadline = deadline or PhaseDeadline(time.monotonic()+21)
        repo, base, producer, tool = (args.source_repository, args.source_base,
                                      args.source_commit, args.source_tool_commit)
        parts = repo.split('/') if isinstance(repo, str) else []
        if (len(parts) != 2 or any(not re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', part)
                                   or part in ('.', '..') for part in parts)
                or base != 'main' or getattr(args, 'source_pr', None) is not None
                or not isinstance(producer, str) or not re.fullmatch(r'[0-9a-f]{40}', producer)
                or not isinstance(tool, str) or not re.fullmatch(r'[0-9a-f]{40}', tool)):
            raise ValueError('invalid main source identity')
        root = Path(__file__).resolve().parent.parent
        current = bounded_source_command(['git', 'rev-parse', 'HEAD'], deadline, cwd=root).decode('ascii').strip()
        if current != tool:
            raise ValueError('local diagnostic commit changed')
        for path in SOURCE_PATHS + ['scripts/pcxt_main_application_acceptance.py']:
            committed = bounded_source_command(['git', 'show', f'{tool}:{path}'], deadline, cwd=root)
            if (root / path).read_bytes() != committed:
                raise ValueError('diagnostic source changed')
        # The visual decoder and BIOS lock must describe the producer too.
        for path in SOURCE_PATHS[-2:]:
            if bounded_source_command(['git', 'show', f'{producer}:{path}'], deadline, cwd=root) != (root / path).read_bytes():
                raise ValueError('producer BIOS inputs differ')
        result = bounded_source_command(['gh', 'api', f'repos/{repo}/commits/main'], deadline)
        value = json.loads(result)
        if not isinstance(value, dict) or value.get('sha') != producer:
            raise ValueError('main head changed')
        deadline.remaining(1)
    except (OSError, ValueError, TypeError, UnicodeError, subprocess.CalledProcessError,
            subprocess.TimeoutExpired, json.JSONDecodeError):
        raise pa.AcceptanceError('cannot verify the requested main head and exact diagnostic source') from None


def require_historical_main(args, deadline=None):
    """Admit one reviewed historical main commit, never the later tip as tested."""
    try:
        deadline = deadline or PhaseDeadline(time.monotonic()+21)
        repo, producer, tool = args.source_repository, args.source_commit, args.source_tool_commit
        tip, diff_sha = args.source_observed_tip, args.source_diff_sha256
        minimum = getattr(args, 'source_last_observed_tip', None) or args.source_minimum_tip
        parts = repo.split('/') if isinstance(repo, str) else []
        if (len(parts) != 2 or any(not re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', part)
                                   or part in ('.', '..') for part in parts)
                or args.source_base != 'main' or getattr(args, 'source_pr', None) is not None
                or getattr(args, 'source_main', False)
                or any(not isinstance(value, str) or not re.fullmatch(r'[0-9a-f]{40}', value)
                       for value in (producer, tool, tip, minimum))
                or producer == tip or not isinstance(diff_sha, str) or not HEX64.fullmatch(diff_sha)):
            raise ValueError('invalid historical source identity')
        root = Path(__file__).resolve().parent.parent
        current = bounded_source_command(['git', 'rev-parse', 'HEAD'], deadline, cwd=root).decode('ascii').strip()
        if current != tool:
            raise ValueError('local diagnostic commit changed')
        for path in SOURCE_PATHS + ['scripts/pcxt_main_application_acceptance.py']:
            committed = bounded_source_command(['git', 'show', f'{tool}:{path}'], deadline, cwd=root)
            if (root / path).read_bytes() != committed:
                raise ValueError('diagnostic source changed')
        for path in SOURCE_PATHS[-2:]:
            if bounded_source_command(['git', 'show', f'{producer}:{path}'], deadline, cwd=root) != (root / path).read_bytes():
                raise ValueError('producer BIOS inputs differ')
        live = json.loads(bounded_source_command(['gh', 'api', f'repos/{repo}/commits/main'], deadline))
        actual_tip = live.get('sha') if isinstance(live, dict) else None
        if not isinstance(actual_tip, str) or not re.fullmatch(r'[0-9a-f]{40}', actual_tip):
            raise ValueError('live main tip is malformed')
        if getattr(args, 'selected_prepared', None) is not None and actual_tip != tip:
            raise ValueError('selected main source is no longer current')
        observations = getattr(args, 'source_tip_observations', None)
        if observations is None:
            observations = []
            args.source_tip_observations = observations
        if not isinstance(observations, list) or len(observations) > 254:
            raise ValueError('source observation history is invalid or full')
        first = {'tip': actual_tip, 'monotonic_ns': time.monotonic_ns(), 'accepted': False}
        observations.append(first)
        # Authenticate the first-parent chain from GitHub itself. A local
        # replace ref or graft must not manufacture main-branch ancestry.
        # Walk far enough to establish the order of every reviewed identity.
        cursor, commit, ancestry = actual_tip, live, [actual_tip]
        for _ in range(32):
            if {producer, tip, minimum}.issubset(ancestry):
                break
            parents = commit.get('parents') if isinstance(commit, dict) else None
            if (not isinstance(parents, list) or not parents or not isinstance(parents[0], dict)
                    or not isinstance(parents[0].get('sha'), str)
                    or not re.fullmatch(r'[0-9a-f]{40}', parents[0]['sha'])):
                raise ValueError('main first-parent chain is unavailable')
            cursor = parents[0]['sha']
            ancestry.append(cursor)
            if {producer, tip, minimum}.issubset(ancestry):
                break
            commit = json.loads(bounded_source_command(['gh', 'api', f'repos/{repo}/commits/{cursor}'], deadline))
            if not isinstance(commit, dict) or commit.get('sha') != cursor:
                raise ValueError('main first-parent commit differs')
        else:
            raise ValueError('reviewed commits are not on bounded main first-parent chain')
        if (producer not in ancestry or tip not in ancestry or minimum not in ancestry
                or ancestry.index(producer) <= ancestry.index(tip)
                or ancestry.index(minimum) > ancestry.index(tip)):
            raise ValueError('live main rolled back or diverged from reviewed history')
        diff = bounded_source_command(['git', 'diff', '--raw', '-z', '--no-abbrev', '--no-renames',
                                       '--no-ext-diff', producer, tip, '--'], deadline, cwd=root)
        if hashlib.sha256(diff).hexdigest() != diff_sha:
            raise ValueError('reviewed source diff changed')
        # Bind the observation across the local ancestry and diff checks.
        live = json.loads(bounded_source_command(['gh', 'api', f'repos/{repo}/commits/main'], deadline))
        final_tip = live.get('sha') if isinstance(live, dict) else None
        if isinstance(final_tip, str) and re.fullmatch(r'[0-9a-f]{40}', final_tip):
            final = {'tip': final_tip, 'monotonic_ns': time.monotonic_ns(), 'accepted': False}
            observations.append(final)
        else:
            raise ValueError('live main tip is malformed')
        if final_tip != actual_tip:
            raise ValueError('observed main tip changed during admission')
        deadline.remaining(1)
        first['accepted'] = final['accepted'] = True
        args.source_last_observed_tip = actual_tip
    except (OSError, ValueError, TypeError, UnicodeError, subprocess.CalledProcessError,
            subprocess.TimeoutExpired, json.JSONDecodeError):
        raise pa.AcceptanceError('cannot verify historical main, reviewed tip and exact diagnostic source') from None


def main_source_evidence(args):
    if getattr(args, 'source_historical_main', False):
        return {'repository': args.source_repository, 'branch': 'main', 'producer_commit': args.source_commit,
                'tool_commit': args.source_tool_commit, 'reviewed_tip': args.source_observed_tip,
                'raw_diff_sha256': args.source_diff_sha256,
                'scope': 'hardware diagnostic tests producer commit only; later changes were not hardware-tested'}
    return {'repository': args.source_repository, 'branch': args.source_base,
            'producer_commit': args.source_commit, 'tool_commit': args.source_tool_commit}


def require_source(args, deadline=None):
    if getattr(args, 'source_historical_main', False):
        require_historical_main(args, deadline)
    elif getattr(args, 'source_main', False):
        if (getattr(args, 'source_observed_tip', None) is not None or getattr(args, 'source_diff_sha256', None) is not None
                or getattr(args, 'source_minimum_tip', None) is not None):
            raise pa.AcceptanceError('historical source arguments require historical mode')
        require_main_head(args, deadline)
    else:
        if (getattr(args, 'source_tool_commit', None) is not None or getattr(args, 'source_observed_tip', None) is not None
                or getattr(args, 'source_diff_sha256', None) is not None or getattr(args, 'source_minimum_tip', None) is not None):
            raise pa.AcceptanceError('tool commit is only valid for main source mode')
        require_pr_head(args, deadline)


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


def previous_receipt(path, expected_sha, args, owned, current_provenance, input_session_id):
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
                not isinstance(sess['id'], str) or not sess['id'] or sess['id'] != owned.session_id or
                not isinstance(sess['flight_id'], str) or not sess['flight_id'] or sess['flight_id'] == owned.flight_id or
                not isinstance(sess.get('input_session_id'), str) or not sess['input_session_id'] or
                not isinstance(input_session_id, str) or not input_session_id or sess['input_session_id'] == input_session_id or
                not isinstance(rec['saved_revision'], str) or not HEX64.fullmatch(rec['saved_revision'])):
            raise ValueError('write receipt does not bind a distinct restart')
        if getattr(args, 'source_historical_main', False):
            observed = rec['source_tip_observations']
            if (not isinstance(observed, list) or not observed
                    or any(not isinstance(item, dict) or set(item) != {'tip', 'monotonic_ns', 'accepted'}
                           or not isinstance(item['tip'], str) or not re.fullmatch(r'[0-9a-f]{40}', item['tip'])
                           or type(item['monotonic_ns']) is not int or item['accepted'] is not True for item in observed)
                    or observed[-1]['tip'] != args.source_minimum_tip):
                raise ValueError('restart does not continue from the write receipt main tip')
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
    require_source(args,PhaseDeadline(args.phase_deadline_monotonic))
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
    require_source(args,PhaseDeadline(args.phase_deadline_monotonic))
    disk = app.verify_disk(args.disk, 'com')
    if disk['floppy_sha256'] != DISK_SHA:
        raise pa.AcceptanceError('unexpected immutable disk image')
    # The known base fixture has FESXT.TXT and no FESSAVE.TXT. A distinct
    # game ID with revision absent is required so this cannot overwrite a save.
    if args.phase == 'write':
        unit_binding(runner.session(), owned, 'absent')
    else:
        prior, digest = previous_receipt(args.write_receipt, args.write_receipt_sha256, args, owned, rec['provenance'], input_session_id)
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
    require_source(args,PhaseDeadline(args.phase_deadline_monotonic))
    if getattr(args, 'source_historical_main', False):
        rec['source_minimum_tip'] = args.source_minimum_tip
        rec['source_tip_observations'] = list(args.source_tip_observations)
    rec['limitations'] = ['physical diagnostic only; not hil-queue acceptance',
                          'caller owns Stop, restart, restoration and kit release',
                          'does not establish whole-board timing or general DOS compatibility']


def check_matching_sources(provenance, source_commit):
    if (provenance['host']['source_commit'] != source_commit or
            provenance['software']['source_commit'] != source_commit):
        raise pa.AcceptanceError('host, software and package source commits differ')


def add_selected_source_arguments(parser):
    parser.add_argument('--selected-prepared', type=Path, help='identity-v2 current-main prepared receipt')
    parser.add_argument('--selected-prepared-sha256', help='pinned prepared receipt digest')
    parser.add_argument('--selected-source-checkout', type=Path, help='clean canonical selected source snapshot')
    parser.add_argument('--producer-source-checkout', type=Path, help='clean canonical original producer snapshot')


def selected_receipt_files(args):
    """Recheck the bytes bound by the complete, earlier resolver proof."""
    prepared = Path(args.selected_prepared)
    raw, digest = pa._read_archive(prepared, maximum=65536)
    if digest != args.selected_prepared_sha256:
        raise ValueError('prepared receipt changed')
    doc = json.loads(raw)
    for key in ('archive', 'selection', 'source_selection'):
        record = doc[key]
        name = record['path']
        if not isinstance(name, str) or not re.fullmatch(r'[a-zA-Z0-9_.-]+', name) or name in ('.', '..'):
            raise ValueError('invalid prepared member')
        _, observed = pa._read_archive(prepared.parent / name,
                                       maximum=65 * 1024 * 1024 if key == 'archive' else 65536)
        if observed != record['sha256']:
            raise ValueError('prepared member changed')
    _, archive_sha = pa._read_archive(args.archive)
    if archive_sha != args.archive_sha256:
        raise ValueError('session archive changed')


def selected_proof_key(args, root):
    names = ('selected_prepared', 'selected_prepared_sha256', 'selected_source_checkout',
             'producer_source_checkout', 'source_repository', 'source_commit', 'source_observed_tip',
             'source_minimum_tip', 'source_tool_commit', 'source_base', 'source_diff_sha256',
             'source_historical_main', 'source_main', 'source_pr', 'archive', 'archive_sha256',
             'package_id', 'rbf_sha256')
    return (str(root), *(getattr(args, name, None) for name in names))


def selected_source_admission(provenance, args, *, root):
    values=[getattr(args,name,None) for name in ('selected_prepared','selected_prepared_sha256',
            'selected_source_checkout','producer_source_checkout')]
    if all(value is None for value in values):
        return False
    if (any(value is None for value in values) or getattr(args,'source_repository',None) != 'DeanoC/fes'
            or not getattr(args,'source_historical_main',False)
            or getattr(args,'source_main',False) or getattr(args,'source_pr',None) is not None
            or not isinstance(args.selected_prepared_sha256,str) or not HEX64.fullmatch(args.selected_prepared_sha256)
            or getattr(args,'source_minimum_tip',None)!=getattr(args,'source_observed_tip',None)):
        raise pa.AcceptanceError('selected package requires complete exact-current historical identity')
    deadline=PhaseDeadline(time.monotonic()+100)
    try:
        # Authenticate the entire imported scripts directory, including helper
        # dependencies, and refuse untracked import candidates.
        if bounded_source_command(['git','status','--porcelain','--untracked-files=normal','--','scripts'],deadline,cwd=root):
            raise ValueError('diagnostic scripts have local changes')
        for checkout,revision in ((args.selected_source_checkout,args.source_observed_tip),
                                  (args.producer_source_checkout,args.source_commit)):
            path=Path(checkout)
            if not path.is_absolute() or path!=path.resolve(strict=True) or not path.is_dir():
                raise ValueError('source snapshot path is not canonical')
            head=bounded_source_command(['git','rev-parse','HEAD'],deadline,cwd=path).decode('ascii').strip()
            status=bounded_source_command(['git','status','--porcelain','--untracked-files=normal'],deadline,cwd=path)
            if head!=revision or status:
                raise ValueError('source snapshot differs from expected clean commit')
        proof = getattr(args, '_selected_source_proof', None)
        if proof is not None and proof['owner_pid'] != os.getpid():
            # The read-only observer inherits proof from the owner. It must
            # still authenticate the exact bytes and clean source checkouts;
            # the cache is evidence of an earlier resolution, not a claim
            # that the shared resolver cache cannot subsequently change.
            if proof['key'] != selected_proof_key(args, root):
                raise ValueError('selected package proof identity changed')
            selected_receipt_files(args)
            result = proof['result']
        else:
            with tempfile.TemporaryDirectory(prefix='pcxt-source-cache-') as cache:
                argv=[sys.executable,'-I','-B','-X','pycache_prefix='+cache,str(root/'scripts/pcxt_selected_source.py'),
                  '--prepared',str(args.selected_prepared),'--prepared-sha256',args.selected_prepared_sha256,
                  '--source-checkout',str(args.selected_source_checkout),'--producer-checkout',str(args.producer_source_checkout),
                  '--archive-sha256',args.archive_sha256,'--package-id',args.package_id,'--rbf-sha256',args.rbf_sha256,
                  '--producer',args.source_commit,'--selected',args.source_observed_tip]
                raw=bounded_source_command(argv,deadline,cwd=root,maximum_seconds=85,safe_python=True,cache_prefix=cache)
            result=json.loads(raw)
            selected_receipt_files(args)
        for checkout,revision in ((args.selected_source_checkout,args.source_observed_tip),
                                  (args.producer_source_checkout,args.source_commit)):
            if (bounded_source_command(['git','rev-parse','HEAD'],deadline,cwd=checkout).decode('ascii').strip()!=revision
                    or bounded_source_command(['git','status','--porcelain','--untracked-files=normal'],deadline,cwd=checkout)):
                raise ValueError('source snapshot changed during verification')
        if (not isinstance(result,dict) or result.get('producer_commit')!=args.source_commit
                or result.get('selected_commit')!=args.source_observed_tip
                or result.get('archive_sha256')!=args.archive_sha256 or result.get('package_id')!=args.package_id
                or result.get('rbf_sha256')!=args.rbf_sha256 or result.get('prepared_sha256')!=args.selected_prepared_sha256):
            raise ValueError('selected package evidence differs')
        if proof is None or proof['owner_pid'] == os.getpid():
            args._selected_source_proof = {'owner_pid': os.getpid(), 'key': selected_proof_key(args, root),
                                           'result': result.copy()}
        provenance['selected_package']=result
        return True
    except (OSError,ValueError,TypeError,KeyError,UnicodeError,subprocess.SubprocessError,json.JSONDecodeError) as exc:
        raise pa.AcceptanceError('selected package source could not be independently verified') from exc


def check_source_admission(provenance, args, *, root=None):
    """Keep same-source default; admit one pinned software source with identical ABI trees.

    The existing provenance check already pins the host binary, both receipts,
    installed bundle and base image. This exception establishes equality of the
    runtime and contract *source trees*, not equality of the complete software
    bundles or proof that the live target is compatible. Normal live package,
    ABI, ownership and session checks remain mandatory.
    """
    root = Path(root) if root is not None else Path(__file__).resolve().parent.parent
    selected = selected_source_admission(provenance,args,root=root)
    companion = getattr(args, 'compatible_software_source', None)
    if companion is None:
        check_matching_sources(provenance, args.source_commit)
        return
    if ((getattr(args, 'source_main', False) is not True and not selected)
            or (getattr(args, 'source_historical_main', False) and not selected)
            or getattr(args, 'source_pr', None) is not None
            or not isinstance(args.source_commit, str) or not re.fullmatch(r'[0-9a-f]{40}', args.source_commit)
            or not isinstance(companion, str) or not re.fullmatch(r'[0-9a-f]{40}', companion)
            or companion == args.source_commit
            or provenance['host']['source_commit'] != args.source_commit
            or provenance['software']['source_commit'] != companion):
        raise pa.AcceptanceError('invalid explicitly pinned companion software source')
    paths = ('sources/libmister-runtime', 'sources/mister-packages')
    root = Path(root) if root is not None else Path(__file__).resolve().parent.parent
    deadline = PhaseDeadline(time.monotonic() + 5)
    def trees(commit):
        raw = bounded_source_command(['git', 'ls-tree', '-z', commit, '--', *paths], deadline, cwd=root)
        rows = {}
        for row in raw.split(b'\0'):
            if not row:
                continue
            match = re.fullmatch(rb'040000 tree ([0-9a-f]{40})\t([^\t\n]+)', row)
            if not match:
                raise ValueError('unexpected source tree entry')
            name = match.group(2).decode('ascii')
            if name not in paths or name in rows:
                raise ValueError('duplicate or unexpected source tree')
            rows[name] = match.group(1).decode('ascii')
        if set(rows) != set(paths):
            raise ValueError('source tree missing')
        return rows
    try:
        producer_trees, software_trees = trees(args.source_commit), trees(companion)
        if producer_trees != software_trees:
            raise ValueError('runtime or ABI contract source differs')
    except (OSError, ValueError, UnicodeError, subprocess.SubprocessError):
        raise pa.AcceptanceError('companion runtime or contract source could not be verified') from None
    provenance['companion_software'] = {
        'policy': 'identical-runtime-and-contract-source-trees-v1',
        'source_commit': companion,
        'trees': producer_trees,
        'scope': 'runtime and contract source equality only; software bundles differ',
    }


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
        p.add_argument('--source-repository', required=True, help='GitHub owner/repository containing the source')
        source_mode = p.add_mutually_exclusive_group(required=True)
        source_mode.add_argument('--source-pr', type=int, help='open pull request number')
        source_mode.add_argument('--source-main', action='store_true', help='require the producer commit to be the live main head')
        source_mode.add_argument('--source-historical-main', action='store_true', help='test a pinned historical main commit only')
        p.add_argument('--source-base', required=True, help='expected PR base branch, or main')
        p.add_argument('--source-tool-commit', help='full diagnostic checkout commit, required in main mode')
        p.add_argument('--source-observed-tip', help='reviewed full live main tip in historical mode')
        p.add_argument('--source-diff-sha256', help='reviewed raw producer-to-tip diff SHA-256 in historical mode')
        p.add_argument('--source-minimum-tip', help='last accepted main tip; write receipt tip on restart')
        p.add_argument('--compatible-software-source', help='explicit software source with identical runtime and contract source trees')
        add_selected_source_arguments(p)
        p.add_argument('--phase-deadline-monotonic', required=True, type=float)
        p.add_argument('--host-pid', required=True, type=int)
        p.add_argument('--write-receipt', type=Path)
        p.add_argument('--write-receipt-sha256')
        return p
    def source(args, font):
        result, host_raw, software_raw = original_source(args, font)
        check_source_admission(result, args)
        if args.source_main or args.source_historical_main:
            result['main_source'] = main_source_evidence(args)
        result['diagnostic_file_sha256']['scripts/pcxt_floppy_persistence_acceptance.py'] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        result['diagnostic_file_sha256']['scripts/pcxt_health_evidence.py'] = hashlib.sha256(Path(health_evidence.__file__).read_bytes()).hexdigest()
        return result, host_raw, software_raw
    def guarded(runner, owned, args, disk_sha):
        return guarded_observation(original_validate_once, runner, owned, args, disk_sha, observer)
    def scenario(runner, owned, args, rec, input_session_id, font):
        try:
            if not math.isfinite(args.phase_deadline_monotonic) or args.phase_deadline_monotonic <= time.monotonic()+1:
                raise pa.AcceptanceError('valid floppy phase deadline required')
            require_source(args, PhaseDeadline(args.phase_deadline_monotonic))
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
        finally:
            if getattr(args, 'source_historical_main', False):
                rec['source_minimum_tip'] = args.source_minimum_tip
                rec['source_tip_observations'] = list(getattr(args, 'source_tip_observations', []))
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
