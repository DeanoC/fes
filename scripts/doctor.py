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
from pathlib import Path
from typing import Any, Sequence


ROOT = Path(__file__).resolve().parents[1]
INSTALL_BIN = ROOT / "build" / "toolchain" / "install" / "bin"
TOOLCHAIN_BUILD = ROOT / "build" / "toolchain" / "build"
TARGET_DEVICE = "5CSEBA6U23I7"
COMMAND_TIMEOUT = 15.0

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


def run_command(command: Sequence[str], timeout: float = COMMAND_TIMEOUT) -> subprocess.CompletedProcess[str] | None:
    """Run one read-only inspection command, returning None on OS failures."""

    try:
        return subprocess.run(
            list(command),
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=timeout,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None


def _load_pins() -> dict[str, Any]:
    try:
        sys.path.insert(0, str(ROOT / "scripts"))
        from lockfile import load_lock

        return load_lock(ROOT / "toolchain.lock")
    except (ImportError, OSError, ValueError):
        return {}


def _identity_digest(tool: str, executable: str, pin: Any) -> tuple[str, str] | None:
    """Authenticate local binaries when Task 3's digest evidence is present."""

    try:
        resolved = Path(executable).resolve()
        local_bin = INSTALL_BIN.resolve()
        if resolved.parent != local_bin:
            return None
        digest_file = TOOLCHAIN_BUILD / tool / f".digest-{pin.commit}.sha256"
        expected = digest_file.read_text(encoding="utf-8").strip()
        if not re.fullmatch(r"[0-9a-f]{64}", expected):
            return ("ERROR", f"invalid digest evidence {digest_file}")
        digest = hashlib.sha256(resolved.read_bytes()).hexdigest()
        if digest != expected:
            return ("ERROR", f"binary digest {digest} does not match {digest_file.name}")
        return ("OK", f"digest {digest}")
    except (OSError, AttributeError):
        return ("ERROR", "local binary digest evidence is unavailable")


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
    result = run_command(command)
    if result is None:
        return make_check(display_name, "ERROR", f"could not run {' '.join(command)}", required)
    detail = f"{' '.join(command)} -> {_short_output(result.stdout, result.stderr)}"
    if pin is not None:
        detail += f"; locked commit {pin.commit}"
    if result.returncode != 0:
        return make_check(display_name, "ERROR", f"exit {result.returncode}; {detail}", required)

    if pin is not None:
        digest = _identity_digest(command_name_to_tool(command_name), executable, pin)
        if digest is not None:
            digest_status, digest_detail = digest
            detail += f"; {digest_detail}"
            if digest_status != "OK":
                return make_check(display_name, digest_status, detail, required)
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
            command_check(
                command_name,
                display_name,
                args,
                required=True,
                pin=pins.get(command_name_to_tool(command_name)),
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
    result = run_command((str(executable), "--version"))
    if result is None:
        return [make_check("Quartus", "ERROR", f"could not run {executable}", False)]
    detail = f"{executable} --version -> {_short_output(result.stdout, result.stderr)}"
    if result.returncode != 0:
        return [make_check("Quartus", "ERROR", f"exit {result.returncode}; {detail}", False)]
    return [make_check("Quartus", "OK", detail, False)]


def _read_only_hardware_probe(
    command_name: str, args: Sequence[str], name: str, *, required: bool = True
) -> dict[str, Any]:
    executable = shutil.which(command_name)
    if executable is None:
        return make_check(name, "NOT READY", f"{command_name} not found; no hardware probe ran", required)
    command = [executable, *args]
    result = run_command(command)
    if result is None:
        return make_check(name, "NOT READY", f"could not run {' '.join(command)}", required)
    detail = f"{' '.join(command)} -> {_short_output(result.stdout, result.stderr)}"
    if result.returncode == 0:
        return make_check(name, "OK", detail, required)
    return make_check(name, "NOT READY", f"exit {result.returncode}; {detail}", required)


def hardware_checks() -> list[dict[str, Any]]:
    checks: list[dict[str, Any]] = []
    lsusb = shutil.which("lsusb")
    if lsusb is None:
        checks.append(make_check("USB/JTAG visibility", "NOT READY", "lsusb not found", True))
    else:
        result = run_command((lsusb,))
        if result is None:
            checks.append(make_check("USB/JTAG visibility", "NOT READY", "could not run lsusb", True))
        else:
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
        _read_only_hardware_probe(
            "openFPGALoader", ("--list-cables",), "Cable listing", required=False
        )
    )
    checks.append(
        _read_only_hardware_probe("openFPGALoader", ("--detect",), "JTAG detect", required=True)
    )
    return checks


def device_checks() -> list[dict[str, Any]]:
    checks: list[dict[str, Any]] = []

    nextpnr = shutil.which("nextpnr-mistral")
    if nextpnr is None:
        checks.append(
            make_check(
                "nextpnr device support",
                "NOT READY",
                f"nextpnr-mistral not found; could not test {TARGET_DEVICE}",
                True,
            )
        )
    else:
        command = (nextpnr, "--device", TARGET_DEVICE, "--test")
        result = run_command(command, timeout=45.0)
        if result is not None and result.returncode == 0:
            checks.append(
                make_check(
                    "nextpnr device support",
                    "OK",
                    f"{' '.join(command)} accepted {TARGET_DEVICE}: {_short_output(result.stdout, result.stderr)}",
                    True,
                )
            )
        else:
            if result is None:
                detail = f"could not run {' '.join(command)}"
            else:
                detail = f"exit {result.returncode}; {' '.join(command)} -> {_short_output(result.stdout, result.stderr)}"
            checks.append(make_check("nextpnr device support", "NOT READY", detail, True))

    mistral = shutil.which("mistral-cv")
    if mistral is None:
        checks.append(
            make_check("Mistral device database", "NOT READY", f"mistral-cv not found; {TARGET_DEVICE} not checked", True)
        )
    else:
        command = (mistral, "models")
        result = run_command(command, timeout=30.0)
        output = "" if result is None else "\n".join((result.stdout, result.stderr))
        if result is not None and result.returncode == 0 and TARGET_DEVICE in output:
            detail = f"{TARGET_DEVICE} listed by {' '.join(command)}"
            checks.append(make_check("Mistral device database", "OK", detail, True))
        elif result is None:
            checks.append(make_check("Mistral device database", "NOT READY", f"could not run {' '.join(command)}", True))
        else:
            checks.append(
                make_check(
                    "Mistral device database",
                    "NOT READY",
                    f"exit {result.returncode}; {TARGET_DEVICE} not listed by {' '.join(command)}",
                    True,
                )
            )

    loader = shutil.which("openFPGALoader")
    if loader is None:
        checks.append(make_check("openFPGALoader board database", "NOT READY", "openFPGALoader not found", True))
    else:
        command = (loader, "--list-boards")
        result = run_command(command)
        output = "" if result is None else "\n".join((result.stdout, result.stderr))
        if result is not None and result.returncode == 0 and re.search(r"(?m)^de10nano\s", output):
            checks.append(make_check("openFPGALoader board database", "OK", "de10nano listed by --list-boards", False))
        elif result is None:
            checks.append(make_check("openFPGALoader board database", "NOT READY", f"could not run {' '.join(command)}", False))
        else:
            checks.append(make_check("openFPGALoader board database", "NOT READY", f"de10nano not listed by {' '.join(command)}", False))
    return checks


def build_report() -> dict[str, list[dict[str, Any]]]:
    pins = _load_pins()
    return {
        "host": host_checks(),
        "required_oss": oss_checks(pins),
        "optional_oracle": quartus_checks(),
        "hardware": hardware_checks(),
        "device": device_checks(),
    }


def _group_ready(checks: Sequence[dict[str, Any]]) -> bool:
    return all(check["status"] == "OK" for check in checks if check["required"])


def _oss_ready(report: dict[str, list[dict[str, Any]]]) -> bool:
    return _group_ready(report["host"]) and _group_ready(report["required_oss"])


def _hardware_ready(report: dict[str, list[dict[str, Any]]]) -> bool:
    visible = any(
        check["name"] == "USB/JTAG visibility" and check["status"] == "OK"
        for check in report["hardware"]
    )
    detected = any(
        check["name"] == "JTAG detect" and check["status"] == "OK"
        for check in report["hardware"]
    )
    device = any(
        check["name"] == "nextpnr device support" and check["status"] == "OK"
        for check in report["device"]
    )
    return visible and detected and device


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
    return parser.parse_args(argv)


def main(argv: Sequence[str] | None = None) -> int:
    args = parse_args(argv)
    report = build_report()
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
