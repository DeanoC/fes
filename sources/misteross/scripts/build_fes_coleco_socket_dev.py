#!/usr/bin/env python3
"""Build a development-only Coleco socket shell, separate from the factory recipe."""

from __future__ import annotations

import argparse
import json
import sys
import tomllib
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_coleco_oss as factory, coleco_expansion
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import encode_manifest
from scripts.export_core_package import (
    build_identity, encode_build_record, export_package, functional_record_fields,
)
from scripts.fes_build_common import (
    BuildError, _authenticate_tools, _prepare_output, _require_clean_source,
    _run_tool, _sha256, _write_atomic,
)
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_coleco_socket_dev.py"
OUTPUT_RELATIVE = Path("build/fes-coleco-socket-dev")
TOOLCHAIN_LOCK = "toolchains/coleco-expansion.lock"
TOOL_COMMITS = {
    "yosys": "e2d425dee148cc60c50f4e9b354a10d90eab15f4",
    "mistral": "18db2489a63bd9fcfbb7ba727ac194e767e7dce3",
    "nextpnr": "dfaa9e7f45929728db122b5b5c3a2111501fde00",
}
RTL_SOURCES = (*factory.RTL_SOURCES, "cores/fes-coleco/rtl/coleco_expansion_socket.v")
PINNED_INPUTS = tuple(dict.fromkeys((
    RECIPE, "scripts/coleco_expansion.py", "cores/fes-coleco/rtl/coleco_bus_pack.vh",
    TOOLCHAIN_LOCK, *factory.PINNED_INPUTS, *RTL_SOURCES,
)))
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log",
    "nextpnr.log", "build-inputs.json", "build-summary.json", "manifest.toml",
    "qor-ranking.json", "socket.qsf",
)


def authenticate_tools(root: Path, cache_root: Path | None):
    return _authenticate_tools(
        root, lock_path=root / TOOLCHAIN_LOCK,
        toolchain_root=root / "build/toolchain/fes-coleco-socket",
        expected_commits=TOOL_COMMITS,
        expected_configuration={"nextpnr": factory.COLECO_TOOLCHAIN_CONFIGURATION},
        gpu_router=factory.COLECO_GPU_ROUTER,
        hip_architectures=factory.COLECO_GPU_ARCHITECTURES,
        cache_root=cache_root,
    )


@guard_functional_source
def create_build_record(root: Path, repository: str, revision: str,
                        identities: Mapping[str, str], execution: dict) -> bytes:
    fields = {
        "format": 1, "repository": repository, "revision": revision,
        "recipe": RECIPE, "recipe_sha256": _sha256(root / RECIPE),
        "abi_definition": factory.ABI_DEFINITION,
        "abi_definition_sha256": _sha256(root / factory.ABI_DEFINITION),
        "dependencies": {}, "tools": dict(identities),
        "parameters": {
            "device": factory.TARGET, "top": factory.TOP,
            "gpu_backend": factory.COLECO_GPU_BACKEND,
            "gpu_architectures": factory.COLECO_GPU_ARCHITECTURES,
            "router": factory.ROUTER,
            "sys_clock_hz": 52_224_000, "pixel_clock_hz": 74_250_000,
            "audio_clock_hz": 12_288_000, "audio_sample_hz": 48_000,
            "reference_clock_hz": 50_000_000,
            "seed": factory.PLACER_SEEDS[0],
            "seed_order": ",".join(map(str, factory.PLACER_SEEDS)),
            "placer_heap_timingweight": factory.PLACER_TIMING_WEIGHT,
            "placer_heap_critexp": factory.PLACER_CRITICALITY_EXPONENT,
            "toolchain_lock": TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(root / TOOLCHAIN_LOCK),
            "expansion_socket": "coleco-bus-v1",
            "expansion_rect": coleco_expansion.SOCKET_RECT,
        },
    }
    fields = functional_record_fields(
        root, fields, source_roots_for_inputs(PINNED_INPUTS), execution,
        pinned_inputs=PINNED_INPUTS,
    )
    return encode_build_record(fields)


def build_commands(root: Path, output: Path, build_id: str,
                   tools: Mapping[str, Path]) -> tuple[tuple[str, ...], tuple[str, ...]]:
    if output != root / OUTPUT_RELATIVE:
        raise BuildError("Coleco socket output path changed")
    if factory.HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("Coleco socket BUILD_ID is malformed")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("Coleco socket commands require authenticated compiler paths")
    program = (
        "read_verilog -sv -DTV80_REFRESH=1 -DFES_COLECO_OSS=1 "
        "-DFES_COLECO_EXPANSION_DEV=1 "
        f"-I cores/fes-common/generated -I cores/fes-coleco/rtl {' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} top; "
        "chparam -set ENABLE_FIRMWARE 1 coleco_application_gp; "
        "synth_intel_alm -nolutram -nodsp -top top; stat; "
        f"write_json {OUTPUT_RELATIVE.as_posix()}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = (
        str(tools["nextpnr-mistral"]), "--json", f"{OUTPUT_RELATIVE}/synth.json",
        "--device", factory.TARGET, "--qsf", f"{OUTPUT_RELATIVE}/socket.qsf",
        "--sdc", factory.SDC, "--freq", "74.25",
        "--seed", str(factory.PLACER_SEEDS[0]),
        "--placer-heap-timingweight", str(factory.PLACER_TIMING_WEIGHT),
        "--placer-heap-critexp", str(factory.PLACER_CRITICALITY_EXPONENT),
        "--router", factory.ROUTER, "--timing-allow-fail",
        "--rbf", f"{OUTPUT_RELATIVE}/core.rbf", "--compress-rbf",
        "--write", f"{OUTPUT_RELATIVE}/routed.json",
        "--report", f"{OUTPUT_RELATIVE}/timing.json", "--detailed-timing-report",
    )
    return yosys, route


def manifest(record: bytes, evidence: dict, repository: str, revision: str,
             identities: Mapping[str, str]) -> bytes:
    fields = tomllib.loads(factory._manifest(
        record, evidence, repository, revision, identities).decode())
    fields["core"]["version"] = "1.1.0"
    fields["core"]["description"] = "Development Coleco CPU expansion socket shell"
    fields["interfaces"].append({
        "id": "fes.expansion.coleco-bus", "major": 1, "minor": 0,
        "required": False,
    })
    return encode_manifest(fields)


@guard_functional_source
def build(root: Path = ROOT, *, cache_root: Path | None = None) -> Path:
    root = root.resolve()
    repository, revision = _require_clean_source(
        root, pinned_inputs=PINNED_INPUTS, identity_version=2)
    tools = authenticate_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    output = _prepare_output(root, relative=OUTPUT_RELATIVE, build_outputs=BUILD_OUTPUTS)
    _write_atomic(output / "socket.qsf", coleco_expansion.shell_qsf(
        (root / factory.QSF).read_text()).encode())
    invocation = FunctionalInvocation(tools, 0)
    try:
        execution, env = invocation.inputs, invocation.env
        record = create_build_record(root, repository, revision, identities, execution)
        _write_atomic(output / "build-inputs.json", record)
        build_id = build_identity(record)
        commands = build_commands(root, output, build_id, {
            name: tools[name].path for name in ("yosys", "nextpnr-mistral")})
        _run_tool(commands[0], root, output / "yosys.log", env=env,
                  audit_source_root=root, output_relative=OUTPUT_RELATIVE)
        coleco_expansion.prepare_shell_netlist(output / "synth.json")
        try:
            winner = route_after_synth(
                nextpnr=tools["nextpnr-mistral"].path,
                fixture=output / "synth.json", dest=output,
                device=factory.TARGET, qsf=output / "socket.qsf",
                sdc=root / factory.SDC, freq="74.25",
                seeds=factory.PLACER_SEEDS,
                weights=(factory.PLACER_TIMING_WEIGHT,),
                critexp=factory.PLACER_CRITICALITY_EXPONENT,
                budget=len(factory.PLACER_SEEDS), mode="first-pass",
                extra=("--router", factory.ROUTER),
                required=factory.PLACER_QOR_CLOCKS, gpu_devices=(0,),
                env=env, audit_source_root=root,
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        coleco_expansion.validate_routed_shell(output / "routed.json")
        evidence = factory.validate_build_evidence(output, root)
        evidence["route"].update(placer_seed=winner.seed,
                                 placer_heap_timingweight=winner.weight,
                                 placer_qor_mode="first-pass")
        evidence.update({
            "build_id": build_id, "device": factory.TARGET, "top": factory.TOP,
            "inputs": {path: _sha256(root / path) for path in sorted(PINNED_INPUTS)},
            "tools": identities, "execution": execution,
            "expansion_rect": coleco_expansion.SOCKET_RECT,
        })
        _write_atomic(output / "build-summary.json",
                      (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities)
        _write_atomic(output / "manifest.toml", encoded)
        if {name: tool.identity for name, tool in authenticate_tools(root, cache_root).items()} != identities:
            raise BuildError("Coleco socket compiler identity changed")
        if _require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=2) != (repository, revision):
            raise BuildError("Coleco socket source changed")
        invocation.verify()
        if create_build_record(root, repository, revision, identities, execution) != record:
            raise BuildError("Coleco socket functional inputs changed")
        return export_package(encoded, output / "core.rbf",
                              root / "build/coleco-socket-packages")
    except Exception:
        for name in ("core.rbf", "manifest.toml", "build-summary.json"):
            path = output / name
            if path.is_file() or path.is_symlink():
                path.unlink()
        raise
    finally:
        invocation.close()


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    args = parser.parse_args(argv)
    try:
        print(build(args.root, cache_root=args.cache_root))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-coleco-socket-dev: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
