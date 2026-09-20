#!/usr/bin/env python3
"""Build and seal the OSS nextpnr/Mistral FES ColecoVision package."""

from __future__ import annotations

import argparse
import hashlib
import os
import stat
from dataclasses import dataclass
import json
import math
import re
import sys
import tempfile
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.source_repository import canonical_repository
from scripts.fes_build_common import (
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
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.functional_execution import execution_environment, execution_inputs, execution_digest, source_roots_for_inputs
from scripts.search_placer_qor import SearchError, _parse_ints, route_after_synth


ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
ROUTER = "gpu"
SEED = 4
# Seed 4 is the historical packed-sprite placement. A sealed BUILD_ID can
# miss 52 MHz on that seed while nearby seeds close; try 4 first, then the
# HIP-checked fallbacks.
PLACER_SEEDS = (4, 1, 2, 3, 5, 12, 7, 10)
# HeAP timing weight 300 + critexp 5 closed seed 4 at 57.45 MHz on the
# sealed HIP netlist (default weight 10 was 51.67 FAIL). Weight 1000 also
# passed (56.62); 2000 was only 52.27. ZX81 keeps 1000 for its own seed 10.
PLACER_TIMING_WEIGHT = 300
PLACER_CRITICALITY_EXPONENT = 5
# Search policy for --best-fmax. First-pass still uses PLACER_TIMING_WEIGHT
# only. The winner is recorded in evidence, not substituted back into these
# constants (that would change BUILD_ID and invalidate the search).
PLACER_WEIGHTS = (10, 100, 300, 1000, 2000)
PLACER_QOR_BUDGET = 24
PLACER_QOR_CLOCKS = (("clk_sys", 52.0), (None, 74.25))
COLECO_GPU_BACKEND = "hip"
COLECO_GPU_ROUTER = "HIP"
COLECO_GPU_ARCHITECTURES = "gfx1100;gfx1201"
COLECO_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={COLECO_GPU_ROUTER}; hip-architectures={COLECO_GPU_ARCHITECTURES}"
)
OUTPUT_RELATIVE = Path("build/fes-coleco-oss")
COLECO_TOOLCHAIN_LOCK = "toolchains/registered-memory.lock"
COLECO_TOOLCHAIN_ROOT = "build/toolchain/fes-coleco"
COLECO_TOOL_COMMITS = {
    "mistral": "b28e30a36b5139aaed5a5d361a30b542e6b7c758",
    "nextpnr": "0fad53a75a0218941c417ec6bb58bdede9070987",
    "yosys": "e2d425dee148cc60c50f4e9b354a10d90eab15f4",
}
RECIPE = "scripts/build_fes_coleco_oss.py"
ABI_DEFINITION = "cores/fes-common/generated/fes_application.vh"
QSF = "cores/fes-coleco/constraints-oss.qsf"
SDC = "cores/fes-coleco/clocks-oss.sdc"
RTL_SOURCES = (
    "cores/fes-common/rtl/sys_pll.v",
    "cores/fes-common/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_application_gp.v",
    "cores/fes-coleco/rtl/coleco_application_gp.v",
    "cores/fes-common/rtl/coleco_dpram.v",
    "cores/fes-common/rtl/coleco_video_dpram.v",
    "cores/fes-common/rtl/coleco_vdp.sv",
    "cores/fes-common/rtl/coleco_video_720p.v",
    "cores/fes-coleco/rtl/coleco_machine.sv",
    "cores/fes-common/rtl/t80pa.v",
    "cores/fes-common/rtl/tv80/tv80_core.v",
    "cores/fes-common/rtl/tv80/tv80_alu.v",
    "cores/fes-common/rtl/tv80/tv80_mcode.v",
    "cores/fes-common/rtl/tv80/tv80_reg.v",
    "cores/fes-coleco/rtl/top.v",
)
PINNED_INPUTS = (
    RECIPE, "scripts/compiler_read_audit.py", "scripts/source_repository.py",
    "scripts/fes_build_common.py",
    ABI_DEFINITION,
    COLECO_TOOLCHAIN_LOCK,
    QSF,
    SDC,
    "cores/fes-coleco/rtl/coleco_reset_rom.hex",
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
    "qor-ranking.json",
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



BIOS_SIZE = 8192


def _read_private_file(path: Path, size: int, label: str) -> bytes:
    path = Path(path).absolute()
    if any(part.is_symlink() for part in (path, *path.parents)):
        raise BuildError(f"{label} must not traverse a symlink")
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), "rb") as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size != size:
            raise BuildError(f"{label} must be a regular file of exactly {size} bytes")
        data = stream.read(size + 1)
        after = os.fstat(stream.fileno())
    if len(data) != size or (before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
            after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns):
        raise BuildError(f"{label} changed while being read")
    return data


@dataclass(frozen=True)
class PrivateBiosSnapshot:
    binary_sha256: str
    hex_sha256: str

    def verify(self, output: Path) -> dict:
        binary = _read_private_file(output / "private-bios.bin", BIOS_SIZE, "private BIOS snapshot")
        hex_bytes = _read_private_file(output / "private-bios.hex", BIOS_SIZE * 3, "private BIOS HEX snapshot")
        for name in ("private-bios.bin", "private-bios.hex"):
            if (output / name).stat().st_mode & 0o222:
                raise BuildError("private BIOS snapshot must remain read-only")
        if (hashlib.sha256(binary).hexdigest() != self.binary_sha256 or
                hashlib.sha256(hex_bytes).hexdigest() != self.hex_sha256 or
                hex_bytes != _bios_hex(binary)):
            raise BuildError("private BIOS snapshot differs from recorded input")
        return {"bios_mode": "private-8192", "bios_size": BIOS_SIZE,
                "bios_sha256": self.binary_sha256, "bios_hex_sha256": self.hex_sha256}


def _bios_hex(data: bytes) -> bytes:
    return "".join(f"{value:02x}\n" for value in data).encode("ascii")


def _snapshot_bios(source: Path, output: Path) -> PrivateBiosSnapshot:
    # Snapshot once; subsequent checks bind these bytes, not a mutable external
    # filename. No private pathname or ROM contents enter the public record.
    binary = _read_private_file(source, BIOS_SIZE, "private BIOS input")
    hex_bytes = _bios_hex(binary)
    if any(path.is_symlink() for path in (output, *output.parents)):
        raise BuildError("private BIOS output must not traverse a symlink")
    output.chmod(0o700)
    for name, data in (("private-bios.bin", binary), ("private-bios.hex", hex_bytes)):
        _write_atomic(output / name, data)
        (output / name).chmod(0o400)
    snapshot = PrivateBiosSnapshot(hashlib.sha256(binary).hexdigest(), hashlib.sha256(hex_bytes).hexdigest())
    snapshot.verify(output)
    return snapshot


def _package_store(root: Path, requested: Path | None, *, private_bios: bool) -> Path:
    expected = root / ("build/private-packages" if private_bios else "build/packages")
    chosen = expected if requested is None else Path(requested).absolute()
    if chosen != expected or any(path.is_symlink() for path in (chosen, *chosen.parents)):
        raise BuildError(f"FES ColecoVision package store must be {expected}")
    if private_bios:
        chosen.mkdir(parents=True, exist_ok=True, mode=0o700)
        chosen.chmod(0o700)
    return chosen


def _authenticate_coleco_tools(root: Path, cache_root: Path | None = None):
    return _authenticate_tools(
        root,
        lock_path=root / COLECO_TOOLCHAIN_LOCK,
        toolchain_root=root / COLECO_TOOLCHAIN_ROOT,
        expected_commits=COLECO_TOOL_COMMITS,
        expected_configuration={"nextpnr": COLECO_TOOLCHAIN_CONFIGURATION},
        gpu_router=COLECO_GPU_ROUTER,
        hip_architectures=COLECO_GPU_ARCHITECTURES,
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


def _require_clean_source(root: Path, *, identity_version: int = 1) -> tuple[str, str]:
    root = Path(root).resolve()
    actual_root = Path(_git(root, "rev-parse", "--show-toplevel")).resolve()
    if actual_root != root and not (identity_version == 2 and root.is_relative_to(actual_root)):
        raise BuildError(f"source root does not match Git checkout root: {root}")
    revision = _git(root, "rev-parse", "HEAD")
    if HEX40_RE.fullmatch(revision) is None:
        raise BuildError("source HEAD is not a full lowercase Git commit")
    if _git(root, "status", "--porcelain", "--untracked-files=all", "--", "."):
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
    try:
        repository = canonical_repository(repositories[0])
    except ValueError as exc:
        raise BuildError(str(exc)) from exc
    return repository, revision


@guard_functional_source
def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
    *,
    qor_mode: str = "first-pass",
    identity_version: int = 1,
    execution: dict | None = None,
    bios_snapshot: PrivateBiosSnapshot | None = None,
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
            "gpu_architectures": COLECO_GPU_ARCHITECTURES,
            "gpu_backend": COLECO_GPU_BACKEND,
            "pixel_clock_hz": 74_250_000,
            "sys_clock_hz": 52_000_000,
            "reference_clock_hz": 50_000_000,
            "seed": PLACER_SEEDS[0],
            "seed_order": ",".join(str(seed) for seed in PLACER_SEEDS),
            "placer_heap_timingweight": PLACER_TIMING_WEIGHT,
            "placer_heap_timingweights": ",".join(str(weight) for weight in PLACER_WEIGHTS),
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT,
            "placer_qor_mode": qor_mode,
            "placer_qor_budget": PLACER_QOR_BUDGET,
            "router": ROUTER,
            "toolchain_lock": COLECO_TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(_regular_input(root, COLECO_TOOLCHAIN_LOCK)),
            "top": TOP,
        },
    }
    if bios_snapshot is not None:
        if identity_version != 2:
            raise BuildError("private BIOS requires build identity version 2")
        fields["parameters"].update(bios_snapshot.verify(root / OUTPUT_RELATIVE))
    if identity_version == 2:
        fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS), execution, pinned_inputs=PINNED_INPUTS)
    elif identity_version != 1:
        raise BuildError("unsupported build identity version")
    return encode_build_record(fields)


def build_commands(
    root: Path,
    output: Path,
    build_id: str,
    tools: Mapping[str, Path],
    seed: int = SEED,
    *, private_bios: bool = False,
) -> tuple[tuple[str, ...], tuple[str, ...]]:
    if output != root / OUTPUT_RELATIVE:
        raise BuildError(f"FES ColecoVision OSS output must be {root / OUTPUT_RELATIVE}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    sources = " ".join(RTL_SOURCES)
    bios_define = " -DFES_COLECO_PRIVATE_BIOS=1" if private_bios else ""
    yosys_program = (
        f"read_verilog -sv -DTV80_REFRESH=1 -DFES_COLECO_OSS=1{bios_define} -I cores/fes-common/generated {sources}; "
        f"chparam -set BUILD_ID 128'h{build_id} {TOP}; "
        f"chparam -set ENABLE_FIRMWARE 1 coleco_application_gp; "
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
        # Seed 4 is the reproducible packed-sprite placement for this sealed
        # route recipe on the live HIP backend. The GPU router can report a
        # provisional timing shortfall before its final repair/signoff pass;
        # allow that intermediate result, then require the structured final
        # timing evidence below to meet both clock constraints. The embedded
        # BUILD_ID changes the placement search space, so this seed is part of
        # the sealed recipe. Timing-driven rip-up is intentionally not enabled:
        # on this netlist it is slower and can move a passing route back below
        # the timing target.
        "--seed", str(seed),
        "--placer-heap-timingweight", str(PLACER_TIMING_WEIGHT),
        "--placer-heap-critexp", str(PLACER_CRITICALITY_EXPONENT),
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


def _clear_route_outputs(output: Path) -> None:
    for name in ("core.rbf", "routed.json", "timing.json", "nextpnr.log"):
        path = output / name
        if path.exists() or path.is_symlink():
            if path.is_symlink() or not path.is_file():
                raise BuildError(f"build output must be a regular file: {path}")
            path.unlink()


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
        raise BuildError("synthesis must map COLECO RAM onto MISTRAL_M10K")
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise BuildError(f"forbidden synthesis cell {name} is in use")
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
    private_bios = record_fields["parameters"].get("bios_mode") == "private-8192"
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    fields = {
        "format": 2,
        "core": {
            "id": "fes.coleco.private-bios" if private_bios else "fes.coleco",
            "name": "FES ColecoVision private BIOS bring-up" if private_bios else "FES ColecoVision",
            "description": ("Private supplied-BIOS bring-up; not the image-selected reset-shim package"
                if private_bios else "Standalone fixed-720p ColecoVision slice for the FES application ABI (OSS)"),
            "version": "1.0.0",
        },
        "target": {
            "platform": "de10_nano",
            "device": TARGET,
            "programming_profile": "fes-gp-v1",
        },
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.application", "major": 1, "minor": 0},
        "interfaces": [
            {"id": "fes.gamepad.ports", "major": 1, "minor": 0, "required": True},
            {"id": "fes.keypad.ports", "major": 1, "minor": 0, "required": True},
            {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
            {"id": "fes.media.blob", "major": 1, "minor": 0, "required": True},
            {"id": "fes.media.blob-stream", "major": 1, "minor": 0, "required": True},
            {"id": "fes.firmware.blob", "major": 1, "minor": 0, "required": False},
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


@guard_functional_source
def build(
    root: Path = ROOT,
    package_store: Path | None = None,
    *,
    cache_root: Path | None = None,
    best_fmax: bool = False,
    gpu_devices: Sequence[int] = (),
    identity_version: int = 1,
    bios: Path | None = None,
) -> Path:
    root = Path(root).resolve()
    if bios is not None and identity_version != 2:
        raise BuildError("private BIOS requires build identity version 2")
    package_store = _package_store(root, package_store, private_bios=bios is not None)
    qor_mode = "staged" if best_fmax else "first-pass"
    qor_weights = PLACER_WEIGHTS if best_fmax else (PLACER_TIMING_WEIGHT,)
    qor_budget = PLACER_QOR_BUDGET if best_fmax else max(len(PLACER_SEEDS), 1)
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    authenticated = _authenticate_coleco_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    output = _prepare_output(root)
    bios_snapshot = _snapshot_bios(bios, output) if bios is not None else None
    execution = None
    controlled_env = None
    private_home = None
    if identity_version == 2:
        if len(gpu_devices) > 1:
            raise BuildError("functional identity currently requires one explicit GPU device")
        gpu_devices = tuple(gpu_devices) or (0,)
        private_home = tempfile.TemporaryDirectory(prefix="fes-coleco-tool-home-")
        tool_paths = {name: tool.path for name, tool in authenticated.items()}
        controlled_env = execution_environment(Path(private_home.name), tool_paths)
        execution = execution_inputs(tool_paths, controlled_env, gpu_devices[0])
    record = create_build_record(
        root, repository, revision, identities, qor_mode=qor_mode,
        identity_version=identity_version, execution=execution, bios_snapshot=bios_snapshot,
    )
    _write_atomic(output / "build-inputs.json", record)
    try:
        build_id = build_identity(record)
        commands = build_commands(
            root, output, build_id,
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
            private_bios=bios_snapshot is not None,
        )
        if bios_snapshot is not None:
            bios_snapshot.verify(output)
        if controlled_env is None:
            _run_tool(commands[0], root, output / "yosys.log", output_relative=OUTPUT_RELATIVE)
        else:
            _run_tool(commands[0], root, output / "yosys.log", env=controlled_env, audit_source_root=root, output_relative=OUTPUT_RELATIVE)
        if bios_snapshot is not None:
            bios_snapshot.verify(output)
        if not (output / "synth.json").is_file():
            raise BuildError("Yosys did not produce synthesis evidence")
        try:
            winner = route_after_synth(
                nextpnr=authenticated["nextpnr-mistral"].path,
                fixture=output / "synth.json",
                dest=output,
                device=TARGET,
                qsf=root / QSF,
                sdc=root / SDC,
                freq="74.25",
                seeds=PLACER_SEEDS,
                weights=qor_weights,
                critexp=PLACER_CRITICALITY_EXPONENT,
                budget=qor_budget,
                mode=qor_mode,
                extra=("--router", ROUTER),
                required=PLACER_QOR_CLOCKS,
                gpu_devices=gpu_devices,
                **({"env": controlled_env, "audit_source_root": root} if controlled_env is not None else {}),
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, root)
        evidence["route"]["placer_seed"] = winner.seed
        evidence["route"]["placer_heap_timingweight"] = winner.weight
        evidence["route"]["placer_qor_mode"] = qor_mode
        if execution is not None:
            evidence["execution"] = execution
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
        final_tools = _authenticate_coleco_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError("authenticated tool identity changed during build")
        final_repository, final_revision = _require_clean_source(root, identity_version=identity_version)
        if (final_repository, final_revision) != (repository, revision):
            raise BuildError("source identity changed during build")
        if execution is not None:
            final_execution = execution_inputs(tool_paths, controlled_env, gpu_devices[0])
            if final_execution != execution:
                raise BuildError("execution inputs changed during build")
            final_record = create_build_record(root, repository, revision, identities,
                qor_mode=qor_mode, identity_version=identity_version, execution=final_execution, bios_snapshot=bios_snapshot)
            if final_record != record:
                raise BuildError("functional source inputs changed during build")
        if bios_snapshot is not None:
            bios_snapshot.verify(output)
        return export_package(manifest, output / "core.rbf", package_store)
    except Exception:
        for name in ("core.rbf", "manifest.toml", "build-summary.json"):
            path = output / name
            if path.is_file() or path.is_symlink():
                path.unlink()
        raise
    finally:
        if private_home is not None:
            private_home.cleanup()


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--bios", type=Path, help="private 8192-byte BIOS bring-up (identity v2; separate package store)")
    parser.add_argument("--print-commands", action="store_true")
    parser.add_argument("--identity-version", type=int, choices=(1, 2), default=2,
                        help="2 opts into functional input identity (single GPU; experimental)")
    parser.add_argument(
        "--best-fmax",
        action="store_true",
        help="after synthesis, search HeAP weight and seed for the best Fmax instead of first-to-pass",
    )
    parser.add_argument(
        "--gpu-devices",
        default=None,
        help="HIP device indices (v1 best-fmax default 0,1; v2 default 0, single device)",
    )
    arguments = parser.parse_args(argv)
    try:
        if arguments.print_commands:
            if arguments.bios is not None:
                raise BuildError("private BIOS requires the controlled build invocation")
            if arguments.identity_version != 1:
                raise BuildError("functional identity requires the controlled build invocation")
            repository, revision = _require_clean_source(arguments.root)
            authenticated = _authenticate_coleco_tools(arguments.root, cache_root=arguments.cache_root)
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
        print(
            build(
                arguments.root,
                arguments.package_output,
                cache_root=arguments.cache_root,
                bios=arguments.bios,
                best_fmax=arguments.best_fmax,
                identity_version=arguments.identity_version,
                gpu_devices=_parse_ints(arguments.gpu_devices or ("0" if arguments.identity_version == 2 else "0,1"))
                    if arguments.best_fmax or arguments.identity_version == 2 else (),
            )
        )
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-coleco-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
