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

`run` reports the peak resident set size of the job and all its descendants, and
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
import signal
import socket
import subprocess
import sys

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


def tree_rss_kib(root: int) -> int:
    """Total resident memory of root and all its descendants.

    By parentage, not process group: producers and fit scripts start each
    Quartus stage in a new session, so a process-group sum misses Quartus."""
    parents, rss = {}, {}
    page_kib = os.sysconf("SC_PAGE_SIZE") // 1024
    for entry in os.listdir("/proc"):
        if not entry.isdigit():
            continue
        try:
            fields = Path(f"/proc/{entry}/stat").read_bytes().rsplit(b")", 1)[1].split()
            parents[int(entry)] = int(fields[1])
            rss[int(entry)] = int(Path(f"/proc/{entry}/statm").read_text().split()[1]) * page_kib
        except (OSError, IndexError, ValueError):
            continue
    children: dict[int, list[int]] = {}
    for pid, parent in parents.items():
        children.setdefault(parent, []).append(pid)
    total, pending = 0, [root]
    while pending:
        pid = pending.pop()
        total += rss.get(pid, 0)
        pending.extend(children.get(pid, ()))
    return total


def run(edition: str, label: str, command: list[str], sample_seconds: float = 5.0) -> int:
    """COMMAND under a slot of this host's pool; the slot is released when it
    exits. A termination signal is passed on to the job's process group and
    the job waited for, so the slot is never released over a live Quartus."""
    if not command:
        raise SystemExit("run needs a command after --")
    with quartus_tools.quartus_lease(edition, label=label or Path(command[0]).name) as slot:
        process = subprocess.Popen(command, start_new_session=True)

        def relay(number, _frame):
            try:
                os.killpg(process.pid, number)
            except ProcessLookupError:
                pass

        previous = {sig: signal.signal(sig, relay) for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)}
        peak = 0
        try:
            while True:
                peak = max(peak, tree_rss_kib(process.pid))
                try:
                    code = process.wait(timeout=sample_seconds)
                    break
                except subprocess.TimeoutExpired:
                    continue
        finally:
            for sig, handler in previous.items():
                signal.signal(sig, handler)
        record = {"host": local_host(), "edition": edition, "slot": slot, "command": command,
                  "exit": code, "peak_rss_kib": peak, "sample_seconds": sample_seconds}
        print(f"quartus_jobs: peak RSS {peak / 1024 ** 2:.2f} GiB (the job and its descendants, sampled every "
              f"{sample_seconds:g} s), exit {code}", file=sys.stderr, flush=True)
        run_dir = os.environ.get("FES_QUARTUS_RUN_DIR")
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
                  command: list[str], stamp: str, origin: str) -> tuple[str, str]:
    """The shell program that checks commit out into a new run directory and
    runs command under the lease there, and that directory.

    The checkout is a detached worktree of a blobless mirror this tool owns on
    each host (fetched from origin, so only pushed commits resolve). It is a
    real checkout because producers ask git for their tree and revision."""
    tag = re.sub(r"[^A-Za-z0-9_.-]", "-", label or "job")[:40]
    run_dir = f"{PROJECTS}/runs/{commit[:12]}-{stamp}-{tag}"
    tree = f"{run_dir}/src"
    mirror = f"{PROJECTS}/mirror.git"
    expand = lambda path: home + path[1:] if path.startswith("~") else path  # noqa: E731
    cache = expand(TOOLCHAIN_CACHES[host])
    inner = [part.replace("{run}", run_dir).replace("{cache}", cache) for part in command]
    q = shlex.quote
    job = {"commit": commit, "host": host, "edition": edition, "command": inner}
    script = "\n".join([
        "set -euo pipefail",
        f"test ! -e {q(run_dir)}",
        f"mkdir -p {q(run_dir)}",
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
        f"exec python3 scripts/quartus_jobs.py run --edition {q(edition)} --label {q(tag)} -- "
        f"{shlex.join(inner)} > >(tee {q(run_dir)}/job.log) 2>&1",
    ])
    return script, run_dir


def remote(edition: str, host: str, revision: str, label: str, command: list[str]) -> int:
    if not command:
        raise SystemExit("remote needs a command after --")
    commit = resolve_pushed(revision)
    origin = subprocess.check_output(["git", "-C", str(REPO), "remote", "get-url", "origin"], text=True).strip()
    if "@" in origin.split("//", 1)[-1].split("/", 1)[0]:
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
                                    label=label, command=command, stamp=stamp, origin=origin)
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
        return run(args.edition, args.label, command)
    return remote(args.edition, args.host, args.commit, args.label, command)


if __name__ == "__main__":
    raise SystemExit(main())
