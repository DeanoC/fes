#!/usr/bin/env python3
"""Render non-mutating host, OSS, oracle, and hardware readiness checks."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Sequence


ROOT = Path(__file__).resolve().parents[1]
INSTALL_BIN = ROOT / "build" / "toolchain" / "install" / "bin"
TOOLCHAIN_BUILD = ROOT / "build" / "toolchain" / "build"
TARGET_DEVICE = "5CSEBA6U23I7"
TARGET_IDCODE = "0x02d020dd"
TARGET_IDCODE_VALUE = int(TARGET_IDCODE, 16)
LOADER_BOARD = "de10nano"
LOADER_CABLE = "usb-blasterII"
COMMAND_TIMEOUT = 15.0
VID_PID_RE = re.compile(
    r"(?<![0-9a-f])(?:0x)?([0-9a-f]{4})\s*:\s*(?:0x)?([0-9a-f]{4})(?![0-9a-f])",
    re.I,
)

# Keep this table explicit: the doctor must not search for a different FPGA
# implementation when a pinned repository-local binary is missing.
OSS_TOOLS: tuple[tuple[str, str, tuple[str, ...]], ...] = (
    ("yosys", "Yosys", ("--version",)),
    ("mistral-cv", "Mistral CLI", ()),
    ("nextpnr-mistral", "nextpnr-mistral", ("--version",)),
    ("verilator", "Verilator", ("--version",)),
    ("openFPGALoader", "openFPGALoader", ("--version",)),
)


def make_check(name: str, status: str, detail: str, required: bool) -> dict[str, Any]:
    """Return the stable check shape used by both human and JSON reports."""

    return {
        "name": name,
        "status": status,
        "detail": " ".join(str(detail).split()),
        "required": bool(required),
    }


def _short_output(stdout: str, stderr: str) -> str:
    combined = "\n".join(part for part in (stdout, stderr) if part).strip()
    if not combined:
        return "no output"
    # The full output is deliberately not emitted by the human/JSON doctor:
    # raw CLI evidence belongs in build/toolchain/evidence/.
    lines = [line.strip() for line in combined.splitlines() if line.strip()]
    return lines[0][:400]


@dataclass(frozen=True)
class CommandOutcome:
    """Preserve the distinction between a completed process and launch errors."""

    completed: subprocess.CompletedProcess[str] | None
    error: str | None = None


def run_command(command: Sequence[str], timeout: float = COMMAND_TIMEOUT) -> CommandOutcome:
    """Run one read-only inspection command with a diagnosable timeout."""

    try:
        result = subprocess.run(
            list(command),
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            check=False,
        )
        return CommandOutcome(result)
    except subprocess.TimeoutExpired:
        return CommandOutcome(None, f"timed out after {timeout:g}s")
    except OSError as exc:
        return CommandOutcome(None, f"could not launch command: {exc}")


def _load_pins() -> dict[str, Any]:
    try:
        sys.path.insert(0, str(ROOT / "scripts"))
        from lockfile import load_lock

        return load_lock(ROOT / "toolchain.lock")
    except (ImportError, OSError, ValueError):
        return {}


def _resolve_local_tool(command_name: str, pin: Any | None) -> tuple[Path | None, str | None]:
    """Resolve and authenticate one pinned executable without consulting PATH."""

    path = INSTALL_BIN / command_name
    if pin is None:
        return None, f"no lock entry for {command_name}; repository-local tool is NOT READY"
    if not path.is_file():
        return None, f"repository-local executable missing at {path}; ambient {command_name} ignored"
    if path.is_symlink():
        return None, f"repository-local executable is a symlink at {path}; ambient {command_name} ignored"
    if not os.access(path, os.X_OK):
        return None, f"repository-local executable is not executable at {path}"

    tool = command_name_to_tool(command_name)
    stamp = TOOLCHAIN_BUILD / tool / f".built-{pin.commit}"
    digest_file = TOOLCHAIN_BUILD / tool / f".digest-{pin.commit}.sha256"
    try:
        stamp_value = stamp.read_text(encoding="utf-8").strip()
    except OSError as exc:
        return None, f"missing current-lock stamp {stamp}: {exc}"
    if stamp_value != f"commit={pin.commit}":
        return None, f"stamp {stamp.name} does not authenticate locked commit {pin.commit}"
    try:
        expected = digest_file.read_text(encoding="utf-8").strip()
    except OSError as exc:
        return None, f"missing current-lock digest {digest_file}: {exc}"
    if not re.fullmatch(r"[0-9a-f]{64}", expected):
        return None, f"invalid current-lock digest evidence {digest_file}"
    actual = hashlib.sha256(path.read_bytes()).hexdigest()
    if actual != expected:
        return None, f"binary digest {actual} does not match {digest_file.name}"
    return path, f"locked commit {pin.commit}; digest {actual}"


def _local_tool_check(
    command_name: str,
    display_name: str,
    args: Sequence[str],
    pin: Any | None,
) -> dict[str, Any]:
    path, authentication = _resolve_local_tool(command_name, pin)
    if path is None:
        return make_check(display_name, "NOT READY", authentication or "repository-local tool is NOT READY", True)
    command = [str(path), *args]
    outcome = run_command(command)
    if outcome.completed is None:
        return make_check(display_name, "NOT READY", f"{outcome.error}; {' '.join(command)}", True)
    result = outcome.completed
    detail = f"{' '.join(command)} -> {_short_output(result.stdout, result.stderr)}; {authentication}"
    if result.returncode != 0:
        return make_check(display_name, "NOT READY", f"exit {result.returncode}; {detail}", True)
    return make_check(display_name, "OK", detail, True)


def command_check(
    command_name: str,
    display_name: str,
    args: Sequence[str],
    *,
    required: bool,
    pin: Any | None = None,
) -> dict[str, Any]:
    executable = shutil.which(command_name)
    missing_status = "MISSING" if required else "OPTIONAL MISSING"
    if executable is None:
        return make_check(display_name, missing_status, f"{command_name} not found on PATH", required)

    command = [executable, *args]
    outcome = run_command(command)
    if outcome.completed is None:
        return make_check(display_name, "ERROR", f"{outcome.error}; {' '.join(command)}", required)
    result = outcome.completed
    detail = f"{' '.join(command)} -> {_short_output(result.stdout, result.stderr)}"
    if pin is not None:
        detail += f"; locked commit {pin.commit}"
    if result.returncode != 0:
        return make_check(display_name, "ERROR", f"exit {result.returncode}; {detail}", required)

    return make_check(display_name, "OK", detail, required)


def command_name_to_tool(command_name: str) -> str:
    return {
        "mistral-cv": "mistral",
        "nextpnr-mistral": "nextpnr",
        "openFPGALoader": "openfpgaloader",
    }.get(command_name, command_name)


def host_checks() -> list[dict[str, Any]]:
    host = make_check(
        "Host OS/architecture",
        "OK",
        f"{platform.system()} {platform.release()} ({platform.machine()})",
        True,
    )
    checks = [host]
    for command_name, display_name in (
        ("cmake", "CMake"),
        ("ninja", "Ninja"),
        ("make", "Make"),
        ("cc", "C compiler"),
        ("c++", "C++ compiler"),
        ("python3", "Python"),
        ("git", "Git"),
    ):
        checks.append(command_check(command_name, display_name, ("--version",), required=True))
    return checks


def oss_checks(pins: dict[str, Any]) -> list[dict[str, Any]]:
    checks: list[dict[str, Any]] = []
    for command_name, display_name, args in OSS_TOOLS:
        checks.append(
            _local_tool_check(
                command_name,
                display_name,
                args,
                pins.get(command_name_to_tool(command_name)),
            )
        )
    return checks


def quartus_checks() -> list[dict[str, Any]]:
    """Inspect Quartus only under an explicit QUARTUS_ROOTDIR."""

    quartus_root = os.environ.get("QUARTUS_ROOTDIR", "").strip()
    if not quartus_root:
        return [
            make_check(
                "Quartus",
                "OPTIONAL MISSING",
                "QUARTUS_ROOTDIR is not set; Quartus was not inspected",
                False,
            )
        ]

    root = Path(quartus_root)
    candidates = (root / "bin" / "quartus_sh", root / "quartus_sh")
    executable = next((candidate for candidate in candidates if candidate.is_file()), None)
    if executable is None:
        return [
            make_check(
                "Quartus",
                "OPTIONAL MISSING",
                f"QUARTUS_ROOTDIR={root}; quartus_sh not found",
                False,
            )
        ]
    outcome = run_command((str(executable), "--version"))
    if outcome.completed is None:
        return [make_check("Quartus", "ERROR", f"{outcome.error}; {executable}", False)]
    result = outcome.completed
    detail = f"{executable} --version -> {_short_output(result.stdout, result.stderr)}"
    if result.returncode != 0:
        return [make_check("Quartus", "ERROR", f"exit {result.returncode}; {detail}", False)]
    return [make_check("Quartus", "OK", detail, False)]


def _local_probe(
    command_name: str,
    args: Sequence[str],
    pins: dict[str, Any],
) -> tuple[list[str] | None, str | None, CommandOutcome | None]:
    path, authentication = _resolve_local_tool(
        command_name, pins.get(command_name_to_tool(command_name))
    )
    if path is None:
        return None, authentication, None
    command = [str(path), *args]
    return command, authentication, run_command(command)


def _local_hardware_probe(
    command_name: str,
    args: Sequence[str],
    name: str,
    pins: dict[str, Any],
    *,
    required: bool,
) -> dict[str, Any]:
    command, authentication, outcome = _local_probe(command_name, args, pins)
    if command is None:
        return make_check(name, "NOT READY", authentication or "repository-local tool is NOT READY", required)
    if outcome is None or outcome.completed is None:
        return make_check(name, "NOT READY", f"{outcome.error if outcome else 'no result'}; {' '.join(command)}", required)
    result = outcome.completed
    detail = f"{' '.join(command)} -> {_short_output(result.stdout, result.stderr)}; {authentication}"
    if result.returncode == 0:
        return make_check(name, "OK", detail, required)
    return make_check(name, "NOT READY", f"exit {result.returncode}; {detail}", required)


def _board_attestation_check(expected_board: str | None) -> dict[str, Any]:
    if expected_board is None:
        return make_check(
            "Board identity attestation",
            "NOT READY",
            "board identity not attested; pass --expected-board de10nano for operator attestation",
            True,
        )
    if expected_board != LOADER_BOARD:
        return make_check(
            "Board identity attestation",
            "NOT READY",
            f"unsupported board attestation {expected_board!r}; supported value is {LOADER_BOARD}",
            True,
        )
    return make_check(
        "Board identity attestation",
        "OK",
        "operator-attested board identity: de10nano; cable/JTAG evidence is measured separately and does not establish package/pin equivalence",
        True,
    )


def _parse_cable_scan(output: str, returncode: int, command: Sequence[str]) -> dict[str, Any]:
    rows: list[tuple[str, int, int]] = []
    for line in output.splitlines():
        stripped = line.strip()
        if not stripped or "vid:pid" in stripped.lower():
            continue
        match = VID_PID_RE.search(stripped)
        if match:
            rows.append((stripped, int(match.group(1), 16), int(match.group(2), 16)))
    command_text = " ".join(command)
    if returncode != 0:
        return make_check(
            "Cable detection",
            "NOT READY",
            f"exit {returncode}; {command_text} -> {_short_output(output, '')}",
            True,
        )
    if len(rows) == 0:
        return make_check(
            "Cable detection",
            "NOT READY",
            f"{command_text} found no connected cable",
            True,
        )
    if len(rows) != 1:
        return make_check(
            "Cable detection",
            "NOT READY",
            f"{command_text} found ambiguous multiple cables ({len(rows)})",
            True,
        )
    row, vendor_id, product_id = rows[0]
    expected_cable = (vendor_id, product_id) == (0x09FB, 0x6810)
    if not expected_cable:
        return make_check(
            "Cable detection",
            "NOT READY",
            f"{command_text} found unexpected cable: {row}; expected {LOADER_CABLE}",
            True,
        )
    return make_check(
        "Cable detection",
        "OK",
        f"{command_text} found one {LOADER_CABLE} (VID:PID 0x{vendor_id:04x}:0x{product_id:04x}): {row}",
        True,
    )


def _parse_jtag_chain(output: str, returncode: int, command: Sequence[str]) -> dict[str, Any]:
    command_text = " ".join(command)
    if returncode != 0:
        return make_check(
            "JTAG chain target",
            "NOT READY",
            f"exit {returncode}; {command_text} -> {_short_output(output, '')}",
            True,
        )
    idcode_tokens = re.findall(r"\bidcode\s*[:=]?\s*(0x[0-9a-f]+)", output, re.I)
    idcodes = [int(token, 16) for token in idcode_tokens]
    target_alias = re.findall(rf"\b{re.escape(TARGET_DEVICE)}\b", output, re.I)
    target_model = re.findall(r"\b5CSE\*A6\b", output, re.I)
    if idcodes:
        device_count = len(idcodes)
    else:
        indexed_devices = re.findall(
            r"(?im)^\s*(?:index|device|tap)\s+\d+|^\s*\d+\s*[:)]", output
        )
        device_count = len(indexed_devices) or len(target_alias) or len(target_model)
    if device_count != 1:
        return make_check(
            "JTAG chain target",
            "NOT READY",
            f"{command_text} found multiple or no JTAG chain devices ({device_count}); expected one",
            True,
        )

    target_alias_match = bool(target_alias)
    target_model_match = bool(target_model)
    target_idcode = TARGET_IDCODE_VALUE in idcodes
    if not (target_alias_match or target_model_match or target_idcode):
        observed = ", ".join(f"0x{value:x}" for value in idcodes) if idcodes else "no IDCODE"
        return make_check(
            "JTAG chain target",
            "NOT READY",
            f"{command_text} one device did not match {TARGET_DEVICE} or IDCODE {TARGET_IDCODE}; observed {observed}",
            True,
        )
    evidence = TARGET_DEVICE if target_alias_match else (TARGET_IDCODE if target_idcode else "5CSE*A6")
    return make_check(
        "JTAG chain target",
        "OK",
        f"{command_text} one Cyclone V SoC device matched {evidence}; JTAG evidence identifies silicon family/IDCODE only and does not establish package/pin equivalence",
        True,
    )


def hardware_checks(
    pins: dict[str, Any], expected_board: str | None = None
) -> list[dict[str, Any]]:
    checks: list[dict[str, Any]] = [_board_attestation_check(expected_board)]
    lsusb = shutil.which("lsusb")
    if lsusb is None:
        checks.append(make_check("USB/JTAG visibility", "NOT READY", "lsusb not found", True))
    else:
        outcome = run_command((lsusb,))
        if outcome.completed is None:
            checks.append(make_check("USB/JTAG visibility", "NOT READY", outcome.error or "could not run lsusb", True))
        else:
            result = outcome.completed
            output = "\n".join((result.stdout, result.stderr))
            # A vendor ID plus an Altera/USB-Blaster descriptor is the
            # conservative, unambiguous physical-cable criterion.
            found = re.search(r"09fb:[0-9a-f]{4}.*(?:usb[- ]?blaster|altera|intel)", output, re.I)
            if result.returncode == 0 and found:
                detail = f"{lsusb} -> {found.group(0)}"
                checks.append(make_check("USB/JTAG visibility", "OK", detail, True))
            else:
                checks.append(
                    make_check(
                        "USB/JTAG visibility",
                        "NOT READY",
                        f"{lsusb} found no unambiguous DE10-Nano/USB-Blaster device",
                        True,
                    )
                )

    checks.append(
        _local_hardware_probe(
            "openFPGALoader", ("--list-cables",), "Cable listing", pins, required=False
        )
    )
    scan_command, scan_auth, scan_outcome = _local_probe(
        "openFPGALoader", ("--board", LOADER_BOARD, "--scan-usb"), pins
    )
    if scan_command is None:
        checks.append(make_check("Cable detection", "NOT READY", scan_auth or "repository-local tool is NOT READY", True))
    elif scan_outcome is None or scan_outcome.completed is None:
        checks.append(
            make_check(
                "Cable detection",
                "NOT READY",
                f"{scan_outcome.error if scan_outcome else 'no result'}; {' '.join(scan_command)}; {scan_auth}",
                True,
            )
        )
    else:
        scan_result = scan_outcome.completed
        cable_check = _parse_cable_scan(
            "\n".join((scan_result.stdout, scan_result.stderr)), scan_result.returncode, scan_command
        )
        cable_check["detail"] = f"{cable_check['detail']}; {scan_auth}"
        checks.append(cable_check)

    detect_command, detect_auth, detect_outcome = _local_probe(
        "openFPGALoader", ("--board", LOADER_BOARD, "--detect"), pins
    )
    if detect_command is None:
        checks.append(make_check("JTAG chain target", "NOT READY", detect_auth or "repository-local tool is NOT READY", True))
    elif detect_outcome is None or detect_outcome.completed is None:
        checks.append(
            make_check(
                "JTAG chain target",
                "NOT READY",
                f"{detect_outcome.error if detect_outcome else 'no result'}; {' '.join(detect_command)}; {detect_auth}",
                True,
            )
        )
    else:
        detect_result = detect_outcome.completed
        chain_check = _parse_jtag_chain(
            "\n".join((detect_result.stdout, detect_result.stderr)), detect_result.returncode, detect_command
        )
        chain_check["detail"] = f"{chain_check['detail']}; {detect_auth}"
        checks.append(chain_check)
    return checks


def device_checks(pins: dict[str, Any]) -> list[dict[str, Any]]:
    checks: list[dict[str, Any]] = []

    nextpnr_path, nextpnr_auth = _resolve_local_tool(
        "nextpnr-mistral", pins.get("nextpnr")
    )
    if nextpnr_path is None:
        checks.append(
            make_check(
                "nextpnr device support",
                "NOT READY",
                nextpnr_auth or f"repository-local nextpnr-mistral missing; could not test {TARGET_DEVICE}",
                True,
            )
        )
    else:
        command = (str(nextpnr_path), "--device", TARGET_DEVICE, "--test")
        outcome = run_command(command, timeout=45.0)
        if outcome.completed is not None and outcome.completed.returncode == 0:
            result = outcome.completed
            checks.append(
                make_check(
                    "nextpnr device support",
                    "OK",
                    f"{' '.join(command)} accepted {TARGET_DEVICE}: {_short_output(result.stdout, result.stderr)}; {nextpnr_auth}",
                    True,
                )
            )
        else:
            if outcome.completed is None:
                detail = f"{outcome.error}; {' '.join(command)}; {nextpnr_auth}"
            else:
                result = outcome.completed
                detail = f"exit {result.returncode}; {' '.join(command)} -> {_short_output(result.stdout, result.stderr)}"
            checks.append(make_check("nextpnr device support", "NOT READY", detail, True))

    mistral_path, mistral_auth = _resolve_local_tool("mistral-cv", pins.get("mistral"))
    if mistral_path is None:
        checks.append(
            make_check("Mistral device database", "NOT READY", f"mistral-cv not found; {TARGET_DEVICE} not checked", True)
        )
    else:
        command = (str(mistral_path), "models")
        outcome = run_command(command, timeout=30.0)
        output = "" if outcome.completed is None else "\n".join((outcome.completed.stdout, outcome.completed.stderr))
        if outcome.completed is not None and outcome.completed.returncode == 0 and TARGET_DEVICE in output:
            detail = f"{TARGET_DEVICE} listed by {' '.join(command)}; {mistral_auth}"
            checks.append(make_check("Mistral device database", "OK", detail, True))
        elif outcome.completed is None:
            checks.append(make_check("Mistral device database", "NOT READY", f"{outcome.error}; {' '.join(command)}; {mistral_auth}", True))
        else:
            result = outcome.completed
            checks.append(
                make_check(
                    "Mistral device database",
                    "NOT READY",
                    f"exit {result.returncode}; {TARGET_DEVICE} not listed by {' '.join(command)}; {mistral_auth}",
                    True,
                )
            )

    loader_path, loader_auth = _resolve_local_tool("openFPGALoader", pins.get("openfpgaloader"))
    if loader_path is None:
        checks.append(make_check("openFPGALoader board database", "NOT READY", loader_auth or "openFPGALoader not found", True))
    else:
        command = (str(loader_path), "--list-boards")
        outcome = run_command(command)
        output = "" if outcome.completed is None else "\n".join((outcome.completed.stdout, outcome.completed.stderr))
        if outcome.completed is not None and outcome.completed.returncode == 0 and re.search(r"(?m)^de10nano\s", output):
            checks.append(make_check("openFPGALoader board database", "OK", f"de10nano listed by --list-boards; {loader_auth}", False))
        elif outcome.completed is None:
            checks.append(make_check("openFPGALoader board database", "NOT READY", f"{outcome.error}; {' '.join(command)}; {loader_auth}", False))
        else:
            result = outcome.completed
            checks.append(make_check("openFPGALoader board database", "NOT READY", f"exit {result.returncode}; de10nano not listed by {' '.join(command)}; {loader_auth}", False))
    return checks


def build_report(expected_board: str | None = None) -> dict[str, list[dict[str, Any]]]:
    pins = _load_pins()
    return {
        "host": host_checks(),
        "required_oss": oss_checks(pins),
        "optional_oracle": quartus_checks(),
        "hardware": hardware_checks(pins, expected_board),
        "device": device_checks(pins),
    }


def _group_ready(checks: Sequence[dict[str, Any]]) -> bool:
    return all(check["status"] == "OK" for check in checks if check["required"])


def _oss_ready(report: dict[str, list[dict[str, Any]]]) -> bool:
    return _group_ready(report["host"]) and _group_ready(report["required_oss"])


def _hardware_ready(report: dict[str, list[dict[str, Any]]]) -> bool:
    board = any(
        check["name"] == "Board identity attestation" and check["status"] == "OK"
        for check in report["hardware"]
    )
    cable = any(
        check["name"] == "Cable detection" and check["status"] == "OK"
        for check in report["hardware"]
    )
    chain = any(
        check["name"] == "JTAG chain target" and check["status"] == "OK"
        for check in report["hardware"]
    )
    device = any(
        check["name"] == "nextpnr device support" and check["status"] == "OK"
        for check in report["device"]
    )
    return board and cable and chain and device


def render_human(report: dict[str, list[dict[str, Any]]]) -> str:
    labels = (
        ("HOST", "host"),
        ("REQUIRED OSS", "required_oss"),
        ("OPTIONAL ORACLE", "optional_oracle"),
        ("HARDWARE", "hardware"),
        ("DEVICE", "device"),
    )
    lines = ["Open MiSTer doctor (non-mutating)", f"Target device: {TARGET_DEVICE}"]
    for label, key in labels:
        lines.append("")
        lines.append(label)
        for check in report[key]:
            required = "required" if check["required"] else "optional"
            lines.append(f"  [{check['status']}] {check['name']} ({required}): {check['detail']}")
    return "\n".join(lines) + "\n"


def parse_args(argv: Sequence[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--json", action="store_true", help="write only the structured JSON report")
    parser.add_argument("--strict", choices=("oss", "hardware"), help="return failure when readiness is not met")
    parser.add_argument(
        "--expected-board",
        metavar="BOARD",
        help="operator-attest the board identity (only de10nano is supported)",
    )
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    report = build_report(args.expected_board)
    if args.json:
        print(json.dumps(report, indent=2))
    else:
        print(render_human(report), end="")

    if args.strict == "oss" and not _oss_ready(report):
        return 1
    if args.strict == "hardware" and (not _oss_ready(report) or not _hardware_ready(report)):
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
