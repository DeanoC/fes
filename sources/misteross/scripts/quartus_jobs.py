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
from datetime import datetime, timezone
import json
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


def probe(host: str, edition: str) -> dict:
    slots = list(quartus_tools.lease_pool(edition, host))
    command = [sys.executable, "-c", PROBE, *slots] if host == local_host() else \
        [*SSH, host, shlex.join(["python3", "-c", PROBE, *slots])]
    result = subprocess.run(command, capture_output=True, text=True, timeout=60)
    if result.returncode:
        return {"host": host, "error": (result.stderr or result.stdout).strip()[-300:]}
    data = json.loads(result.stdout)
    data["host"] = host
    data["free"] = sum(not busy for busy in data["slots"].values())
    return data


def pick(edition: str) -> tuple[str, list[dict]]:
    """The host to run an edition job on, and every probe."""
    probes = [probe(host, edition) for host in HOSTS]
    usable = [p for p in probes if "error" not in p]
    if not usable:
        raise SystemExit("no build host answered: " + "; ".join(p["error"] for p in probes))
    need = quartus_tools.PEAK_RSS_GIB[quartus_tools.edition_class(edition)] * 1024 * 1024
    # A free slot on a host with the memory for one job wins; otherwise the
    # job waits for a slot where the most memory is available.
    preferred = PREFERRED.get(quartus_tools.edition_class(edition))
    def rank(p):
        ready = p["free"] > 0 and p["available_kib"] >= need
        return ready, ready and p["host"] == preferred, p["free"], p["available_kib"]

    best = max(usable, key=rank)
    return best["host"], probes


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


def _signal_all(pids, number: int) -> None:
    for pid in pids:
        try:
            os.kill(pid, number)
        except (ProcessLookupError, PermissionError):
            pass


def run(edition: str, label: str, command: list[str], sample_seconds: float = 5.0,
        cap_seconds: float = DEFAULT_CAP_MINUTES * 60) -> int:
    """COMMAND under a slot of this host's pool.

    The slot is released only when every process of the job has exited,
    including any it left running in the background. A termination signal is
    passed to every process of the job. After cap_seconds the job is sent
    SIGTERM, then SIGKILL after a grace, and run returns 124."""
    if not command:
        raise SystemExit("run needs a command after --")
    run_dir = os.environ.get("FES_QUARTUS_RUN_DIR")
    for place in {Path.cwd(), *([Path(run_dir)] if run_dir else [])}:
        free = shutil.disk_usage(place).free / 1024 ** 3
        if free < MIN_FREE_GIB:
            print(f"quartus_jobs: refusing: {place} has {free:.1f} GiB free, under {MIN_FREE_GIB} GiB",
                  file=sys.stderr, flush=True)
            return 75
    token = f"{os.getpid()}-{time.monotonic_ns()}"
    with quartus_tools.quartus_lease(edition, label=label or Path(command[0]).name) as slot:
        was_subreaper = _set_subreaper(True)
        before = _children_of_self()
        process = subprocess.Popen(command, start_new_session=True, env=dict(os.environ, **{JOB_TOKEN: token}))
        scan = lambda: job_processes(token, process.pid, before)  # noqa: E731
        pending_signal = []

        def relay(number, _frame):
            pending_signal.append(number)
            _signal_all([process.pid, *scan()], number)

        previous = {sig: signal.signal(sig, relay) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        peak, code, capped, killed_at = 0, None, False, None
        started = time.monotonic()
        deadline, beat = started + cap_seconds, started
        try:
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
                        Path(run_dir, "heartbeat").write_text(
                            datetime.now(timezone.utc).isoformat() + " " + line + "\n")
                if now >= deadline and not capped:
                    capped, killed_at = True, now
                    print(f"quartus_jobs: time cap of {cap_seconds / 60:g} min reached; stopping the job",
                          file=sys.stderr, flush=True)
                    _signal_all([process.pid, *live], signal.SIGTERM)
                elif capped and now - killed_at >= KILL_GRACE_SECONDS:
                    _signal_all([process.pid, *live], signal.SIGKILL)
                if code is not None:
                    time.sleep(sample_seconds)
                    _reap_adopted(before | {process.pid})
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)
            _reap_adopted(before | {process.pid})
            _set_subreaper(was_subreaper)
        if capped:
            code = 124
        record = {"host": local_host(), "edition": edition, "slot": slot, "command": command,
                  "exit": code, "capped": capped, "cap_seconds": cap_seconds,
                  "peak_rss_kib": peak, "sample_seconds": sample_seconds}
        print(f"quartus_jobs: peak RSS {peak / 1024 ** 2:.2f} GiB (every process of the job, sampled every "
              f"{sample_seconds:g} s), exit {code}", file=sys.stderr, flush=True)
        if run_dir:
            Path(run_dir, "peak-rss.json").write_text(json.dumps(record, indent=2) + "\n")
        return code


def resolve_pushed(revision: str) -> str:
    commit = subprocess.check_output(["git", "-C", str(REPO), "rev-parse", "--verify", revision + "^{commit}"],
                                     text=True).strip()
    remote = subprocess.run(["git", "-C", str(REPO), "branch", "-r", "--contains", commit],
                            capture_output=True, text=True)
    if remote.returncode or not remote.stdout.strip():
        raise SystemExit(f"{commit} is on no remote branch; push it first (remote runs build pushed commits only)")
    return commit


def remote_script(*, commit: str, host: str, home: str, edition: str, label: str,
                  command: list[str], stamp: str, origin: str,
                  cap_minutes: float = DEFAULT_CAP_MINUTES) -> tuple[str, str]:
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
        # One writer at a time on the mirror: cloning, fetching and adding a
        # worktree all change it.
        f"exec 9>>{q(PROJECTS)}/.mirror.lock",
        "flock 9",
        f"[ -d {q(mirror)} ] || git clone -q --bare --filter=blob:none {q(origin)} {q(mirror)}",
        f"git -C {q(mirror)} fetch -q origin {q(commit)}",
        f"git -C {q(mirror)} worktree add -q --detach {q(tree)} {q(commit)}",
        "flock -u 9",
        f"cd {q(tree)}/sources/misteross",
        f'test "$(git rev-parse HEAD)" = {q(commit)} && test -z "$(git status --porcelain)"',
        f"export QUARTUS_ROOTDIR={q(expand(QUARTUS_ROOTS[quartus_tools.edition_class(edition)]))}",
        f"export FES_TOOLCHAIN_CACHE_ROOT={q(cache)}",
        f"export FES_QUARTUS_RUN_DIR={q(run_dir)}",
        f"printf '%s\\n' {q(json.dumps(job))} > {q(run_dir)}/job.json",
        f"exec python3 scripts/quartus_jobs.py run --edition {q(edition)} --label {q(tag)} "
        f"--cap-minutes {cap_minutes:g} -- "
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
    if host == "auto":
        host, probes = pick(edition)
    elif host in HOSTS:
        probes = [probe(host, edition)]
    else:
        raise SystemExit(f"unknown build host {host!r}; one of {', '.join(HOSTS)}")
    for p in probes:
        print(f"quartus_jobs: {p['host']}: " + (p.get("error") or
              f"{p['free']} free {edition} slot(s), {p['available_kib'] // 1024 ** 2} GiB available"),
              file=sys.stderr)
    chosen = next(p for p in probes if p["host"] == host)
    if "error" in chosen:
        raise SystemExit(f"{host} did not answer: {chosen['error']}")
    print(f"quartus_jobs: running on {host}", file=sys.stderr, flush=True)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    script, run_dir = remote_script(commit=commit, host=host, home=chosen["home"], edition=edition,
                                    label=label, command=command, stamp=stamp, origin=origin,
                                    cap_minutes=cap_minutes)
    target = ["bash", "-c", script] if host == local_host() else [*SSH, host, shlex.join(["bash", "-c", script])]
    result = subprocess.run(target, stdin=subprocess.DEVNULL)
    print(f"quartus_jobs: {host}:{run_dir} (exit {result.returncode})", file=sys.stderr)
    return result.returncode


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
        sub.add_argument("--cap-minutes", type=float, default=DEFAULT_CAP_MINUTES,
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
        host, probes = pick(args.edition)
        print(json.dumps({"host": host, "probes": probes}, indent=2) if args.json else host)
        return 0
    if args.action == "run":
        return run(args.edition, args.label, command, cap_seconds=args.cap_minutes * 60)
    return remote(args.edition, args.host, args.commit, args.label, command, args.cap_minutes)


if __name__ == "__main__":
    raise SystemExit(main())
