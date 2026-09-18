#!/usr/bin/env python3
"""Build and seal the OSS nextpnr/Mistral FES Master System package (fes.sms)."""

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
    _require_gpu_backend,
    _run_tool,
    _sha256,
    _write_atomic,
)
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
ROUTER = "gpu"
# Seed 1 is the fes.sms production seed. Coleco / SG-1000 remain seed 4.
# A sealed BUILD_ID changes the placement search space; re-check after
# format-2 seal.
SEED = 1
SMS_GPU_BACKEND = "hip"
SMS_GPU_ROUTER = "HIP"
SMS_GPU_ARCHITECTURES = "gfx1100;gfx1201"
SMS_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={SMS_GPU_ROUTER}; hip-architectures={SMS_GPU_ARCHITECTURES}"
)
OUTPUT_RELATIVE = Path("build/fes-sms-oss")
SMS_TOOLCHAIN_LOCK = "cores/fes-sms/toolchain.lock"
SMS_TOOLCHAIN_ROOT = "build/toolchain/fes-sms"
SMS_TOOL_COMMITS = {
    "mistral": "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
    "nextpnr": "0fad53a75a0218941c417ec6bb58bdede9070987",
    "yosys": "e2d425dee148cc60c50f4e9b354a10d90eab15f4",
}
RECIPE = "scripts/build_fes_sms_oss.py"
ABI_DEFINITION = "cores/fes-sms/generated/fes_simple_computer.vh"
QSF = "cores/fes-sms/constraints-oss.qsf"
SDC = "cores/fes-sms/clocks-oss.sdc"
RTL_SOURCES = (
    "cores/fes-coleco/rtl/sys_pll.v",
    "cores/fes-coleco/rtl/pixel_pll.v",
    "cores/fes-coleco/rtl/fes_computer_gp.v",
    "cores/fes-coleco/rtl/coleco_dpram.v",
    "cores/fes-coleco/rtl/coleco_video_dpram.v",
    "cores/fes-coleco/rtl/coleco_vdp.sv",
    "cores/fes-coleco/rtl/coleco_video_720p.v",
    "cores/fes-sms/rtl/sms_machine.sv",
    "cores/fes-coleco/rtl/t80pa.v",
    "cores/fes-coleco/rtl/tv80/tv80_core.v",
    "cores/fes-coleco/rtl/tv80/tv80_alu.v",
    "cores/fes-coleco/rtl/tv80/tv80_mcode.v",
    "cores/fes-coleco/rtl/tv80/tv80_reg.v",
    "cores/fes-sms/rtl/top.v",
)
PINNED_INPUTS = (
    RECIPE,
    ABI_DEFINITION,
    SMS_TOOLCHAIN_LOCK,
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
        "MISTRAL_MUL18X19",
        "MISTRAL_MUL18X19_COMBINED",
        "MISTRAL_MUL27X27",
    }
)
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
HEX40_RE = re.compile(r"[0-9a-f]{40}\Z")


def _authenticate_sms_tools(root: Path, cache_root: Path | None = None):
    return _authenticate_tools(
        root,
        lock_path=root / SMS_TOOLCHAIN_LOCK,
        toolchain_root=root / SMS_TOOLCHAIN_ROOT,
        expected_commits=SMS_TOOL_COMMITS,
        expected_configuration={"nextpnr": SMS_TOOLCHAIN_CONFIGURATION},
        gpu_router=SMS_GPU_ROUTER,
        hip_architectures=SMS_GPU_ARCHITECTURES,
        cache_root=cache_root,
    )


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
            "gpu_architectures": SMS_GPU_ARCHITECTURES,
            "gpu_backend": SMS_GPU_BACKEND,
            "pixel_clock_hz": 74_250_000,
            "sys_clock_hz": 52_000_000,
            "reference_clock_hz": 50_000_000,
            "seed": SEED,
            "router": ROUTER,
            "toolchain_lock": SMS_TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(_regular_input(root, SMS_TOOLCHAIN_LOCK)),
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
        raise BuildError(f"FES SMS OSS output must be {root / OUTPUT_RELATIVE}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    sources = " ".join(RTL_SOURCES)
    yosys_program = (
        f"read_verilog -sv -DTV80_REFRESH=1 -DFES_SMS_OSS=1 -DFES_COLECO_OSS=1 "
        f"-I cores/fes-sms/generated -I cores/fes-coleco/generated {sources}; "
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
        # The GPU router can report a provisional timing shortfall before
        # its final repair/signoff pass; allow that intermediate result,
        # then require the structured final timing evidence below to meet
        # both clock constraints. Timing-driven rip-up is intentionally
        # not enabled. A sealed BUILD_ID changes placement.
        "--seed", str(SEED),
        "--router", ROUTER,
        "--timing-allow-fail",
        "--rbf", f"{OUTPUT_RELATIVE.as_posix()}/core.rbf",
        "--compress-rbf",
        "--write", f"{OUTPUT_RELATIVE.as_posix()}/routed.json",
        "--report", f"{OUTPUT_RELATIVE.as_posix()}/timing.json",
        "--detailed-timing-report",
    )
    return yosys, nextpnr


def _prepare_output(root: Path) -> Path:
    root = Path(root)
    build_root = root / OUTPUT_RELATIVE.parent
    if build_root.is_symlink() or (build_root.exists() and not build_root.is_dir()):
        raise BuildError(f"build path must be a non-symlink directory: {build_root}")
    build_root.mkdir(parents=True, exist_ok=True)
    output = root / OUTPUT_RELATIVE
    if output.is_symlink() or (output.exists() and not output.is_dir()):
        raise BuildError(f"build output path must be a non-symlink directory: {output}")
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


def validate_synth_evidence(output: Path) -> dict:
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    _i2c_evidence(synthesis, "synthesized")
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    if counts.get("MISTRAL_IO", 0) < 1:
        raise BuildError("synthesis must contain MISTRAL_IO HDMI I2C pads")
    if counts.get("MISTRAL_M10K", 0) + counts.get("MISTRAL_M10K_TDP", 0) < 1:
        raise BuildError("synthesis must map SMS RAM onto MISTRAL_M10K")
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise BuildError(f"forbidden synthesis cell {name} is in use")
    return {
        "status": "pass",
        "synth_only": True,
        "synthesis_cells": {name: counts[name] for name in sorted(counts)},
    }


def validate_build_evidence(output: Path, source_root: Path = ROOT) -> dict:
    routed = _read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    synth_evidence = validate_synth_evidence(output)
    counts = synth_evidence["synthesis_cells"]
    _i2c_evidence(routed, "routed")
    route_log = output / "nextpnr.log"
    if route_log.is_symlink() or not route_log.is_file():
        raise BuildError(f"missing route log: {route_log}")
    route_text = route_log.read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    gpu_backend = _require_gpu_backend(route_text)
    if "50 MHz -> 52 MHz" not in route_text:
        raise BuildError("route log does not contain the 50-to-52 MHz system PLL")
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
        "route": {"status": "pass", "unrouted": False, "gpu_backend": gpu_backend},
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
            "id": "fes.sms",
            "name": "FES Master System",
            "description": "Standalone fixed-720p Master System slice for the FES simple-computer ABI (OSS)",
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
            {"id": "fes.media.blob-stream", "major": 1, "minor": 0, "required": True},
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


def build(root: Path = ROOT, package_store: Path | None = None, *, cache_root: Path | None = None) -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES SMS package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root)
    authenticated = _authenticate_sms_tools(root, cache_root=cache_root)
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
        final_tools = _authenticate_sms_tools(root, cache_root=cache_root)
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


def synth(root: Path = ROOT, *, cache_root: Path | None = None) -> dict:
    """Run Yosys only. Does not require a clean tree and does not seal a package."""
    root = Path(root).resolve()
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
    authenticated = _authenticate_sms_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    output = _prepare_output(root)
    build_id = "0" * 32
    yosys, _nextpnr = build_commands(
        root,
        output,
        build_id,
        {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
    )
    _run_tool(yosys, root, output / "yosys.log")
    if not (output / "synth.json").is_file():
        raise BuildError("Yosys did not produce synthesis evidence")
    evidence = validate_synth_evidence(output)
    evidence.update(
        {
            "build_id": build_id,
            "device": TARGET,
            "inputs": {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)},
            "sealed": False,
            "tools": identities,
            "top": TOP,
        }
    )
    _write_atomic(
        output / "build-summary.json",
        (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode("utf-8"),
    )
    return evidence


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--print-commands", action="store_true")
    parser.add_argument(
        "--synth-only",
        action="store_true",
        help="run Yosys only; skip clean-tree seal and nextpnr",
    )
    arguments = parser.parse_args(argv)
    try:
        if arguments.print_commands:
            root = arguments.root.resolve()
            if arguments.synth_only:
                for relative in PINNED_INPUTS:
                    _regular_input(root, relative)
                authenticated = _authenticate_sms_tools(root, cache_root=arguments.cache_root)
                build_id = "0" * 32
            else:
                repository, revision = _require_clean_source(arguments.root)
                authenticated = _authenticate_sms_tools(arguments.root, cache_root=arguments.cache_root)
                identities = {name: tool.identity for name, tool in authenticated.items()}
                record = create_build_record(arguments.root, repository, revision, identities)
                build_id = build_identity(record)
            yosys, nextpnr = build_commands(
                root,
                root / OUTPUT_RELATIVE,
                build_id,
                {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
            )
            print(" ".join(yosys))
            if not arguments.synth_only:
                print(" ".join(nextpnr))
            return 0
        if arguments.synth_only:
            evidence = synth(arguments.root, cache_root=arguments.cache_root)
            cells = evidence["synthesis_cells"]
            print(
                "synth-only "
                f"altera_pll={cells.get('altera_pll', 0)} "
                f"MISTRAL_IO={cells.get('MISTRAL_IO', 0)} "
                f"MISTRAL_M10K={cells.get('MISTRAL_M10K', 0)} "
                f"MISTRAL_M10K_TDP={cells.get('MISTRAL_M10K_TDP', 0)}"
            )
            return 0
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-sms-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
