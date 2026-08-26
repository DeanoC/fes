#!/usr/bin/env python3
"""Parse and validate the repository's immutable toolchain lock."""

from __future__ import annotations

import re
import sys
import tomllib
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping, Sequence
from urllib.parse import urlparse


EXPECTED_TOOLS = (
    "yosys",
    "mistral",
    "nextpnr",
    "verilator",
    "openfpgaloader",
)
REQUIRED_FIELDS = frozenset({"repo", "commit", "order", "rationale"})
SHA_RE = re.compile(r"^[0-9a-f]{40}$")
DEFAULT_LOCK = Path(__file__).resolve().parents[1] / "toolchain.lock"


@dataclass(frozen=True)
class ToolPin:
    repo: str
    commit: str
    order: int
    rationale: str


class LockfileError(ValueError):
    """Raised when a lock file cannot satisfy the toolchain contract."""


def _read_toml(path: Path) -> Mapping[str, Any]:
    path = Path(path)
    try:
        with path.open("rb") as stream:
            data = tomllib.load(stream)
    except FileNotFoundError as exc:
        raise LockfileError(f"lock file not found: {path}") from exc
    except OSError as exc:
        raise LockfileError(f"cannot read lock file {path}: {exc}") from exc
    except tomllib.TOMLDecodeError as exc:
        raise LockfileError(f"invalid TOML in {path}: {exc}") from exc
    if not isinstance(data, dict):
        raise LockfileError(f"lock file {path} must contain a TOML table")
    return data


def _validate_data(data: Mapping[str, Any]) -> list[str]:
    errors: list[str] = []

    unknown_top_level = sorted(set(data) - {"tool"})
    if unknown_top_level:
        errors.append("unknown top-level key(s): " + ", ".join(unknown_top_level))
    if "tool" not in data:
        errors.append("missing top-level table: tool")
        return errors

    tools = data["tool"]
    if not isinstance(tools, dict):
        errors.append("top-level table tool must contain tool tables")
        return errors

    expected = set(EXPECTED_TOOLS)
    actual = set(tools)
    missing_tools = [name for name in EXPECTED_TOOLS if name not in actual]
    if missing_tools:
        errors.append("missing tool table(s): " + ", ".join(missing_tools))
    unknown_tools = sorted(actual - expected)
    if unknown_tools:
        errors.append("unknown tool table(s): " + ", ".join(unknown_tools))

    orders: dict[int, list[str]] = {}
    for name in EXPECTED_TOOLS:
        if name not in tools:
            continue
        section = tools[name]
        prefix = f"tool.{name}"
        if not isinstance(section, dict):
            errors.append(f"{prefix} must be a TOML table")
            continue

        actual_fields = set(section)
        missing_fields = sorted(REQUIRED_FIELDS - actual_fields)
        if missing_fields:
            errors.append(f"{prefix} missing field(s): {', '.join(missing_fields)}")
        unknown_fields = sorted(actual_fields - REQUIRED_FIELDS)
        if unknown_fields:
            errors.append(f"{prefix} unknown field(s): {', '.join(unknown_fields)}")

        repo = section.get("repo")
        if not isinstance(repo, str):
            errors.append(f"{prefix}.repo must be a string")
        else:
            parsed = urlparse(repo)
            if parsed.scheme != "https" or not parsed.netloc:
                errors.append(f"{prefix}.repo must be an HTTPS URL")

        commit = section.get("commit")
        if not isinstance(commit, str) or SHA_RE.fullmatch(commit) is None:
            errors.append(f"{prefix}.commit must be a 40-character lowercase hexadecimal SHA")

        order = section.get("order")
        if not isinstance(order, int) or isinstance(order, bool) or order <= 0:
            errors.append(f"{prefix}.order must be a positive integer")
        else:
            orders.setdefault(order, []).append(name)

        rationale = section.get("rationale")
        if not isinstance(rationale, str) or not rationale.strip():
            errors.append(f"{prefix}.rationale must be a non-empty string")

    for order, names in sorted(orders.items()):
        if len(names) > 1:
            errors.append(f"duplicate build order {order}: {', '.join(names)}")

    mistral = tools.get("mistral")
    nextpnr = tools.get("nextpnr")
    if isinstance(mistral, dict) and isinstance(nextpnr, dict):
        mistral_order = mistral.get("order")
        nextpnr_order = nextpnr.get("order")
        if (
            isinstance(mistral_order, int)
            and not isinstance(mistral_order, bool)
            and isinstance(nextpnr_order, int)
            and not isinstance(nextpnr_order, bool)
            and mistral_order >= nextpnr_order
        ):
            errors.append("build order must place mistral before nextpnr")

    return errors


def validate_lock(path: Path) -> list[str]:
    """Return validation errors for *path*, or an empty list when valid."""

    try:
        data = _read_toml(path)
    except LockfileError as exc:
        return [str(exc)]
    return _validate_data(data)


def load_lock(path: Path) -> dict[str, ToolPin]:
    """Load and validate *path* as typed tool pins."""

    path = Path(path)
    data = _read_toml(path)
    errors = _validate_data(data)
    if errors:
        raise LockfileError("\n".join(errors))

    tools = data["tool"]
    return {
        name: ToolPin(
            repo=tools[name]["repo"],
            commit=tools[name]["commit"],
            order=tools[name]["order"],
            rationale=tools[name]["rationale"],
        )
        for name in EXPECTED_TOOLS
    }


def _single_line(message: object) -> str:
    return " ".join(str(message).splitlines())


def main(argv: Sequence[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    try:
        if args == ["validate"]:
            errors = validate_lock(DEFAULT_LOCK)
            if errors:
                message = _single_line("; ".join(errors))
                print(f"{DEFAULT_LOCK.name}: invalid: {message}", file=sys.stderr)
                return 2
            print(f"{DEFAULT_LOCK.name}: valid")
            return 0

        if len(args) == 3 and args[0] == "get":
            _, tool, field = args
            lock = load_lock(DEFAULT_LOCK)
            if tool not in lock:
                raise LockfileError(f"unknown tool: {tool}")
            if field not in REQUIRED_FIELDS:
                raise LockfileError(f"unknown field: {field}")
            print(getattr(lock[tool], field))
            return 0

        raise LockfileError("usage: lockfile.py validate | get TOOL FIELD")
    except (LockfileError, OSError, ValueError) as exc:
        print(f"lockfile: {_single_line(exc)}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
