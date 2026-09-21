"""Load and resolve the immutable inputs for a native boot-media image."""
from dataclasses import dataclass
import fcntl
import hashlib
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import tomllib


EXPECTED_LAYOUT = "de10-nano-mister-v1"
# core=menu.rbf is the locked U-Boot filename for the splash slot.
EXPECTED_ENVIRONMENT = (
    "mmcroot=/dev/mmcblk0p1",
    "bootimage=/linux/zImage_dtb",
    "core=menu.rbf",
    "loop=linux/linux.img ro rootwait",
)


@dataclass(frozen=True)
class Partition:
    start_sector: int
    sector_count: int
    type: int
    active: bool
    chs_start: str
    chs_end: str


@dataclass(frozen=True)
class Payload:
    path: str
    size: int
    sha256: str


@dataclass(frozen=True)
class MediaLock:
    format: int
    repository: str
    commit: str
    layout: str
    sector_size: int
    total_sectors: int
    disk_id: int
    fat_serial: int
    fat_label: str
    partition_1: Partition
    partition_2: Partition
    uboot: Payload
    kernel: Payload
    environment: tuple[str, ...]

    @classmethod
    def load(cls, path: Path):
        return cls.loads(Path(path).read_text())

    @classmethod
    def loads(cls, text: str):
        try:
            data = tomllib.loads(text)
        except tomllib.TOMLDecodeError as error:
            raise ValueError(f"invalid boot-media lock: {error}") from error
        _fields(data, {"format", "repository", "commit", "layout", "sector_size",
                       "total_sectors", "disk_id", "fat_serial", "fat_label",
                       "partition_1", "partition_2", "uboot", "kernel"})
        partition_1 = _partition(data["partition_1"])
        partition_2 = _partition(data["partition_2"])
        uboot, environment = _uboot(data["uboot"])
        kernel = _payload(data["kernel"], "zImage_dtb")
        lock = cls(data["format"], data["repository"], data["commit"], data["layout"],
                   data["sector_size"], data["total_sectors"], data["disk_id"],
                   data["fat_serial"], data["fat_label"], partition_1, partition_2,
                   uboot, kernel, environment)
        _validate(lock)
        return lock


@dataclass(frozen=True)
class Payloads:
    uboot: Path
    kernel: Path


def _fields(data, expected):
    if not isinstance(data, dict):
        raise ValueError("boot-media lock table is invalid")
    unknown = set(data) - expected
    if unknown:
        raise ValueError("unknown boot-media lock field: " + sorted(unknown)[0])
    missing = expected - set(data)
    if missing:
        raise ValueError("missing boot-media lock field: " + sorted(missing)[0])


def _partition(data):
    fields = {"start_sector", "sector_count", "type", "active", "chs_start", "chs_end"}
    _fields(data, fields)
    _types(data, {"start_sector": int, "sector_count": int, "type": int,
                  "active": bool, "chs_start": str, "chs_end": str})
    return Partition(**data)


def _payload(data, expected_path):
    _fields(data, {"path", "size", "sha256"})
    _types(data, {"path": str, "size": int, "sha256": str})
    payload = Payload(**data)
    if payload.path != expected_path:
        raise ValueError(f"boot-media payload path must be {expected_path}")
    if type(payload.size) is not int or payload.size <= 0:
        raise ValueError(f"{expected_path} size is invalid")
    if not isinstance(payload.sha256, str) or not re.fullmatch(r"[0-9a-f]{64}", payload.sha256):
        raise ValueError(f"{expected_path} digest is invalid")
    return payload


def _uboot(data):
    _fields(data, {"path", "size", "sha256", "environment"})
    payload = _payload({key: value for key, value in data.items() if key != "environment"}, "uboot.img")
    environment = data["environment"]
    if (type(environment) is not list or any(type(value) is not str for value in environment)
            or tuple(environment) != EXPECTED_ENVIRONMENT):
        raise ValueError("U-Boot environment strings differ from boot-media lock policy")
    return payload, tuple(environment)


def _validate(lock):
    if type(lock.format) is not int or lock.format != 1:
        raise ValueError("boot-media lock format must be 1")
    if type(lock.repository) is not str or not lock.repository:
        raise ValueError("boot-media repository is invalid")
    if type(lock.commit) is not str or not re.fullmatch(r"[0-9a-f]{40}", lock.commit):
        raise ValueError("boot-media commit is invalid")
    if type(lock.layout) is not str or lock.layout != EXPECTED_LAYOUT:
        raise ValueError("boot-media layout differs from policy")
    if (type(lock.sector_size) is not int or type(lock.total_sectors) is not int
            or lock.sector_size != 512 or lock.total_sectors != 528384):
        raise ValueError("boot-media sector geometry differs from policy")
    if (type(lock.disk_id) is not int or type(lock.fat_serial) is not int
            or type(lock.fat_label) is not str or lock.disk_id != 0x46455331
            or lock.fat_serial != 0xf35d0001 or lock.fat_label != "FESDATA"):
        raise ValueError("boot-media disk identifiers differ from policy")
    expected = (
        (lock.partition_1, 2048, 524288, 0x0c, True),
        (lock.partition_2, 526336, 2048, 0xa2, False),
    )
    for partition, start, count, type_code, active in expected:
        if (partition.start_sector, partition.sector_count, partition.type, partition.active,
                partition.chs_start, partition.chs_end) != (start, count, type_code, active, "feffff", "feffff"):
            raise ValueError("boot-media partition geometry differs from policy")


def _types(data, expected):
    for field, field_type in expected.items():
        if type(data[field]) is not field_type:
            raise ValueError(f"boot-media lock {field} has an invalid type")


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify_file(path, expected_size, expected_sha, label):
    path = Path(path)
    if not path.is_file() or path.is_symlink():
        raise ValueError(f"{label} is not a regular file")
    if path.stat().st_size != expected_size or digest(path) != expected_sha:
        raise ValueError(f"{label} digest or size differs from boot-media lock")


def _git(cache, *args):
    return subprocess.check_output(["git", "-C", str(cache), *args], text=True).strip()


def _validate_cache(cache, lock):
    try:
        if _git(cache, "rev-parse", "--is-inside-work-tree") != "true":
            raise ValueError("image-creator cache is not a Git worktree")
        if subprocess.run(["git", "-C", str(cache), "symbolic-ref", "-q", "HEAD"],
                          stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0:
            raise ValueError("image-creator cache must use a detached revision")
        if _git(cache, "rev-parse", "HEAD") != lock.commit:
            raise ValueError("image-creator cache revision differs from boot-media lock")
    except subprocess.CalledProcessError as error:
        raise ValueError("image-creator cache is not a valid locked checkout") from error


def _require_clean(cache):
    if _git(cache, "status", "--porcelain", "--untracked-files=all", "--ignored"):
        raise ValueError("image-creator cache is changed")


def _populate_cache(cache, lock, run):
    temporary_parent = Path(tempfile.mkdtemp(prefix=cache.name + ".tmp-", dir=cache.parent))
    temporary = temporary_parent / "repository"
    try:
        run(["git", "clone", "--no-checkout", lock.repository, temporary])
        subprocess.run(["git", "-C", str(temporary), "checkout", "--detach", lock.commit], check=True)
        _validate_cache(temporary, lock)
        _require_clean(temporary)
        temporary.replace(cache)
    finally:
        shutil.rmtree(temporary_parent, ignore_errors=True)


def resolve_payloads(root: Path, lock: MediaLock, run):
    root = Path(root)
    cache = root / "out/work/boot-media" / ("image-creator-" + lock.commit)
    if cache.is_symlink():
        raise ValueError("image-creator cache path must not be a symlink")
    cache.parent.mkdir(parents=True, exist_ok=True)
    lock_path = cache.parent / (cache.name + ".lock")
    with lock_path.open("a+") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        if not cache.exists():
            _populate_cache(cache, lock, run)
        _validate_cache(cache, lock)
        uboot = cache / lock.uboot.path
        kernel = cache / lock.kernel.path
        verify_file(uboot, lock.uboot.size, lock.uboot.sha256, "uboot.img")
        verify_file(kernel, lock.kernel.size, lock.kernel.sha256, "zImage_dtb")
        uboot_bytes = uboot.read_bytes()
        for value in lock.environment:
            if value.encode() not in uboot_bytes:
                raise ValueError("U-Boot environment string differs from boot-media lock: " + value)
        _require_clean(cache)
        return Payloads(uboot, kernel)
