#!/usr/bin/env python3
"""Explicit maintenance diagnostic: validate and program volatile JTAG SRAM.

Coordinate kit ownership before use. Normal loads use FogCast package APIs.
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
_SCRIPTS = Path(__file__).resolve().parent
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))
from experiment_policy import PolicyError, policy_for  # noqa: E402
TARGET_DEVICE = "5CSEBA6U23I7"
LOADER_BOARD = "de10nano"
EXPECTED_CABLE_VID = 0x09FB
EXPECTED_CABLE_PID = 0x6810
EXPECTED_IDCODE = 0x02D020DD
COMMAND_TIMEOUT = 20.0
REMOTE_TIMEOUT = 15.0
EXPERIMENT_RE = re.compile(r"^[0-9]{3}_[a-z0-9_]+$")
LANE_RE = re.compile(r"^[a-z][a-z0-9_-]*$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
VID_PID_RE = re.compile(
    r"(?<![0-9A-Fa-f])(?:0x)?([0-9A-Fa-f]{4})\s*:\s*(?:0x)?([0-9A-Fa-f]{4})(?![0-9A-Fa-f])",
    re.IGNORECASE,
)
IDCODE_RE = re.compile(r"\bidcode\s*[:=]?\s*(0x[0-9A-Fa-f]+)", re.IGNORECASE)
TARGET_ALIAS_RE = re.compile(rf"\b{re.escape(TARGET_DEVICE)}\b", re.IGNORECASE)
TARGET_MODEL_RE = re.compile(r"\b5CSE\*A6\b", re.IGNORECASE)
EXPECTED_JTAG_BOARD = "de10nano"
EXPECTED_CABLE_INTERFACE = "usb-blasterII"
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
# External programmer invocations remain bounded.


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


def _validate_oss_policy_resources(build: dict[str, Any], experiment: str) -> None:
    """Classify OSS utilization with the closed experiment policy."""

    try:
        policy = policy_for(experiment)
    except PolicyError as exc:
        raise _fail(str(exc)) from exc

    resources = build.get("resources")
    if not isinstance(resources, dict):
        raise _fail("oss manifest resources must be an object")
    try:
        policy.validate_resources(resources)
    except PolicyError as exc:
        raise _fail(f"oss manifest resources violate experiment policy: {exc}") from exc

    resource_classes = build.get("resource_classes")
    if not isinstance(resource_classes, dict):
        raise _fail("oss manifest resource_classes must be an object")
    if set(resource_classes) != set(resources):
        raise _fail("oss manifest resource_classes must exactly match resources")
    for name in resources:
        expected = policy.classify_resource(name)
        if resource_classes.get(name) != expected:
            raise _fail(f"oss manifest resource {name} is classified {resource_classes.get(name)!r}, expected {expected!r}")

    unknown = build.get("unknown_resources")
    if not isinstance(unknown, dict):
        raise _fail("manifest unknown resource evidence is missing or malformed")
    if unknown:
        raise _fail("manifest contains unknown resource utilization keys")

    hard_blocks = build.get("hard_blocks")
    if not isinstance(hard_blocks, dict):
        raise _fail("oss manifest hard_blocks evidence is missing")
    expected_hard = {
        name for name, classification in resource_classes.items() if classification in {"allowed", "forbidden"}
    }
    if set(hard_blocks) != expected_hard:
        raise _fail("oss manifest hard_blocks must represent every allowed or forbidden resource exactly")
    for name, record in hard_blocks.items():
        _validate_resource_record(record, f"oss.{name}")
        used = _nonnegative_integer(record["used"], f"oss.{name}.used")
        classification = resource_classes[name]
        if classification == "forbidden" and used != 0:
            raise _fail(f"manifest reports forbidden hard-block use for {name}: {used}")
        if classification == "allowed" and used != policy.allowed_hard_blocks[name]:
            raise _fail(
                f"manifest reports {name} used {used}, expected exactly {policy.allowed_hard_blocks[name]}"
            )
        resource = resources[name]
        if record.get("used") != resource.get("used") or record.get("available") != resource.get("available"):
            raise _fail(f"oss hard-block evidence disagrees with resource record for {name}")


def _validate_resources(build: dict[str, Any], lane: str, experiment: str) -> None:
    if lane == "oss":
        try:
            policy_for(experiment)
        except PolicyError:
            pass
        else:
            _validate_oss_policy_resources(build, experiment)
            return
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
    _validate_resources(build, lane, experiment)
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


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--experiment", default=os.environ.get("EXP", "010_blinky"))
    parser.add_argument("--build", default=os.environ.get("BUILD", "oss"))
    parser.add_argument("--transport", choices=("jtag",), default=os.environ.get("PROGRAM_TRANSPORT", "jtag"))
    parser.add_argument("--cable", default=_env_value("PROGRAM_CABLE"))
    parser.add_argument("--cable-index", default=_env_value("PROGRAM_CABLE_INDEX"))
    parser.add_argument("--programmer", default=_env_value("PROGRAMMER", "OPENFPGALOADER", "OPENFPGALOADER_BIN"))
    parser.add_argument("--expected-board", default=_env_value("PROGRAM_EXPECTED_BOARD", "EXPECTED_BOARD"))
    parser.add_argument("--repo-root", type=Path, default=SCRIPT_ROOT)
    parser.add_argument("--dry-run", action="store_true", default=_env_bool("PROGRAM_DRY_RUN"))
    args = parser.parse_args(argv)
    if args.transport != "jtag":
        parser.error("only explicit JTAG maintenance programming is supported")
    if args.cable_index is not None:
        if re.fullmatch(r"[0-9]+", str(args.cable_index)) is None:
            parser.error("--cable-index must be a non-negative integer")
        args.cable_index = int(args.cable_index, 10)
    if args.expected_board is not None and re.fullmatch(r"[A-Za-z0-9_-]{1,32}", str(args.expected_board)) is None:
        parser.error("--expected-board must be a strict board attestation value")
    return args


def main(argv: Sequence[str] | None = None) -> int:
    try:
        if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT", "").strip():
            raise _fail(
                "shared toolchain cache is unsupported for program.py loader selection; "
                "unset FES_TOOLCHAIN_CACHE_ROOT for the existing local programming workflow"
            )
        args = parse_args(argv)
        evidence = validate_artifact(Path(args.repo_root), str(args.experiment), str(args.build))
        _print_artifact(evidence)
        return _jtag_transport(evidence, args)
    except ProgramError as exc:
        print(f"program: {exc}", file=sys.stderr)
        return 2
    except OSError as exc:
        print(f"program: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
