#!/usr/bin/env python3
"""Collect deterministic, path-safe evidence for one build lane.

The collector intentionally has no third-party dependencies.  Inputs are
hashed as regular files and represented with repository-relative POSIX paths;
symlink components and paths outside the declared repository/build roots are
refused so a manifest cannot silently attest to a different source tree.
"""

from __future__ import annotations

import argparse
import datetime as _datetime
import hashlib
import json
import os
import platform
import re
import shlex
import subprocess
import sys
from pathlib import Path
from typing import Any, Mapping, Sequence


SCRIPT_ROOT = Path(__file__).resolve().parents[1]
DEFAULT_REPO_ROOT = SCRIPT_ROOT
TARGET_DEVICE = "5CSEBA6U23I7"
HASH_CHUNK_SIZE = 1024 * 1024
SHA256_RE = re.compile(r"^[0-9a-f]{64}$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
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
SYNTHESIS_REPORT_SUFFIXES = {
    "oss": "timing.json",
    "oracle": "top.fit.rpt",
}
STATIC_PROOF_BASIS = "static source/project/command exclusion"
STATIC_PROOF_PATTERNS = (
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
OSS_STATIC_FIELD_PATTERNS = {
    # These are field-specific contracts, not the broad policy's common
    # forbidden-resource list.  Bare alphabetic identifiers use the same
    # Verilog boundary semantics as experiment_policy.py: underscores and
    # digits are not alphabetic boundaries, so canonical MISTRAL primitives
    # are caught without treating a substring of an unrelated word as a hit.
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


class ManifestError(ValueError):
    """Raised when evidence cannot be collected safely."""


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
    return list(STATIC_PROOF_PATTERNS)


def _absolute(path: Path, base: Path) -> Path:
    """Make *path* absolute without following symlinks."""

    if path.is_absolute():
        return Path(os.path.abspath(os.fspath(path)))
    return Path(os.path.abspath(os.fspath(base / path)))


def _is_within(path: Path, root: Path) -> bool:
    try:
        path.relative_to(root)
    except ValueError:
        return False
    return True


def _contains_symlink(path: Path) -> bool:
    """Return whether an existing path component is a symlink."""

    current = Path(path.anchor)
    for component in path.parts[1:]:
        current /= component
        try:
            if current.is_symlink():
                return True
        except OSError as exc:
            raise ManifestError(f"cannot inspect path {path}: {exc}") from exc
    return False


def _root_path(value: Path, *, label: str, create: bool = False) -> Path:
    path = Path(value)
    absolute = _absolute(path, Path.cwd())
    if absolute.is_symlink():
        raise ManifestError(f"{label} root must not be a symlink: {absolute}")
    if not absolute.exists() and create:
        if _contains_symlink(absolute):
            raise ManifestError(f"{label} root path contains a symlink: {absolute}")
        try:
            absolute.mkdir(parents=True, exist_ok=True)
        except OSError as exc:
            raise ManifestError(f"cannot create {label} root {absolute}: {exc}") from exc
    try:
        resolved = absolute.resolve(strict=True)
    except FileNotFoundError as exc:
        raise ManifestError(f"{label} root is missing: {absolute}") from exc
    except OSError as exc:
        raise ManifestError(f"cannot inspect {label} root {absolute}: {exc}") from exc
    if not resolved.is_dir():
        raise ManifestError(f"{label} root is not a directory: {resolved}")
    return resolved


def _safe_path(
    value: Path,
    *,
    label: str,
    base: Path,
    allowed_roots: Sequence[Path],
    require_exists: bool,
) -> Path:
    """Resolve one input while preserving and checking its path spelling."""

    candidate = _absolute(Path(value), base)
    if _contains_symlink(candidate):
        raise ManifestError(f"{label} path contains a symlink: {value}")

    try:
        resolved = candidate.resolve(strict=False)
    except OSError as exc:
        raise ManifestError(f"cannot resolve {label} path {value}: {exc}") from exc

    if not any(_is_within(resolved, root) for root in allowed_roots):
        roots = ", ".join(str(root) for root in allowed_roots)
        raise ManifestError(f"{label} path is outside allowed roots ({roots}): {value}")

    if require_exists and not candidate.exists():
        raise ManifestError(f"missing {label}: {value}")
    return candidate


def _path_argument(value: Path, *, base: Path, fallback: Path | None = None) -> Path:
    """Anchor relative paths at the repository, with output-dir convenience."""

    if Path(value).is_absolute():
        return Path(value)
    primary = _absolute(Path(value), base)
    if fallback is not None:
        alternate = _absolute(Path(value), fallback)
        if not primary.exists() and alternate.exists():
            return alternate
    return primary


def _hash_file(path: Path) -> str:
    digest = hashlib.sha256()
    try:
        with path.open("rb") as stream:
            while True:
                chunk = stream.read(HASH_CHUNK_SIZE)
                if not chunk:
                    break
                digest.update(chunk)
    except OSError as exc:
        raise ManifestError(f"cannot hash file {path}: {exc}") from exc
    return digest.hexdigest()


def _files_for_input(path: Path, *, label: str) -> list[Path]:
    """Return regular files below one source/artifact input in stable order."""

    try:
        if path.is_symlink():
            raise ManifestError(f"{label} path is a symlink: {path}")
        if path.is_file():
            return [path]
        if not path.is_dir():
            raise ManifestError(f"{label} is not a regular file or directory: {path}")
    except OSError as exc:
        raise ManifestError(f"cannot inspect {label} {path}: {exc}") from exc

    files: list[Path] = []
    for current, directory_names, file_names in os.walk(path, topdown=True, followlinks=False):
        current_path = Path(current)
        directory_names.sort()
        file_names.sort()
        # os.walk does not descend into symlinked directories when
        # followlinks=False, so reject them explicitly instead of silently
        # omitting them from the evidence.
        for name in directory_names:
            child = current_path / name
            if child.is_symlink():
                raise ManifestError(f"{label} tree contains a symlink: {child}")
        for name in file_names:
            child = current_path / name
            if child.is_symlink():
                raise ManifestError(f"{label} tree contains a symlink: {child}")
            if not child.is_file():
                raise ManifestError(f"{label} tree contains a non-regular file: {child}")
            files.append(child)
    return files


def _relative_label(path: Path, *, repo_root: Path, build_root: Path) -> str:
    if _is_within(path, build_root):
        return Path("build", path.relative_to(build_root)).as_posix()
    if _is_within(path, repo_root):
        return path.relative_to(repo_root).as_posix()
    raise ManifestError(f"cannot create repository-relative path for {path}")


def _file_records(
    values: Sequence[Path],
    *,
    label: str,
    base: Path,
    repo_root: Path,
    build_root: Path,
    fallback: Path | None = None,
    require_exists: bool = True,
    allow_directories: bool = True,
) -> list[dict[str, Any]]:
    records: dict[str, dict[str, Any]] = {}
    for value in values:
        candidate = _path_argument(Path(value), base=base, fallback=fallback)
        safe = _safe_path(
            candidate,
            label=label,
            base=base,
            allowed_roots=(repo_root, build_root),
            require_exists=require_exists,
        )
        if safe.is_dir() and not allow_directories:
            raise ManifestError(f"{label} is not a regular file: {value}")
        for path in _files_for_input(safe, label=label):
            relative = _relative_label(path, repo_root=repo_root, build_root=build_root)
            if relative in records:
                raise ManifestError(f"duplicate {label} path: {relative}")
            records[relative] = {"path": relative, "sha256": _hash_file(path)}
    return [records[key] for key in sorted(records)]


def _oss_command_contract_error(
    path: Path,
    command_name: str,
    experiment: str,
) -> str | None:
    """Check the exact mailbox OSS argv before it can support static zeros."""

    if experiment != "020_linux_mailbox" or command_name not in {
        "yosys.log",
        "nextpnr-help.log",
        "nextpnr.log",
    }:
        return None
    try:
        first_line = path.read_text(encoding="utf-8", errors="replace").splitlines()[0]
    except (OSError, IndexError) as exc:
        return f"OSS {command_name} cannot provide its first command header: {exc}"
    if not first_line.startswith("command:"):
        return f"OSS {command_name} does not start with a command header"
    try:
        tokens = shlex.split(first_line[len("command:") :].strip())
    except ValueError as exc:
        return f"OSS {command_name} command header is not shell-parseable: {exc}"
    expected_yosys_program = (
        "read_verilog experiments/020_linux_mailbox/rtl/top.v; "
        "synth_intel_alm -nobram -nolutram -nodsp -top top; stat; "
        "write_json build/oss/020_linux_mailbox/synth.json"
    )
    if command_name == "yosys.log":
        if len(tokens) != 3 or Path(tokens[0]).name != "yosys" or tokens[1] != "-p":
            return "OSS yosys.log command contract is not the authenticated mailbox synthesis argv"
        if tokens[2] != expected_yosys_program:
            return "OSS yosys.log command contract has wrong flags, source, top, or synthesis output"
        return None
    if command_name == "nextpnr-help.log":
        if len(tokens) != 2 or Path(tokens[0]).name != "nextpnr-mistral" or tokens[1] != "--help":
            return "OSS nextpnr-help.log command contract is not the authenticated nextpnr help argv"
        return None
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
        return "OSS nextpnr.log command contract has wrong flags, target, constraints, or outputs"
    return None


def _authenticated_tool_path(
    value: Any,
    *,
    tool: str,
    repository: Path,
) -> Path:
    if not isinstance(value, str) or not value:
        raise ManifestError(f"OSS authenticated {tool} path is missing")
    expected_relative = OSS_AUTHENTICATED_TOOL_PATHS.get(tool)
    if expected_relative is not None and value != expected_relative:
        raise ManifestError(
            f"OSS authenticated {tool} path must be the canonical repository tool path: {value}"
        )
    raw_path = Path(value)
    if raw_path.as_posix() != value or any(part in {"", ".", ".."} for part in raw_path.parts):
        raise ManifestError(f"OSS authenticated {tool} path is not canonical: {value}")
    candidate = _absolute(raw_path, repository)
    if not _is_within(candidate, repository):
        raise ManifestError(f"OSS authenticated {tool} path is outside the repository: {value}")
    if _contains_symlink(candidate):
        raise ManifestError(f"OSS authenticated {tool} path contains a symlink: {value}")
    if not candidate.is_file() or not os.access(candidate, os.X_OK):
        raise ManifestError(f"OSS authenticated {tool} path is not an executable regular file: {value}")
    return candidate


def _tool_pin_commit(value: Any) -> str | None:
    if isinstance(value, str):
        return value
    if isinstance(value, dict) and isinstance(value.get("commit"), str):
        return value["commit"]
    return None


def _validate_oss_authenticated_tool_argv(
    summary: Mapping[str, Any],
    command_logs: Sequence[dict[str, Any]],
    *,
    repository: Path,
    experiment: str,
    lane: str,
    resolved_command_logs: Mapping[str, Path] | None = None,
    canonical_tool_pins: Mapping[str, Mapping[str, Any]] | None = None,
) -> None:
    """Bind normalized OSS executable argv tokens to reopened tool records."""

    if lane != "oss" or experiment != "020_linux_mailbox":
        return
    required_tools = ("yosys", "nextpnr-mistral")
    authenticated = summary.get("authenticated_tools")
    if not isinstance(authenticated, dict) or set(authenticated) != set(required_tools):
        raise ManifestError(
            "OSS authenticated_tools must contain exactly yosys and nextpnr-mistral"
        )
    pins = summary.get("tool_pins")
    required_pins = {"yosys", "nextpnr"}
    if not isinstance(pins, dict) or set(pins) != required_pins:
        raise ManifestError("OSS tool_pins must contain exactly yosys and nextpnr")
    if canonical_tool_pins is None:
        canonical_tool_pins = _tool_pins(repository)

    tool_paths: dict[str, Path] = {}
    for tool in required_tools:
        record = authenticated.get(tool)
        if not isinstance(record, dict):
            raise ManifestError(f"OSS authenticated {tool} record is missing or malformed")
        for field in ("commit", "path", "sha256"):
            if not isinstance(record.get(field), str) or not record[field]:
                raise ManifestError(f"OSS authenticated {tool} {field} is missing or malformed")
        commit = record["commit"]
        digest = record["sha256"]
        if COMMIT_RE.fullmatch(commit) is None:
            raise ManifestError(f"OSS authenticated {tool} commit is invalid")
        if SHA256_RE.fullmatch(digest) is None:
            raise ManifestError(f"OSS authenticated {tool} sha256 is invalid")
        path = _authenticated_tool_path(record["path"], tool=tool, repository=repository)
        if path.name != tool:
            raise ManifestError(f"OSS authenticated {tool} path has the wrong basename")
        if _hash_file(path) != digest:
            raise ManifestError(f"OSS authenticated {tool} sha256 does not match opened executable")
        lock_name = "nextpnr" if tool == "nextpnr-mistral" else tool
        pin_commit = _tool_pin_commit(pins.get(lock_name))
        if pin_commit is None or COMMIT_RE.fullmatch(pin_commit) is None:
            raise ManifestError(f"OSS {lock_name} tool pin commit is missing or invalid")
        canonical_pin = canonical_tool_pins.get(lock_name)
        canonical_commit = _tool_pin_commit(canonical_pin)
        if canonical_commit is None or COMMIT_RE.fullmatch(canonical_commit) is None:
            raise ManifestError(f"OSS canonical {lock_name} toolchain.lock pin is missing or invalid")
        if pin_commit != canonical_commit:
            raise ManifestError(f"OSS {lock_name} tool pin disagrees with canonical manifest pin")
        if commit != canonical_commit:
            raise ManifestError(f"OSS authenticated {tool} commit disagrees with canonical manifest pin")
        if pin_commit != commit:
            raise ManifestError(f"OSS authenticated {tool} commit disagrees with tool pin")
        tool_paths[tool] = path

    expected_logs = {
        "yosys.log": "yosys",
        "nextpnr-help.log": "nextpnr-mistral",
        "nextpnr.log": "nextpnr-mistral",
    }
    by_name = {
        Path(record.get("path", "")).name: record
        for record in command_logs
        if isinstance(record, dict) and isinstance(record.get("path"), str)
    }
    for command_name, tool in expected_logs.items():
        record = by_name.get(command_name)
        if record is None:
            raise ManifestError(f"OSS {command_name} executable provenance record is missing")
        relative = record.get("path")
        if not isinstance(relative, str):
            raise ManifestError(f"OSS {command_name} command-log path is malformed")
        path = (
            resolved_command_logs.get(relative)
            if resolved_command_logs is not None
            else _safe_path(
                repository / relative,
                label=f"OSS {command_name} command log",
                base=repository,
                allowed_roots=(repository,),
                require_exists=True,
            )
        )
        if path is None or _contains_symlink(path) or not path.is_file():
            raise ManifestError(f"OSS {command_name} command log is missing or not regular")
        try:
            first_line = path.read_text(encoding="utf-8", errors="replace").splitlines()[0]
            tokens = shlex.split(first_line[len("command:") :].strip())
        except (OSError, IndexError, ValueError) as exc:
            raise ManifestError(f"OSS {command_name} command header cannot be parsed: {exc}") from exc
        if not first_line.startswith("command:") or not tokens:
            raise ManifestError(f"OSS {command_name} command header is missing")
        token = Path(tokens[0])
        if not token.is_absolute() or Path(os.path.abspath(os.fspath(token))) != tool_paths[tool]:
            raise ManifestError(
                f"OSS {command_name} executable token is not bound to authenticated {tool}"
            )


def _command_log_records(
    values: Sequence[Path],
    *,
    base: Path,
    fallback: Path | None,
    repo_root: Path,
    build_root: Path,
    experiment: str = "",
    lane: str = "",
    resolved_paths: dict[str, Path] | None = None,
) -> tuple[list[dict[str, Any]], list[str]]:
    records: dict[str, dict[str, Any]] = {}
    commands: list[str] = []
    for value in values:
        candidate = _path_argument(Path(value), base=base, fallback=fallback)
        safe = _safe_path(
            candidate,
            label="command log",
            base=base,
            allowed_roots=(repo_root, build_root),
            require_exists=True,
        )
        if not safe.is_file() or safe.is_symlink():
            raise ManifestError(f"command log is not a regular file: {value}")
        relative = _relative_label(safe, repo_root=repo_root, build_root=build_root)
        if relative in records:
            raise ManifestError(f"duplicate command log path: {relative}")
        records[relative] = {"path": relative, "sha256": _hash_file(safe)}
        if resolved_paths is not None:
            resolved_paths[relative] = safe
        try:
            text = safe.read_text(encoding="utf-8", errors="replace")
        except OSError as exc:
            raise ManifestError(f"cannot read command log {safe}: {exc}") from exc
        # run_logged.sh emits exactly one authenticated header as line one.
        # Later command:-prefixed lines may be tool output and are not
        # evidence of a command invocation.
        lines = text.splitlines()
        first_line = lines[0] if lines else ""
        if first_line.startswith("command:"):
            commands.append(first_line.rstrip("\r"))
        if lane == "oss" and experiment == "020_linux_mailbox":
            error = _oss_command_contract_error(safe, Path(relative).name, experiment)
            if error is not None:
                raise ManifestError(error)
    return [records[key] for key in sorted(records)], commands


def _build_summary_record(
    value: Path | None,
    *,
    base: Path,
    repo_root: Path,
    build_root: Path,
) -> dict[str, Any] | None:
    if value is None:
        return None
    candidate = _path_argument(Path(value), base=base)
    safe = _safe_path(
        candidate,
        label="build summary",
        base=base,
        allowed_roots=(repo_root, build_root),
        require_exists=True,
    )
    if safe.is_symlink() or not safe.is_file():
        raise ManifestError(f"build summary is not a regular file: {value}")
    try:
        summary = json.loads(safe.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ManifestError(f"cannot read build summary {safe}: {exc}") from exc
    if not isinstance(summary, dict):
        raise ManifestError(f"build summary must contain a JSON object: {value}")
    return summary


def _policy_for(experiment: str) -> Any:
    """Load the repository's closed policy without probing external tools."""

    scripts_dir = str(SCRIPT_ROOT)
    if scripts_dir not in sys.path:
        sys.path.insert(0, scripts_dir)
    try:
        from experiment_policy import policy_for

        return policy_for(experiment)
    except (ImportError, OSError, ValueError) as exc:
        raise ManifestError(f"cannot load experiment policy: {exc}") from exc


def _canonical_policy_hash(policy: Any) -> str:
    encoded = json.dumps(
        policy.as_dict(), ensure_ascii=True, separators=(",", ":"), sort_keys=True
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def _used_count(value: Any, *, label: str) -> int:
    if isinstance(value, dict):
        value = value.get("used")
    if type(value) is not int or value < 0 or value > 0xFFFFFFFF:
        raise ManifestError(f"{label} must be a JSON integer in 0..4294967295")
    return value


def _synthesis_report_path(experiment: str, lane: str) -> str:
    suffix = SYNTHESIS_REPORT_SUFFIXES.get(lane)
    if suffix is None:
        raise ManifestError(f"unsupported build lane for synthesis report: {lane}")
    return f"build/{lane}/{experiment}/{suffix}"


def _synthesis_report_binding(
    artifacts: Sequence[dict[str, Any]],
    *,
    experiment: str,
    lane: str,
) -> dict[str, str]:
    """Select exactly one canonical lane report from already-hashed artifacts."""

    expected_path = _synthesis_report_path(experiment, lane)
    matches = [record for record in artifacts if record.get("path") == expected_path]
    if len(matches) != 1:
        raise ManifestError(
            f"exactly one normalized synthesis report is required at {expected_path}; "
            f"found {len(matches)}"
        )
    record = matches[0]
    digest = record.get("sha256")
    if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
        raise ManifestError(f"normalized synthesis report hash is invalid: {expected_path}")
    return {"path": expected_path, "sha256": digest}


def _command_proof_records(
    command_logs: Sequence[dict[str, Any]],
    *,
    experiment: str,
    lane: str,
) -> list[dict[str, str]]:
    """Return the immutable command-log subset that feeds static proof."""

    expected_names = (
        ("yosys.log", "nextpnr-help.log", "nextpnr.log")
        if lane == "oss"
        else ("quartus-version.log", "quartus.log")
    )
    expected_paths = tuple(
        f"build/{lane}/{experiment}/{name}" for name in expected_names
    )
    expected_prefix = f"build/{lane}/{experiment}/"
    by_path: dict[str, dict[str, Any]] = {}
    for record in command_logs:
        path = record.get("path")
        if not isinstance(path, str):
            continue
        if path in by_path:
            raise ManifestError(f"duplicate command-log path: {path}")
        by_path[path] = record
    if not all(path in by_path for path in expected_paths):
        missing = ", ".join(path for path in expected_paths if path not in by_path)
        raise ManifestError(f"canonical static-proof command logs are incomplete: {missing}")
    unexpected = sorted(
        path
        for path in by_path
        if path.startswith(expected_prefix)
        and path not in expected_paths
        and path != f"{expected_prefix}summary.log"
    )
    if unexpected:
        raise ManifestError(
            "static-proof command logs contain unexpected lane records: "
            + ", ".join(unexpected)
        )
    selected: list[dict[str, str]] = []
    for path in expected_paths:
        record = by_path.get(path)
        if record is None:
            # Small collector fixtures may have one differently named command
            # log; use it only when no canonical lane logs exist.  Real build
            # manifests always take the exact expected path branch.
            continue
        digest = record.get("sha256")
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            raise ManifestError(f"command-log hash is invalid: {path}")
        selected.append({"path": path, "sha256": digest})
    return selected


def _top_port_evidence(source: str, clock: str) -> dict[str, int]:
    """Count the ANSI ports of the production top module.

    The mailbox top is deliberately a one-input clock shell.  Counting its
    declaration, rather than the implementation's nested protocol module,
    keeps HPS GPI/GPO wiring internal and makes output-port regressions visible.
    """

    match = re.search(
        r"(?ms)\bmodule\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
        source,
    )
    if match is None:
        raise ManifestError("production top module port declaration is missing")
    port_text = match.group(1)
    counts = {"input": 0, "output": 0, "inout": 0}
    current_direction: str | None = None
    declared_segments: list[tuple[str, str]] = []
    for raw_segment in port_text.split(","):
        segment = re.sub(r"//[^\n]*|/\*.*?\*/", " ", raw_segment, flags=re.S).strip()
        if not segment:
            continue
        direction_match = re.match(r"^(input|output|inout)\b(?P<tail>.*)$", segment, re.I | re.S)
        if direction_match:
            current_direction = direction_match.group(1).lower()
            tail = direction_match.group("tail")
        elif current_direction is not None:
            tail = segment
        else:
            raise ManifestError("production top port declaration has an undeclared ANSI segment")
        # The final Verilog identifier in an ANSI segment is the port name;
        # widths and net/reg/signed type tokens precede it.  Keeping the
        # direction across comma-separated segments handles
        # ``input wire clk, hidden_input`` without treating hidden_input as
        # an untyped declaration.
        names = re.findall(r"\b[A-Za-z_][A-Za-z0-9_$]*\b", tail)
        if not names:
            raise ManifestError("production top port declaration is malformed")
        counts[current_direction] += 1
        declared_segments.append((current_direction, segment))
    clock_inputs = sum(
        1
        for direction, segment in declared_segments
        if direction == "input" and re.search(rf"\b{re.escape(clock)}\b", segment)
    )
    if clock_inputs != 1 or counts["input"] < clock_inputs:
        raise ManifestError("production top must expose exactly one intended clock input")
    return {
        "clock_inputs": clock_inputs,
        "external_input_ports": counts["input"] - clock_inputs,
        "external_output_ports": counts["output"],
        "bidirectional_ports": counts["inout"],
    }


def _canonical_static_source_records(
    source_records: Sequence[dict[str, Any]],
    *,
    experiment: str,
    lane: str,
) -> list[dict[str, str]]:
    expected_paths = [
        f"experiments/{experiment}/rtl/top.v",
        "boards/de10nano/pins.qsf",
        "boards/de10nano/clocks.sdc",
    ]
    if lane == "oracle":
        expected_paths.extend(
            (
                f"experiments/{experiment}/oracle/top.qpf",
                f"experiments/{experiment}/oracle/top.qsf",
            )
        )
    by_path: dict[str, dict[str, Any]] = {}
    for record in source_records:
        path = record.get("path")
        if isinstance(path, str):
            if path in by_path:
                raise ManifestError(f"duplicate source path: {path}")
            by_path[path] = record
    selected: list[dict[str, str]] = []
    for path in expected_paths:
        record = by_path.get(path)
        digest = record.get("sha256") if record is not None else None
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            raise ManifestError(f"static proof source hash is missing or invalid: {path}")
        selected.append({"path": path, "sha256": digest})
    return selected


def _validate_oss_static_source_scan(
    source_records: Sequence[dict[str, str]],
    repository: Path,
    field: str,
    experiment: str,
    lane: str,
) -> None:
    """Re-open canonical OSS inputs and prove one static field is absent."""

    patterns = OSS_STATIC_FIELD_PATTERNS.get(field)
    if lane != "oss" or experiment != "020_linux_mailbox" or patterns is None:
        return
    expected_paths = (
        f"experiments/{experiment}/rtl/top.v",
        "boards/de10nano/pins.qsf",
        "boards/de10nano/clocks.sdc",
    )
    by_path = {record.get("path"): record for record in source_records}
    for relative in expected_paths:
        record = by_path.get(relative)
        digest = record.get("sha256") if isinstance(record, dict) else None
        if not isinstance(digest, str) or SHA256_RE.fullmatch(digest) is None:
            raise ManifestError(f"OSS {field} static source hash is missing or invalid: {relative}")
        path = repository / relative
        if _contains_symlink(path) or not path.is_file():
            raise ManifestError(f"OSS {field} static source is missing or not regular: {relative}")
        try:
            source_bytes = path.read_bytes()
            source_text = source_bytes.decode("utf-8", errors="replace")
        except OSError as exc:
            raise ManifestError(f"cannot read OSS {field} static source {relative}: {exc}") from exc
        if _hash_file(path) != digest:
            raise ManifestError(f"OSS {field} static source hash does not match opened file: {relative}")
        for pattern in patterns:
            if _verilog_source_pattern_matches(pattern, source_text):
                raise ManifestError(
                    f"OSS {field} static source scan matched forbidden pattern {pattern!r}: {relative}"
                )


def _validate_resource_evidence_provenance(
    raw: Any,
    *,
    evidence: Mapping[str, int],
    report: Mapping[str, str],
    source_records: Sequence[dict[str, Any]],
    command_logs: Sequence[dict[str, Any]],
    experiment: str,
    lane: str,
    measured_fields: set[str],
    repository: Path,
) -> dict[str, Any]:
    if not isinstance(raw, dict) or set(raw) != {"report", "fields"}:
        raise ManifestError("resource_evidence_provenance must contain report and fields")
    declared_report = raw.get("report")
    if declared_report != dict(report):
        raise ManifestError("resource evidence report binding disagrees with synthesis report")
    fields = raw.get("fields")
    if not isinstance(fields, dict) or tuple(fields) != SEMANTIC_RESOURCE_FIELDS:
        raise ManifestError("resource_evidence_provenance fields are not canonical")
    static_sources = _canonical_static_source_records(
        source_records, experiment=experiment, lane=lane
    )
    static_commands = _command_proof_records(
        command_logs, experiment=experiment, lane=lane
    )
    for field in SEMANTIC_RESOURCE_FIELDS:
        record = fields.get(field)
        if not isinstance(record, dict):
            raise ManifestError(f"resource evidence provenance is malformed: {field}")
        kind = record.get("kind")
        if kind == "measured_report_row":
            if set(record) != {"kind", "label", "report"}:
                raise ManifestError(f"measured resource evidence provenance is malformed: {field}")
            if not isinstance(record.get("label"), str) or not record["label"]:
                raise ManifestError(f"measured resource evidence label is missing: {field}")
            if record.get("report") != dict(report):
                raise ManifestError(f"measured resource evidence report disagrees: {field}")
            if field not in measured_fields:
                raise ManifestError(
                    f"measured resource evidence has no corresponding report row: {field}"
                )
        elif kind == "source_port_declaration":
            if set(record) != {"kind", "source"}:
                raise ManifestError(f"port evidence provenance is malformed: {field}")
            source = record.get("source")
            if not isinstance(source, dict) or set(source) != {"path", "sha256"}:
                raise ManifestError(f"port evidence provenance source is malformed: {field}")
            protocol = next(
                (
                    item
                    for item in source_records
                    if item.get("path") == f"experiments/{experiment}/rtl/top.v"
                ),
                None,
            )
            if source != protocol:
                raise ManifestError(f"port evidence provenance source disagrees: {field}")
        elif kind == "static_exclusion":
            expected_keys = {"kind", "basis", "patterns", "sources", "commands", "report"}
            if set(record) != expected_keys:
                raise ManifestError(f"static resource evidence provenance is malformed: {field}")
            if record.get("basis") != STATIC_PROOF_BASIS:
                raise ManifestError(f"static resource evidence basis is invalid: {field}")
            if record.get("patterns") != _static_patterns_for_field(field, lane):
                raise ManifestError(f"static resource evidence patterns are not canonical: {field}")
            if record.get("sources") != static_sources:
                raise ManifestError(f"static resource evidence sources are not canonical: {field}")
            if record.get("commands") != static_commands:
                raise ManifestError(f"static resource evidence commands are not canonical: {field}")
            if record.get("report") != dict(report):
                raise ManifestError(f"static resource evidence report disagrees: {field}")
            if field in measured_fields:
                raise ManifestError(
                    f"static resource evidence masks a measured report row: {field}"
                )
            _validate_oss_static_source_scan(
                static_sources,
                repository,
                field,
                experiment,
                lane,
            )
        else:
            raise ManifestError(f"resource evidence provenance kind is unknown: {field}")
        if field not in evidence:
            raise ManifestError(f"resource evidence provenance has unknown field: {field}")
    return raw


def _resource_evidence_provenance(
    summary: dict[str, Any],
    *,
    evidence: Mapping[str, int],
    report: Mapping[str, str],
    source_records: Sequence[dict[str, Any]],
    command_logs: Sequence[dict[str, Any]],
    experiment: str,
    lane: str,
    policy: Any,
    repository: Path,
) -> dict[str, Any]:
    measured_labels = {
        "hps_general_purpose_interfaces": "cyclonev_hps_interface_mpu_general_purpose",
        "pll_blocks": "PLL",
        "dsp_blocks": "DSP",
        "block_memory_bits": "block_memory_bits",
        "lutram_bits": "lutram_bits",
        "sdram_interfaces": "sdram_interfaces",
    }
    resources: dict[str, Any] = {}
    for key in ("resources", "hard_blocks"):
        values = summary.get(key)
        if isinstance(values, dict):
            resources.update(values)
    measured_fields = {
        field for field, label in measured_labels.items() if label in resources
    }
    supplied = summary.get("resource_evidence_provenance")
    if supplied is not None:
        return _validate_resource_evidence_provenance(
            supplied,
            evidence=evidence,
            report=report,
            source_records=source_records,
            command_logs=command_logs,
            experiment=experiment,
            lane=lane,
            measured_fields=measured_fields,
            repository=repository,
        )

    static_sources = _canonical_static_source_records(
        source_records, experiment=experiment, lane=lane
    )
    static_commands = _command_proof_records(
        command_logs, experiment=experiment, lane=lane
    )
    def static_record_for(field: str) -> dict[str, Any]:
        return {
            "kind": "static_exclusion",
            "basis": STATIC_PROOF_BASIS,
            "patterns": _static_patterns_for_field(field, lane),
            "sources": static_sources,
            "commands": static_commands,
            "report": dict(report),
        }
    protocol_source = next(
        (
            item
            for item in source_records
            if item.get("path") == f"experiments/{experiment}/rtl/top.v"
        ),
        None,
    )
    if protocol_source is None:
        raise ManifestError("port evidence requires the production protocol source record")
    fields: dict[str, Any] = {}
    for field in SEMANTIC_RESOURCE_FIELDS:
        if field in {"clock_inputs", "external_input_ports", "external_output_ports", "bidirectional_ports"}:
            fields[field] = {
                "kind": "source_port_declaration",
                "source": dict(protocol_source),
            }
            continue
        resource_name = {
            "hps_general_purpose_interfaces": "cyclonev_hps_interface_mpu_general_purpose",
            "pll_blocks": "PLL",
            "dsp_blocks": "DSP",
            "block_memory_bits": "block_memory_bits",
            "lutram_bits": "lutram_bits",
            "sdram_interfaces": "sdram_interfaces",
        }[field]
        if resource_name in resources:
            fields[field] = {
                "kind": "measured_report_row",
                "label": resource_name,
                "report": dict(report),
            }
        else:
            fields[field] = static_record_for(field)
    generated = {"report": dict(report), "fields": fields}
    return _validate_resource_evidence_provenance(
        generated,
        evidence=evidence,
        report=report,
        source_records=source_records,
        command_logs=command_logs,
        experiment=experiment,
        lane=lane,
        measured_fields=measured_fields,
        repository=repository,
    )


def _semantic_resource_evidence(
    build: dict[str, Any],
    *,
    source_text: str,
    clock: str,
    allowed_hard_blocks: Mapping[str, int],
) -> dict[str, int]:
    """Return exact semantic resource counts, preserving observed non-zero values."""

    port_evidence = _top_port_evidence(source_text, clock)
    supplied = build.get("resource_evidence")
    if supplied is not None:
        if not isinstance(supplied, dict) or set(supplied) != set(SEMANTIC_RESOURCE_FIELDS):
            raise ManifestError("resource_evidence must contain the exact semantic field set")
        evidence = {}
        for field in SEMANTIC_RESOURCE_FIELDS:
            value = supplied[field]
            if type(value) is not int or value < 0 or value > 0xFFFFFFFF:
                raise ManifestError(
                    f"resource_evidence.{field} must be a JSON integer in 0..4294967295"
                )
            evidence[field] = value
        for field in ("clock_inputs", "external_input_ports", "external_output_ports", "bidirectional_ports"):
            if evidence[field] != port_evidence[field]:
                raise ManifestError(
                    f"resource_evidence.{field} disagrees with production top ports"
                )
    else:
        evidence = dict(port_evidence)
        resources: dict[str, Any] = {}
        for key in ("resources", "hard_blocks"):
            records = build.get(key)
            if isinstance(records, dict):
                resources.update(records)

        def total_matching(markers: tuple[str, ...]) -> int:
            total = 0
            for name, record in resources.items():
                if isinstance(name, str) and any(marker in name.lower() for marker in markers):
                    total += _used_count(record, label=f"resource {name}.used")
            return total

        allowed_name = next(iter(allowed_hard_blocks), None)
        if allowed_name is None:
            hps_count = 0
        else:
            matching = [resources[name] for name in resources if name == allowed_name]
            if not matching:
                raise ManifestError(f"missing allowed primitive evidence: {allowed_name}")
            hps_count = _used_count(matching[0], label=f"resource {allowed_name}.used")
        evidence.update(
            {
                "hps_general_purpose_interfaces": hps_count,
                "pll_blocks": total_matching(("pll", "phase_locked")),
                "dsp_blocks": total_matching(("dsp", "mul", "mac")),
                "block_memory_bits": total_matching(("block_memory_bits", "block memory bits")),
                "lutram_bits": total_matching(("lutram_bits", "lutram bits", "mlab bits")),
                "sdram_interfaces": total_matching(("sdram",)),
            }
        )
    return evidence


def _augment_mailbox_summary(
    summary: dict[str, Any],
    *,
    experiment: str,
    lane: str,
    target: str,
    source_records: Sequence[dict[str, Any]],
    command_logs: Sequence[dict[str, Any]],
    report: Mapping[str, str],
    repository: Path,
    resolved_command_logs: Mapping[str, Path] | None = None,
    canonical_tool_pins: Mapping[str, Mapping[str, Any]] | None = None,
) -> dict[str, Any]:
    """Bind mailbox policy/protocol/resource evidence into a schema-2 summary."""

    policy = _policy_for(experiment)
    expected_policy = policy.as_dict()
    current_policy = summary.get("experiment_policy")
    if current_policy is not None and current_policy != expected_policy:
        raise ManifestError("build summary experiment_policy does not match the closed policy")
    declared_experiment = summary.get("experiment")
    if declared_experiment is not None and declared_experiment != experiment:
        raise ManifestError("build summary experiment does not match the selected experiment")
    declared_target = summary.get("target")
    if declared_target is not None and declared_target != policy.target:
        raise ManifestError("build summary target does not match the closed policy")
    declared_top = summary.get("top")
    if declared_top is not None and declared_top != policy.top:
        raise ManifestError("build summary top does not match the closed policy")
    declared_clock = summary.get("clock_intent")
    if declared_clock is not None and declared_clock != policy.clock:
        raise ManifestError("build summary clock_intent does not match the closed policy")
    declared_constraint = summary.get("clock_constraint_mhz")
    if declared_constraint is not None and (
        type(declared_constraint) not in (int, float)
        or isinstance(declared_constraint, bool)
        or float(declared_constraint) != float(policy.clock_mhz)
    ):
        raise ManifestError("build summary clock_constraint_mhz does not match the closed policy")
    declared_allowed = summary.get("allowed_hard_blocks")
    if declared_allowed is not None and declared_allowed != dict(policy.allowed_hard_blocks):
        raise ManifestError("build summary allowed_hard_blocks does not match the closed policy")
    declared_lane = summary.get("lane")
    if declared_lane is not None and declared_lane != lane:
        raise ManifestError("build summary lane does not match the selected lane")
    summary["experiment_policy"] = expected_policy
    policy_hash = _canonical_policy_hash(policy)
    for field in ("experiment_policy_sha256", "policy_sha256"):
        declared = summary.get(field)
        if declared is not None and (not isinstance(declared, str) or declared != policy_hash):
            raise ManifestError(f"build summary {field} does not match experiment_policy")
        summary[field] = policy_hash

    source_map = {
        record["path"]: record["sha256"]
        for record in source_records
        if isinstance(record, dict)
        and isinstance(record.get("path"), str)
        and isinstance(record.get("sha256"), str)
    }
    protocol_path = f"experiments/{experiment}/rtl/top.v"
    protocol_hash = source_map.get(protocol_path)
    if not isinstance(protocol_hash, str) or SHA256_RE.fullmatch(protocol_hash) is None:
        raise ManifestError(f"manifest source hash is missing: {protocol_path}")
    declared_protocol_path = summary.get("protocol_source")
    if declared_protocol_path is not None and declared_protocol_path != protocol_path:
        raise ManifestError("build summary protocol_source does not match production RTL")
    declared_protocol = summary.get("protocol_source_sha256")
    if declared_protocol is not None and declared_protocol != protocol_hash:
        raise ManifestError("build summary protocol_source_sha256 does not match production RTL")
    declared_source_hashes = summary.get("source_hashes")
    if isinstance(declared_source_hashes, dict):
        declared_source_hash = declared_source_hashes.get(protocol_path)
        if declared_source_hash is not None and declared_source_hash != protocol_hash:
            raise ManifestError("build summary source_hashes does not match production RTL")
    summary["protocol_source_sha256"] = protocol_hash
    summary["protocol_source"] = protocol_path

    if target != policy.target:
        raise ManifestError(f"target must be {policy.target}, got {target!r}")
    summary["target"] = target
    summary["lane"] = lane
    summary["experiment"] = experiment
    summary["top"] = policy.top
    summary["clock_intent"] = policy.clock
    summary["clock_constraint_mhz"] = float(policy.clock_mhz)
    summary["allowed_hard_blocks"] = dict(policy.allowed_hard_blocks)

    expected_timing_clock = (
        "protocol.FPGA_CLK1_50" if lane == "oss" else "FPGA_CLK1_50"
    )
    timing = summary.get("timing")
    if not isinstance(timing, dict) or timing.get("clock") != expected_timing_clock:
        raise ManifestError(
            f"build summary timing clock must be exactly {expected_timing_clock} for {lane}"
        )

    source_path = repository / protocol_path
    try:
        source_text = source_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ManifestError(f"cannot read protocol source {source_path}: {exc}") from exc
    evidence = _semantic_resource_evidence(
        summary,
        source_text=source_text,
        clock=policy.clock,
        allowed_hard_blocks=policy.allowed_hard_blocks,
    )
    expected_evidence = {
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
    for field, expected in expected_evidence.items():
        if evidence.get(field) != expected:
            raise ManifestError(
                f"resource_evidence.{field} is {evidence.get(field)!r}, expected {expected}"
            )
    summary["resource_evidence"] = evidence
    _validate_oss_authenticated_tool_argv(
        summary,
        command_logs,
        repository=repository,
        experiment=experiment,
        lane=lane,
        resolved_command_logs=resolved_command_logs,
        canonical_tool_pins=canonical_tool_pins,
    )
    summary["synthesis_report"] = dict(report)
    summary["synthesis_report_path"] = report["path"]
    summary["synthesis_report_sha256"] = report["sha256"]
    summary["resource_evidence_provenance"] = _resource_evidence_provenance(
        summary,
        evidence=evidence,
        report=report,
        source_records=source_records,
        command_logs=command_logs,
        experiment=experiment,
        lane=lane,
        policy=policy,
        repository=repository,
    )
    return summary


def _timestamp() -> str:
    value = os.environ.get("SOURCE_DATE_EPOCH")
    try:
        if value is None:
            instant = _datetime.datetime.now(_datetime.timezone.utc)
        else:
            instant = _datetime.datetime.fromtimestamp(int(value), tz=_datetime.timezone.utc)
    except (OverflowError, OSError, ValueError) as exc:
        raise ManifestError(f"invalid SOURCE_DATE_EPOCH: {value!r}") from exc
    return instant.replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _host() -> dict[str, str]:
    return {
        "architecture": platform.machine(),
        "name": platform.node(),
        "os": platform.system(),
        "os_release": platform.release(),
        "python": platform.python_version(),
    }


def _git_state(repo_root: Path) -> dict[str, Any]:
    def run(*arguments: str) -> str | None:
        try:
            result = subprocess.run(
                ["git", "-C", str(repo_root), *arguments],
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
        except OSError:
            return None
        if result.returncode != 0:
            return None
        return result.stdout.strip()

    commit = run("rev-parse", "HEAD")
    status_text = run("status", "--porcelain=v1", "--untracked-files=all")
    if commit is None or status_text is None:
        return {"commit": commit, "dirty": None, "state": "unknown", "changes": []}
    changes = sorted(line for line in status_text.splitlines() if line)
    dirty = bool(changes)
    return {
        "commit": commit,
        "dirty": dirty,
        "state": "dirty" if dirty else "clean",
        "changes": changes,
    }


def _tool_pins(repo_root: Path) -> dict[str, dict[str, Any]]:
    scripts_dir = str(repo_root / "scripts")
    if scripts_dir not in sys.path:
        sys.path.insert(0, scripts_dir)
    try:
        from lockfile import load_lock

        pins = load_lock(repo_root / "toolchain.lock")
    except (ImportError, OSError, ValueError) as exc:
        raise ManifestError(f"cannot load toolchain.lock: {exc}") from exc
    return {
        name: {
            "commit": pin.commit,
            "order": pin.order,
            "rationale": pin.rationale,
            "repo": pin.repo,
        }
        for name, pin in sorted(pins.items())
    }


def collect_manifest(
    output_dir: Path,
    experiment: str,
    lane: str,
    target: str,
    source_paths: Sequence[Path] = (),
    command_log_paths: Sequence[Path] = (),
    artifact_paths: Sequence[Path] = (),
    *,
    repo_root: Path = DEFAULT_REPO_ROOT,
    build_root: Path | None = None,
    commands: Sequence[str] = (),
    manifest_path: Path | None = None,
    build_summary_path: Path | None = None,
) -> dict[str, Any]:
    """Collect and write one manifest, returning the JSON-compatible object."""

    if not experiment or not lane:
        raise ManifestError("experiment and lane must be non-empty")
    if target != TARGET_DEVICE:
        raise ManifestError(f"target must be {TARGET_DEVICE}, got {target!r}")

    repository = _root_path(Path(repo_root), label="repository")
    build = _root_path(
        Path(build_root) if build_root is not None else repository / "build",
        label="build",
        create=True,
    )
    output_candidate = _path_argument(Path(output_dir), base=repository)
    output = _safe_path(
        output_candidate,
        label="output directory",
        base=repository,
        allowed_roots=(build,),
        require_exists=False,
    )
    if output.exists() and not output.is_dir():
        raise ManifestError(f"output directory is not a directory: {output_dir}")
    try:
        output.mkdir(parents=True, exist_ok=True)
    except OSError as exc:
        raise ManifestError(f"cannot create output directory {output}: {exc}") from exc

    sources = _file_records(
        source_paths,
        label="source",
        base=repository,
        repo_root=repository,
        build_root=build,
    )
    artifacts = _file_records(
        artifact_paths,
        label="artifact",
        base=repository,
        fallback=output,
        repo_root=repository,
        build_root=build,
        allow_directories=False,
    )
    resolved_command_logs: dict[str, Path] = {}
    command_logs, logged_commands = _command_log_records(
        command_log_paths,
        base=repository,
        fallback=output,
        repo_root=repository,
        build_root=build,
        experiment=experiment,
        lane=lane,
        resolved_paths=resolved_command_logs,
    )
    build_summary = _build_summary_record(
        build_summary_path,
        base=repository,
        repo_root=repository,
        build_root=build,
    )
    manifest_tool_pins = _tool_pins(repository)
    synthesis_report: dict[str, str] | None = None
    if build_summary is not None and experiment == "020_linux_mailbox":
        synthesis_report = _synthesis_report_binding(
            artifacts, experiment=experiment, lane=lane
        )
        build_summary = _augment_mailbox_summary(
            build_summary,
            experiment=experiment,
            lane=lane,
            target=target,
            source_records=sources,
            command_logs=command_logs,
            report=synthesis_report,
            repository=repository,
            resolved_command_logs=resolved_command_logs,
            canonical_tool_pins=manifest_tool_pins,
        )
    all_commands = sorted(dict.fromkeys([*commands, *logged_commands]))

    manifest: dict[str, Any] = {
        "artifacts": artifacts,
        "command_logs": command_logs,
        "commands": all_commands,
        "experiment": experiment,
        "git": _git_state(repository),
        "host": _host(),
        "lane": lane,
        "schema": 2 if build_summary is not None else 1,
        "sources": sources,
        "target": target,
        "timestamp": _timestamp(),
        "tool_pins": manifest_tool_pins,
    }
    if build_summary is not None:
        manifest["build"] = build_summary
        if experiment == "020_linux_mailbox":
            manifest["experiment_policy"] = build_summary["experiment_policy"]
            manifest["experiment_policy_sha256"] = build_summary["experiment_policy_sha256"]
            manifest["policy_sha256"] = build_summary["policy_sha256"]
            manifest["protocol_source_sha256"] = build_summary["protocol_source_sha256"]
            manifest["resource_evidence"] = build_summary["resource_evidence"]
            manifest["synthesis_report"] = build_summary["synthesis_report"]
            manifest["synthesis_report_path"] = build_summary["synthesis_report_path"]
            manifest["synthesis_report_sha256"] = build_summary["synthesis_report_sha256"]
            manifest["resource_evidence_provenance"] = build_summary[
                "resource_evidence_provenance"
            ]

    destination = Path(manifest_path) if manifest_path is not None else output / "manifest.json"
    if not destination.is_absolute():
        destination = _absolute(destination, output if manifest_path is not None else repository)
    destination = _safe_path(
        destination,
        label="manifest",
        base=repository,
        allowed_roots=(output,),
        require_exists=False,
    )
    if destination.exists() and destination.is_symlink():
        raise ManifestError(f"manifest path is a symlink: {destination}")
    if destination.exists() and not destination.is_file():
        raise ManifestError(f"manifest path is not a regular file: {destination}")
    try:
        destination.parent.mkdir(parents=True, exist_ok=True)
        encoded = json.dumps(
            manifest,
            ensure_ascii=False,
            indent=2,
            # Preserve the semantic evidence's explicit field order.  The
            # surrounding manifest remains deterministic because every input
            # record and command list is constructed in stable order.
            sort_keys=False,
        ) + "\n"
        destination.write_text(encoded, encoding="utf-8")
    except OSError as exc:
        raise ManifestError(f"cannot write manifest {destination}: {exc}") from exc
    return manifest


# A descriptive alias is useful to callers that distinguish construction from
# the side effect of writing manifest.json.
build_manifest = collect_manifest


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("positional", nargs="*", metavar="VALUE")
    parser.add_argument("-o", "--output-dir", dest="output_dir")
    parser.add_argument("--experiment")
    parser.add_argument("--lane")
    parser.add_argument("--target", default=TARGET_DEVICE)
    parser.add_argument("--repo-root", type=Path, default=DEFAULT_REPO_ROOT)
    parser.add_argument("--build-root", type=Path)
    parser.add_argument("--source", "--source-path", dest="sources", action="append", type=Path, default=[])
    parser.add_argument(
        "--command-log",
        "--command-log-path",
        "--log",
        dest="command_logs",
        action="append",
        type=Path,
        default=[],
    )
    parser.add_argument("--artifact", "--artifact-path", dest="artifacts", action="append", type=Path, default=[])
    parser.add_argument("--command", action="append", default=[])
    parser.add_argument("--manifest", type=Path)
    parser.add_argument("--build-summary", dest="build_summary", type=Path)
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
    positional = list(arguments.positional)
    if positional:
        parser.error("positional selector form is not supported; use explicit options")
    missing = [field for field in ("output_dir", "experiment", "lane") if not getattr(arguments, field)]
    if missing:
        parser.error("missing required option(s): " + ", ".join("--" + field.replace("_", "-") for field in missing))
    return arguments


def main(argv: Sequence[str] | None = None) -> int:
    try:
        arguments = _arguments(argv)
        collect_manifest(
            Path(arguments.output_dir),
            arguments.experiment,
            arguments.lane,
            arguments.target,
            arguments.sources,
            arguments.command_logs,
            arguments.artifacts,
            repo_root=arguments.repo_root,
            build_root=arguments.build_root,
            commands=arguments.command,
            manifest_path=arguments.manifest,
            build_summary_path=arguments.build_summary,
        )
        return 0
    except ManifestError as exc:
        print(f"collect_manifest: {exc}", file=sys.stderr)
        return 2
    except (OSError, ValueError) as exc:
        print(f"collect_manifest: {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
