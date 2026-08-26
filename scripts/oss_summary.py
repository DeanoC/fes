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
ORDINARY_RESOURCES = frozenset(
    {"MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO"}
)
FORBIDDEN_MISTRAL_DSP_RESOURCES = frozenset(
    {"MISTRAL_MUL9X9", "MISTRAL_MUL18X18", "MISTRAL_MUL27X27"}
)
FORBIDDEN_RESOURCE_MARKERS = (
    ("pll", "PLL"),
    ("phase_locked", "PLL"),
    ("bram", "BRAM/M10K"),
    ("m10k", "BRAM/M10K"),
    ("ram_block", "BRAM/M10K"),
    ("mlab", "MLAB/LUTRAM"),
    ("lutram", "MLAB/LUTRAM"),
    ("dsp", "DSP"),
    ("mac", "DSP"),
    ("mult", "DSP"),
    ("hps", "HPS"),
    ("mpu", "HPS"),
    ("arm", "HPS"),
    ("oscillator", "vendor oscillator"),
)
TOOL_LOCK_NAMES = {"yosys": "yosys", "nextpnr-mistral": "nextpnr"}


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


def _resource_class(name: str) -> str:
    if name in ORDINARY_RESOURCES:
        return "ordinary"
    if name in FORBIDDEN_MISTRAL_DSP_RESOURCES:
        return "forbidden"
    normalized = name.lower()
    if any(marker in normalized for marker, _ in FORBIDDEN_RESOURCE_MARKERS):
        return "forbidden"
    return "unknown"


def _resources(
    timing_path: Path,
) -> tuple[
    dict[str, dict[str, Any]],
    dict[str, dict[str, Any]],
    dict[str, dict[str, Any]],
    dict[str, str],
    str,
    str,
]:
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
    hard_blocks: dict[str, dict[str, Any]] = {}
    unknown_resources: dict[str, dict[str, Any]] = {}
    resource_classes: dict[str, str] = {}
    forbidden_in_use: list[str] = []
    for name, values in resources.items():
        classification = _resource_class(name)
        resource_classes[name] = classification
        if classification == "forbidden":
            hard_blocks[name] = values
            if values["used"] > 0:
                forbidden_in_use.append(f"{name}={values['used']}")
        elif classification == "unknown":
            unknown_resources[name] = values

    reasons: list[str] = []
    if forbidden_in_use:
        reasons.append("forbidden hard resources in use: " + ", ".join(sorted(forbidden_in_use)))
    if unknown_resources:
        reasons.append("unknown utilization resources: " + ", ".join(sorted(unknown_resources)))
    status = "fail" if reasons else "pass"
    reason = "; ".join(reasons) if reasons else "no forbidden hard resources or unknown utilization keys"
    return resources, hard_blocks, unknown_resources, resource_classes, status, reason


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
        if name in tools:
            raise SummaryError(f"duplicate authenticated tool record: {name!r}")
        tools[name] = {
            "commit": commit,
            "path": f"build/toolchain/install/bin/{name}",
            "sha256": digest,
        }
    return {name: tools[name] for name in sorted(tools)}


def _keyed_records(values: Sequence[str], *, label: str, pattern: re.Pattern[str]) -> dict[str, str]:
    records: dict[str, str] = {}
    for value in values:
        name_and_value = value.split("=", 1)
        if len(name_and_value) != 2:
            raise SummaryError(f"invalid {label} record: {value!r}")
        name, record = name_and_value
        if not name or name.startswith("/") or name.startswith("../"):
            raise SummaryError(f"invalid {label} name: {value!r}")
        if pattern.fullmatch(record) is None:
            raise SummaryError(f"invalid {label} value: {value!r}")
        if name in records:
            raise SummaryError(f"duplicate {label} record: {name!r}")
        records[name] = record
    return {name: records[name] for name in sorted(records)}


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
        "rbf_stability_measured": False,
        "rbf_stable": None,
        "rbf_stability_reason": "provenance has not been checked",
    }


def _manifest_records(value: Any, *, label: str) -> dict[str, str] | None:
    if not isinstance(value, list):
        return None
    records: dict[str, str] = {}
    for entry in value:
        if not isinstance(entry, dict):
            return None
        name = entry.get("path")
        digest = entry.get("sha256")
        if not isinstance(name, str) or not name or not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            return None
        if name in records:
            return None
        records[name] = digest
    return records


def _stability(
    reproducibility: dict[str, Any],
    previous_manifest: Path | None,
    *,
    lane: str,
    experiment: str,
    target: str,
    source_hashes: dict[str, str],
    tool_pins: dict[str, str],
    authenticated_tools: dict[str, dict[str, str]],
) -> dict[str, Any]:
    previous_hash = reproducibility["previous_rbf_sha256"]
    current_hash = reproducibility["rbf_sha256"]
    reasons: list[str] = []
    if previous_hash is None:
        reasons.append("no pre-existing RBF hash was supplied")
    if previous_manifest is None:
        reasons.append("no previous manifest proves the lane provenance")
    else:
        try:
            manifest_path = _regular_file(previous_manifest, "previous manifest")
            previous = json.loads(manifest_path.read_text(encoding="utf-8"))
            if not isinstance(previous, dict):
                raise SummaryError("previous manifest is not a JSON object")
        except (OSError, SummaryError, ValueError, json.JSONDecodeError) as exc:
            reasons.append(f"previous manifest is unusable: {exc}")
            previous = None

        if previous is not None:
            if previous.get("lane") != lane:
                reasons.append(f"previous lane is {previous.get('lane')!r}, expected {lane!r}")
            if previous.get("experiment") != experiment:
                reasons.append(
                    f"previous experiment is {previous.get('experiment')!r}, expected {experiment!r}"
                )
            if previous.get("target") != target:
                reasons.append(f"previous target is {previous.get('target')!r}, expected {target!r}")

            previous_build = previous.get("build")
            if not isinstance(previous_build, dict):
                reasons.append("previous manifest has no machine-readable build result")
            else:
                if previous_build.get("status") != "pass" or previous_build.get("build_status") != "pass":
                    reasons.append("previous manifest does not prove a successful build")
                if previous_build.get("route_status") != "pass":
                    reasons.append("previous manifest does not prove a successful route")
                previous_route = previous_build.get("route")
                if not isinstance(previous_route, dict) or previous_route.get("status") != "pass" or previous_route.get("unrouted") is not False:
                    reasons.append("previous manifest does not prove a routed design")
                previous_timing = previous_build.get("timing")
                if not isinstance(previous_timing, dict) or previous_timing.get("status") != "pass":
                    reasons.append("previous manifest does not prove passing timing")
                if previous_build.get("hard_block_status") != "pass":
                    reasons.append("previous manifest does not prove passing hard-block classification")

                previous_sources = _manifest_records(previous.get("sources"), label="sources")
                if previous_sources is None or previous_sources != source_hashes:
                    reasons.append("previous source/constraint hashes do not match")

                previous_tools = previous_build.get("authenticated_tools")
                if not isinstance(previous_tools, dict) or set(previous_tools) != set(authenticated_tools):
                    reasons.append("previous authenticated tool set does not match")
                else:
                    for name, current in authenticated_tools.items():
                        prior = previous_tools.get(name)
                        if not isinstance(prior, dict) or prior.get("commit") != current["commit"] or prior.get("sha256") != current["sha256"]:
                            reasons.append(f"previous authenticated tool {name} commit/digest does not match")

                previous_pins = previous.get("tool_pins")
                if not isinstance(previous_pins, dict):
                    reasons.append("previous manifest has no tool pins")
                else:
                    for tool_name, lock_name in TOOL_LOCK_NAMES.items():
                        commit = tool_pins.get(lock_name)
                        prior_pin = previous_pins.get(lock_name)
                        if commit is None or not isinstance(prior_pin, dict) or prior_pin.get("commit") != commit:
                            reasons.append(f"previous {lock_name} tool pin does not match")

                expected_artifact = f"build/oss/{experiment}/top.rbf"
                previous_artifacts = _manifest_records(previous.get("artifacts"), label="artifacts")
                if previous_artifacts is None or previous_artifacts.get(expected_artifact) != previous_hash:
                    reasons.append("previous manifest RBF artifact hash does not match the pre-existing RBF")
                previous_repro = previous_build.get("reproducibility")
                if not isinstance(previous_repro, dict) or previous_repro.get("rbf_sha256") != previous_hash:
                    reasons.append("previous build summary RBF hash does not match the pre-existing RBF")

    if reasons:
        reproducibility["rbf_stability_measured"] = False
        reproducibility["rbf_stable"] = None
        reproducibility["rbf_stability_reason"] = "; ".join(dict.fromkeys(reasons))
    else:
        reproducibility["rbf_stability_measured"] = True
        reproducibility["rbf_stable"] = previous_hash == current_hash
        if reproducibility["rbf_stable"]:
            reproducibility["rbf_stability_reason"] = "previous successful manifest provenance matched"
        else:
            reproducibility["rbf_stability_reason"] = "previous successful manifest provenance matched, but RBF bytes changed"
    return reproducibility


def build_summary(
    timing_json: Path,
    route_log: Path,
    rbf: Path,
    *,
    requested_mhz: float,
    clock_prefix: str,
    authenticated_tools: Sequence[str] = (),
    source_hashes: Sequence[str] = (),
    tool_pins: Sequence[str] = (),
    previous_rbf: Path | None = None,
    previous_rbf_sha256: str | None = None,
    previous_manifest: Path | None = None,
    lane: str = "oss",
    experiment: str = "010_blinky",
    target: str = TARGET_DEVICE,
) -> dict[str, Any]:
    if target != TARGET_DEVICE:
        raise SummaryError(f"target must be {TARGET_DEVICE}, got {target!r}")
    if requested_mhz <= 0 or not math.isfinite(requested_mhz):
        raise SummaryError("requested frequency must be positive and finite")
    _route_status(route_log)
    clock_name, achieved_mhz = _timing(timing_json, requested_mhz, clock_prefix)
    resources, hard_blocks, unknown_resources, resource_classes, hard_block_status, hard_block_reason = _resources(timing_json)
    tools = _authenticated_tools(authenticated_tools)
    source_records = _keyed_records(source_hashes, label="source hash", pattern=SHA256_RE)
    pin_records = _keyed_records(tool_pins, label="tool pin", pattern=COMMIT_RE)
    reproducibility = _rbf(rbf, previous_rbf, previous_rbf_sha256)
    reproducibility = _stability(
        reproducibility,
        previous_manifest,
        lane=lane,
        experiment=experiment,
        target=target,
        source_hashes=source_records,
        tool_pins=pin_records,
        authenticated_tools=tools,
    )
    build_status = "pass" if hard_block_status == "pass" else "fail"
    return {
        "status": build_status,
        "build_status": build_status,
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
        "resource_classes": resource_classes,
        "unknown_resources": unknown_resources,
        "hard_block_status": hard_block_status,
        "hard_block_reason": hard_block_reason,
        "authenticated_tools": tools,
        "reproducibility": reproducibility,
        "source_hashes": source_records,
        "tool_pins": pin_records,
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
        f"hard_block_status: {summary['hard_block_status']}",
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
    parser.add_argument("--source-hash", action="append", default=[])
    parser.add_argument("--tool-pin", action="append", default=[])
    parser.add_argument("--previous-rbf", type=Path)
    parser.add_argument("--previous-rbf-sha256")
    parser.add_argument("--previous-manifest", type=Path)
    parser.add_argument("--lane", default="oss")
    parser.add_argument("--experiment", default="010_blinky")
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
            source_hashes=arguments.source_hash,
            tool_pins=arguments.tool_pin,
            previous_rbf=arguments.previous_rbf,
            previous_rbf_sha256=arguments.previous_rbf_sha256,
            previous_manifest=arguments.previous_manifest,
            lane=arguments.lane,
            experiment=arguments.experiment,
            target=arguments.target,
        )
        _write_json(arguments.output, summary)
        _write_timing(arguments.timing_output, summary)
        if summary["status"] != "pass":
            print(f"oss_summary: {summary['hard_block_reason']}", file=sys.stderr)
            return 2
        return 0
    except (OSError, SummaryError, ValueError) as exc:
        print(f"oss_summary: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
