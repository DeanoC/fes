#!/usr/bin/env python3
"""Build and export the installable OSS PC/XT diagnostic package.

The package enables the small fes.computer identity/execution mailbox and
boots the built-in diagnostic ROM to CGA Direct output. Keyboard and media
interfaces are not declared. The routed timing miss tracked by FES #840 is
recorded as unqualified and does not gate this diagnostic package.
"""
from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Mapping

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
_SCRIPT_DIRECTORY = str(Path(__file__).resolve().parent)
if _SCRIPT_DIRECTORY not in sys.path:
    # Keep test discovery ahead of these legacy top-level producer imports.
    sys.path.append(_SCRIPT_DIRECTORY)

from scripts import build_fes_pcxt as board
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import encode_manifest
from scripts.export_core_package import (
    build_identity, encode_build_record, export_package, functional_record_fields,
)
from scripts.fes_build_common import (
    BuildError, _invalidate_failed_artifact, _prepare_output,
    _read_json, _require_clean_source as require_clean_source,
    _run_tool, _sha256, _write_atomic,
)
from scripts.functional_execution import FunctionalInvocation, source_roots_for_producer
from scripts import pnr_recipe

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_pcxt_oss.py"
OUTPUT_RELATIVE = Path("build/fes-pcxt-oss")
PACKAGE_STORE_RELATIVE = Path("build/packages")
TOOLCHAIN_LOCK = "cores/fes-pcxt/toolchain.lock"
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
PNR = pnr_recipe.recipe_for("fes.pcxt")
PLACER_SEED = PNR["seeds"][0]
PLACER_WEIGHT = PNR["timing_weights"][0]
PLACER_CRITICALITY_EXPONENT = PNR["critexp"]
ROUTE_TIMEOUT_SECONDS = PNR["timeout_s"]

VENDOR_INPUTS = []
for _vendor in (*board.CHIP_VENDORS, "mcl86"):
    _vendor_root = board.vendor_dir(_vendor)
    _vendor_manifest = _vendor_root / "source.json"
    VENDOR_INPUTS.append(_vendor_manifest.relative_to(ROOT).as_posix())
    _vendor_data = json.loads(_vendor_manifest.read_text(encoding="utf-8"))
    VENDOR_INPUTS.extend(
        (_vendor_root / item["path"]).relative_to(ROOT).as_posix()
        for item in _vendor_data["files"]
    )

PINNED_INPUTS = tuple(dict.fromkeys((
    RECIPE, "scripts/build_fes_pcxt.py", "scripts/sim_fes_pcxt.py",
    "scripts/compiler_read_audit.py", "scripts/core_package.py",
    "scripts/export_core_package.py", "scripts/fes_build_common.py",
    "scripts/functional_execution.py", "scripts/lockfile.py",
    "scripts/nextpnr_lease.py", "scripts/pnr_recipe.py",
    "scripts/source_closure.py",
    "scripts/source_repository.py", "scripts/toolchain_cache.py",
    "scripts/bootstrap.sh", TOOLCHAIN_LOCK, ABI_DEFINITION,
    *board.RTL, board.QSF, board.SDC, board.CPU_MUX_MAP,
    board.ROM_CLOCK_MAP,
    "cores/fes-common/generated/fes_pcxt_isa.vh",
    "cores/fes-common/generated/fes_video_part.vh",
    "cores/fes-pcxt/firmware/diagnostic.S",
    *VENDOR_INPUTS,
)))

BUILD_OUTPUTS = (
    "build-inputs.json", "build-summary.json", "manifest.toml",
    "synth.json", "routed.json", "core.rbf", "timing.json",
    "yosys.log", "nextpnr.log", "diagnostic.o", "diagnostic.bin",
    "mcl86_ucode.mem", "mcl86_facts.vh",
    "mcl86_ucode_byte0.mem", "mcl86_ucode_byte1.mem",
    "mcl86_ucode_byte2.mem", "mcl86_ucode_byte3.mem",
    *(f"pcxt_diagnostic.bank{index}.hex" for index in range(8)),
)


def authenticate_tools(root: Path, *, cache_root: Path | None = None):
    return board.authenticate_tools(root, cache_root, "HIP")


def _require_clean_source(root: Path, *, identity_version: int = 2):
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    return require_clean_source(root, pinned_inputs=PINNED_INPUTS,
                                identity_version=identity_version)


@guard_functional_source
def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
    *,
    identity_version: int = 2,
    execution: dict | None = None,
) -> bytes:
    root = Path(root)
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    fields = {
        "format": 1,
        "repository": repository,
        "revision": revision,
        "recipe": RECIPE,
        "recipe_sha256": _sha256(root / RECIPE),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": _sha256(root / ABI_DEFINITION),
        "dependencies": {},
        "tools": dict(tool_identities),
        "parameters": {
            "device": board.TARGET,
            "top": board.TOP,
            "gpu_backend": "hip",
            "gpu_architectures": board.FES_GPU_ARCHITECTURES,
            "router": "gpu",
            "seed": PLACER_SEED,
            "placer_heap_timingweight": PLACER_WEIGHT,
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT,
            "frequency_mhz": 100,
            "pixel_clock_hz": 74_250_000,
            "display": "cga-direct",
            "runtime_abi": "fes.computer/1.0-fixed-video-only",
            "timing_status": "unqualified-known-miss-fes-840",
            "toolchain_lock": TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(root / TOOLCHAIN_LOCK),
        },
    }
    roots = source_roots_for_producer(__name__, PINNED_INPUTS, root)
    fields = functional_record_fields(root, fields, roots, execution,
                                      pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)


def manifest(record: bytes, rbf: dict, repository: str, revision: str,
             identities: Mapping[str, str]) -> bytes:
    record_fields = json.loads(record)
    toolchain = "; ".join(f"{name} {identities[name]}" for name in sorted(identities))
    return encode_manifest({
        "format": 2,
        "core": {
            "id": "fes.pcxt",
            "name": "FES PC/XT Diagnostic",
            "description": "Built-in 8088 diagnostic ROM with fixed CGA Direct output; timing remains unqualified under FES #840",
            "version": "0.1.0",
        },
        "target": {
            "platform": "de10_nano",
            "device": board.TARGET,
            "programming_profile": "fes-gp-v1",
        },
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{
            "id": "fes.video.fixed-720p60", "major": 1, "minor": 0,
            "required": True,
        }],
        "build": {
            "id": build_identity(record),
            "repository": repository,
            "revision": revision,
            "recipe_sha256": record_fields["recipe_sha256"],
            "toolchain": toolchain,
        },
    })


def synthesis_command(root: Path, output: Path, build_id: str,
                      tools: Mapping[str, Path]) -> tuple[str, ...]:
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("PC/XT package build requires authenticated Yosys and nextpnr")
    program = board.yosys_program(
        output, False, False, board.synthesis_command(),
        enable_fes_computer=True, build_id=build_id,
    )
    return str(tools["yosys"]), "-p", program


def route_command(root: Path, output: Path, nextpnr: Path, gpu_device: int,
                  env: Mapping[str, str]) -> tuple[tuple[str, ...], dict[str, str]]:
    return pnr_recipe.build(
        "fes.pcxt", nextpnr, output / "synth.json", PLACER_SEED, gpu_device,
        output=OUTPUT_RELATIVE, env=env, device=board.TARGET,
        qsf=root / board.QSF, sdc=root / board.SDC,
        weight=PLACER_WEIGHT, critexp=PLACER_CRITICALITY_EXPONENT,
    )


@guard_functional_source
def build(
    root: Path = ROOT,
    package_store: Path | None = None,
    *,
    cache_root: Path | None = None,
    identity_version: int = 2,
    gpu_device: int = 0,
) -> Path:
    root = Path(root).resolve()
    expected_store = root / PACKAGE_STORE_RELATIVE
    package_store = expected_store if package_store is None else Path(package_store).resolve()
    if package_store != expected_store:
        raise BuildError(f"PC/XT package store must be {expected_store}")
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    tools = authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    invocation = FunctionalInvocation(tools, gpu_device)
    output = None
    try:
        record = create_build_record(root, repository, revision, identities,
                                     identity_version=identity_version,
                                     execution=invocation.inputs)
        output = _prepare_output(root, relative=OUTPUT_RELATIVE,
                                 build_outputs=BUILD_OUTPUTS)
        _write_atomic(output / "build-inputs.json", record)
        for name in board.CHIP_VENDORS:
            board.verify_vendor(name)
        board.board_inputs(output)
        build_id = build_identity(record)
        yosys = synthesis_command(root, output, build_id,
                                  {name: tool.path for name, tool in tools.items()})
        _run_tool(yosys, output, output / "yosys.log",
                  output_relative=Path("."), env=invocation.env,
                  audit_source_root=root)
        if not (output / "synth.json").is_file():
            raise BuildError("Yosys did not produce the PC/XT package netlist")

        route, route_env = route_command(root, OUTPUT_RELATIVE, tools["nextpnr-mistral"].path,
                                         gpu_device, invocation.env)
        _run_tool(route, root, output / "nextpnr.log",
                  output_relative=OUTPUT_RELATIVE, env=route_env,
                  audit_source_root=root, timeout=ROUTE_TIMEOUT_SECONDS)
        timing = _read_json(output / "timing.json", "PC/XT timing report")
        routed = _read_json(output / "routed.json", "PC/XT routed design")
        route_text = (output / "nextpnr.log").read_text(encoding="utf-8")
        board._require_gpu_backend(route_text, "hip")
        rbf_path = output / "core.rbf"
        if rbf_path.is_symlink() or not rbf_path.is_file() or rbf_path.stat().st_size == 0:
            raise BuildError("nextpnr did not produce a nonempty PC/XT package RBF")
        rbf = {"size": rbf_path.stat().st_size, "sha256": _sha256(rbf_path)}
        summary = {
            "classification": "installable-diagnostic-package",
            "status": "built-timing-unqualified",
            "core_id": "fes.pcxt",
            "build_id": build_id,
            "device": board.TARGET,
            "top": board.TOP,
            "profile": {"mda": False, "scanlines": False, "half_rate_board": False},
            "route": {"seed": PLACER_SEED, "timing_weight": PLACER_WEIGHT,
                      "critexp": PLACER_CRITICALITY_EXPONENT, "backend": "HIP"},
            "timing": timing,
            "timing_acceptance": "not required for this diagnostic package; known misses remain tracked by FES #840",
            "package_rbf": rbf,
            "routed_json_sha256": _sha256(output / "routed.json"),
            "inputs": {path: _sha256(root / path) for path in sorted(PINNED_INPUTS)},
            "tools": identities,
            "execution": invocation.inputs,
            "limits": [
                "setup timing remains below the 100 MHz and 74.25 MHz targets",
                "SDRAM pad timing, physical socket containment and hardware acceptance remain open",
                "keyboard and media interfaces are not implemented",
            ],
        }
        _write_atomic(output / "build-summary.json",
                      (json.dumps(summary, ensure_ascii=False, indent=2,
                                  sort_keys=True) + "\n").encode())
        encoded = manifest(record, rbf, repository, revision, identities)
        _write_atomic(output / "manifest.toml", encoded)
        final_tools = authenticate_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError("authenticated PC/XT tool identity changed during build")
        if _require_clean_source(root, identity_version=identity_version) != (repository, revision):
            raise BuildError("PC/XT package source identity changed during build")
        invocation.verify()
        if create_build_record(root, repository, revision, identities,
                               identity_version=identity_version,
                               execution=invocation.inputs) != record:
            raise BuildError("PC/XT functional inputs changed during build")
        return export_package(encoded, rbf_path, package_store)
    except Exception:
        if output is not None:
            _invalidate_failed_artifact(output)
        raise
    finally:
        invocation.close()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(2,), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    args = parser.parse_args(argv)
    try:
        print(build(args.root, args.package_output, cache_root=args.cache_root,
                    identity_version=args.identity_version, gpu_device=args.gpu_device))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-pcxt-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
