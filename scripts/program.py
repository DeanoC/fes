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
import hashlib
import json
import os
import re
import shlex
import shutil
import stat
import subprocess
import sys
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
STAGE_RE = re.compile(r"^/tmp/misteross-[0-9]{3}_[a-z0-9_]*-[0-9a-f]{16}\.rbf$")
ARM_ARCHITECTURES = frozenset(("aarch64", "armv7l", "armv8l", "armv7", "arm"))
MAIN_PROCESS_NAMES = frozenset(("Main_MiSTer", "MiSTer"))

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


@dataclass(frozen=True)
class CableRow:
    text: str
    vendor_id: int
    product_id: int


@dataclass(frozen=True)
class SelectedCable:
    row: CableRow
    identifier: str | None


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


def _validate_hard_blocks(build: dict[str, Any]) -> None:
    hard_blocks = build.get("hard_blocks")
    if not isinstance(hard_blocks, dict) or not hard_blocks:
        raise _fail("manifest is missing hard-block utilization evidence")
    for name, record in hard_blocks.items():
        if not isinstance(name, str) or not isinstance(record, dict):
            raise _fail("manifest hard-block evidence is malformed")
        used = record.get("used")
        if used is None:
            if record.get("status") != "excluded" or record.get("evidence_kind") != "static_exclusion":
                raise _fail(f"manifest hard-block evidence is ambiguous for {name}")
            continue
        if not _is_number(used) or used < 0:
            raise _fail(f"manifest hard-block usage is malformed for {name}")
        if used != 0:
            raise _fail(f"manifest reports forbidden hard-block use for {name}: {used}")
    unknown = build.get("unknown_resources")
    if not isinstance(unknown, dict):
        raise _fail("manifest unknown resource evidence is missing or malformed")
    if unknown:
        raise _fail("manifest contains unknown resource utilization keys")


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
    if not _is_number(requested) or requested != 50.0:
        raise _fail(f"manifest timing request must be exactly 50.0 MHz, got {requested!r}")
    if not _is_number(achieved) or achieved < requested:
        raise _fail(f"manifest timing does not meet 50.0 MHz: {achieved!r}")
    clock = timing.get("clock")
    if not isinstance(clock, str) or not clock.startswith("FPGA_CLK1_50"):
        raise _fail(f"manifest timing clock is not the expected FPGA_CLK1_50 input: {clock!r}")
    _validate_hard_blocks(build)

    reproducibility = _as_mapping(build.get("reproducibility"), "build.reproducibility")
    if reproducibility.get("rbf_sha256") != expected_hash:
        raise _fail("manifest reproducibility RBF hash does not match canonical artifact record")
    size = reproducibility.get("rbf_size_bytes")
    if not isinstance(size, int) or isinstance(size, bool) or size <= 0:
        raise _fail("manifest reproducibility RBF size is missing or invalid")
    if size != artifact_size:
        raise _fail(f"canonical RBF size mismatch: manifest {size}, actual {artifact_size}")
    if reproducibility.get("rbf_stability_measured") is not True:
        raise _fail("manifest RBF stability was not measured")
    if reproducibility.get("rbf_stable") is not True:
        raise _fail("manifest RBF stability is not pass")
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
    )


def _env_value(*names: str) -> str | None:
    for name in names:
        value = os.environ.get(name)
        if value is not None and value != "":
            return value
    return None


def _env_bool(name: str) -> bool:
    return os.environ.get(name, "").strip().lower() in {"1", "true", "yes", "on"}


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
    print("rbf_stability: measured/pass", flush=True)


def _identifier_tokens(text: str) -> set[str]:
    return {token for token in re.split(r"[^A-Za-z0-9_.-]+", text) if token}


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
            rows.append(CableRow(stripped, int(match.group(1), 16), int(match.group(2), 16)))
    return rows


def select_cable(rows: Sequence[CableRow], cable: str | None) -> SelectedCable:
    if not rows:
        raise _fail("no DE10-Nano detected by openFPGALoader")
    selected: list[CableRow]
    if cable is None:
        if len(rows) != 1:
            raise _fail(f"ambiguous multiple DE10-Nano/USB-Blaster devices ({len(rows)}); pass an exact --cable")
        selected = [rows[0]]
    else:
        if re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", cable) is None:
            raise _fail("invalid cable identifier: expected one strict discovered identifier")
        selected = [row for row in rows if cable in _identifier_tokens(row.text)]
        if len(selected) != 1:
            raise _fail(f"--cable must match exactly one discovered cable identifier: {cable}")
    row = selected[0]
    if (row.vendor_id, row.product_id) != (EXPECTED_CABLE_VID, EXPECTED_CABLE_PID):
        raise _fail(
            "unexpected USB-Blaster cable VID/PID: "
            f"0x{row.vendor_id:04x}:0x{row.product_id:04x}; expected 0x{EXPECTED_CABLE_VID:04x}:0x{EXPECTED_CABLE_PID:04x}"
        )
    identifier = cable
    if identifier is None:
        for token in _identifier_tokens(row.text):
            if token.lower().startswith("usb-blaster"):
                identifier = token
                break
    return SelectedCable(row=row, identifier=identifier)


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


def _jtag_transport(evidence: ArtifactEvidence, args: argparse.Namespace) -> int:
    configured = args.programmer or str(evidence.repo_root / "build/toolchain/install/bin/openFPGALoader")
    programmer = _resolve_executable(configured, "openFPGALoader")
    scan_command = [programmer, "--board", LOADER_BOARD, "--scan-usb"]
    _print_command("cable discovery", scan_command)
    scan_result = _run(scan_command)
    scan_output = "\n".join((scan_result.stdout, scan_result.stderr))
    rows = parse_cable_scan(scan_output, scan_result.returncode, scan_command)
    selected = select_cable(rows, args.cable)

    selector = ["--board", LOADER_BOARD]
    if selected.identifier is not None:
        # An exact cable selector is used only after it was observed verbatim.
        selector = ["--cable", selected.identifier]
    detect_command = [programmer, *selector, "--detect"]
    _print_command("JTAG discovery", detect_command)
    detect_result = _run(detect_command)
    detect_output = "\n".join((detect_result.stdout, detect_result.stderr))
    matched = parse_jtag_detect(detect_output, detect_result.returncode, detect_command)

    final_command = [programmer, *selector, "--write-sram", str(evidence.artifact_path)]
    print(f"board: {LOADER_BOARD}")
    print(f"cable: {selected.identifier or 'one exact USB-Blaster II VID/PID row'}")
    print(f"JTAG target: {matched}")
    _print_command("volatile action", final_command)
    if args.dry_run:
        print("DRY RUN: no SRAM programming action invoked")
        return 0
    result = _run(final_command, timeout=REMOTE_TIMEOUT)
    if result.returncode != 0:
        raise _fail(f"programmer failed with exit {result.returncode}: {_short_output(result.stdout, result.stderr)}")
    print("volatile SRAM programming complete")
    return 0


def _remote_command(
    ssh: str,
    target: str,
    command: str,
    *,
    timeout: float = REMOTE_TIMEOUT,
) -> subprocess.CompletedProcess[str]:
    return _run([ssh, *SSH_OPTIONS, target, command], timeout=timeout)


def _remote_check(
    ssh: str,
    target: str,
    command: str,
    label: str,
) -> subprocess.CompletedProcess[str]:
    _print_command(f"remote preflight ({label})", [ssh, *SSH_OPTIONS, target, command])
    result = _remote_command(ssh, target, command)
    if result.returncode != 0:
        raise _fail(f"remote {label} check failed: {_short_output(result.stdout, result.stderr)}")
    return result


def _mister_transport(evidence: ArtifactEvidence, args: argparse.Namespace) -> int:
    host = validate_host(args.host)
    user = validate_user(args.user)
    ssh = _resolve_executable(args.ssh or "ssh", "ssh")
    scp = _resolve_executable(args.scp or "scp", "scp")
    target = f"{user}@{host}"
    stage = f"/tmp/misteross-{evidence.experiment}-{evidence.sha256[:16]}.rbf"
    if STAGE_RE.fullmatch(stage) is None:
        raise _fail(f"internal stage path failed validation: {stage}")

    architecture = _remote_check(ssh, target, "uname -m", "architecture")
    arch_lines = [line.strip() for line in architecture.stdout.splitlines() if line.strip()]
    if len(arch_lines) != 1 or arch_lines[0] not in ARM_ARCHITECTURES:
        raise _fail(f"remote architecture is not an expected ARM target: {arch_lines or 'unknown'}")

    _remote_check(ssh, target, "test -p /dev/MiSTer_cmd", "FIFO")
    _remote_check(ssh, target, "test -d /tmp && test -w /tmp", "volatile /tmp")
    process = _remote_check(ssh, target, "ps -e -o comm=", "Main_MiSTer process")
    process_names = [
        token
        for line in process.stdout.splitlines()
        if line.strip()
        for token in line.strip().split()
    ]
    matching_processes = [name for name in process_names if name in MAIN_PROCESS_NAMES]
    if len(matching_processes) != 1:
        raise _fail(
            "remote expected MiSTer process is ambiguous or missing; "
            "need exactly one Main_MiSTer/MiSTer process"
        )
    _remote_check(ssh, target, f"test ! -e {shlex.quote(stage)}", "unique /tmp staging path")

    # SCP accepts the same bounded ``-o key=value`` options but has no SSH
    # ``-T`` switch in all supported OpenSSH versions.  Keep the option pairs
    # intact when dropping that first SSH-only flag.
    scp_command = [scp, *SSH_OPTIONS[1:], str(evidence.artifact_path), f"{target}:{stage}"]
    load_line = f"load_core {stage}"
    load_remote = f"printf '%s\\n' {shlex.quote(load_line)} > /dev/MiSTer_cmd"
    load_command = [ssh, *SSH_OPTIONS, target, load_remote]

    print("transport: mister ARM-side volatile FIFO")
    print("board: MiSTer-compatible DE10-Nano RBF contract")
    print(f"remote_stage: {stage}")
    _print_command("upload action", scp_command)
    _print_command("load action", load_command)
    if args.dry_run:
        print("DRY RUN: remote preflight complete; no upload or FIFO load performed")
        return 0

    upload = _run(scp_command, timeout=REMOTE_TIMEOUT)
    if upload.returncode != 0:
        raise _fail(f"SCP upload failed with exit {upload.returncode}: {_short_output(upload.stdout, upload.stderr)}")

    verify_command = [ssh, *SSH_OPTIONS, target, f"sha256sum -- {shlex.quote(stage)}"]
    _print_command("remote hash verification", verify_command)
    verify = _remote_command(ssh, target, f"sha256sum -- {shlex.quote(stage)}")
    if verify.returncode != 0:
        raise _fail(f"remote SHA-256 verification failed: {_short_output(verify.stdout, verify.stderr)}")
    digest_lines = [line.strip() for line in verify.stdout.splitlines() if line.strip()]
    if len(digest_lines) != 1:
        raise _fail("remote SHA-256 verification was ambiguous")
    fields = digest_lines[0].split()
    if not fields or SHA256_RE.fullmatch(fields[0]) is None:
        raise _fail("remote SHA-256 output was malformed")
    if len(fields) > 2:
        raise _fail("remote SHA-256 output was ambiguous")
    if len(fields) > 1 and fields[-1].lstrip("*") != stage:
        raise _fail("remote SHA-256 output named an unexpected path")
    if fields[0] != evidence.sha256:
        raise _fail(f"remote SHA-256 mismatch: expected {evidence.sha256}, got {fields[0]}")

    loaded = _remote_command(ssh, target, load_remote)
    if loaded.returncode != 0:
        raise _fail(f"remote FIFO load failed with exit {loaded.returncode}: {_short_output(loaded.stdout, loaded.stderr)}")
    print(f"volatile MiSTer load complete: {load_line}")
    return 0


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
    parser.add_argument("--programmer", default=_env_value("PROGRAMMER", "OPENFPGALOADER", "OPENFPGALOADER_BIN"))
    parser.add_argument("--ssh", default=_env_value("PROGRAM_SSH", "SSH_BIN"))
    parser.add_argument("--scp", default=_env_value("PROGRAM_SCP", "SCP_BIN"))
    parser.add_argument("--repo-root", type=Path, default=SCRIPT_ROOT)
    parser.add_argument("--dry-run", action="store_true", default=_env_bool("PROGRAM_DRY_RUN"))
    args = parser.parse_args(argv)
    args.transport = str(args.transport).lower()
    if args.transport not in {"mister", "jtag"}:
        parser.error("--transport must be mister or jtag")
    if args.transport == "mister" and args.cable is not None:
        parser.error("--cable is only valid with --transport jtag")
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
