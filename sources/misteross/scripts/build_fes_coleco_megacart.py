#!/usr/bin/env python3
"""Build and seal the development Coleco two-ROM MegaCart shell."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import sys
import tomllib
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_coleco_oss as factory
from scripts import build_fes_coleco_socket_v2 as socket
from scripts import coleco_expansion, rom_map
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.fes_build_common import BuildError, _prepare_output, _require_clean_source, _run_tool, _sha256, _write_atomic
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_coleco_megacart.py"
OUTPUT_RELATIVE = Path("build/fes-coleco-megacart")
ROM_LANES = tuple((5, row) for row in (*range(32, 56), *range(73, 81))) + tuple((14, row) for row in range(1, 81)) + tuple((38, row) for row in range(1, 25))
assert len(ROM_LANES) == 136
ROM_DATABASE_SHA256 = {
    "data/m10k-mux.txt": "22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe",
    "libmistral/cvd-sx120f.cc": "e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7",
    "libmistral/cyclonev.h": "48c0acadd2d1dc47398d7e7ab8ad840e98cb3fda489c3197eace6f3ba59e6f21",
}
RTL_SOURCES = (*socket.RTL_SOURCES, "cores/fes-coleco/rtl/coleco_megacart_rom.v")
PINNED_INPUTS = tuple(dict.fromkeys((
    RECIPE, "scripts/rom_map.py", "cores/fes-coleco/rtl/coleco_megacart_rom.v",
    *(name for name in socket.PINNED_INPUTS if name != socket.RECIPE),
)))
BUILD_OUTPUTS = (*socket.BUILD_OUTPUTS, "rom-map.json")


def require_clean_source(root: Path) -> tuple[str, str]:
    return _require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=2)


@guard_functional_source
def create_build_record(root: Path, repository: str, revision: str,
                        identities: Mapping[str, str], execution: dict | None = None) -> bytes:
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
            "seed": socket.PLACER_SEEDS[0],
            "seed_order": ",".join(map(str, socket.PLACER_SEEDS)),
            "placer_heap_timingweight": 2000,
            "placer_heap_critexp": factory.PLACER_CRITICALITY_EXPONENT,
            "toolchain_lock": socket.TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(root / socket.TOOLCHAIN_LOCK),
            "expansion_socket": "coleco-bus-v2",
            "expansion_rect": coleco_expansion.SOCKET_RECT_V2,
            "package_format": 4,
            "rom_source_size": 139264,
            "rom_encoding": "m10k-1024x10-v1",
            "rom_database_sha256": json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(",", ":")),
        },
    }
    fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS),
                                      execution, pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)


def build_commands(root: Path, output: Path, build_id: str,
                   tools: Mapping[str, Path]) -> tuple[tuple[str, ...], tuple[str, ...]]:
    if output != root / OUTPUT_RELATIVE or factory.HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("invalid Coleco MegaCart build output or ID")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("Coleco MegaCart commands require authenticated tools")
    program = (
        "read_verilog -sv -DTV80_REFRESH=1 -DFES_COLECO_OSS=1 "
        "-DFES_COLECO_EXPANSION_V2_DEV=1 -DFES_COLECO_MEGACART_LINK=1 "
        f"-I cores/fes-common/generated -I cores/fes-coleco/rtl {' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} top; "
        "synth_intel_alm -nolutram -nodsp -top top; stat; "
        f"write_json {OUTPUT_RELATIVE.as_posix()}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = list(socket.build_commands(root, root / socket.OUTPUT_RELATIVE, build_id, tools)[1])
    return yosys, tuple(arg.replace(str(socket.OUTPUT_RELATIVE), str(OUTPUT_RELATIVE)) for arg in route)


def manifest(record: bytes, evidence: dict, repository: str, revision: str,
             identities: Mapping[str, str]) -> bytes:
    fields = tomllib.loads(factory._manifest(record, evidence, repository, revision, identities).decode())
    fields["format"] = 4
    fields["core"]["version"] = "1.3.0"
    fields["core"]["description"] = "ColecoVision MegaCart with download-time linked BIOS and cartridge"
    fields["interfaces"] = [item for item in fields["interfaces"] if not item["id"].startswith(("fes.media.", "fes.firmware."))]
    fields["interfaces"].append({"id": "fes.expansion.coleco-bus", "major": 2, "minor": 0, "required": False})
    fields["roms"] = [
        {"id": "coleco-bios", "role": "firmware", "source_size": 8192, "source_offset": 0},
        {"id": "coleco-cart", "role": "cartridge", "source_size": 131072, "source_offset": 8192},
    ]
    fields["rom_map"] = evidence["rom_map"]
    return encode_manifest(fields)


@guard_functional_source
def build(root: Path = ROOT, package_store: Path | None = None, *,
          cache_root: Path | None = None) -> Path:
    root = root.resolve()
    repository, revision = require_clean_source(root)
    package_store = factory._package_store(root, package_store, private_bios=False)
    tools = socket.authenticate_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    database_root = tools["mistral"].path.parents[2] / "src/mistral"
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    output = _prepare_output(root, relative=OUTPUT_RELATIVE, build_outputs=BUILD_OUTPUTS)
    _write_atomic(output / "socket.qsf", coleco_expansion.shell_qsf(
        (root / factory.QSF).read_text(), version=2).encode())
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
        coleco_expansion.prepare_shell_netlist(output / "synth.json", version=2)
        try:
            winner = route_after_synth(
                nextpnr=tools["nextpnr-mistral"].path, fixture=output / "synth.json",
                dest=output, device=factory.TARGET, qsf=output / "socket.qsf",
                sdc=root / factory.SDC, freq="74.25", seeds=socket.PLACER_SEEDS,
                weights=(2000,), critexp=factory.PLACER_CRITICALITY_EXPONENT,
                budget=len(socket.PLACER_SEEDS), mode="first-pass",
                extra=("--router", factory.ROUTER), required=factory.PLACER_QOR_CLOCKS,
                gpu_devices=(0,), env=env, audit_source_root=root)
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        coleco_expansion.validate_routed_shell(output / "routed.json", version=2)
        evidence = factory.validate_build_evidence(output, root)
        mapping, map_evidence = rom_map.build_rom_map(
            database, (output / "core.rbf").read_bytes(),
            routed=json.loads((output / "routed.json").read_text()),
            lane_rows=ROM_LANES, reserved_rect=(1769, 32, 2806, 1800))
        map_bytes = (json.dumps(mapping, sort_keys=True, separators=(",", ":")) + "\n").encode()
        _write_atomic(output / "rom-map.json", map_bytes)
        evidence["rom_map"] = {"file": "rom-map.json", "size": len(map_bytes),
                               "sha256": hashlib.sha256(map_bytes).hexdigest()}
        evidence["rom_map_database"] = map_evidence
        evidence["route"].update(placer_seed=winner.seed, placer_heap_timingweight=winner.weight,
                                 placer_qor_mode="first-pass")
        evidence.update({"build_id": build_id, "device": factory.TARGET, "top": factory.TOP,
                         "inputs": {name: _sha256(root / name) for name in sorted(PINNED_INPUTS)},
                         "tools": identities, "execution": execution,
                         "expansion_rect": coleco_expansion.SOCKET_RECT_V2})
        _write_atomic(output / "build-summary.json", (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities)
        _write_atomic(output / "manifest.toml", encoded)
        if {name: tool.identity for name, tool in socket.authenticate_tools(root, cache_root).items()} != identities:
            raise BuildError("Coleco MegaCart compiler identity changed")
        if require_clean_source(root) != (repository, revision) or rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError("Coleco MegaCart source or database changed")
        invocation.verify()
        if create_build_record(root, repository, revision, identities, execution) != record:
            raise BuildError("Coleco MegaCart functional inputs changed")
        return export_package(encoded, output / "core.rbf", package_store,
                              rom_map=output / "rom-map.json")
    except Exception:
        for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
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
    args = parser.parse_args(argv)
    try:
        print(build(args.root, args.package_output, cache_root=args.cache_root))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-coleco-megacart: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
