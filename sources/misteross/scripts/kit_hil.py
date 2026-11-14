#!/usr/bin/env python3
"""Guards for kit HIL runs: end of run, the cleanup gate and the run's verdict.

Session HIL drivers are per-run copies of one another (`hil_games.py`,
`hil_x87.py`, `hil_m5g.py` ...). They share three habits that each cost an
operator recovery or a void run on 14 November 2026, so the habits live here
instead, where the test suite holds them:

* `drain` ends a run deterministically. A command file with no final
  `{"op": "quit"}` ends the run instead of idling silently, and a live console
  that does wait says, over and over, exactly what it waits for.
* `cleanup_gate` refuses a post-run tail while the kit still has a core loaded.
  A removal under a loaded core preceded the stop/close that returned HTTP 500
  and left Kit B's lease `blocked` with "operator recovery required".
* `verdict` makes a command error the run's own verdict. A `load` the kit
  refused at admission (HTTP 422 `INVALID_ARCHIVE`) must not leave `rc=0`.

`run_lock` and `lock_holder` answer "is my driver still alive?" without
`pgrep -f`, which matches the asking shell's own command line.

The CLI serves the shell wrappers:

    kit_hil.py guard-idle --config PRIVATE.toml     # before any post-run tail
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


class RunFailed(Exception):
    """A run that must not report success, whatever its frames look like."""


class CleanupRefused(Exception):
    """The kit is not in a state in which a post-run tail may touch it."""


def error_in(value):
    """Return the first error this result reports, at any depth, or None.

    The driver records a refused command as `{'error': ...}` in its event and
    in `result.json`; a nested reply can carry one too.
    """

    if isinstance(value, dict):
        reported = value.get('error')
        if reported not in (None, '', {}, []):
            return reported
        for nested in value.values():
            found = error_in(nested)
            if found is not None:
                return found
        return None
    if isinstance(value, list):
        for nested in value:
            found = error_in(nested)
            if found is not None:
                return found
    return None


def halting(result):
    """Return why this command's result must end the run, or None.

    An error is one reason. A `recovery` is the other: a `stop` that answers
    `{"recovery": "reboot_required"}` reports no error, and the drivers break
    their loop on it through their own `halted` key. Returning it here means an
    adopter needs no second mechanism for it.
    """

    found = error_in(result)
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
    halt in `halted`, a failed release in `close_error`.
    """

    reasons = []
    for key in ('halted', 'close_error'):
        if result.get(key):
            reasons.append(f'{key}: {result[key]}')
    for event in result.get('events', []):
        if not isinstance(event, dict):
            continue
        found = error_in(event.get('result'))
        if found is None:
            found = error_in({k: v for k, v in event.items() if k != 'result'})
        if found is not None:
            seq = event.get('seq', '?')
            op = (event.get('cmd') or {}).get('op') if isinstance(event.get('cmd'), dict) else event.get('op')
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
        op = json.loads(line)
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


def loaded_core(status):
    """Name what the agent has loaded, or None when it has nothing."""

    package = status.get('core_package')
    if isinstance(package, dict) and (package.get('package_id') or package.get('core_id')):
        return ' '.join(str(package[key]) for key in ('core_id', 'package_id') if package.get(key))
    if status.get('state') not in (None, 'idle'):
        return str(status.get('game_id') or status.get('observed_core') or status.get('state'))
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
        core = loaded_core(status)
        state, lease_state = status.get('state'), lease.get('state')
        recovery = status.get('recovery') or ''
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
        reasons = verdict(result if isinstance(result, dict) else {})
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
