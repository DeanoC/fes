#!/usr/bin/env python3
"""Validate final OSS timing and build a machine-readable build summary."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import re
import sys
from pathlib import Path
from typing import Any, Sequence


TARGET_DEVICE = "5CSEBA6U23I7"
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
FABRIC_RESOURCES = frozenset({"MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO", "MISTRAL_CLKENA"})


class SummaryError(ValueError):
    """Raised when the route evidence cannot satisfy the OSS contract."""


def _regular_file(path: Path, label: str) -> Path:
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise SummaryError(f"{label} is not a regular file: {path}")
    return path


def _number(value: Any, *, label: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise SummaryError(f"{label} must be numeric")
    number = float(value)
    if not math.isfinite(number):
        raise SummaryError(f"{label} must be finite")
    return number


def _integer(value: Any, *, label: str) -> int:
    number = _number(value, label=label)
    if number < 0 or not number.is_integer():
        raise SummaryError(f"{label} must be a non-negative integer")
    return int(number)


def _timing(path: Path, requested_mhz: float, clock_prefix: str) -> tuple[str, float]:
    timing_path = _regular_file(path, "timing report")
    try:
        timing = json.loads(timing_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SummaryError(f"cannot read timing report {timing_path}: {exc}") from exc
    if not isinstance(timing, dict) or not isinstance(timing.get("fmax"), dict):
        raise SummaryError("timing report has no structured fmax data")

    matches: list[tuple[str, dict[str, Any]]] = []
    for clock_name, values in timing["fmax"].items():
        if isinstance(clock_name, str) and clock_name.startswith(clock_prefix) and isinstance(values, dict):
            matches.append((clock_name, values))
    if len(matches) != 1:
        raise SummaryError(
            f"timing report must contain exactly one intended clock with prefix {clock_prefix!r}; "
            f"found {len(matches)}"
        )

    clock_name, values = matches[0]
    constraint = _number(values.get("constraint"), label=f"{clock_name} constraint")
    if constraint != requested_mhz:
        raise SummaryError(
            f"intended clock {clock_name} constraint is {constraint:g} MHz, expected {requested_mhz:g} MHz"
        )
    achieved = _number(values.get("achieved"), label=f"{clock_name} achieved frequency")
    if achieved < requested_mhz:
        raise SummaryError(
            f"intended clock {clock_name} achieves {achieved:g} MHz, below {requested_mhz:g} MHz"
        )
    return clock_name, achieved


def _route_status(path: Path) -> None:
    route_path = _regular_file(path, "route log")
    try:
        lines = route_path.read_text(encoding="utf-8", errors="replace").splitlines()
    except OSError as exc:
        raise SummaryError(f"cannot read route log {route_path}: {exc}") from exc
    if any("unrouted" in line.lower() for line in lines):
        raise SummaryError(f"route log contains an unrouted marker: {route_path}")


def _resources(timing_path: Path) -> tuple[dict[str, dict[str, Any]], dict[str, dict[str, Any]]]:
    try:
        timing = json.loads(_regular_file(timing_path, "timing report").read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise SummaryError(f"cannot read timing report for resources: {exc}") from exc
    utilization = timing.get("utilization") if isinstance(timing, dict) else None
    if not isinstance(utilization, dict):
        raise SummaryError("timing report has no structured utilization data")

    resources: dict[str, dict[str, Any]] = {}
    for name, values in sorted(utilization.items()):
        if not isinstance(name, str) or not isinstance(values, dict):
            raise SummaryError("timing report contains malformed utilization data")
        used = _integer(values.get("used"), label=f"{name} used")
        available = _integer(values.get("available"), label=f"{name} available")
        percent = None if available == 0 else round(used * 100.0 / available, 6)
        resources[name] = {
            "available": available,
            "used": used,
            "utilization_percent": percent,
        }
    hard_blocks = {name: values for name, values in resources.items() if name not in FABRIC_RESOURCES}
    return resources, hard_blocks


def _authenticated_tools(values: Sequence[str]) -> dict[str, dict[str, str]]:
    tools: dict[str, dict[str, str]] = {}
    for value in values:
        name_and_value = value.split("=", 1)
        if len(name_and_value) != 2:
            raise SummaryError(f"invalid authenticated tool record: {value!r}")
        name, commit_and_digest = name_and_value
        commit_digest = commit_and_digest.split(":")
        if len(commit_digest) != 2:
            raise SummaryError(f"invalid authenticated tool record: {value!r}")
        commit, digest = commit_digest
        if not name or COMMIT_RE.fullmatch(commit) is None or SHA256_RE.fullmatch(digest) is None:
            raise SummaryError(f"invalid authenticated tool record: {value!r}")
        tools[name] = {
            "commit": commit,
            "path": f"build/toolchain/install/bin/{name}",
            "sha256": digest,
        }
    return {name: tools[name] for name in sorted(tools)}


def _rbf(path: Path, previous_path: Path | None, previous_hash: str | None) -> dict[str, Any]:
    current_path = _regular_file(path, "RBF")
    current_bytes = current_path.read_bytes()
    if not current_bytes:
        raise SummaryError(f"RBF is empty: {current_path}")
    current_digest = hashlib.sha256(current_bytes).hexdigest()

    if previous_path is not None and previous_hash is not None:
        raise SummaryError("provide one previous RBF input, not two")
    if previous_path is not None:
        previous_file = _regular_file(previous_path, "previous RBF")
        previous_digest = hashlib.sha256(previous_file.read_bytes()).hexdigest()
    elif previous_hash is not None:
        if SHA256_RE.fullmatch(previous_hash) is None:
            raise SummaryError("previous RBF SHA-256 is invalid")
        previous_digest = previous_hash
    else:
        previous_digest = None

    return {
        "rbf_sha256": current_digest,
        "rbf_size_bytes": len(current_bytes),
        "previous_rbf_sha256": previous_digest,
        "rbf_stability_measured": previous_digest is not None,
        "rbf_stable": None if previous_digest is None else previous_digest == current_digest,
    }


def build_summary(
    timing_json: Path,
    route_log: Path,
    rbf: Path,
    *,
    requested_mhz: float,
    clock_prefix: str,
    authenticated_tools: Sequence[str] = (),
    previous_rbf: Path | None = None,
    previous_rbf_sha256: str | None = None,
    target: str = TARGET_DEVICE,
) -> dict[str, Any]:
    if target != TARGET_DEVICE:
        raise SummaryError(f"target must be {TARGET_DEVICE}, got {target!r}")
    if requested_mhz <= 0 or not math.isfinite(requested_mhz):
        raise SummaryError("requested frequency must be positive and finite")
    _route_status(route_log)
    clock_name, achieved_mhz = _timing(timing_json, requested_mhz, clock_prefix)
    resources, hard_blocks = _resources(timing_json)
    return {
        "status": "pass",
        "build_status": "pass",
        "route": {"status": "pass", "unrouted": False},
        "route_status": "pass",
        "timing": {
            "status": "pass",
            "clock": clock_name,
            "requested_mhz": float(requested_mhz),
            "achieved_mhz": achieved_mhz,
        },
        "resources": resources,
        "hard_blocks": hard_blocks,
        "authenticated_tools": _authenticated_tools(authenticated_tools),
        "reproducibility": _rbf(rbf, previous_rbf, previous_rbf_sha256),
        "target": target,
    }


def _write_json(path: Path, value: dict[str, Any]) -> None:
    if path.exists() and path.is_symlink():
        raise SummaryError(f"summary path is a symlink: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def _write_timing(path: Path, summary: dict[str, Any]) -> None:
    timing = summary["timing"]
    repro = summary["reproducibility"]
    lines = [
        f"target: {summary['target']}",
        f"constraint: {timing['requested_mhz']:g} MHz",
        f"clock: {timing['clock']}",
        f"achieved: {timing['achieved_mhz']:.6f} MHz",
        f"status: {timing['status']}",
        f"rbf_size_bytes: {repro['rbf_size_bytes']}",
        f"rbf_sha256: {repro['rbf_sha256']}",
    ]
    if repro["previous_rbf_sha256"] is not None:
        lines.append(f"previous_rbf_sha256: {repro['previous_rbf_sha256']}")
    lines.append(f"rbf_stability_measured: {str(repro['rbf_stability_measured']).lower()}")
    lines.append(f"rbf_stable: {str(repro['rbf_stable']).lower()}")
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--timing-json", type=Path, required=True)
    parser.add_argument("--route-log", type=Path, required=True)
    parser.add_argument("--rbf", type=Path, required=True)
    parser.add_argument("--requested-mhz", type=float, required=True)
    parser.add_argument("--clock-prefix", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--timing-output", type=Path, required=True)
    parser.add_argument("--authenticated-tool", action="append", default=[])
    parser.add_argument("--previous-rbf", type=Path)
    parser.add_argument("--previous-rbf-sha256")
    parser.add_argument("--target", default=TARGET_DEVICE)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    arguments = _parser().parse_args(argv)
    try:
        summary = build_summary(
            arguments.timing_json,
            arguments.route_log,
            arguments.rbf,
            requested_mhz=arguments.requested_mhz,
            clock_prefix=arguments.clock_prefix,
            authenticated_tools=arguments.authenticated_tool,
            previous_rbf=arguments.previous_rbf,
            previous_rbf_sha256=arguments.previous_rbf_sha256,
            target=arguments.target,
        )
        _write_json(arguments.output, summary)
        _write_timing(arguments.timing_output, summary)
        return 0
    except (OSError, SummaryError, ValueError) as exc:
        print(f"oss_summary: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
