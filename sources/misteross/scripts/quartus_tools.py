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
QUARTUS_PROGRAM = re.compile(r"quartus_(?!pfg$|cpf$|pgm$|pgmw$)[a-z]+")
LEASE_NOTICE_SECONDS = 60

# Leases this process took: quartus_lease's per thread, so parallel seeds in
# threads each hold their own slot, and hold_quartus_lease's for every thread.
_local = threading.local()
_process_held: dict[str, tuple[str, object]] = {}
_own_descriptors: set[int] = set()


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
    pool = lease_pool(edition)
    inherited = _inherited_slot(pool)
    if inherited is not None:
        return inherited
    ancestors = _ancestors()
    for path in pool:
        if flock_holders(path) & ancestors:
            return path
    return None


def _take(edition: str, label: str) -> tuple[str, object]:
    pool = lease_pool(edition)
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
                if time.monotonic() - notice >= LEASE_NOTICE_SECONDS:
                    holders = {path: sorted(flock_holders(path)) for path in pool}
                    print(f"Quartus lease: waiting for a {edition_class(edition)} slot on "
                          f"{socket.gethostname()}; holders {holders}", file=sys.stderr, flush=True)
                    notice = time.monotonic()
                time.sleep(2)
    finally:
        for stream in opened:
            if chosen is None or stream is not chosen[1]:
                _own_descriptors.discard(stream.fileno())
                stream.close()
    print(f"Quartus lease: holding {chosen[0]} ({edition}{', ' + label if label else ''})",
          file=sys.stderr, flush=True)
    return chosen


@contextmanager
def quartus_lease(edition: str, *, label: str = ""):
    """Hold a slot of this host's pool for edition for the block.

    Reentrant: inside a lease already held (here, inherited or by an ancestor)
    it takes nothing more and releases nothing. Released on every exit from
    the block, so a multi-seed runner takes one per seed."""
    held = held_quartus_lease(edition)
    if held is not None:
        yield held
        return
    path, stream = _take(edition, label)
    held = _thread_held()
    held[edition_class(edition)] = (path, stream)
    try:
        yield path
    finally:
        del held[edition_class(edition)]
        fcntl.flock(stream, fcntl.LOCK_UN)
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
    directory = os.path.dirname(os.path.abspath(program))
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


def licensed_quartus_editions(argv) -> set[str]:
    """Editions of the licensed Quartus programs a command line would run.

    Looks at every argument, and every word of an argument, so a wrapper such
    as run_logged.sh or `bash -c` is seen through. A `--version` or `--help`
    probe needs no lease. A bare program name resolves through PATH."""
    if isinstance(argv, (str, bytes, os.PathLike)):
        argv = [argv]
    words = []
    for argument in argv or ():
        text = os.fsdecode(argument) if isinstance(argument, (bytes, os.PathLike)) else str(argument)
        try:
            words.extend(shlex.split(text))
        except ValueError:
            words.extend(text.split())
    editions = set()
    for index, word in enumerate(words):
        if not QUARTUS_PROGRAM.fullmatch(os.path.basename(word)):
            continue
        if index + 1 < len(words) and words[index + 1] in ("--version", "-v", "--help", "-h"):
            continue
        program = word if os.sep in word else next(
            (os.path.join(d, word) for d in os.environ.get("PATH", "").split(os.pathsep)
             if d and os.path.isfile(os.path.join(d, word))), None)
        edition = _licensed_edition(program) if program else None
        if edition is not None:
            editions.add(edition)
    return editions


def require_quartus_lease(argv) -> None:
    """Raise QuartusLeaseError unless every licensed Quartus program argv would
    run has a lease held for its edition."""
    for edition in sorted(licensed_quartus_editions(argv)):
        if held_quartus_lease(edition) is None:
            raise QuartusLeaseError(
                f"refusing to start licensed Quartus {edition} without a lease on "
                f"{socket.gethostname()}: take quartus_tools.quartus_lease({edition!r}) or run "
                f"under scripts/quartus_jobs.py run --edition {edition}")


def _launch_guard(event, args):
    if event == "subprocess.Popen":
        require_quartus_lease(args[1] if args[1] is not None else args[0])
    elif event in ("os.exec", "os.posix_spawn"):
        require_quartus_lease(args[1])
    elif event == "os.spawn":
        require_quartus_lease(args[2])
    elif event == "os.system":
        require_quartus_lease(args[0])


sys.addaudithook(_launch_guard)
