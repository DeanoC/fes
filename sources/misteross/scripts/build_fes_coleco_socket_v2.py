#!/usr/bin/env python3
"""Build the Coleco v2 shell with an optional SGM expansion socket."""

from __future__ import annotations

import argparse
import json
import sys
import tomllib
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_coleco_oss as factory, coleco_expansion, video_parts, native_video_parts
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import encode_manifest
from scripts.export_core_package import (
    build_identity, encode_build_record, export_package, functional_record_fields,
)
from scripts.fes_build_common import (
    BuildError, _authenticate_tools, _prepare_output,
    _require_clean_source as require_clean_source,
    _run_tool, _sha256, _write_atomic,
)
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_coleco_socket_v2.py"
OUTPUT_RELATIVE = Path("build/fes-coleco-socket-v2")
VIDEO_OUTPUT_RELATIVE = Path("build/fes-coleco-video")
NATIVE_VIDEO_OUTPUT_RELATIVE = Path("build/fes-coleco-native-video")
TOOLCHAIN_LOCK = "toolchains/coleco-sgm.lock"
COLECO_TOOLCHAIN_LOCK = TOOLCHAIN_LOCK
PLACER_SEEDS = (3, 4, 5, 1, 2, 6, 7, 8, 9, 10)
PLACER_QOR_MODE = "first-pass"
PLACER_TIMEOUT_SECONDS = 600
TOOL_COMMITS = {
    "yosys": "e2d425dee148cc60c50f4e9b354a10d90eab15f4",
    "mistral": "7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039",
    "nextpnr": "a93fe013af841214ecb4f7be3af0de65f3de3a0f",
}
RTL_SOURCES = (*factory.RTL_SOURCES,
    "cores/fes-coleco/rtl/coleco_expansion_socket_v2.v",
    "cores/fes-coleco/rtl/coleco_expansion_ram.v",
    "cores/fes-coleco/rtl/coleco_audio_mix.v")
PINNED_INPUTS = tuple(dict.fromkeys((
    RECIPE, "scripts/coleco_expansion.py", "cores/fes-coleco/rtl/coleco_bus_v2_pack.vh",
    TOOLCHAIN_LOCK,
    *(path for path in factory.PINNED_INPUTS
      if path not in (factory.RECIPE, factory.COLECO_TOOLCHAIN_LOCK)),
    *RTL_SOURCES,
)))
VIDEO_INPUTS = (*PINNED_INPUTS, "scripts/video_parts.py", video_parts.RTL,
                "cores/fes-common/generated/fes_video_part.vh")
NATIVE_VIDEO_SOURCES = (
    "cores/fes-common/rtl/coleco_native_video.v",
    "cores/fes-common/rtl/fes_native_cdc.v", native_video_parts.RTL,
)
NATIVE_VIDEO_INPUTS = (*PINNED_INPUTS, *NATIVE_VIDEO_SOURCES,
    "scripts/native_video_parts.py", "cores/fes-common/generated/fes_native_video.vh")
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log",
    "nextpnr.log", "build-inputs.json", "build-summary.json", "manifest.toml",
    "qor-ranking.json", "socket.qsf",
)


def video_profile(*, video_socket: bool = False, native_video: bool = False):
    if video_socket and native_video:
        raise BuildError("select either raster or native video socket")
    if native_video:
        return native_video_parts, NATIVE_VIDEO_OUTPUT_RELATIVE, NATIVE_VIDEO_INPUTS
    if video_socket:
        return video_parts, VIDEO_OUTPUT_RELATIVE, VIDEO_INPUTS
    return None, OUTPUT_RELATIVE, PINNED_INPUTS


def placement_policy(*, native_video: bool = False) -> tuple[tuple[int, ...], int]:
    weights = (2000, 1000) if native_video else (2000,)
    return weights, len(PLACER_SEEDS) * len(weights)


def _route_placement(root: Path, output: Path, nextpnr: Path, *,
                     native_video: bool = False, env=None):
    weights, budget = placement_policy(native_video=native_video)
    return route_after_synth(
        nextpnr=nextpnr, fixture=output / "synth.json", dest=output,
        device=factory.TARGET, qsf=output / "socket.qsf",
        sdc=root / factory.SDC, freq="74.25", seeds=PLACER_SEEDS,
        weights=weights, critexp=factory.PLACER_CRITICALITY_EXPONENT,
        budget=budget, mode=PLACER_QOR_MODE, timeout=PLACER_TIMEOUT_SECONDS,
        extra=("--router", factory.ROUTER),
        required=factory.PLACER_QOR_CLOCKS, gpu_devices=(0,),
        env=env, audit_source_root=root,
    )


def authenticate_tools(root: Path, cache_root: Path | None):
    return _authenticate_tools(
        root, lock_path=root / TOOLCHAIN_LOCK,
        toolchain_root=root / "build/toolchain/fes-coleco-socket-v2",
        expected_commits=TOOL_COMMITS,
        expected_configuration={"nextpnr": factory.COLECO_TOOLCHAIN_CONFIGURATION},
        gpu_router=factory.COLECO_GPU_ROUTER,
        hip_architectures=factory.COLECO_GPU_ARCHITECTURES,
        cache_root=cache_root,
    )


def _require_clean_source(root: Path, *, identity_version: int = 2, video_socket: bool = False, native_video: bool = False):
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    _, _, inputs = video_profile(video_socket=video_socket, native_video=native_video)
    return require_clean_source(root, pinned_inputs=inputs,
                                identity_version=identity_version)


@guard_functional_source
def create_build_record(root: Path, repository: str, revision: str,
                        identities: Mapping[str, str], execution: dict | None = None,
                        *, identity_version: int = 2, video_socket: bool = False, native_video: bool = False) -> bytes:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    weights, budget = placement_policy(native_video=native_video)
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
            "seed": PLACER_SEEDS[0],
            "seed_order": ",".join(str(seed) for seed in PLACER_SEEDS),
            "placer_heap_timingweight": weights[0],
            "placer_heap_timingweights": ",".join(str(weight) for weight in weights),
            "placer_heap_critexp": factory.PLACER_CRITICALITY_EXPONENT,
            "placer_qor_mode": PLACER_QOR_MODE,
            "placer_qor_budget": budget,
            "placer_qor_timeout_seconds": PLACER_TIMEOUT_SECONDS,
            "toolchain_lock": TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(root / TOOLCHAIN_LOCK),
            "expansion_socket": "coleco-bus-v2",
            "expansion_rect": coleco_expansion.SOCKET_RECT_V2,
        },
    }
    profile, _, inputs = video_profile(video_socket=video_socket, native_video=native_video)
    if profile:
        fields["parameters"].update(video_socket=profile.MAP,
            parts_layout=profile.LAYOUT, video_rect=profile.PLACEMENT,
            video_clock_hz=74_250_000)
    if native_video:
        fields["parameters"].update(native_width=256, native_height=192,
                                     native_encoding="Index4Tms9918", native_frame_banks=2)
    fields = functional_record_fields(
        root, fields, source_roots_for_inputs(inputs), execution,
        pinned_inputs=inputs,
    )
    return encode_build_record(fields)


def build_commands(root: Path, output: Path, build_id: str,
                   tools: Mapping[str, Path], *, video_socket: bool = False, native_video: bool = False) -> tuple[tuple[str, ...], tuple[str, ...]]:
    profile, relative, _ = video_profile(video_socket=video_socket, native_video=native_video)
    weights, _ = placement_policy(native_video=native_video)
    sources = (*RTL_SOURCES, *NATIVE_VIDEO_SOURCES) if native_video else (
        (*RTL_SOURCES, profile.RTL) if profile else RTL_SOURCES)
    video_define = ("-DFES_COLECO_NATIVE_VIDEO_DEV=1 -DFES_COLECO_NATIVE_VIDEO_PART_DEV=1 "
                    if native_video else "-DFES_COLECO_VIDEO_PART_DEV=1 " if video_socket else "")
    if output != root / relative:
        raise BuildError("Coleco socket output path changed")
    if factory.HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("Coleco socket BUILD_ID is malformed")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("Coleco socket commands require authenticated compiler paths")
    program = (
        "read_verilog -sv -DTV80_REFRESH=1 -DFES_COLECO_OSS=1 "
        "-DFES_COLECO_EXPANSION_V2_DEV=1 "
        f"{video_define}-I cores/fes-common/generated -I cores/fes-coleco/rtl {' '.join(sources)}; "
        f"chparam -set BUILD_ID 128'h{build_id} top; "
        "chparam -set ENABLE_FIRMWARE 1 coleco_application_gp; "
        "synth_intel_alm -nolutram -nodsp -top top; stat; "
        f"write_json {relative.as_posix()}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = (
        str(tools["nextpnr-mistral"]), "--json", f"{relative}/synth.json",
        "--device", factory.TARGET, "--qsf", f"{relative}/socket.qsf",
        "--sdc", factory.SDC, "--freq", "74.25",
        "--seed", str(PLACER_SEEDS[0]),
        "--placer-heap-timingweight", str(weights[0]),
        "--placer-heap-critexp", str(factory.PLACER_CRITICALITY_EXPONENT),
        "--router", factory.ROUTER, "--timing-allow-fail",
        "--rbf", f"{relative}/core.rbf", "--compress-rbf",
        "--write", f"{relative}/routed.json",
        "--report", f"{relative}/timing.json", "--detailed-timing-report",
    )
    return yosys, route


def manifest(record: bytes, evidence: dict, repository: str, revision: str,
             identities: Mapping[str, str], *, video_socket: bool = False, native_video: bool = False) -> bytes:
    fields = tomllib.loads(factory._manifest(
        record, evidence, repository, revision, identities).decode())
    fields["core"]["version"] = "1.2.0"
    fields["core"]["description"] = "ColecoVision with optional SGM expansion socket"
    fields["interfaces"].append({
        "id": "fes.expansion.coleco-bus", "major": 2, "minor": 0,
        "required": False,
    })
    profile, _, _ = video_profile(video_socket=video_socket, native_video=native_video)
    if profile:
        fields["core"]["description"] = "ColecoVision development shell with CPU and pixel-domain video sockets"
        fields["interfaces"].append({"id": profile.INTERFACE, "major": 1,
                                     "minor": 0, "required": False})
    if native_video:
        fields["core"]["description"] = "ColecoVision native-pixel shell requiring a linked video part"
    return encode_manifest(fields)


@guard_functional_source
def build(root: Path = ROOT, package_store: Path | None = None, *,
          cache_root: Path | None = None, identity_version: int = 2,
          video_socket: bool = False, native_video: bool = False) -> Path:
    root = root.resolve()
    repository, revision = _require_clean_source(root, identity_version=identity_version, video_socket=video_socket, native_video=native_video)
    profile, relative, inputs = video_profile(video_socket=video_socket, native_video=native_video)
    package_store = factory._package_store(root, package_store, private_bios=False)
    tools = authenticate_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    output = _prepare_output(root, relative=relative, build_outputs=BUILD_OUTPUTS)
    qsf = coleco_expansion.shell_qsf((root / factory.QSF).read_text(), version=2)
    if profile:
        qsf = profile.shell_qsf(qsf)
    _write_atomic(output / "socket.qsf", qsf.encode())
    invocation = FunctionalInvocation(tools, 0)
    try:
        execution, env = invocation.inputs, invocation.env
        record = create_build_record(root, repository, revision, identities,
                                     execution=execution, identity_version=identity_version, video_socket=video_socket, native_video=native_video)
        _write_atomic(output / "build-inputs.json", record)
        build_id = build_identity(record)
        commands = build_commands(root, output, build_id, {
            name: tools[name].path for name in ("yosys", "nextpnr-mistral")}, video_socket=video_socket, native_video=native_video)
        _run_tool(commands[0], root, output / "yosys.log", env=env,
                  audit_source_root=root, output_relative=relative)
        coleco_expansion.prepare_shell_netlist(output / "synth.json", version=2)
        if profile:
            profile.validate_boundary(json.loads((output / "synth.json").read_bytes())["modules"]["top"], routed=False)
        try:
            winner = _route_placement(
                root, output, tools["nextpnr-mistral"].path,
                native_video=native_video, env=env,
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        coleco_expansion.validate_routed_shell(output / "routed.json", version=2)
        if profile:
            profile.validate_boundary(json.loads((output / "routed.json").read_bytes())["modules"]["top"], routed=True)
        evidence = factory.validate_build_evidence(output, root)
        evidence["route"].update(placer_seed=winner.seed,
                                 placer_heap_timingweight=winner.weight,
                                 placer_qor_mode=PLACER_QOR_MODE)
        evidence.update({
            "build_id": build_id, "device": factory.TARGET, "top": factory.TOP,
            "inputs": {path: _sha256(root / path) for path in sorted(inputs)},
            "tools": identities, "execution": execution,
            "expansion_rect": coleco_expansion.SOCKET_RECT_V2,
        })
        _write_atomic(output / "build-summary.json",
                      (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities, video_socket=video_socket, native_video=native_video)
        _write_atomic(output / "manifest.toml", encoded)
        if {name: tool.identity for name, tool in authenticate_tools(root, cache_root).items()} != identities:
            raise BuildError("Coleco socket compiler identity changed")
        if _require_clean_source(root, identity_version=identity_version, video_socket=video_socket, native_video=native_video) != (repository, revision):
            raise BuildError("Coleco socket source changed")
        invocation.verify()
        if create_build_record(root, repository, revision, identities,
                               execution=execution, identity_version=identity_version, video_socket=video_socket, native_video=native_video) != record:
            raise BuildError("Coleco socket functional inputs changed")
        return export_package(encoded, output / "core.rbf", package_store)
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
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(2,), default=2)
    video_options = parser.add_mutually_exclusive_group()
    video_options.add_argument("--native-video-socket", action="store_true", help="seal the native-pixel shell requiring a linked video part")
    video_options.add_argument("--video-socket", action="store_true", help="build the separate development shell with a pixel-domain video socket")
    args = parser.parse_args(argv)
    try:
        print(build(args.root, args.package_output, cache_root=args.cache_root,
                    identity_version=args.identity_version, video_socket=args.video_socket, native_video=args.native_video_socket))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-coleco-socket-v2: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
