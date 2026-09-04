#!/usr/bin/env python3
"""Parse and validate pinned FPGA core trees."""

from __future__ import annotations

import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping
from urllib.parse import urlparse

SHA_RE = re.compile(r"^[0-9a-f]{40}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
CORE_NAME_RE = re.compile(r"^[a-z][a-z0-9_]*$")
REQUIRED_FIELDS = frozenset(
    {"repo", "commit", "rbf_path", "rbf_sha256", "rbf_size", "project", "rationale"}
)
DEFAULT_LOCK = Path(__file__).resolve().parents[1] / "cores.lock"


@dataclass(frozen=True)
class CorePin:
    name: str
    repo: str
    commit: str
    rbf_path: str
    rbf_sha256: str
    rbf_size: int
    project: str
    rationale: str


class CoreLockError(ValueError):
    """Raised when a core lock file cannot satisfy the pin contract."""


def _read_toml(path: Path) -> Mapping[str, Any]:
    try:
        import tomllib
    except ImportError as exc:  # pragma: no cover
        raise CoreLockError("Python 3.11+ is required to read cores.lock") from exc
    path = Path(path)
    try:
        with path.open("rb") as stream:
            data = tomllib.load(stream)
    except FileNotFoundError as exc:
        raise CoreLockError(f"lock file not found: {path}") from exc
    except OSError as exc:
        raise CoreLockError(f"cannot read lock file {path}: {exc}") from exc
    except tomllib.TOMLDecodeError as exc:
        raise CoreLockError(f"invalid TOML in {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise CoreLockError(f"lock file {path} must contain a TOML table")
    return data


def _valid_tree_path(value: str) -> bool:
    if not value or value.startswith("/") or "\\" in value:
        return False
    parts = value.split("/")
    return all(part not in ("", ".", "..") for part in parts)


def _validate_data(data: Mapping[str, Any]) -> list[str]:
    errors: list[str] = []
    unknown_top = sorted(set(data) - {"core"})
    if unknown_top:
        errors.append("unknown top-level key(s): " + ", ".join(unknown_top))
    if "core" not in data:
        errors.append("missing top-level table: core")
        return errors
    cores = data["core"]
    if not isinstance(cores, dict) or not cores:
        errors.append("top-level table core must contain at least one core table")
        return errors
    for name, section in cores.items():
        prefix = f"core.{name}"
        if CORE_NAME_RE.fullmatch(name) is None:
            errors.append(f"{prefix} name must be a lowercase identifier")
        if not isinstance(section, dict):
            errors.append(f"{prefix} must be a TOML table")
            continue
        missing = sorted(REQUIRED_FIELDS - set(section))
        if missing:
            errors.append(f"{prefix} missing field(s): {', '.join(missing)}")
        unknown = sorted(set(section) - REQUIRED_FIELDS)
        if unknown:
            errors.append(f"{prefix} unknown field(s): {', '.join(unknown)}")
        repo = section.get("repo")
        if not isinstance(repo, str):
            errors.append(f"{prefix}.repo must be a string")
        else:
            parsed = urlparse(repo)
            if parsed.scheme != "https" or not parsed.netloc:
                errors.append(f"{prefix}.repo must be an HTTPS URL")
        commit = section.get("commit")
        if not isinstance(commit, str) or SHA_RE.fullmatch(commit) is None:
            errors.append(
                f"{prefix}.commit must be a 40-character lowercase hexadecimal SHA"
            )
        rbf_path = section.get("rbf_path")
        if not isinstance(rbf_path, str) or not _valid_tree_path(rbf_path):
            errors.append(f"{prefix}.rbf_path must be a relative in-tree path")
        elif not rbf_path.lower().endswith(".rbf"):
            errors.append(f"{prefix}.rbf_path must end in .rbf")
        digest = section.get("rbf_sha256")
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            errors.append(f"{prefix}.rbf_sha256 must be 64 lowercase hex characters")
        size = section.get("rbf_size")
        if not isinstance(size, int) or isinstance(size, bool) or size <= 0:
            errors.append(f"{prefix}.rbf_size must be a positive integer")
        project = section.get("project")
        if not isinstance(project, str) or not _valid_tree_path(project):
            errors.append(f"{prefix}.project must be a relative in-tree path")
        elif not project.lower().endswith(".qpf"):
            errors.append(f"{prefix}.project must end in .qpf")
        rationale = section.get("rationale")
        if not isinstance(rationale, str) or not rationale.strip():
            errors.append(f"{prefix}.rationale must be a non-empty string")
    return errors


def validate_lock(path: Path) -> list[str]:
    try:
        data = _read_toml(path)
    except CoreLockError as exc:
        return [str(exc)]
    return _validate_data(data)


def load_lock(path: Path) -> dict[str, CorePin]:
    path = Path(path)
    data = _read_toml(path)
    errors = _validate_data(data)
    if errors:
        raise CoreLockError("\n".join(errors))
    pins: dict[str, CorePin] = {}
    for name, section in data["core"].items():
        pins[name] = CorePin(
            name=name,
            repo=section["repo"],
            commit=section["commit"],
            rbf_path=section["rbf_path"],
            rbf_sha256=section["rbf_sha256"],
            rbf_size=section["rbf_size"],
            project=section["project"],
            rationale=section["rationale"],
        )
    return pins


def main(argv: list[str] | None = None) -> int:
    args = sys.argv[1:] if argv is None else argv
    path = DEFAULT_LOCK
    if args and args[0] == "validate":
        errors = validate_lock(path)
        if errors:
            print("\n".join(errors), file=sys.stderr)
            return 1
        print(f"ok {path}")
        return 0
    if args and args[0] == "get" and len(args) == 3:
        pins = load_lock(path)
        name, field = args[1], args[2]
        if name not in pins:
            print(f"unknown core {name}", file=sys.stderr)
            return 1
        print(getattr(pins[name], field))
        return 0
    print("usage: core_lock.py validate | get <core> <field>", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main())
