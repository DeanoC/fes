#!/usr/bin/env python3
"""Guards for kit HIL runs: end of run, the cleanup gate and the run's verdict.

Session HIL drivers are per-run copies of one another (`hil_games.py`,
`hil_x87.py`, `hil_m5g.py` ...). They share three habits that each cost an
operator recovery or a void run on 14 November 2026, so the habits live here
instead, where the test suite holds them:

* `drain` ends a run deterministically. A command file with no final
  `{"op": "quit"}` ends the run instead of idling silently, and a live console
  that does wait says, over and over, exactly what it waits for.
* `cleanup_gate` reads whether the kit is safe to touch, and `clean` refuses to
  remove anything unless the caller's own lease is live and the kit is idle. A
  removal under a loaded core preceded the stop/close that returned HTTP 500
  and left Kit B's lease `blocked` with "operator recovery required".
* `verdict` makes a command error the run's own verdict. A `load` the kit
  refused at admission (HTTP 422 `INVALID_ARCHIVE`) must not leave `rc=0`.

`run_lock` and `lock_holder` answer "is my driver still alive?" without
`pgrep -f`, which matches the asking shell's own command line.

`clean` removes exactly the trees a run created, inside the lease the run
already holds. It is a library call for the driver, not a CLI command: only a
live renewing process holds a kit lease, so a shell tail after the driver has
exited has no lease to clean under. See the validation record for why the
cleanup no longer manages a lease of its own, and issue #1865 for the
agent-side enforcement that would make these guards unnecessary rather than
merely correct.

The CLI serves the shell wrappers:

    kit_hil.py guard-idle --config PRIVATE.toml     # read the kit's state
    kit_hil.py verdict runs/RUN/result.json         # the run's own verdict
    kit_hil.py alive runs/RUN/kit-hil-run.lock      # is the driver alive?
"""
import argparse
import errno
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import tomllib

sys.path.insert(0, str(Path(__file__).resolve().parent))
import kit as kitlib  # noqa: E402

# A live console waits for the next appended command, but minutes, not the half
# hour that read as a dead wrapper.
IDLE_LIMIT = 300.0
HEARTBEAT = 30.0
# A clean close leaves the lease `revoking` for a few seconds before `free`.
GATE_WAIT = 30.0
GATE_POLL = 1.0
LOCK_NAME = 'kit-hil-run.lock'
# The FogCast data directories a run's own trees appear under. A removal may
# touch nothing else, whatever a run's list says.
DATA_ROOTS = ('/media/fat/fogcast/disk-images', '/media/fat/fogcast/core-data',
              '/media/fat/fogcast/saves')


class RunFailed(Exception):
    """A run that must not report success, whatever its frames look like."""


class CleanupRefused(Exception):
    """The kit is not in a state in which a post-run tail may touch it."""


class MalformedResult(Exception):
    """A result whose shape cannot be judged, so no verdict can be given.

    A guard that can be defeated by a malformed version of what it inspects is
    the failure it exists to catch: plausible-looking success over a failure.
    So an unreadable shape is neither a pass nor a crash — it is the
    no-verdict exit code, the same one an unreadable file gets.
    """


# A result.json is machine-written and shallow. A file nested past this is not
# something to recurse into: the recursion limit would decide the exit code.
MAX_RESULT_DEPTH = 64


def error_in(value, depth=0):
    """Return the first error this result reports, at any depth, or None.

    The driver records a refused command as `{'error': ...}` in its event and
    in `result.json`; a nested reply can carry one too.
    """

    if depth > MAX_RESULT_DEPTH:
        raise MalformedResult(f'the result nests deeper than {MAX_RESULT_DEPTH} levels, '
                              f'so it cannot be searched for errors')
    if isinstance(value, dict):
        reported = value.get('error')
        if reported not in (None, '', {}, []):
            return reported
        for nested in value.values():
            found = error_in(nested, depth + 1)
            if found is not None:
                return found
        return None
    if isinstance(value, list):
        for nested in value:
            found = error_in(nested, depth + 1)
            if found is not None:
                return found
    return None


def result_reasons(result):
    """Return the stored `reasons` as a list of strings, or raise.

    `drain` returns a list of strings and the run stores it. A number, a
    mapping or anything else there means the file was not written by what this
    reads, so its absence of errors proves nothing.
    """

    stored = result.get('reasons')
    if stored is None:
        # A null here is read as "none", unlike a null `events`: a passing run
        # has no reasons, so there is nothing a null could be hiding, while a
        # null `events` means the commands were not recorded at all and an
        # absence of errors in it proves nothing.
        return []
    if isinstance(stored, str):
        return [stored]
    if not isinstance(stored, list) or any(not isinstance(item, str) for item in stored):
        raise MalformedResult(f'`reasons` must be a string or a list of strings, not '
                              f'{type(stored).__name__}'
                              + ('' if not isinstance(stored, list) else
                                 ' of ' + ', '.join(sorted({type(item).__name__
                                                            for item in stored}))))
    return stored


def result_events(result):
    """Return the stored `events` as a list of event objects, or raise.

    The shape that mattered: `{"events": {"1": {...}}}` iterates the mapping's
    keys, every key is a string rather than an event, and a loop that skips
    non-events would report no command error over a result that holds one.
    """

    stored = result.get('events')
    if stored is None and 'events' not in result:
        return []
    if not isinstance(stored, list):
        raise MalformedResult(f'`events` must be a list, not {type(stored).__name__}; '
                              f'a mapping or a null here hides whatever it holds')
    for index, event in enumerate(stored):
        if not isinstance(event, dict):
            raise MalformedResult(f'event {index} is a {type(event).__name__}, not an object, '
                                  f'so what it reports cannot be read')
        command = event.get('cmd')
        if command is not None and not isinstance(command, dict):
            raise MalformedResult(f'event {index} has a `cmd` that is a '
                                  f'{type(command).__name__}, not an object')
    return stored


def halting(result):
    """Return why this command's result must end the run, or None.

    An error is one reason. A `recovery` is the other: a `stop` that answers
    `{"recovery": "reboot_required"}` reports no error, and the drivers break
    their loop on it through their own `halted` key. Returning it here means an
    adopter needs no second mechanism for it.
    """

    try:
        found = error_in(result)
    except MalformedResult as error:
        # A command whose own reply cannot be read is not a command that
        # passed; `drain` turns this into the run's reason.
        return f'unreadable result: {error}'
    if found is not None:
        return found
    if isinstance(result, dict):
        for key in ('recovery', 'halted'):
            value = result.get(key)
            if value:
                return f'{key}: {value}'
            nested = result.get('stop') if key == 'recovery' else None
            if isinstance(nested, dict) and nested.get('recovery'):
                return f'recovery: {nested["recovery"]}'
    return None


def verdict(result):
    """Return the reasons this run failed. Empty means the run passed.

    The invariant the wrappers need: never exit 0 with an error in
    `result.json`. A refused admission is reported in an event's result, a
    halt in `halted`, a failed release in `close_error`, and whatever ended the
    run in `reasons` — `drain`'s own list, which carries the failures no event
    holds: a live run that ran out of commands without a `quit`, or one that
    reached its run limit.

    Anything else the run recorded is searched too, at any depth: the
    invariant is that no error in `result.json` can pass, and a verdict that
    read only the events would be narrower than the `grep '"error"'` it
    replaced.

    Raises `MalformedResult` for any shape it cannot judge, which the CLI
    turns into the no-verdict exit code. Reporting no error over a result
    whose errors are unreadable would be the same plausible success this guard
    exists to prevent.
    """

    if not isinstance(result, dict):
        raise MalformedResult(f'a result must be an object, not {type(result).__name__}')
    reasons = []
    for reason in result_reasons(result):
        if reason:
            reasons.append(f'reasons: {reason}')
    for key in ('halted', 'close_error'):
        if result.get(key):
            reasons.append(f'{key}: {result[key]}')
    # Everything else the run recorded, at any depth: a setup failure, a
    # staging reply, an outer-run error the driver wrote beside its events.
    # The invariant is that no error in result.json can pass, and the shell
    # check this replaced was a plain `grep '"error"'` over the whole file —
    # so inspecting only the events would be narrower than what it replaced.
    # `events` is excluded because each one is reported below with its own
    # sequence number and op.
    HANDLED = ('events', 'reasons', 'halted', 'close_error')
    outer = error_in({key: value for key, value in result.items() if key not in HANDLED})
    if outer is not None:
        reasons.append(f'result: {outer}')
    for event in result_events(result):
        found = error_in(event.get('result'))
        if found is None:
            found = error_in({k: v for k, v in event.items() if k != 'result'})
        if found is not None:
            seq = event.get('seq', '?')
            op = (event.get('cmd') or {}).get('op') or event.get('op')
            reasons.append(f'command {seq} {op}: {found}')
    return reasons


def split_commands(text, *, complete):
    """Return the command lines this file offers now.

    A live file is appended to while the run reads it, so a last line with no
    newline is still being written and is not offered. A complete file's last
    line is whole however it ends.
    """

    if complete:
        return [line for line in text.split('\n')]
    head, _, _ = text.rpartition('\n')
    return head.split('\n') if head or '\n' in text else []


def drain(source, execute, *, mode='batch', idle_limit=IDLE_LIMIT, heartbeat=HEARTBEAT,
          log=print, describe=None, now=time.monotonic, sleep=time.sleep, run_limit=None):
    """Run the commands `source()` offers and return (done, reasons).

    `mode='batch'`: the command file is complete, so running out of commands
    ends the run. A missing final `quit` costs nothing.

    `mode='live'`: commands are appended while the run watches frames, so
    running out of them waits — for `idle_limit` seconds, not indefinitely,
    saying every `heartbeat` seconds what it is waiting for and that the file
    has no `quit`. That is the one mode in which a wait is the design; it still
    ends by itself.

    A command whose result carries an error ends the run here, through the
    caller's own stop and release: `reasons` is how the run reports it, so the
    remaining commands never run over a core that was never loaded. That holds
    in live mode too, so a mistyped op on a console ends that run: a refused
    command is the moment to stop and look rather than keep driving a kit, and
    the run ends through the ordinary stop and release either way.
    """

    if mode not in ('batch', 'live'):
        raise ValueError('mode must be batch or live')
    done, reasons = 0, []
    began = now()
    idle_since, spoke = None, None
    while True:
        lines = split_commands(source(), complete=mode == 'batch')
        if done >= len(lines):
            if mode == 'batch':
                log('end of run: the command file is exhausted'
                    + ('' if not done else f' after {done} commands')
                    + '; it has no final {"op": "quit"}, which costs nothing in batch mode')
                return done, reasons
            if idle_since is None:
                idle_since, spoke = now(), None
            waited = now() - idle_since
            if waited > idle_limit:
                reasons.append(f'no command for {idle_limit:g}s and the command file has no '
                               f'final {{"op": "quit"}}; ending the run after {done} commands')
                log(f'END OF RUN: {reasons[-1]}')
                return done, reasons
            if spoke is None or now() - spoke >= heartbeat:
                spoke = now()
                state = describe() if describe else {}
                log(f'WAITING for command {done + 1}: the command file has {done} commands and no '
                    f'final {{"op": "quit"}}. The driver is alive and holds the kit; it ends this '
                    f'run by itself in {idle_limit - waited:.0f}s. Append more commands, or '
                    f'{{"op": "quit"}} to end it now.'
                    + (' ' + json.dumps(state) if state else ''))
            if run_limit is not None and now() - began > run_limit:
                reasons.append(f'run time limit {run_limit:g}s reached after {done} commands')
                log(f'END OF RUN: {reasons[-1]}')
                return done, reasons
            sleep(min(0.5, max(0.0, idle_limit - waited)))
            continue
        line = lines[done]
        done += 1
        idle_since = None
        if not line.strip():
            continue
        try:
            op = json.loads(line)
            if not isinstance(op, dict):
                raise ValueError(f'a command must be an object, not {type(op).__name__}')
        except ValueError as error:
            # A command file written by hand can hold a malformed line. It is
            # this run's failure like any other, not an exception out of a
            # function that promises (done, reasons).
            reasons.append(f'command {done} is not a command: {error}')
            log(f'END OF RUN: {reasons[-1]}')
            return done, reasons
        if op.get('op') == 'quit':
            log(f'end of run: quit after {done - 1} commands')
            return done, reasons
        try:
            result = execute(op)
        except (Exception, SystemExit) as error:  # noqa: BLE001 - every failure is the run's
            # The ops raise: `call` turns an HTTP 422 into a KitError, and an
            # unknown command raises SystemExit. A driver may hand its own
            # `run` straight in, so catch here rather than require a wrapper.
            result = {'error': f'{type(error).__name__}: {error}'}
        found = halting(result)
        if found is not None:
            reasons.append(f'command {done} {op.get("op")}: {found}')
            log(f'END OF RUN: {reasons[-1]} - the rest of the command file is NOT run; '
                f'a refused command means a failed run')
            return done, reasons
        if run_limit is not None and now() - began > run_limit:
            reasons.append(f'run time limit {run_limit:g}s reached after {done} commands')
            log(f'END OF RUN: {reasons[-1]}')
            return done, reasons


def recognised_idle(status):
    """Raise unless this is a status we understand that says nothing is running.

    "Nothing is loaded" must not be inferred from failing to find evidence of
    a core. `protocol.Status` always serialises `state` and `game_id` (neither
    is `omitempty`) and omits `core_package` only when there is none, so a
    payload without those two keys is not the status this reads — and treating
    it as idle would be a check that silently cannot fail. A non-null
    `game_id` says something is running even if the core's own field were ever
    renamed.
    """

    if not isinstance(status, dict) or 'state' not in status or 'game_id' not in status:
        raise CleanupRefused('the agent\'s status is not a shape this understands (it has no '
                             '`state` and `game_id`), so whether anything is loaded cannot be '
                             'read from it')


def loaded_core(status):
    """Name what the agent has loaded, or None when it has nothing.

    Three independent signals, so no single field moving can turn a loaded kit
    into an idle-looking one: the core package's own identity, a non-null
    `game_id`, and a state that is not idle. `recognised_idle` has already
    established that this is a status whose absence of them means something.
    """

    package = status.get('core_package')
    if isinstance(package, dict) and (package.get('package_id') or package.get('core_id')):
        return ' '.join(str(package[key]) for key in ('core_id', 'package_id') if package.get(key))
    if status.get('game_id') is not None:
        return f'game {status["game_id"]}'
    if status.get('state') not in (None, 'idle'):
        return str(status.get('observed_core') or status.get('state'))
    return None


def cleanup_gate(client, *, wait=GATE_WAIT, poll=GATE_POLL, now=time.monotonic, sleep=time.sleep):
    """Return the kit's state, or refuse: nothing may be removed under a core.

    Fails closed. An unreachable agent, a lease that is not free, an agent that
    is not idle, a loaded core and a pending recovery all refuse, because each
    of them means a removal would race the runtime or touch a kit that already
    needs attention. A lease reads `revoking` for a few seconds after a clean
    close, so that one is waited out. An idle agent carries no `core_package`:
    a Stop sets a fresh `protocol.Status{State: StateIdle}`
    (`sources/FogCast/internal/agent/coordinator.go:508`).
    """

    deadline = now() + wait
    while True:
        try:
            status = client.request('/v1/status')
            lease = client.request('/v1/kit/lease')
        except kitlib.KitError as error:
            raise CleanupRefused(f'the kit agent did not answer, so its state is unknown: {error}') from None
        try:
            recognised_idle(status)
            core = loaded_core(status)
        except CleanupRefused as unreadable:
            if now() >= deadline:
                raise
            core = str(unreadable)
        state, lease_state = status.get('state') if isinstance(status, dict) else None, lease.get('state')
        recovery = (status.get('recovery') or '') if isinstance(status, dict) else ''
        if core is None and not recovery and state == 'idle' and lease_state == 'free':
            return {'state': state, 'lease': lease_state}
        held = {key: lease[key] for key in ('state', 'owner', 'purpose', 'reason') if key in lease}
        if core is not None:
            refusal = (f'the kit has {core} loaded (agent {state}); a removal under a loaded '
                       f'core preceded the stop/close that blocked this lease on 14 November '
                       f'2026. Stop the core first. Lease: {json.dumps(held)}')
        elif recovery:
            refusal = (f'the kit agent reports recovery {recovery!r} (state {state}); it needs that '
                       f'recovery before anything else touches it. Lease: {json.dumps(held)}')
        elif lease_state == 'revoking' and now() < deadline:
            sleep(min(poll, max(0.0, deadline - now())))
            continue
        else:
            refusal = (f'the kit agent is {state} with lease {lease_state}, not idle with a free '
                       f'lease. Lease: {json.dumps(held)}')
        if now() >= deadline:
            raise CleanupRefused(refusal)
        sleep(min(poll, max(0.0, deadline - now())))


class KitShell:
    """The kit's own ssh helper, as the wrappers invoke it."""

    # One command must not be able to outlast the kit's 90 s lease: a slow
    # find or rm that did would leave the removal running unprotected.
    def __init__(self, ssh, timeout=60):
        self.ssh, self.timeout = str(ssh), timeout
        self.absent = []

    def run(self, command):
        try:
            done = subprocess.run([self.ssh, command], stdin=subprocess.DEVNULL,
                                  capture_output=True, text=True, timeout=self.timeout)
        except (OSError, subprocess.SubprocessError) as error:
            raise CleanupRefused(f'the kit shell failed: {type(error).__name__}') from None
        return done.returncode, done.stdout

    def find(self, roots):
        """List every root, or refuse. A root that is absent is empty, not unknown.

        One `find` over all the roots exits nonzero when any single one is
        unreadable while still printing the others, and a partial snapshot is
        what a removal would then certify its diff against. So each root is
        asked for separately: a root that does not exist contributes nothing
        and says so, and a root that exists must list cleanly.
        """

        paths, self.absent = [], []
        for root in roots:
            check_path(root, 'data root')
            # A bare `test -d` cannot tell a root that is absent from a probe
            # that failed: an ssh helper exiting 255, or a directory that
            # cannot be searched, would both read as "absent" and the diff
            # would then be certified over a root never seen. Make the probe
            # say which, and refuse anything that is not one of the two.
            code, out = self.run(f"test -d '{root}' && echo PRESENT || echo ABSENT")
            answer = out.strip().splitlines()[-1].strip() if out.strip() else ''
            if code != 0 or answer not in ('PRESENT', 'ABSENT'):
                raise CleanupRefused(f'the probe for {root} failed (exit {code}, said '
                                     f'{answer or "nothing"!r}), so whether it exists is '
                                     f'unknown; nothing removed')
            if answer == 'ABSENT':
                self.absent.append(root)
                continue
            code, out = self.run(f"find '{root}' | sort")
            if code != 0:
                raise CleanupRefused(f'listing {root} failed, so what is under it '
                                     f'is unknown; nothing removed')
            paths.extend(line for line in out.splitlines() if line)
        return sorted(paths)

    def remove(self, path):
        code, _ = self.run(f"rm -r -- '{path}'")
        if code != 0:
            raise CleanupRefused(f'rm -r {path} failed; nothing further removed')


def check_path(path, what='path'):
    """Raise unless this is a plain absolute path safe to quote into a shell."""

    if not isinstance(path, str) or path != path.strip() or not path.startswith('/'):
        raise CleanupRefused(f'{path!r} is not a plain absolute {what}')
    if any(bad in path for bad in ("'", '"', '\\', '\n', '\r', '$', '`', '*', '?', ';', '|', '&')):
        raise CleanupRefused(f'{path!r} is not a safe {what} to pass to a shell')
    if '/../' in path or path.endswith('/..') or '//' in path:
        raise CleanupRefused(f'{path!r} is not a normalised {what}')


def check_removable(listed, created, *, roots=DATA_ROOTS, keep=()):
    """Raise unless every listed tree is this run's own and inside the roots.

    The wrappers build their list from a `find` diff, so a bad list is the
    thing to catch: a path the run did not create, a path outside FogCast's
    data directories, a shared artefact, a root itself, or a path that could
    not survive the shell quoting a removal needs.
    """

    if not listed:
        raise CleanupRefused('the run lists no trees to remove')
    for root in roots:
        check_path(root, 'data root')
    allowed = tuple(root.rstrip('/') + '/' for root in roots)
    for path in listed:
        check_path(path)
        if not path.startswith(allowed):
            raise CleanupRefused(f'{path!r} is outside {", ".join(roots)}')
        if path.rstrip('/') in tuple(root.rstrip('/') for root in roots):
            raise CleanupRefused(f'{path!r} is a data root, not a run\'s tree')
        if any(item and item in path for item in keep):
            raise CleanupRefused(f'{path!r} is kept and must not be removed')
        if path not in created:
            raise CleanupRefused(f'{path!r} is not in this run\'s own new paths')


def require_caller_lease(client, session):
    """Raise unless the caller's own lease is live, ours, and over an idle kit.

    `clean` claims, renews and releases nothing. The run that created the data
    holds the lease for its whole life and the cleanup happens inside that
    held session, so there is no second interval for a lease to lapse across.
    What is left is to verify the caller really holds it, from both sides: the
    local session (token, renewal, deadline) and the agent's own view of the
    lease (held, and the generation this session was granted).

    An earlier version of this module claimed a lease of its own here. Five
    review rounds on PR #1850 all asked one question — is the lease held for
    the whole interval, and does the exit code say so — and two of their
    findings were introduced by earlier fixes in the same series. A path that
    orchestrates ssh commands which can outrun a lease is the wrong place to
    manage one, so it no longer does; see the validation record and issue
    #1865.
    """

    if session is None:
        raise CleanupRefused('a cleanup runs inside the run\'s own held lease; this one was '
                             'given no session, and it will not claim one of its own')
    require_lease(session, 'the cleanup')
    try:
        lease = client.request('/v1/kit/lease')
        status = client.request('/v1/status')
    except kitlib.KitError as error:
        raise CleanupRefused(f'the kit agent did not answer, so the caller\'s lease cannot be '
                             f'confirmed: {error}') from None
    generation = lease.get('generation')
    if lease.get('state') != 'held' or generation != session.generation:
        held = {key: lease[key] for key in ('state', 'owner', 'purpose', 'reason') if key in lease}
        raise CleanupRefused(f'the kit lease is not the caller\'s: the agent reports '
                             f'{json.dumps(held)} and generation {generation!r}, the caller holds '
                             f'{session.generation!r}')
    recognised_idle(status)
    core = loaded_core(status)
    recovery = status.get('recovery') or ''
    if core is not None or recovery or status.get('state') != 'idle':
        raise CleanupRefused(
            f'the kit is {status.get("state")}'
            + (f' with {core} loaded' if core else '')
            + (f' reporting recovery {recovery!r}' if recovery else '')
            + '; stop the core before removing the run\'s data. A removal under a loaded core '
              'preceded the stop/close that blocked a lease on 14 November 2026.')
    return {'state': status.get('state'), 'lease': lease.get('state'),
            'generation': session.generation}


def clean(shell, listed, created, *, roots=DATA_ROOTS, keep=(), session=None, client=None,
          apply=False, log=print):
    """Remove exactly the trees a run created, inside the run's own lease.

    The caller holds the lease; this function verifies it and never touches
    lease state. Call it from the driver that did the run, before it closes
    its session — not from a shell tail after that session has gone, which
    would have no lease to run under.

    The list is checked against what the run created, the caller's lease is
    verified, every tree must still be there, and afterwards the `find` diff
    must be exactly those trees with nothing appeared. `require_lease` runs
    again before each `rm -r` and before the verification listing, because a
    renewal can still fail mid-removal and `kit.Session` only sets a flag.

    `apply` must be set to remove anything; the default reports the plan.
    """

    check_removable(listed, created, roots=roots, keep=keep)
    if client is not None or session is not None:
        log(f'lease: {json.dumps(require_caller_lease(client, session))}')
    else:
        raise CleanupRefused('a cleanup needs the caller\'s session and client to verify the '
                             'lease it runs under')
    require_lease(session, 'the kit listing')
    before = shell.find(roots)
    if getattr(shell, 'absent', None):
        log(f'absent roots (nothing under them): {json.dumps(shell.absent)}')
    missing = [path for path in listed if path not in before]
    if missing:
        raise CleanupRefused(f'{missing[0]!r} is no longer on the kit')
    expected = sorted(path for path in before
                      if any(path == tree or path.startswith(tree + '/') for tree in listed))
    plan = {'trees': len(listed), 'paths': len(expected), 'applied': bool(apply)}
    if not apply:
        log(f'plan (nothing removed): {json.dumps(plan)}')
        return plan
    for path in listed:
        require_lease(session, f'removing {path}')
        shell.remove(path)
        log(f'removed {path}')
    require_lease(session, 'the check that the removal took exactly the listed trees')
    after = shell.find(roots)
    appeared = [path for path in after if path not in before]
    if appeared:
        raise CleanupRefused(f'{len(appeared)} paths appeared during the removal, '
                             f'first {appeared[0]!r}')
    gone = sorted(path for path in before if path not in after)
    if gone != expected:
        raise CleanupRefused(f'the removal took {len(gone)} paths, not the '
                             f'{len(expected)} the listed trees hold')
    log(f'removed {json.dumps(plan)}')
    return plan


def client_for(config_path, target=None):
    """A client for one named target in the private host TOML.

    `kit.configuration` reads only the config's own `selected_target`, so every
    session reaching the other kit writes a private copy of the host TOML with
    that line rewritten. Name the target instead: nothing copies the host's
    bearer token to a second file to ask one read-only question.
    """

    try:
        with open(config_path, 'rb') as source:
            config = tomllib.load(source)
    except (OSError, ValueError):
        raise kitlib.KitError('the host configuration could not be read') from None
    name = target or config.get('selected_target')
    chosen = [entry for entry in config.get('targets', []) if entry.get('name') == name]
    if len(chosen) != 1:
        raise kitlib.KitError(f'target {name!r} is not uniquely configured')
    if not chosen[0].get('enabled', False):
        raise kitlib.KitError(f'target {name!r} is disabled')
    # A named target wins over the environment. A gate that passes for one kit
    # while the tail removes on another is worse than no gate, so the
    # environment may only fill in what the chosen target does not give.
    address = chosen[0].get('address') or os.environ.get('FOGCAST_BASE_URL', '')
    bearer = chosen[0].get('agent') or os.environ.get('FOGCAST_TOKEN', '')
    if target and not chosen[0].get('address'):
        raise kitlib.KitError(f'target {name!r} gives no address')
    client = kitlib.Client(address, bearer)
    client.target_name = name
    return client


def require_lease(session, step):
    """Raise unless the cleanup's lease is still live. Call before each step.

    A claim excludes other sessions only while it is live. `kit.Session`
    renews in the background and, when a renewal fails, only sets `failed` —
    nothing interrupts the caller — so a cleanup that does not look is a
    cleanup that can keep deleting under a lease it no longer holds. The kit's
    production lease expires after 90 s (`kitlease.New(90*time.Second, ...)`,
    `sources/FogCast/cmd/mister-agent/main.go:511`), which a single slow ssh
    command can outlast. This is `kit.Session.mutate`'s own guard, applied to
    the steps the agent never sees.
    """

    if session is None:
        return
    if (not getattr(session, 'token', None) or session.failed.is_set()
            or time.monotonic() >= session.deadline):
        raise CleanupRefused(f'the cleanup lease is no longer held, so {step} would run '
                             f'unprotected and another session may already have the kit; '
                             f'nothing further is removed')


def run_lock(path):
    """Take this run's lock and record who holds it. Returns the open file.

    The driver holds it for the whole run. Keep the returned file open: closing
    it releases the lock. `lock_holder` reads it without taking it, so "is my
    driver alive?" never has to match a command line.
    """

    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = path.open('a+')
    try:
        fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except OSError as error:
        handle.close()
        if error.errno in (errno.EACCES, errno.EAGAIN):
            holder = lock_holder(path)
            raise RunFailed(f'{path} is already held: {json.dumps(holder)}') from None
        raise
    handle.seek(0)
    handle.truncate()
    handle.write(json.dumps({'pid': os.getpid(), 'cmdline': proc_cmdline(os.getpid()),
                             'started': time.strftime('%F %T %Z')}) + '\n')
    handle.flush()
    return handle


def proc_cmdline(pid):
    """Return a process's command line from /proc, or None if it is gone."""

    try:
        raw = Path(f'/proc/{pid}/cmdline').read_bytes()
    except OSError:
        return None
    return [part for part in raw.decode('utf-8', 'replace').split('\0') if part]


def lock_holder(path):
    """Return the live holder of a run lock, or None when nothing holds it.

    A non-blocking flock attempt, never a `pgrep -f` pattern: `pgrep -f` also
    matches the shell that asks, which is how a live wrapper was read as dead.
    """

    path = Path(path)
    try:
        handle = path.open('r+')
    except OSError:
        return None
    with handle:
        try:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError as error:
            if error.errno not in (errno.EACCES, errno.EAGAIN):
                raise
        else:
            fcntl.flock(handle.fileno(), fcntl.LOCK_UN)
            return None
        try:
            recorded = json.loads(handle.read() or '{}')
        except ValueError:
            recorded = {}
    pid = recorded.get('pid')
    if isinstance(pid, int):
        recorded['cmdline_now'] = proc_cmdline(pid)
    return recorded


def program_pids(program, *, needle=None, pgrep='pgrep'):
    """Return live pids of `program`, never this process or its parents.

    `pgrep -x` matches the executable name only, so the asking shell is not a
    candidate; `/proc/<pid>/cmdline` then confirms each one, and `needle` picks
    out one script among several run by the same interpreter.
    """

    try:
        found = subprocess.run([pgrep, '-x', program], stdin=subprocess.DEVNULL,
                               capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.SubprocessError):
        return []
    mine = {os.getpid(), os.getppid()}
    pids = []
    for field in found.stdout.split():
        try:
            pid = int(field)
        except ValueError:
            continue
        if pid in mine:
            continue
        cmdline = proc_cmdline(pid)
        if cmdline is None:
            continue
        if needle is not None and not any(needle in part for part in cmdline):
            continue
        pids.append(pid)
    return pids


def where(client):
    """Which kit a gate answered about, for a message that must not mislead."""

    if client is None:
        return 'no target'
    return f'{getattr(client, "target_name", None) or "selected"} at {client.url.hostname}'


def parser():
    ask = argparse.ArgumentParser(description=__doc__,
                                  formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = ask.add_subparsers(dest='command', required=True)
    gate = commands.add_parser('guard-idle', help='refuse a post-run tail unless the kit is idle')
    gate.add_argument('--config', type=Path, help='private FogCast host TOML')
    gate.add_argument('--url', help='target agent origin (or FOGCAST_BASE_URL)')
    gate.add_argument('--target', help="the config's target to ask, e.g. dev or kit2 "
                                       "(default: its selected_target)")
    gate.add_argument('--wait', type=float, default=GATE_WAIT,
                      help='seconds to wait out a revoking lease (default %(default)s)')
    said = commands.add_parser('verdict', help="a run's own verdict from its result.json")
    said.add_argument('result', type=Path)
    alive = commands.add_parser('alive', help='is the driver that holds this run lock alive?')
    alive.add_argument('lock', type=Path)
    return ask


def main(argv=None):
    args = parser().parse_args(argv)
    if args.command == 'verdict':
        try:
            result = json.loads(args.result.read_text())
        except (OSError, ValueError) as error:
            print(f'{args.result}: no readable result, so the run has no verdict: {error}',
                  file=sys.stderr)
            return 2
        try:
            reasons = verdict(result)
        except MalformedResult as error:
            print(f'{args.result}: {error}, so the run has no verdict', file=sys.stderr)
            return 2
        for reason in reasons:
            print(f'FAILED {reason}', file=sys.stderr)
        if not reasons:
            print('run reported no command error')
        return 1 if reasons else 0
    if args.command == 'alive':
        holder = lock_holder(args.lock)
        if holder is None:
            print(f'{args.lock}: nothing holds this run lock')
            return 1
        print(json.dumps(holder))
        return 0
    client = None
    try:
        client = (client_for(args.config, args.target) if args.config
                  else kitlib.configuration(args))
        state = cleanup_gate(client, wait=args.wait)
    except CleanupRefused as error:
        print(f'REFUSED ({where(client)}): {error}', file=sys.stderr)
        return 3
    except kitlib.KitError as error:
        print(f'REFUSED ({where(client)}): {error}', file=sys.stderr)
        return 3
    state['target'] = getattr(client, 'target_name', None) or args.target or 'selected'
    state['host'] = client.url.hostname
    print(json.dumps(state))
    return 0


if __name__ == '__main__':
    sys.exit(main())
