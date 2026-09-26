"""Stand-in executables for the canonical OSS tool prefix.

``collect_manifest`` reopens
``build/toolchain/install/bin/{yosys,nextpnr-mistral}`` under the repository.
A shared compiler cache never creates that prefix. Mailbox provenance tests
install a tiny executable there when the real tool is absent, and remove only
the files this module created.
"""

from __future__ import annotations

import atexit
import os
from pathlib import Path


STUB = (
    "#!/bin/sh\n"
    "# misteross unit-test stand-in; removed when the suite finishes\n"
    "exit 0\n"
)
_CREATED: list[Path] = []
_CLEANUP_REGISTERED = False


def _has_symlink_component(path: Path) -> bool:
    current = Path(path.anchor)
    for part in path.parts[1:]:
        current /= part
        if current.is_symlink():
            return True
    return False


def _is_stub(path: Path) -> bool:
    try:
        return path.is_file() and not path.is_symlink() and path.read_text(encoding="utf-8") == STUB
    except OSError:
        return False


def _register_cleanup() -> None:
    global _CLEANUP_REGISTERED
    if not _CLEANUP_REGISTERED:
        atexit.register(cleanup_canonical_oss_tools)
        _CLEANUP_REGISTERED = True


def ensure_canonical_oss_tools(root: Path) -> dict[str, Path]:
    """Return yosys and nextpnr paths, creating stand-ins when absent."""

    _register_cleanup()
    bin_dir = root / "build" / "toolchain" / "install" / "bin"
    paths: dict[str, Path] = {}
    for name in ("yosys", "nextpnr-mistral"):
        path = bin_dir / name
        if (
            path.is_file()
            and not path.is_symlink()
            and os.access(path, os.X_OK)
            and not _has_symlink_component(path)
            and not _is_stub(path)
        ):
            paths[name] = path
            continue
        if _is_stub(path):
            if path not in _CREATED:
                _CREATED.append(path)
            paths[name] = path
            continue
        if path.exists() or path.is_symlink() or _has_symlink_component(bin_dir):
            paths[name] = path
            continue
        bin_dir.mkdir(parents=True, exist_ok=True)
        path.write_text(STUB, encoding="utf-8")
        path.chmod(0o755)
        _CREATED.append(path)
        paths[name] = path
    return paths


def cleanup_canonical_oss_tools() -> None:
    """Remove stand-ins created or adopted by :func:`ensure_canonical_oss_tools`."""

    created = list(_CREATED)
    _CREATED.clear()
    for path in created:
        try:
            if _is_stub(path):
                path.unlink()
        except OSError:
            pass
