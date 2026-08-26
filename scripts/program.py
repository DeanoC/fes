#!/usr/bin/env python3
"""Safely preflight and load one volatile Cyclone V RBF.

The default transport targets a MiSTer-compatible ARM host.  It stages the
validated artifact in that host's volatile ``/tmp`` and sends one ``load_core``
line to the authenticated Main_MiSTer FIFO.  An external USB-Blaster/JTAG
transport is retained for a real DE10-Nano and uses openFPGALoader's SRAM-only
operation.  Neither transport has a persistent-storage mode.
"""

from __future__ import annotations

import argparse
import contextlib
import hashlib
import json
import math
import os
import re
import secrets
import shlex
import shutil
import stat
import subprocess
import sys
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Sequence


SCRIPT_ROOT = Path(__file__).resolve().parents[1]
TARGET_DEVICE = "5CSEBA6U23I7"
LOADER_BOARD = "de10nano"
EXPECTED_CABLE_VID = 0x09FB
EXPECTED_CABLE_PID = 0x6810
EXPECTED_IDCODE = 0x02D020DD
COMMAND_TIMEOUT = 20.0
REMOTE_TIMEOUT = 15.0
CONTROL_COMMAND_TIMEOUT = 5.0
CONTROL_WAIT_TIMEOUT = 1.0
CONTROL_WAIT_INTERVAL = 0.05
EXPERIMENT_RE = re.compile(r"^[0-9]{3}_[a-z0-9_]+$")
LANE_RE = re.compile(r"^[a-z][a-z0-9_-]*$")
HOST_LABEL_RE = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$")
USER_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_.-]{0,31}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
VID_PID_RE = re.compile(
    r"(?<![0-9A-Fa-f])(?:0x)?([0-9A-Fa-f]{4})\s*:\s*(?:0x)?([0-9A-Fa-f]{4})(?![0-9A-Fa-f])",
    re.IGNORECASE,
)
IDCODE_RE = re.compile(r"\bidcode\s*[:=]?\s*(0x[0-9A-Fa-f]+)", re.IGNORECASE)
TARGET_ALIAS_RE = re.compile(rf"\b{re.escape(TARGET_DEVICE)}\b", re.IGNORECASE)
TARGET_MODEL_RE = re.compile(r"\b5CSE\*A6\b", re.IGNORECASE)
REMOTE_STAGE_DIR_RE = re.compile(r"^/tmp/misteross-[0-9a-f]{32}$")
REMOTE_STAGE_FILE_RE = re.compile(r"^/tmp/misteross-[0-9a-f]{32}/artifact\.rbf$")
ARM_ARCHITECTURES = frozenset(("aarch64", "armv7l", "armv8l", "armv7", "arm"))
MAIN_PROCESS_NAMES = frozenset(("Main_MiSTer", "MiSTer"))
EXPECTED_NETWORK_BOARD = "misterpi"
EXPECTED_JTAG_BOARD = "de10nano"
EXPECTED_CABLE_INTERFACE = "usb-blasterII"
MAIN_SOURCE_COMMIT = "d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d"
TRUE_ENV_VALUES = frozenset(("1", "true", "yes", "on"))
FALSE_ENV_VALUES = frozenset(("0", "false", "no", "off"))

OSS_RESOURCE_CLASSES = {
    "MISTRAL_BUF": "ordinary",
    "MISTRAL_CLKENA": "ordinary",
    "MISTRAL_COMB": "ordinary",
    "MISTRAL_FF": "ordinary",
    "MISTRAL_IO": "ordinary",
    "MISTRAL_M10K": "forbidden",
    "cyclonev_hps_interface_mpu_general_purpose": "forbidden",
    "cyclonev_oscillator": "forbidden",
}
OSS_FORBIDDEN_HARD_BLOCKS = frozenset(
    name for name, classification in OSS_RESOURCE_CLASSES.items() if classification == "forbidden"
)
ORACLE_RESOURCE_CLASSES = {"ALM": "ordinary", "register": "ordinary", "IO": "ordinary"}
ORACLE_HARD_BLOCKS = frozenset(("PLL", "BRAM/M10K", "DSP", "MLAB/LUTRAM", "HPS"))

# These options bound a failed/disconnected operator action.  They contain no
# credentials and are applied to both SSH and SCP arrays.
SSH_OPTIONS = (
    "-T",
    "-o",
    "BatchMode=no",
    "-o",
    "ConnectTimeout=10",
    "-o",
    "ConnectionAttempts=1",
    "-o",
    "ServerAliveInterval=5",
    "-o",
    "ServerAliveCountMax=2",
    "-o",
    "StrictHostKeyChecking=yes",
)


class ProgramError(ValueError):
    """Raised when local or transport preflight cannot prove safety."""


@dataclass(frozen=True)
class ArtifactEvidence:
    repo_root: Path
    experiment: str
    lane: str
    manifest_path: Path
    artifact_path: Path
    sha256: str
    size_bytes: int
    stability_measured: bool = True
    stability_reason: str = ""


@dataclass(frozen=True)
class CableRow:
    text: str
    vendor_id: int
    product_id: int


@dataclass(frozen=True)
class SelectedCable:
    row: CableRow
    interface: str


@dataclass(frozen=True)
class RemotePreflight:
    architecture: str
    fifo_type: str
    fifo_uid: int
    fifo_gid: int
    fifo_mode: int
    fifo_inode: int
    fifo_nlink: int
    tmp_type: str
    tmp_writable: bool
    main_pid: int
    main_uid: int
    main_executable: str
    main_executable_type: str
    main_executable_uid: int
    main_executable_gid: int
    main_executable_mode: int
    main_executable_nlink: int
    main_sha256: str
    main_fifo_inode: int
    main_fifo_nlink: int


@dataclass(frozen=True)
class RemoteStageMetadata:
    directory_type: str
    directory_uid: int
    directory_gid: int
    directory_mode: int
    directory_nlink: int
    file_type: str
    file_uid: int
    file_gid: int
    file_mode: int
    file_nlink: int
    sha256: str
    path: str


def _fail(message: str) -> ProgramError:
    return ProgramError(message)


def _contains_symlink(path: Path) -> bool:
    """Return whether any existing component in *path* is a symlink."""

    absolute = Path(os.path.abspath(os.fspath(path)))
    current = Path(absolute.anchor)
    for component in absolute.parts[1:]:
        current /= component
        try:
            if current.is_symlink():
                return True
        except OSError as exc:
            raise _fail(f"cannot inspect path {path}: {exc}") from exc
    return False


def _regular_file(path: Path, label: str, *, nonempty: bool = False) -> Path:
    if _contains_symlink(path):
        raise _fail(f"{label} path contains a symlink: {path}")
    try:
        info = path.lstat()
    except FileNotFoundError as exc:
        raise _fail(f"missing {label}: {path}") from exc
    except OSError as exc:
        raise _fail(f"cannot inspect {label} {path}: {exc}") from exc
    if not stat.S_ISREG(info.st_mode):
        raise _fail(f"{label} is not a regular file: {path}")
    if nonempty and info.st_size <= 0:
        raise _fail(f"{label} is empty: {path}")
    return path


def _directory(path: Path, label: str) -> Path:
    if _contains_symlink(path):
        raise _fail(f"{label} path contains a symlink: {path}")
    try:
        info = path.lstat()
    except FileNotFoundError as exc:
        raise _fail(f"missing {label}: {path}") from exc
    except OSError as exc:
        raise _fail(f"cannot inspect {label} {path}: {exc}") from exc
    if not stat.S_ISDIR(info.st_mode):
        raise _fail(f"{label} is not a directory: {path}")
    return path


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as stream:
            for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(chunk)
    except OSError as exc:
        raise _fail(f"cannot hash artifact {path}: {exc}") from exc
    return digest.hexdigest()


def _as_mapping(value: Any, label: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise _fail(f"manifest {label} must be an object")
    return value


def _required_status(mapping: dict[str, Any], path: str) -> None:
    current: Any = mapping
    for component in path.split("."):
        if not isinstance(current, dict) or component not in current:
            raise _fail(f"manifest is missing required {path}")
        current = current[component]
    if current != "pass":
        raise _fail(f"manifest {path} is not pass: {current!r}")


def _is_number(value: Any) -> bool:
    return isinstance(value, (int, float)) and not isinstance(value, bool)


def _finite_number(value: Any, label: str) -> float:
    if not _is_number(value):
        raise _fail(f"manifest {label} must be finite numeric evidence: {value!r}")
    try:
        number = float(value)
    except (OverflowError, ValueError):
        raise _fail(f"manifest {label} must be finite numeric evidence: {value!r}")
    if not math.isfinite(number):
        raise _fail(f"manifest {label} must be finite numeric evidence: {value!r}")
    return number


def _nonnegative_integer(value: Any, label: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        raise _fail(f"manifest {label} must be a non-negative integer: {value!r}")
    return value


def _validate_resource_record(
    record: Any,
    label: str,
    *,
    allow_null: bool = False,
    allow_measurement_metadata: bool = False,
) -> None:
    if not isinstance(record, dict):
        raise _fail(f"manifest {label} resource record is malformed")
    allowed = {"used", "available", "utilization_percent"}
    if allow_measurement_metadata:
        allowed.update({"evidence_kind", "measured"})
    unexpected = set(record) - allowed
    if unexpected:
        raise _fail(f"manifest {label} contains arbitrary resource fields: {sorted(unexpected)}")
    if "used" not in record or "available" not in record:
        raise _fail(f"manifest {label} resource record must include used and available")
    used = record["used"]
    available = record["available"]
    if allow_null and used is None and available is None:
        pass
    else:
        _nonnegative_integer(used, f"{label}.used")
        _nonnegative_integer(available, f"{label}.available")
    if "utilization_percent" in record:
        percent = record["utilization_percent"]
        if percent is not None and _finite_number(percent, f"{label}.utilization_percent") < 0:
            raise _fail(f"manifest {label}.utilization_percent must be non-negative")


def _validate_static_exclusion(record: Any, label: str) -> None:
    if not isinstance(record, dict):
        raise _fail(f"manifest {label} static hard-block evidence is malformed")
    expected_fields = {"used", "available", "status", "evidence_kind", "measured", "exclusion"}
    if set(record) != expected_fields:
        raise _fail(f"manifest {label} static exclusion fields are not exact")
    if record.get("used") is not None or record.get("available") is not None:
        raise _fail(f"manifest {label} static exclusion must use null counts")
    if record.get("status") != "excluded" or record.get("evidence_kind") != "static_exclusion":
        raise _fail(f"manifest {label} static exclusion is not explicitly classified")
    if record.get("measured") is not False:
        raise _fail(f"manifest {label} static exclusion must be unmeasured")
    exclusion = record.get("exclusion")
    if not isinstance(exclusion, dict):
        raise _fail(f"manifest {label} static exclusion provenance is missing")
    if not isinstance(exclusion.get("basis"), str) or not exclusion["basis"]:
        raise _fail(f"manifest {label} static exclusion basis is missing")
    patterns = exclusion.get("patterns")
    sources = exclusion.get("sources")
    if not isinstance(patterns, list) or not patterns or any(not isinstance(value, str) or not value for value in patterns):
        raise _fail(f"manifest {label} static exclusion patterns are missing")
    if not isinstance(sources, list) or not sources:
        raise _fail(f"manifest {label} static exclusion sources are missing")
    for source in sources:
        if not isinstance(source, dict) or set(source) != {"path", "sha256"}:
            raise _fail(f"manifest {label} static exclusion source record is malformed")
        if not isinstance(source["path"], str) or not source["path"] or not SHA256_RE.fullmatch(str(source["sha256"])):
            raise _fail(f"manifest {label} static exclusion source hash is malformed")


def _validate_oracle_provenance(build: dict[str, Any]) -> None:
    authenticated = build.get("authenticated_tools")
    if not isinstance(authenticated, dict) or set(authenticated) != {"quartus_sh"}:
        raise _fail("oracle manifest must contain exactly quartus_sh provenance")
    quartus = authenticated["quartus_sh"]
    if not isinstance(quartus, dict):
        raise _fail("oracle quartus_sh provenance is malformed")
    required = {
        "path",
        "executable",
        "sha256",
        "executable_sha256",
        "version",
        "required_version",
        "version_output_sha256",
    }
    if set(quartus) != required:
        raise _fail("oracle quartus_sh provenance schema is not exact")
    if not isinstance(quartus["path"], str) or not quartus["path"] or quartus["path"] != quartus["executable"]:
        raise _fail("oracle quartus_sh executable identity is malformed")
    for key in ("sha256", "executable_sha256", "version_output_sha256"):
        if not isinstance(quartus[key], str) or SHA256_RE.fullmatch(quartus[key]) is None:
            raise _fail(f"oracle quartus_sh {key} is not a lowercase SHA-256")
    if quartus["sha256"] != quartus["executable_sha256"]:
        raise _fail("oracle quartus_sh executable hashes disagree")
    if quartus.get("required_version") != "17.0.2" or "17.0.2" not in str(quartus.get("version")):
        raise _fail("oracle quartus_sh provenance is not pinned to 17.0.2")
    pins = build.get("tool_pins")
    if not isinstance(pins, dict) or set(pins) != {"quartus"} or pins["quartus"] != quartus:
        raise _fail("oracle manifest quartus tool pin does not match authenticated provenance")


def _validate_resources(build: dict[str, Any], lane: str) -> None:
    expected_classes = OSS_RESOURCE_CLASSES if lane == "oss" else ORACLE_RESOURCE_CLASSES
    resource_classes = build.get("resource_classes")
    if not isinstance(resource_classes, dict) or resource_classes != expected_classes:
        raise _fail(f"{lane} manifest resource_classes must exactly match the lane contract")
    resources = build.get("resources")
    if not isinstance(resources, dict) or set(resources) != set(expected_classes):
        raise _fail(f"{lane} manifest resources must exactly match resource_classes")
    for name in expected_classes:
        _validate_resource_record(resources[name], f"{lane}.{name}")

    unknown = build.get("unknown_resources")
    if not isinstance(unknown, dict):
        raise _fail("manifest unknown resource evidence is missing or malformed")
    if unknown:
        raise _fail("manifest contains unknown resource utilization keys")

    hard_blocks = build.get("hard_blocks")
    if not isinstance(hard_blocks, dict):
        raise _fail(f"{lane} manifest hard_blocks evidence is missing")
    expected_hard = OSS_FORBIDDEN_HARD_BLOCKS if lane == "oss" else ORACLE_HARD_BLOCKS
    if set(hard_blocks) != set(expected_hard):
        raise _fail(f"{lane} manifest hard_blocks must represent every expected classified entry exactly")
    for name, record in hard_blocks.items():
        if lane == "oracle" and name in {"MLAB/LUTRAM", "HPS"}:
            _validate_static_exclusion(record, f"{lane}.{name}")
            continue
        _validate_resource_record(record, f"{lane}.{name}", allow_measurement_metadata=lane == "oracle")
        used = _nonnegative_integer(record["used"], f"{lane}.{name}.used")
        if used != 0:
            raise _fail(f"manifest reports forbidden hard-block use for {name}: {used}")
        if lane == "oss" and resource_classes.get(name) != "forbidden":
            raise _fail(f"manifest forbidden hard block is not classified as forbidden: {name}")
        if lane == "oracle" and (
            record.get("evidence_kind") != "fitter_summary" or record.get("measured") is not True
        ):
            raise _fail(f"oracle measured hard-block evidence is not explicit for {name}")

    if lane == "oss":
        for name in OSS_FORBIDDEN_HARD_BLOCKS:
            resource = resources[name]
            hard = hard_blocks[name]
            if hard.get("used") != resource.get("used") or hard.get("available") != resource.get("available"):
                raise _fail(f"oss hard-block evidence disagrees with resource record for {name}")
    else:
        evidence = build.get("hard_block_evidence")
        if not isinstance(evidence, dict) or set(evidence) != set(ORACLE_HARD_BLOCKS):
            raise _fail("oracle hard_block_evidence must exactly mirror hard_blocks")
        if evidence != hard_blocks:
            raise _fail("oracle hard_block_evidence disagrees with hard_blocks")


def validate_artifact(
    repo_root: Path,
    experiment: str,
    lane: str,
) -> ArtifactEvidence:
    """Validate the exact schema-2 lane manifest and canonical RBF artifact."""

    if not EXPERIMENT_RE.fullmatch(experiment):
        raise _fail(f"invalid experiment: {experiment!r}")
    if not LANE_RE.fullmatch(lane):
        raise _fail(f"invalid build lane: {lane!r}")
    if lane not in {"oss", "oracle"}:
        raise _fail(f"unsupported build lane for programming: {lane!r}")

    repository = Path(os.path.abspath(os.fspath(repo_root)))
    if _contains_symlink(repository):
        raise _fail(f"repository root contains a symlink component: {repository}")
    _directory(repository, "repository root")

    output = repository / "build" / lane / experiment
    manifest_path = output / "manifest.json"
    artifact_path = output / "top.rbf"
    _regular_file(manifest_path, "manifest")
    _regular_file(artifact_path, "canonical RBF", nonempty=True)

    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        raise _fail(f"cannot read manifest {manifest_path}: {exc}") from exc
    if not isinstance(manifest, dict):
        raise _fail("manifest must contain a JSON object")
    if manifest.get("schema") != 2:
        raise _fail(f"manifest schema must be exactly 2, got {manifest.get('schema')!r}")
    if manifest.get("experiment") != experiment:
        raise _fail(f"manifest experiment mismatch: expected {experiment}, got {manifest.get('experiment')!r}")
    if manifest.get("lane") != lane:
        raise _fail(f"manifest lane mismatch: expected {lane}, got {manifest.get('lane')!r}")
    if manifest.get("target") != TARGET_DEVICE:
        raise _fail(f"manifest target must be {TARGET_DEVICE}, got {manifest.get('target')!r}")

    canonical_rel = f"build/{lane}/{experiment}/top.rbf"
    artifacts = manifest.get("artifacts")
    if not isinstance(artifacts, list):
        raise _fail("manifest artifacts must be a list")
    matches = [
        record
        for record in artifacts
        if isinstance(record, dict) and record.get("path") == canonical_rel
    ]
    if len(matches) != 1:
        raise _fail(f"manifest must contain exactly one canonical RBF artifact record: {canonical_rel}")
    artifact_record = matches[0]
    expected_hash = artifact_record.get("sha256")
    if not isinstance(expected_hash, str) or not SHA256_RE.fullmatch(expected_hash):
        raise _fail("manifest canonical RBF hash is not a lowercase SHA-256")
    actual_hash = _hash_file(artifact_path)
    if actual_hash != expected_hash:
        raise _fail(f"canonical RBF hash mismatch: expected {expected_hash}, got {actual_hash}")
    artifact_size = artifact_path.stat().st_size
    for size_key in ("size_bytes", "size"):
        if size_key in artifact_record:
            declared_size = artifact_record[size_key]
            if not isinstance(declared_size, int) or isinstance(declared_size, bool) or declared_size != artifact_size:
                raise _fail(f"canonical RBF {size_key} does not match artifact size: {declared_size!r}")

    build = _as_mapping(manifest.get("build"), "build")
    if "target" in build and build.get("target") != TARGET_DEVICE:
        raise _fail(f"manifest build target must be {TARGET_DEVICE}, got {build.get('target')!r}")
    for status_path in (
        "status",
        "build_status",
        "route_status",
        "route.status",
        "timing.status",
        "hard_block_status",
    ):
        _required_status(build, status_path)
    route = _as_mapping(build.get("route"), "build.route")
    if route.get("unrouted") is not False:
        raise _fail(f"manifest route.unrouted must be false, got {route.get('unrouted')!r}")

    timing = _as_mapping(build.get("timing"), "build.timing")
    requested = timing.get("requested_mhz")
    achieved = timing.get("achieved_mhz")
    _finite_number(requested, "timing.requested_mhz")
    _finite_number(achieved, "timing.achieved_mhz")
    if requested != 50.0:
        raise _fail(f"manifest timing request must be exactly 50.0 MHz, got {requested!r}")
    if achieved < requested:
        raise _fail(f"manifest timing does not meet 50.0 MHz: {achieved!r}")
    clock = timing.get("clock")
    if not isinstance(clock, str) or not clock.startswith("FPGA_CLK1_50"):
        raise _fail(f"manifest timing clock is not the expected FPGA_CLK1_50 input: {clock!r}")
    _validate_resources(build, lane)
    if lane == "oracle":
        _validate_oracle_provenance(build)

    reproducibility = _as_mapping(build.get("reproducibility"), "build.reproducibility")
    if reproducibility.get("rbf_sha256") != expected_hash:
        raise _fail("manifest reproducibility RBF hash does not match canonical artifact record")
    size = reproducibility.get("rbf_size_bytes")
    if not isinstance(size, int) or isinstance(size, bool) or size <= 0:
        raise _fail("manifest reproducibility RBF size is missing or invalid")
    if size != artifact_size:
        raise _fail(f"canonical RBF size mismatch: manifest {size}, actual {artifact_size}")
    if lane == "oss":
        if reproducibility.get("rbf_stability_measured") is not True:
            raise _fail("manifest RBF stability was not measured")
        if reproducibility.get("rbf_stable") is not True:
            raise _fail("manifest RBF stability is not pass")
    else:
        if reproducibility.get("rbf_stability_measured") is not False:
            raise _fail("oracle manifest stability status must explicitly be unmeasured")
        if reproducibility.get("rbf_stable") is not None:
            raise _fail("oracle manifest cannot claim measured stability")
        reason = reproducibility.get("rbf_stability_reason")
        if not isinstance(reason, str) or not reason.strip():
            raise _fail("oracle manifest must provide an explicit unmeasured stability reason")
    previous_hash = reproducibility.get("previous_rbf_sha256")
    if previous_hash is not None:
        if not isinstance(previous_hash, str) or not SHA256_RE.fullmatch(previous_hash):
            raise _fail("manifest previous RBF hash is malformed")
        if previous_hash != expected_hash:
            raise _fail("manifest previous RBF hash does not match stable canonical artifact")

    return ArtifactEvidence(
        repo_root=repository,
        experiment=experiment,
        lane=lane,
        manifest_path=manifest_path,
        artifact_path=artifact_path,
        sha256=expected_hash,
        size_bytes=artifact_size,
        stability_measured=reproducibility.get("rbf_stability_measured") is True,
        stability_reason=str(reproducibility.get("rbf_stability_reason", "")),
    )


def _env_value(*names: str) -> str | None:
    for name in names:
        value = os.environ.get(name)
        if value is not None and value != "":
            return value
    return None


def _env_bool(name: str) -> bool:
    value = os.environ.get(name)
    # Make exports this optional variable as an empty string when no operator
    # value is supplied. Treat that representation like an unset variable and
    # keep the fail-closed default as dry-run.
    if value is None or value == "":
        return True
    if not isinstance(value, str):
        raise _fail(f"{name} must be a string boolean (unset, true, or false)")
    normalized = value.lower()
    if normalized in TRUE_ENV_VALUES:
        return True
    if normalized in FALSE_ENV_VALUES:
        return False
    raise _fail(
        f"{name} must be exactly one of {sorted(TRUE_ENV_VALUES | FALSE_ENV_VALUES)} "
        "without surrounding whitespace"
    )


def validate_host(value: str | None) -> str:
    if value is None or value == "":
        raise _fail("mister transport requires an explicit host (--host or MISTER_HOST)")
    if len(value) > 253 or value.endswith(".") or ".." in value:
        raise _fail("invalid host: expected a strict DNS hostname or IPv4 address")
    if any(char.isspace() or ord(char) < 32 or ord(char) == 127 for char in value):
        raise _fail("invalid host: whitespace/control characters are not allowed")
    labels = value.split(".")
    if any(not label or len(label) > 63 or HOST_LABEL_RE.fullmatch(label) is None for label in labels):
        raise _fail("invalid host: expected a strict DNS hostname or IPv4 address")
    return value


def validate_user(value: str | None) -> str:
    if value is None or value == "":
        raise _fail("mister transport requires an explicit user (--user or MISTER_USER)")
    if USER_RE.fullmatch(value) is None:
        raise _fail("invalid user: use only letters, digits, '_', '.', and '-'")
    return value


def _resolve_executable(value: str, label: str) -> str:
    if not value:
        raise _fail(f"{label} executable is not configured")
    if os.sep in value:
        path = Path(value)
        _regular_file(path, label)
        if not os.access(path, os.X_OK):
            raise _fail(f"{label} is not executable: {path}")
        return str(Path(os.path.abspath(os.fspath(path))))
    resolved = shutil.which(value)
    if resolved is None:
        raise _fail(f"{label} executable not found: {value}")
    path = Path(resolved)
    _regular_file(path, label)
    if not os.access(path, os.X_OK):
        raise _fail(f"{label} is not executable: {path}")
    return str(path)


def _short_output(stdout: str, stderr: str) -> str:
    text = " ".join((stdout, stderr)).strip().replace("\r", " ").replace("\n", " ")
    text = re.sub(
        r"(?i)(password|passwd|passphrase|sshp?ass)(\s*(?:[=:]|is)\s*|\s+)[^\s]+",
        r"\1\2<redacted>",
        text,
    )
    return text[:240] if text else "no diagnostic output"


def _run(command: Sequence[str], *, timeout: float = COMMAND_TIMEOUT) -> subprocess.CompletedProcess[str]:
    # Authentication remains operator-interactive through SSH's normal tty
    # prompt.  Strip password-bearing helper variables so an ambient secret
    # can never be handed to a fake/alternate executable or appear in output.
    environment = os.environ.copy()
    for name in tuple(environment):
        upper = name.upper()
        if "PASSWORD" in upper or "PASSWD" in upper or upper in {"SSHPASS", "SSH_PASS"}:
            environment.pop(name, None)
    try:
        return subprocess.run(
            list(command),
            env=environment,
            text=True,
            stdin=None,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=timeout,
        )
    except subprocess.TimeoutExpired as exc:
        raise _fail(f"command timed out after {timeout:g}s: {shlex.join(command)}") from exc
    except OSError as exc:
        raise _fail(f"could not launch command {shlex.join(command)}: {exc}") from exc


def _print_command(label: str, command: Sequence[str]) -> None:
    print(f"{label}: {shlex.join(list(command))}", flush=True)


def _print_artifact(evidence: ArtifactEvidence) -> None:
    """Render the authenticated local evidence before transport probing."""

    print(f"artifact: {evidence.artifact_path}", flush=True)
    print(f"artifact_sha256: {evidence.sha256}", flush=True)
    print(f"artifact_size_bytes: {evidence.size_bytes}", flush=True)
    print("manifest_schema: 2", flush=True)
    print(f"experiment: {evidence.experiment}", flush=True)
    print(f"lane: {evidence.lane}", flush=True)
    print(f"target: {TARGET_DEVICE}", flush=True)
    print("build_status: pass", flush=True)
    print("route_status: pass", flush=True)
    print("timing_status: pass", flush=True)
    print("hard_block_status: pass", flush=True)
    if evidence.stability_measured:
        print("rbf_stability: measured/pass", flush=True)
    else:
        print(f"rbf_stability: unmeasured ({evidence.stability_reason})", flush=True)


def parse_cable_scan(output: str, returncode: int, command: Sequence[str]) -> list[CableRow]:
    if returncode != 0:
        raise _fail(f"openFPGALoader cable discovery failed: {shlex.join(command)} -> {_short_output(output, '')}")
    rows: list[CableRow] = []
    for line in output.splitlines():
        stripped = line.strip()
        if not stripped or "vid:pid" in stripped.lower():
            continue
        match = VID_PID_RE.search(stripped)
        if match:
            vendor_id, product_id = int(match.group(1), 16), int(match.group(2), 16)
            rows.append(CableRow(stripped, vendor_id, product_id))
    return rows


def select_cable(
    rows: Sequence[CableRow],
    interface: str | None = None,
    cable_index: int | None = None,
) -> SelectedCable:
    if not rows:
        raise _fail("no DE10-Nano detected by openFPGALoader")
    if interface is None:
        interface = EXPECTED_CABLE_INTERFACE
    if interface != EXPECTED_CABLE_INTERFACE:
        raise _fail(
            f"unsupported --cable interface {interface!r}; expected {EXPECTED_CABLE_INTERFACE!r}"
        )
    if len(rows) != 1:
        raise _fail(
            f"multiple DE10-Nano/USB-Blaster devices ({len(rows)}) are unsafe; "
            "USB-Blaster II selection requires exactly one discovered device"
        )
    if cable_index is not None:
        raise _fail(
            "--cable-index is unsupported for the pinned USB-Blaster II interface; "
            "physical multi-probe selection is disabled"
        )
    row = rows[0]
    if (row.vendor_id, row.product_id) != (EXPECTED_CABLE_VID, EXPECTED_CABLE_PID):
        raise _fail(
            "unexpected USB-Blaster cable VID/PID: "
            f"0x{row.vendor_id:04x}:0x{row.product_id:04x}; expected 0x{EXPECTED_CABLE_VID:04x}:0x{EXPECTED_CABLE_PID:04x}"
        )
    return SelectedCable(row=row, interface=interface)


def parse_jtag_detect(output: str, returncode: int, command: Sequence[str]) -> str:
    if returncode != 0:
        raise _fail(f"openFPGALoader JTAG detection failed: {shlex.join(command)} -> {_short_output(output, '')}")
    idcodes = [int(token, 16) for token in IDCODE_RE.findall(output)]
    aliases = TARGET_ALIAS_RE.findall(output)
    models = TARGET_MODEL_RE.findall(output)
    if idcodes:
        count = len(idcodes)
    else:
        indexed = re.findall(r"(?im)^\s*(?:index|device|tap)\s+\d+|^\s*\d+\s*[:)]", output)
        count = len(indexed) or len(aliases) or len(models)
    if count != 1:
        raise _fail(f"JTAG detection must identify exactly one device; observed {count}")
    if aliases:
        return TARGET_DEVICE
    if models:
        return "5CSE*A6"
    if EXPECTED_IDCODE in idcodes:
        return f"0x{EXPECTED_IDCODE:08x}"
    observed = ", ".join(f"0x{value:x}" for value in idcodes) if idcodes else "unknown"
    raise _fail(f"JTAG device did not match {TARGET_DEVICE} or IDCODE 0x{EXPECTED_IDCODE:08x}; observed {observed}")


@contextlib.contextmanager
def _artifact_snapshot(evidence: ArtifactEvidence):
    """Yield a private, no-follow local snapshot and remove it after use."""

    build_root = evidence.repo_root / "build"
    _directory(build_root, "build output root")
    try:
        private_dir = Path(tempfile.mkdtemp(prefix="program-", dir=build_root))
        os.chmod(private_dir, 0o700)
    except OSError as exc:
        raise _fail(f"cannot create private programming snapshot directory: {exc}") from exc

    snapshot = private_dir / "artifact.rbf"
    source_fd = -1
    destination_fd = -1
    try:
        no_follow = getattr(os, "O_NOFOLLOW", 0)
        close_on_exec = getattr(os, "O_CLOEXEC", 0)
        try:
            source_fd = os.open(os.fspath(evidence.artifact_path), os.O_RDONLY | no_follow | close_on_exec)
        except OSError as exc:
            raise _fail(f"cannot open validated RBF without following symlinks: {exc}") from exc
        source_stat = os.fstat(source_fd)
        if not stat.S_ISREG(source_stat.st_mode) or source_stat.st_size != evidence.size_bytes:
            raise _fail("validated RBF changed before the immutable snapshot was opened")
        try:
            destination_fd = os.open(
                os.fspath(snapshot),
                os.O_WRONLY | os.O_CREAT | os.O_EXCL | no_follow | close_on_exec,
                0o600,
            )
        except OSError as exc:
            raise _fail(f"cannot create private RBF snapshot without following symlinks: {exc}") from exc

        digest = hashlib.sha256()
        copied = 0
        while True:
            block = os.read(source_fd, 1024 * 1024)
            if not block:
                break
            digest.update(block)
            view = memoryview(block)
            while view:
                written = os.write(destination_fd, view)
                if written <= 0:
                    raise _fail("private RBF snapshot write made no progress")
                view = view[written:]
            copied += len(block)
        os.fsync(destination_fd)
        os.fchmod(destination_fd, 0o400)
        destination_stat = os.fstat(destination_fd)
        if digest.hexdigest() != evidence.sha256 or copied != evidence.size_bytes:
            raise _fail("validated RBF changed while creating the immutable snapshot")
        if (
            not stat.S_ISREG(destination_stat.st_mode)
            or destination_stat.st_size != evidence.size_bytes
            or destination_stat.st_nlink != 1
            or stat.S_IMODE(destination_stat.st_mode) != 0o400
        ):
            raise _fail("private RBF snapshot metadata is unsafe")
        private_stat = private_dir.lstat()
        if (
            not stat.S_ISDIR(private_stat.st_mode)
            or stat.S_IMODE(private_stat.st_mode) != 0o700
            or private_stat.st_nlink < 2
            or snapshot.is_symlink()
        ):
            raise _fail("private RBF snapshot directory metadata is unsafe")
        yield snapshot
    except OSError as exc:
        raise _fail(f"private RBF snapshot failed: {exc}") from exc
    finally:
        for descriptor in (source_fd, destination_fd):
            if descriptor >= 0:
                try:
                    os.close(descriptor)
                except OSError:
                    pass
        shutil.rmtree(private_dir, ignore_errors=True)


def _require_board_attestation(args: argparse.Namespace, expected: str) -> None:
    value = args.expected_board
    if args.dry_run:
        if value:
            if value != expected:
                raise _fail(f"board attestation mismatch: expected {expected}, got {value!r}")
        else:
            print(f"board_attestation: not supplied (dry-run; expected {expected})", flush=True)
        return
    if value != expected:
        if not value:
            raise _fail(f"non-dry {expected} action requires explicit board attestation --expected-board {expected}")
        raise _fail(f"board attestation mismatch: expected {expected}, got {value!r}")
    print(f"board_attestation: {value}", flush=True)


def _jtag_transport(evidence: ArtifactEvidence, args: argparse.Namespace) -> int:
    _require_board_attestation(args, EXPECTED_JTAG_BOARD)
    configured = args.programmer or str(evidence.repo_root / "build/toolchain/install/bin/openFPGALoader")
    programmer = _resolve_executable(configured, "openFPGALoader")
    with _artifact_snapshot(evidence) as snapshot:
        print(f"local_snapshot: {snapshot}", flush=True)
        scan_command = [programmer, "--board", LOADER_BOARD, "--scan-usb"]
        _print_command("cable discovery", scan_command)
        scan_result = _run(scan_command)
        scan_output = "\n".join((scan_result.stdout, scan_result.stderr))
        rows = parse_cable_scan(scan_output, scan_result.returncode, scan_command)
        selected = select_cable(rows, args.cable, args.cable_index)

        selector = [
            "--board",
            LOADER_BOARD,
            "--cable",
            selected.interface,
        ]
        detect_command = [programmer, *selector, "--detect"]
        _print_command("JTAG discovery", detect_command)
        detect_result = _run(detect_command)
        detect_output = "\n".join((detect_result.stdout, detect_result.stderr))
        matched = parse_jtag_detect(detect_output, detect_result.returncode, detect_command)

        final_command = [programmer, *selector, "--write-sram", str(snapshot)]
        print(f"board: {LOADER_BOARD}")
        print(f"cable: {selected.interface}")
        print(f"JTAG target: {matched}")
        _print_command("volatile action", final_command)
        if args.dry_run:
            print("DRY RUN: no SRAM programming action invoked")
            return 0
        result = _run(final_command, timeout=REMOTE_TIMEOUT)
        if result.returncode != 0:
            raise _fail(f"programmer failed with exit {result.returncode}: {_short_output(result.stdout, result.stderr)}")
        print("volatile SRAM programming request dispatched; outcome unverified")
        return 0


def _remote_command(
    ssh: str,
    target: str,
    command: str,
    *,
    ssh_options: Sequence[str] = SSH_OPTIONS,
    timeout: float = REMOTE_TIMEOUT,
) -> subprocess.CompletedProcess[str]:
    return _run([ssh, *ssh_options, target, command], timeout=timeout)


def _decimal(value: str, label: str) -> int:
    if re.fullmatch(r"[0-9]+", value) is None:
        raise _fail(f"remote {label} is malformed: {value!r}")
    return int(value, 10)


def _octal(value: str, label: str) -> int:
    if re.fullmatch(r"[0-7]+", value) is None:
        raise _fail(f"remote {label} mode is malformed: {value!r}")
    return int(value, 8)


def _parse_remote_preflight(output: str) -> RemotePreflight:
    lines = [line.strip() for line in output.splitlines() if line.strip()]
    if not lines or lines[0] != "PREFLIGHT_V1":
        raise _fail("remote Main/FIFO preflight response is missing its authenticated schema")
    values: dict[str, list[list[str]]] = {}
    for line in lines[1:]:
        fields = line.split("|")
        if not fields or fields[0] not in {"ARCH", "FIFO", "TMP", "MAIN", "MAIN_SHA", "MAIN_FD"}:
            raise _fail(f"remote Main/FIFO preflight response contains an unknown record: {line!r}")
        values.setdefault(fields[0], []).append(fields[1:])
    if len(values.get("ARCH", [])) != 1 or len(values.get("FIFO", [])) != 1 or len(values.get("TMP", [])) != 1:
        raise _fail("remote Main/FIFO preflight response is incomplete or ambiguous")
    arch_fields = values["ARCH"][0]
    if len(arch_fields) != 1:
        raise _fail("remote architecture evidence is malformed")
    fifo = values["FIFO"][0]
    if len(fifo) != 6:
        raise _fail("remote FIFO metadata evidence is malformed")
    fifo_type = fifo[0].strip().lower()
    fifo_uid, fifo_gid = _decimal(fifo[1], "FIFO uid"), _decimal(fifo[2], "FIFO gid")
    fifo_mode = _octal(fifo[3], "FIFO")
    fifo_inode, fifo_nlink = _decimal(fifo[4], "FIFO inode"), _decimal(fifo[5], "FIFO nlink")
    tmp = values["TMP"][0]
    if len(tmp) == 2:
        tmp_type, tmp_writable = tmp[0].lower(), tmp[1] == "1"
    elif len(tmp) in {6, 7}:
        tmp_type = tmp[0].lower()
        _decimal(tmp[1], "tmp uid")
        _decimal(tmp[2], "tmp gid")
        _octal(tmp[3], "tmp")
        _decimal(tmp[4], "tmp inode")
        _decimal(tmp[5], "tmp nlink")
        tmp_writable = len(tmp) == 6 or tmp[6] == "1"
    else:
        raise _fail("remote /tmp metadata evidence is malformed")
    mains = values.get("MAIN", [])
    hashes = values.get("MAIN_SHA", [])
    fds = values.get("MAIN_FD", [])
    if len(mains) != 1 or len(hashes) != 1 or len(fds) != 1:
        raise _fail("remote Main process evidence must identify exactly one PID, digest, and FIFO descriptor")
    main = mains[0]
    if len(main) != 8:
        raise _fail("remote Main executable metadata evidence is malformed")
    main_pid = _decimal(main[0], "Main PID")
    main_uid = _decimal(main[1], "Main uid")
    executable = main[2]
    executable_type = main[3].lower()
    executable_uid = _decimal(main[4], "Main executable uid")
    executable_gid = _decimal(main[5], "Main executable gid")
    executable_mode = _octal(main[6], "Main executable")
    executable_nlink = _decimal(main[7], "Main executable nlink")
    if not executable.startswith("/") or "|" in executable or executable.endswith(" (deleted)"):
        raise _fail("remote Main executable identity is malformed")
    main_sha = hashes[0]
    if len(main_sha) != 2 or _decimal(main_sha[0], "Main digest PID") != main_pid or SHA256_RE.fullmatch(main_sha[1]) is None:
        raise _fail("remote Main executable SHA-256 evidence is malformed")
    main_fd = fds[0]
    if len(main_fd) != 3 or _decimal(main_fd[0], "Main FIFO PID") != main_pid:
        raise _fail("remote Main FIFO descriptor evidence is malformed")
    main_fifo_inode = _decimal(main_fd[1], "Main FIFO inode")
    main_fifo_nlink = _decimal(main_fd[2], "Main FIFO nlink")
    if arch_fields[0] not in ARM_ARCHITECTURES:
        raise _fail(f"remote architecture is not an expected ARM target: {arch_fields[0]!r}")
    if fifo_type not in {"fifo", "named pipe"} or fifo_uid != 0 or fifo_gid != 0 or fifo_inode <= 0 or fifo_nlink <= 0:
        raise _fail("remote /dev/MiSTer_cmd is not a root-owned FIFO with safe metadata")
    if tmp_type not in {"directory", "dir"} or not tmp_writable:
        raise _fail("remote /tmp is not a writable directory")
    if main_uid != 0 or executable_type not in {"regular file", "regular"} or executable_uid != 0 or executable_gid != 0:
        raise _fail("remote Main process/executable is not root-owned regular-file identity")
    if main_pid <= 0 or executable_nlink <= 0 or not (executable_mode & 0o111):
        raise _fail("remote Main executable metadata is unsafe")
    if main_fifo_inode != fifo_inode or main_fifo_nlink <= 0:
        raise _fail("remote Main PID does not hold the same /dev/MiSTer_cmd FIFO inode")
    return RemotePreflight(
        architecture=arch_fields[0],
        fifo_type=fifo_type,
        fifo_uid=fifo_uid,
        fifo_gid=fifo_gid,
        fifo_mode=fifo_mode,
        fifo_inode=fifo_inode,
        fifo_nlink=fifo_nlink,
        tmp_type=tmp_type,
        tmp_writable=tmp_writable,
        main_pid=main_pid,
        main_uid=main_uid,
        main_executable=executable,
        main_executable_type=executable_type,
        main_executable_uid=executable_uid,
        main_executable_gid=executable_gid,
        main_executable_mode=executable_mode,
        main_executable_nlink=executable_nlink,
        main_sha256=main_sha[1],
        main_fifo_inode=main_fifo_inode,
        main_fifo_nlink=main_fifo_nlink,
    )


def _parse_remote_directory(output: str) -> tuple[str, int, int, int, int]:
    lines = [line.strip() for line in output.splitlines() if line.strip()]
    if any(not line.startswith("DIR|") for line in lines):
        raise _fail("remote private staging directory response contains an unknown record")
    records = [line.split("|")[1:] for line in lines if line.startswith("DIR|")]
    if len(records) != 1 or len(records[0]) != 5:
        raise _fail("remote private staging directory metadata is missing or ambiguous")
    record = records[0]
    return (
        record[0].lower(),
        _decimal(record[1], "staging directory uid"),
        _decimal(record[2], "staging directory gid"),
        _octal(record[3], "staging directory"),
        _decimal(record[4], "staging directory nlink"),
    )


def _parse_remote_stage(output: str, expected_path: str) -> RemoteStageMetadata:
    lines = [line.strip() for line in output.splitlines() if line.strip()]
    if not lines or lines[0] != "VERIFY_V1":
        raise _fail("remote staged RBF verification response is missing its authenticated schema")
    if any(not line.startswith(("DIR|", "FILE|", "HASH|")) for line in lines[1:]):
        raise _fail("remote staged RBF response contains an unknown record")
    dirs = [line.split("|")[1:] for line in lines[1:] if line.startswith("DIR|")]
    files = [line.split("|")[1:] for line in lines[1:] if line.startswith("FILE|")]
    hashes = [line.split("|")[1:] for line in lines[1:] if line.startswith("HASH|")]
    if len(dirs) != 1 or len(files) != 1 or len(hashes) != 1 or len(dirs[0]) != 5 or len(files[0]) != 5 or len(hashes[0]) != 2:
        raise _fail("remote staged RBF metadata/hash response is incomplete or ambiguous")
    directory, file_record, digest = dirs[0], files[0], hashes[0]
    path = digest[1]
    if path != expected_path:
        raise _fail(f"remote SHA-256 output named an unexpected path: {path!r}")
    directory_type = directory[0].lower()
    directory_uid, directory_gid = _decimal(directory[1], "staging directory uid"), _decimal(directory[2], "staging directory gid")
    directory_mode = _octal(directory[3], "staging directory")
    directory_nlink = _decimal(directory[4], "staging directory nlink")
    file_type = file_record[0].lower()
    file_uid, file_gid = _decimal(file_record[1], "staged RBF uid"), _decimal(file_record[2], "staged RBF gid")
    file_mode = _octal(file_record[3], "staged RBF")
    file_nlink = _decimal(file_record[4], "staged RBF nlink")
    if directory_type not in {"directory", "dir"} or directory_uid != 0 or directory_gid != 0 or directory_mode != 0o700 or directory_nlink < 2:
        raise _fail("remote private staging directory metadata is unsafe")
    if file_type not in {"regular file", "regular"} or file_uid != 0 or file_gid != 0 or file_nlink != 1:
        raise _fail("remote staged RBF is not a root-owned regular file with one link")
    if file_mode != 0o400:
        raise _fail("remote staged RBF mode is not exactly private mode 0400")
    if SHA256_RE.fullmatch(digest[0]) is None:
        raise _fail("remote staged RBF SHA-256 is malformed")
    return RemoteStageMetadata(
        directory_type=directory_type,
        directory_uid=directory_uid,
        directory_gid=directory_gid,
        directory_mode=directory_mode,
        directory_nlink=directory_nlink,
        file_type=file_type,
        file_uid=file_uid,
        file_gid=file_gid,
        file_mode=file_mode,
        file_nlink=file_nlink,
        sha256=digest[0],
        path=path,
    )


def _remote_preflight_script() -> str:
    return r''': MISTEROSS_PREFLIGHT_V1
set -eu
printf 'PREFLIGHT_V1\n'
fifo=/dev/MiSTer_cmd
[ -p "$fifo" ]
fifo_stat=$(stat -Lc '%F|%u|%g|%a|%i|%h' "$fifo")
printf 'FIFO|%s\n' "$fifo_stat"
printf 'ARCH|%s\n' "$(uname -m)"
[ -d /tmp ] && [ -w /tmp ]
tmp_stat=$(stat -Lc '%F|%u|%g|%a|%i|%h' /tmp)
printf 'TMP|%s\n' "$tmp_stat"
main_count=0
for pid_dir in /proc/[0-9]*; do
    [ -r "$pid_dir/comm" ] || continue
    pid=${pid_dir#/proc/}
    comm=$(cat "$pid_dir/comm" 2>/dev/null || true)
    case "$comm" in
        Main_MiSTer|MiSTer)
            main_count=$((main_count + 1))
            uid=$(awk '$1 == "Uid:" { if ($2 != 0 || $3 != 0 || $4 != 0) exit 1; print $2; exit }' "$pid_dir/status" 2>/dev/null || true)
            exe=$(readlink "$pid_dir/exe" 2>/dev/null || true)
            [ -n "$uid" ] && [ -n "$exe" ] || exit 1
            exe_stat=$(stat -Lc '%F|%u|%g|%a|%h' "$pid_dir/exe" 2>/dev/null || true)
            exe_sha=$(sha256sum "$pid_dir/exe" 2>/dev/null | awk 'NF { print $1; exit }' || true)
            [ -n "$exe_stat" ] && [ -n "$exe_sha" ] || exit 1
            printf 'MAIN|%s|%s|%s|%s\n' "$pid" "$uid" "$exe" "$exe_stat"
            printf 'MAIN_SHA|%s|%s\n' "$pid" "$exe_sha"
            fifo_inode=$(printf '%s\n' "$fifo_stat" | awk -F'|' '{ print $5 }')
            for fd in "$pid_dir"/fd/*; do
                [ -e "$fd" ] || continue
                fd_stat=$(stat -Lc '%F|%i|%h' "$fd" 2>/dev/null || true)
                fd_type=$(printf '%s\n' "$fd_stat" | awk -F'|' '{ print $1 }')
                fd_inode=$(printf '%s\n' "$fd_stat" | awk -F'|' '{ print $2 }')
                if [ "$fd_type" = fifo ] && [ "$fd_inode" = "$fifo_inode" ]; then
                    fd_nlink=$(printf '%s\n' "$fd_stat" | awk -F'|' '{ print $3 }')
                    printf 'MAIN_FD|%s|%s|%s\n' "$pid" "$fd_inode" "$fd_nlink"
                    break
                fi
            done
            ;;
    esac
done
[ "$main_count" -eq 1 ]
'''


def _remote_mkdir_script(stage_dir: str) -> str:
    quoted = shlex.quote(stage_dir)
    return (
        f": MISTEROSS_MKDIR_V1 {quoted}; set -eu; umask 077; mkdir -m 0700 -- {quoted}; "
        f"[ ! -L {quoted} ]; stat -Lc 'DIR|%F|%u|%g|%a|%h' -- {quoted}"
    )


def _remote_verify_script(stage_dir: str, stage_file: str) -> str:
    quoted_dir = shlex.quote(stage_dir)
    quoted_file = shlex.quote(stage_file)
    return (
        f": MISTEROSS_VERIFY_V1 {shlex.quote(stage_file)}; set -eu; "
        f"[ ! -L {quoted_dir} ]; [ -d {quoted_dir} ]; "
        f"[ ! -L {quoted_file} ]; [ -f {quoted_file} ]; "
        f"chmod 0400 -- {quoted_file}; "
        f"[ ! -L {quoted_file} ]; [ -f {quoted_file} ]; "
        f"printf 'VERIFY_V1\\n'; stat -Lc 'DIR|%F|%u|%g|%a|%h' -- {quoted_dir}; "
        f"stat -Lc 'FILE|%F|%u|%g|%a|%h' -- {quoted_file}; "
        f"digest=$(sha256sum -- {quoted_file} | awk 'NF == 2 {{print $1; exit}}'); "
        f"[ -n \"$digest\" ]; printf 'HASH|%s|%s\\n' \"$digest\" {quoted_file}"
    )


def _remote_load_script(
    stage_file: str,
    fifo_inode: int,
    main_pid: int,
    main_sha256: str,
    *,
    artifact_sha256: str | None = None,
    artifact_size: int | None = None,
) -> str:
    if REMOTE_STAGE_FILE_RE.fullmatch(stage_file) is None:
        raise _fail("internal remote load path failed validation")
    if not isinstance(fifo_inode, int) or fifo_inode <= 0 or not isinstance(main_pid, int) or main_pid <= 0:
        raise _fail("internal remote load identity failed validation")
    if SHA256_RE.fullmatch(main_sha256) is None:
        raise _fail("internal Main executable digest failed validation")
    if (artifact_sha256 is None) != (artifact_size is None):
        raise _fail("internal artifact identity is incomplete")
    if artifact_sha256 is not None and SHA256_RE.fullmatch(artifact_sha256) is None:
        raise _fail("internal artifact digest failed validation")
    if artifact_size is not None and (not isinstance(artifact_size, int) or artifact_size <= 0):
        raise _fail("internal artifact size failed validation")
    stage_dir = shlex.quote(str(Path(stage_file).parent))
    quoted_stage = shlex.quote(stage_file)
    artifact_recheck = ""
    if artifact_sha256 is not None and artifact_size is not None:
        artifact_recheck = (
            f"[ \"$(sha256sum -- {quoted_stage} | awk 'NF == 2 {{print $1; exit}}')\" = {artifact_sha256} ]; "
            f"[ \"$(stat -Lc '%s' -- {quoted_stage})\" = {artifact_size} ]; "
        )
    return (
        f": MISTEROSS_LOAD_V1 {quoted_stage}; set -eu; fifo=/dev/MiSTer_cmd; "
        f"[ ! -L {stage_dir} ]; [ -d {stage_dir} ]; "
        f"[ ! -L {quoted_stage} ]; [ -f {quoted_stage} ]; "
        f"stage_stat=$(stat -Lc '%F|%u|%g|%a|%h' -- {quoted_stage}); "
        f"[ \"$(printf '%s\\n' \"$stage_stat\" | awk -F'|' '{{print $1}}')\" = 'regular file' ]; "
        f"[ \"$(printf '%s\\n' \"$stage_stat\" | awk -F'|' '{{print $2}}')\" = 0 ]; "
        f"[ \"$(printf '%s\\n' \"$stage_stat\" | awk -F'|' '{{print $3}}')\" = 0 ]; "
        f"[ \"$(printf '%s\\n' \"$stage_stat\" | awk -F'|' '{{print $4}}')\" = 400 ]; "
        f"[ \"$(printf '%s\\n' \"$stage_stat\" | awk -F'|' '{{print $5}}')\" = 1 ]; "
        f"{artifact_recheck}"
        f"[ ! -L \"$fifo\" ]; [ -p \"$fifo\" ]; "
        f"[ \"$(stat -Lc '%i' -- \"$fifo\")\" = {fifo_inode} ]; "
        f"[ \"$(awk '$1 == \"Uid:\" {{print $2; exit}}' /proc/{main_pid}/status)\" = 0 ]; "
        f"exe_stat=$(stat -Lc '%F|%u|%g|%a|%h' -- /proc/{main_pid}/exe); "
        f"[ \"$(printf '%s\\n' \"$exe_stat\" | awk -F'|' '{{print $1}}')\" = 'regular file' ]; "
        f"[ \"$(printf '%s\\n' \"$exe_stat\" | awk -F'|' '{{print $2}}')\" = 0 ]; "
        f"[ \"$(printf '%s\\n' \"$exe_stat\" | awk -F'|' '{{print $3}}')\" = 0 ]; "
        f"[ \"$(sha256sum -- /proc/{main_pid}/exe | awk 'NF == 2 {{print $1; exit}}')\" = {main_sha256} ]; "
        f"held=0; for fd in /proc/{main_pid}/fd/*; do [ -e \"$fd\" ] || continue; "
        f"fd_stat=$(stat -Lc '%F|%i' -- \"$fd\" 2>/dev/null || true); "
        f"fd_type=$(printf '%s\\n' \"$fd_stat\" | awk -F'|' '{{print $1}}'); "
        f"fd_inode=$(printf '%s\\n' \"$fd_stat\" | awk -F'|' '{{print $2}}'); "
        f"if [ \"$fd_type\" = fifo ] && [ \"$fd_inode\" = {fifo_inode} ]; then held=1; break; fi; done; "
        f"[ \"$held\" = 1 ]; printf '%s\\n' {shlex.quote(f'load_core {stage_file}')} > \"$fifo\""
    )


def _new_remote_stage() -> tuple[str, str]:
    token = secrets.token_hex(16)
    stage_dir = f"/tmp/misteross-{token}"
    stage_file = f"{stage_dir}/artifact.rbf"
    if REMOTE_STAGE_DIR_RE.fullmatch(stage_dir) is None or REMOTE_STAGE_FILE_RE.fullmatch(stage_file) is None:
        raise _fail("internal remote stage path failed validation")
    return stage_dir, stage_file


def _control_dir_empty(control_dir: Path) -> bool:
    try:
        next(control_dir.iterdir())
    except StopIteration:
        return True
    except FileNotFoundError:
        return True
    except OSError as exc:
        raise _fail(f"cannot inspect SSH control directory {control_dir}: {exc}") from exc
    return False


def _wait_for_control_shutdown(control_dir: Path) -> bool:
    deadline = time.monotonic() + CONTROL_WAIT_TIMEOUT
    while True:
        if _control_dir_empty(control_dir):
            return True
        if time.monotonic() >= deadline:
            return False
        time.sleep(CONTROL_WAIT_INTERVAL)


def _control_operation(
    ssh: str,
    target: str,
    ssh_options: Sequence[str],
    operation: str,
) -> subprocess.CompletedProcess[str]:
    return _run(
        [ssh, *ssh_options, "-o", "BatchMode=yes", "-O", operation, target],
        timeout=CONTROL_COMMAND_TIMEOUT,
    )


def _cleanup_ssh_control_master(
    ssh: str,
    target: str,
    ssh_options: Sequence[str],
    control_dir: Path,
) -> None:
    """Close the master and remove its directory only after bounded confirmation."""

    exited = _control_operation(ssh, target, ssh_options, "exit")
    if exited.returncode == 0 and _wait_for_control_shutdown(control_dir):
        try:
            shutil.rmtree(control_dir)
        except OSError as exc:
            raise _fail(
                f"SSH control master stopped but private control directory cleanup failed; "
                f"preserve {control_dir}: {exc}"
            ) from exc
        if control_dir.exists():
            raise _fail(
                f"SSH control master stopped but private control directory remains; "
                f"preserve {control_dir}"
            )
        return

    # A failed exit can leave a master accepting sessions. Stop accepting new
    # sessions, request shutdown again, and only unlink after the socket path
    # has disappeared. Every operation and wait is deliberately bounded.
    stopped = _control_operation(ssh, target, ssh_options, "stop")
    if stopped.returncode == 0:
        retried = _control_operation(ssh, target, ssh_options, "exit")
        if retried.returncode == 0 or _control_dir_empty(control_dir):
            if _wait_for_control_shutdown(control_dir):
                try:
                    shutil.rmtree(control_dir)
                except OSError as exc:
                    raise _fail(
                        f"SSH control master fallback stopped but private control directory cleanup failed; "
                        f"preserve {control_dir}: {exc}"
                    ) from exc
                if control_dir.exists():
                    raise _fail(
                        f"SSH control master fallback stopped but private control directory remains; "
                        f"preserve {control_dir}"
                    )
                return

    raise _fail(
        f"SSH control master shutdown could not be confirmed; preserve {control_dir} "
        "for operator cleanup"
    )


@contextlib.contextmanager
def _ephemeral_ssh_session(ssh: str, target: str):
    """Share one private, temporary SSH control socket across a run."""

    control_dir: Path | None = None
    ssh_options: tuple[str, ...] = SSH_OPTIONS
    created_dir: Path | None = None
    try:
        try:
            created_dir = Path(tempfile.mkdtemp(prefix="misteross-ssh-"))
            os.chmod(created_dir, 0o700)
        except OSError as exc:
            if created_dir is not None:
                shutil.rmtree(created_dir, ignore_errors=True)
            raise _fail(f"cannot create private SSH control directory: {exc}") from exc
        control_dir = created_dir
        control_path = control_dir / "control-%C"
        ssh_options = (
            *SSH_OPTIONS,
            "-o",
            "ControlMaster=auto",
            "-o",
            "ControlPersist=120s",
            "-o",
            f"ControlPath={control_path}",
        )
        yield ssh_options
    finally:
        if control_dir is not None:
            _cleanup_ssh_control_master(ssh, target, ssh_options, control_dir)


def _mister_transport_session(
    evidence: ArtifactEvidence,
    args: argparse.Namespace,
    ssh: str,
    scp: str,
    target: str,
    ssh_options: Sequence[str],
) -> int:
    stage_dir, stage_file = _new_remote_stage()
    expected_main = args.expected_main_sha256
    if not args.dry_run and expected_main is None:
        raise _fail(
            "non-dry network action requires explicit expected Main executable SHA-256 "
            "(--expected-main-sha256 or MISTER_EXPECTED_MAIN_SHA256)"
        )

    preflight_text = _remote_preflight_script()
    preflight_command = [ssh, *ssh_options, target, preflight_text]
    _print_command("remote preflight (ARM/FIFO/Main, read-only)", preflight_command)
    preflight_result = _remote_command(ssh, target, preflight_text, ssh_options=ssh_options)
    if preflight_result.returncode != 0:
        raise _fail(
            f"remote ARM/FIFO/Main preflight failed: "
            f"{_short_output(preflight_result.stdout, preflight_result.stderr)}"
        )
    preflight = _parse_remote_preflight(preflight_result.stdout)
    if expected_main is not None and expected_main != preflight.main_sha256:
        if args.dry_run:
            print(
                f"Main executable digest discovered: {preflight.main_sha256}; "
                f"operator value differs ({expected_main}), dry-run will not act",
                flush=True,
            )
        else:
            raise _fail(
                f"Main executable SHA-256 mismatch: expected {expected_main}, got {preflight.main_sha256}"
            )
    identity = (
        "discovered (not authenticated for action)"
        if args.dry_run
        else f"authenticated sha256={preflight.main_sha256}"
    )

    # SCP accepts the same bounded ``-o key=value`` options but has no SSH
    # ``-T`` switch in all supported OpenSSH versions.  Keep the option pairs
    # intact when dropping that first SSH-only flag.
    scp_command = [scp, *ssh_options[1:], str(evidence.artifact_path), f"{target}:{stage_file}"]
    load_line = f"load_core {stage_file}"
    load_remote = _remote_load_script(
        stage_file,
        preflight.fifo_inode,
        preflight.main_pid,
        preflight.main_sha256,
        artifact_sha256=evidence.sha256,
        artifact_size=evidence.size_bytes,
    )
    load_command = [ssh, *ssh_options, target, load_remote]

    print("transport: mister ARM-side volatile FIFO")
    print("board: MiSTer-compatible DE10-Nano RBF contract")
    print(f"Main identity: {identity}")
    print(f"remote_stage: {stage_file}")
    print(f"remote_private_directory: {stage_dir} (volatile; left for reboot/recovery)")
    _print_command("upload action", scp_command)
    _print_command("load action", load_command)
    if args.dry_run:
        print("DRY RUN: remote read-only preflight complete; no mkdir, upload, hash, or FIFO load performed")
        return 0

    mkdir_text = _remote_mkdir_script(stage_dir)
    mkdir_command = [ssh, *ssh_options, target, mkdir_text]
    _print_command("private staging directory action", mkdir_command)
    mkdir_result = _remote_command(ssh, target, mkdir_text, ssh_options=ssh_options)
    if mkdir_result.returncode != 0:
        raise _fail(
            "remote private staging directory creation failed (collision/race/permissions): "
            f"{_short_output(mkdir_result.stdout, mkdir_result.stderr)}"
        )
    directory_type, directory_uid, directory_gid, directory_mode, directory_nlink = _parse_remote_directory(
        mkdir_result.stdout
    )
    if directory_type not in {"directory", "dir"} or directory_uid != 0 or directory_gid != 0 or directory_mode != 0o700 or directory_nlink < 2:
        raise _fail("remote private staging directory is not root-owned mode 0700 with safe links")

    upload = _run(scp_command, timeout=REMOTE_TIMEOUT)
    if upload.returncode != 0:
        raise _fail(f"SCP upload failed with exit {upload.returncode}: {_short_output(upload.stdout, upload.stderr)}")

    verify_text = _remote_verify_script(stage_dir, stage_file)
    verify_command = [ssh, *ssh_options, target, verify_text]
    _print_command("remote metadata/hash verification", verify_command)
    verify = _remote_command(ssh, target, verify_text, ssh_options=ssh_options)
    if verify.returncode != 0:
        raise _fail(f"remote staged RBF verification failed: {_short_output(verify.stdout, verify.stderr)}")
    metadata = _parse_remote_stage(verify.stdout, stage_file)
    if metadata.sha256 != evidence.sha256:
        raise _fail(f"remote SHA-256 mismatch: expected {evidence.sha256}, got {metadata.sha256}")

    _print_command("load request action", load_command)
    loaded = _remote_command(ssh, target, load_remote, ssh_options=ssh_options)
    if loaded.returncode != 0:
        raise _fail(
            f"remote FIFO load request failed with exit {loaded.returncode}: "
            f"{_short_output(loaded.stdout, loaded.stderr)}"
        )
    print(f"load request dispatched; outcome unverified: {load_line}")
    return 0


def _mister_transport(evidence: ArtifactEvidence, args: argparse.Namespace) -> int:
    _require_board_attestation(args, EXPECTED_NETWORK_BOARD)
    host = validate_host(args.host)
    user = validate_user(args.user)
    ssh = _resolve_executable(args.ssh or "ssh", "ssh")
    scp = _resolve_executable(args.scp or "scp", "scp")
    target = f"{user}@{host}"
    if not args.dry_run and args.expected_main_sha256 is None:
        raise _fail(
            "non-dry network action requires explicit expected Main executable SHA-256 "
            "(--expected-main-sha256 or MISTER_EXPECTED_MAIN_SHA256)"
        )
    with _ephemeral_ssh_session(ssh, target) as ssh_options:
        return _mister_transport_session(evidence, args, ssh, scp, target, ssh_options)


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--experiment", default=os.environ.get("EXP", "010_blinky"))
    parser.add_argument("--build", default=os.environ.get("BUILD", "oss"))
    parser.add_argument(
        "--transport",
        default=os.environ.get("PROGRAM_TRANSPORT", "mister"),
        help="mister (default ARM/FIFO) or jtag (optional external USB-Blaster)",
    )
    parser.add_argument("--host", default=_env_value("MISTER_HOST", "MISTER_PI_HOST", "PROGRAM_HOST"))
    parser.add_argument("--user", default=_env_value("MISTER_USER", "MISTER_PI_USER", "PROGRAM_USER"))
    parser.add_argument("--cable", default=_env_value("PROGRAM_CABLE"))
    parser.add_argument("--cable-index", default=_env_value("PROGRAM_CABLE_INDEX"))
    parser.add_argument("--programmer", default=_env_value("PROGRAMMER", "OPENFPGALOADER", "OPENFPGALOADER_BIN"))
    parser.add_argument("--ssh", default=_env_value("PROGRAM_SSH", "SSH_BIN"))
    parser.add_argument("--scp", default=_env_value("PROGRAM_SCP", "SCP_BIN"))
    parser.add_argument("--expected-board", default=_env_value("PROGRAM_EXPECTED_BOARD", "EXPECTED_BOARD"))
    parser.add_argument(
        "--expected-main-sha256",
        default=_env_value("MISTER_EXPECTED_MAIN_SHA256", "PROGRAM_EXPECTED_MAIN_SHA256"),
    )
    parser.add_argument("--repo-root", type=Path, default=SCRIPT_ROOT)
    parser.add_argument("--dry-run", action="store_true", default=_env_bool("PROGRAM_DRY_RUN"))
    args = parser.parse_args(argv)
    args.transport = str(args.transport).lower()
    if args.transport not in {"mister", "jtag"}:
        parser.error("--transport must be mister or jtag")
    if args.transport == "mister" and args.cable is not None:
        parser.error("--cable is only valid with --transport jtag")
    if args.cable_index is not None:
        if re.fullmatch(r"[0-9]+", str(args.cable_index)) is None:
            parser.error("--cable-index must be a non-negative integer")
        args.cable_index = int(args.cable_index, 10)
    if args.expected_main_sha256 is not None and SHA256_RE.fullmatch(str(args.expected_main_sha256)) is None:
        parser.error("--expected-main-sha256 must be a lowercase SHA-256")
    if args.expected_board is not None and re.fullmatch(r"[A-Za-z0-9_-]{1,32}", str(args.expected_board)) is None:
        parser.error("--expected-board must be a strict board attestation value")
    return args


def main(argv: Sequence[str] | None = None) -> int:
    try:
        args = parse_args(argv)
        evidence = validate_artifact(Path(args.repo_root), str(args.experiment), str(args.build))
        _print_artifact(evidence)
        if args.transport == "jtag":
            return _jtag_transport(evidence, args)
        return _mister_transport(evidence, args)
    except ProgramError as exc:
        print(f"program: {exc}", file=sys.stderr)
        return 2
    except OSError as exc:
        print(f"program: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
