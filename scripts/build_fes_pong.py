#!/usr/bin/env python3
"""Build and seal the deterministic standalone FES Pong package."""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package
from scripts.lockfile import LockfileError, load_lock


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
OUTPUT_RELATIVE = Path("build/fes-pong")
QSF = "cores/fes-pong/constraints.qsf"
SDC = "boards/de10nano/clocks.sdc"
RECIPE = "scripts/build_fes_pong.py"
ABI_DEFINITION = "cores/fes-pong/generated/fes_gp.vh"
RTL_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-pong/rtl/top.v",
    "cores/fes-pong/rtl/fes_gp.v",
    "cores/fes-pong/rtl/video_720p.v",
    "cores/pong/rtl/pong_game.sv",
)
PINNED_INPUTS = (
    RECIPE,
    ABI_DEFINITION,
    "toolchain.lock",
    QSF,
    SDC,
    *RTL_SOURCES,
)
BUILD_OUTPUTS = (
    "synth.json",
    "routed.json",
    "core.rbf",
    "timing.json",
    "yosys.log",
    "nextpnr.log",
    "build-summary.json",
    "manifest.toml",
)
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
HEX40_RE = re.compile(r"[0-9a-f]{40}\Z")
HEX64_RE = re.compile(r"[0-9a-f]{64}\Z")
ORDINARY_RESOURCES = frozenset(
    {"MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO"}
)
REQUIRED_RESOURCES = {
    "altera_pll": 1,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
}
FORBIDDEN_RESOURCES = frozenset(
    {
        "MISTRAL_M10K",
        "MISTRAL_MLAB",
        "MISTRAL_MUL9X9",
        "MISTRAL_MUL18X18",
        "MISTRAL_MUL27X27",
    }
)
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
REFERENCE_SDC_BYTES = (
    b"# 50 MHz DE10-Nano input clock.\n"
    b"create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]\n"
)
PLL_PARAMETERS = {
    "duty_cycle0": "00000000000000000000000000110010",
    "fractional_vco_multiplier": "true",
    "number_of_clocks": "00000000000000000000000000000001",
    "operation_mode": "direct",
    "output_clock_frequency0": "74.25 MHz",
    "phase_shift0": "0 ps",
    "reference_clock_frequency": "50.0 MHz",
}
REFERENCE_CONSTRAINT_LOG = "Info: constraining clock net 'FPGA_CLK1_50' to 50.00 MHz"
PLL_ROUTE_LOG = (
    "Info: PLL 'video_clock.pll': 50 MHz -> 74.25 MHz, direct, "
    "M=8 N=1 C6=6, bel altera_pll.0.14.0"
)
EXPECTED_TOOL_COMMITS = {
    "mistral": "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
    "nextpnr": "9144784db9b4b85c1257be4d1943b9ac11ceb3e8",
    "yosys": "fca8ca0a5354e52ce0e158bc6e1eed481e590ed8",
}


class BuildError(ValueError):
    """Raised when the build cannot produce authenticated passing evidence."""


@dataclass(frozen=True)
class AuthenticatedTool:
    path: Path
    identity: str


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _git(root: Path, *arguments: str) -> str:
    try:
        result = subprocess.run(
            ["git", "-C", str(root), *arguments],
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = exc.stderr.strip() if isinstance(exc, subprocess.CalledProcessError) else str(exc)
        raise BuildError(f"Git source verification failed: {detail}") from exc
    return result.stdout.strip()


def _contains_symlink(root: Path, relative: str) -> bool:
    current = root
    for part in Path(relative).parts:
        current /= part
        if current.is_symlink():
            return True
    return False


def _regular_input(root: Path, relative: str) -> Path:
    path = root / relative
    if _contains_symlink(root, relative) or not path.is_file():
        raise BuildError(f"pinned input must be a regular non-symlink file: {relative}")
    return path


def _require_clean_source(root: Path) -> tuple[str, str]:
    root = Path(root).resolve()
    actual_root = Path(_git(root, "rev-parse", "--show-toplevel")).resolve()
    if actual_root != root:
        raise BuildError(f"source root does not match Git checkout root: {root}")
    revision = _git(root, "rev-parse", "HEAD")
    if HEX40_RE.fullmatch(revision) is None:
        raise BuildError("source HEAD is not a full lowercase Git commit")
    if _git(root, "status", "--porcelain", "--untracked-files=all"):
        raise BuildError("source checkout must be clean before build and export")
    repositories = _git(root, "remote", "get-url", "--all", "origin").splitlines()
    if len(repositories) != 1:
        raise BuildError("source checkout must have exactly one origin URL")
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
        try:
            _git(root, "ls-files", "--error-unmatch", "--", relative)
        except BuildError as exc:
            raise BuildError(f"pinned build input is not tracked: {relative}") from exc
    return repositories[0], revision


def _read_evidence(path: Path, expected: str) -> str:
    if path.is_symlink() or not path.is_file():
        raise BuildError(f"missing tool authentication evidence: {path}")
    try:
        value = path.read_text(encoding="utf-8").strip()
    except OSError as exc:
        raise BuildError(f"cannot read tool authentication evidence: {path}") from exc
    if expected == "digest" and HEX64_RE.fullmatch(value) is None:
        raise BuildError(f"invalid tool digest evidence: {path}")
    return value


def _authenticate_tools(root: Path) -> dict[str, AuthenticatedTool]:
    try:
        pins = load_lock(root / "toolchain.lock")
    except (OSError, LockfileError, ValueError) as exc:
        raise BuildError(f"cannot load pinned toolchain: {exc}") from exc
    install = root / "build/toolchain/install/bin"
    build_root = root / "build/toolchain/build"
    definitions = (
        ("yosys", "yosys", "yosys", ("--version",)),
        ("mistral", "mistral", "mistral-cv", ("models",)),
        ("nextpnr-mistral", "nextpnr", "nextpnr-mistral", ("--version",)),
    )
    authenticated: dict[str, AuthenticatedTool] = {}
    for record_name, lock_name, executable, arguments in definitions:
        pin = pins[lock_name]
        if pin.commit != EXPECTED_TOOL_COMMITS[lock_name]:
            raise BuildError(
                f"Task10 requires {lock_name} commit {EXPECTED_TOOL_COMMITS[lock_name]}, "
                f"got {pin.commit}"
            )
        path = install / executable
        if path.is_symlink() or not path.is_file() or not os.access(path, os.X_OK):
            raise BuildError(f"pinned tool is not a regular executable: {path}")
        stamp = build_root / lock_name / f".built-{pin.commit}"
        digest_path = build_root / lock_name / f".digest-{pin.commit}.sha256"
        if _read_evidence(stamp, "stamp") != f"commit={pin.commit}":
            raise BuildError(f"tool build stamp does not match locked commit: {executable}")
        expected_digest = _read_evidence(digest_path, "digest")
        actual_digest = _sha256(path)
        if actual_digest != expected_digest:
            raise BuildError(f"tool executable digest does not match lock evidence: {executable}")
        try:
            result = subprocess.run(
                [str(path), *arguments],
                cwd=root,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=30.0,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise BuildError(f"cannot execute authenticated tool identity check: {executable}") from exc
        output = "\n".join((result.stdout, result.stderr)).strip()
        if result.returncode != 0 or not output:
            raise BuildError(f"authenticated tool identity check failed: {executable}")
        if lock_name == "mistral" and TARGET not in output:
            raise BuildError(f"authenticated Mistral database does not list {TARGET}")
        authenticated[record_name] = AuthenticatedTool(
            path=path,
            identity=f"commit={pin.commit}; sha256={actual_digest}",
        )
    return authenticated


def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
) -> bytes:
    root = Path(root)
    fields = {
        "format": 1,
        "repository": repository,
        "revision": revision,
        "recipe": RECIPE,
        "recipe_sha256": _sha256(_regular_input(root, RECIPE)),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": _sha256(_regular_input(root, ABI_DEFINITION)),
        "dependencies": {},
        "tools": dict(tool_identities),
        "parameters": {
            "device": TARGET,
            "pixel_clock_hz": 74_250_000,
            "pll_fractional_vco_multiplier": True,
            "reference_clock_hz": 50_000_000,
            "seed": 1,
            "top": TOP,
        },
    }
    return encode_build_record(fields)


def build_commands(
    root: Path,
    output: Path,
    build_id: str,
    tools: Mapping[str, Path],
) -> tuple[tuple[str, ...], tuple[str, ...]]:
    root = Path(root).resolve()
    output = Path(output).resolve()
    if output != root / OUTPUT_RELATIVE:
        raise BuildError(f"FES Pong output must be {root / OUTPUT_RELATIVE}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    sources = " ".join(RTL_SOURCES)
    yosys_program = (
        f"read_verilog -sv -I cores/fes-pong/generated {sources}; "
        f"chparam -set BUILD_ID 128'h{build_id} {TOP}; "
        f"synth_intel_alm -nobram -nolutram -nodsp -top {TOP}; "
        f"stat; write_json {OUTPUT_RELATIVE.as_posix()}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", yosys_program)
    nextpnr = (
        str(tools["nextpnr-mistral"]),
        "--json", f"{OUTPUT_RELATIVE.as_posix()}/synth.json",
        "--device", TARGET,
        "--qsf", QSF,
        "--sdc", SDC,
        "--freq", "74.25",
        "--seed", "1",
        "--rbf", f"{OUTPUT_RELATIVE.as_posix()}/core.rbf",
        "--compress-rbf",
        "--write", f"{OUTPUT_RELATIVE.as_posix()}/routed.json",
        "--report", f"{OUTPUT_RELATIVE.as_posix()}/timing.json",
        "--detailed-timing-report",
    )
    return yosys, nextpnr


def _write_atomic(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.is_symlink() or (path.exists() and not path.is_file()):
        raise BuildError(f"build output is not a regular file: {path}")
    descriptor, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    temporary = Path(temporary_name)
    try:
        with os.fdopen(descriptor, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary.exists():
            temporary.unlink()


def _prepare_output(root: Path) -> Path:
    output = root / OUTPUT_RELATIVE
    build_root = root / "build"
    if build_root.is_symlink() or (build_root.exists() and not build_root.is_dir()):
        raise BuildError(f"build root must be a non-symlink directory: {build_root}")
    if output.is_symlink() or (output.exists() and not output.is_dir()):
        raise BuildError(f"FES Pong output must be a non-symlink directory: {output}")
    output.mkdir(parents=True, exist_ok=True)
    for name in BUILD_OUTPUTS:
        path = output / name
        if path.is_symlink() or (path.exists() and not path.is_file()):
            raise BuildError(f"build output must be a regular file: {path}")
        if path.exists():
            path.unlink()
    return output


def _run_tool(command: tuple[str, ...], cwd: Path, log: Path) -> None:
    try:
        result = subprocess.run(
            list(command),
            cwd=cwd,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
        )
    except OSError as exc:
        raise BuildError(f"cannot run authenticated tool: {command[0]}") from exc
    _write_atomic(log, result.stdout)
    if result.returncode != 0:
        raise BuildError(f"tool failed with exit {result.returncode}: {command[0]}; see {log}")


def _read_json(path: Path, label: str) -> dict:
    if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
        raise BuildError(f"missing nonempty {label}: {path}")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BuildError(f"invalid {label}: {path}") from exc
    if not isinstance(value, dict):
        raise BuildError(f"{label} must be a JSON object")
    return value


def _cell_counts(synthesis: dict) -> dict[str, int]:
    modules = synthesis.get("modules")
    if not isinstance(modules, dict):
        raise BuildError("synthesis evidence has no modules")
    counts: dict[str, int] = {}
    for module in modules.values():
        if not isinstance(module, dict) or not isinstance(module.get("cells"), dict):
            continue
        for cell in module["cells"].values():
            if isinstance(cell, dict) and isinstance(cell.get("type"), str):
                name = cell["type"]
                counts[name] = counts.get(name, 0) + 1
    return counts


def _frequency_rows(fmax: object, expected: float, label: str) -> tuple[str, float, float]:
    if not isinstance(fmax, dict):
        raise BuildError("timing report has no structured fmax data")
    matches: list[tuple[str, float, float]] = []
    for name, fields in fmax.items():
        if not isinstance(name, str) or not isinstance(fields, dict):
            continue
        constraint = fields.get("constraint")
        achieved = fields.get("achieved")
        if (
            isinstance(constraint, bool)
            or not isinstance(constraint, (int, float))
            or isinstance(achieved, bool)
            or not isinstance(achieved, (int, float))
        ):
            continue
        if not math.isfinite(float(constraint)) or not math.isfinite(float(achieved)):
            raise BuildError(f"{label} timing frequencies must be finite")
        tolerance = max(1e-6, expected * 5e-5)
        if abs(float(constraint) - expected) <= tolerance:
            matches.append((name, float(constraint), float(achieved)))
    if len(matches) != 1:
        raise BuildError(f"timing report must contain exactly one {label} {expected:g} MHz constraint")
    name, constraint, achieved = matches[0]
    if achieved < constraint:
        raise BuildError(
            f"{label} timing achieved {achieved:g} MHz, below reported constraint {constraint!r} MHz"
        )
    return name, constraint, achieved


def _pll_cell_parameters(design: dict, label: str) -> None:
    modules = design.get("modules")
    top = modules.get(TOP) if isinstance(modules, dict) else None
    cells = top.get("cells") if isinstance(top, dict) else None
    matches = [
        cell
        for cell in cells.values()
        if isinstance(cell, dict) and cell.get("type") == "altera_pll"
    ] if isinstance(cells, dict) else []
    if len(matches) != 1 or matches[0].get("parameters") != PLL_PARAMETERS:
        raise BuildError(f"{label} PLL parameters do not match the fixed 50-to-74.25 MHz profile")


def _reference_clock_evidence(source_root: Path, route_text: str) -> dict[str, object]:
    sdc = _regular_input(source_root, SDC)
    if sdc.read_bytes() != REFERENCE_SDC_BYTES:
        raise BuildError("tracked SDC does not contain the exact FPGA_CLK1_50 20.000 ns constraint")
    if route_text.count(REFERENCE_CONSTRAINT_LOG) != 1:
        raise BuildError("route log must apply the FPGA_CLK1_50 50.00 MHz constraint exactly once")
    if route_text.count(PLL_ROUTE_LOG) != 1:
        raise BuildError("route log does not contain the expected fixed fractional PLL mapping")
    return {
        "clock": "FPGA_CLK1_50",
        "constraint_mhz": 50.0,
        "evidence": "boards/de10nano/clocks.sdc and routed PLL",
        "requested_mhz": 50.0,
        "status": "pass",
    }


def _i2c_evidence(design: dict, label: str) -> None:
    module = design.get("modules", {}).get(TOP, {})
    cells = module.get("cells", {})
    bridges = [cell for cell in cells.values()
               if cell.get("type") == "cyclonev_hps_interface_peripheral_i2c"]
    if len(bridges) != 1:
        raise BuildError(f"{label} HDMI I2C requires exactly one HPS bridge")
    bridge = bridges[0]
    site = "cyclonev_hps_interface_peripheral_i2c.52.60.0"
    placement = "NEXTPNR_BEL" if label == "routed" else "BEL"
    if bridge.get("attributes", {}).get(placement) != site:
        raise BuildError(f"{label} HDMI I2C must use HPS site X52 Y60")
    connections = bridge.get("connections", {})
    if (set(connections) != {"out_clk", "out_data", "scl", "sda"}
            or any(not isinstance(bits, list) or len(bits) != 1
                   or type(bits[0]) is not int for bits in connections.values())
            or len({bits[0] for bits in connections.values()}) != 4):
        raise BuildError(f"{label} HDMI I2C requires four distinct signal nets")
    grounds = [["0"]]
    if label == "routed":
        grounds += [cell.get("connections", {}).get("Q") for cell in cells.values()
                    if cell.get("type") == "MISTRAL_CONST"
                    and re.fullmatch("0+", str(cell.get("parameters", {}).get("LUT", "")))
                    and isinstance(cell.get("connections", {}).get("Q"), list)
                    and len(cell["connections"]["Q"]) == 1]
    for name, enable, feedback, pin, bel in (
        ("hdmi_scl_pad", "out_clk", "scl", "PIN_U10", "MISTRAL_IO.6.0.0"),
        ("hdmi_sda_pad", "out_data", "sda", "PIN_AA4", "MISTRAL_IO.4.0.2"),
    ):
        pad = cells.get(name, {})
        ports = pad.get("connections", {})
        if (pad.get("type") != "MISTRAL_IO" or ports.get("I") not in grounds
                or ports.get("OE") != connections[enable]
                or ports.get("O") != connections[feedback]):
            raise BuildError(f"{label} HDMI I2C {name} must drive low or release with pad feedback")
        port_name = "HDMI_I2C_SCL" if enable == "out_clk" else "HDMI_I2C_SDA"
        port = module.get("ports", {}).get(port_name, {})
        if (port.get("direction") != "inout" or not isinstance(port.get("bits"), list)
                or len(port["bits"]) != 1 or ports.get("PAD") != port["bits"]):
            raise BuildError(f"{label} HDMI I2C {name} must connect its bidirectional pad")
        if label == "routed" and (
                pad.get("attributes", {}).get("LOC") != pin
                or pad.get("attributes", {}).get("NEXTPNR_BEL") != bel):
            raise BuildError(f"routed HDMI I2C {name} must use {pin}")


def validate_build_evidence(output: Path, source_root: Path = ROOT) -> dict:
    output = Path(output)
    source_root = Path(source_root)
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    routed = _read_json(output / "routed.json", "routed design")
    routed_modules = routed.get("modules")
    if not isinstance(routed_modules, dict) or not isinstance(routed_modules.get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    _pll_cell_parameters(synthesis, "synthesized")
    _pll_cell_parameters(routed, "routed")
    _i2c_evidence(synthesis, "synthesized")
    _i2c_evidence(routed, "routed")
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise BuildError(f"forbidden synthesis cell {name} is in use")

    route_log = output / "nextpnr.log"
    if route_log.is_symlink() or not route_log.is_file():
        raise BuildError(f"missing route log: {route_log}")
    route_text = route_log.read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    reference = _reference_clock_evidence(source_root, route_text)

    timing = _read_json(output / "timing.json", "timing report")
    fmax = timing.get("fmax")
    if not isinstance(fmax, dict) or len(fmax) != 1:
        raise BuildError("timing report must contain the single pixel sequential domain")
    pixel = _frequency_rows(fmax, 74.25, "pixel clock")
    utilization = timing.get("utilization")
    if not isinstance(utilization, dict):
        raise BuildError("timing report has no structured utilization data")
    resources: dict[str, dict[str, int]] = {}
    known = (
        ORDINARY_RESOURCES
        | set(REQUIRED_RESOURCES)
        | FORBIDDEN_RESOURCES
        | REQUIRED_ZERO_RESOURCES
    )
    unknown = sorted(set(utilization) - known)
    if unknown:
        raise BuildError("timing report contains unknown resources: " + ", ".join(unknown))
    for name, fields in sorted(utilization.items()):
        if not isinstance(fields, dict):
            raise BuildError(f"malformed resource evidence: {name}")
        used = fields.get("used")
        available = fields.get("available")
        if (
            isinstance(used, bool)
            or not isinstance(used, int)
            or used < 0
            or isinstance(available, bool)
            or not isinstance(available, int)
            or available < 0
        ):
            raise BuildError(f"malformed resource counts: {name}")
        resources[name] = {"available": available, "used": used}
    for name, expected in REQUIRED_RESOURCES.items():
        if name not in resources or resources[name]["used"] != expected:
            actual = "missing" if name not in resources else str(resources[name]["used"])
            raise BuildError(f"resource {name} must be exactly {expected}, got {actual}")
    for name in REQUIRED_ZERO_RESOURCES:
        if name not in resources or resources[name]["used"] != 0:
            actual = "missing" if name not in resources else str(resources[name]["used"])
            raise BuildError(f"resource {name} must be exactly 0, got {actual}")
    for name in FORBIDDEN_RESOURCES:
        if name in resources and resources[name]["used"] != 0:
            raise BuildError(f"forbidden resource {name} is in use")

    rbf = output / "core.rbf"
    if rbf.is_symlink() or not rbf.is_file() or not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise BuildError(f"RBF must be a nonempty bounded regular file: {rbf}")
    return {
        "status": "pass",
        "route": {"status": "pass", "unrouted": False},
        "timing": {
            "pixel": {
                "clock": pixel[0],
                "constraint_mhz": pixel[1],
                "requested_mhz": 74.25,
                "achieved_mhz": pixel[2],
                "status": "pass",
            },
            "reference": reference,
            "status": "pass",
        },
        "resources": resources,
        "synthesis_cells": {name: counts[name] for name in sorted(counts)},
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }


def _manifest(record: bytes, evidence: dict, repository: str, revision: str, tools: Mapping[str, str]) -> bytes:
    record_fields = json.loads(record)
    rbf = evidence["rbf"]
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    fields = {
        "format": 2,
        "core": {
            "id": "fes.pong",
            "name": "FES Pong",
            "description": "Standalone fixed-720p Pong for the FES general-purpose ABI",
            "version": "1.0.0",
        },
        "target": {
            "platform": "de10_nano",
            "device": TARGET,
            "programming_profile": "fes-gp-v1",
        },
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.simple-game", "major": 1, "minor": 0},
        "interfaces": [
            {"id": "fes.gamepad", "major": 1, "minor": 0, "required": True},
            {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
        ],
        "build": {
            "id": build_identity(record),
            "repository": repository,
            "revision": revision,
            "recipe_sha256": record_fields["recipe_sha256"],
            "toolchain": toolchain,
        },
    }
    return encode_manifest(fields)


def _build_after_record(
    root: Path,
    package_store: Path,
    repository: str,
    revision: str,
    authenticated: Mapping[str, AuthenticatedTool],
    identities: Mapping[str, str],
    record: bytes,
    output: Path,
) -> Path:
    build_id = build_identity(record)
    commands = build_commands(
        root,
        output,
        build_id,
        {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
    )
    _run_tool(commands[0], root, output / "yosys.log")
    if not (output / "synth.json").is_file():
        raise BuildError("Yosys did not produce synthesis evidence")
    _run_tool(commands[1], root, output / "nextpnr.log")
    evidence = validate_build_evidence(output, root)
    evidence.update(
        {
            "build_id": build_id,
            "device": TARGET,
            "inputs": {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)},
            "tools": identities,
            "top": TOP,
        }
    )
    _write_atomic(
        output / "build-summary.json",
        (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8"),
    )
    manifest = _manifest(record, evidence, repository, revision, identities)
    _write_atomic(output / "manifest.toml", manifest)
    final_tools = _authenticate_tools(root)
    if {name: tool.identity for name, tool in final_tools.items()} != identities:
        raise BuildError("authenticated tool identity changed during build")
    final_repository, final_revision = _require_clean_source(root)
    if (final_repository, final_revision) != (repository, revision):
        raise BuildError("source identity changed during build")
    return export_package(manifest, output / "core.rbf", package_store)


def _invalidate_failed_artifact(output: Path) -> None:
    for name in ("core.rbf", "manifest.toml", "build-summary.json"):
        path = output / name
        if path.is_symlink() or path.is_file():
            path.unlink()
        elif path.exists():
            raise BuildError(f"cannot invalidate non-file failed build output: {path}")


def build(root: Path = ROOT, package_store: Path | None = None) -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES Pong package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root)
    authenticated = _authenticate_tools(root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    record = create_build_record(root, repository, revision, identities)
    output = _prepare_output(root)
    _write_atomic(output / "build-inputs.json", record)
    try:
        return _build_after_record(
            root,
            package_store,
            repository,
            revision,
            authenticated,
            identities,
            record,
            output,
        )
    except Exception:
        _invalidate_failed_artifact(output)
        raise


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    arguments = _parser().parse_args(argv)
    try:
        print(build(arguments.root, arguments.package_output))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-pong: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
