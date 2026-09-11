#!/usr/bin/env python3
"""Build and seal the OSS nextpnr/Mistral FES ZX81 package."""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.build_fes_pong import (
    BuildError,
    _authenticate_tools,
    _cell_counts,
    _git,
    _i2c_evidence,
    _read_json,
    _run_tool,
    _sha256,
    _write_atomic,
)
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
OUTPUT_RELATIVE = Path("build/fes-zx81-oss")
RECIPE = "scripts/build_fes_zx81_oss.py"
ABI_DEFINITION = "cores/fes-zx81/generated/fes_simple_computer.vh"
QSF = "cores/fes-zx81/constraints-oss.qsf"
SDC = "cores/fes-zx81/clocks-oss.sdc"
RTL_SOURCES = (
    "cores/fes-zx81/rtl/sys_pll.v",
    "cores/fes-zx81/rtl/pixel_pll.v",
    "cores/fes-zx81/rtl/fes_computer_gp.v",
    "cores/fes-zx81/rtl/zx81_dpram.v",
    "cores/fes-zx81/rtl/zx81_video_720p.v",
    "cores/fes-zx81/rtl/zx81_machine.sv",
    "cores/fes-zx81/rtl/t80pa.v",
    "cores/fes-zx81/rtl/tv80/tv80_core.v",
    "cores/fes-zx81/rtl/tv80/tv80_alu.v",
    "cores/fes-zx81/rtl/tv80/tv80_mcode.v",
    "cores/fes-zx81/rtl/tv80/tv80_reg.v",
    "cores/fes-zx81/rtl/top.v",
)
PINNED_INPUTS = (
    RECIPE,
    ABI_DEFINITION,
    "toolchain.lock",
    QSF,
    SDC,
    "cores/fes-zx81/rtl/zx8x.hex",
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
ORDINARY_RESOURCES = frozenset(
    {
        "MISTRAL_BUF",
        "MISTRAL_CLKENA",
        "MISTRAL_COMB",
        "MISTRAL_FF",
        "MISTRAL_IO",
        "MISTRAL_M10K",
        "MISTRAL_M10K_TDP",
    }
)
REQUIRED_RESOURCES = {
    "altera_pll": 2,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
}
FORBIDDEN_RESOURCES = frozenset(
    {
        "MISTRAL_MLAB",
        "MISTRAL_MUL9X9",
        "MISTRAL_MUL18X18",
        "MISTRAL_MUL27X27",
    }
)
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
HEX40_RE = re.compile(r"[0-9a-f]{40}\Z")


def _regular_input(root: Path, relative: str) -> Path:
    path = root / relative
    current = root
    for part in Path(relative).parts:
        current /= part
        if current.is_symlink():
            raise BuildError(f"pinned input must be a regular non-symlink file: {relative}")
    if not path.is_file() or path.is_symlink():
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


def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
) -> bytes:
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
            "sys_clock_hz": 52_000_000,
            "reference_clock_hz": 50_000_000,
            "seed": 3,
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
    if output != root / OUTPUT_RELATIVE:
        raise BuildError(f"FES ZX81 OSS output must be {root / OUTPUT_RELATIVE}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    sources = " ".join(RTL_SOURCES)
    yosys_program = (
        f"read_verilog -sv -DTV80_REFRESH=1 -DFES_ZX81_OSS=1 -I cores/fes-zx81/generated {sources}; "
        f"chparam -set BUILD_ID 128'h{build_id} {TOP}; "
        f"synth_intel_alm -nolutram -nodsp -top {TOP}; "
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
        "--seed", "3",
        "--router", "router1",
        "--tmg-ripup",
        "--rbf", f"{OUTPUT_RELATIVE.as_posix()}/core.rbf",
        "--compress-rbf",
        "--write", f"{OUTPUT_RELATIVE.as_posix()}/routed.json",
        "--report", f"{OUTPUT_RELATIVE.as_posix()}/timing.json",
        "--detailed-timing-report",
    )
    return yosys, nextpnr


def _prepare_output(root: Path) -> Path:
    output = root / OUTPUT_RELATIVE
    output.mkdir(parents=True, exist_ok=True)
    for name in BUILD_OUTPUTS:
        path = output / name
        if path.exists():
            if path.is_symlink() or not path.is_file():
                raise BuildError(f"build output must be a regular file: {path}")
            path.unlink()
    return output


def _frequency_row(
    fmax: object,
    expected: float,
    label: str,
    name_contains: str | None = None,
) -> tuple[str, float, float]:
    if not isinstance(fmax, dict):
        raise BuildError("timing report has no structured fmax data")
    matches: list[tuple[str, float, float]] = []
    for name, fields in fmax.items():
        if not isinstance(name, str) or not isinstance(fields, dict):
            continue
        if name_contains is not None and name_contains not in name:
            continue
        constraint = fields.get("constraint")
        achieved = fields.get("achieved")
        if not isinstance(constraint, (int, float)) or isinstance(constraint, bool):
            continue
        if not isinstance(achieved, (int, float)) or isinstance(achieved, bool):
            continue
        if not math.isfinite(float(constraint)) or not math.isfinite(float(achieved)):
            raise BuildError(f"{label} timing frequencies must be finite")
        if abs(float(constraint) - expected) <= max(1e-6, expected * 5e-5):
            matches.append((name, float(constraint), float(achieved)))
    if len(matches) != 1:
        raise BuildError(f"timing report must contain exactly one {label} {expected:g} MHz constraint")
    name, constraint, achieved = matches[0]
    if achieved < constraint:
        raise BuildError(
            f"{label} timing achieved {achieved:g} MHz, below reported constraint {constraint!r} MHz"
        )
    return name, constraint, achieved


def validate_build_evidence(output: Path, source_root: Path = ROOT) -> dict:
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    routed = _read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    _i2c_evidence(synthesis, "synthesized")
    _i2c_evidence(routed, "routed")
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    if counts.get("MISTRAL_M10K", 0) + counts.get("MISTRAL_M10K_TDP", 0) < 1:
        raise BuildError("synthesis must map ZX81 RAM onto MISTRAL_M10K")
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise BuildError(f"forbidden synthesis cell {name} is in use")
    route_log = output / "nextpnr.log"
    if route_log.is_symlink() or not route_log.is_file():
        raise BuildError(f"missing route log: {route_log}")
    route_text = route_log.read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    timing = _read_json(output / "timing.json", "timing report")
    system = _frequency_row(timing.get("fmax"), 52.0, "system clock", "clk_sys")
    pixel = _frequency_row(timing.get("fmax"), 74.25, "pixel clock")
    utilization = timing.get("utilization")
    if not isinstance(utilization, dict):
        raise BuildError("timing report has no structured utilization data")
    known = ORDINARY_RESOURCES | set(REQUIRED_RESOURCES) | FORBIDDEN_RESOURCES | REQUIRED_ZERO_RESOURCES
    unknown = sorted(set(utilization) - known)
    if unknown:
        raise BuildError("timing report contains unknown resources: " + ", " .join(unknown))
    resources: dict[str, dict[str, int]] = {}
    for name, fields in sorted(utilization.items()):
        if not isinstance(fields, dict):
            raise BuildError(f"malformed resource evidence: {name}")
        used, available = fields.get("used"), fields.get("available")
        if not isinstance(used, int) or used < 0 or not isinstance(available, int) or available < 0:
            raise BuildError(f"malformed resource counts: {name}")
        resources[name] = {"available": available, "used": used}
    rbf = output / "core.rbf"
    if rbf.is_symlink() or not rbf.is_file() or not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise BuildError(f"RBF must be a nonempty bounded regular file: {rbf}")
    return {
        "status": "pass",
        "route": {"status": "pass", "unrouted": False},
        "timing": {
            "system": {
                "clock": system[0],
                "constraint_mhz": system[1],
                "requested_mhz": 52.0,
                "achieved_mhz": system[2],
                "status": "pass",
            },
            "pixel": {
                "clock": pixel[0],
                "constraint_mhz": pixel[1],
                "requested_mhz": 74.25,
                "achieved_mhz": pixel[2],
                "status": "pass",
            },
            "status": "pass",
        },
        "resources": resources,
        "synthesis_cells": {name: counts[name] for name in sorted(counts)},
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }


def _manifest(
    record: bytes,
    evidence: dict,
    repository: str,
    revision: str,
    tools: Mapping[str, str],
) -> bytes:
    rbf = evidence["rbf"]
    record_fields = json.loads(record)
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    fields = {
        "format": 2,
        "core": {
            "id": "fes.zx81",
            "name": "FES ZX81",
            "description": "Standalone fixed-720p ZX81 for the FES simple-computer ABI (OSS)",
            "version": "1.0.0",
        },
        "target": {
            "platform": "de10_nano",
            "device": TARGET,
            "programming_profile": "fes-gp-v1",
        },
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.simple-computer", "major": 1, "minor": 0},
        "interfaces": [
            {"id": "fes.keyboard", "major": 1, "minor": 0, "required": True},
            {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
            {"id": "fes.media.blob", "major": 1, "minor": 0, "required": True},
        ],
        "build": {
            "id": evidence["build_id"],
            "repository": repository,
            "revision": revision,
            "recipe_sha256": record_fields["recipe_sha256"],
            "toolchain": toolchain,
        },
    }
    return encode_manifest(fields)


def build(root: Path = ROOT, package_store: Path | None = None) -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES ZX81 package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root)
    authenticated = _authenticate_tools(root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    record = create_build_record(root, repository, revision, identities)
    output = _prepare_output(root)
    _write_atomic(output / "build-inputs.json", record)
    try:
        build_id = build_identity(record)
        commands = build_commands(
            root, output, build_id,
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
    except Exception:
        for name in ("core.rbf", "manifest.toml", "build-summary.json"):
            path = output / name
            if path.is_file() or path.is_symlink():
                path.unlink()
        raise


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--print-commands", action="store_true")
    arguments = parser.parse_args(argv)
    try:
        if arguments.print_commands:
            repository, revision = _require_clean_source(arguments.root)
            authenticated = _authenticate_tools(arguments.root)
            identities = {name: tool.identity for name, tool in authenticated.items()}
            record = create_build_record(arguments.root, repository, revision, identities)
            build_id = build_identity(record)
            yosys, nextpnr = build_commands(
                arguments.root.resolve(),
                (arguments.root.resolve() / OUTPUT_RELATIVE),
                build_id,
                {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
            )
            print(" ".join(yosys))
            print(" ".join(nextpnr))
            return 0
        print(build(arguments.root, arguments.package_output))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-zx81-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
