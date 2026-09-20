#!/usr/bin/env python3
"""Copy the upstream or rebuild RBF to build/current/ for operator selection."""

from __future__ import annotations

import argparse
import hashlib
import shutil
import sys
from pathlib import Path

try:
    from .core_lock import DEFAULT_LOCK, CoreLockError, CorePin, load_lock
except ImportError:  # pragma: no cover - CLI script entry
    from core_lock import DEFAULT_LOCK, CoreLockError, CorePin, load_lock

ROOT = Path(__file__).resolve().parents[1]
ARTIFACTS = ("upstream", "rebuild")


class SelectError(RuntimeError):
    """Raised when a core artifact cannot be selected."""


def _hash_file(path: Path) -> tuple[str, int]:
    data = path.read_bytes()
    return hashlib.sha256(data).hexdigest(), len(data)


def select_artifact(pin: CorePin, root: Path, artifact: str) -> Path:
    if artifact not in ARTIFACTS:
        raise SelectError(f"unknown artifact {artifact}; use upstream or rebuild")
    if artifact == "upstream":
        source = root / "build" / "cores" / pin.name / pin.rbf_path
        if not source.is_file():
            raise SelectError(
                f"missing upstream RBF; run make fetch-core CORE={pin.name} first: {source}"
            )
        digest, size = _hash_file(source)
        if digest != pin.rbf_sha256 or size != pin.rbf_size:
            raise SelectError(
                f"upstream RBF does not match lock: sha256 {digest} size {size}"
            )
    else:
        source = root / "build" / "rebuild" / pin.name / f"{pin.name}.rbf"
        if not source.is_file():
            raise SelectError(
                f"missing rebuild RBF; run make rebuild-core CORE={pin.name} first: {source}"
            )
        digest, size = _hash_file(source)
    dest_dir = root / "build" / "current"
    dest_dir.mkdir(parents=True, exist_ok=True)
    dest = dest_dir / f"{pin.name}.rbf"
    shutil.copy2(source, dest)
    (dest_dir / f"{pin.name}.artifact").write_text(artifact + "\n", encoding="utf-8")
    print(f"ok {pin.name} {artifact} sha256 {digest} size {size} -> {dest}")
    return dest


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--core", default="megadrive")
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--artifact", choices=ARTIFACTS, default="rebuild")
    args = parser.parse_args(argv)
    try:
        pins = load_lock(args.lock)
        if args.core not in pins:
            raise SelectError(f"unknown core {args.core}")
        select_artifact(pins[args.core], args.root, args.artifact)
    except (CoreLockError, SelectError) as exc:
        print(f"select-core: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
