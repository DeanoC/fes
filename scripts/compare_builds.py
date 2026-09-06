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
import shlex
import sys
from pathlib import Path
from typing import Any, Mapping, Sequence

try:  # Imports work both as ``scripts.compare_builds`` and as a CLI script.
    from .experiment_policy import PolicyError, policy_for
except ImportError:  # pragma: no cover - exercised by the script entry point.
    from experiment_policy import PolicyError, policy_for

try:  # The comparator must reopen the repository lock instead of trusting manifests.
    from .lockfile import load_lock
except ImportError:  # pragma: no cover - exercised by the script entry point.
    from lockfile import load_lock


SCRIPT_ROOT = Path(__file__).resolve().parents[1]
TARGET_DEVICE = "5CSEBA6U23I7"
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
REQUIRED_HARD_BLOCKS = ("PLL", "BRAM/M10K", "MLAB/LUTRAM", "DSP", "HPS")
MEASURED_HARD_BLOCKS = ("PLL", "BRAM/M10K", "DSP")
STATIC_HARD_BLOCKS = ("MLAB/LUTRAM", "HPS")
MAILBOX_ALLOWED_HARD_BLOCK = "cyclonev_hps_interface_mpu_general_purpose"
SYNTHESIS_REPORT_SUFFIXES = {
    "oss": "timing.json",
    "oracle": "top.fit.rpt",
}
MAILBOX_STATIC_PROOF_BASIS = "static source/project/command exclusion"
MAILBOX_STATIC_PROOF_PATTERNS = (
    "PLL",
    "phase_locked",
    "BRAM",
    "M10K",
    "RAM",
    "ram_block",
    "MLAB",
    "LUTRAM",
    "DSP",
    "MAC",
    "MUL",
    "oscillator",
    "SDRAM",
    "video",
    "audio",
    "HPS",
    "MPU",
    "ARM",
)
SEMANTIC_RESOURCE_FIELDS = (
    "clock_inputs",
    "external_input_ports",
    "external_output_ports",
    "bidirectional_ports",
    "hps_general_purpose_interfaces",
    "pll_blocks",
    "dsp_blocks",
    "block_memory_bits",
    "lutram_bits",
    "sdram_interfaces",
)
QUARTUS_VERSION_RE = re.compile(r"(^|[^0-9])17\.0\.2(?:\s|$)")
STATIC_EXCLUSION_CONTRACTS = {
    "MLAB/LUTRAM": {
        "basis": "static source/project exclusion",
        "patterns": (
            r"\bmlab(?:s)?\b",
            r"\blutram\b",
            r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
            r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
        ),
    },
    "HPS": {
        "basis": "static source/project exclusion",
        "patterns": (
            r"\bhps\b",
            r"\bhard[_ ]processor",
            r"\b(?:altera|cyclonev)[_ ]hps\b",
            r"\b(?:hps_component|soc_system|soc_id|arm)\b",
            r"\bsoc\b",
        ),
    },
}
OSS_STATIC_FIELD_PATTERNS = {
    # Keep the serialized contracts field-specific.  Alphabetic identifiers
    # use experiment_policy.py's Verilog boundary semantics rather than
    # Python's ``\b`` (which treats underscores as word characters).
    "pll_blocks": ("PLL", "MISTRAL_PLL", "phase_locked", "altpll"),
    "dsp_blocks": (
        "DSP",
        "MUL",
        "MISTRAL_MUL9X9",
        "MISTRAL_MUL18X18",
        "MISTRAL_MUL27X27",
        "MAC",
    ),
    "block_memory_bits": (
        "BRAM",
        "M10K",
        "MISTRAL_M10K",
        "M20K",
        "RAM",
        "ram_block",
        "altsyncram",
    ),
    "lutram_bits": (
        "MLAB",
        "MISTRAL_MLAB",
        "LUTRAM",
        "altsyncram",
        "lpm_ram",
        "mlab_cell",
        r"re:(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
    ),
    "sdram_interfaces": (
        "SDRAM",
        "DDR",
        "cyclonev_hps_interface_fpga2sdram",
        "hps_sdram",
    ),
}
OSS_AUTHENTICATED_TOOL_PATHS = {
    "yosys": "build/toolchain/install/bin/yosys",
    "nextpnr-mistral": "build/toolchain/install/bin/nextpnr-mistral",
}


class ComparisonError(ValueError):
    """Raised when comparison inputs or output paths are unsafe."""


def _verilog_source_pattern_matches(pattern: str, source_text: str) -> bool:
    """Match one static-source pattern with experiment-policy boundaries."""

    if pattern.startswith("re:"):
        expression = pattern[3:]
    elif pattern.isalpha():
        expression = rf"(?<![A-Za-z]){re.escape(pattern)}(?![A-Za-z])"
    else:
        expression = re.escape(pattern)
    return re.search(expression, source_text, re.IGNORECASE | re.MULTILINE) is not None


def _static_patterns_for_field(field: str, lane: str) -> list[str]:
    if lane == "oss" and field in OSS_STATIC_FIELD_PATTERNS:
        return list(OSS_STATIC_FIELD_PATTERNS[field])
    return list(MAILBOX_STATIC_PROOF_PATTERNS)


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


def _canonical_manifest_tool_pins(
    repo_root: Path,
) -> tuple[dict[str, dict[str, Any]] | None, list[str]]:
    """Reopen and normalize the repository lock for manifest provenance."""

    try:
        pins = load_lock(repo_root / "toolchain.lock")
    except (OSError, ValueError) as exc:
        return None, [f"oss canonical toolchain.lock cannot be loaded: {exc}"]
    return (
        {
            name: {
                "commit": pin.commit,
                "order": pin.order,
                "rationale": pin.rationale,
                "repo": pin.repo,
            }
            for name, pin in sorted(pins.items())
        },
        [],
    )


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


def _source_records(manifest: Mapping[str, Any]) -> tuple[dict[str, str], list[str]]:
    raw_records = manifest.get("sources")
    if not isinstance(raw_records, list) or not raw_records:
        return {}, ["common source hash records are missing"]
    records: dict[str, str] = {}
    failures: list[str] = []
    for raw in raw_records:
        if not isinstance(raw, dict):
            failures.append("source hash record is malformed")
            continue
        path = raw.get("path")
        digest = raw.get("sha256")
        if not isinstance(path, str) or not path:
            failures.append("source hash record has no path")
            continue
        if path in records:
            failures.append(f"duplicate source hash record: {path}")
            continue
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"source hash is invalid: {path}")
            continue
        records[path] = digest
    return records, failures


def _summary_source_records(
    build: Mapping[str, Any],
    lane: str,
    experiment: str,
    manifest_sources: Mapping[str, str],
) -> list[str]:
    """Check the build summary's common-input hashes against manifest sources.

    Schema-2 manifests expose the collector's top-level ``sources`` records,
    while the normalized build summary also carries the hashes used by the
    compiler invocation.  Requiring the common records in both places prevents
    a stale summary from being paired with a newly collected manifest (or vice
    versa).
    """

    failures: list[str] = []
    raw = build.get("source_hashes")
    if not isinstance(raw, dict) or not raw:
        return [f"{lane} build source hash map is missing or malformed"]
    summary_sources: dict[str, str] = {}
    for path, digest in raw.items():
        if not isinstance(path, str) or not path:
            failures.append(f"{lane} build source hash path is malformed")
            continue
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"{lane} build source hash is invalid: {path}")
            continue
        summary_sources[path] = digest

    common_sources = (
        f"experiments/{experiment}/rtl/top.v",
        "boards/de10nano/pins.qsf",
        "boards/de10nano/clocks.sdc",
    )
    for path in common_sources:
        manifest_digest = manifest_sources.get(path)
        summary_digest = summary_sources.get(path)
        if not isinstance(manifest_digest, str):
            failures.append(f"{lane} common source hash is missing: {path}")
        if not isinstance(summary_digest, str):
            failures.append(f"{lane} build common source hash is missing: {path}")
        if (
            isinstance(manifest_digest, str)
            and isinstance(summary_digest, str)
            and manifest_digest != summary_digest
        ):
            failures.append(f"{lane} common source hash disagrees with build summary: {path}")
    return failures


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


def _expected_rbf_path(raw: Any, lane: str, experiment: str) -> bool:
    if not isinstance(raw, str) or not raw:
        return False
    expected = f"build/{lane}/{experiment}/top.rbf"
    # collect_manifest emits repository-relative POSIX paths.  Requiring that
    # exact spelling prevents an absolute path outside the repository from
    # masquerading as a lane artifact merely because it has the same suffix.
    return not Path(raw).is_absolute() and raw == expected


def _expected_synthesis_report_path(lane: str, experiment: str) -> str | None:
    suffix = SYNTHESIS_REPORT_SUFFIXES.get(lane)
    if suffix is None or experiment != "020_linux_mailbox":
        return None
    return f"build/{lane}/{experiment}/{suffix}"


def _canonical_relative_path(raw: Any) -> bool:
    """Reject alternate spellings that could escape a lane-bound path."""

    if not isinstance(raw, str) or not raw or Path(raw).is_absolute():
        return False
    path = Path(raw)
    return path.as_posix() == raw and all(part not in {"", ".", ".."} for part in path.parts)


def _artifact_records(
    manifest: Mapping[str, Any],
    manifest_path: Path,
    repo_root: Path,
    lane: str,
    experiment: str,
) -> tuple[list[dict[str, Any]], list[str]]:
    raw_records = manifest.get("artifacts")
    if not isinstance(raw_records, list):
        return [], ["missing required artifact list"]
    records: list[dict[str, Any]] = []
    failures: list[str] = []
    expected_rbf = f"build/{lane}/{experiment}/top.rbf"
    rbf_record_count = 0
    seen_paths: set[str] = set()
    for raw in raw_records:
        if not isinstance(raw, dict):
            failures.append("artifact record is malformed")
            continue
        raw_path = raw.get("path")
        digest = raw.get("sha256")
        if not _canonical_relative_path(raw_path):
            failures.append(f"{lane} artifact path is not a canonical relative path: {raw_path}")
        elif raw_path in seen_paths:
            failures.append(f"duplicate artifact path: {raw_path}")
        else:
            seen_paths.add(raw_path)
        is_rbf = isinstance(raw_path, str) and raw_path.lower().endswith(".rbf")
        if experiment == "020_linux_mailbox" and isinstance(raw_path, str):
            expected_prefix = Path("build") / lane / experiment
            if Path(raw_path).parts[:3] != expected_prefix.parts:
                failures.append(
                    f"{lane} artifact path is not bound to its lane and experiment: {raw_path}"
                )
        if is_rbf and not _expected_rbf_path(raw_path, lane, experiment):
            failures.append(f"RBF artifact path is not the exact {expected_rbf}: {raw_path}")
        if is_rbf and _expected_rbf_path(raw_path, lane, experiment):
            rbf_record_count += 1
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
        entry["size_bytes"] = resolved.stat().st_size
        declared_size = raw.get("size_bytes")
        if declared_size is not None and (
            isinstance(declared_size, bool)
            or not isinstance(declared_size, int)
            or declared_size < 0
            or declared_size != entry["size_bytes"]
        ):
            failures.append(f"artifact size mismatch: {raw_path}")
        if actual != digest:
            failures.append(f"artifact hash mismatch: {raw_path}")
        records.append(entry)

    rbf_records = [
        record
        for record in records
        if _expected_rbf_path(record.get("path"), lane, experiment)
    ]
    if rbf_record_count != 1:
        failures.append(f"missing required artifact: exact RBF path {expected_rbf}")
    elif not any(record.get("present") and record.get("hash_matches") for record in rbf_records):
        failures.append("missing required artifact: usable RBF")

    build = manifest.get("build")
    reproducibility = build.get("reproducibility") if isinstance(build, dict) else None
    expected_digest = reproducibility.get("rbf_sha256") if isinstance(reproducibility, dict) else None
    expected_size = reproducibility.get("rbf_size_bytes") if isinstance(reproducibility, dict) else None
    if not isinstance(expected_digest, str) or SHA256_RE.fullmatch(expected_digest) is None:
        failures.append("RBF reproducibility SHA-256 is missing or invalid")
    if isinstance(expected_size, bool) or not isinstance(expected_size, int) or expected_size < 1:
        failures.append("RBF reproducibility size is missing or invalid")
    if rbf_records:
        rbf = rbf_records[0]
        if isinstance(expected_digest, str) and rbf.get("sha256") != expected_digest:
            failures.append("RBF artifact hash disagrees with reproducibility hash")
        if rbf.get("present") and isinstance(expected_size, int) and rbf.get("size_bytes") != expected_size:
            failures.append("RBF artifact size disagrees with reproducibility size")
    return records, failures


def _canonical_policy_hash(policy: Any) -> str:
    encoded = json.dumps(
        policy.as_dict(), ensure_ascii=True, separators=(",", ":"), sort_keys=True
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _report_binding_failures(
    manifest: Mapping[str, Any],
    build: Mapping[str, Any],
    artifacts: Sequence[Mapping[str, Any]],
    lane: str,
    experiment: str,
) -> tuple[dict[str, str], list[str]]:
    """Require one opened, lane-specific synthesis report in every evidence layer."""

    expected_path = _expected_synthesis_report_path(lane, experiment)
    if expected_path is None:
        return {}, []

    failures: list[str] = []
    artifact_matches = [
        record for record in artifacts if record.get("path") == expected_path
    ]
    if len(artifact_matches) != 1:
        failures.append(
            f"{lane} synthesis report artifact must occur exactly once at {expected_path}"
        )
    artifact = artifact_matches[0] if len(artifact_matches) == 1 else None
    actual_digest = artifact.get("actual_sha256") if isinstance(artifact, Mapping) else None
    if artifact is not None:
        if not artifact.get("present") or not artifact.get("hash_matches"):
            failures.append(f"{lane} synthesis report artifact is missing or hash-mismatched")
        if not isinstance(actual_digest, str) or SHA256_RE.fullmatch(actual_digest) is None:
            failures.append(f"{lane} synthesis report artifact hash is unavailable")
        size_bytes = artifact.get("size_bytes")
        if isinstance(size_bytes, bool) or not isinstance(size_bytes, int) or size_bytes < 1:
            failures.append(f"{lane} synthesis report artifact is empty")

    expected: dict[str, str] = {}
    for label, record in (("manifest", manifest), ("build", build)):
        path = record.get("synthesis_report_path")
        digest = record.get("synthesis_report_sha256")
        if path != expected_path:
            failures.append(
                f"{lane} {label} synthesis_report_path must be exactly {expected_path}"
            )
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"{lane} {label} synthesis_report_sha256 is missing or invalid")
        elif isinstance(actual_digest, str) and digest != actual_digest:
            failures.append(f"{lane} {label} synthesis report hash disagrees with opened report")
        elif not expected:
            expected = {"path": expected_path, "sha256": digest}
        elif digest != expected.get("sha256"):
            failures.append(f"{lane} synthesis report hashes disagree between manifest and build")

        binding = record.get("synthesis_report")
        if not isinstance(binding, dict) or set(binding) != {"path", "sha256"}:
            failures.append(f"{lane} {label} synthesis_report binding is missing or malformed")
        elif binding != {"path": expected_path, "sha256": digest}:
            failures.append(f"{lane} {label} synthesis_report binding disagrees with direct fields")

    if not expected:
        expected = {"path": expected_path, "sha256": ""}
    return expected, failures


def _mailbox_policy_failures(
    manifest: Mapping[str, Any],
    build: Mapping[str, Any],
    source_hashes: Mapping[str, str],
    lane: str,
) -> tuple[Any | None, list[str]]:
    """Validate immutable mailbox policy/protocol bindings in one lane."""

    failures: list[str] = []
    try:
        policy = policy_for("020_linux_mailbox")
    except PolicyError as exc:  # pragma: no cover - closed table regression.
        return None, [f"{lane} mailbox policy is unavailable: {exc}"]
    expected_policy = policy.as_dict()
    expected_policy_hash = _canonical_policy_hash(policy)
    protocol_path = "experiments/020_linux_mailbox/rtl/top.v"
    expected_protocol_hash = source_hashes.get(protocol_path)
    for label, record in (("manifest", manifest), ("build", build)):
        descriptor = record.get("experiment_policy")
        if not isinstance(descriptor, dict):
            failures.append(f"{lane} {label} experiment_policy is missing or malformed")
        elif descriptor != expected_policy:
            failures.append(f"{lane} {label} experiment_policy does not match the closed policy")

        declared_hash = record.get("experiment_policy_sha256")
        alias_hash = record.get("policy_sha256")
        if declared_hash is None:
            declared_hash = alias_hash
        if not isinstance(declared_hash, str) or SHA256_RE.fullmatch(declared_hash) is None:
            failures.append(f"{lane} {label} experiment policy hash is missing or invalid")
        elif declared_hash != expected_policy_hash:
            failures.append(f"{lane} {label} experiment policy hash does not match policy")
        if alias_hash is not None and (
            not isinstance(alias_hash, str)
            or SHA256_RE.fullmatch(alias_hash) is None
            or alias_hash != expected_policy_hash
        ):
            failures.append(f"{lane} {label} policy_sha256 does not match policy")

        protocol_hash = record.get("protocol_source_sha256")
        if not isinstance(protocol_hash, str) or SHA256_RE.fullmatch(protocol_hash) is None:
            failures.append(f"{lane} {label} protocol_source_sha256 is missing or invalid")
        elif isinstance(expected_protocol_hash, str) and protocol_hash != expected_protocol_hash:
            failures.append(f"{lane} {label} protocol_source_sha256 disagrees with source")
        protocol_source = record.get("protocol_source")
        if protocol_source is not None and protocol_source != protocol_path:
            failures.append(f"{lane} {label} protocol_source is not the production RTL")

        allowed = record.get("allowed_hard_blocks")
        if allowed is not None and allowed != dict(policy.allowed_hard_blocks):
            failures.append(f"{lane} {label} allowed_hard_blocks does not match policy")
        elif allowed is None and label == "build":
            failures.append(f"{lane} build allowed_hard_blocks is missing")

        if label == "build":
            if record.get("experiment") != "020_linux_mailbox":
                failures.append(f"{lane} build experiment is not 020_linux_mailbox")
            if record.get("target") != policy.target:
                failures.append(f"{lane} build target is not {policy.target}")
            clock_intent = record.get("clock_intent")
            if clock_intent != policy.clock:
                failures.append(f"{lane} build clock intent is not {policy.clock}")
            clock_constraint = _number(record.get("clock_constraint_mhz"))
            if clock_constraint != float(policy.clock_mhz):
                failures.append(f"{lane} build clock constraint is not {policy.clock_mhz:g} MHz")
    return policy, failures


def _semantic_resource_failures(
    manifest: Mapping[str, Any],
    build: Mapping[str, Any],
    lane: str,
) -> tuple[dict[str, int], list[str]]:
    """Validate the exact mailbox semantic resource evidence object."""

    failures: list[str] = []
    candidates = (("manifest", manifest.get("resource_evidence")), ("build", build.get("resource_evidence")))
    decoded: dict[str, int] | None = None
    for label, raw in candidates:
        if not isinstance(raw, dict):
            failures.append(f"{lane} {label} resource_evidence is missing or malformed")
            continue
        if tuple(raw) != SEMANTIC_RESOURCE_FIELDS:
            failures.append(f"{lane} {label} resource_evidence fields are not canonical")
        if set(raw) != set(SEMANTIC_RESOURCE_FIELDS):
            failures.append(f"{lane} {label} resource_evidence field set is not exact")
            continue
        current: dict[str, int] = {}
        for field in SEMANTIC_RESOURCE_FIELDS:
            value = raw.get(field)
            if type(value) is not int or value < 0 or value > 0xFFFFFFFF:
                failures.append(f"{lane} {label} resource_evidence.{field} is not a uint32")
            else:
                current[field] = value
        if decoded is None and len(current) == len(SEMANTIC_RESOURCE_FIELDS):
            decoded = current
        elif decoded is not None and current != decoded:
            failures.append(f"{lane} manifest/build resource_evidence disagrees")

    evidence = decoded or {}
    expected = {
        "clock_inputs": 1,
        "external_input_ports": 0,
        "external_output_ports": 0,
        "bidirectional_ports": 0,
        "hps_general_purpose_interfaces": 1,
        "pll_blocks": 0,
        "dsp_blocks": 0,
        "block_memory_bits": 0,
        "lutram_bits": 0,
        "sdram_interfaces": 0,
    }
    for field, expected_value in expected.items():
        if field in evidence and evidence[field] != expected_value:
            failures.append(
                f"{lane} resource_evidence.{field} is {evidence[field]}, expected {expected_value}"
            )
    return evidence, failures


def _mailbox_hard_block_view(
    build: Mapping[str, Any],
    lane: str,
    experiment: str,
    manifest_sources: Mapping[str, str],
    manifest_commands: Sequence[Mapping[str, str]] = (),
) -> tuple[dict[str, dict[str, Any]], list[str]]:
    """Validate the mailbox's exact HPS primitive and forbidden-resource evidence."""

    failures: list[str] = []
    policy = policy_for("020_linux_mailbox")
    if lane == "oracle":
        raw = build.get("hard_block_evidence")
        legacy = build.get("hard_blocks")
        if not isinstance(raw, dict) or not raw:
            return {}, ["oracle hard-block evidence map is missing or empty"]
        if not isinstance(legacy, dict) or not legacy:
            return {}, ["oracle hard-block summary map is missing or empty"]
        if raw != legacy:
            failures.append("oracle hard-block summary disagrees with evidence")
    else:
        raw = build.get("hard_blocks")
        legacy = None
        if not isinstance(raw, dict) or not raw:
            return {}, [f"{lane} hard-block evidence map is missing or empty"]

    view: dict[str, dict[str, Any]] = {}
    expected_oracle = {"PLL", "BRAM/M10K", "DSP", "MLAB/LUTRAM", MAILBOX_ALLOWED_HARD_BLOCK}
    for name, record in raw.items():
        if not isinstance(name, str) or not name or not isinstance(record, dict):
            failures.append(f"{lane} hard-block evidence record is malformed")
            continue
        view[name] = dict(record)
        used = record.get("used")
        if type(used) is not int or used < 0:
            if lane == "oracle" and name == "MLAB/LUTRAM" and used is None:
                pass
            else:
                failures.append(f"{lane} hard-block evidence is malformed: {name}")
                continue
        if name == MAILBOX_ALLOWED_HARD_BLOCK:
            if used != 1:
                failures.append(f"{lane} allowed primitive must be used exactly once")
            if lane == "oracle" and (
                record.get("evidence_kind") != "fitter_summary" or record.get("measured") is not True
            ):
                failures.append(f"oracle allowed primitive evidence is not measured fitter evidence")
        elif name in {"PLL", "BRAM/M10K", "DSP"}:
            if lane == "oracle" and (
                record.get("evidence_kind") != "fitter_summary" or record.get("measured") is not True
            ):
                failures.append(f"oracle {name} evidence kind/completeness is invalid")
            if used != 0:
                failures.append(f"{lane} forbidden hard block in use: {name}={used}")
        elif name == "MLAB/LUTRAM" and lane == "oracle":
            failures.extend(
                _exclusion_failures(
                    record,
                    lane,
                    name,
                    experiment,
                    manifest_sources,
                    manifest_commands,
                )
            )
        else:
            classification = policy.classify_resource(name)
            if classification == "forbidden" and used == 0:
                continue
            failures.append(f"{lane} unexpected hard block evidence: {name}")

    if MAILBOX_ALLOWED_HARD_BLOCK not in view:
        failures.append(f"{lane} hard-block evidence is missing allowed primitive: {MAILBOX_ALLOWED_HARD_BLOCK}")
    if lane == "oracle":
        unknown = sorted(set(view) - expected_oracle)
        if unknown:
            failures.append(f"oracle hard-block evidence has unrecognized classes: {', '.join(unknown)}")
        if "MLAB/LUTRAM" not in view:
            failures.append("oracle hard-block evidence is missing required class: MLAB/LUTRAM")
        if isinstance(legacy, dict):
            unknown_legacy = sorted(set(legacy) - expected_oracle)
            if unknown_legacy:
                failures.append(f"oracle hard-block summary has unrecognized classes: {', '.join(unknown_legacy)}")
    return view, failures


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


def _canonical_static_paths(experiment: str) -> tuple[str, ...]:
    return (
        f"experiments/{experiment}/rtl/top.v",
        "boards/de10nano/pins.qsf",
        "boards/de10nano/clocks.sdc",
        f"experiments/{experiment}/oracle/top.qsf",
    )


def _canonical_static_paths_for_lane(experiment: str, lane: str) -> tuple[str, ...]:
    common = (
        f"experiments/{experiment}/rtl/top.v",
        "boards/de10nano/pins.qsf",
        "boards/de10nano/clocks.sdc",
    )
    if lane == "oracle" and experiment == "020_linux_mailbox":
        return (*common, f"experiments/{experiment}/oracle/top.qpf", f"experiments/{experiment}/oracle/top.qsf")
    if lane == "oracle":
        return (*common, f"experiments/{experiment}/oracle/top.qsf")
    return common


def _logged_command_tokens(path: Path, lane: str, command_name: str) -> tuple[list[str], list[str]]:
    try:
        first_line = path.read_text(encoding="utf-8", errors="replace").splitlines()[0]
    except (OSError, IndexError) as exc:
        return [], [f"{lane} command log cannot provide its first command header: {path} ({exc})"]
    if not first_line.startswith("command:"):
        return [], [f"{lane} {command_name} does not start with a command header"]
    try:
        tokens = shlex.split(first_line[len("command:") :].strip())
    except ValueError as exc:
        return [], [f"{lane} {command_name} command header is not shell-parseable: {exc}"]
    if not tokens:
        return [], [f"{lane} {command_name} command header is empty"]
    return tokens, []


def _oss_command_contract_failures(
    path: Path,
    command_name: str,
    experiment: str,
) -> list[str]:
    """Validate the small trusted-host command contract behind OSS zeros.

    The command-log digest proves which bytes were supplied, but not that the
    bytes describe the synthesis and route operation that produced the
    evidence.  Keep this contract deliberately narrow and lane-specific: it
    checks the exact mailbox Yosys/nextpnr argv, including source, target,
    constraints, synthesis controls, and output paths.
    """

    if experiment != "020_linux_mailbox" or command_name not in {
        "yosys.log",
        "nextpnr-help.log",
        "nextpnr.log",
    }:
        return []
    tokens, failures = _logged_command_tokens(path, "oss", command_name)
    if failures:
        return failures
    expected_yosys_program = (
        "read_verilog experiments/020_linux_mailbox/rtl/top.v; "
        "synth_intel_alm -nobram -nolutram -nodsp -top top; stat; "
        "write_json build/oss/020_linux_mailbox/synth.json"
    )
    if command_name == "yosys.log":
        if len(tokens) != 3 or Path(tokens[0]).name != "yosys" or tokens[1] != "-p":
            return ["oss yosys.log command contract is not the authenticated mailbox synthesis argv"]
        if tokens[2] != expected_yosys_program:
            return ["oss yosys.log command contract has wrong flags, source, top, or synthesis output"]
        return []
    if command_name == "nextpnr-help.log":
        if len(tokens) != 2 or Path(tokens[0]).name != "nextpnr-mistral" or tokens[1] != "--help":
            return ["oss nextpnr-help.log command contract is not the authenticated nextpnr help argv"]
        return []
    expected = [
        "--json",
        "build/oss/020_linux_mailbox/synth.json",
        "--device",
        TARGET_DEVICE,
        "--qsf",
        "boards/de10nano/pins.qsf",
        "--sdc",
        "boards/de10nano/clocks.sdc",
        "--freq",
        "50",
        "--rbf",
        "build/oss/020_linux_mailbox/top.rbf",
        "--compress-rbf",
        "--write",
        "build/oss/020_linux_mailbox/routed.json",
        "--report",
        "build/oss/020_linux_mailbox/timing.json",
        "--detailed-timing-report",
    ]
    if len(tokens) != len(expected) + 1 or Path(tokens[0]).name != "nextpnr-mistral" or tokens[1:] != expected:
        return ["oss nextpnr.log command contract has wrong flags, target, constraints, or outputs"]
    return []


def _oss_static_source_scan_failures(
    source_hashes: Mapping[str, str],
    repo_root: Path,
    field: str,
    experiment: str,
    lane: str,
) -> list[str]:
    """Re-open canonical OSS source/project inputs and scan one field contract."""

    patterns = OSS_STATIC_FIELD_PATTERNS.get(field)
    if lane != "oss" or experiment != "020_linux_mailbox" or patterns is None:
        return []
    failures: list[str] = []
    expected_paths = _canonical_static_paths_for_lane(experiment, lane)
    for relative in expected_paths:
        declared = source_hashes.get(relative)
        if not isinstance(declared, str) or SHA256_RE.fullmatch(declared) is None:
            failures.append(f"oss {field} static source hash is missing or invalid: {relative}")
            continue
        path = repo_root / relative
        if _contains_symlink(Path(os.path.abspath(os.fspath(path)))) or not path.is_file():
            failures.append(f"oss {field} static source is missing or not regular: {relative}")
            continue
        try:
            source_bytes = path.read_bytes()
            source_text = source_bytes.decode("utf-8", errors="replace")
        except OSError as exc:
            failures.append(f"oss {field} static source cannot be read: {relative} ({exc})")
            continue
        actual = hashlib.sha256(source_bytes).hexdigest()
        if actual != declared:
            failures.append(f"oss {field} static source hash does not match opened file: {relative}")
            continue
        for pattern in patterns:
            if _verilog_source_pattern_matches(pattern, source_text):
                failures.append(
                    f"oss {field} static source scan matched forbidden pattern {pattern!r}: {relative}"
                )
    return failures


def _oss_authenticated_tool_failures(
    build: Mapping[str, Any],
    command_records: Sequence[Mapping[str, str]],
    *,
    manifest_path: Path,
    repo_root: Path,
    experiment: str,
    lane: str,
    manifest: Mapping[str, Any] | None = None,
) -> list[str]:
    """Reopen OSS tools and bind every logged executable token to its record."""

    if lane != "oss" or experiment != "020_linux_mailbox":
        return []
    failures: list[str] = []
    canonical_pins, lock_failures = _canonical_manifest_tool_pins(repo_root)
    failures.extend(lock_failures)
    if canonical_pins is not None:
        manifest_pins = manifest.get("tool_pins") if isinstance(manifest, Mapping) else None
        if manifest_pins != canonical_pins:
            failures.append("oss manifest tool_pins do not match reopened toolchain.lock")
    required_tools = ("yosys", "nextpnr-mistral")
    authenticated = build.get("authenticated_tools")
    if not isinstance(authenticated, dict) or set(authenticated) != set(required_tools):
        return ["oss authenticated_tools must contain exactly yosys and nextpnr-mistral"]
    pins = build.get("tool_pins")
    if not isinstance(pins, dict) or set(pins) != {"yosys", "nextpnr"}:
        failures.append("oss tool_pins must contain exactly yosys and nextpnr")

    tool_paths: dict[str, Path] = {}
    for tool in required_tools:
        record = authenticated.get(tool)
        if not isinstance(record, dict):
            failures.append(f"oss authenticated {tool} record is missing or malformed")
            continue
        for field in ("commit", "path", "sha256"):
            if not isinstance(record.get(field), str) or not record[field]:
                failures.append(f"oss authenticated {tool} {field} is missing or malformed")
        commit = record.get("commit")
        digest = record.get("sha256")
        if not isinstance(commit, str) or COMMIT_RE.fullmatch(commit) is None:
            failures.append(f"oss authenticated {tool} commit is invalid")
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"oss authenticated {tool} sha256 is invalid")
        raw_path = record.get("path")
        if not isinstance(raw_path, str) or not raw_path:
            continue
        expected_relative = OSS_AUTHENTICATED_TOOL_PATHS[tool]
        if raw_path != expected_relative:
            failures.append(
                f"oss authenticated {tool} path must be the canonical repository tool path: {raw_path}"
            )
            continue
        raw_path_obj = Path(raw_path)
        if raw_path_obj.as_posix() != raw_path or any(
            part in {"", ".", ".."} for part in raw_path_obj.parts
        ):
            failures.append(f"oss authenticated {tool} path is not canonical: {raw_path}")
            continue
        candidate = Path(
            os.path.abspath(
                os.fspath(raw_path_obj if raw_path_obj.is_absolute() else repo_root / raw_path_obj)
            )
        )
        if not _is_within(candidate, repo_root):
            failures.append(f"oss authenticated {tool} path is outside the repository: {raw_path}")
            continue
        if _contains_symlink(candidate) or not candidate.is_file() or not os.access(candidate, os.X_OK):
            failures.append(f"oss authenticated {tool} path is missing or not executable: {raw_path}")
            continue
        if candidate.name != tool:
            failures.append(f"oss authenticated {tool} path has the wrong basename")
        if isinstance(digest, str) and SHA256_RE.fullmatch(digest):
            try:
                actual = hashlib.sha256(candidate.read_bytes()).hexdigest()
            except OSError as exc:
                failures.append(f"cannot hash oss authenticated {tool}: {exc}")
            else:
                if actual != digest:
                    failures.append(f"oss authenticated {tool} sha256 does not match opened executable")
        lock_name = "nextpnr" if tool == "nextpnr-mistral" else tool
        pin = pins.get(lock_name) if isinstance(pins, dict) else None
        pin_commit = pin.get("commit") if isinstance(pin, dict) else pin
        if not isinstance(pin_commit, str) or COMMIT_RE.fullmatch(pin_commit) is None:
            failures.append(f"oss {lock_name} tool pin commit is missing or invalid")
        elif isinstance(commit, str) and pin_commit != commit:
            failures.append(f"oss authenticated {tool} commit disagrees with tool pin")
        canonical_pin = canonical_pins.get(lock_name) if canonical_pins is not None else None
        canonical_commit = (
            canonical_pin.get("commit") if isinstance(canonical_pin, Mapping) else None
        )
        if not isinstance(canonical_commit, str) or COMMIT_RE.fullmatch(canonical_commit) is None:
            failures.append(f"oss canonical {lock_name} toolchain.lock pin is missing or invalid")
        else:
            if isinstance(pin_commit, str) and pin_commit != canonical_commit:
                failures.append(f"oss {lock_name} tool pin disagrees with reopened toolchain.lock")
            if isinstance(commit, str) and commit != canonical_commit:
                failures.append(
                    f"oss authenticated {tool} commit disagrees with reopened toolchain.lock"
                )
        tool_paths[tool] = candidate

    expected_logs = {
        "yosys.log": "yosys",
        "nextpnr-help.log": "nextpnr-mistral",
        "nextpnr.log": "nextpnr-mistral",
    }
    by_name = {
        Path(record.get("path", "")).name: record
        for record in command_records
        if isinstance(record, Mapping) and isinstance(record.get("path"), str)
    }
    for command_name, tool in expected_logs.items():
        record = by_name.get(command_name)
        if record is None:
            failures.append(f"oss {command_name} executable provenance record is missing")
            continue
        relative = record.get("path")
        resolved = _artifact_path(relative, manifest_path, repo_root)
        if resolved is None or resolved.is_symlink() or not resolved.is_file():
            failures.append(f"oss {command_name} command log is missing or not regular")
            continue
        try:
            first_line = resolved.read_text(encoding="utf-8", errors="replace").splitlines()[0]
            if not first_line.startswith("command:"):
                raise ValueError("missing command header")
            tokens = shlex.split(first_line[len("command:") :].strip())
        except (OSError, IndexError, ValueError) as exc:
            failures.append(f"oss {command_name} command header cannot be parsed: {exc}")
            continue
        expected_path = tool_paths.get(tool)
        token = Path(tokens[0]) if tokens else None
        if expected_path is None or token is None or not token.is_absolute() or Path(os.path.abspath(os.fspath(token))) != expected_path:
            failures.append(f"oss {command_name} executable token is not bound to authenticated {tool}")
    return failures


def _canonical_command_records(
    manifest: Mapping[str, Any],
    manifest_path: Path,
    repo_root: Path,
    lane: str,
    experiment: str,
) -> tuple[list[dict[str, str]], list[str]]:
    """Open and hash every command log used by a mailbox static proof."""

    raw_records = manifest.get("command_logs")
    if not isinstance(raw_records, list):
        return [], [f"{lane} command log records are missing or malformed"]
    by_path: dict[str, Mapping[str, Any]] = {}
    failures: list[str] = []
    for raw in raw_records:
        if not isinstance(raw, dict):
            failures.append(f"{lane} command log record is malformed")
            continue
        path = raw.get("path")
        digest = raw.get("sha256")
        if not _canonical_relative_path(path):
            failures.append(f"{lane} command log path is not canonical: {path}")
            continue
        if path in by_path:
            failures.append(f"duplicate {lane} command log path: {path}")
            continue
        by_path[path] = raw
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"{lane} command log hash is invalid: {path}")
            continue
        expected_prefix = f"build/{lane}/{experiment}/"
        if not path.startswith(expected_prefix):
            failures.append(f"{lane} command log is not bound to its lane and experiment: {path}")
            continue
        resolved = _artifact_path(path, manifest_path, repo_root)
        if resolved.is_symlink() or _contains_symlink(Path(os.path.abspath(os.fspath(resolved)))) or not resolved.is_file():
            failures.append(f"{lane} command log is missing or not regular: {path}")
            continue
        try:
            actual = hashlib.sha256(resolved.read_bytes()).hexdigest()
        except OSError as exc:
            failures.append(f"cannot hash {lane} command log {path}: {exc}")
            continue
        if actual != digest:
            failures.append(f"{lane} command log hash mismatch: {path}")
        if lane == "oss" and experiment == "020_linux_mailbox":
            command_name = Path(path).name
            failures.extend(_oss_command_contract_failures(resolved, command_name, experiment))

    expected_names = (
        ("yosys.log", "nextpnr-help.log", "nextpnr.log")
        if lane == "oss"
        else ("quartus-version.log", "quartus.log")
    )
    expected_paths = tuple(f"build/{lane}/{experiment}/{name}" for name in expected_names)
    expected_prefix = f"build/{lane}/{experiment}/"
    unexpected = sorted(
        path
        for path in by_path
        if path.startswith(expected_prefix)
        and path not in expected_paths
        and path != f"{expected_prefix}summary.log"
    )
    if unexpected:
        failures.append(
            f"{lane} static-proof command logs contain unexpected lane records: "
            + ", ".join(unexpected)
        )
    if not all(path in by_path for path in expected_paths):
        missing = ", ".join(path for path in expected_paths if path not in by_path)
        failures.append(f"{lane} canonical static-proof command logs are incomplete: {missing}")
    if all(path in by_path for path in expected_paths):
        selected_paths = expected_paths
    else:
        selected_paths = ()
    if not selected_paths:
        failures.append(f"{lane} static proof has no bound command logs")
    selected: list[dict[str, str]] = []
    for path in selected_paths:
        digest = by_path[path].get("sha256")
        if isinstance(digest, str) and SHA256_RE.fullmatch(digest):
            selected.append({"path": path, "sha256": digest})
    return selected, failures


def _exclusion_failures(
    record: Mapping[str, Any],
    lane: str,
    name: str,
    experiment: str,
    manifest_sources: Mapping[str, str],
    manifest_commands: Sequence[Mapping[str, str]] = (),
) -> list[str]:
    contract = STATIC_EXCLUSION_CONTRACTS.get(name)
    if contract is None:
        return [f"{lane} {name} has no canonical static-exclusion contract"]
    exclusion = record.get("exclusion")
    if not isinstance(exclusion, dict):
        return [f"{lane} {name} static-exclusion evidence is missing"]
    failures: list[str] = []
    if "used" not in record or record.get("used") is not None:
        failures.append(f"{lane} {name} static exclusion must use null used, not a fitted count")
    if "available" not in record or record.get("available") is not None:
        failures.append(f"{lane} {name} static exclusion must not claim fitted capacity")
    if record.get("status") != "excluded":
        failures.append(f"{lane} {name} static exclusion status must be excluded")
    if record.get("evidence_kind") != "static_exclusion" or record.get("measured") is not False:
        failures.append(f"{lane} {name} static exclusion evidence kind/completeness is invalid")
    expected_basis = (
        MAILBOX_STATIC_PROOF_BASIS
        if experiment == "020_linux_mailbox"
        else contract["basis"]
    )
    if exclusion.get("basis") != expected_basis:
        failures.append(f"{lane} {name} static-exclusion basis is invalid")
    patterns = exclusion.get("patterns")
    expected_patterns = list(contract["patterns"])
    if patterns != expected_patterns:
        failures.append(f"{lane} {name} static-exclusion patterns are not canonical")

    sources = exclusion.get("sources")
    expected_paths = _canonical_static_paths_for_lane(experiment, lane)
    if not isinstance(sources, list):
        failures.append(f"{lane} {name} static-exclusion source path set is missing or malformed")
        return failures
    if len(sources) != len(expected_paths):
        failures.append(f"{lane} {name} static-exclusion source path set has unexpected size")
    expected_source_records = [
        {"path": path, "sha256": manifest_sources.get(path)}
        for path in expected_paths
    ]
    if all(isinstance(item["sha256"], str) for item in expected_source_records):
        if sources != expected_source_records:
            failures.append(f"{lane} {name} static-exclusion source records are not canonical")
    seen: set[str] = set()
    for source in sources:
        if not isinstance(source, dict) or set(source) != {"path", "sha256"}:
            failures.append(f"{lane} {name} static-exclusion source record is malformed")
            continue
        path = source.get("path")
        digest = source.get("sha256")
        if not isinstance(path, str) or not path:
            failures.append(f"{lane} {name} static-exclusion source path is malformed")
            continue
        if path in seen:
            failures.append(f"{lane} {name} static-exclusion source path is duplicated: {path}")
        seen.add(path)
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"{lane} {name} static-exclusion source hash is invalid: {path}")
            continue
        expected_digest = manifest_sources.get(path)
        if path not in expected_paths:
            failures.append(f"{lane} {name} static-exclusion source path is not canonical: {path}")
        elif not isinstance(expected_digest, str):
            failures.append(f"{lane} {name} static-exclusion source hash has no manifest source: {path}")
        elif digest != expected_digest:
            failures.append(f"{lane} {name} static-exclusion source hash disagrees with manifest source: {path}")
    missing_paths = sorted(set(expected_paths) - seen)
    if missing_paths:
        failures.append(f"{lane} {name} static-exclusion source path is missing: {', '.join(missing_paths)}")

    if experiment == "020_linux_mailbox":
        commands = exclusion.get("commands")
        expected_commands = [dict(item) for item in manifest_commands]
        if not isinstance(commands, list):
            failures.append(f"{lane} {name} static-exclusion command hash set is missing or malformed")
        elif commands != expected_commands:
            failures.append(f"{lane} {name} static-exclusion command hashes are not canonical")
    return failures


def _resource_evidence_provenance_failures(
    manifest: Mapping[str, Any],
    build: Mapping[str, Any],
    source_hashes: Mapping[str, str],
    command_records: Sequence[Mapping[str, str]],
    report: Mapping[str, str],
    lane: str,
    experiment: str,
    repo_root: Path,
    manifest_path: Path,
) -> list[str]:
    """Validate the report/source/command origin of mailbox semantic fields."""

    if experiment != "020_linux_mailbox":
        return []
    failures: list[str] = []
    failures.extend(
        _oss_authenticated_tool_failures(
            build,
            command_records,
            manifest_path=manifest_path,
            repo_root=repo_root,
            experiment=experiment,
            lane=lane,
            manifest=manifest,
        )
    )
    protocol_path = f"experiments/{experiment}/rtl/top.v"
    protocol_hash = source_hashes.get(protocol_path)
    protocol_source = (
        {"path": protocol_path, "sha256": protocol_hash}
        if isinstance(protocol_hash, str)
        else None
    )
    expected_sources = []
    for path in _canonical_static_paths_for_lane(experiment, lane):
        digest = source_hashes.get(path)
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            failures.append(f"{lane} static proof source hash is missing: {path}")
        else:
            expected_sources.append({"path": path, "sha256": digest})

    measured_labels = {
        "hps_general_purpose_interfaces": MAILBOX_ALLOWED_HARD_BLOCK,
        "pll_blocks": "PLL",
        "dsp_blocks": "DSP",
        "block_memory_bits": "block_memory_bits",
        "lutram_bits": "lutram_bits",
        "sdram_interfaces": "sdram_interfaces",
    }
    observed_resources: dict[str, Any] = {}
    for key in ("resources", "hard_blocks"):
        values = build.get(key)
        if isinstance(values, dict):
            observed_resources.update(values)
    measured_fields = {
        field
        for field, label in measured_labels.items()
        if label in observed_resources
    }
    static_fields = set(measured_labels) - measured_fields
    for field in sorted(static_fields):
        failures.extend(
            _oss_static_source_scan_failures(
                source_hashes,
                repo_root,
                field,
                experiment,
                lane,
            )
        )
    expected_report = dict(report)
    expected_fields: dict[str, Any] = {
        field: {
            "kind": "source_port_declaration",
            "source": protocol_source,
        }
        for field in (
            "clock_inputs",
            "external_input_ports",
            "external_output_ports",
            "bidirectional_ports",
        )
    }
    # A measured report row is the only acceptable provenance for an observed
    # fitted count.  A static exclusion is the explicit fallback for the
    # classes for which the selected lane has no aggregate fitted row.
    for field, label in measured_labels.items():
        expected_fields[field] = {
            "kind": "static_exclusion" if field in static_fields else "measured_report_row",
            "basis": MAILBOX_STATIC_PROOF_BASIS,
            "patterns": _static_patterns_for_field(field, lane),
            "sources": expected_sources,
            "commands": [dict(item) for item in command_records],
            "report": expected_report,
        } if field in static_fields else {
            "kind": "measured_report_row",
            "label": label,
            "report": expected_report,
        }

    expected_provenance = {"report": expected_report, "fields": expected_fields}
    observed: dict[str, Any] | None = None
    for label, record in (("manifest", manifest), ("build", build)):
        raw = record.get("resource_evidence_provenance")
        if not isinstance(raw, dict) or set(raw) != {"report", "fields"}:
            failures.append(f"{lane} {label} resource_evidence_provenance is missing or malformed")
            continue
        if raw != expected_provenance:
            failures.append(f"{lane} {label} resource_evidence_provenance is not canonical")
        if observed is None:
            observed = raw
        elif raw != observed:
            failures.append(f"{lane} manifest/build resource evidence provenance disagrees")

        fields = raw.get("fields")
        if not isinstance(fields, dict) or tuple(fields) != SEMANTIC_RESOURCE_FIELDS:
            failures.append(f"{lane} {label} resource evidence provenance fields are not canonical")
    if protocol_source is None:
        failures.append(f"{lane} port evidence source hash is missing: {protocol_path}")
    return failures


def _quartus_provenance_failures(build: Mapping[str, Any]) -> list[str]:
    """Validate the oracle's authenticated Quartus executable record.

    The oracle is optional, but a manifest claiming to be its output must
    carry enough provenance to identify exactly which compiler produced the
    reports.  Keep this check independent of the host: the recorded path may
    not exist on the machine performing comparison, while the executable and
    version-output digests still authenticate the evidence that was collected.
    """

    failures: list[str] = []
    authenticated = build.get("authenticated_tools")
    if not isinstance(authenticated, dict):
        return ["oracle Quartus provenance is missing authenticated_tools"]
    if set(authenticated) != {"quartus_sh"}:
        return ["oracle Quartus provenance authenticated_tools must contain only quartus_sh"]
    executable_record = authenticated.get("quartus_sh")
    if not isinstance(executable_record, dict):
        failures.append("oracle Quartus provenance is missing authenticated quartus_sh record")

    pins = build.get("tool_pins")
    if not isinstance(pins, dict):
        failures.append("oracle Quartus provenance is missing tool_pins")
    elif set(pins) != {"quartus"}:
        failures.append("oracle Quartus provenance tool_pins must contain only quartus")
    pin_record = pins.get("quartus") if isinstance(pins, dict) else None
    if not isinstance(pin_record, dict):
        failures.append("oracle Quartus provenance is missing quartus tool pin/version record")

    if not isinstance(executable_record, dict) or not isinstance(pin_record, dict):
        return failures

    required_string_fields = ("path", "executable", "version", "required_version")
    required_digest_fields = ("sha256", "executable_sha256", "version_output_sha256")
    for label, record in (("authenticated quartus_sh", executable_record), ("quartus tool pin", pin_record)):
        for field in required_string_fields:
            value = record.get(field)
            if not isinstance(value, str) or not value:
                failures.append(f"oracle Quartus provenance {label} has missing or malformed {field}")
        for field in required_digest_fields:
            value = record.get(field)
            if not isinstance(value, str) or SHA256_RE.fullmatch(value) is None:
                failures.append(f"oracle Quartus provenance {label} has invalid {field}")

        path = record.get("path")
        executable = record.get("executable")
        if isinstance(path, str) and not Path(path).is_absolute():
            failures.append(f"oracle Quartus provenance {label} path is not absolute")
        if isinstance(path, str) and any(character.isspace() for character in path):
            failures.append(f"oracle Quartus provenance {label} path contains whitespace")
        if isinstance(path, str) and Path(path).name != "quartus_sh":
            failures.append(f"oracle Quartus provenance {label} path is not quartus_sh")
        if isinstance(path, str) and isinstance(executable, str) and path != executable:
            failures.append(f"oracle Quartus provenance {label} path/executable disagree")
        if (
            isinstance(record.get("sha256"), str)
            and isinstance(record.get("executable_sha256"), str)
            and record["sha256"] != record["executable_sha256"]
        ):
            failures.append(f"oracle Quartus provenance {label} executable digests disagree")
        if record.get("required_version") != "17.0.2":
            failures.append(f"oracle Quartus provenance {label} required version is not exact 17.0.2")
        version = record.get("version")
        if not isinstance(version, str) or QUARTUS_VERSION_RE.search(version) is None:
            failures.append(f"oracle Quartus provenance {label} is not exact Quartus 17.0.2")

    # The two records are deliberately redundant: one authenticates the
    # executable used by the wrapper, and one pins the tool in the build
    # summary.  Requiring them to agree prevents a stale pin from being paired
    # with a newer compiler record.
    for field in ("path", "executable", "sha256", "executable_sha256", "version", "required_version", "version_output_sha256"):
        if executable_record.get(field) != pin_record.get(field):
            failures.append(f"oracle Quartus provenance authenticated/pin {field} disagrees")
    return failures


def _hard_block_view(
    build: Mapping[str, Any],
    lane: str,
    experiment: str = "",
    manifest_sources: Mapping[str, str] | None = None,
    manifest_commands: Sequence[Mapping[str, str]] = (),
) -> tuple[dict[str, dict[str, Any]], list[str]]:
    if lane == "oracle":
        raw = build.get("hard_block_evidence")
        legacy = build.get("hard_blocks")
        if not isinstance(raw, dict) or not raw:
            return {}, ["oracle hard-block evidence map is missing or empty"]
        if not isinstance(legacy, dict) or not legacy:
            return {}, ["oracle hard-block summary map is missing or empty"]
    else:
        raw = build.get("hard_blocks")
        legacy = None
        if not isinstance(raw, dict) or not raw:
            return {}, [f"{lane} hard-block evidence map is missing or empty"]

    view: dict[str, dict[str, Any]] = {}
    failures: list[str] = []
    for name, record in raw.items():
        if not isinstance(name, str) or not name or not isinstance(record, dict):
            failures.append(f"{lane} hard-block evidence record is malformed")
            continue
        evidence_kind = record.get("evidence_kind")
        is_static = lane == "oracle" and name in STATIC_HARD_BLOCKS and evidence_kind == "static_exclusion"
        if is_static:
            used = record.get("used")
            available = record.get("available")
            if used is not None or available is not None:
                failures.append(f"{lane} hard-block static evidence is malformed: {name}")
        else:
            used = _number(record.get("used"))
            available = _number(record.get("available"))
            if used is None or used < 0:
                failures.append(f"{lane} hard-block evidence is malformed: {name}")
                continue
            if available is None:
                failures.append(f"{lane} hard-block evidence is malformed: {name}")
                continue
            if available < 0:
                failures.append(f"{lane} hard-block evidence is malformed: {name}")
                continue
        view[name] = dict(record)
        if isinstance(used, (int, float)) and used > 0:
            failures.append(f"{lane} unexpected hard block in use: {name}={record['used']}")

        if lane == "oracle":
            if name in MEASURED_HARD_BLOCKS:
                if evidence_kind != "fitter_summary" or record.get("measured") is not True or available is None:
                    failures.append(f"oracle {name} evidence kind/completeness is invalid for measured fitter evidence")
            elif name in STATIC_HARD_BLOCKS:
                failures.extend(
                    _exclusion_failures(
                        record,
                        lane,
                        name,
                        experiment,
                        manifest_sources or {},
                        manifest_commands,
                    )
                )
            else:
                failures.append(f"oracle hard-block evidence has unrecognized class: {name}")

    if lane == "oracle":
        expected = set(REQUIRED_HARD_BLOCKS)
        unknown_keys = sorted(set(view) - expected)
        if unknown_keys:
            failures.append(f"oracle hard-block evidence has unrecognized classes: {', '.join(unknown_keys)}")
        for name in REQUIRED_HARD_BLOCKS:
            record = view.get(name)
            if record is None:
                failures.append(f"oracle hard-block evidence is missing required class: {name}")
                continue
            if name in STATIC_HARD_BLOCKS:
                if record.get("used") is not None:
                    failures.append(f"oracle static exclusion is not null: {name}={record.get('used')}")
            elif _number(record.get("used")) != 0:
                failures.append(f"oracle required hard block is nonzero: {name}={record.get('used')}")
            legacy_record = legacy.get(name) if isinstance(legacy, dict) else None
            if not isinstance(legacy_record, dict):
                failures.append(f"oracle hard-block summary is missing required class: {name}")
            elif legacy_record != record:
                failures.append(f"oracle hard-block summary disagrees with evidence: {name}")
        if isinstance(legacy, dict):
            legacy_unknown = sorted(set(legacy) - expected)
            if legacy_unknown:
                failures.append(f"oracle hard-block summary has unrecognized classes: {', '.join(legacy_unknown)}")
    return view, failures


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

    manifest_experiment = manifest.get("experiment") if isinstance(manifest.get("experiment"), str) else ""
    source_hashes, source_failures = _source_records(manifest)
    failures.extend(f"{lane} {failure}" for failure in source_failures)
    command_records: list[dict[str, str]] = []
    if manifest_experiment == "020_linux_mailbox":
        expected_clock = "protocol.FPGA_CLK1_50" if lane == "oss" else "FPGA_CLK1_50"
        clock_name = timing_view.get("clock")
        if clock_name != expected_clock:
            failures.append(f"{lane} timing clock must be exactly {expected_clock}")
        command_records, command_failures = _canonical_command_records(
            manifest,
            manifest_path,
            repo_root,
            lane,
            manifest_experiment,
        )
        failures.extend(command_failures)

    hard_status = build.get("hard_block_status")
    if manifest_experiment == "020_linux_mailbox":
        _policy, policy_failures = _mailbox_policy_failures(
            manifest, build, source_hashes, lane
        )
        failures.extend(policy_failures)
        semantic, semantic_failures = _semantic_resource_failures(manifest, build, lane)
        failures.extend(semantic_failures)
        hard_blocks, hard_failures = _mailbox_hard_block_view(
            build,
            lane,
            manifest_experiment,
            source_hashes,
            command_records,
        )
    else:
        semantic = {}
        hard_blocks, hard_failures = _hard_block_view(
            build,
            lane,
            manifest_experiment,
            source_hashes,
        )
    failures.extend(hard_failures)
    if lane == "oracle":
        failures.extend(_quartus_provenance_failures(build))
    unknown = build.get("unknown_resources")
    if hard_status != "pass":
        failures.append(f"{lane} has unexpected hard blocks or unknown resources")
    if not isinstance(unknown, dict):
        failures.append(f"{lane} unknown-resource evidence map is missing or malformed")
    elif unknown:
        failures.append(f"{lane} has unknown resources: {', '.join(sorted(str(item) for item in unknown))}")

    simulation = _simulation_view(manifest, build)
    simulation_status = str(simulation.get("status", "not-recorded")).lower()
    if simulation_status in {"fail", "failed", "failure", "error", "not-pass"}:
        failures.append(f"{lane} simulation failed")

    artifacts, artifact_failures = _artifact_records(
        manifest, manifest_path, repo_root, lane, manifest_experiment
    )
    failures.extend(f"{failure}" for failure in artifact_failures)
    report_binding: dict[str, str] = {}
    if manifest_experiment == "020_linux_mailbox":
        report_binding, report_failures = _report_binding_failures(
            manifest,
            build,
            artifacts,
            lane,
            manifest_experiment,
        )
        failures.extend(report_failures)
        failures.extend(
            _resource_evidence_provenance_failures(
                manifest,
                build,
                source_hashes,
                command_records,
                report_binding,
                lane,
                manifest_experiment,
                repo_root,
                manifest_path,
            )
        )
    if isinstance(raw_build, dict) and manifest_experiment:
        failures.extend(
            _summary_source_records(raw_build, lane, manifest_experiment, source_hashes)
        )

    return {
        "lane": lane,
        "status": "fail" if failures else "pass",
        "manifest": str(manifest_path),
        "target": manifest.get("target"),
        "build_status": status,
        "route_status": route_status,
        "timing": timing_view,
        "resources": _resource_view(build),
        "hard_blocks": hard_blocks,
        "hard_block_status": hard_status,
        "unknown_resources": unknown if isinstance(unknown, dict) else {},
        "simulation": simulation,
        "source_hashes": source_hashes,
        "resource_evidence": semantic,
        "experiment_policy": build.get("experiment_policy", manifest.get("experiment_policy")),
        "experiment_policy_sha256": build.get(
            "experiment_policy_sha256", manifest.get("experiment_policy_sha256")
        ),
        "protocol_source_sha256": build.get(
            "protocol_source_sha256", manifest.get("protocol_source_sha256")
        ),
        "clock_intent": build.get("clock_intent"),
        "synthesis_report": report_binding,
        "synthesis_report_path": report_binding.get("path"),
        "synthesis_report_sha256": report_binding.get("sha256"),
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


def _hard_block_display(lane: Mapping[str, Any], name: str) -> Any:
    value = lane.get("hard_blocks", {}).get(name) if isinstance(lane.get("hard_blocks"), dict) else None
    if (
        isinstance(value, dict)
        and value.get("evidence_kind") == "static_exclusion"
        and value.get("status") == "excluded"
        and value.get("measured") is False
    ):
        return "excluded (static)"
    return value.get("used") if isinstance(value, dict) and "used" in value else "absent"


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
        common_sources = (
            f"experiments/{expected_experiment}/rtl/top.v",
            "boards/de10nano/pins.qsf",
            "boards/de10nano/clocks.sdc",
        )
        for source_path in common_sources:
            oss_digest = oss.get("source_hashes", {}).get(source_path)
            oracle_digest = oracle.get("source_hashes", {}).get(source_path)
            if not isinstance(oss_digest, str):
                failures.append(f"OSS common source hash is missing: {source_path}")
            if not isinstance(oracle_digest, str):
                failures.append(f"Quartus oracle common source hash is missing: {source_path}")
            if isinstance(oss_digest, str) and isinstance(oracle_digest, str) and oss_digest != oracle_digest:
                failures.append(f"common source hash differs: {source_path}")
    if oss_manifest.get("target") != oracle_manifest.get("target"):
        failures.append("lane targets differ")

    if expected_experiment == "020_linux_mailbox":
        if oss.get("experiment_policy_sha256") != oracle.get("experiment_policy_sha256"):
            failures.append("experiment policy hashes differ")
        if oss.get("protocol_source_sha256") != oracle.get("protocol_source_sha256"):
            failures.append("protocol source hashes differ")
        if oss.get("experiment_policy") != oracle.get("experiment_policy"):
            failures.append("experiment policy descriptors differ")
        oss_allowed = (
            oss.get("experiment_policy", {}).get("allowed_hard_blocks")
            if isinstance(oss.get("experiment_policy"), dict)
            else None
        )
        oracle_allowed = (
            oracle.get("experiment_policy", {}).get("allowed_hard_blocks")
            if isinstance(oracle.get("experiment_policy"), dict)
            else None
        )
        if oss_allowed != oracle_allowed:
            failures.append("allowed hard-block policies differ")
        if oss.get("clock_intent") != oracle.get("clock_intent"):
            failures.append("clock intents differ")

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
                "both lanes contain complete explicit required hard-block evidence",
                "mailbox policy, protocol-source hash, clock intent, and semantic resource evidence match",
                "oracle static exclusions use the canonical source path set, patterns, and manifest-matching hashes",
                "oracle Quartus provenance authenticates the exact 17.0.2 executable and matching tool pin",
                "failed simulations fail the comparison",
                "common RTL, pin-QSF, and clock-SDC SHA-256 values match exactly",
                "each lane has the exact nonempty lane RBF with matching hash and size evidence",
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
            f"| {cell(name)} | {cell(_hard_block_display(oss, name))} | {cell(_hard_block_display(oracle, name))} |"
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
    raw_argv = list(sys.argv[1:] if argv is None else argv)
    experiment_options = [
        token
        for token in raw_argv
        if token == "--experiment" or token.startswith("--experiment=")
    ]
    if len(experiment_options) > 1:
        parser.error("--experiment may be specified only once")
    arguments = parser.parse_args(raw_argv)
    if arguments.manifests:
        parser.error("positional manifest form is not supported; use explicit options")
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
