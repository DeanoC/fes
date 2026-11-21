"""Quartus discovery, authenticated against a core's quartus.lock.

Each Quartus producer names its core's lock (cores/<core>/quartus.lock). The lock
holds the exact `quartus_sh --version` line, the edition, the device family and
the sha256 of quartus_sh, and, for a core whose Yosys front end has no
toolchain.lock of its own, the Yosys commit and binary digest:

    [quartus]
    edition = "lite"
    version = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
    device_family = "cyclonev"
    quartus_sh_sha256 = "<64 hex>"

    [yosys]
    commit = "<40 hex>"
    sha256 = "<64 hex>"

Every version string a producer writes (QSF, QPF, compiler label, toolchain
text) is derived from the lock, so moving a lock forward moves them all.

This module also owns the Quartus lease (see "Quartus lease" below): every run
of a licensed Quartus holds one slot of its host's pool for its edition.
locate_quartus takes it, and an audit hook refuses to start a licensed Quartus
program from a process that holds none.
"""
from contextlib import contextmanager
import fcntl
import hashlib
import os
import re
import shlex
import socket
import subprocess
import sys
import threading
import time
import tomllib
from dataclasses import dataclass
from pathlib import Path

EDITIONS = {"lite": "Lite", "standard": "Standard", "pro": "Pro"}
DEVICE_FAMILIES = {"lite": ("cyclonev",), "standard": ("cyclonev",), "pro": ("agilex5",)}
VERSION_LINE_RE = re.compile(
    r"Version (?P<release>(?P<series>[0-9]+[.][0-9]+)[.][0-9]+) Build [0-9]+ \S+ SJ "
    r"(?P<edition>Lite|Standard|Pro) Edition"
)
HEX40 = re.compile(r"[0-9a-f]{40}")
HEX64 = re.compile(r"[0-9a-f]{64}")

class RebuildError(RuntimeError):
    """Raised when a core rebuild cannot run or produce an RBF."""


@dataclass(frozen=True)
class QuartusLock:
    """A core's locked Quartus (and, where the lock carries it, Yosys)."""

    path: str
    edition: str
    version: str
    device_family: str
    quartus_sh_sha256: str
    yosys_commit: str | None = None
    yosys_sha256: str | None = None

    def _field(self, name: str) -> str:
        return VERSION_LINE_RE.fullmatch(self.version).group(name)

    @property
    def release(self) -> str:
        """The dotted release, for example 17.0.2."""
        return self._field("release")

    @property
    def series(self) -> str:
        """Major.minor, as a QPF's QUARTUS_VERSION names it, for example 17.0."""
        return self._field("series")

    @property
    def edition_name(self) -> str:
        return EDITIONS[self.edition]

    @property
    def product(self) -> str:
        """For example "Quartus Prime Lite 17.0.2"."""
        return f"Quartus Prime {self.edition_name} {self.release}"

    @property
    def compiler(self) -> str:
        """The build record's compiler label, for example quartus-17.0.2."""
        return f"quartus-{self.release}"

    @property
    def qsf_version(self) -> str:
        """LAST_QUARTUS_VERSION's value, for example "17.0.2 Lite Edition"."""
        return f"{self.release} {self.edition_name} Edition"


def load_quartus_lock(repo_root: Path, lock_path: str) -> QuartusLock:
    """Read and validate cores/<core>/quartus.lock (lock_path is repo-relative)."""

    path = Path(repo_root) / lock_path
    if not path.is_file() or path.is_symlink():
        raise RebuildError(f"Quartus lock is not a regular file: {lock_path}")
    try:
        data = tomllib.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, tomllib.TOMLDecodeError) as error:
        raise RebuildError(f"cannot read Quartus lock {lock_path}: {error}") from error
    if not set(data) <= {"quartus", "yosys"} or not isinstance(data.get("quartus"), dict):
        raise RebuildError(f"{lock_path} must hold [quartus] and optionally [yosys], nothing else")
    quartus = data["quartus"]
    keys = {"edition", "version", "device_family", "quartus_sh_sha256"}
    if set(quartus) != keys or not all(isinstance(quartus[k], str) for k in keys):
        raise RebuildError(f"{lock_path} [quartus] must set exactly {', '.join(sorted(keys))} as strings")
    edition = quartus["edition"]
    match = VERSION_LINE_RE.fullmatch(quartus["version"])
    if edition not in EDITIONS or match is None or match.group("edition") != EDITIONS[edition]:
        raise RebuildError(f"{lock_path} version line does not match its edition {edition!r}")
    if quartus["device_family"] not in DEVICE_FAMILIES[edition]:
        raise RebuildError(f"{lock_path} device family {quartus['device_family']!r} is not a {edition} family")
    if not HEX64.fullmatch(quartus["quartus_sh_sha256"]):
        raise RebuildError(f"{lock_path} quartus_sh_sha256 must be 64 lower-case hex digits")
    yosys_commit = yosys_sha256 = None
    if "yosys" in data:
        yosys = data["yosys"]
        if (not isinstance(yosys, dict) or set(yosys) != {"commit", "sha256"}
                or not isinstance(yosys["commit"], str) or not HEX40.fullmatch(yosys["commit"])
                or not isinstance(yosys["sha256"], str) or not HEX64.fullmatch(yosys["sha256"])):
            raise RebuildError(f"{lock_path} [yosys] must set exactly a 40-hex commit and a 64-hex sha256")
        yosys_commit, yosys_sha256 = yosys["commit"], yosys["sha256"]
    return QuartusLock(lock_path, edition, quartus["version"], quartus["device_family"],
                       quartus["quartus_sh_sha256"], yosys_commit, yosys_sha256)


def _sha256(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()

def _symlinked_under(path: Path, root: Path) -> bool:
    """True if path is outside root or a component below root is a symlink."""

    absolute = Path(os.path.abspath(os.fspath(path)))
    root_abs = Path(os.path.abspath(os.fspath(root)))
    try:
        relative = absolute.relative_to(root_abs)
    except ValueError:
        return True
    current = root_abs
    for component in relative.parts:
        current /= component
        if current.is_symlink():
            return True
    return False

def locate_quartus(quartus_root: str, repo_root: Path, lock: QuartusLock) -> tuple[Path, Path, str]:
    """Find quartus_sh below QUARTUS_ROOTDIR and authenticate it against lock.

    Returns (root, quartus_sh, version line). quartus_sh must have the lock's
    sha256, and exactly one line of `quartus_sh --version` must be the lock's
    version line.
    """
    if not quartus_root.strip():
        raise RebuildError(
            "Quartus rebuild unavailable; OSS and simulation remain usable "
            f"(set QUARTUS_ROOTDIR to an installed {lock.product} tree, as {lock.path} locks)"
        )
    root = Path(quartus_root)
    if not root.is_absolute():
        root = repo_root / root
    if any(character.isspace() for character in str(root)):
        raise RebuildError(
            f"Quartus rebuild unavailable; QUARTUS_ROOTDIR must not contain spaces: {root}"
        )
    if not root.is_dir() or root.is_symlink():
        raise RebuildError(
            f"Quartus rebuild unavailable; QUARTUS_ROOTDIR is not a directory: {root}"
        )
    for candidate in (root / "quartus" / "bin" / "quartus_sh", root / "bin" / "quartus_sh"):
        if (
            candidate.is_file()
            and not candidate.is_symlink()
            and os.access(candidate, os.X_OK)
            and not _symlinked_under(candidate, root)
        ):
            digest = _sha256(candidate)
            if digest != lock.quartus_sh_sha256:
                raise RebuildError(
                    f"Quartus rebuild unavailable; {candidate} has sha256 {digest}, "
                    f"not {lock.path}'s {lock.quartus_sh_sha256}"
                )
            version = _version_line(candidate, lock)
            # Authenticated and probed; now wait for a slot. Held for the rest
            # of this process unless the caller already holds a lease, which a
            # multi-seed runner does so it can release between seeds. Only a
            # licensed installation, read from the file itself, takes one: a
            # test that stands in for the digest check never takes a host's slot.
            if _licensed_edition(str(candidate)) is not None:
                hold_quartus_lease(lock.edition, label=lock.path)
            return root, candidate, version
    raise RebuildError(
        "Quartus rebuild unavailable; expected quartus_sh below "
        "QUARTUS_ROOTDIR/bin or QUARTUS_ROOTDIR/quartus/bin"
    )

def _version_line(quartus_sh: Path, lock: QuartusLock) -> str:
    result = subprocess.run(
        [str(quartus_sh), "--version"],
        text=True,
        capture_output=True,
        check=False,
    )
    output = (result.stdout or "") + (result.stderr or "")
    if result.returncode != 0:
        raise RebuildError(
            f"Quartus rebuild unavailable; could not execute {quartus_sh} --version"
        )
    matches = [line for line in output.splitlines() if line == lock.version]
    if len(matches) != 1:
        raise RebuildError(
            f"Quartus rebuild requires exactly one --version line {lock.version!r} ({lock.path})"
        )
    return matches[0]


# ---------------------------------------------------------------------------
# Quartus lease
#
# Each build host has, per edition class, a pool of flock slot files sized to
# its RAM. A Quartus job holds one slot for as long as any Quartus program of
# that edition runs. A slot file is never unlinked or replaced: unlinking a
# held lock file would let a second job lock a new inode at the same path.
#
# Each pool includes the lock file Lander's automation already takes as a
# whole-job mutex (Lander's seal-remote.sh and lander-quartus-remote run
# `flock <legacy path> ...`). It is the last slot tried, so new-scheme jobs
# leave it to Lander when another slot is free. A process whose ancestor holds
# a slot of the right pool, by flock(1) or by this module, already holds the
# lease: a producer that Lander runs under its flock does not wait on its own
# parent.
#
# Slot counts budget both editions against one host's RAM. Peak resident set
# sizes are in docs/design/quartus-hosts.md, measured from leased runs.
# ---------------------------------------------------------------------------

class QuartusLeaseError(RebuildError):
    """A licensed Quartus program would run without a lease."""


class QuartusLeaseTimeout(QuartusLeaseError):
    """No slot came free before the caller's deadline."""


# The quartus_sh digests the repository's quartus.lock files authenticate.
# Only these installations are leased, so test fixtures' stand-in programs
# never touch a build host's pools. tests/test_quartus_lease.py fails when a
# committed quartus.lock names a digest missing here.
LICENSED_QUARTUS_SH = {
    "71035cc10a244002d1b07bef128c7cd429f34ccd69b417c0a80f47c639d8ab93": "lite",  # Lite 17.0.2 Build 602
    "06c1bd805bc078d9636015c472e09a054160c405f3f06b7c555e7a4b6f7d3f14": "pro",   # Pro 26.3.0 Build 114
}
EDITION_CLASS = {"lite": "lite", "standard": "lite", "pro": "pro"}
LEGACY_CHIP = "/tmp/lander-quartus-3090.lock"
# Per host: slot files per edition class, most-preferred first; the legacy
# Lander lock is always last. chip's Lite and Pro pools share its legacy lock,
# which Lander takes for both editions, so a Lander job there counts against
# both. Budgets: chip 23 GB, Powerboat 62 GB shared with Deano's harness.
LEASE_POOLS = {
    "chip-30x0-ai-dev": {
        "pro": (LEGACY_CHIP,),
        "lite": ("/tmp/fes-quartus-lite-1.lock", LEGACY_CHIP),
    },
    "powerboat": {
        "pro": ("/tmp/fes-quartus-pro-1.lock", "/tmp/lander-quartus-powerboat-pro.lock"),
        "lite": ("/tmp/fes-quartus-lite-1.lock", "/tmp/fes-quartus-lite-2.lock",
                 "/tmp/lander-quartus-lite.lock"),
    },
}
# Any other host (a workstation, CI) gets one slot per edition class.
DEFAULT_POOL = {"pro": ("/tmp/fes-quartus-pro-0.lock",), "lite": ("/tmp/fes-quartus-lite-0.lock",)}
# Measured peak resident set sizes (GiB, rounded up) of one job of each edition
# class, and each host's RAM with what it keeps back for everything else; see
# docs/design/quartus-hosts.md. tests/test_quartus_lease.py checks that every
# pool fits.
PEAK_RSS_GIB = {"pro": 9, "lite": 6}
HOST_RAM_GIB = {"chip-30x0-ai-dev": (23, 8), "powerboat": (62, 24)}
# Leased programs. quartus_pfg, quartus_cpf and quartus_pgm convert or program
# an existing bitstream in seconds and a few hundred MB; they take no slot.
UNKNOWN_EDITION = "unknown"
QUARTUS_PROGRAM = re.compile(
    r"quartus_(sh|map|syn|fit|sta|asm|eda|cdb|ipgenerate|npp|pow|drc|si|tan|dse|stp|jli|staw|sim)")
LEASE_NOTICE_SECONDS = 60

# Leases this process took: quartus_lease's per thread, so parallel seeds in
# threads each hold their own slot, and hold_quartus_lease's for every thread.
_local = threading.local()
_process_held: dict[str, tuple[str, object]] = {}
_own_descriptors: set[int] = set()
# Guards _own_descriptors against /proc/self/fd: a descriptor this process
# opens or closes for its own lease must never be visible to another thread's
# inherited-slot scan without being marked as its own, or that thread would
# flock the same open file and believe it held the slot too.
_descriptor_lock = threading.Lock()
# Which thread's explicit lease uses the slot an ancestor holds, per edition.
_outer_claims: dict[str, int] = {}
_claim_lock = threading.Lock()


def _thread_held() -> dict[str, tuple[str, object]]:
    if not hasattr(_local, "held"):
        _local.held = {}
    return _local.held


def edition_class(edition: str) -> str:
    try:
        return EDITION_CLASS[edition]
    except KeyError:
        raise QuartusLeaseError(f"unknown Quartus edition {edition!r}") from None


def lease_pool(edition: str, host: str | None = None) -> tuple[str, ...]:
    """The slot files of host's pool for edition, most-preferred first."""
    host = host or socket.gethostname()
    return LEASE_POOLS.get(host, DEFAULT_POOL)[edition_class(edition)]


def _ancestors() -> set[int]:
    """This process's ancestors, not itself: a slot this process holds is one
    of its own leases, which belong to the thread that took them."""
    result, pid = set(), os.getpid()
    while pid > 1 and pid not in result:
        result.add(pid)
        try:
            with open(f"/proc/{pid}/stat", "rb") as stream:
                fields = stream.read().rsplit(b")", 1)[1].split()
            pid = int(fields[1])
        except (OSError, IndexError, ValueError):
            break
    result.discard(os.getpid())
    return result


def _file_key(path: str) -> str | None:
    """A path's /proc/locks device:inode key, or None when it does not exist."""
    try:
        info = os.stat(path)
    except OSError:
        return None
    return f"{os.major(info.st_dev):02x}:{os.minor(info.st_dev):02x}:{info.st_ino}"


def flock_holders(path: str) -> set[int]:
    """PIDs that took an exclusive flock on path's inode, from /proc/locks.

    Reading /proc/locks never contends for the lock, so a probe cannot block
    anyone. Waiters (the "->" lines) are not holders."""
    key = _file_key(path)
    if key is None:
        return set()
    holders = set()
    try:
        with open("/proc/locks", encoding="ascii", errors="replace") as stream:
            for line in stream:
                fields = line.split()
                if len(fields) >= 6 and fields[1] == "FLOCK" and fields[3] == "WRITE" and fields[5] == key:
                    holders.add(int(fields[4]))
    except OSError:
        pass
    return holders


def _inherited_slot(pool: tuple[str, ...]) -> str | None:
    """A slot this process holds through a descriptor it inherited.

    flock(1) leaves its locked descriptor open in the command it runs. A
    non-blocking flock on that same open file succeeds only when it holds the
    lock already or the lock is free; either way this process now holds it."""
    with _descriptor_lock:
        return _scan_inherited(pool)


def _scan_inherited(pool: tuple[str, ...]) -> str | None:
    keys = {_file_key(path): path for path in pool}
    try:
        names = os.listdir("/proc/self/fd")
    except OSError:
        return None
    for name in names:
        try:
            descriptor = int(name)
            key = _file_key(f"/proc/self/fd/{name}")
        except (ValueError, OSError):
            continue
        if descriptor in _own_descriptors:
            continue
        if key in keys and key is not None:
            try:
                fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except OSError:
                continue
            return keys[key]
    return None


def held_quartus_lease(edition: str) -> str | None:
    """The slot this process holds for edition, held here, inherited, or held
    by an ancestor process; None when it holds none."""
    held = _thread_held().get(edition_class(edition)) or _process_held.get(edition_class(edition))
    if held is not None:
        return held[0]
    return _outer_slot(edition)


def _outer_slot(edition: str) -> str | None:
    """A slot of edition's pool held through an inherited descriptor or by an
    ancestor process, or None."""
    pool = lease_pool(edition)
    inherited = _inherited_slot(pool)
    if inherited is not None:
        return inherited
    ancestors = _ancestors()
    for path in pool:
        if flock_holders(path) & ancestors:
            return path
    return None


def _take(edition: str, label: str, claim=None, deadline: float | None = None) -> tuple[str, object | None]:
    """Wait for a free slot of edition's pool. claim, when given, is tried on
    every round as well: it takes the ancestor's slot once no other thread of
    this process uses it, and then (outer path, None) is returned."""
    pool = lease_pool(edition)
    with _descriptor_lock:
        opened = [open(path, "a") for path in pool]
        _own_descriptors.update(stream.fileno() for stream in opened)
    chosen = None
    try:
        notice = time.monotonic() - LEASE_NOTICE_SECONDS
        while chosen is None:
            for path, stream in zip(pool, opened):
                try:
                    fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
                except BlockingIOError:
                    continue
                chosen = (path, stream)
                break
            else:
                if claim is not None:
                    claimed = claim()
                    if claimed is not None:
                        chosen = (claimed, None)
                        break
                if deadline is not None and time.monotonic() >= deadline:
                    raise QuartusLeaseTimeout(f"no {edition_class(edition)} slot came free on "
                                              f"{socket.gethostname()} before the deadline")
                if time.monotonic() - notice >= LEASE_NOTICE_SECONDS:
                    holders = {path: sorted(flock_holders(path)) for path in pool}
                    print(f"Quartus lease: waiting for a {edition_class(edition)} slot on "
                          f"{socket.gethostname()}; holders {holders}", file=sys.stderr, flush=True)
                    notice = time.monotonic()
                time.sleep(2 if deadline is None else max(0.0, min(2.0, deadline - time.monotonic())))
    finally:
        with _descriptor_lock:
            for stream in opened:
                if chosen is None or stream is not chosen[1]:
                    _own_descriptors.discard(stream.fileno())
                    stream.close()
    if chosen[1] is not None:
        print(f"Quartus lease: holding {chosen[0]} ({edition}{', ' + label if label else ''})",
              file=sys.stderr, flush=True)
    return chosen


@contextmanager
def quartus_lease(edition: str, *, label: str = "", deadline: float | None = None):
    """Hold a slot of this host's pool for edition for the block.

    Reentrant: inside a lease already held (here, inherited or by an ancestor)
    it takes nothing more and releases nothing. Released on every exit from
    the block, so a multi-seed runner takes one per seed."""
    cls = edition_class(edition)
    mine = _thread_held().get(cls) or _process_held.get(cls)
    if mine is not None:
        yield mine[0]
        return
    # A slot an ancestor holds (flock(1), a `run` wrapper) covers one job:
    # the first explicit lease in this process claims it, and a concurrent
    # one in another thread (a parallel seed) takes its own slot.
    outer = _outer_slot(edition)

    def claim():
        with _claim_lock:
            if cls in _outer_claims:
                return None
            _outer_claims[cls] = threading.get_ident()
            return outer

    # The ancestor's slot when no other thread uses it; otherwise wait for
    # either a slot of the pool or that claim, whichever frees first.
    path, stream = (claim(), None) if outer is not None else (None, None)
    if path is None:
        path, stream = _take(edition, label, claim if outer is not None else None, deadline)
    if stream is None:
        _thread_held()[cls] = (path, None)
        try:
            yield path
        finally:
            del _thread_held()[cls]
            with _claim_lock:
                del _outer_claims[cls]
        return
    held = _thread_held()
    held[edition_class(edition)] = (path, stream)
    try:
        yield path
    finally:
        del held[edition_class(edition)]
        fcntl.flock(stream, fcntl.LOCK_UN)
        with _descriptor_lock:
            _own_descriptors.discard(stream.fileno())
            stream.close()


def hold_quartus_lease(edition: str, *, label: str = "") -> str:
    """Hold a slot for edition until this process exits, unless one is held.

    A slot held this way is released by the kernel when the process exits."""
    held = held_quartus_lease(edition)
    if held is not None:
        return held
    _process_held[edition_class(edition)] = _take(edition, label)
    return _process_held[edition_class(edition)][0]


_licensed_dirs: dict[str, str | None] = {}


def _licensed_edition(program: str) -> str | None:
    """The edition of the licensed installation program belongs to, if any."""
    # Through any symlink to the real file: its installation's quartus_sh.
    directory = os.path.dirname(os.path.realpath(program))
    if directory not in _licensed_dirs:
        edition = None
        quartus_sh = os.path.join(directory, "quartus_sh")
        try:
            with open(quartus_sh, "rb") as stream:
                edition = LICENSED_QUARTUS_SH.get(hashlib.file_digest(stream, "sha256").hexdigest())
        except OSError:
            pass
        _licensed_dirs[directory] = edition
    return _licensed_dirs[directory]


SHELL_SEPARATORS = {";", "&&", "||", "|", "&", "(", ")", ";;", "|&", "{", "}", "!", "\n"}
SHELL_KEYWORDS = {"if", "then", "else", "elif", "do", "while", "until", "!", "time", "{", "fi", "done"}
SHELL_WRAPPERS = {"exec", "command", "builtin", "env", "nohup", "timeout", "nice", "xargs", "time"}


def _expanded_command(tokens: list[str]) -> bool:
    """Whether shell code runs a command whose name comes from an expansion
    ($Q, ${Q}sh, `...`, $(...), a glob like ./quartus_[f]it, a brace
    expansion like quartus_{fit,fit}) or from eval:
    its program cannot be known here, so it is treated as possibly either
    Quartus edition."""
    at_head, wrapped = True, False
    skip = 0
    for index, token in enumerate(tokens):
        if skip:
            skip -= 1
            continue
        if token in SHELL_SEPARATORS:
            at_head, wrapped = True, False
            continue
        # A redirection (>log, 2>&1, <in) and its operand may come before the
        # command word without taking its place.
        if re.fullmatch(r"[<>]+[&|]?|&>>?", token):
            skip = 1
            continue
        if token.isdigit() and index + 1 < len(tokens) and re.fullmatch(r"[<>]+[&|]?", tokens[index + 1]):
            continue
        # Parameter and command expansion, and pathname expansion (globs):
        # the shell, not this text, decides what such a word names.
        expanded = "$" in token or "`" in token or any(c in token for c in "*?[{")
        if wrapped and expanded:
            # Under a wrapper (env -u X $Q, timeout -s KILL 5 $Q) the command
            # is somewhere among its arguments: any expansion may be it.
            return True
        if token in SHELL_KEYWORDS:
            at_head, wrapped = True, False  # then, do, else...: a command follows
            continue
        if not at_head:
            continue
        if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*=.*", token, re.S):
            if expanded:
                continue  # an assignment before the command word
            continue
        if token in SHELL_WRAPPERS:
            wrapped = True
            continue
        at_head = False
        if token == "eval" or expanded:
            return True
    return False


SHELLS = {"sh", "bash", "dash", "zsh", "ksh", "mksh", "busybox", "ash"}


def _shell_payloads(texts: list[str], whole: bool) -> set[int]:
    """Indexes of the arguments a shell will run as code: a command string
    (shell=True, os.system), the argument after -c of a shell, and the
    remote command words of ssh."""
    if whole:
        return {0}
    found = set()
    names = [os.path.basename(t) for t in texts]
    for index, text in enumerate(texts):
        if re.fullmatch(r"-[A-Za-z]*c[A-Za-z]*", text) and any(n in SHELLS for n in names[:index]) \
                and index + 1 < len(texts):
            found.add(index + 1)
    if names and names[0] == "ssh":
        found.update(i for i, t in enumerate(texts) if i > 0 and any(c.isspace() for c in t))
    return found


def licensed_quartus_editions(argv, search_path: str | None = None, cwd=None) -> set[str]:
    """Editions of the licensed Quartus programs a command line would run.

    Looks at every argument, and every word of an argument, so a wrapper such
    as run_logged.sh or `bash -c` is seen through. A `--version` or `--help`
    probe needs no lease. A bare program name resolves through PATH, and a
    relative path against cwd (the child's working directory)."""
    whole = isinstance(argv, (str, bytes))
    argv = [argv] if isinstance(argv, (str, bytes, os.PathLike)) else list(argv or ())
    texts = [os.fsdecode(a) if isinstance(a, (bytes, os.PathLike)) else str(a) for a in argv]
    payloads = _shell_payloads(texts, whole)
    words, element = [], 0
    for position, text in enumerate(texts):
        if position in payloads:
            # A newline ends a command in shell code, as ; does; a
            # backslash-newline is a line continuation, which the shell removes.
            text = text.replace("\\\n", "").replace("\n", " ; ")
        # Split as the shell does, operators included, so a program name
        # with a redirection or separator attached (quartus_sh>log,
        # quartus_sh&) is still seen.
        lexer = shlex.shlex(text, posix=True, punctuation_chars=True)
        lexer.whitespace_split = True
        try:
            tokens = list(lexer)
        except ValueError:
            tokens = re.split(r"[\s;&|<>()]+", text)
        shell_code = element in payloads
        words.extend((element, token, shell_code) for token in tokens)
        if shell_code and _expanded_command(tokens):
            words.append((element, UNKNOWN_EDITION, True))
        element += 1
    probes = ("--version", "-v", "--help", "-h")
    argv_list = [w for w in words]
    editions = set()
    for index, (element, word, in_payload) in enumerate(argv_list):
        if word == UNKNOWN_EDITION:
            editions.add(UNKNOWN_EDITION)
            continue
        # NAME=value in shell code can be run later as $NAME: the value is a
        # program name like any other.
        # (in shell code, or as an argument of env and the like).
        assigned = re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*=(.+)", word, re.S)
        if assigned:
            word = assigned.group(1)
        if not QUARTUS_PROGRAM.fullmatch(os.path.basename(word)):
            continue
        # A probe excuses a program only within one argument (the next word
        # of the same string), or when the program is the one being executed
        # (the first argument) and the next argument is the probe. A --help
        # elsewhere in argv never excuses a program named in another place.
        following = argv_list[index + 1] if index + 1 < len(argv_list) else (None, None, False)
        if following[1] in probes and (following[0] == element or (element == 0 and index == 0)):
            continue
        if in_payload and not os.path.isabs(word):
            # A relative or bare name inside shell code is resolved after
            # whatever the code does first (cd, PATH=...), which this cannot
            # follow: it needs a lease of some edition.
            editions.add(UNKNOWN_EDITION)
        base = os.fsdecode(cwd) if cwd is not None else os.getcwd()
        if os.sep in word:
            candidates = [word if os.path.isabs(word) else os.path.join(base, word)]
        else:
            # Every executable of that name on the child's PATH, each
            # component (an empty one is ".") resolved from the child's
            # working directory. Which one execvp ends up running depends on
            # failures it skips (a missing #! interpreter or ELF loader, a
            # noexec mount), so every licensed one needs its lease. A bare
            # name with both editions on PATH needs both; use a full path.
            search = os.environ.get("PATH", "") if search_path is None else search_path
            candidates = [os.path.join(d if os.path.isabs(d) else os.path.join(base, d or "."), word)
                          for d in search.split(os.pathsep)]
        for program in candidates:
            if os.path.isfile(program) and os.access(program, os.X_OK):
                edition = _licensed_edition(program)
                if edition is not None:
                    editions.add(edition)
    return editions


def require_quartus_lease(argv, search_path: str | None = None, cwd=None) -> None:
    """Raise QuartusLeaseError unless every licensed Quartus program argv would
    run has a lease held for its edition (both editions, for a program named
    in shell code that cannot be resolved here)."""
    for edition in sorted(licensed_quartus_editions(argv, search_path, cwd)):
        if edition == UNKNOWN_EDITION:
            # It may resolve to either installation: both leases, or a full path.
            if held_quartus_lease("lite") is None or held_quartus_lease("pro") is None:
                raise QuartusLeaseError(
                    "refusing to run a Quartus program named by a relative path in shell code: it may be "
                    "either edition. Give its full path, or hold both editions' leases")
            continue
        if held_quartus_lease(edition) is None:
            raise QuartusLeaseError(
                f"refusing to start licensed Quartus {edition} without a lease on "
                f"{socket.gethostname()}: take quartus_tools.quartus_lease({edition!r}) or run "
                f"under scripts/quartus_jobs.py run --edition {edition}")


def _check_remote_quartus(argv) -> None:
    """ssh runs its command on another host, whose lease this process cannot
    hold: a remote command naming a leased Quartus program is refused unless
    it runs under `quartus_jobs.py run` there (as `quartus_jobs.py remote`
    arranges, passing its program on stdin)."""
    if isinstance(argv, (str, bytes, os.PathLike)):
        argv = [argv]
    texts = [os.fsdecode(a) if isinstance(a, (bytes, os.PathLike)) else str(a) for a in argv]
    # Through execution wrappers (env [-i] [NAME=value], timeout N, nice -n N,
    # nohup, exec...) to the command they run.
    # Wrapper options take operands of their own (env -u NAME, timeout -s
    # SIG), so rather than model each, a wrapped command line whose
    # arguments include ssh is taken to run ssh from there.
    if texts and (os.path.basename(texts[0]) in SHELL_WRAPPERS or texts[0] == "sudo"):
        at = next((i for i, t in enumerate(texts) if os.path.basename(t) == "ssh"), None)
        texts = texts[at:] if at is not None else []
    if not texts or os.path.basename(texts[0]) != "ssh":
        return
    # ssh [options] destination [command [argument ...]]: skip the options
    # (and their operands) and the destination.
    index = 1
    while index < len(texts) and texts[index].startswith("-"):
        option = texts[index]
        index += 2 if (len(option) == 2 and option[1] in "BbcDEeFIiJLlmOoPpQRSWw") else 1
    remote = texts[index + 1:]
    tokens = []
    for text in remote:
        lexer = shlex.shlex(text.replace("\\\n", "").replace("\n", " ; "), posix=True, punctuation_chars=True)
        lexer.whitespace_split = True
        try:
            tokens.extend(lexer)
        except ValueError:
            tokens.extend(text.split())
    # Simple commands, split at shell separators.
    commands, command = [], []
    for token in tokens:
        if token in SHELL_SEPARATORS:
            commands.append(command)
            command = []
        else:
            command.append(token)
    commands.append(command)
    for command in commands:
        # The one exemption: a command that is itself `[python3] quartus_jobs.py run ... -- PROGRAM`.
        words = [w for w in command if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*=.*", w)]
        start = 1 if words and os.path.basename(words[0]).startswith("python") else 0
        wrapped = (len(words) > start + 1 and os.path.basename(words[start]) == "quartus_jobs.py"
                   and words[start + 1] == "run" and "--" in words)
        if not wrapped and _expanded_command(command):
            # The remote shell decides what an expanded command word names.
            raise QuartusLeaseError(
                "refusing an ssh remote command whose program comes from an expansion: run Quartus on "
                "another host through scripts/quartus_jobs.py remote")
        for index, token in enumerate(command):
            if not QUARTUS_PROGRAM.fullmatch(os.path.basename(token)):
                continue
            if index + 1 < len(command) and command[index + 1] in ("--version", "-v", "--help", "-h"):
                continue
            if wrapped and index > command.index("--"):
                continue
            raise QuartusLeaseError(
                f"refusing to run {token} on a remote host outside its lease: use scripts/quartus_jobs.py "
                "remote, or run it under quartus_jobs.py run there")


def _check_launch(argv, env, cwd=None, executable=None) -> None:
    """argv, and each value of the environment the child gets (its own, or
    this process's when none is given): a shell can run a program it finds
    there ($Q --flow compile). Each environment value is checked on its own,
    so a --help in one cannot excuse a program in another. Bare names resolve
    on the child's PATH, relative paths against its working directory."""
    _check_remote_quartus(argv)
    env = os.environ if env is None else env
    sources = [argv] + [[os.fsdecode(value)] for value in env.values() if isinstance(value, (str, bytes))]
    first = argv if isinstance(argv, (str, bytes, os.PathLike)) else (list(argv)[:1] or [None])[0]
    if executable is not None and (first is None or os.fsdecode(executable) != os.fsdecode(first)):
        # The program actually executed, when it is not argv[0].
        sources.append([os.fsdecode(executable)])
    search = ""
    for key in ("PATH", b"PATH"):
        try:
            search = env[key]
            break
        except (KeyError, TypeError):
            continue
    for source in sources:
        require_quartus_lease(source, search_path=os.fsdecode(search) if search else "", cwd=cwd)


def _launch_guard(event, args):
    if event == "subprocess.Popen":
        _check_launch(args[1] if args[1] is not None else args[0], args[3], args[2], executable=args[0])
    elif event in ("os.exec", "os.posix_spawn"):
        _check_launch(args[1], args[2] if len(args) > 2 else None, executable=args[0])
    elif event == "os.spawn":
        _check_launch(args[2], args[3] if len(args) > 3 else None, executable=args[1])
    elif event == "os.system":
        _check_launch(args[0], None)


sys.addaudithook(_launch_guard)
