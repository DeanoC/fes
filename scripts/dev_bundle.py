#!/usr/bin/env python3
"""Build deterministic unsigned development artifact bundles.

The producer consumes one already-authenticated lane manifest, reopens its
declared report and RBF, checks the complete mailbox provenance tuple against
the current clean repository, and atomically publishes four private files.
Only the standard library and the repository's closed experiment policy are
used.
"""

import argparse
import ctypes
import hashlib
import json
import math
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Mapping

try:  # Imports work both as ``scripts.dev_bundle`` and as a CLI script.
    from .experiment_policy import PolicyError, policy_for
except ImportError:  # pragma: no cover - exercised by the script entry point.
    from experiment_policy import PolicyError, policy_for


REPO_ROOT = Path(__file__).resolve().parents[1]
TARGET_DEVICE = "5CSEBA6U23I7"
BUNDLE_MEMBER_NAMES = ("manifest.json", "resource_evidence.json", "top.rbf")
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
SYNTHESIS_REPORT_NAMES = {"oss": "timing.json", "oracle": "top.fit.rpt"}
ORACLE_SOURCE_PATHS = (
    "experiments/020_linux_mailbox/oracle/top.qpf",
    "experiments/020_linux_mailbox/oracle/top.qsf",
)


RESOURCE_EVIDENCE_KEYS = (
    "schema",
    "experiment",
    "board",
    "build_lane",
    "source_commit",
    "artifact_sha256",
    "synthesis_report_sha256",
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
ARTIFACT_MANIFEST_KEYS = (
    "schema",
    "run_id",
    "experiment",
    "board",
    "build_lane",
    "artifact_filename",
    "artifact_size",
    "artifact_sha256",
    "source_commit",
)
SUPPORTED_EXPERIMENT = "020_linux_mailbox"
SUPPORTED_BOARD = "misterpi"
SUPPORTED_LANES = frozenset({"oss", "oracle"})
MAX_RESOURCE_EVIDENCE_BYTES = 2048
MIN_ARTIFACT_SIZE = 1
MAX_ARTIFACT_SIZE = 16 * 1024 * 1024
MAX_RESOURCE_COUNT = 2**32 - 1
_COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
_SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
_RUN_ID_RE = re.compile(r"^[0-9a-f]{32}$")
_AT_FDCWD = -100
_RENAME_EXCHANGE = 0x2
_RENAMEAT2_SYSCALLS = {
    "aarch64": 276,
    "amd64": 316,
    "arm": 382,
    "arm64": 276,
    "armv6l": 382,
    "armv7l": 382,
    "i386": 353,
    "i486": 353,
    "i586": 353,
    "i686": 353,
    "ppc64": 357,
    "ppc64le": 357,
    "s390x": 347,
    "x86": 353,
    "x86_64": 316,
}


class BundleEncodingError(ValueError):
    """Raised when an unsigned development record is not canonical."""


@dataclass(frozen=True)
class ResourceEvidenceV2:
    schema: int
    experiment: str
    board: str
    build_lane: str
    source_commit: str
    artifact_sha256: str
    synthesis_report_sha256: str
    clock_inputs: int
    external_input_ports: int
    external_output_ports: int
    bidirectional_ports: int
    hps_general_purpose_interfaces: int
    pll_blocks: int
    dsp_blocks: int
    block_memory_bits: int
    lutram_bits: int
    sdram_interfaces: int


@dataclass(frozen=True)
class ArtifactManifestV1:
    schema: int
    run_id: str
    experiment: str
    board: str
    build_lane: str
    artifact_filename: str
    artifact_size: int
    artifact_sha256: str
    source_commit: str


def _require_exact_string(value: object, field: str) -> str:
    if type(value) is not str:
        raise BundleEncodingError(f"{field} must be a string")
    return value


def _require_exact_integer(value: object, field: str) -> int:
    # ``bool`` is an ``int`` subclass, so an isinstance check would encode
    # true/false as JSON numbers and violate the evidence grammar.
    if type(value) is not int:
        raise BundleEncodingError(f"{field} must be a JSON integer")
    if not 0 <= value <= MAX_RESOURCE_COUNT:
        raise BundleEncodingError(f"{field} must be in 0..{MAX_RESOURCE_COUNT}")
    return value


def _require_schema(value: object, expected: int) -> int:
    if type(value) is not int or value != expected:
        raise BundleEncodingError(f"schema must be the JSON integer {expected}")
    return value


def _require_hex(value: object, field: str, pattern: re.Pattern[str]) -> str:
    text = _require_exact_string(value, field)
    if pattern.fullmatch(text) is None:
        raise BundleEncodingError(f"{field} must use its lowercase hexadecimal grammar")
    return text


def _require_lane(value: object) -> str:
    lane = _require_exact_string(value, "build_lane")
    if lane not in SUPPORTED_LANES:
        raise BundleEncodingError(f"build_lane must be one of {sorted(SUPPORTED_LANES)}")
    return lane


def _compact_json(record: dict[str, object], *, maximum_bytes: int) -> bytes:
    # The record is built by the caller in canonical insertion order.  Do not
    # sort keys: key order is part of the wire contract.
    payload = (json.dumps(record, separators=(",", ":"), ensure_ascii=True) + "\n").encode(
        "utf-8"
    )
    if len(payload) > maximum_bytes:
        raise BundleEncodingError(f"encoded payload exceeds {maximum_bytes} bytes")
    return payload


def encode_resource_evidence(value: ResourceEvidenceV2) -> bytes:
    """Encode one canonical unsigned resource-evidence schema-2 object."""

    if not isinstance(value, ResourceEvidenceV2):
        raise BundleEncodingError("resource evidence must be a ResourceEvidenceV2")

    _require_schema(value.schema, 2)
    experiment = _require_exact_string(value.experiment, "experiment")
    if experiment != SUPPORTED_EXPERIMENT:
        raise BundleEncodingError(f"experiment must be {SUPPORTED_EXPERIMENT!r}")
    board = _require_exact_string(value.board, "board")
    if board != SUPPORTED_BOARD:
        raise BundleEncodingError(f"board must be {SUPPORTED_BOARD!r}")
    lane = _require_lane(value.build_lane)
    source_commit = _require_hex(value.source_commit, "source_commit", _COMMIT_RE)
    artifact_sha256 = _require_hex(value.artifact_sha256, "artifact_sha256", _SHA256_RE)
    synthesis_report_sha256 = _require_hex(
        value.synthesis_report_sha256,
        "synthesis_report_sha256",
        _SHA256_RE,
    )

    count_fields = RESOURCE_EVIDENCE_KEYS[7:]
    counts = {
        field: _require_exact_integer(getattr(value, field), field) for field in count_fields
    }
    if counts["clock_inputs"] != 1:
        raise BundleEncodingError("clock_inputs must be exactly 1")
    if counts["hps_general_purpose_interfaces"] != 1:
        raise BundleEncodingError("hps_general_purpose_interfaces must be exactly 1")
    for field in (
        "external_input_ports",
        "external_output_ports",
        "bidirectional_ports",
        "pll_blocks",
        "dsp_blocks",
        "block_memory_bits",
        "lutram_bits",
        "sdram_interfaces",
    ):
        if counts[field] != 0:
            raise BundleEncodingError(f"{field} must be exactly 0")

    record: dict[str, object] = {
        "schema": 2,
        "experiment": experiment,
        "board": board,
        "build_lane": lane,
        "source_commit": source_commit,
        "artifact_sha256": artifact_sha256,
        "synthesis_report_sha256": synthesis_report_sha256,
    }
    record.update(counts)
    if tuple(record) != RESOURCE_EVIDENCE_KEYS:
        raise BundleEncodingError("resource evidence keys are not canonical")
    return _compact_json(record, maximum_bytes=MAX_RESOURCE_EVIDENCE_BYTES)


def encode_artifact_manifest(value: ArtifactManifestV1) -> bytes:
    """Encode one canonical FogCast artifact-manifest schema-1 object."""

    if not isinstance(value, ArtifactManifestV1):
        raise BundleEncodingError("artifact manifest must be an ArtifactManifestV1")

    _require_schema(value.schema, 1)
    run_id = _require_hex(value.run_id, "run_id", _RUN_ID_RE)
    experiment = _require_exact_string(value.experiment, "experiment")
    if experiment != SUPPORTED_EXPERIMENT:
        raise BundleEncodingError(f"experiment must be {SUPPORTED_EXPERIMENT!r}")
    board = _require_exact_string(value.board, "board")
    if board != SUPPORTED_BOARD:
        raise BundleEncodingError(f"board must be {SUPPORTED_BOARD!r}")
    lane = _require_lane(value.build_lane)
    artifact_filename = _require_exact_string(value.artifact_filename, "artifact_filename")
    if artifact_filename != "top.rbf":
        raise BundleEncodingError("artifact_filename must be 'top.rbf'")
    artifact_size = _require_exact_integer(value.artifact_size, "artifact_size")
    if not MIN_ARTIFACT_SIZE <= artifact_size <= MAX_ARTIFACT_SIZE:
        raise BundleEncodingError(
            f"artifact_size must be in {MIN_ARTIFACT_SIZE}..{MAX_ARTIFACT_SIZE}"
        )
    artifact_sha256 = _require_hex(value.artifact_sha256, "artifact_sha256", _SHA256_RE)
    source_commit = _require_hex(value.source_commit, "source_commit", _COMMIT_RE)

    record: dict[str, object] = {
        "schema": 1,
        "run_id": run_id,
        "experiment": experiment,
        "board": board,
        "build_lane": lane,
        "artifact_filename": artifact_filename,
        "artifact_size": artifact_size,
        "artifact_sha256": artifact_sha256,
        "source_commit": source_commit,
    }
    if tuple(record) != ARTIFACT_MANIFEST_KEYS:
        raise BundleEncodingError("artifact manifest keys are not canonical")
    return _compact_json(record, maximum_bytes=MAX_RESOURCE_EVIDENCE_BYTES)


class BundleError(ValueError):
    """Raised when a lane manifest cannot be bound to one bundle."""


def _fail(message: str) -> None:
    raise BundleError(message)


def _absolute(path: Path) -> Path:
    return Path(os.path.abspath(os.fspath(path)))


def _contains_symlink(path: Path) -> bool:
    """Return whether an existing path component is a symbolic link."""

    candidate = _absolute(path)
    current = Path(candidate.anchor)
    for component in candidate.parts[1:]:
        current /= component
        try:
            if current.is_symlink():
                return True
        except OSError as exc:
            _fail(f"cannot inspect path {path}: {exc}")
    return False


def _require_directory(path: Path, label: str, *, create: bool = False) -> Path:
    candidate = _absolute(path)
    if _contains_symlink(candidate):
        _fail(f"{label} path contains a symlink: {candidate}")
    if not candidate.exists():
        if not create:
            _fail(f"missing {label}: {candidate}")
        try:
            candidate.mkdir(parents=True, exist_ok=True, mode=0o700)
        except OSError as exc:
            _fail(f"cannot create {label} {candidate}: {exc}")
    if candidate.is_symlink() or not candidate.is_dir():
        _fail(f"{label} is not a regular directory: {candidate}")
    return candidate


def _require_regular(path: Path, label: str, *, root: Path | None = None) -> Path:
    candidate = _absolute(path)
    if _contains_symlink(candidate):
        _fail(f"{label} path contains a symlink: {candidate}")
    if root is not None:
        try:
            candidate.relative_to(root)
        except ValueError:
            _fail(f"{label} is outside the repository: {candidate}")
    try:
        if not candidate.is_file() or candidate.is_symlink():
            _fail(f"{label} is not a regular file: {candidate}")
    except OSError as exc:
        _fail(f"cannot inspect {label} {candidate}: {exc}")
    return candidate


def _canonical_relative(value: object, label: str) -> str:
    if type(value) is not str or not value:
        _fail(f"{label} must be a non-empty relative path")
    path = Path(value)
    if path.is_absolute() or "\\" in value or path.as_posix() != value:
        _fail(f"{label} must be a canonical relative POSIX path")
    if any(part in ("", ".", "..") for part in path.parts):
        _fail(f"{label} must not contain traversal components")
    return value


def _hash_bytes(payload: bytes) -> str:
    return hashlib.sha256(payload).hexdigest()


def _hash_file(path: Path, label: str) -> tuple[bytes, str]:
    _require_regular(path, label)
    digest = hashlib.sha256()
    chunks: list[bytes] = []
    try:
        with path.open("rb") as stream:
            while True:
                chunk = stream.read(1024 * 1024)
                if not chunk:
                    break
                chunks.append(chunk)
                digest.update(chunk)
    except OSError as exc:
        _fail(f"cannot read {label} {path}: {exc}")
    return b"".join(chunks), digest.hexdigest()


def _read_json(path: Path, label: str) -> dict[str, Any]:
    try:
        payload = path.read_bytes()
        value = json.loads(payload.decode("utf-8"), object_pairs_hook=_json_object)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, BundleError) as exc:
        _fail(f"cannot read {label} {path}: {exc}")
    if not isinstance(value, dict):
        _fail(f"{label} must contain a JSON object")
    return value


def _json_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    value: dict[str, Any] = {}
    for key, item in pairs:
        if key in value:
            _fail(f"duplicate JSON key: {key}")
        value[key] = item
    return value


def _required_map(value: object, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        _fail(f"{label} must be an object")
    return value


def _required_list(value: object, label: str) -> list[Any]:
    if not isinstance(value, list):
        _fail(f"{label} must be an array")
    return value


def _same(value: object, expected: object, label: str) -> None:
    if value != expected or type(value) is not type(expected):
        _fail(f"{label} does not match the closed bundle contract")


def _exact_json_value(value: object, expected: object, label: str) -> None:
    """Compare parsed policy JSON with exact recursive JSON types."""

    if type(expected) is dict:
        if type(value) is not dict or set(value) != set(expected):
            _fail(f"{label} does not match the closed bundle policy shape")
        assert isinstance(value, dict)
        assert isinstance(expected, dict)
        for key, expected_item in expected.items():
            _exact_json_value(value[key], expected_item, f"{label}.{key}")
        return
    if type(expected) is list:
        if type(value) is not list or len(value) != len(expected):
            _fail(f"{label} does not match the closed bundle policy shape")
        assert isinstance(value, list)
        assert isinstance(expected, list)
        for index, expected_item in enumerate(expected):
            _exact_json_value(value[index], expected_item, f"{label}[{index}]")
        return
    if type(value) is not type(expected) or value != expected:
        _fail(f"{label} does not match the closed bundle policy value")


def _validate_policy_binding(
    value: object,
    declared_hash: object,
    declared_alias_hash: object,
    *,
    expected: dict[str, Any],
    expected_hash: str,
    label: str,
) -> None:
    _exact_json_value(value, expected, label)
    assert isinstance(value, dict)
    encoded = json.dumps(value, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
    digest = _hash_bytes(encoded.encode("utf-8"))
    _same(digest, expected_hash, f"{label} canonical hash")
    _same(declared_hash, digest, f"{label} declared hash")
    _same(declared_alias_hash, digest, f"{label} alias hash")


def _valid_hash(value: object, label: str) -> str:
    if type(value) is not str or _SHA256_RE.fullmatch(value) is None:
        _fail(f"{label} must be 64 lowercase hexadecimal characters")
    return value


def _valid_commit(value: object, label: str) -> str:
    if type(value) is not str or _COMMIT_RE.fullmatch(value) is None:
        _fail(f"{label} must be 40 lowercase hexadecimal characters")
    return value


def _current_git_state(root: Path) -> str:
    try:
        top = subprocess.run(
            ["git", "-C", str(root), "rev-parse", "--show-toplevel"],
            text=True,
            capture_output=True,
            check=False,
        )
        if top.returncode != 0:
            _fail("repository is not a git worktree")
        top_path = _absolute(Path(top.stdout.strip()))
        if top_path != root:
            _fail("repository root does not match the git worktree")
        commit_result = subprocess.run(
            ["git", "-C", str(root), "rev-parse", "HEAD"],
            text=True,
            capture_output=True,
            check=False,
        )
        if commit_result.returncode != 0:
            _fail("repository HEAD is unavailable")
        commit = commit_result.stdout.strip()
        _valid_commit(commit, "repository HEAD")
        status_result = subprocess.run(
            ["git", "-C", str(root), "status", "--porcelain=v1", "--untracked-files=all"],
            text=True,
            capture_output=True,
            check=False,
        )
        if status_result.returncode != 0:
            _fail("repository status is unavailable")
        if status_result.stdout.strip():
            _fail("repository and build selection must be clean")
    except OSError as exc:
        _fail(f"cannot inspect repository git state: {exc}")
    return commit


def _policy_contract(experiment: str) -> tuple[Any, dict[str, Any], str]:
    try:
        policy = policy_for(experiment)
    except PolicyError as exc:
        _fail(f"cannot load closed experiment policy: {exc}")
    policy_dict = policy.as_dict()
    encoded = json.dumps(policy_dict, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
    return policy, policy_dict, _hash_bytes(encoded.encode("utf-8"))


def _validate_sources(
    manifest: Mapping[str, Any],
    *,
    root: Path,
    lane: str,
    experiment: str,
    policy: Any,
) -> dict[str, str]:
    records = _required_list(manifest.get("sources"), "manifest sources")
    by_path: dict[str, str] = {}
    for index, raw in enumerate(records):
        record = _required_map(raw, f"manifest source record {index}")
        if set(record) != {"path", "sha256"}:
            _fail(f"manifest source record {index} has unknown or missing fields")
        relative = _canonical_relative(record.get("path"), f"manifest source record {index} path")
        if relative in by_path:
            _fail(f"manifest has duplicate source record: {relative}")
        declared = _valid_hash(record.get("sha256"), f"manifest source hash: {relative}")
        path = _require_regular(root / relative, f"manifest source {relative}", root=root)
        _, actual = _hash_file(path, f"manifest source {relative}")
        if actual != declared:
            _fail(f"manifest source hash does not match opened file: {relative}")
        by_path[relative] = declared

    expected = [*policy.sources, *policy.constraints]
    expected_build = list(expected)
    if lane == "oracle":
        expected.extend(ORACLE_SOURCE_PATHS)
    for relative in expected:
        if relative not in by_path:
            _fail(f"manifest source hash is missing: {relative}")

    build = _required_map(manifest.get("build"), "manifest build")
    source_hashes = _required_map(build.get("source_hashes"), "build source hashes")
    for raw_path, raw_digest in source_hashes.items():
        relative = _canonical_relative(raw_path, "build source hash path")
        digest = _valid_hash(raw_digest, f"build source hash: {relative}")
        if relative not in by_path or by_path[relative] != digest:
            _fail(f"build source hash disagrees with manifest source: {relative}")
    for relative in expected_build:
        if relative not in source_hashes:
            _fail(f"build source hash is missing: {relative}")
    return by_path


def _validate_artifacts(
    manifest: Mapping[str, Any],
    *,
    root: Path,
    lane: str,
    experiment: str,
) -> tuple[Path, bytes, str, Path, bytes, str]:
    _require_directory(root / "build" / lane / experiment, "lane build directory")
    records = _required_list(manifest.get("artifacts"), "manifest artifacts")
    by_path: dict[str, str] = {}
    loaded: dict[str, tuple[Path, bytes, str]] = {}
    for index, raw in enumerate(records):
        record = _required_map(raw, f"manifest artifact record {index}")
        if set(record) != {"path", "sha256"}:
            _fail(f"manifest artifact record {index} has unknown or missing fields")
        relative = _canonical_relative(record.get("path"), f"manifest artifact record {index} path")
        if relative in by_path:
            _fail(f"manifest has duplicate artifact record: {relative}")
        if not relative.startswith(f"build/{lane}/{experiment}/"):
            _fail(f"manifest artifact is outside the selected lane: {relative}")
        declared = _valid_hash(record.get("sha256"), f"manifest artifact hash: {relative}")
        path = _require_regular(root / relative, f"manifest artifact {relative}", root=root)
        payload, actual = _hash_file(path, f"manifest artifact {relative}")
        if actual != declared:
            _fail(f"manifest artifact hash does not match opened file: {relative}")
        by_path[relative] = declared
        loaded[relative] = (path, payload, actual)

    rbf_relative = f"build/{lane}/{experiment}/top.rbf"
    report_relative = f"build/{lane}/{experiment}/{SYNTHESIS_REPORT_NAMES[lane]}"
    if rbf_relative not in loaded:
        _fail(f"manifest must bind exactly one RBF artifact at {rbf_relative}")
    if report_relative not in loaded:
        _fail(f"manifest must bind exactly one synthesis report at {report_relative}")
    rbf_path, rbf_bytes, rbf_hash = loaded[rbf_relative]
    report_path, report_bytes, report_hash = loaded[report_relative]
    if not 1 <= len(rbf_bytes) <= MAX_ARTIFACT_SIZE:
        _fail(f"RBF size must be in {MIN_ARTIFACT_SIZE}..{MAX_ARTIFACT_SIZE}")
    if not report_bytes:
        _fail("synthesis report must not be empty")
    if rbf_path.name != "top.rbf":
        _fail("artifact filename must be top.rbf")
    if report_path.name != SYNTHESIS_REPORT_NAMES[lane]:
        _fail("synthesis report filename does not match the selected lane")
    return rbf_path, rbf_bytes, rbf_hash, report_path, report_bytes, report_hash


def _validate_resource_tuple(
    manifest: Mapping[str, Any],
    *,
    source_commit: str,
    artifact_hash: str,
    report_hash: str,
    lane: str,
) -> dict[str, int]:
    expected: dict[str, int] = {
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
    build = _required_map(manifest.get("build"), "manifest build")
    for location, raw in (
        ("manifest resource evidence", manifest.get("resource_evidence")),
        ("build resource evidence", build.get("resource_evidence")),
    ):
        evidence = _required_map(raw, location)
        if tuple(evidence) != SEMANTIC_RESOURCE_FIELDS:
            _fail(f"{location} fields are not canonical")
        for field in SEMANTIC_RESOURCE_FIELDS:
            value = evidence.get(field)
            if type(value) is not int or not 0 <= value <= MAX_RESOURCE_COUNT:
                _fail(f"{location}.{field} must be a JSON integer in 0..{MAX_RESOURCE_COUNT}")
            if value != expected[field]:
                _fail(f"{location}.{field} does not match the closed policy")

    encoded = encode_resource_evidence(
        ResourceEvidenceV2(
            schema=2,
            experiment=SUPPORTED_EXPERIMENT,
            board=SUPPORTED_BOARD,
            build_lane=lane,
            source_commit=source_commit,
            artifact_sha256=artifact_hash,
            synthesis_report_sha256=report_hash,
            **expected,
        )
    )
    if len(encoded) > MAX_RESOURCE_EVIDENCE_BYTES:
        _fail("resource evidence exceeds its size bound")
    return expected


def _validate_manifest(
    manifest: Mapping[str, Any],
    *,
    root: Path,
    experiment: str,
    lane: str,
    source_commit: str,
    artifact_hash: str,
    artifact_size: int,
    report_hash: str,
    report_path: Path,
    policy: Any,
    policy_dict: dict[str, Any],
    policy_hash: str,
    source_hashes: Mapping[str, str],
) -> dict[str, int]:
    _same(manifest.get("schema"), 2, "manifest schema")
    _same(manifest.get("experiment"), experiment, "manifest experiment")
    _same(manifest.get("lane"), lane, "manifest lane")
    _same(manifest.get("target"), TARGET_DEVICE, "manifest target")
    git = _required_map(manifest.get("git"), "manifest git state")
    _same(git.get("commit"), source_commit, "manifest source commit")
    _same(git.get("dirty"), False, "manifest dirty state")
    _same(git.get("state"), "clean", "manifest git state")
    _same(git.get("changes"), [], "manifest git changes")

    _validate_policy_binding(
        manifest.get("experiment_policy"),
        manifest.get("experiment_policy_sha256"),
        manifest.get("policy_sha256"),
        expected=policy_dict,
        expected_hash=policy_hash,
        label="manifest experiment policy",
    )
    _same(manifest.get("protocol_source_sha256"), source_hashes[policy.sources[0]], "manifest protocol hash")
    _same(manifest.get("synthesis_report"), {
        "path": f"build/{lane}/{experiment}/{SYNTHESIS_REPORT_NAMES[lane]}",
        "sha256": report_hash,
    }, "manifest synthesis report")
    report_relative = f"build/{lane}/{experiment}/{SYNTHESIS_REPORT_NAMES[lane]}"
    _same(manifest.get("synthesis_report_path"), report_relative, "manifest synthesis report path")
    _same(manifest.get("synthesis_report_sha256"), report_hash, "manifest synthesis report hash")
    _same(report_path, root / report_relative, "opened synthesis report path")

    build = _required_map(manifest.get("build"), "manifest build")
    for field, expected in (
        ("experiment", experiment),
        ("lane", lane),
        ("target", TARGET_DEVICE),
        ("top", policy.top),
        ("clock_intent", policy.clock),
        ("clock_constraint_mhz", float(policy.clock_mhz)),
        ("protocol_source", policy.sources[0]),
        ("protocol_source_sha256", source_hashes[policy.sources[0]]),
        ("synthesis_report", {
            "path": report_relative,
            "sha256": report_hash,
        }),
        ("synthesis_report_path", report_relative),
        ("synthesis_report_sha256", report_hash),
    ):
        _same(build.get(field), expected, f"build {field}")

    _validate_policy_binding(
        build.get("experiment_policy"),
        build.get("experiment_policy_sha256"),
        build.get("policy_sha256"),
        expected=policy_dict,
        expected_hash=policy_hash,
        label="build experiment policy",
    )

    for field in ("status", "build_status", "route_status", "hard_block_status"):
        _same(build.get(field), "pass", f"build {field}")
    route = _required_map(build.get("route"), "build route")
    _same(route.get("status"), "pass", "build route status")
    _same(route.get("unrouted"), False, "build route unrouted")
    timing = _required_map(build.get("timing"), "build timing")
    expected_clock = "protocol.FPGA_CLK1_50" if lane == "oss" else "FPGA_CLK1_50"
    _same(timing.get("status"), "pass", "build timing status")
    _same(timing.get("clock"), expected_clock, "build timing clock")
    requested = timing.get("requested_mhz")
    achieved = timing.get("achieved_mhz")
    if type(requested) not in (int, float) or isinstance(requested, bool) or float(requested) != float(policy.clock_mhz):
        _fail("build timing requested frequency does not match the closed policy")
    if type(achieved) not in (int, float) or isinstance(achieved, bool) or not math.isfinite(float(achieved)) or float(achieved) < float(policy.clock_mhz):
        _fail("build timing achieved frequency is not passing")
    repro = _required_map(build.get("reproducibility"), "build reproducibility")
    _same(repro.get("rbf_sha256"), artifact_hash, "build RBF hash")
    _same(repro.get("rbf_size_bytes"), artifact_size, "build RBF size")
    _same(build.get("unknown_resources"), {}, "build unknown resources")
    return _validate_resource_tuple(
        manifest,
        source_commit=source_commit,
        artifact_hash=artifact_hash,
        report_hash=report_hash,
        lane=lane,
    )


def _write_private_file(path: Path, payload: bytes) -> None:
    try:
        descriptor = os.open(
            os.fspath(path),
            os.O_WRONLY | os.O_CREAT | os.O_EXCL,
            0o600,
        )
        try:
            os.fchmod(descriptor, 0o600)
            view = memoryview(payload)
            while view:
                written = os.write(descriptor, view)
                if written <= 0:
                    raise OSError("short write")
                view = view[written:]
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    except OSError as exc:
        _fail(f"cannot write private bundle member {path}: {exc}")


def _fsync_directory(path: Path, label: str) -> None:
    try:
        descriptor = os.open(os.fspath(path), os.O_RDONLY | getattr(os, "O_DIRECTORY", 0))
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    except OSError as exc:
        _fail(f"cannot fsync {label} {path}: {exc}")


def _rename_exchange(source: Path, destination: Path) -> None:
    """Atomically exchange two existing paths using Linux renameat2."""

    if sys.platform != "linux":
        _fail("atomic directory exchange is unavailable on this host")
    try:
        libc = ctypes.CDLL(None, use_errno=True)
    except OSError as exc:
        _fail(f"cannot load the Linux renameat2 interface: {exc}")

    source_bytes = os.fsencode(os.fspath(source))
    destination_bytes = os.fsencode(os.fspath(destination))
    renameat2 = getattr(libc, "renameat2", None)
    if renameat2 is not None:
        renameat2.argtypes = [
            ctypes.c_int,
            ctypes.c_char_p,
            ctypes.c_int,
            ctypes.c_char_p,
            ctypes.c_uint,
        ]
        renameat2.restype = ctypes.c_int
        result = renameat2(
            _AT_FDCWD,
            source_bytes,
            _AT_FDCWD,
            destination_bytes,
            _RENAME_EXCHANGE,
        )
    else:
        syscall = getattr(libc, "syscall", None)
        syscall_number = _RENAMEAT2_SYSCALLS.get(platform.machine().lower())
        if syscall is None or syscall_number is None:
            _fail("atomic directory exchange is unavailable on this host")
        syscall.argtypes = [
            ctypes.c_long,
            ctypes.c_int,
            ctypes.c_char_p,
            ctypes.c_int,
            ctypes.c_char_p,
            ctypes.c_uint,
        ]
        syscall.restype = ctypes.c_long
        result = syscall(
            syscall_number,
            _AT_FDCWD,
            source_bytes,
            _AT_FDCWD,
            destination_bytes,
            _RENAME_EXCHANGE,
        )
    if result != 0:
        error_number = ctypes.get_errno()
        detail = os.strerror(error_number) if error_number else "unknown error"
        _fail(f"atomic directory exchange failed: {detail}")


def _remove_tree(path: Path, label: str) -> None:
    """Remove one private staging tree, refusing links and non-directories."""

    try:
        if path.is_symlink():
            _fail(f"{label} is a symlink: {path}")
        if not path.exists():
            return
        if not path.is_dir():
            _fail(f"{label} is not a directory: {path}")
        shutil.rmtree(path)
    except OSError as exc:
        _fail(f"cannot remove {label} {path}: {exc}")


def _remove_tree_without_rmtree(path: Path, label: str) -> None:
    """Remove a private tree with primitive no-follow operations.

    This is a cleanup fallback for an injected or transient ``rmtree``
    failure.  It is deliberately restricted to the already-created staging
    paths and refuses symlinks before descending.
    """

    try:
        if path.is_symlink():
            _fail(f"{label} is a symlink: {path}")
        if not path.exists():
            return
        if not path.is_dir():
            _fail(f"{label} is not a directory: {path}")
        children = sorted(path.iterdir(), key=lambda child: child.name)
    except OSError as exc:
        _fail(f"cannot inspect {label} {path}: {exc}")
    for child in children:
        if child.is_symlink():
            _fail(f"{label} contains a symlink: {child}")
        if child.is_dir():
            _remove_tree_without_rmtree(child, label)
        else:
            try:
                child.unlink()
            except OSError as exc:
                _fail(f"cannot remove {label} member {child}: {exc}")
    try:
        path.rmdir()
    except OSError as exc:
        _fail(f"cannot remove {label} {path}: {exc}")


def _cleanup_tree(path: Path, label: str, errors: list[BaseException]) -> bool:
    """Try normal and primitive cleanup while retaining both diagnostics."""

    try:
        _remove_tree(path, label)
        return not path.exists()
    except (BundleError, OSError) as exc:
        errors.append(exc)
    try:
        _remove_tree_without_rmtree(path, label)
    except (BundleError, OSError) as exc:
        errors.append(exc)
    return not path.exists()


def _preserve_stale_tree(
    parent: Path,
    final: Path,
    tree: Path,
    errors: list[BaseException],
) -> Path | None:
    """Move an uncleared tree to a unique diagnostic sibling."""

    try:
        if tree.is_symlink():
            raise BundleError(f"stale tree is a symlink: {tree}")
        if not tree.exists():
            # A delete hook may have removed the whole tree before raising;
            # still make that successful parent-directory mutation durable.
            try:
                _fsync_directory(parent, "stale bundle parent directory")
            except (BundleError, OSError) as fsync_error:
                errors.append(fsync_error)
            return None
        stale = parent / f".{final.name}.stale-{uuid.uuid4().hex}"
        os.replace(tree, stale)
    except (BundleError, OSError) as exc:
        errors.append(BundleError(f"stale tree remains at {tree}: {exc}"))
        return tree
    errors.append(BundleError(f"preserved stale tree at {stale}"))
    try:
        _fsync_directory(parent, "stale bundle parent directory")
    except (BundleError, OSError) as exc:
        errors.append(exc)
    return stale


def _aggregate_publish_error(
    context: str,
    primary: BaseException,
    cleanup_errors: list[BaseException],
) -> None:
    details = [f"{context}: {primary}"]
    details.extend(f"rollback: {error}" for error in cleanup_errors)
    _fail("; ".join(details))


def _rollback_exchange(parent: Path, final: Path, new_tree: Path) -> list[BaseException]:
    """Restore the old final before commit and remove the new tree."""

    errors: list[BaseException] = []
    exchanged_back = False
    try:
        _rename_exchange(final, new_tree)
        exchanged_back = True
    except (BundleError, OSError) as exc:
        errors.append(exc)
    if not exchanged_back:
        # The old tree must remain untouched when the restoring exchange did
        # not complete; deleting either path could destroy the only rollback
        # copy or the still-published new bundle.
        return errors
    try:
        _fsync_directory(parent, "bundle rollback parent directory")
    except (BundleError, OSError) as exc:
        errors.append(exc)
    try:
        removed = _cleanup_tree(new_tree, "new staged bundle", errors)
        if removed:
            _fsync_directory(parent, "bundle rollback cleanup parent directory")
        else:
            _preserve_stale_tree(parent, final, new_tree, errors)
    except (BundleError, OSError) as exc:  # pragma: no cover - defensive wrapper
        errors.append(exc)
    return errors


def _private_output_parent(root: Path, lane: str, experiment: str) -> tuple[Path, Path]:
    build_root = _require_directory(root / "build", "build root", create=True)
    parent = _require_directory(build_root / "dev-bundle", "bundle root", create=True)
    lane_parent = _require_directory(parent / lane, "bundle lane directory", create=True)
    try:
        os.chmod(parent, 0o700)
        os.chmod(lane_parent, 0o700)
    except OSError as exc:
        _fail(f"cannot make bundle parent private: {exc}")
    final = lane_parent / experiment
    if _contains_symlink(final):
        _fail(f"bundle output path contains a symlink: {final}")
    if final.exists() and not final.is_dir():
        _fail(f"bundle output path is not a directory: {final}")
    return lane_parent, final


def _publish_bundle(parent: Path, final: Path, payloads: Mapping[str, bytes]) -> Path:
    temporary: Path | None = None
    try:
        temporary = Path(tempfile.mkdtemp(prefix=f".{final.name}.tmp-", dir=str(parent)))
        os.chmod(temporary, 0o700)
    except OSError as exc:
        cleanup_errors: list[BaseException] = []
        if temporary is not None:
            removed = _cleanup_tree(temporary, "bundle staging directory", cleanup_errors)
            if removed:
                try:
                    _fsync_directory(parent, "bundle staging cleanup parent directory")
                except (BundleError, OSError) as cleanup_error:
                    cleanup_errors.append(cleanup_error)
        _aggregate_publish_error(
            f"cannot create private bundle staging directory {final}",
            exc,
            cleanup_errors,
        )
    try:
        for name in (*BUNDLE_MEMBER_NAMES, "bundle.sha256"):
            _write_private_file(temporary / name, payloads[name])
        _fsync_directory(temporary, "bundle staging directory")
    except (BundleError, OSError) as exc:
        cleanup_errors: list[BaseException] = []
        if temporary is not None:
            removed = _cleanup_tree(temporary, "bundle staging directory", cleanup_errors)
            if removed:
                try:
                    _fsync_directory(parent, "bundle staging cleanup parent directory")
                except (BundleError, OSError) as cleanup_error:
                    cleanup_errors.append(cleanup_error)
        _aggregate_publish_error(f"cannot stage bundle {final}", exc, cleanup_errors)

    try:
        final_exists = final.exists() or final.is_symlink()
    except OSError as exc:
        final_exists = False
        primary = BundleError(f"cannot inspect bundle output {final}: {exc}")
        cleanup_errors: list[BaseException] = []
        if temporary is not None:
            removed = _cleanup_tree(temporary, "bundle staging directory", cleanup_errors)
            if removed:
                try:
                    _fsync_directory(parent, "bundle staging cleanup parent directory")
                except (BundleError, OSError) as cleanup_error:
                    cleanup_errors.append(cleanup_error)
        _aggregate_publish_error(f"cannot publish bundle {final}", primary, cleanup_errors)

    if final_exists:
        try:
            if _contains_symlink(final):
                _fail(f"existing bundle contains a symlink: {final}")
            for child in final.rglob("*"):
                if child.is_symlink():
                    _fail(f"existing bundle contains a symlink: {child}")
            assert temporary is not None
            _rename_exchange(temporary, final)
        except (BundleError, OSError) as exc:
            cleanup_errors: list[BaseException] = []
            if temporary is not None:
                removed = _cleanup_tree(temporary, "bundle staging directory", cleanup_errors)
                if removed:
                    try:
                        _fsync_directory(parent, "bundle staging cleanup parent directory")
                    except (BundleError, OSError) as cleanup_error:
                        cleanup_errors.append(cleanup_error)
            _aggregate_publish_error(f"cannot atomically exchange bundle {final}", exc, cleanup_errors)

        # The exchange leaves the old tree at ``temporary``.  The first
        # successful parent fsync is the publication commit point.  Before
        # that point the old tree remains intact and can be exchanged back.
        try:
            _fsync_directory(parent, "bundle parent directory")
        except (BundleError, OSError) as exc:
            cleanup_errors = _rollback_exchange(parent, final, temporary)
            _aggregate_publish_error(f"cannot publish bundle before commit {final}", exc, cleanup_errors)

        # From here onward the new final is authoritative.  Never exchange it
        # back after touching the old tree: a partial delete must remain a
        # diagnosable stale tree rather than becoming a damaged final.
        try:
            _remove_tree(temporary, "old bundle tree")
        except (BundleError, OSError) as exc:
            cleanup_errors = []
            _preserve_stale_tree(parent, final, temporary, cleanup_errors)
            _aggregate_publish_error(
                f"bundle {final} remains authoritative; old-tree cleanup failed",
                exc,
                cleanup_errors,
            )
        try:
            _fsync_directory(parent, "bundle old-tree cleanup parent directory")
        except (BundleError, OSError) as exc:
            _aggregate_publish_error(
                f"bundle {final} remains authoritative; old-tree deletion fsync failed",
                exc,
                [],
            )
        return final

    # First publication has no previous tree to exchange.  A normal rename is
    # already atomic; if its durability fsync fails, remove the new tree and
    # fsync the parent again before reporting failure.
    assert temporary is not None
    try:
        os.replace(temporary, final)
        temporary = None
    except (BundleError, OSError) as exc:
        cleanup_errors: list[BaseException] = []
        if temporary is not None:
            removed = _cleanup_tree(temporary, "bundle staging directory", cleanup_errors)
            if not removed:
                _preserve_stale_tree(parent, final, temporary, cleanup_errors)
        try:
            _fsync_directory(parent, "bundle rollback parent directory")
        except (BundleError, OSError) as cleanup_error:
            cleanup_errors.append(cleanup_error)
        _aggregate_publish_error(f"cannot publish bundle {final}", exc, cleanup_errors)
    try:
        _fsync_directory(parent, "bundle parent directory")
    except (BundleError, OSError) as exc:
        cleanup_errors = []
        removed = _cleanup_tree(final, "new bundle", cleanup_errors)
        if removed:
            try:
                _fsync_directory(parent, "bundle rollback cleanup parent directory")
            except (BundleError, OSError) as cleanup_error:
                cleanup_errors.append(cleanup_error)
        else:
            _preserve_stale_tree(parent, final, final, cleanup_errors)
        _aggregate_publish_error(f"cannot publish bundle {final}", exc, cleanup_errors)
    return final


def build_bundle(experiment: str, lane: str, run_id: str | None = None) -> Path:
    """Validate one clean lane and atomically publish its four bundle files."""

    if type(experiment) is not str or experiment != SUPPORTED_EXPERIMENT:
        _fail(f"experiment must be {SUPPORTED_EXPERIMENT!r}")
    if type(lane) is not str or lane not in SUPPORTED_LANES:
        _fail(f"build_lane must be one of {sorted(SUPPORTED_LANES)}")
    if run_id is None:
        selected_run_id = uuid.uuid4().hex
    else:
        selected_run_id = run_id
    if type(selected_run_id) is not str or _RUN_ID_RE.fullmatch(selected_run_id) is None:
        _fail("run_id must be 32 lowercase hexadecimal characters")

    root = _require_directory(Path(REPO_ROOT), "repository root")
    source_commit = _current_git_state(root)
    policy, policy_dict, policy_hash = _policy_contract(experiment)
    manifest_path = root / "build" / lane / experiment / "manifest.json"
    _require_regular(manifest_path, "lane manifest", root=root)
    manifest = _read_json(manifest_path, "lane manifest")
    source_hashes = _validate_sources(
        manifest,
        root=root,
        lane=lane,
        experiment=experiment,
        policy=policy,
    )
    rbf_path, rbf_bytes, artifact_hash, report_path, _report_bytes, report_hash = _validate_artifacts(
        manifest,
        root=root,
        lane=lane,
        experiment=experiment,
    )
    _validate_manifest(
        manifest,
        root=root,
        experiment=experiment,
        lane=lane,
        source_commit=source_commit,
        artifact_hash=artifact_hash,
        artifact_size=len(rbf_bytes),
        report_hash=report_hash,
        report_path=report_path,
        policy=policy,
        policy_dict=policy_dict,
        policy_hash=policy_hash,
        source_hashes=source_hashes,
    )

    artifact_payload = encode_artifact_manifest(
        ArtifactManifestV1(
            schema=1,
            run_id=selected_run_id,
            experiment=experiment,
            board=SUPPORTED_BOARD,
            build_lane=lane,
            artifact_filename="top.rbf",
            artifact_size=len(rbf_bytes),
            artifact_sha256=artifact_hash,
            source_commit=source_commit,
        )
    )
    evidence_payload = encode_resource_evidence(
        ResourceEvidenceV2(
            schema=2,
            experiment=experiment,
            board=SUPPORTED_BOARD,
            build_lane=lane,
            source_commit=source_commit,
            artifact_sha256=artifact_hash,
            synthesis_report_sha256=report_hash,
            clock_inputs=1,
            external_input_ports=0,
            external_output_ports=0,
            bidirectional_ports=0,
            hps_general_purpose_interfaces=1,
            pll_blocks=0,
            dsp_blocks=0,
            block_memory_bits=0,
            lutram_bits=0,
            sdram_interfaces=0,
        )
    )
    checksum_payloads = {
        "manifest.json": artifact_payload,
        "resource_evidence.json": evidence_payload,
        "top.rbf": rbf_bytes,
    }
    checksums = "".join(
        f"{_hash_bytes(checksum_payloads[name])}  {name}\n" for name in BUNDLE_MEMBER_NAMES
    ).encode("ascii")
    parent, final = _private_output_parent(root, lane, experiment)
    _publish_bundle(
        parent,
        final,
        {
            "manifest.json": artifact_payload,
            "resource_evidence.json": evidence_payload,
            "top.rbf": rbf_bytes,
            "bundle.sha256": checksums,
        },
    )
    return final


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--experiment", required=True)
    parser.add_argument("--lane", required=True)
    parser.add_argument("--run-id")
    return parser


def main(argv: list[str] | None = None) -> int:
    try:
        arguments = _parser().parse_args(argv)
        path = build_bundle(arguments.experiment, arguments.lane, arguments.run_id)
        print(path)
        return 0
    except (BundleError, OSError, ValueError) as exc:
        print(f"dev_bundle: {exc}", file=sys.stderr)
        return 2


__all__ = [
    "ArtifactManifestV1",
    "BundleError",
    "BundleEncodingError",
    "ResourceEvidenceV2",
    "build_bundle",
    "encode_artifact_manifest",
    "encode_resource_evidence",
]


if __name__ == "__main__":
    raise SystemExit(main())
