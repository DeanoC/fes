#!/usr/bin/env python3
"""Compare the OSS and Quartus lanes without treating implementation details as gates.

The comparison consumes schema-2 manifests produced by ``collect_manifest``.
Resource counts and RBF bytes are evidence, not acceptance criteria.  Only the
explicit build, routing, timing, hard-resource, simulation, and artifact rules
can make the comparison fail.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import sys
from pathlib import Path
from typing import Any, Mapping, Sequence


SCRIPT_ROOT = Path(__file__).resolve().parents[1]
TARGET_DEVICE = "5CSEBA6U23I7"
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")


class ComparisonError(ValueError):
    """Raised when comparison inputs or output paths are unsafe."""


def _is_within(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return False
    return True


def _contains_symlink(path: Path) -> bool:
    current = Path(path.anchor)
    for component in path.parts[1:]:
        current /= component
        if current.is_symlink():
            return True
    return False


def _safe_output_dir(path: Path) -> Path:
    candidate = Path(os.path.abspath(os.fspath(path)))
    if _contains_symlink(candidate):
        raise ComparisonError(f"comparison output path contains a symlink: {candidate}")
    if candidate.exists() and not candidate.is_dir():
        raise ComparisonError(f"comparison output path is not a directory: {candidate}")
    parent = candidate
    while not parent.exists():
        parent = parent.parent
    if parent.is_symlink():
        raise ComparisonError(f"comparison output parent is a symlink: {parent}")
    try:
        candidate.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        raise ComparisonError(f"cannot create comparison output directory {candidate}: {exc}") from exc
    return candidate


def _read_manifest(path: Path) -> dict[str, Any]:
    candidate = Path(path)
    if not candidate.is_absolute():
        candidate = Path.cwd() / candidate
    candidate = Path(os.path.abspath(os.fspath(candidate)))
    if candidate.is_symlink() or not candidate.is_file():
        raise ComparisonError(f"manifest is not a regular file: {path}")
    try:
        value = json.loads(candidate.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ComparisonError(f"cannot read manifest {candidate}: {exc}") from exc
    if not isinstance(value, dict):
        raise ComparisonError(f"manifest must contain a JSON object: {candidate}")
    return value


def _number(value: Any) -> float | None:
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    number = float(value)
    return number if math.isfinite(number) else None


def _artifact_path(raw: Any, manifest_path: Path, repo_root: Path) -> Path | None:
    if not isinstance(raw, str) or not raw:
        return None
    path = Path(raw)
    if path.is_absolute():
        return path
    # collect_manifest uses repository-relative paths.  A small fixture may
    # use a path relative to its manifest, so prefer that convenient local
    # spelling when it exists and then fall back to the repository root.
    local_candidate = manifest_path.parent / path
    if local_candidate.exists():
        return local_candidate
    repository_candidate = repo_root / path
    if repository_candidate.exists():
        return repository_candidate
    return local_candidate


def _artifact_records(manifest: Mapping[str, Any], manifest_path: Path, repo_root: Path) -> tuple[list[dict[str, Any]], list[str]]:
    raw_records = manifest.get("artifacts")
    if not isinstance(raw_records, list):
        return [], ["missing required artifact list"]
    records: list[dict[str, Any]] = []
    failures: list[str] = []
    for raw in raw_records:
        if not isinstance(raw, dict):
            failures.append("artifact record is malformed")
            continue
        raw_path = raw.get("path")
        digest = raw.get("sha256")
        resolved = _artifact_path(raw_path, manifest_path, repo_root)
        entry: dict[str, Any] = {
            "path": raw_path,
            "sha256": digest,
            "present": False,
            "hash_matches": False,
        }
        if resolved is None:
            failures.append("artifact record has no valid path")
            records.append(entry)
            continue
        entry["resolved_path"] = str(resolved)
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"artifact {raw_path!r} has an invalid SHA-256")
            records.append(entry)
            continue
        if _contains_symlink(Path(os.path.abspath(os.fspath(resolved)))) or resolved.is_symlink() or not resolved.is_file():
            failures.append(f"missing required artifact: {raw_path}")
            records.append(entry)
            continue
        try:
            actual = hashlib.sha256(resolved.read_bytes()).hexdigest()
        except OSError as exc:
            failures.append(f"cannot hash artifact {raw_path}: {exc}")
            records.append(entry)
            continue
        entry["present"] = True
        entry["hash_matches"] = actual == digest
        entry["actual_sha256"] = actual
        if actual != digest:
            failures.append(f"artifact hash mismatch: {raw_path}")
        records.append(entry)

    rbf_records = [record for record in records if isinstance(record.get("path"), str) and str(record["path"]).lower().endswith(".rbf")]
    if not rbf_records:
        failures.append("missing required artifact: RBF")
    elif not any(record.get("present") and record.get("hash_matches") for record in rbf_records):
        failures.append("missing required artifact: usable RBF")
    return records, failures


def _resource_view(build: Mapping[str, Any]) -> dict[str, dict[str, Any]]:
    """Copy resource records without inventing zero values for absent fields."""

    raw = build.get("resources")
    if not isinstance(raw, dict):
        return {}
    view: dict[str, dict[str, Any]] = {}
    for name, value in sorted(raw.items()):
        if not isinstance(name, str) or not isinstance(value, dict):
            continue
        record: dict[str, Any] = {}
        for key in ("used", "available", "utilization_percent"):
            if key in value:
                record[key] = value[key]
        view[name] = record
    return view


def _simulation_view(manifest: Mapping[str, Any], build: Mapping[str, Any]) -> dict[str, Any]:
    value = build.get("simulation", manifest.get("simulation"))
    if not isinstance(value, dict):
        return {"status": "not-recorded"}
    return dict(value)


def _rbf_digest(build: Mapping[str, Any], artifacts: Sequence[Mapping[str, Any]]) -> str | None:
    repro = build.get("reproducibility")
    if isinstance(repro, dict) and isinstance(repro.get("rbf_sha256"), str):
        return repro["rbf_sha256"]
    for record in artifacts:
        path = record.get("path")
        if isinstance(path, str) and path.lower().endswith(".rbf") and isinstance(record.get("sha256"), str):
            return record["sha256"]
    return None


def _lane_view(
    manifest: Mapping[str, Any],
    manifest_path: Path,
    lane: str,
    repo_root: Path,
) -> tuple[dict[str, Any], list[str]]:
    failures: list[str] = []
    if manifest.get("schema") != 2:
        failures.append(f"{lane} manifest is not schema 2")
    if manifest.get("lane") != lane:
        failures.append(f"{lane} manifest lane is {manifest.get('lane')!r}")
    if manifest.get("target") != TARGET_DEVICE:
        failures.append(f"{lane} target is not {TARGET_DEVICE}")

    raw_build = manifest.get("build")
    build = raw_build if isinstance(raw_build, dict) else {}
    if not isinstance(raw_build, dict):
        failures.append(f"{lane} build result is missing")

    status = build.get("status", build.get("build_status"))
    build_status = build.get("build_status", status)
    if status != "pass" or build_status != "pass":
        failures.append(f"{lane} build status is not pass ({status!r})")

    route = build.get("route")
    route_status = build.get("route_status")
    unrouted = route.get("unrouted") if isinstance(route, dict) else None
    if route_status != "pass" or not isinstance(route, dict) or route.get("status") != "pass" or unrouted is not False:
        failures.append(f"{lane} route is missing, failed, or unrouted")

    timing = build.get("timing")
    if not isinstance(timing, dict):
        failures.append(f"{lane} timing evidence is missing")
        timing_view: dict[str, Any] = {"status": "absent"}
    else:
        timing_view = {key: timing.get(key) for key in ("status", "clock", "requested_mhz", "achieved_mhz") if key in timing}
        requested = _number(timing.get("requested_mhz"))
        achieved = _number(timing.get("achieved_mhz"))
        if timing.get("status") != "pass" or requested is None or requested != 50.0 or achieved is None:
            failures.append(f"{lane} timing evidence is missing or failed")
        elif achieved < 50.0:
            failures.append(f"{lane} timing is below 50 MHz ({achieved:g} MHz)")

    hard_status = build.get("hard_block_status")
    hard_blocks = build.get("hard_blocks")
    unknown = build.get("unknown_resources")
    if hard_status != "pass":
        failures.append(f"{lane} has unexpected hard blocks or unknown resources")
    if isinstance(hard_blocks, dict):
        for name, record in hard_blocks.items():
            if isinstance(record, dict) and _number(record.get("used")) is not None and float(record["used"]) > 0:
                failures.append(f"{lane} unexpected hard block in use: {name}={record['used']}")
    if isinstance(unknown, dict) and unknown:
        failures.append(f"{lane} has unknown resources: {', '.join(sorted(str(item) for item in unknown))}")

    simulation = _simulation_view(manifest, build)
    simulation_status = str(simulation.get("status", "not-recorded")).lower()
    if simulation_status in {"fail", "failed", "failure", "error", "not-pass"}:
        failures.append(f"{lane} simulation failed")

    artifacts, artifact_failures = _artifact_records(manifest, manifest_path, repo_root)
    failures.extend(f"{failure}" for failure in artifact_failures)

    return {
        "lane": lane,
        "status": "fail" if failures else "pass",
        "manifest": str(manifest_path),
        "target": manifest.get("target"),
        "build_status": status,
        "route_status": route_status,
        "timing": timing_view,
        "resources": _resource_view(build),
        "hard_blocks": hard_blocks if isinstance(hard_blocks, dict) else {},
        "hard_block_status": hard_status,
        "unknown_resources": unknown if isinstance(unknown, dict) else {},
        "simulation": simulation,
        "rbf_sha256": _rbf_digest(build, artifacts),
        "artifacts": [
            {key: value for key, value in record.items() if key != "resolved_path"}
            for record in artifacts
        ],
    }, failures


def _resource_value(resources: Mapping[str, Any], name: str, key: str = "used") -> Any:
    value = resources.get(name)
    if not isinstance(value, dict) or key not in value:
        return "absent"
    return value[key]


def _differences(oss: Mapping[str, Any], oracle: Mapping[str, Any]) -> list[str]:
    differences: list[str] = []
    names = sorted(set(oss.get("resources", {})) | set(oracle.get("resources", {})))
    for name in names:
        oss_value = _resource_value(oss.get("resources", {}), name)
        oracle_value = _resource_value(oracle.get("resources", {}), name)
        if oss_value != oracle_value:
            differences.append(f"resource {name} used differs (OSS {oss_value}, Quartus oracle {oracle_value})")
    oss_rbf = oss.get("rbf_sha256")
    oracle_rbf = oracle.get("rbf_sha256")
    if oss_rbf != oracle_rbf:
        differences.append("RBF SHA-256 differs (informational)")
    oss_timing = oss.get("timing", {})
    oracle_timing = oracle.get("timing", {})
    if isinstance(oss_timing, dict) and isinstance(oracle_timing, dict):
        if oss_timing.get("achieved_mhz") != oracle_timing.get("achieved_mhz"):
            differences.append("reported timing differs (informational)")
    return differences


def compare_manifests(
    oss_manifest: Mapping[str, Any],
    oracle_manifest: Mapping[str, Any],
    *,
    oss_path: Path | None = None,
    oracle_path: Path | None = None,
    repo_root: Path = SCRIPT_ROOT,
    experiment: str | None = None,
    hardware_observation: str | None = None,
) -> dict[str, Any]:
    """Return a semantic comparison and explicit acceptance failures."""

    oss_path = Path(oss_path or "oss-manifest.json")
    oracle_path = Path(oracle_path or "oracle-manifest.json")
    oss, oss_failures = _lane_view(oss_manifest, oss_path, "oss", repo_root)
    oracle, oracle_failures = _lane_view(oracle_manifest, oracle_path, "oracle", repo_root)
    failures = [*oss_failures, *oracle_failures]

    expected_experiment = experiment or oss_manifest.get("experiment")
    if not isinstance(expected_experiment, str) or not expected_experiment:
        failures.append("experiment is missing")
    else:
        for lane_name, manifest in (("OSS", oss_manifest), ("Quartus oracle", oracle_manifest)):
            if manifest.get("experiment") != expected_experiment:
                failures.append(f"{lane_name} experiment does not match {expected_experiment}")
    if oss_manifest.get("target") != oracle_manifest.get("target"):
        failures.append("lane targets differ")

    differences = _differences(oss, oracle)
    observation = hardware_observation
    if observation is None:
        for value in (oss_manifest, oracle_manifest):
            candidate = value.get("hardware_observation")
            if isinstance(candidate, str):
                observation = candidate
                break
    if observation is None:
        observation = "not-recorded"

    return {
        "schema": 1,
        "status": "fail" if failures else "pass",
        "experiment": expected_experiment,
        "target": TARGET_DEVICE,
        "lanes": {"oss": oss, "oracle": oracle},
        "differences": differences,
        "failures": failures,
        "hardware_observation": observation,
        "acceptance": {
            "passed": not failures,
            "rules": [
                "both manifests are schema 2 and contain a passing build",
                "both lanes are routed and not unrouted",
                "both lanes meet the requested 50 MHz timing",
                "both lanes have no unexpected hard blocks or unknown resources",
                "failed simulations fail the comparison",
                "each lane has a present, hash-matching RBF artifact",
            ],
        },
    }


def comparison_markdown(comparison: Mapping[str, Any]) -> str:
    lanes = comparison.get("lanes", {})
    oss = lanes.get("oss", {}) if isinstance(lanes, dict) else {}
    oracle = lanes.get("oracle", {}) if isinstance(lanes, dict) else {}

    def cell(value: Any) -> str:
        if value is None:
            return "absent"
        return str(value).replace("|", "\\|").replace("\n", " ")

    lines = [
        f"# Build comparison: {cell(comparison.get('experiment'))}",
        "",
        f"Status: **{str(comparison.get('status', 'fail')).upper()}**",
        "",
        "| Evidence | OSS | Quartus oracle |",
        "| --- | --- | --- |",
        f"| target | {cell(oss.get('target'))} | {cell(oracle.get('target'))} |",
        f"| lane status | {cell(oss.get('status'))} | {cell(oracle.get('status'))} |",
        f"| build status | {cell(oss.get('build_status'))} | {cell(oracle.get('build_status'))} |",
        f"| route status | {cell(oss.get('route_status'))} | {cell(oracle.get('route_status'))} |",
    ]

    for key, label in (("requested_mhz", "requested clock (MHz)"), ("achieved_mhz", "reported timing (MHz)")):
        oss_timing = oss.get("timing", {}) if isinstance(oss.get("timing"), dict) else {}
        oracle_timing = oracle.get("timing", {}) if isinstance(oracle.get("timing"), dict) else {}
        lines.append(f"| {label} | {cell(oss_timing.get(key))} | {cell(oracle_timing.get(key))} |")
    lines.extend(
        [
            f"| RBF SHA-256 | {cell(oss.get('rbf_sha256'))} | {cell(oracle.get('rbf_sha256'))} |",
            f"| hardware observation | {cell(comparison.get('hardware_observation'))} | {cell(comparison.get('hardware_observation'))} |",
            "",
            "## Resources (informational)",
            "",
            "| Resource | OSS used | Quartus oracle used |",
            "| --- | ---: | ---: |",
        ]
    )
    resources = set()
    for lane in (oss, oracle):
        if isinstance(lane.get("resources"), dict):
            resources.update(lane["resources"])
    for name in sorted(resources):
        lines.append(
            f"| {cell(name)} | {cell(_resource_value(oss.get('resources', {}), name))} | {cell(_resource_value(oracle.get('resources', {}), name))} |"
        )
    if not resources:
        lines.append("| (absent; not zero) | absent | absent |")

    lines.extend(
        [
            "",
            "## Hard blocks (acceptance evidence)",
            "",
            "| Resource | OSS used | Quartus oracle used |",
            "| --- | ---: | ---: |",
        ]
    )
    hard_blocks = set()
    for lane in (oss, oracle):
        if isinstance(lane.get("hard_blocks"), dict):
            hard_blocks.update(lane["hard_blocks"])
    for name in sorted(hard_blocks):
        lines.append(
            f"| {cell(name)} | {cell(_resource_value(oss.get('hard_blocks', {}), name))} | {cell(_resource_value(oracle.get('hard_blocks', {}), name))} |"
        )
    if not hard_blocks:
        lines.append("| (absent; not zero) | absent | absent |")

    lines.extend(["", "## Informational differences", ""])
    differences = comparison.get("differences", [])
    if differences:
        lines.extend(f"- {item}" for item in differences)
    else:
        lines.append("- None")
    lines.extend(["", "## Acceptance", ""])
    failures = comparison.get("failures", [])
    if failures:
        lines.extend(f"- FAIL: {item}" for item in failures)
    else:
        lines.append("- PASS: all stated acceptance rules")
    return "\n".join(lines) + "\n"


def write_comparison(comparison: Mapping[str, Any], output_dir: Path) -> tuple[Path, Path]:
    output = _safe_output_dir(output_dir)
    json_path = output / "comparison.json"
    markdown_path = output / "comparison.md"
    for path in (json_path, markdown_path):
        if path.is_symlink():
            raise ComparisonError(f"comparison output is a symlink: {path}")
    try:
        json_path.write_text(json.dumps(comparison, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        markdown_path.write_text(comparison_markdown(comparison), encoding="utf-8")
    except OSError as exc:
        raise ComparisonError(f"cannot write comparison output: {exc}") from exc
    return json_path, markdown_path


def _unavailable_comparison(experiment: str, oss_path: Path, oracle_path: Path, reason: str) -> dict[str, Any]:
    """Build a report even when one manifest cannot be loaded."""

    return {
        "schema": 1,
        "status": "fail",
        "experiment": experiment,
        "target": TARGET_DEVICE,
        "lanes": {
            "oss": {"lane": "oss", "status": "unavailable", "manifest": str(oss_path), "resources": {}},
            "oracle": {"lane": "oracle", "status": "unavailable", "manifest": str(oracle_path), "resources": {}},
        },
        "differences": [],
        "failures": [reason],
        "hardware_observation": "not-recorded",
        "acceptance": {"passed": False, "rules": ["required manifests must be readable"]},
    }


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifests", nargs="*", metavar="MANIFEST")
    parser.add_argument("--oss-manifest", "--oss", type=Path)
    parser.add_argument("--oracle-manifest", "--oracle", type=Path)
    parser.add_argument("--experiment", default="010_blinky")
    parser.add_argument("--output-dir", "--output", type=Path)
    parser.add_argument("--hardware-observation")
    return parser


def _arguments(argv: Sequence[str] | None) -> argparse.Namespace:
    parser = _parser()
    arguments = parser.parse_args(argv)
    if arguments.manifests:
        if len(arguments.manifests) != 2:
            parser.error("positional form requires OSS_MANIFEST ORACLE_MANIFEST")
        if arguments.oss_manifest is not None or arguments.oracle_manifest is not None:
            parser.error("do not mix positional manifests with --oss-manifest/--oracle-manifest")
        arguments.oss_manifest, arguments.oracle_manifest = map(Path, arguments.manifests)
    if arguments.oss_manifest is None:
        arguments.oss_manifest = SCRIPT_ROOT / "build" / "oss" / arguments.experiment / "manifest.json"
    if arguments.oracle_manifest is None:
        arguments.oracle_manifest = SCRIPT_ROOT / "build" / "oracle" / arguments.experiment / "manifest.json"
    if arguments.output_dir is None:
        arguments.output_dir = SCRIPT_ROOT / "build" / "compare" / arguments.experiment
    return arguments


def main(argv: Sequence[str] | None = None) -> int:
    try:
        arguments = _arguments(argv)
        oss_path = Path(arguments.oss_manifest)
        oracle_path = Path(arguments.oracle_manifest)
        try:
            oss = _read_manifest(oss_path)
            oracle = _read_manifest(oracle_path)
        except ComparisonError as exc:
            if oracle_path.is_symlink() or not oracle_path.is_file():
                reason = f"Quartus oracle unavailable; comparison requires an oracle manifest ({exc})"
            elif oss_path.is_symlink() or not oss_path.is_file():
                reason = f"OSS manifest unavailable; comparison requires an OSS manifest ({exc})"
            else:
                reason = str(exc)
            comparison = _unavailable_comparison(arguments.experiment, oss_path, oracle_path, reason)
            json_path, markdown_path = write_comparison(comparison, Path(arguments.output_dir))
            print(f"comparison: {json_path}")
            print(f"comparison markdown: {markdown_path}")
            print("comparison failed: " + reason, file=sys.stderr)
            return 2
        comparison = compare_manifests(
            oss,
            oracle,
            oss_path=oss_path,
            oracle_path=oracle_path,
            repo_root=SCRIPT_ROOT,
            experiment=arguments.experiment,
            hardware_observation=arguments.hardware_observation,
        )
        json_path, markdown_path = write_comparison(comparison, Path(arguments.output_dir))
        print(f"comparison: {json_path}")
        print(f"comparison markdown: {markdown_path}")
        if comparison["status"] != "pass":
            print("comparison failed: " + "; ".join(comparison["failures"]), file=sys.stderr)
            return 2
        return 0
    except (ComparisonError, OSError, ValueError) as exc:
        print(f"compare_builds: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
