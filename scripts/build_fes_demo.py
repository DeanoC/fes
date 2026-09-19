#!/usr/bin/env python3
"""Build two composable application examples through the authenticated HIP lane.

The shared Pong producer supplies the identical board/tool/electrical evidence
checks. This recipe owns application RTL, synthesis parameters and manifests;
it never mutates another producer's module globals or relaxes provenance.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_pong as board
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_demo.py"
ABI_DEFINITION = "cores/fes-common/generated/fes_application.vh"
RTL_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_application_gp.v",
    "cores/fes-common/rtl/fes_video_720p.v",
    "cores/fes-demo/rtl/fes_demo_core.v",
    "cores/fes-demo/rtl/top.v",
)
PINNED_INPUTS = (
    RECIPE, "scripts/build_fes_pong.py", ABI_DEFINITION, "toolchain.lock",
    board.QSF, board.SDC, *RTL_SOURCES,
)

def output_relative(media: bool) -> Path:
    return Path("build/fes-demo-media" if media else "build/fes-demo")

def create_build_record(root: Path, repository: str, revision: str,
                        identities: dict[str, str], *, media: bool = False) -> bytes:
    return encode_build_record({
        "format": 1, "repository": repository, "revision": revision,
        "recipe": RECIPE, "recipe_sha256": board._sha256(board._regular_input(root, RECIPE)),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": board._sha256(board._regular_input(root, ABI_DEFINITION)),
        "dependencies": {}, "tools": identities,
        "parameters": {
            "device": board.TARGET, "gpu_architectures": board.FES_GPU_ARCHITECTURES,
            "gpu_backend": "hip", "router": "gpu", "seed": 1, "top": "top",
            "pixel_clock_hz": 74_250_000, "reference_clock_hz": 50_000_000,
            "pll_fractional_vco_multiplier": True,
            "enable_gamepad": media, "enable_media": media,
        },
    })

def build_commands(root: Path, build_id: str, tools: dict[str, Path], *, media: bool = False):
    if board.HEX32_RE.fullmatch(build_id) is None:
        raise board.BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise board.BuildError("build commands require authenticated tool paths")
    output = output_relative(media).as_posix()
    program = (
        f"read_verilog -sv -I cores/fes-common/generated {' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} -set ENABLE_GAMEPAD {int(media)} "
        f"-set ENABLE_MEDIA {int(media)} top; "
        "synth_intel_alm -nobram -nolutram -nodsp -top top; "
        f"stat; write_json {output}/synth.json"
    )
    return (
        (str(tools["yosys"]), "-p", program),
        (str(tools["nextpnr-mistral"]), "--json", f"{output}/synth.json",
         "--device", board.TARGET, "--qsf", board.QSF, "--sdc", board.SDC,
         "--freq", "74.25", "--seed", "1", "--router", "gpu",
         "--rbf", f"{output}/core.rbf", "--compress-rbf",
         "--write", f"{output}/routed.json", "--report", f"{output}/timing.json",
         "--detailed-timing-report"),
    )

def manifest(record: bytes, evidence: dict, repository: str, revision: str,
             identities: dict[str, str], *, media: bool = False) -> bytes:
    interface_ids = ["fes.video.fixed-720p60"]
    if media:
        interface_ids += ["fes.gamepad", "fes.media.blob"]
    return encode_manifest({
        "format": 2,
        "core": {
            "id": "fes.demo-media" if media else "fes.demo",
            "name": "FES Palette Demo" if media else "FES Autonomous Demo",
            "description": "Procedural fixed-720p application reference; silent output",
            "version": "1.0.0",
        },
        "target": {"platform": "de10_nano", "device": board.TARGET,
                   "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", **evidence["rbf"]},
        "abi": {"id": "fes.application", "major": 1, "minor": 0},
        "interfaces": [{"id": name, "major": 1, "minor": 0, "required": True}
                       for name in interface_ids],
        "build": {"id": build_identity(record), "repository": repository, "revision": revision,
                  "recipe_sha256": json.loads(record)["recipe_sha256"],
                  "toolchain": "; ".join(f"{key} {identities[key]}" for key in sorted(identities))},
    })

def build(root: Path = ROOT, *, media: bool = False, cache_root: Path | None = None) -> Path:
    root = Path(root).resolve()
    repository, revision = board._require_clean_source(root, pinned_inputs=PINNED_INPUTS)
    authenticated = board._authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    record = create_build_record(root, repository, revision, identities, media=media)
    output = board._prepare_output(root, relative=output_relative(media))
    board._write_atomic(output / "build-inputs.json", record)
    try:
        commands = build_commands(root, build_identity(record),
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, media=media)
        board._run_tool(commands[0], root, output / "yosys.log")
        board._run_tool(commands[1], root, output / "nextpnr.log",
                        output_relative=output_relative(media))
        evidence = board.validate_build_evidence(output, root)
        evidence.update({"build_id": build_identity(record), "device": board.TARGET,
                         "inputs": {p: board._sha256(root / p) for p in sorted(PINNED_INPUTS)},
                         "tools": identities, "top": "top"})
        board._write_atomic(output / "build-summary.json",
                            (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities, media=media)
        board._write_atomic(output / "manifest.toml", encoded)
        final_tools = board._authenticate_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise board.BuildError("authenticated tool identity changed during build")
        if board._require_clean_source(root, pinned_inputs=PINNED_INPUTS) != (repository, revision):
            raise board.BuildError("source identity changed during build")
        return export_package(encoded, output / "core.rbf", root / "build/packages")
    except Exception:
        board._invalidate_failed_artifact(output)
        raise

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--media", action="store_true", help="gamepad + RGB palette asset variant")
    args = parser.parse_args()
    try:
        print(build(args.root, media=args.media, cache_root=args.cache_root))
    except (board.BuildError, ValueError) as exc:
        print(f"FES demo: {exc}", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
