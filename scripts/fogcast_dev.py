#!/usr/bin/env python3
"""Host-only transport for the FogCast FPGA development recovery protocol.

The transport consumes an already-reviewed four-file bundle.  It never builds
or rewrites evidence, never places credentials in argv, and never invokes a
shell locally.  Remote shell text contains only fixed tokens and paths derived
from a strictly validated 32-character run ID.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import shlex
import shutil
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Mapping, Sequence


REPO_ROOT = Path(__file__).resolve().parents[1]
EXPERIMENT = "020_linux_mailbox"
BOARD = "misterpi"
MEMBER_NAMES = ("manifest.json", "resource_evidence.json", "top.rbf", "bundle.sha256")
CHECKSUM_MEMBER_NAMES = MEMBER_NAMES[:3]
RUN_ID_RE = re.compile(r"^[0-9a-f]{32}$")
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
BOOT_ID_RE = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
USER_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_.-]{0,31}$")
HOST_RE = re.compile(r"^[A-Za-z0-9](?:[A-Za-z0-9.:-]{0,251}[A-Za-z0-9])?$")
EXECUTABLE_RE = re.compile(r"^[A-Za-z0-9_+./-]+$")
REMOTE_STAGE_RE = re.compile(r"^/tmp/misteross-fpgadev-[0-9a-f]{32}$")

MANIFEST_KEYS = (
    "schema", "run_id", "experiment", "board", "build_lane",
    "artifact_filename", "artifact_size", "artifact_sha256", "source_commit",
)
EVIDENCE_KEYS = (
    "schema", "experiment", "board", "build_lane", "source_commit",
    "artifact_sha256", "synthesis_report_sha256", "clock_inputs",
    "external_input_ports", "external_output_ports", "bidirectional_ports",
    "hps_general_purpose_interfaces", "pll_blocks", "dsp_blocks",
    "block_memory_bits", "lutram_bits", "sdram_interfaces",
)
RESULT_KEYS = (
    "schema", "run_id", "generation", "session", "mode", "experiment",
    "build_lane", "artifact_sha256", "source_commit", "phase", "primary_code",
    "primary_detail", "payload_hex", "payload_length", "payload_sha256",
    "terminal_word", "recovery_request", "elapsed_ms",
)
READY_KEYS = (
    "schema", "boot_id", "journal_sha256", "owner_session", "owner_generation",
    "profile_sha256", "capabilities", "supervisor_pid", "supervisor_start_time",
    "main_pid", "main_start_time", "agent_pid", "agent_start_time",
)
OWNER_KEYS = (
    "schema", "state", "phase", "boot_id", "run_id", "generation_high_water",
    "active_session", "active_generation", "active_mode", "candidate_session",
    "candidate_generation", "candidate_mode", "quiescing_owner",
    "candidate_owner", "active_owner", "active_leases", "requested_resources",
    "first_failure",
)
NORMAL_MAIN_LEASES = (
    "command_fifo", "core_input_saves", "core_protocol", "fpga_bridges",
    "fpga_generation", "fpga_programming", "main_process_set",
    "native_video_audio",
)
CAPABILITIES = (
    "cast_unavailable", "input_unavailable", "controller_routes_unavailable",
    "presentation_unavailable", "audio_unavailable",
)
RESULT_PHASES = frozenset((
    "intent_committed", "load_attempted", "main_absent", "lease_active",
    "hello_observed", "message_partial", "end_ack_written", "done_observed",
))
RESULT_CODES = frozenset((
    "ok", "load_dispatch_failed", "main_handoff_timeout",
    "no_owner_qualification_failed", "state_store_failed", "mmio_failed",
    "protocol_violation", "message_timeout", "payload_mismatch",
))
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()

REMOTE_COMMAND_TIMEOUT = 30.0
RUN_TIMEOUT = 60.0
RECONNECT_TIMEOUT = 120.0
READINESS_TIMEOUT = 30.0
FAULT_INSPECT_TIMEOUT = 10.0
FAULT_CHILD_TIMEOUT = 10.0
CONTROL_TIMEOUT = 5.0
CONTROL_WAIT_TIMEOUT = 1.0
MAX_RECONNECT_ATTEMPTS = 121
MAX_RESULT_BYTES = 64 * 1024
CLOSED_SSH_OUTCOMES = frozenset((
    255,
    -signal.SIGHUP,
    -signal.SIGINT,
    -signal.SIGKILL,
    -signal.SIGTERM,
))
RESULT_UNAVAILABLE_STDERR = "FOGCAST_FPGA_DEV_RESULT_UNAVAILABLE code=state_store_failed\n"
CORE_NAME_FILE = "/tmp/CORENAME"

SSH_OPTIONS = (
    "-q", "-T",
    "-o", "BatchMode=no",
    "-o", "ConnectTimeout=10",
    "-o", "ConnectionAttempts=1",
    "-o", "ServerAliveInterval=5",
    "-o", "ServerAliveCountMax=2",
    "-o", "StrictHostKeyChecking=yes",
)
SCP_OPTIONS = (
    "-q", "-O",
    "-o", "BatchMode=no",
    "-o", "ConnectTimeout=10",
    "-o", "ConnectionAttempts=1",
    "-o", "ServerAliveInterval=5",
    "-o", "ServerAliveCountMax=2",
    "-o", "StrictHostKeyChecking=yes",
)


class TransportError(ValueError):
    """Raised when local evidence or remote transport cannot be proven safe."""


@dataclass(frozen=True)
class Bundle:
    path: Path
    run_id: str
    lane: str
    artifact_sha256: str
    source_commit: str
    manifest: Mapping[str, object]
    evidence: Mapping[str, object]
    members: tuple[Path, ...]
    member_sha256: Mapping[str, str]
    member_sizes: Mapping[str, int]


@dataclass(frozen=True)
class Attestation:
    board: str
    tool_sha256: str
    main_sha256: str


@dataclass(frozen=True)
class ReadyRecord:
    boot_id: str
    journal_sha256: str
    session: str
    generation: int
    profile_sha256: str
    capabilities: tuple[str, ...]
    supervisor_pid: int
    supervisor_start_time: int
    main_pid: int
    main_start_time: int
    agent_pid: int
    agent_start_time: int
    raw: bytes


@dataclass(frozen=True)
class ResultRecord:
    run_id: str
    generation: int
    session: str
    mode: str
    experiment: str
    build_lane: str
    artifact_sha256: str
    source_commit: str
    phase: str
    primary_code: str
    recovery_request: str
    raw: bytes


@dataclass(frozen=True)
class RunFraming:
    run_id: str
    primary_code: str


@dataclass(frozen=True)
class Inspection:
    run_id: str
    session: str
    generation: int
    phase: str
    pid: int
    start_time: int
    executable_sha256: str


@dataclass(frozen=True)
class PreflightReport:
    ok: bool
    trace_dir: Path | None = None
    argv: tuple[tuple[str, ...], ...] = ()


@dataclass(frozen=True)
class LoadReport:
    ok: bool
    result: ResultRecord | None = None
    ready: ReadyRecord | None = None
    trace_dir: Path | None = None
    argv: tuple[tuple[str, ...], ...] = ()


@dataclass(frozen=True)
class FaultReport:
    ok: bool
    inspect_output: str = ""
    fault_kill_output: str = ""
    inspection: Inspection | None = None
    ready: ReadyRecord | None = None
    run_returncode: int | None = None
    reboot_returncode: int | None = None
    trace_dir: Path | None = None
    argv: tuple[tuple[str, ...], ...] = ()


def _default_resolve(host: str) -> str:
    rows = socket.getaddrinfo(host, None, type=socket.SOCK_STREAM)
    if not rows:
        raise OSError("host did not resolve")
    return rows[0][4][0]


@dataclass(frozen=True)
class DevConfig:
    host: str
    user: str
    expected_board: str
    expected_main_sha256: str
    expected_tool_sha256: str
    ssh: str
    scp: str
    dry_run: bool
    repo_root: Path = REPO_ROOT
    trace_root: Path = REPO_ROOT / "build" / "fogcast-dev"
    run_command: Callable[..., Any] = subprocess.run
    popen_factory: Callable[..., Any] = subprocess.Popen
    scp_command: Callable[..., Any] = subprocess.run
    resolve_host: Callable[[str], str] = _default_resolve
    clock: Callable[[], float] = time.monotonic
    sleep: Callable[[float], None] = time.sleep


def parse_dry_run(value: str) -> bool:
    if value in ("1", "true"):
        return True
    if value in ("0", "false"):
        return False
    raise TransportError("FOGCAST_DEV_DRY_RUN must be exactly 0, 1, false, or true")


def _require_run_id(value: str) -> str:
    if not isinstance(value, str) or RUN_ID_RE.fullmatch(value) is None:
        raise TransportError("run ID must be exactly 32 lowercase hexadecimal characters")
    return value


def _require_lane(value: str) -> str:
    if value not in ("oss", "oracle"):
        raise TransportError("build lane must be exactly oss or oracle")
    return value


def _validate_executable_token(value: str, label: str) -> str:
    if not isinstance(value, str) or not value or EXECUTABLE_RE.fullmatch(value) is None:
        raise TransportError(f"unsafe {label} executable")
    path = Path(value)
    if "/" in value and (not path.is_absolute() or path != Path(os.path.normpath(value))):
        raise TransportError(f"unsafe {label} executable path")
    if ".." in path.parts:
        raise TransportError(f"unsafe {label} executable path")
    return value


def _validate_target(user: str, host: str) -> str:
    if USER_RE.fullmatch(user or "") is None:
        raise TransportError("unsafe FogCast SSH user")
    if HOST_RE.fullmatch(host or "") is None or ".." in host or "@" in host:
        raise TransportError("unsafe FogCast SSH host")
    return f"{user}@{host}"


def remote_stage(run_id: str) -> str:
    value = f"/tmp/misteross-fpgadev-{_require_run_id(run_id)}"
    if REMOTE_STAGE_RE.fullmatch(value) is None:
        raise TransportError("internal remote stage path is unsafe")
    return value


def _fixed_remote(tokens: Sequence[str]) -> str:
    return shlex.join(tuple(tokens))


def remote_run_command(run_id: str) -> str:
    stage = remote_stage(run_id)
    return _fixed_remote((
        "exec", "mister-fpga-dev", "run", "--manifest", f"{stage}/manifest.json",
        "--artifact", f"{stage}/top.rbf",
    ))


def remote_preflight_command(run_id: str) -> str:
    stage = remote_stage(run_id)
    return _fixed_remote((
        "exec", "mister-fpga-dev", "preflight", "--manifest", f"{stage}/manifest.json",
        "--artifact", f"{stage}/top.rbf",
    ))


def remote_fault_command(command: str, run_id: str) -> str:
    if command not in ("fault-arm", "inspect", "fault-kill", "recovery-reboot"):
        raise TransportError("unsafe FogCast fault command")
    return _fixed_remote(("exec", "mister-fpga-dev", command, "--run-id", _require_run_id(run_id)))


def ssh_argv(ssh: str, user: str, host: str, command: str) -> list[str]:
    executable = _validate_executable_token(ssh, "SSH")
    target = _validate_target(user, host)
    if not isinstance(command, str) or not command or "\x00" in command:
        raise TransportError("unsafe remote command")
    return [executable, *SSH_OPTIONS, target, command]


def _canonical_object(raw: bytes, keys: tuple[str, ...], label: str, maximum: int) -> dict[str, object]:
    if not isinstance(raw, bytes) or not 0 < len(raw) <= maximum or not raw.endswith(b"\n") or raw.count(b"\n") != 1:
        raise TransportError(f"{label} is not one bounded canonical JSON line")
    duplicates: list[str] = []

    def pairs(values: list[tuple[str, object]]) -> dict[str, object]:
        result: dict[str, object] = {}
        for key, value in values:
            if key in result:
                duplicates.append(key)
            result[key] = value
        return result

    try:
        value = json.loads(raw, object_pairs_hook=pairs)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise TransportError(f"malformed {label}: {exc}") from exc
    if duplicates or not isinstance(value, dict) or tuple(value) != keys:
        raise TransportError(f"{label} fields are missing, duplicate, unknown, or out of order")
    canonical = (json.dumps(value, separators=(",", ":"), ensure_ascii=True) + "\n").encode()
    if canonical != raw:
        raise TransportError(f"{label} is not canonical")
    return value


def _exact_int(value: object, label: str, *, minimum: int = 0, maximum: int = 2**64 - 1) -> int:
    if type(value) is not int or not minimum <= value <= maximum:
        raise TransportError(f"{label} is not an exact bounded integer")
    return value


def _exact_text(value: object, label: str) -> str:
    if type(value) is not str:
        raise TransportError(f"{label} is not a string")
    return value


def _path_contains_symlink(path: Path) -> bool:
    absolute = Path(os.path.abspath(os.fspath(path)))
    current = Path(absolute.anchor)
    for component in absolute.parts[1:]:
        current /= component
        try:
            if current.is_symlink():
                return True
        except OSError as exc:
            raise TransportError(f"cannot inspect local path {path}: {exc}") from exc
    return False


def _read_private_file(path: Path, label: str, maximum: int) -> tuple[bytes, os.stat_result]:
    if _path_contains_symlink(path):
        raise TransportError(f"{label} path contains a symlink")
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_NONBLOCK", 0)
    try:
        fd = os.open(path, flags)
    except OSError as exc:
        raise TransportError(f"cannot open {label} without following links: {exc}") from exc
    try:
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode) or stat.S_IMODE(before.st_mode) != 0o600:
            raise TransportError(f"{label} must be a private regular 0600 file")
        if before.st_uid != os.getuid() or before.st_nlink != 1 or not 0 < before.st_size <= maximum:
            raise TransportError(f"{label} metadata is unsafe")
        chunks: list[bytes] = []
        remaining = maximum + 1
        while remaining > 0:
            chunk = os.read(fd, min(1024 * 1024, remaining))
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        raw = b"".join(chunks)
        after = os.fstat(fd)
        identity = (before.st_dev, before.st_ino, before.st_size, before.st_mode, before.st_uid, before.st_gid, before.st_nlink, before.st_mtime_ns, before.st_ctime_ns)
        next_identity = (after.st_dev, after.st_ino, after.st_size, after.st_mode, after.st_uid, after.st_gid, after.st_nlink, after.st_mtime_ns, after.st_ctime_ns)
        if identity != next_identity or len(raw) != before.st_size or len(raw) > maximum:
            raise TransportError(f"{label} changed while reading")
        return raw, before
    finally:
        os.close(fd)


def load_bundle(bundle: Path, run_id: str, lane: str) -> Bundle:
    run_id = _require_run_id(run_id)
    lane = _require_lane(lane)
    path = Path(bundle)
    if not path.is_absolute():
        path = Path(os.path.abspath(os.fspath(path)))
    if _path_contains_symlink(path):
        raise TransportError("bundle path contains a symlink")
    try:
        info = path.lstat()
    except OSError as exc:
        raise TransportError(f"cannot inspect bundle: {exc}") from exc
    if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid() or info.st_nlink < 2:
        raise TransportError("bundle must be a private owned 0700 directory")
    try:
        names = tuple(sorted(entry.name for entry in path.iterdir()))
    except OSError as exc:
        raise TransportError(f"cannot enumerate bundle: {exc}") from exc
    if names != tuple(sorted(MEMBER_NAMES)):
        raise TransportError("bundle must contain exactly the reviewed four members")
    limits = {"manifest.json": 4096, "resource_evidence.json": 2048, "top.rbf": 16 * 1024 * 1024, "bundle.sha256": 1024}
    contents: dict[str, bytes] = {}
    sizes: dict[str, int] = {}
    digests: dict[str, str] = {}
    for name in MEMBER_NAMES:
        raw, member_info = _read_private_file(path / name, name, limits[name])
        contents[name] = raw
        sizes[name] = member_info.st_size
        digests[name] = hashlib.sha256(raw).hexdigest()

    manifest = _canonical_object(contents["manifest.json"], MANIFEST_KEYS, "manifest", 4096)
    if _exact_int(manifest["schema"], "manifest schema") != 1:
        raise TransportError("manifest schema must be 1")
    exact_manifest = {
        "run_id": run_id, "experiment": EXPERIMENT, "board": BOARD,
        "build_lane": lane, "artifact_filename": "top.rbf",
    }
    for key, expected in exact_manifest.items():
        if _exact_text(manifest[key], f"manifest {key}") != expected:
            raise TransportError(f"manifest {key} does not match selected transport")
    artifact_size = _exact_int(manifest["artifact_size"], "manifest artifact_size", minimum=1, maximum=16 * 1024 * 1024)
    artifact_hash = _exact_text(manifest["artifact_sha256"], "manifest artifact_sha256")
    source_commit = _exact_text(manifest["source_commit"], "manifest source_commit")
    if SHA256_RE.fullmatch(artifact_hash) is None or COMMIT_RE.fullmatch(source_commit) is None:
        raise TransportError("manifest hashes are not canonical")
    if artifact_size != sizes["top.rbf"] or artifact_hash != digests["top.rbf"]:
        raise TransportError("manifest artifact identity does not match top.rbf")

    evidence = _canonical_object(contents["resource_evidence.json"], EVIDENCE_KEYS, "resource evidence", 2048)
    if _exact_int(evidence["schema"], "resource schema") != 2:
        raise TransportError("resource evidence schema must be 2")
    for key, expected in (("experiment", EXPERIMENT), ("board", BOARD), ("build_lane", lane), ("source_commit", source_commit), ("artifact_sha256", artifact_hash)):
        if _exact_text(evidence[key], f"resource {key}") != expected:
            raise TransportError(f"resource evidence {key} does not match manifest")
    if SHA256_RE.fullmatch(_exact_text(evidence["synthesis_report_sha256"], "synthesis report hash")) is None:
        raise TransportError("resource evidence synthesis hash is not canonical")
    counts = {key: _exact_int(evidence[key], f"resource {key}", maximum=2**32 - 1) for key in EVIDENCE_KEYS[7:]}
    if counts["clock_inputs"] != 1 or counts["hps_general_purpose_interfaces"] != 1:
        raise TransportError("resource evidence requires one clock and one HPS GP interface")
    for key in ("external_input_ports", "external_output_ports", "bidirectional_ports", "pll_blocks", "dsp_blocks", "block_memory_bits", "lutram_bits", "sdram_interfaces"):
        if counts[key] != 0:
            raise TransportError(f"resource evidence {key} must be zero")

    wanted_checksums = b"".join(f"{digests[name]}  {name}\n".encode() for name in CHECKSUM_MEMBER_NAMES)
    if contents["bundle.sha256"] != wanted_checksums:
        raise TransportError("bundle.sha256 does not bind the canonical member set")
    return Bundle(path, run_id, lane, artifact_hash, source_commit, manifest, evidence,
                  tuple(path / name for name in MEMBER_NAMES), digests, sizes)


def parse_attestation(output: str, *, expected_board: str, expected_main_sha256: str, expected_tool_sha256: str) -> Attestation:
    expected = (expected_board, expected_main_sha256, expected_tool_sha256)
    if expected_board != BOARD or any(type(value) is not str for value in expected) or SHA256_RE.fullmatch(expected_main_sha256) is None or SHA256_RE.fullmatch(expected_tool_sha256) is None:
        raise TransportError("invalid expected FogCast attestation")
    lines = output.splitlines(keepends=True)
    if len(lines) != 4 or lines[0] != "FOGCAST_MISTEROSS_ATTEST_V1\n" or any(not line.endswith("\n") for line in lines):
        raise TransportError("remote executable attestation is malformed or ambiguous")
    board_fields = lines[1].rstrip("\n").split("|")
    if board_fields != ["BOARD", expected_board]:
        raise TransportError("wrong target board attestation")

    def executable(line: str, tag: str, path: str, digest: str) -> None:
        fields = line.rstrip("\n").split("|")
        if fields != [tag, path, digest, "regular file", "0", "0", "755", "1"]:
            raise TransportError(f"unsafe or mismatched {tag.lower()} executable attestation")

    executable(lines[2], "TOOL", "/usr/bin/mister-fpga-dev", expected_tool_sha256)
    executable(lines[3], "MAIN", "/media/fat/MiSTer", expected_main_sha256)
    return Attestation(expected_board, expected_tool_sha256, expected_main_sha256)


def parse_ready_record(raw: bytes) -> ReadyRecord:
    value = _canonical_object(raw, READY_KEYS, "ready record", 4096)
    if _exact_int(value["schema"], "ready schema") != 3:
        raise TransportError("ready record schema must be 3")
    boot_id = _exact_text(value["boot_id"], "ready boot_id")
    journal = _exact_text(value["journal_sha256"], "ready journal_sha256")
    session = _exact_text(value["owner_session"], "ready owner_session")
    generation = _exact_int(value["owner_generation"], "ready owner_generation", minimum=1)
    profile = _exact_text(value["profile_sha256"], "ready profile_sha256")
    capabilities = value["capabilities"]
    if BOOT_ID_RE.fullmatch(boot_id) is None or SHA256_RE.fullmatch(journal) is None or RUN_ID_RE.fullmatch(session) is None or SHA256_RE.fullmatch(profile) is None:
        raise TransportError("ready record identity is not canonical")
    if not isinstance(capabilities, list) or tuple(capabilities) != CAPABILITIES:
        raise TransportError("ready record capabilities are not canonical")
    process_values = tuple(
        _exact_int(value[key], f"ready {key}", minimum=1) for key in READY_KEYS[7:]
    )
    return ReadyRecord(
        boot_id, journal, session, generation, profile, tuple(capabilities),
        *process_values, raw,
    )


def parse_readiness(output: str, config: DevConfig) -> ReadyRecord:
    lines = output.splitlines(keepends=True)
    if len(lines) != 7:
        raise TransportError("readiness response is incomplete or ambiguous")
    parse_attestation(
        "".join(lines[:4]), expected_board=config.expected_board,
        expected_main_sha256=config.expected_main_sha256,
        expected_tool_sha256=config.expected_tool_sha256,
    )
    if lines[4] != "FOGCAST_MISTEROSS_READY_V3\n":
        raise TransportError("readiness schema is missing")
    metadata = lines[5].rstrip("\n").split("|")
    record_line = lines[6].rstrip("\n").split("|", 1)
    if len(metadata) != 8 or metadata[:6] != ["READY", "regular file", "0", "0", "600", "1"] or len(record_line) != 2 or record_line[0] != "RECORD":
        raise TransportError("ready record no-follow metadata is unsafe")
    try:
        raw = bytes.fromhex(record_line[1])
        size = int(metadata[6], 10)
    except (ValueError, OverflowError) as exc:
        raise TransportError("ready record response is malformed") from exc
    if str(size) != metadata[6] or size != len(raw) or SHA256_RE.fullmatch(metadata[7]) is None or hashlib.sha256(raw).hexdigest() != metadata[7]:
        raise TransportError("ready record metadata does not bind its bytes")
    return parse_ready_record(raw)


def _canonical_decimal(value: str, label: str, *, maximum: int = 2**64 - 1) -> int:
    try:
        parsed = int(value, 10)
    except (ValueError, OverflowError) as exc:
        raise TransportError(f"{label} is not a canonical decimal integer") from exc
    if parsed < 1 or parsed > maximum or str(parsed) != value:
        raise TransportError(f"{label} is not a canonical decimal integer")
    return parsed


def parse_live_readiness(output: str, ready: ReadyRecord) -> None:
    lines = output.splitlines(keepends=True)
    if len(lines) != 13 or any(not line.endswith("\n") for line in lines):
        raise TransportError("live readiness response is incomplete or ambiguous")
    fields = [line[:-1].split("|") for line in lines]
    if fields[0] != ["FOGCAST_MISTEROSS_LIVE_V1"]:
        raise TransportError("live readiness schema is missing")

    if len(fields[1]) != 2 or fields[1][0] != "BOOT":
        raise TransportError("live boot evidence is malformed")
    try:
        boot_raw = bytes.fromhex(fields[1][1])
    except ValueError as exc:
        raise TransportError("live boot evidence is malformed") from exc
    if boot_raw != (ready.boot_id + "\n").encode():
        raise TransportError("live boot does not match ready record")

    expected_files = (
        ("JOURNAL", ready.journal_sha256),
        ("PROFILE", ready.profile_sha256),
    )
    for observed, (label, digest) in zip(fields[2:4], expected_files, strict=True):
        if observed != [label, "regular file", "0", "0", "600", "1", digest]:
            raise TransportError(f"live {label.lower()} evidence does not match ready record")

    owner_meta = fields[4]
    owner_record = fields[5]
    if len(owner_meta) != 8 or owner_meta[:6] != ["OWNER", "regular file", "0", "0", "600", "1"]:
        raise TransportError("live owner metadata is unsafe")
    if len(owner_record) != 2 or owner_record[0] != "OWNER_RECORD":
        raise TransportError("live owner record is missing")
    try:
        owner_size = _canonical_decimal(owner_meta[6], "owner size", maximum=4096)
        owner_raw = bytes.fromhex(owner_record[1])
    except ValueError as exc:
        raise TransportError("live owner record encoding is malformed") from exc
    if (
        owner_size != len(owner_raw)
        or SHA256_RE.fullmatch(owner_meta[7]) is None
        or hashlib.sha256(owner_raw).hexdigest() != owner_meta[7]
    ):
        raise TransportError("live owner metadata does not bind its bytes")
    owner = _canonical_object(owner_raw, OWNER_KEYS, "owner record", 4096)
    expected_owner = {
        "schema": 1,
        "state": "normal_main",
        "phase": "",
        "boot_id": ready.boot_id,
        "run_id": "",
        "generation_high_water": ready.generation,
        "active_session": ready.session,
        "active_generation": ready.generation,
        "active_mode": "fpga_native",
        "candidate_session": "",
        "candidate_generation": 0,
        "candidate_mode": "none",
        "quiescing_owner": "none",
        "candidate_owner": "none",
        "active_owner": "compat_main",
        "active_leases": list(NORMAL_MAIN_LEASES),
        "requested_resources": [],
        "first_failure": "",
    }
    if owner != expected_owner:
        raise TransportError("live owner record does not bind normal_main readiness")

    expected_processes = (
        ("supervisor", ready.supervisor_pid, ready.supervisor_start_time),
        ("main", ready.main_pid, ready.main_start_time),
        ("agent", ready.agent_pid, ready.agent_start_time),
    )
    for observed, (name, pid, start_time) in zip(fields[6:9], expected_processes, strict=True):
        if observed != ["PROC", name, str(pid), str(start_time)]:
            raise TransportError(f"live {name} process tuple is stale")
    if fields[9] != ["FIFO", "fifo", "0", "0", "600", "1"]:
        raise TransportError("live Main FIFO is not a root-owned 0600 singleton")
    if fields[10] != ["FPGA", "6f7065726174696e670a"]:
        raise TransportError("live FPGA manager is not operating")
    if fields[11] != ["MENU_FIRST", "4d454e550a"] or fields[12] != ["MENU_SECOND", "4d454e550a"]:
        raise TransportError("live MENU observations are not exact and stable")


def _safe_detail(value: str) -> bool:
    return (
        len(value.encode("utf-8")) <= 256
        and "/" not in value
        and "\\" not in value
        and all(character == " " or (character.isprintable() and not character.isspace()) for character in value)
    )


def parse_result(raw: bytes, bundle: Bundle) -> ResultRecord:
    value = _canonical_object(raw, RESULT_KEYS, "result", MAX_RESULT_BYTES)
    if _exact_int(value["schema"], "result schema") != 1:
        raise TransportError("result schema must be 1")
    run_id = _exact_text(value["run_id"], "result run_id")
    generation = _exact_int(value["generation"], "result generation", minimum=1)
    session = _exact_text(value["session"], "result session")
    mode = _exact_text(value["mode"], "result mode")
    experiment = _exact_text(value["experiment"], "result experiment")
    lane = _exact_text(value["build_lane"], "result build_lane")
    artifact = _exact_text(value["artifact_sha256"], "result artifact_sha256")
    source = _exact_text(value["source_commit"], "result source_commit")
    phase = _exact_text(value["phase"], "result phase")
    primary = _exact_text(value["primary_code"], "result primary_code")
    detail = _exact_text(value["primary_detail"], "result primary_detail")
    payload_hex = _exact_text(value["payload_hex"], "result payload_hex")
    payload_length = _exact_int(value["payload_length"], "result payload_length", maximum=256)
    payload_sha = _exact_text(value["payload_sha256"], "result payload_sha256")
    terminal = _exact_text(value["terminal_word"], "result terminal_word")
    recovery = _exact_text(value["recovery_request"], "result recovery_request")
    _exact_int(value["elapsed_ms"], "result elapsed_ms")
    if RUN_ID_RE.fullmatch(run_id) is None or RUN_ID_RE.fullmatch(session) is None or SHA256_RE.fullmatch(artifact) is None or COMMIT_RE.fullmatch(source) is None:
        raise TransportError("result immutable identity is not canonical")
    if mode != "updating" or experiment != EXPERIMENT or lane not in ("oss", "oracle") or phase not in RESULT_PHASES or primary not in RESULT_CODES:
        raise TransportError("result enum is outside the closed schema")
    if not _safe_detail(detail) or (primary == "ok" and detail != "") or (primary != "ok" and detail == ""):
        raise TransportError("result detail is inconsistent or unsafe")
    if recovery not in ("pending", "failed"):
        raise TransportError("result recovery_request is outside the closed schema")
    if len(payload_hex) % 2 or any(character not in "0123456789abcdef" for character in payload_hex):
        raise TransportError("result payload_hex is not canonical")
    try:
        payload = bytes.fromhex(payload_hex)
    except ValueError as exc:
        raise TransportError("result payload_hex is invalid") from exc
    if len(payload) != payload_length or SHA256_RE.fullmatch(payload_sha) is None or hashlib.sha256(payload).hexdigest() != payload_sha:
        raise TransportError("result payload accounting is inconsistent")
    if not re.fullmatch(r"[0-9a-f]{8}", terminal):
        raise TransportError("result terminal word is malformed")
    if phase == "done_observed":
        if terminal != "d3130c00":
            raise TransportError("done result does not retain the DONE word")
    elif terminal != "00000000":
        raise TransportError("pre-DONE result has a nonzero terminal word")
    if primary == "ok" and (phase != "done_observed" or payload != b"OSS FPGA OK\n" or terminal != "d3130c00"):
        raise TransportError("successful result does not contain the exact mailbox outcome")
    bindings = (
        (run_id, bundle.run_id, "run_id"), (lane, bundle.lane, "build_lane"),
        (artifact, bundle.artifact_sha256, "artifact_sha256"),
        (source, bundle.source_commit, "source_commit"),
    )
    for observed, expected, label in bindings:
        if observed != expected:
            raise TransportError(f"result {label} does not match the selected run/readiness identity")
    if recovery == "failed":
        raise TransportError("result recovery_request=failed")
    return ResultRecord(run_id, generation, session, mode, experiment, lane, artifact, source, phase, primary, recovery, raw)


RUN_RESULT_RE = re.compile(
    r"^(FPGA> OSS FPGA OK\n)?FOGCAST_FPGA_DEV_RESULT "
    r"run_id=([0-9a-f]{32}) primary=([a-z_]+)\n$"
)


def parse_run_stdout(output: str) -> RunFraming:
    match = RUN_RESULT_RE.fullmatch(output)
    if match is None:
        raise TransportError("nonempty run stdout is not one exact result framing")
    success_line, run_id, primary = match.groups()
    if primary not in RESULT_CODES:
        raise TransportError("run stdout primary code is outside the closed result schema")
    if (primary == "ok") != (success_line == "FPGA> OSS FPGA OK\n"):
        raise TransportError("run stdout success payload contradicts its primary code")
    return RunFraming(run_id, primary)


def require_fresh_recovery(
    development_session: str,
    development_generation: int,
    ready: ReadyRecord,
) -> None:
    if ready.session == development_session:
        raise TransportError("recovered normal owner did not allocate a fresh recovery session")
    if ready.generation <= development_generation:
        raise TransportError("recovered normal owner lacks a strictly higher recovery generation")


INSPECT_RE = re.compile(
    r"^FOGCAST_FPGA_DEV_INSPECT run_id=([0-9a-f]{32}) session=([0-9a-f]{32}) "
    r"generation=([1-9][0-9]*) phase=load_attempted pid=([1-9][0-9]*) "
    r"start_time=([1-9][0-9]*) executable_sha256=([0-9a-f]{64})\n$"
)


def parse_inspection(output: str, run_id: str, expected_tool_sha256: str) -> Inspection:
    match = INSPECT_RE.fullmatch(output)
    if match is None:
        raise TransportError("fault inspect response is not the exact load_attempted grammar")
    observed_run, session, generation, pid, start_time, digest = match.groups()
    if observed_run != _require_run_id(run_id) or digest != expected_tool_sha256:
        raise TransportError("fault inspect identity does not match the armed run/executable")
    return Inspection(observed_run, session, int(generation), "load_attempted", int(pid), int(start_time), digest)


def parse_fault_kill(output: str, run_id: str) -> None:
    if output != f"FOGCAST_FPGA_DEV_FAULT_KILLED run_id={_require_run_id(run_id)}\n":
        raise TransportError("fault-kill response is not exact")


def _parse_preflight(completed: Any) -> None:
    if completed.returncode != 0 or completed.stdout != "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n" or completed.stderr != "":
        raise TransportError("preflight response is not the exact success framing")


def _parse_recovery_preflight(completed: Any) -> None:
    if (
        completed.returncode == 0
        and completed.stdout == "FOGCAST_FPGA_DEV_PREFLIGHT code=ok\n"
        and completed.stderr == ""
    ):
        return
    if (
        completed.returncode == 2
        and completed.stdout == ""
        and completed.stderr == "FOGCAST_FPGA_DEV_PREFLIGHT code=result_conflict\n"
    ):
        return
    raise TransportError(
        "recovery preflight response is neither exact success nor the retained-result conflict"
    )


def _parse_dir(output: str) -> None:
    if output != "DIR|directory|0|0|700|2\n":
        raise TransportError("remote stage directory metadata is unsafe")


def _parse_stage(output: str, bundle: Bundle) -> None:
    lines = output.splitlines()
    if len(lines) != 6 or lines[0] != "FOGCAST_MISTEROSS_STAGE_V1" or lines[1] != "DIR|directory|0|0|700|2":
        raise TransportError("remote stage response is incomplete or ambiguous")
    for line, name in zip(lines[2:], MEMBER_NAMES, strict=True):
        fields = line.split("|")
        expected = ["FILE", name, "regular file", "0", "0", "600", "1", str(bundle.member_sizes[name]), bundle.member_sha256[name]]
        if fields != expected:
            raise TransportError(f"remote staged {name} no-follow metadata/hash is unsafe")


def _parse_result_probe(output: str) -> tuple[int, str]:
    lines = output.splitlines()
    if len(lines) != 1:
        raise TransportError("remote result metadata is missing or ambiguous")
    fields = lines[0].split("|")
    if len(fields) != 8 or fields[:6] != ["RESULT", "regular file", "0", "0", "600", "1"]:
        raise TransportError("remote result no-follow metadata is unsafe")
    try:
        size = int(fields[6], 10)
    except ValueError as exc:
        raise TransportError("remote result size is malformed") from exc
    if str(size) != fields[6] or not 0 < size <= MAX_RESULT_BYTES or SHA256_RE.fullmatch(fields[7]) is None:
        raise TransportError("result_unavailable: remote result identity is unsafe")
    return size, fields[7]


def _metadata_helper() -> str:
    # MiSTer's BusyBox has no stat applet.  The fixed awk parser consumes only
    # inode/mode/link/uid/gid columns and never a pathname field.
    return r'''misteross_meta() {
  [ "$#" -eq 1 ] || return 1
  LC_ALL=C busybox ls -din "$1" | LC_ALL=C busybox awk '
  function b(c,x) { return c==x ? 1 : c=="-" ? 0 : -8 }
  { if (NR!=1 || NF<6 || length($2)!=10) exit 1
    t=substr($2,1,1); k=t=="-"?"regular file":t=="d"?"directory":t=="l"?"symbolic link":t=="p"?"fifo":"other"
    u=b(substr($2,2,1),"r")*4+b(substr($2,3,1),"w")*2+b(substr($2,4,1),"x")
    g=b(substr($2,5,1),"r")*4+b(substr($2,6,1),"w")*2+b(substr($2,7,1),"x")
    o=b(substr($2,8,1),"r")*4+b(substr($2,9,1),"w")*2+b(substr($2,10,1),"x")
    if (u<0 || g<0 || o<0 || $3!~/^[0-9]+$/ || $4!~/^[0-9]+$/ || $5!~/^[0-9]+$/) exit 1
    printf "%s|%s|%s|%o|%s\n",k,$4,$5,u*64+g*8+o,$3 }
  END { if (NR!=1) exit 1 }'
}
'''


def _attestation_script() -> str:
    return _metadata_helper() + r''': FOGCAST_MISTEROSS_ATTEST_V1
set -eu
[ "$(command -v mister-fpga-dev)" = /usr/bin/mister-fpga-dev ]
[ ! -L /usr/bin/mister-fpga-dev ]
[ ! -L /media/fat/MiSTer ]
tool_meta=$(misteross_meta /usr/bin/mister-fpga-dev)
main_meta=$(misteross_meta /media/fat/MiSTer)
printf 'FOGCAST_MISTEROSS_ATTEST_V1\n'
printf 'BOARD|misterpi\n'
printf 'TOOL|/usr/bin/mister-fpga-dev|%s|%s\n' "$(sha256sum /usr/bin/mister-fpga-dev | busybox awk 'NF==2 {print $1; exit}')" "$tool_meta"
printf 'MAIN|/media/fat/MiSTer|%s|%s\n' "$(sha256sum /media/fat/MiSTer | busybox awk 'NF==2 {print $1; exit}')" "$main_meta"
'''


def _mkdir_script(run_id: str) -> str:
    stage = shlex.quote(remote_stage(run_id))
    return _metadata_helper() + f": FOGCAST_MISTEROSS_MKDIR_V1\nset -eu\numask 077\nmkdir -m 0700 {stage}\n[ ! -L {stage} ]\nprintf 'DIR|%s\\n' \"$(misteross_meta {stage})\"\n"


def _verify_script(run_id: str) -> str:
    stage = shlex.quote(remote_stage(run_id))
    members = " ".join(shlex.quote(name) for name in MEMBER_NAMES)
    return _metadata_helper() + f''': FOGCAST_MISTEROSS_VERIFY_V1
set -eu
[ ! -L {stage} ]
[ -d {stage} ]
cd {stage}
set -- {members}
for name do [ ! -L "$name" ] && [ -f "$name" ]; chmod 0600 "$name"; done
printf 'FOGCAST_MISTEROSS_STAGE_V1\n'
printf 'DIR|%s\n' "$(misteross_meta {stage})"
for name do printf 'FILE|%s|%s|%s|%s\n' "$name" "$(misteross_meta "$name")" "$(wc -c < "$name")" "$(sha256sum "$name" | busybox awk 'NF==2 {{print $1; exit}}')"; done
'''


def _cleanup_script(run_id: str) -> str:
    stage = shlex.quote(remote_stage(run_id))
    return f": FOGCAST_MISTEROSS_CLEANUP_V1\nset -eu\n[ ! -L {stage} ]\n[ -d {stage} ]\nrm -rf -- {stage}\n"


def _readiness_script() -> str:
    path = "/run/fogcast/fpgadev-ready-v3.json"
    return _attestation_script() + _metadata_helper() + f''': FOGCAST_MISTEROSS_READINESS_V3
set -eu
[ ! -L {path} ]
[ -f {path} ]
ready_meta=$(misteross_meta {path})
ready_size=$(wc -c < {path})
ready_hash=$(sha256sum {path} | busybox awk 'NF==2 {{print $1; exit}}')
ready_hex=$(busybox od -An -tx1 {path} | busybox tr -d ' \n')
printf 'FOGCAST_MISTEROSS_READY_V3\n'
printf 'READY|%s|%s|%s\n' "$ready_meta" "$ready_size" "$ready_hash"
printf 'RECORD|%s\n' "$ready_hex"
'''


def _reconnect_script() -> str:
    return ": FOGCAST_MISTEROSS_RECONNECT_V1\nset -eu\nprintf 'FOGCAST_MISTEROSS_RECONNECT_V1\\n'\n"


def _live_readiness_script(ready: ReadyRecord) -> str:
    processes = (
        ("supervisor", ready.supervisor_pid),
        ("main", ready.main_pid),
        ("agent", ready.agent_pid),
    )
    process_lines = []
    for name, pid in processes:
        process_lines.append(
            f"{name}_start=$(proc_start /proc/{pid}/stat {pid})\n"
            f"printf 'PROC|{name}|{pid}|%s\\n' \"${{{name}_start}}\"\n"
        )
    return _metadata_helper() + r''': FOGCAST_MISTEROSS_LIVE_V1
set -eu
proc_start() {
  [ "$#" -eq 2 ]
  [ ! -L "$1" ]
  [ -f "$1" ]
  LC_ALL=C busybox awk -v expected="$2" '
  { suffix=index($0,") "); if (NR!=1 || suffix==0) exit 1
    if (substr($0,1,index($0," ")-1)!=expected) exit 1
    rest=substr($0,suffix+2); count=split(rest,fields," ")
    if (count<20 || fields[20]!~/^[1-9][0-9]*$/) exit 1
    print fields[20] }
  END { if (NR!=1) exit 1 }' "$1"
}
boot=/proc/sys/kernel/random/boot_id
journal=/var/lib/fogcast/fpgadev-install-v1.json
profile=/etc/fogcast/agent.toml
owner=/var/lib/fogcast/hardware-owner-v1.json
fifo=/dev/MiSTer_cmd
fpga=/sys/class/fpga_manager/fpga0/state
core=''' + CORE_NAME_FILE + r'''
for path in "$boot" "$journal" "$profile" "$owner" "$fifo" "$fpga" "$core"; do [ ! -L "$path" ]; done
for path in "$boot" "$journal" "$profile" "$owner" "$fpga" "$core"; do [ -f "$path" ]; done
boot_hex=$(busybox od -An -tx1 "$boot" | busybox tr -d ' \n')
journal_meta=$(misteross_meta "$journal")
profile_meta=$(misteross_meta "$profile")
owner_meta=$(misteross_meta "$owner")
journal_hash=$(sha256sum "$journal" | busybox awk 'NF==2 {print $1; exit}')
profile_hash=$(sha256sum "$profile" | busybox awk 'NF==2 {print $1; exit}')
owner_size=$(wc -c < "$owner")
owner_hash=$(sha256sum "$owner" | busybox awk 'NF==2 {print $1; exit}')
owner_hex=$(busybox od -An -tx1 "$owner" | busybox tr -d ' \n')
fifo_meta=$(misteross_meta "$fifo")
fpga_hex=$(busybox od -An -tx1 "$fpga" | busybox tr -d ' \n')
menu_first=$(busybox od -An -tx1 "$core" | busybox tr -d ' \n')
busybox sleep 0.01
menu_second=$(busybox od -An -tx1 "$core" | busybox tr -d ' \n')
printf 'FOGCAST_MISTEROSS_LIVE_V1\n'
printf 'BOOT|%s\n' "$boot_hex"
printf 'JOURNAL|%s|%s\n' "$journal_meta" "$journal_hash"
printf 'PROFILE|%s|%s\n' "$profile_meta" "$profile_hash"
printf 'OWNER|%s|%s|%s\n' "$owner_meta" "$owner_size" "$owner_hash"
printf 'OWNER_RECORD|%s\n' "$owner_hex"
''' + "".join(process_lines) + r'''printf 'FIFO|%s\n' "$fifo_meta"
printf 'FPGA|%s\n' "$fpga_hex"
printf 'MENU_FIRST|%s\n' "$menu_first"
printf 'MENU_SECOND|%s\n' "$menu_second"
'''


def _result_path(run_id: str) -> str:
    return f"/var/lib/fogcast/fpga-dev/results/result-{_require_run_id(run_id)}.json"


def _result_probe_script(run_id: str) -> str:
    path = shlex.quote(_result_path(run_id))
    return _metadata_helper() + f''': FOGCAST_MISTEROSS_RESULT_V1
set -eu
[ ! -L {path} ]
[ -f {path} ]
printf 'RESULT|%s|%s|%s\n' "$(misteross_meta {path})" "$(wc -c < {path})" "$(sha256sum {path} | busybox awk 'NF==2 {{print $1; exit}}')"
'''


def _result_delete_script(run_id: str) -> str:
    path = shlex.quote(_result_path(run_id))
    return f": FOGCAST_MISTEROSS_RESULT_DELETE_V1\nset -eu\n[ ! -L {path} ]\n[ -f {path} ]\nrm -- {path}\n"


def _fault_recovery_script(run_id: str) -> str:
    checked_run_id = _require_run_id(run_id)
    socket_digest = hashlib.sha256(checked_run_id.encode()).hexdigest()
    paths = (
        remote_stage(checked_run_id),
        _result_path(checked_run_id),
        f"/run/fogcast/fpga-dev-{checked_run_id}.diagnostic",
        f"/run/fogcast/fpga-dev-{checked_run_id}.armed",
        f"/run/fogcast/f-{socket_digest}.sock",
    )
    quoted = " ".join(shlex.quote(path) for path in paths)
    return (
        ": FOGCAST_MISTEROSS_FAULT_RECOVERY_V1\n"
        "set -eu\n"
        f"for path in {quoted}; do [ ! -e \"$path\" ]; [ ! -L \"$path\" ]; done\n"
        "printf 'FOGCAST_MISTEROSS_FAULT_RECOVERY_V1\\n'\n"
    )


def _cleanup_options(options: Sequence[str]) -> tuple[str, ...]:
    cleaned: list[str] = ["-q", "-T", "-o", "BatchMode=yes"]
    index = 0
    while index < len(options):
        if options[index] in ("-q", "-T"):
            index += 1
            continue
        if options[index] == "-o" and index + 1 < len(options):
            setting = options[index + 1]
            index += 2
            if setting.lower().startswith("batchmode="):
                continue
            cleaned.extend(("-o", setting))
            continue
        cleaned.append(options[index])
        index += 1
    return tuple(cleaned)


class _ControlSession:
    def __init__(self, transport: "Transport"):
        self.transport = transport
        self.directory = Path(tempfile.mkdtemp(prefix="misteross-fogcast-ssh-"))
        os.chmod(self.directory, 0o700)
        self.control_path = self.directory / "control-%C"
        self.options = (*SSH_OPTIONS, "-o", "ControlMaster=auto", "-o", "ControlPersist=120s", "-o", f"ControlPath={self.control_path}")

    def ssh(self, command: str) -> list[str]:
        return [self.transport.config.ssh, *self.options, self.transport.target, command]

    def scp(self, sources: Sequence[str], destination: str) -> list[str]:
        settings = self.options[2:]  # SCP owns its own -q/-O and does not accept SSH -T.
        return [self.transport.config.scp, *SCP_OPTIONS[:2], *settings, *sources, destination]

    def close(self, *, deadline: float | None = None) -> None:
        try:
            entries = list(self.directory.iterdir())
        except FileNotFoundError:
            return
        if not entries:
            self.directory.rmdir()
            return
        cleanup = _cleanup_options(self.options)
        timeout = CONTROL_TIMEOUT
        if deadline is not None:
            timeout = self.transport._deadline_budget(deadline, CONTROL_TIMEOUT, "reconnect 120-second bound")
        primary = self.transport._invoke(
            [self.transport.config.ssh, *cleanup, "-O", "exit", self.transport.target],
            timeout=timeout,
        )
        wait_budget = CONTROL_WAIT_TIMEOUT
        if deadline is not None:
            wait_budget = self.transport._deadline_budget(deadline, CONTROL_WAIT_TIMEOUT, "reconnect 120-second bound")
        wait_deadline = time.monotonic() + wait_budget
        while primary.returncode == 0 and time.monotonic() < wait_deadline:
            if not any(self.directory.iterdir()):
                self.directory.rmdir()
                return
            time.sleep(0.02)
        timeout = CONTROL_TIMEOUT
        if deadline is not None:
            timeout = self.transport._deadline_budget(deadline, CONTROL_TIMEOUT, "reconnect 120-second bound")
        stopped = self.transport._invoke(
            [self.transport.config.ssh, *cleanup, "-O", "stop", self.transport.target],
            timeout=timeout,
        )
        if stopped.returncode == 0:
            timeout = CONTROL_TIMEOUT
            if deadline is not None:
                timeout = self.transport._deadline_budget(deadline, CONTROL_TIMEOUT, "reconnect 120-second bound")
            retried = self.transport._invoke(
                [self.transport.config.ssh, *cleanup, "-O", "exit", self.transport.target],
                timeout=timeout,
            )
            wait_budget = CONTROL_WAIT_TIMEOUT
            if deadline is not None:
                wait_budget = self.transport._deadline_budget(deadline, CONTROL_WAIT_TIMEOUT, "reconnect 120-second bound")
            wait_deadline = time.monotonic() + wait_budget
            while (retried.returncode == 0 or not any(self.directory.iterdir())) and time.monotonic() < wait_deadline:
                if not any(self.directory.iterdir()):
                    self.directory.rmdir()
                    return
                time.sleep(0.02)
        raise TransportError(f"SSH control cleanup failed; preserve {self.directory}")


def _combine(primary: BaseException | None, cleanup: BaseException | None) -> TransportError | None:
    if primary is None and cleanup is None:
        return None
    if primary is None:
        return TransportError(str(cleanup))
    if cleanup is None:
        return primary if isinstance(primary, TransportError) else TransportError(str(primary))
    return TransportError(f"primary transport error: {primary}; cleanup error: {cleanup}")


def _durable_write(path: Path, raw: bytes) -> None:
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    fd = os.open(path, flags, 0o600)
    try:
        view = memoryview(raw)
        while view:
            written = os.write(fd, view)
            if written <= 0:
                raise OSError("short durable write")
            view = view[written:]
        os.fsync(fd)
    finally:
        os.close(fd)


def _fsync_directory(path: Path) -> None:
    fd = os.open(path, os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_DIRECTORY", 0))
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _fsync_existing_file(path: Path) -> None:
    try:
        fd = os.open(path, os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0))
    except FileNotFoundError:
        return
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


class Transport:
    def __init__(self, config: DevConfig):
        self.config = config
        self.target = _validate_target(config.user, config.host)
        _validate_executable_token(config.ssh, "SSH")
        _validate_executable_token(config.scp, "SCP")
        if config.expected_board != BOARD or SHA256_RE.fullmatch(config.expected_main_sha256 or "") is None or SHA256_RE.fullmatch(config.expected_tool_sha256 or "") is None:
            raise TransportError("expected board/Main/tool attestation is incomplete")
        self.last_readiness_budget = READINESS_TIMEOUT

    @staticmethod
    def _manifest_identity(path: Path) -> tuple[str, str]:
        raw, _ = _read_private_file(path / "manifest.json", "manifest.json", 4096)
        value = _canonical_object(raw, MANIFEST_KEYS, "manifest", 4096)
        return _exact_text(value["run_id"], "manifest run_id"), _exact_text(value["build_lane"], "manifest build_lane")

    @classmethod
    def _manifest_run_id(cls, path: Path) -> str:
        return cls._manifest_identity(path)[0]

    @classmethod
    def _manifest_lane(cls, path: Path) -> str:
        return cls._manifest_identity(path)[1]

    def _prepare(self, path: Path | Bundle) -> tuple[Bundle, Path]:
        if isinstance(path, Bundle):
            bundle = path
        else:
            run_id, lane = self._manifest_identity(Path(path))
            bundle = load_bundle(Path(path), run_id, lane)
        trace = self.config.trace_root / bundle.run_id
        if self.config.dry_run:
            return bundle, trace
        root = self.config.trace_root
        if _path_contains_symlink(root):
            raise TransportError("trace root contains a symlink")
        try:
            root.mkdir(mode=0o700, parents=True, exist_ok=True)
        except OSError as exc:
            raise TransportError(f"cannot create trace root: {exc}") from exc
        info = root.lstat()
        if not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700 or info.st_uid != os.getuid():
            raise TransportError("trace root must be a private owned 0700 directory")
        try:
            trace.mkdir(mode=0o700)
        except FileExistsError as exc:
            raise TransportError(f"run trace collision for {bundle.run_id}") from exc
        _fsync_directory(root)
        return bundle, trace

    def _invoke(self, argv: Sequence[str], *, timeout: float) -> Any:
        try:
            result = self.config.run_command(
                list(argv), timeout=timeout, text=True, capture_output=True,
                check=False, env=os.environ.copy(),
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise TransportError(f"command failed or timed out: {exc}") from exc
        if not isinstance(result.returncode, int) or not isinstance(result.stdout, str) or not isinstance(result.stderr, str):
            raise TransportError("transport returned malformed command evidence")
        return result

    def _scp(self, argv: Sequence[str], *, timeout: float) -> Any:
        try:
            result = self.config.scp_command(
                list(argv), timeout=timeout, text=True, capture_output=True,
                check=False, env=os.environ.copy(),
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise TransportError(f"SCP failed or timed out: {exc}") from exc
        if not isinstance(result.returncode, int):
            raise TransportError("SCP returned malformed command evidence")
        return result

    def _attest(self, session: _ControlSession) -> Attestation:
        result = self._invoke(session.ssh(_attestation_script()), timeout=REMOTE_COMMAND_TIMEOUT)
        if result.returncode != 0 or result.stderr != "":
            raise TransportError("remote executable attestation command failed")
        return parse_attestation(
            result.stdout, expected_board=self.config.expected_board,
            expected_main_sha256=self.config.expected_main_sha256,
            expected_tool_sha256=self.config.expected_tool_sha256,
        )

    def _stage(self, bundle: Bundle, session: _ControlSession) -> None:
        self._attest(session)
        created = self._invoke(session.ssh(_mkdir_script(bundle.run_id)), timeout=REMOTE_COMMAND_TIMEOUT)
        if created.returncode != 0:
            raise TransportError("remote private stage collision or mkdir failure")
        try:
            if created.stderr != "":
                raise TransportError("remote private stage mkdir produced unexpected stderr")
            _parse_dir(created.stdout)
            destination = f"{self.target}:{remote_stage(bundle.run_id)}/"
            copied = self._scp(session.scp([str(path) for path in bundle.members], destination), timeout=REMOTE_COMMAND_TIMEOUT)
            if copied.returncode != 0 or getattr(copied, "stdout", "") != "" or getattr(copied, "stderr", "") != "":
                raise TransportError("staging the reviewed four-file bundle failed")
            verified = self._invoke(session.ssh(_verify_script(bundle.run_id)), timeout=REMOTE_COMMAND_TIMEOUT)
            if verified.returncode != 0 or verified.stderr != "":
                raise TransportError("remote no-follow stage verification failed")
            _parse_stage(verified.stdout, bundle)
        except BaseException as primary:
            cleanup: BaseException | None = None
            try:
                self._cleanup_stage(bundle, session)
            except BaseException as exc:
                cleanup = exc
            raise _combine(primary, cleanup) or TransportError("remote stage failed")

    def _cleanup_stage(self, bundle: Bundle, session: _ControlSession) -> None:
        result = self._invoke(session.ssh(_cleanup_script(bundle.run_id)), timeout=REMOTE_COMMAND_TIMEOUT)
        if result.returncode != 0 or result.stdout != "" or result.stderr != "":
            raise TransportError("remote stage cleanup failed")

    def _dry_plan(self, bundle: Bundle, action: str, trace: Path) -> tuple[tuple[str, ...], ...]:
        ssh = lambda command: tuple(ssh_argv(self.config.ssh, self.config.user, self.config.host, command))
        destination = f"{self.target}:{remote_stage(bundle.run_id)}/"
        scp_upload = tuple([self.config.scp, *SCP_OPTIONS, *[str(path) for path in bundle.members], destination])
        common = [ssh(_attestation_script()), ssh(_mkdir_script(bundle.run_id)), scp_upload, ssh(_verify_script(bundle.run_id))]
        if action == "preflight":
            common.extend((ssh(remote_preflight_command(bundle.run_id)), ssh(_cleanup_script(bundle.run_id))))
        elif action == "load":
            common.extend((
                ssh(remote_run_command(bundle.run_id)), ssh(_reconnect_script()),
                ssh(remote_preflight_command(bundle.run_id)), ssh(_readiness_script()),
                ssh(_result_probe_script(bundle.run_id)),
                tuple([self.config.scp, *SCP_OPTIONS, f"{self.target}:{_result_path(bundle.run_id)}", str(trace / "result.json")]),
                ssh(_result_delete_script(bundle.run_id)), ssh(remote_preflight_command(bundle.run_id)),
                ssh(_cleanup_script(bundle.run_id)),
            ))
        elif action == "fault":
            common.extend((
                ssh(remote_fault_command("fault-arm", bundle.run_id)), ssh(remote_run_command(bundle.run_id)),
                ssh(remote_fault_command("inspect", bundle.run_id)), ssh(remote_fault_command("fault-kill", bundle.run_id)),
                ssh(remote_fault_command("recovery-reboot", bundle.run_id)), ssh(_reconnect_script()),
                ssh(_attestation_script()), ssh(_readiness_script()),
                ssh(_fault_recovery_script(bundle.run_id)),
            ))
        else:
            raise TransportError("unknown dry-run action")
        return tuple(common)

    def preflight(self, bundle: Path | Bundle) -> PreflightReport:
        selected, trace = self._prepare(bundle)
        if self.config.dry_run:
            return PreflightReport(True, argv=self._dry_plan(selected, "preflight", trace))
        session = _ControlSession(self)
        staged = False
        primary: BaseException | None = None
        cleanup: BaseException | None = None
        try:
            self._stage(selected, session)
            staged = True
            result = self._invoke(session.ssh(remote_preflight_command(selected.run_id)), timeout=REMOTE_COMMAND_TIMEOUT)
            _parse_preflight(result)
        except BaseException as exc:
            primary = exc
        finally:
            if staged:
                try:
                    self._cleanup_stage(selected, session)
                except BaseException as exc:
                    cleanup = exc
            try:
                session.close()
            except BaseException as exc:
                cleanup = _combine(cleanup, exc)
        error = _combine(primary, cleanup)
        if error is not None:
            raise error
        _durable_write(trace / "preflight.json", (json.dumps({"schema": 1, "run_id": selected.run_id, "code": "ok"}, separators=(",", ":")) + "\n").encode())
        _fsync_directory(trace)
        return PreflightReport(True, trace)

    @staticmethod
    def _safe_remote_stream(value: str) -> bool:
        return len(value.encode("utf-8", errors="strict")) <= 4096 and all(character in "\n\r\t" or ord(character) >= 0x20 for character in value)

    def _classify_disconnect(self, result: Any, *, label: str, require_empty_stdout: bool) -> None:
        if result.returncode not in CLOSED_SSH_OUTCOMES:
            raise TransportError(f"{label} did not produce an expected closed SSH disconnect")
        if require_empty_stdout and result.stdout != "":
            raise TransportError(f"{label} produced unexpected stdout or result framing")
        if not self._safe_remote_stream(result.stdout) or not self._safe_remote_stream(result.stderr):
            raise TransportError(f"{label} produced unsafe or unbounded stream evidence")
        if result.stderr == RESULT_UNAVAILABLE_STDERR:
            raise TransportError("result_unavailable: target could not publish durable result")

    def _deadline_budget(self, deadline: float, maximum: float, label: str) -> float:
        remaining = deadline - self.config.clock()
        if remaining <= 0:
            raise TransportError(f"{label} exceeded its cumulative deadline")
        return min(maximum, remaining)

    def _resolve_before(self, deadline: float) -> str:
        outcome: list[tuple[bool, object]] = []
        done = threading.Event()

        def resolve() -> None:
            try:
                outcome.append((True, self.config.resolve_host(self.config.host)))
            except BaseException as exc:
                outcome.append((False, exc))
            finally:
                done.set()

        worker = threading.Thread(target=resolve, name="fogcast-dev-resolver", daemon=True)
        worker.start()
        budget = self._deadline_budget(deadline, RECONNECT_TIMEOUT, "reconnect 120-second bound")
        if not done.wait(budget):
            raise TransportError("host-key re-resolution exceeded the absolute 120-second reconnect deadline")
        if not outcome or not outcome[0][0]:
            failure = outcome[0][1] if outcome else "resolver returned no outcome"
            raise TransportError(f"host-key re-resolution failed: {failure}")
        if not isinstance(outcome[0][1], str) or not outcome[0][1]:
            raise TransportError("host-key re-resolution returned an invalid address")
        return outcome[0][1]

    def _reconnect(self, disconnected_at: float) -> tuple[_ControlSession, float]:
        deadline = disconnected_at + RECONNECT_TIMEOUT
        last_error: BaseException | None = None
        for _attempt in range(MAX_RECONNECT_ATTEMPTS):
            if self.config.clock() >= deadline:
                break
            try:
                self._resolve_before(deadline)
            except BaseException as exc:
                last_error = exc
                session = None
            else:
                if self.config.clock() >= deadline:
                    last_error = TransportError("reconnect exceeded the absolute 120-second deadline during host-key resolution")
                    break
                session = _ControlSession(self)
                try:
                    budget = self._deadline_budget(deadline, REMOTE_COMMAND_TIMEOUT, "reconnect")
                    result = self._invoke(session.ssh(_reconnect_script()), timeout=budget)
                    if self.config.clock() > deadline:
                        raise TransportError("reconnect exceeded the absolute 120-second deadline")
                    if result.returncode != 0 or result.stdout != "FOGCAST_MISTEROSS_RECONNECT_V1\n" or result.stderr != "":
                        raise TransportError("exact reconnect probe failed")
                    return session, self.config.clock()
                except BaseException as exc:
                    last_error = exc
                    try:
                        session.close(deadline=deadline)
                    except BaseException as cleanup:
                        last_error = _combine(last_error, cleanup)
            if self.config.clock() >= deadline:
                break
            self.config.sleep(min(1.0, max(0.0, deadline - self.config.clock())))
        raise TransportError(f"reconnect failed within the absolute 120-second bound: {last_error}")

    def _verify_readiness(
        self,
        selected: Bundle,
        session: _ControlSession,
        connected_at: float,
    ) -> ReadyRecord:
        deadline = connected_at + READINESS_TIMEOUT
        last_error: BaseException | None = None
        for _attempt in range(MAX_RECONNECT_ATTEMPTS):
            try:
                budget = self._deadline_budget(deadline, READINESS_TIMEOUT, "readiness 30-second bound")
                self.last_readiness_budget = budget
                preflight = self._invoke(
                    session.ssh(remote_preflight_command(selected.run_id)), timeout=budget
                )
                if self.config.clock() > deadline:
                    raise TransportError("readiness exceeded its cumulative 30-second deadline")
                _parse_recovery_preflight(preflight)

                budget = self._deadline_budget(deadline, READINESS_TIMEOUT, "readiness 30-second bound")
                result = self._invoke(session.ssh(_readiness_script()), timeout=budget)
                if self.config.clock() > deadline:
                    raise TransportError("readiness exceeded its cumulative 30-second deadline")
                if result.returncode != 0 or result.stderr != "":
                    raise TransportError("exact readiness record command failed")
                ready = parse_readiness(result.stdout, self.config)

                budget = self._deadline_budget(deadline, READINESS_TIMEOUT, "readiness 30-second bound")
                live = self._invoke(session.ssh(_live_readiness_script(ready)), timeout=budget)
                if self.config.clock() > deadline:
                    raise TransportError("readiness exceeded its cumulative 30-second deadline")
                if live.returncode != 0 or live.stderr != "":
                    raise TransportError("exact live readiness command failed")
                parse_live_readiness(live.stdout, ready)
                return ready
            except BaseException as exc:
                last_error = exc
            if self.config.clock() >= deadline:
                break
            self.config.sleep(min(1.0, max(0.0, deadline - self.config.clock())))
        raise TransportError(f"readiness failed within the cumulative 30-second bound: {last_error}")

    def _verify_fault_recovery(
        self,
        selected: Bundle,
        session: _ControlSession,
        connected_at: float,
        inspection: Inspection,
    ) -> ReadyRecord:
        deadline = connected_at + READINESS_TIMEOUT
        last_error: BaseException | None = None
        for _attempt in range(MAX_RECONNECT_ATTEMPTS):
            try:
                budget = self._deadline_budget(
                    deadline, READINESS_TIMEOUT, "readiness 30-second bound"
                )
                attestation = self._invoke(
                    session.ssh(_attestation_script()), timeout=budget
                )
                if self.config.clock() > deadline:
                    raise TransportError(
                        "readiness exceeded its cumulative 30-second deadline"
                    )
                if attestation.returncode != 0 or attestation.stderr != "":
                    raise TransportError("exact post-reboot attestation command failed")
                parse_attestation(
                    attestation.stdout,
                    expected_board=self.config.expected_board,
                    expected_main_sha256=self.config.expected_main_sha256,
                    expected_tool_sha256=self.config.expected_tool_sha256,
                )

                budget = self._deadline_budget(
                    deadline, READINESS_TIMEOUT, "readiness 30-second bound"
                )
                result = self._invoke(session.ssh(_readiness_script()), timeout=budget)
                if self.config.clock() > deadline:
                    raise TransportError(
                        "readiness exceeded its cumulative 30-second deadline"
                    )
                if result.returncode != 0 or result.stderr != "":
                    raise TransportError("exact readiness record command failed")
                ready = parse_readiness(result.stdout, self.config)

                budget = self._deadline_budget(
                    deadline, READINESS_TIMEOUT, "readiness 30-second bound"
                )
                live = self._invoke(
                    session.ssh(_live_readiness_script(ready)), timeout=budget
                )
                if self.config.clock() > deadline:
                    raise TransportError(
                        "readiness exceeded its cumulative 30-second deadline"
                    )
                if live.returncode != 0 or live.stderr != "":
                    raise TransportError("exact live readiness command failed")
                parse_live_readiness(live.stdout, ready)
                require_fresh_recovery(
                    inspection.session, inspection.generation, ready
                )

                budget = self._deadline_budget(
                    deadline, READINESS_TIMEOUT, "readiness 30-second bound"
                )
                cleanup = self._invoke(
                    session.ssh(_fault_recovery_script(selected.run_id)),
                    timeout=budget,
                )
                if self.config.clock() > deadline:
                    raise TransportError(
                        "readiness exceeded its cumulative 30-second deadline"
                    )
                if (
                    cleanup.returncode != 0
                    or cleanup.stdout != "FOGCAST_MISTEROSS_FAULT_RECOVERY_V1\n"
                    or cleanup.stderr != ""
                ):
                    raise TransportError(
                        "fault recovery retained a result, stage, diagnostic, hook, or socket"
                    )
                return ready
            except BaseException as exc:
                last_error = exc
            if self.config.clock() >= deadline:
                break
            self.config.sleep(min(1.0, max(0.0, deadline - self.config.clock())))
        raise TransportError(
            "fault recovery readiness failed within the cumulative "
            f"30-second bound: {last_error}"
        )

    def _wait_fault_recovery(
        self,
        selected: Bundle,
        disconnected_at: float,
        inspection: Inspection,
    ) -> tuple[ReadyRecord, _ControlSession]:
        session, connected_at = self._reconnect(disconnected_at)
        try:
            return (
                self._verify_fault_recovery(
                    selected, session, connected_at, inspection
                ),
                session,
            )
        except BaseException as primary:
            cleanup: BaseException | None = None
            try:
                session.close()
            except BaseException as exc:
                cleanup = exc
            raise _combine(primary, cleanup) or TransportError("readiness failed")

    def _retrieve_result_bytes(
        self,
        selected: Bundle,
        trace: Path,
        session: _ControlSession,
        deadline: float,
    ) -> bytes:
        first = self._invoke(
            session.ssh(_result_probe_script(selected.run_id)),
            timeout=self._deadline_budget(deadline, REMOTE_COMMAND_TIMEOUT, "readiness 30-second bound"),
        )
        if first.returncode != 0 or first.stderr != "":
            raise TransportError("result_unavailable: protected target result is missing")
        size, digest = _parse_result_probe(first.stdout)
        temporary = trace / ".result.json.partial"
        destination = str(temporary)
        copied = self._scp(
            session.scp([f"{self.target}:{_result_path(selected.run_id)}"], destination),
            timeout=self._deadline_budget(deadline, REMOTE_COMMAND_TIMEOUT, "readiness 30-second bound"),
        )
        if copied.returncode != 0:
            raise TransportError("result_unavailable: SCP retrieval failed")
        try:
            os.chmod(temporary, 0o600, follow_symlinks=False)
        except (OSError, NotImplementedError) as exc:
            raise TransportError(f"result_unavailable: cannot privatize host copy: {exc}") from exc
        raw, _ = _read_private_file(temporary, "retrieved result", MAX_RESULT_BYTES)
        if len(raw) != size or hashlib.sha256(raw).hexdigest() != digest:
            raise TransportError("result_unavailable: retrieved bytes do not match target metadata")
        fd = os.open(temporary, os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0))
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
        final = trace / "result.json"
        os.replace(temporary, final)
        _fsync_directory(trace)
        second = self._invoke(
            session.ssh(_result_probe_script(selected.run_id)),
            timeout=self._deadline_budget(deadline, REMOTE_COMMAND_TIMEOUT, "readiness 30-second bound"),
        )
        if second.returncode != 0 or second.stderr != "" or _parse_result_probe(second.stdout) != (size, digest):
            raise TransportError("target result changed before durable deletion")
        deleted = self._invoke(
            session.ssh(_result_delete_script(selected.run_id)),
            timeout=self._deadline_budget(deadline, REMOTE_COMMAND_TIMEOUT, "readiness 30-second bound"),
        )
        if deleted.returncode != 0 or deleted.stdout != "" or deleted.stderr != "":
            raise TransportError("durable result copied but target result deletion failed")
        return raw

    def load(self, bundle: Path | Bundle) -> LoadReport:
        selected, trace = self._prepare(bundle)
        if self.config.dry_run:
            return LoadReport(True, argv=self._dry_plan(selected, "load", trace))
        first = _ControlSession(self)
        errors: list[BaseException] = []
        staged = False
        disconnected_at: float | None = None
        run_framing: RunFraming | None = None
        try:
            self._stage(selected, first)
            staged = True
            run = self._invoke(first.ssh(remote_run_command(selected.run_id)), timeout=RUN_TIMEOUT)
            disconnected_at = self.config.clock()
            if run.returncode == 2 and run.stdout == "" and run.stderr == RESULT_UNAVAILABLE_STDERR:
                errors.append(TransportError("result_unavailable: target reported state_store_failed"))
            else:
                try:
                    self._classify_disconnect(run, label="load", require_empty_stdout=False)
                except BaseException as exc:
                    errors.append(exc)
                if run.stdout != "":
                    try:
                        run_framing = parse_run_stdout(run.stdout)
                    except BaseException as exc:
                        errors.append(exc)
            _durable_write(trace / "run.stdout", run.stdout.encode())
            _durable_write(trace / "run.stderr", run.stderr.encode())
        except BaseException as exc:
            if staged and disconnected_at is None:
                disconnected_at = self.config.clock()
            errors.append(exc)
        try:
            first.close(
                deadline=(disconnected_at + RECONNECT_TIMEOUT) if disconnected_at is not None else None
            )
        except BaseException as exc:
            errors.append(exc)

        ready: ReadyRecord | None = None
        recovery: _ControlSession | None = None
        result_raw: bytes | None = None
        connected_at: float | None = None
        if staged and disconnected_at is not None:
            try:
                recovery, connected_at = self._reconnect(disconnected_at)
            except BaseException as exc:
                errors.append(exc)
        if recovery is not None and connected_at is not None:
            readiness_deadline = connected_at + READINESS_TIMEOUT
            try:
                ready = self._verify_readiness(selected, recovery, connected_at)
                _durable_write(trace / "ready.json", ready.raw)
            except BaseException as exc:
                errors.append(exc)
            if ready is not None:
                try:
                    result_raw = self._retrieve_result_bytes(
                        selected, trace, recovery, readiness_deadline
                    )
                except BaseException as exc:
                    errors.append(exc)
            if result_raw is not None:
                try:
                    budget = self._deadline_budget(
                        readiness_deadline,
                        REMOTE_COMMAND_TIMEOUT,
                        "readiness 30-second bound",
                    )
                    preflight = self._invoke(
                        recovery.ssh(remote_preflight_command(selected.run_id)),
                        timeout=budget,
                    )
                    if self.config.clock() > readiness_deadline:
                        raise TransportError(
                            "readiness exceeded its cumulative 30-second deadline"
                        )
                    _parse_preflight(preflight)
                except BaseException as exc:
                    errors.append(exc)
        result_record: ResultRecord | None = None
        if ready is not None and result_raw is not None:
            try:
                result_record = parse_result(result_raw, selected)
                require_fresh_recovery(
                    result_record.session, result_record.generation, ready
                )
                if run_framing is not None and (
                    run_framing.run_id != result_record.run_id
                    or run_framing.primary_code != result_record.primary_code
                ):
                    raise TransportError(
                        "run stdout framing contradicts the persisted result"
                    )
                if result_record.primary_code != "ok":
                    raise TransportError(f"target result primary_code={result_record.primary_code}")
            except BaseException as exc:
                errors.append(exc)
        if recovery is not None:
            try:
                self._cleanup_stage(selected, recovery)
            except BaseException as exc:
                errors.append(exc)
            try:
                recovery.close()
            except BaseException as exc:
                errors.append(exc)
        if errors:
            raise TransportError("; ".join(str(error) for error in errors))
        if result_record is None or ready is None:
            raise TransportError("result_unavailable: successful recovery lacks bound result/readiness")
        _fsync_directory(trace)
        return LoadReport(True, result_record, ready, trace)

    def _terminate_and_reap(self, process: Any) -> None:
        try:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5.0)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5.0)
            else:
                process.wait(timeout=0)
        except BaseException as exc:
            raise TransportError(f"cannot terminate/reap owned SSH child: {exc}") from exc

    def fault_inject(self, bundle: Path | Bundle) -> FaultReport:
        selected, trace = self._prepare(bundle)
        if self.config.dry_run:
            return FaultReport(True, argv=self._dry_plan(selected, "fault", trace))
        session = _ControlSession(self)
        process: Any | None = None
        reaped = False
        inspection: Inspection | None = None
        inspect_output = ""
        kill_output = ""
        stdout_path = trace / "run.stdout"
        stderr_path = trace / "run.stderr"
        error: BaseException | None = None
        reboot_disconnected_at: float | None = None
        try:
            self._stage(selected, session)
            armed = self._invoke(session.ssh(remote_fault_command("fault-arm", selected.run_id)), timeout=REMOTE_COMMAND_TIMEOUT)
            if armed.returncode != 0 or armed.stdout != "" or armed.stderr != "":
                raise TransportError("fault-arm did not succeed exactly")
            stdout_fd = os.open(stdout_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0), 0o600)
            stderr_fd = os.open(stderr_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_CLOEXEC", 0), 0o600)
            with os.fdopen(stdout_fd, "wb", closefd=True) as stdout_file, os.fdopen(stderr_fd, "wb", closefd=True) as stderr_file:
                try:
                    process = self.config.popen_factory(
                        session.ssh(remote_run_command(selected.run_id)),
                        stdin=subprocess.DEVNULL, stdout=stdout_file, stderr=stderr_file,
                        env=os.environ.copy(), close_fds=True,
                    )
                except OSError as exc:
                    raise TransportError(f"cannot start owned SSH run child: {exc}") from exc
                deadline = self.config.clock() + FAULT_INSPECT_TIMEOUT
                attempts: list[str] = []
                last_inspect: BaseException | None = None
                while self.config.clock() < deadline:
                    if process.poll() is not None:
                        raise TransportError("premature run disconnect before exact fault checkpoint")
                    inspect_budget = self._deadline_budget(
                        deadline,
                        FAULT_INSPECT_TIMEOUT,
                        "fault inspect 10-second bound",
                    )
                    observed = self._invoke(
                        session.ssh(remote_fault_command("inspect", selected.run_id)),
                        timeout=inspect_budget,
                    )
                    attempts.append(observed.stdout)
                    if self.config.clock() >= deadline:
                        last_inspect = TransportError(
                            "inspect response arrived outside the absolute 10-second window"
                        )
                        break
                    if observed.returncode == 0 and observed.stderr == "":
                        try:
                            inspection = parse_inspection(observed.stdout, selected.run_id, self.config.expected_tool_sha256)
                            inspect_output = observed.stdout
                            break
                        except BaseException as exc:
                            last_inspect = exc
                    else:
                        last_inspect = TransportError("inspect command failed")
                    self.config.sleep(
                        min(0.05, max(0.0, deadline - self.config.clock()))
                    )
                _durable_write(trace / "inspect-attempts.log", "".join(attempts).encode())
                if inspection is None:
                    raise TransportError(f"exact bound load_attempted inspect was not observed: {last_inspect}")
                killed = self._invoke(session.ssh(remote_fault_command("fault-kill", selected.run_id)), timeout=REMOTE_COMMAND_TIMEOUT)
                if killed.returncode != 0 or killed.stderr != "":
                    raise TransportError("fault-kill command failed")
                parse_fault_kill(killed.stdout, selected.run_id)
                kill_output = killed.stdout
                _durable_write(trace / "inspect.log", inspect_output.encode())
                _durable_write(trace / "fault-kill.log", kill_output.encode())
                try:
                    returncode = process.wait(timeout=FAULT_CHILD_TIMEOUT)
                    reaped = True
                except subprocess.TimeoutExpired as exc:
                    raise TransportError("owned run SSH child did not exit after fault-kill") from exc
                stdout_file.flush()
                stderr_file.flush()
                os.fsync(stdout_file.fileno())
                os.fsync(stderr_file.fileno())
            run_stdout = stdout_path.read_bytes()
            run_stderr = stderr_path.read_bytes()
            if run_stdout != b"" or run_stderr != b"":
                raise TransportError("faulted run child emitted unexpected output or result framing")
            if returncode != 255:
                raise TransportError("faulted run child exit must be exactly 255")
            reboot = self._invoke(session.ssh(remote_fault_command("recovery-reboot", selected.run_id)), timeout=RUN_TIMEOUT)
            reboot_disconnected_at = self.config.clock()
            _durable_write(trace / "reboot.stdout", reboot.stdout.encode())
            _durable_write(trace / "reboot.stderr", reboot.stderr.encode())
            _durable_write(
                trace / "reboot.json",
                (json.dumps({"schema": 1, "run_id": selected.run_id, "ssh_returncode": reboot.returncode}, separators=(",", ":")) + "\n").encode(),
            )
            self._classify_disconnect(reboot, label="recovery reboot", require_empty_stdout=True)
        except BaseException as exc:
            error = exc
            returncode = None
            reboot = None
        finally:
            if process is not None and not reaped:
                try:
                    self._terminate_and_reap(process)
                except BaseException as cleanup:
                    error = _combine(error, cleanup)
            try:
                _fsync_existing_file(stdout_path)
                _fsync_existing_file(stderr_path)
                _fsync_directory(trace)
            except BaseException as cleanup:
                error = _combine(error, cleanup)
            try:
                session.close(
                    deadline=(reboot_disconnected_at + RECONNECT_TIMEOUT)
                    if reboot_disconnected_at is not None else None
                )
            except BaseException as cleanup:
                error = _combine(error, cleanup)
        if error is not None:
            raise error if isinstance(error, TransportError) else TransportError(str(error))
        assert inspection is not None and reboot is not None and returncode is not None and reboot_disconnected_at is not None
        ready: ReadyRecord | None = None
        recovery_session: _ControlSession | None = None
        try:
            ready, recovery_session = self._wait_fault_recovery(
                selected, reboot_disconnected_at, inspection
            )
            _durable_write(trace / "ready.json", ready.raw)
            fence = {
                "schema": 1, "run_id": inspection.run_id, "session": inspection.session,
                "generation": inspection.generation, "phase": inspection.phase,
                "pid": inspection.pid, "start_time": inspection.start_time,
                "executable_sha256": inspection.executable_sha256,
                "run_ssh_returncode": returncode, "reboot_ssh_returncode": reboot.returncode,
                "ready_boot_id": ready.boot_id,
            }
            _durable_write(trace / "fence-recovery.json", (json.dumps(fence, separators=(",", ":")) + "\n").encode())
            _fsync_directory(trace)
        finally:
            if recovery_session is not None:
                recovery_session.close()
        return FaultReport(True, inspect_output, kill_output, inspection, ready, returncode, reboot.returncode, trace)


def _resolve_pinned(value: str, label: str) -> str:
    _validate_executable_token(value, label)
    found = shutil.which(value) if "/" not in value else value
    if not found:
        raise TransportError(f"cannot resolve pinned {label} executable")
    path = Path(found)
    if _path_contains_symlink(path):
        path = Path(os.path.realpath(path))
    try:
        info = path.stat()
    except OSError as exc:
        raise TransportError(f"cannot inspect pinned {label}: {exc}") from exc
    if not stat.S_ISREG(info.st_mode) or not (info.st_mode & stat.S_IXUSR) or info.st_mode & 0o022:
        raise TransportError(f"pinned {label} executable metadata is unsafe")
    return str(path)


def config_from_environment(repo_root: Path = REPO_ROOT) -> DevConfig:
    required = (
        "FOGCAST_DEV_HOST", "FOGCAST_DEV_USER", "FOGCAST_DEV_EXPECTED_BOARD",
        "FOGCAST_DEV_EXPECTED_MAIN_SHA256", "FOGCAST_DEV_TOOL_SHA256",
    )
    missing = [name for name in required if not os.environ.get(name)]
    if missing:
        raise TransportError(f"missing required FogCast environment: {', '.join(missing)}")
    dry_run = parse_dry_run(os.environ.get("FOGCAST_DEV_DRY_RUN", "1"))
    ssh = _resolve_pinned(os.environ.get("FOGCAST_DEV_SSH") or "ssh", "SSH")
    scp = _resolve_pinned(os.environ.get("FOGCAST_DEV_SCP") or "scp", "SCP")
    return DevConfig(
        host=os.environ["FOGCAST_DEV_HOST"], user=os.environ["FOGCAST_DEV_USER"],
        expected_board=os.environ["FOGCAST_DEV_EXPECTED_BOARD"],
        expected_main_sha256=os.environ["FOGCAST_DEV_EXPECTED_MAIN_SHA256"],
        expected_tool_sha256=os.environ["FOGCAST_DEV_TOOL_SHA256"],
        ssh=ssh, scp=scp, dry_run=dry_run, repo_root=repo_root,
        trace_root=repo_root / "build" / "fogcast-dev",
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("load", "preflight", "fault-inject"))
    parser.add_argument("--experiment", required=True)
    parser.add_argument("--build", required=True, choices=("oss", "oracle"))
    parser.add_argument("--run-id", required=True)
    args = parser.parse_args(argv)
    if args.experiment != EXPERIMENT:
        parser.error(f"--experiment must be {EXPERIMENT}")
    try:
        _require_run_id(args.run_id)
        root = REPO_ROOT
        path = root / "build" / "dev-bundle" / args.build / EXPERIMENT
        selected = load_bundle(path, args.run_id, args.build)
        transport = Transport(config_from_environment(root))
        method = {"load": transport.load, "preflight": transport.preflight, "fault-inject": transport.fault_inject}[args.action]
        report = method(selected)
        if report.argv:
            for command in report.argv:
                print("FOGCAST_DEV_DRY_RUN argv=" + json.dumps(list(command), separators=(",", ":")))
        else:
            print(f"FOGCAST_DEV_{args.action.replace('-', '_').upper()} code=ok run_id={args.run_id}")
        return 0
    except TransportError as exc:
        print(f"fogcast_dev: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
