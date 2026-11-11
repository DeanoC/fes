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
"""
import hashlib
import os
import re
import subprocess
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
            return root, candidate, _version_line(candidate, lock)
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
