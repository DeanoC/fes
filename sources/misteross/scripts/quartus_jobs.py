#!/usr/bin/env python3
"""Run Quartus jobs under the shared lease, here or on whichever build host has
a free slot.

    quartus_jobs.py status [--edition E]
        This host's slots for each edition and who holds them.

    quartus_jobs.py run --edition E [--label L] -- COMMAND...
        Hold a slot of this host's pool for E while COMMAND runs. For a shell
        script, a manual quartus_sh, or a script whose bytes cannot change.

    quartus_jobs.py pick --edition E
        Probe both build hosts and print the one to use: free slots first,
        then the most available memory. Reads /proc/locks, so it contends for
        nothing.

    quartus_jobs.py remote --edition E [--host auto|HOST] [--commit REV] [--label L] -- COMMAND...
        Check a pushed commit out, clean, into a new run directory under
        /data/projects/fes-quartus on the chosen host and run COMMAND there
        from sources/misteross, under `run`. In COMMAND, `{run}` becomes a new run directory on /data and
        `{cache}` that host's toolchain cache. Output goes on /data, never to
        /tmp (Powerboat's /tmp is a shared tmpfs).

`run` holds the slot until every process of the job has exited, including any
left in the background, prints a heartbeat every minute (also written to
$FES_QUARTUS_RUN_DIR/heartbeat), and stops the job at its time cap
(--cap-minutes, default 240; exit 124). Both `run` and `remote` refuse a
filesystem with under 30 GiB free (exit 75). It reports the peak resident set size of the job's
processes, and
writes it to $FES_QUARTUS_RUN_DIR/peak-rss.json when that is set: the figure
the pools in quartus_tools are sized from.

The lease itself lives in scripts/quartus_tools.py; this file is the command
line around it and is not imported by any producer.
"""
from __future__ import annotations

import argparse
import fcntl
from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import re
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from scripts import quartus_tools  # noqa: E402

# Build hosts: the SSH name that reaches each (its hostname is the same), and
# each edition's QUARTUS_ROOTDIR there. Both carry the same two installations.
HOSTS = ("powerboat", "chip-30x0-ai-dev")
QUARTUS_ROOTS = {"lite": "~/intelFPGA_lite/17.0/quartus", "pro": "/data/altera_pro/26.3/quartus"}
# Each host's shared toolchain cache (FES_TOOLCHAIN_CACHE_ROOT there).
TOOLCHAIN_CACHES = {"powerboat": "~/fes/out/cache/misteross-toolchains",
                    "chip-30x0-ai-dev": "~/fes-toolchain-cache"}
# Where both hosts have a free slot, an edition's preferred host: DE25 builds
# go to the 3090 by preference to keep Powerboat free (AGENTS.md).
PREFERRED = {"pro": "chip-30x0-ai-dev"}
PROJECTS = "/data/projects/fes-quartus"
SSH = ("ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", "-o", "ServerAliveInterval=30")

# Run on the probed host, with the slot paths as arguments: which are held and
# how much memory is available. Standard library only, no source tree needed.
PROBE = r"""
import json, os, sys
def key(path):
    try: s = os.stat(path)
    except OSError: return None
    return "%02x:%02x:%d" % (os.major(s.st_dev), os.minor(s.st_dev), s.st_ino)
held = set()
for line in open("/proc/locks"):
    f = line.split()
    if len(f) >= 6 and f[1] == "FLOCK" and f[3] == "WRITE":
        held.add(f[5])
mem = {l.split(":")[0]: int(l.split()[1]) for l in open("/proc/meminfo")}
print(json.dumps({"slots": {p: (key(p) in held) for p in sys.argv[1:]},
                  "available_kib": mem["MemAvailable"], "home": os.path.expanduser("~")}))
"""


def local_host() -> str:
    return socket.gethostname()


def probe(host: str, edition: str, timeout: float = 60) -> dict:
    slots = list(quartus_tools.lease_pool(edition, host))
    command = [sys.executable, "-c", PROBE, *slots] if host == local_host() else \
        [*SSH, host, shlex.join(["python3", "-c", PROBE, *slots])]
    # A host that times out or answers garbage is one failed probe, not a
    # failed pick: the other host can still take the job.
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=max(timeout, 1))
        if result.returncode:
            return {"host": host, "error": (result.stderr or result.stdout).strip()[-300:]}
        data = json.loads(result.stdout)
        if not isinstance(data, dict) or not {"slots", "available_kib", "home"} <= set(data):
            raise ValueError("probe answered without slots, memory and home")
    except (subprocess.TimeoutExpired, OSError, ValueError) as error:
        return {"host": host, "error": f"probe failed: {error}"[:300]}
    data["host"] = host
    data["free"] = sum(not busy for busy in data["slots"].values())
    return data


REPROBE_SECONDS = 60


def is_ready(p: dict, edition: str) -> bool:
    """A probed host can take one edition job now: a free slot and the memory."""
    need = quartus_tools.PEAK_RSS_GIB[quartus_tools.edition_class(edition)] * 1024 * 1024
    return "error" not in p and p["free"] > 0 and p["available_kib"] >= need


def pick(edition: str, timeout: float = 60) -> tuple[str | None, list[dict], bool]:
    """The host to run an edition job on, every probe, and whether that host
    is ready: a free slot and the memory for one job. When none is ready the
    host is where the most memory is, for a caller that waits there."""
    probes = [probe(host, edition, timeout=timeout / len(HOSTS)) for host in HOSTS]
    usable = [p for p in probes if "error" not in p]
    if not usable:
        return None, probes, False
    preferred = PREFERRED.get(quartus_tools.edition_class(edition))

    def ready(p):
        return is_ready(p, edition)

    def rank(p):
        # A free slot below the memory threshold counts for nothing.
        if ready(p):
            return True, p["host"] == preferred, p["free"], p["available_kib"]
        return False, False, 0, p["available_kib"]

    best = max(usable, key=rank)
    return best["host"], probes, ready(best)


def status(edition: str | None) -> int:
    for name in ([edition] if edition else ["lite", "pro"]):
        print(f"{local_host()} {name}:")
        for path in quartus_tools.lease_pool(name):
            holders = sorted(quartus_tools.flock_holders(path))
            described = ", ".join(f"{pid} {_command(pid)}" for pid in holders) or "free"
            print(f"  {path}: {described}")
    return 0


def _command(pid: int) -> str:
    try:
        return Path(f"/proc/{pid}/cmdline").read_bytes().replace(b"\0", b" ").decode(errors="replace")[:120]
    except OSError:
        return "?"


JOB_TOKEN = "FES_QUARTUS_JOB"
DEFAULT_CAP_MINUTES = 240
KILL_GRACE_SECONDS = 60
HEARTBEAT_SECONDS = 60
MIN_FREE_GIB = 30


def _children_of_self() -> set[int]:
    me, found = os.getpid(), set()
    for entry in os.listdir("/proc"):
        if entry.isdigit():
            try:
                if int(Path(f"/proc/{entry}/stat").read_bytes().rsplit(b")", 1)[1].split()[1]) == me:
                    found.add(int(entry))
            except (OSError, IndexError, ValueError):
                continue
    return found


def _set_subreaper(on: bool) -> bool:
    """Make this process the reaper of orphans below it (Linux prctl), so a
    job's process that outlives its parent is reparented here, not to init,
    and stays a descendant. Returns the previous setting."""
    import ctypes
    libc = ctypes.CDLL(None, use_errno=True)
    previous = ctypes.c_int(0)
    libc.prctl(37, ctypes.byref(previous), 0, 0, 0)  # PR_GET_CHILD_SUBREAPER
    if libc.prctl(36, int(on), 0, 0, 0) != 0:  # PR_SET_CHILD_SUBREAPER
        raise OSError(ctypes.get_errno(), "prctl(PR_SET_CHILD_SUBREAPER) failed")
    return bool(previous.value)


def job_processes(token: str, root: int | None = None, adopted_except: set[int] | None = None) -> dict[int, int]:
    """Every live process of one `run` job, with its resident memory (KiB).

    A job's processes are root and everything below it. `run` is a child
    subreaper, so a process orphaned anywhere in the job (left in the
    background, daemonized, or started by a producer with a scrubbed
    environment, as functional_execution does) is reparented to `run`; those
    adopted children are roots too, except the ones in adopted_except that
    were already children before the job. Processes whose environment carries
    the job's token are roots as well, in case one escapes to another reaper."""
    marker = f"{JOB_TOKEN}={token}".encode()
    page_kib = os.sysconf("SC_PAGE_SIZE") // 1024
    parents, rss, tagged = {}, {}, set()
    for entry in os.listdir("/proc"):
        if not entry.isdigit() or int(entry) == os.getpid():
            continue
        pid = int(entry)
        try:
            fields = Path(f"/proc/{entry}/stat").read_bytes().rsplit(b")", 1)[1].split()
            if fields[0] == b"Z":
                continue
            parents[pid] = int(fields[1])
            rss[pid] = int(Path(f"/proc/{entry}/statm").read_text().split()[1]) * page_kib
            if marker in Path(f"/proc/{entry}/environ").read_bytes().split(b"\0"):
                tagged.add(pid)
        except (OSError, IndexError, ValueError):
            continue
    children: dict[int, list[int]] = {}
    for pid, parent in parents.items():
        children.setdefault(parent, []).append(pid)
    adopted = set(children.get(os.getpid(), ())) - (adopted_except or set()) if adopted_except is not None else set()
    found, pending = set(), [*tagged, *adopted, *([root] if root in parents else [])]
    while pending:
        pid = pending.pop()
        if pid in found:
            continue
        found.add(pid)
        pending.extend(children.get(pid, ()))
    return {pid: rss[pid] for pid in found}


def _reap_adopted(keep: set[int]) -> None:
    """Collect the exit status of adopted orphans that have finished."""
    for pid in _children_of_self() - keep:
        try:
            os.waitpid(pid, os.WNOHANG)
        except ChildProcessError:
            pass


def _best_effort_write(path: Path, text: str) -> None:
    """A record write that cannot unwind the wrapper (a full or vanished run
    directory) while the job holds its slot."""
    try:
        path.write_text(text)
    except OSError as error:
        print(f"quartus_jobs: could not write {path}: {error}", file=sys.stderr, flush=True)


def _stop_job(process: subprocess.Popen | None, scan) -> None:
    """SIGTERM every process of the job, SIGKILL after the grace, and wait
    until none is left. Interrupts are ignored until it has finished."""
    if process is None:
        return
    previous = {sig: signal.signal(sig, signal.SIG_IGN) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
    try:
        _signal_all([*_launcher(process), *scan()], signal.SIGTERM)
        deadline = time.monotonic() + KILL_GRACE_SECONDS
        while True:
            if process.poll() is None or scan():
                if time.monotonic() >= deadline:
                    _signal_all([*_launcher(process), *scan()], signal.SIGKILL)
                time.sleep(0.2)
                continue
            return
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def _launcher(process) -> list[int]:
    """The launcher's pid while it is still the launcher. Once it has exited
    (and been waited for) the number may name an unrelated process."""
    return [process.pid] if process is not None and process.poll() is None else []


def _signal_all(pids, number: int) -> None:
    for pid in pids:
        try:
            os.kill(pid, number)
        except (ProcessLookupError, PermissionError):
            pass


ADMISSION_LOCK = "/tmp/fes-quartus-admission.lock"
# A `run` waiting for admission writes <pid> here; it holds a slot but has
# started nothing, so other admissions do not reserve memory for it.
ADMISSION_WAITERS = "/tmp/fes-quartus-admission-waiting"
ADMISSION_RETRY_SECONDS = 30
NOTIFY_ENV = "FES_QUARTUS_NOTIFY"


def _mem_available_kib() -> int:
    for line in Path("/proc/meminfo").read_text().splitlines():
        if line.startswith("MemAvailable:"):
            return int(line.split()[1])
    raise OSError("no MemAvailable in /proc/meminfo")


def _tree_rss_kib(root: int) -> int:
    parents, rss = {}, {}
    page_kib = os.sysconf("SC_PAGE_SIZE") // 1024
    for entry in os.listdir("/proc"):
        if entry.isdigit():
            try:
                parents[int(entry)] = int(Path(f"/proc/{entry}/stat").read_bytes().rsplit(b")", 1)[1].split()[1])
                rss[int(entry)] = int(Path(f"/proc/{entry}/statm").read_text().split()[1]) * page_kib
            except (OSError, IndexError, ValueError):
                continue
    children: dict[int, list[int]] = {}
    for pid, parent in parents.items():
        children.setdefault(parent, []).append(pid)
    total, pending, seen = 0, [root], set()
    while pending:
        pid = pending.pop()
        if pid in seen:
            continue
        seen.add(pid)
        total += rss.get(pid, 0)
        pending.extend(children.get(pid, ()))
    return total


def _ancestors_of(pid: int) -> list[int]:
    """pid's ancestors, nearest first."""
    found = []
    while pid > 1:
        try:
            pid = int(Path(f"/proc/{pid}/stat").read_bytes().rsplit(b")", 1)[1].split()[1])
        except (OSError, IndexError, ValueError):
            break
        if pid in found:
            break
        found.append(pid)
    return found


class Cancelled(Exception):
    """A termination signal before the job started."""

    def __init__(self, number: int):
        super().__init__(f"signal {number}")
        self.number = number


def _start_time(pid: int) -> str:
    """A process's start time since boot (in clock ticks), which with its pid
    names one process; empty when it is gone."""
    try:
        return Path(f"/proc/{pid}/stat").read_bytes().rsplit(b")", 1)[1].split()[19].decode()
    except (OSError, IndexError):
        return ""


def outstanding_kib(own_slot: str) -> int:
    """Memory the other running Quartus jobs on this host may still grow into:
    for each other held slot, its edition's peak less what its holder's
    process tree already uses."""
    peaks: dict[str, int] = {}
    for name in ("lite", "pro"):
        for path in quartus_tools.lease_pool(name):
            peaks[path] = max(peaks.get(path, 0), quartus_tools.PEAK_RSS_GIB[name] * 1024 * 1024)
    waiting = set()
    try:
        names = os.listdir(ADMISSION_WAITERS)
    except OSError:
        names = []
    for name in names:
        # A marker counts only for the process that wrote it: same pid and
        # same start time. One left by a killed waiter, whose pid another
        # process has since taken, exempts nobody.
        try:
            if name.isdigit() and Path(ADMISSION_WAITERS, name).read_text().strip() == _start_time(int(name)):
                waiting.add(int(name))
        except OSError:
            continue
    # Per holder: a process holding several slots (parallel seeds in one
    # runner) may grow to the sum of their peaks, and its tree's memory
    # counts once against that sum.
    held: dict[int, int] = {}
    for path, peak in peaks.items():
        if path == own_slot:
            continue
        for holder in quartus_tools.flock_holders(path) - {os.getpid()} - waiting:
            held[holder] = held.get(holder, 0) + peak
    # Holders nested in one process tree (a run wrapper and the seed runner
    # below it) are one job: their peaks add up, and the tree's memory, read
    # at its topmost holder, counts once.
    groups: dict[int, int] = {}
    for holder, peak in held.items():
        top = holder
        for ancestor in _ancestors_of(holder):
            if ancestor in held:
                top = ancestor
        groups[top] = groups.get(top, 0) + peak
    return sum(max(0, peak - _tree_rss_kib(top)) for top, peak in groups.items())


def admit(edition: str, slot: str, deadline: float = float("inf")) -> bool:
    """Wait, holding the slot, until this host has the memory for one more
    job beyond what the other running jobs may still grow into. Checks are
    serialised by a host-wide lock, so two callers cannot both pass on the
    same free memory: the second sees the first as a holder still to grow.
    False when deadline (time.monotonic) passes first: the wait for memory
    counts against the job's time cap, since it holds a slot."""
    need = quartus_tools.PEAK_RSS_GIB[quartus_tools.edition_class(edition)] * 1024 * 1024
    marker = Path(ADMISSION_WAITERS, str(os.getpid()))
    try:
        while True:
            with open(ADMISSION_LOCK, "a") as guard:
                fcntl.flock(guard, fcntl.LOCK_EX)
                # The cap first: a job is never started after it has expired.
                if time.monotonic() >= deadline:
                    return False
                available, reserved = _mem_available_kib(), outstanding_kib(slot)
                if available - reserved >= need:
                    # Admitted: from here on others reserve this job's peak.
                    marker.unlink(missing_ok=True)
                    return True
                # Waiting, under the lock: no other admission counts this one.
                marker.parent.mkdir(mode=0o1777, exist_ok=True)
                marker.write_text(_start_time(os.getpid()))
            print(f"quartus_jobs: waiting for memory: {available / 1024 ** 2:.1f} GiB available, "
              f"{reserved / 1024 ** 2:.1f} GiB reserved by running jobs, {need / 1024 ** 2:g} GiB needed",
              file=sys.stderr, flush=True)
            time.sleep(ADMISSION_RETRY_SECONDS)
    finally:
        marker.unlink(missing_ok=True)


def notify(summary: dict) -> None:
    """Record the outcome in the run directory and pass it to the command in
    $FES_QUARTUS_NOTIFY (run by the shell, summary as JSON on stdin), if set.
    Neither can fail the job."""
    text = json.dumps(summary, indent=2) + "\n"
    run_dir = os.environ.get("FES_QUARTUS_RUN_DIR")
    if run_dir:
        _best_effort_write(Path(run_dir, "result.json"), text)
    hook = os.environ.get(NOTIFY_ENV)
    if hook:
        try:
            subprocess.run(["sh", "-c", hook], input=text, text=True, timeout=60, check=False)
        except (OSError, subprocess.SubprocessError) as error:
            print(f"quartus_jobs: notify hook failed: {error}", file=sys.stderr, flush=True)


def run(edition: str, label: str, command: list[str], sample_seconds: float = 5.0,
        cap_seconds: float = DEFAULT_CAP_MINUTES * 60, deadline_epoch: float | None = None) -> int:
    """_run, with a notification of how it ended: finished, failed, refused
    or a wrapper error."""
    base = {"host": local_host(), "edition": edition, "label": label, "command": command,
            "finished_utc": None}
    if deadline_epoch is not None:
        # An absolute deadline from a dispatcher: setup already used some.
        cap_seconds = min(cap_seconds, deadline_epoch - time.time())
        if cap_seconds <= 0:
            notify(dict(base, finished_utc=datetime.now(timezone.utc).isoformat(), status="capped", exit=124))
            return 124
    try:
        code = _run(edition, label, command, sample_seconds, cap_seconds)
    except BaseException as error:
        notify(dict(base, finished_utc=datetime.now(timezone.utc).isoformat(), status="wrapper-error",
                    error=f"{type(error).__name__}: {error}"))
        raise
    status = {0: "finished", 75: "refused", 124: "capped"}.get(code, "cancelled" if code > 128 else "failed")
    notify(dict(base, finished_utc=datetime.now(timezone.utc).isoformat(), status=status, exit=code))
    return code


def _run(edition: str, label: str, command: list[str], sample_seconds: float,
         cap_seconds: float) -> int:
    """COMMAND under a slot of this host's pool.

    The slot is released only when every process of the job has exited,
    including any it left running in the background. A termination signal is
    passed to every process of the job. After cap_seconds the job is sent
    SIGTERM, then SIGKILL after a grace, and run returns 124."""
    if not command:
        raise SystemExit("run needs a command after --")
    if not math.isfinite(cap_seconds) or cap_seconds <= 0:
        raise SystemExit(f"the time cap must be finite and positive, not {cap_seconds}")
    run_dir = os.environ.get("FES_QUARTUS_RUN_DIR")
    for place in {Path.cwd(), *([Path(run_dir)] if run_dir else [])}:
        free = shutil.disk_usage(place).free / 1024 ** 3
        if free < MIN_FREE_GIB:
            print(f"quartus_jobs: refusing: {place} has {free:.1f} GiB free, under {MIN_FREE_GIB} GiB",
                  file=sys.stderr, flush=True)
            return 75
    token = f"{os.getpid()}-{time.monotonic_ns()}"
    with quartus_tools.quartus_lease(edition, label=label or Path(command[0]).name) as slot:
        # The cap runs from the moment a slot is held, so waiting for memory
        # cannot keep a slot past it.
        started = time.monotonic()
        deadline = started + cap_seconds

        def cancel(number, _frame):
            raise Cancelled(number)

        # A signal while waiting for memory ends the run as a cancellation,
        # through the normal exit (and its notification), not by default death.
        waiting = {sig: signal.signal(sig, cancel) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        try:
            admitted = admit(edition, slot, deadline)
        except Cancelled as cancelled:
            print(f"quartus_jobs: cancelled by {cancelled} while waiting for memory; nothing was started",
                  file=sys.stderr, flush=True)
            return 128 + cancelled.number
        finally:
            for sig, handler in waiting.items():
                signal.signal(sig, handler)
        if not admitted:
            print(f"quartus_jobs: time cap of {cap_seconds / 60:g} min reached waiting for memory; "
                  "nothing was started", file=sys.stderr, flush=True)
            return 124
        was_subreaper = _set_subreaper(True)
        before = _children_of_self()
        pending_signal: list[int] = []
        process = None

        def scan():
            # The launcher's pid is followed only while it is the launcher:
            # once it has exited its number may name an unrelated process.
            # What it left behind was adopted here (subreaper) or is tagged.
            if process is None:
                return {}
            return job_processes(token, process.pid if process.returncode is None else None, before)

        def relay(number, _frame):
            pending_signal.append(number)
            if process is not None:
                _signal_all([*_launcher(process), *scan()], number)

        # The relay is in place before the job exists: a signal in between is
        # recorded and passed on as soon as there is a job to pass it to, so
        # the wrapper can never die and release the slot over a live job.
        previous = {sig: signal.signal(sig, relay) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        peak, code, capped, killed_at = 0, None, False, None
        beat = time.monotonic()
        try:
            process = subprocess.Popen(command, start_new_session=True,
                                       env=dict(os.environ, **{JOB_TOKEN: token}))
            if pending_signal:
                _signal_all([process.pid], pending_signal[0])
            while True:
                live = scan()
                peak = max(peak, sum(live.values()))
                if code is None:
                    try:
                        code = process.wait(timeout=sample_seconds)
                    except subprocess.TimeoutExpired:
                        pass
                    live = scan()
                if code is not None and not live:
                    break
                if code is not None and live and not capped:
                    print(f"quartus_jobs: the command exited; holding the slot for {len(live)} "
                          "process(es) it left running", file=sys.stderr, flush=True)
                now = time.monotonic()
                if now - beat >= HEARTBEAT_SECONDS:
                    beat = now
                    line = (f"quartus_jobs heartbeat: {(now - started) / 60:.1f} min, {len(live)} process(es), "
                            f"{sum(live.values()) / 1024 ** 2:.2f} GiB resident, slot {slot}")
                    print(line, file=sys.stderr, flush=True)
                    if run_dir:
                        _best_effort_write(Path(run_dir, "heartbeat"),
                                           datetime.now(timezone.utc).isoformat() + " " + line + "\n")
                if now >= deadline and not capped:
                    capped, killed_at = True, now
                    print(f"quartus_jobs: time cap of {cap_seconds / 60:g} min reached; stopping the job",
                          file=sys.stderr, flush=True)
                    _signal_all([*_launcher(process), *live], signal.SIGTERM)
                elif capped and now - killed_at >= KILL_GRACE_SECONDS:
                    _signal_all([*_launcher(process), *live], signal.SIGKILL)
                if code is not None:
                    time.sleep(sample_seconds)
                    _reap_adopted(before | {process.pid})
        except BaseException:
            # Whatever unwound the wrapper (an interrupt, an error of its
            # own), the slot is released below: stop and reap every process
            # of the job first, so nothing of it runs once another job can
            # take the slot.
            _stop_job(process, scan)
            raise
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)
            _reap_adopted(before | ({process.pid} if process is not None else set()))
            _set_subreaper(was_subreaper)
        if code is not None and code < 0:
            code = 128 - code  # killed by a signal, as a shell reports it
        if capped:
            code = 124
        elif pending_signal and code == 0:
            # A cancelled job is never reported as a success, even when its
            # command trapped the signal or had already exited.
            code = 128 + pending_signal[0]
        record = {"host": local_host(), "edition": edition, "slot": slot, "command": command,
                  "exit": code, "capped": capped, "cap_seconds": cap_seconds,
                  "peak_rss_kib": peak, "sample_seconds": sample_seconds}
        print(f"quartus_jobs: peak RSS {peak / 1024 ** 2:.2f} GiB (every process of the job, sampled every "
              f"{sample_seconds:g} s), exit {code}", file=sys.stderr, flush=True)
        if run_dir:
            _best_effort_write(Path(run_dir, "peak-rss.json"), json.dumps(record, indent=2) + "\n")
        return code


def resolve_pushed(revision: str) -> str:
    commit = subprocess.check_output(["git", "-C", str(REPO), "rev-parse", "--verify", revision + "^{commit}"],
                                     text=True).strip()
    # Only origin: that is the remote the build host fetches from.
    remote = subprocess.run(["git", "-C", str(REPO), "branch", "-r", "--contains", commit, "--list", "origin/*"],
                            capture_output=True, text=True)
    if remote.returncode or not remote.stdout.strip():
        raise SystemExit(f"{commit} is on no origin branch; push it to origin first "
                         "(remote runs build pushed commits only)")
    return commit


def remote_script(*, commit: str, host: str, home: str, edition: str, label: str,
                  command: list[str], stamp: str, origin: str,
                  cap_minutes: float = DEFAULT_CAP_MINUTES, deadline_epoch: float | None = None) -> tuple[str, str]:
    """The shell program that checks commit out into a new run directory and
    runs command under the lease there, and that directory.

    The checkout is a detached worktree of a blobless mirror this tool owns on
    each host (fetched from origin, so only pushed commits resolve). It is a
    real checkout because producers ask git for their tree and revision."""
    tag = re.sub(r"[^A-Za-z0-9_.-]", "-", label or "job")[:40]
    # A random suffix, and a plain mkdir that fails on an existing directory,
    # so every invocation gets a new run directory.
    run_dir = f"{PROJECTS}/runs/{commit[:12]}-{stamp}-{tag}-{os.urandom(3).hex()}"
    tree = f"{run_dir}/src"
    mirror = f"{PROJECTS}/mirror.git"
    expand = lambda path: home + path[1:] if path.startswith("~") else path  # noqa: E731
    cache = expand(TOOLCHAIN_CACHES[host])
    inner = [part.replace("{run}", run_dir).replace("{cache}", cache) for part in command]
    q = shlex.quote
    job = {"commit": commit, "host": host, "edition": edition, "command": inner}
    script = "\n".join([
        "set -euo pipefail",
        f"mkdir -p {q(PROJECTS)}/runs",
        # The AGENTS.md disk rule, before anything is written.
        f"free=$(df --output=avail -BG {q(PROJECTS)} | tail -1 | tr -dc 0-9)",
        f'[ "$free" -ge {MIN_FREE_GIB} ] || {{ echo "quartus_jobs: refusing: {PROJECTS} has ${{free}}G free, '
        f'under {MIN_FREE_GIB}G" >&2; exit 75; }}',
        f"mkdir {q(run_dir)}",
        # A setup step that fails (clone, fetch, worktree, checkout check)
        # still leaves an outcome; exec below replaces the shell, so this
        # never fires once the run wrapper has started.
        f"trap 'rc=$?; [ -e {q(run_dir)}/result.json ] || printf '\\''{{\"status\": \"setup-failed\", "
        f"\"exit\": %d}}\\n'\\'' $rc > {q(run_dir)}/result.json' EXIT",
        # One writer at a time on the mirror: cloning, fetching and adding a
        # worktree all change it.
        f"exec 9>>{q(PROJECTS)}/.mirror.lock",
        # Every setup step is bounded by what is left of the job's cap.
        f"left() {{ echo $(( {int(deadline_epoch) if deadline_epoch else 2 ** 40} - $(date +%s) )); }}",
        'bounded() { s=$(left); [ "$s" -gt 0 ] || { echo "quartus_jobs: time cap reached during setup" >&2; '
        'exit 124; }; timeout "$s" "$@"; }',
        'bounded flock 9',
        f"[ -d {q(mirror)} ] || bounded git clone -q --bare --filter=blob:none {q(origin)} {q(mirror)}",
        # The caller's origin, whoever created the mirror.
        f"git -C {q(mirror)} remote set-url origin {q(origin)}",
        f"bounded git -C {q(mirror)} fetch -q origin {q(commit)}",
        f"bounded git -C {q(mirror)} worktree add -q --detach {q(tree)} {q(commit)}",
        "flock -u 9",
        f"cd {q(tree)}/sources/misteross",
        f'test "$(git rev-parse HEAD)" = {q(commit)} && test -z "$(git status --porcelain)"',
        f"export QUARTUS_ROOTDIR={q(expand(QUARTUS_ROOTS[quartus_tools.edition_class(edition)]))}",
        f"export FES_TOOLCHAIN_CACHE_ROOT={q(cache)}",
        f"export FES_QUARTUS_RUN_DIR={q(run_dir)}",
        f"printf '%s\\n' {q(json.dumps(job))} > {q(run_dir)}/job.json",
        f"exec python3 scripts/quartus_jobs.py run --edition {q(edition)} --label {q(tag)} "
        f"--cap-minutes {cap_minutes:g} "
        + (f"--deadline-epoch {deadline_epoch:.0f} " if deadline_epoch else "") + "-- "
        f"{shlex.join(inner)} > >(tee {q(run_dir)}/job.log) 2>&1",
    ])
    return script, run_dir


def url_has_credentials(url: str) -> bool:
    """Whether an http(s) origin embeds a user or token. SSH forms
    (git@host:path, ssh://git@host/path) name a login, not a secret."""
    match = re.match(r"https?://([^/]*)@", url)
    return match is not None


def remote(edition: str, host: str, revision: str, label: str, command: list[str],
           cap_minutes: float = DEFAULT_CAP_MINUTES) -> int:
    if not command:
        raise SystemExit("remote needs a command after --")
    commit = resolve_pushed(revision)
    origin = subprocess.check_output(["git", "-C", str(REPO), "remote", "get-url", "origin"], text=True).strip()
    if url_has_credentials(origin):
        raise SystemExit("origin's URL carries credentials; give remote hosts a plain URL")
    if host != "auto" and host not in HOSTS:
        raise SystemExit(f"unknown build host {host!r}; one of {', '.join(HOSTS)}")
    # Dispatch only onto a ready host, chosen or named; otherwise wait and
    # probe again, so a free slot on a host without the memory is never taken.
    # The cap runs from here: waiting for a host counts against it.
    asked = host
    started = time.monotonic()
    deadline = started + cap_minutes * 60
    while True:
        left = max(deadline - time.monotonic(), 1)
        if asked == "auto":
            host, probes, ready = pick(edition, timeout=min(60, left))
        else:
            probes = [probe(asked, edition, timeout=min(60, left))]
            ready = is_ready(probes[0], edition)
        if all("error" in p for p in probes):
            print("quartus_jobs: no build host answered: " + "; ".join(p["error"] for p in probes),
                  file=sys.stderr, flush=True)
            notify({"host": None, "edition": edition, "label": label, "command": command, "commit": commit,
                    "finished_utc": datetime.now(timezone.utc).isoformat(), "status": "remote-failed",
                    "exit": 255, "error": "; ".join(p["error"] for p in probes)})
            return 255
        if ready:
            break
        if time.monotonic() - started >= cap_minutes * 60:
            print(f"quartus_jobs: time cap of {cap_minutes:g} min reached waiting for a build host; "
                  "nothing was started", file=sys.stderr, flush=True)
            notify({"host": None, "edition": edition, "label": label, "command": command, "commit": commit,
                    "finished_utc": datetime.now(timezone.utc).isoformat(), "status": "capped", "exit": 124})
            return 124
        print(f"quartus_jobs: {'no build host' if asked == 'auto' else asked} has a free {edition} slot and "
              f"{quartus_tools.PEAK_RSS_GIB[quartus_tools.edition_class(edition)]} GiB available; "
              f"probing again in {REPROBE_SECONDS} s", file=sys.stderr, flush=True)
        time.sleep(max(0, min(REPROBE_SECONDS, deadline - time.monotonic())))
    for p in probes:
        print(f"quartus_jobs: {p['host']}: " + (p.get("error") or
              f"{p['free']} free {edition} slot(s), {p['available_kib'] // 1024 ** 2} GiB available"),
              file=sys.stderr)
    chosen = next(p for p in probes if p["host"] == host)
    if "error" in chosen:
        raise SystemExit(f"{host} did not answer: {chosen['error']}")
    print(f"quartus_jobs: running on {host}", file=sys.stderr, flush=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    # One absolute deadline for setup and the run on the host: the cap began
    # when this call started waiting for a host.
    remaining = max(cap_minutes - (time.monotonic() - started) / 60, 1 / 60)
    deadline_epoch = time.time() + remaining * 60
    script, run_dir = remote_script(commit=commit, host=host, home=chosen["home"], edition=edition,
                                    label=label, command=command, stamp=stamp, origin=origin,
                                    cap_minutes=remaining, deadline_epoch=deadline_epoch)
    target = ["bash", "-c", script] if host == local_host() else [*SSH, host, shlex.join(["bash", "-c", script])]
    result = subprocess.run(target, stdin=subprocess.DEVNULL)
    print(f"quartus_jobs: {host}:{run_dir} (exit {result.returncode})", file=sys.stderr)
    # The run wrapper on the host records its own outcome there; the caller's
    # FES_QUARTUS_NOTIFY (not forwarded over SSH) hears every outcome here,
    # including a remote that failed before the wrapper started.
    code = result.returncode
    # 255 is ssh's own failure; 129-254 a signal on the host; anything else
    # is the job's or the setup's failure.
    status = {0: "finished", 75: "refused", 124: "capped"}.get(
        code, "cancelled" if 128 < code < 255 else "remote-failed")
    notify({"host": host, "edition": edition, "label": label, "command": command, "commit": commit,
            "run_dir": run_dir, "finished_utc": datetime.now(timezone.utc).isoformat(),
            "status": status, "exit": code})
    return result.returncode


def cap_minutes(text: str) -> float:
    """A time cap: a finite, positive number of minutes. nan or inf would
    never expire."""
    value = float(text)
    if not math.isfinite(value) or value <= 0:
        raise argparse.ArgumentTypeError(f"the time cap must be a finite, positive number of minutes, not {text}")
    return value


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest="action", required=True)
    editions = sorted(quartus_tools.EDITION_CLASS)
    sub = commands.add_parser("status")
    sub.add_argument("--edition", choices=editions)
    sub = commands.add_parser("pick")
    sub.add_argument("--edition", choices=editions, required=True)
    sub.add_argument("--json", action="store_true")
    for name in ("run", "remote"):
        sub = commands.add_parser(name)
        sub.add_argument("--edition", choices=editions, required=True)
        sub.add_argument("--label", default="")
        if name == "run":
            sub.add_argument("--deadline-epoch", type=float, default=None,
                             help="an absolute end (Unix time) the cap may not pass; set by remote")
        sub.add_argument("--cap-minutes", type=cap_minutes, default=DEFAULT_CAP_MINUTES,
                         help="stop the job after this long (default %(default)s); exit 124")
        if name == "remote":
            sub.add_argument("--host", default="auto")
            sub.add_argument("--commit", default="HEAD")
        sub.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args(argv)
    command = getattr(args, "command", [])
    if command[:1] == ["--"]:
        command = command[1:]
    if args.action == "status":
        return status(args.edition)
    if args.action == "pick":
        host, probes, ready = pick(args.edition)
        if host is None and not args.json:
            raise SystemExit("no build host answered: " + "; ".join(p["error"] for p in probes))
        print(json.dumps({"host": host, "ready": ready, "probes": probes}, indent=2) if args.json
              else host + ("" if ready else " (no host ready: a remote job would wait)"))
        return 0
    if args.action == "run":
        return run(args.edition, args.label, command, cap_seconds=args.cap_minutes * 60,
                   deadline_epoch=args.deadline_epoch)
    return remote(args.edition, args.host, args.commit, args.label, command, args.cap_minutes)


if __name__ == "__main__":
    raise SystemExit(main())
