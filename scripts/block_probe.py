"""Decide whether a block device may be opened, using only sysfs.

Optical nodes (``sr*`` or SCSI type 5), removable devices with no media, and
any other zero-capacity device are refused before an open. A normal USB stick
and the SD/FESDATA card stay usable.
"""

from __future__ import annotations

import fcntl
import os
from pathlib import Path
import threading


OPTICAL = "optical"
NO_MEDIA = "no-media"
ZERO = "zero-capacity"
OPEN_TIMEOUT = 3.0


class UnsafeDevice(Exception):
    def __init__(self, reason: str, name: str):
        super().__init__(f"{reason} block device {name}")
        self.reason = reason
        self.name = name


def _read(path: Path) -> str | None:
    try:
        return path.read_text().strip()
    except OSError:
        return None


def _parent(name: str) -> str:
    if name.startswith(("mmcblk", "nvme")) and "p" in name:
        base, _, part = name.rpartition("p")
        if base and part.isdigit():
            return base
        return ""
    end = len(name)
    while end > 0 and name[end - 1].isdigit():
        end -= 1
    if end == len(name) or end == 0:
        return ""
    base = name[:end]
    if base in {"sd", "sr", "hd", "vd", "xvd", "loop"}:
        return ""
    return base


def _optical_name(name: str) -> bool:
    return name.startswith("sr") and name[2:].isdigit() and name[2:] != ""


def _device_dir(block_dir: Path, name: str) -> Path | None:
    direct = block_dir / name
    if direct.is_dir():
        return direct
    parent = _parent(name)
    if parent:
        nested = block_dir / parent / name
        if nested.is_dir():
            return nested
    return None


def reason(block_dir: Path | str, name: str) -> str:
    """Return a skip reason, or '' when name may be opened.

    ``block_dir`` is ``/sys/class/block`` or ``/sys/block``. An unknown name
    that is not ``sr*`` returns ''.
    """

    name = Path(name).name
    block_dir = Path(block_dir)
    if _optical_name(name) and _device_dir(block_dir, name) is None:
        return OPTICAL
    directory = _device_dir(block_dir, name)
    if directory is None:
        return OPTICAL if _optical_name(name) else ""
    if _optical_name(name):
        return OPTICAL
    scsi = _read(directory / "device" / "type")
    if scsi is None:
        parent = _parent(name)
        parent_dir = _device_dir(block_dir, parent) if parent else None
        if parent_dir is not None:
            scsi = _read(parent_dir / "device" / "type")
    if scsi == "5":
        return OPTICAL
    size = _read(directory / "size")
    removable = _read(directory / "removable")
    if removable is None:
        parent = _parent(name)
        parent_dir = _device_dir(block_dir, parent) if parent else None
        if parent_dir is not None:
            removable = _read(parent_dir / "removable")
    if size == "0" and removable == "1":
        return NO_MEDIA
    if size == "0":
        return ZERO
    return ""


def reason_for(path: str, sysfs: str = "/sys") -> str:
    """Classify a ``/dev`` node. Other paths are left usable."""

    resolved = path
    try:
        resolved = os.path.realpath(path)
    except OSError:
        pass
    if not (path == "/dev" or path.startswith("/dev/") or resolved.startswith("/dev/")):
        return ""
    name = Path(resolved).name
    root = Path(sysfs)
    for relative in ("class/block", "block"):
        block = root / relative
        if _device_dir(block, name) is not None or _optical_name(name):
            return reason(block, name)
    return OPTICAL if _optical_name(name) else ""


def require(path: str, sysfs: str = "/sys") -> None:
    found = reason_for(path, sysfs=sysfs)
    if found:
        raise UnsafeDevice(found, Path(path).name)


def open_binary(path: str, mode: str = "rb", timeout: float = OPEN_TIMEOUT, sysfs: str = "/sys"):
    """Open a file. A refused block device is not opened."""

    path = str(path)
    if path.startswith("/dev/"):
        require(path, sysfs=sysfs)
        return _open_timeout(path, mode, timeout)
    return Path(path).open(mode)


def _open_timeout(path: str, mode: str, timeout: float):
    flags = os.O_CLOEXEC
    if "r" in mode and "+" not in mode:
        flags |= os.O_RDONLY
    elif "w" in mode or "a" in mode or "+" in mode:
        flags |= os.O_RDWR if "+" in mode or "r" in mode else os.O_WRONLY
    outcome: dict = {}
    state = {"abandoned": False}
    lock = threading.Lock()

    def run() -> None:
        try:
            opened = os.open(path, flags | os.O_NONBLOCK)
        except OSError as error:
            with lock:
                outcome["err"] = error
            return
        with lock:
            if state["abandoned"]:
                os.close(opened)
                return
            outcome["fd"] = opened

    worker = threading.Thread(target=run, daemon=True)
    worker.start()
    worker.join(timeout)
    with lock:
        if worker.is_alive():
            state["abandoned"] = True
            late = outcome.pop("fd", None)
            if late is not None:
                os.close(late)
            raise TimeoutError(f"opening {path} timed out after {timeout}s")
        if "err" in outcome:
            raise outcome["err"]
        fd = outcome["fd"]
    try:
        # The open used O_NONBLOCK so a not-ready node cannot stall the
        # caller. Later reads and writes of an admitted USB stick must block
        # the way a normal open does, or a large copy returns EAGAIN.
        flags = fcntl.fcntl(fd, fcntl.F_GETFL)
        fcntl.fcntl(fd, fcntl.F_SETFL, flags & ~os.O_NONBLOCK)
        return os.fdopen(fd, mode)
    except OSError:
        os.close(fd)
        raise
