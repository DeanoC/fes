#!/usr/bin/env python3
"""Check out a pinned core tree and hash the locked release RBF."""

from __future__ import annotations

import argparse
import hashlib
import subprocess
import sys
from pathlib import Path

try:
    from .core_lock import DEFAULT_LOCK, CoreLockError, CorePin, load_lock
except ImportError:  # pragma: no cover - CLI script entry
    from core_lock import DEFAULT_LOCK, CoreLockError, CorePin, load_lock

ROOT = Path(__file__).resolve().parents[1]


class FetchError(RuntimeError):
    """Raised when a core checkout or hash check fails."""


def _run_git(args: list[str], cwd: Path | None = None) -> None:
    result = subprocess.run(
        ["git", *args],
        cwd=cwd,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise FetchError(detail or f"git {' '.join(args)} failed")


def checkout_pin(pin: CorePin, source_dir: Path) -> None:
    source_dir.parent.mkdir(parents=True, exist_ok=True)
    if not source_dir.exists():
        print(f"==> cloning {pin.name} at {pin.commit}", flush=True)
        clone = ["clone", "--no-checkout"]
        if pin.repo.startswith("https://"):
            clone[1:1] = ["--filter=blob:none"]
        _run_git([*clone, pin.repo, str(source_dir)])
        _run_git(["fetch", "--no-tags", "origin", pin.commit], cwd=source_dir)
        _run_git(["checkout", "--detach", pin.commit], cwd=source_dir)
    else:
        git_dir = source_dir / ".git"
        if not git_dir.exists():
            raise FetchError(f"source path exists but is not a Git checkout: {source_dir}")
        dirty = subprocess.run(
            ["git", "status", "--porcelain=v1", "--untracked-files=all"],
            cwd=source_dir,
            text=True,
            capture_output=True,
            check=False,
        )
        if dirty.returncode != 0:
            raise FetchError(dirty.stderr.strip() or "git status failed")
        if dirty.stdout.strip():
            raise FetchError(
                f"source checkout is dirty; clean it manually before fetch: {source_dir}"
            )
        head = subprocess.run(
            ["git", "rev-parse", "HEAD"],
            cwd=source_dir,
            text=True,
            capture_output=True,
            check=True,
        ).stdout.strip()
        if head != pin.commit:
            raise FetchError(
                f"source checkout {pin.name} is at {head}, expected locked commit {pin.commit}; move it manually"
            )

    gitmodules = source_dir / ".gitmodules"
    if gitmodules.is_file():
        _run_git(["submodule", "sync", "--recursive"], cwd=source_dir)
        _run_git(["submodule", "update", "--init", "--recursive"], cwd=source_dir)

    head = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=source_dir,
        text=True,
        capture_output=True,
        check=True,
    ).stdout.strip()
    if head != pin.commit:
        raise FetchError(
            f"checkout verification failed for {pin.name}: got {head}, expected {pin.commit}"
        )


def hash_rbf(pin: CorePin, source_dir: Path) -> None:
    rbf = source_dir / pin.rbf_path
    if not rbf.is_file():
        raise FetchError(f"missing locked RBF: {rbf}")
    data = rbf.read_bytes()
    digest = hashlib.sha256(data).hexdigest()
    size = len(data)
    if size != pin.rbf_size:
        raise FetchError(f"rbf_size got {size} want {pin.rbf_size}")
    if digest != pin.rbf_sha256:
        raise FetchError(f"rbf_sha256 got {digest} want {pin.rbf_sha256}")
    print(f"ok {pin.name} {pin.commit} {digest}")


def fetch_core(pin: CorePin, root: Path) -> Path:
    source_dir = root / "build" / "cores" / pin.name
    checkout_pin(pin, source_dir)
    hash_rbf(pin, source_dir)
    return source_dir


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--core", default="megadrive")
    parser.add_argument("--lock", type=Path, default=DEFAULT_LOCK)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument(
        "--check-lock",
        action="store_true",
        help="validate cores.lock without cloning",
    )
    args = parser.parse_args(argv)
    try:
        pins = load_lock(args.lock)
        if args.core not in pins:
            raise FetchError(f"unknown core {args.core}")
        if args.check_lock:
            print(f"ok {args.core} {pins[args.core].commit}")
            return 0
        fetch_core(pins[args.core], args.root)
    except (CoreLockError, FetchError) as exc:
        print(f"fetch-core: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
