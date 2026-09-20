#!/usr/bin/env python3
"""Build the original FES Catch game with the shared application shell."""
from __future__ import annotations
import argparse
import json
from pathlib import Path
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_demo as demo
from scripts import fes_build_common as board
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import encode_manifest
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_catch.py"
OUTPUT = Path("build/fes-catch")
GAME_SOURCES = tuple("cores/fes-demo/rtl/" + name for name in
    ("fes_catch_game.v", "fes_catch_core.v", "fes_catch_audio.v"))
PINNED_INPUTS = (*demo.PINNED_INPUTS, demo.AUDIO_QSF, *demo.AUDIO_SOURCES,
    *GAME_SOURCES, RECIPE, "scripts/functional_execution.py")
_authenticate_tools = board._authenticate_tools

def _require_clean_source(root, *, identity_version=2):
    return board._require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)

@guard_functional_source
def create_build_record(root, repository, revision, identities, *, identity_version=2, execution=None):
    fields = json.loads(demo.create_build_record(root, repository, revision, identities, audio=True))
    fields.update(recipe=RECIPE, abi_definition=demo.ABI_DEFINITION,
                  recipe_sha256=board._sha256(root / RECIPE))
    fields["parameters"]["application"] = "catch"
    if identity_version == 2:
        fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS),
            execution, pinned_inputs=PINNED_INPUTS)
    elif identity_version != 1:
        raise board.BuildError("unsupported build identity version")
    return encode_build_record(fields)

def manifest(record, evidence, repository, revision, identities):
    import tomllib
    fields = tomllib.loads(demo.manifest(record, evidence, repository, revision, identities, audio=True).decode())
    fields["core"] = {"id": "fes.catch", "name": "FES Catch", "version": "1.0.0",
        "description": "Original ROM-less paddle game; Left/Right move, A restarts"}
    return encode_manifest(fields)

@guard_functional_source
def build(root=ROOT, *, cache_root=None, identity_version=2, gpu_device=0):
    root = Path(root).resolve()
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    tools = _authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    invocation = FunctionalInvocation(tools, gpu_device) if identity_version == 2 else None
    output = None
    try:
        record = create_build_record(root, repository, revision, identities,
            identity_version=identity_version, execution=invocation.inputs if invocation else None)
        output = board._prepare_output(root, relative=OUTPUT, build_outputs=demo.BUILD_OUTPUTS)
        board._write_atomic(output / "build-inputs.json", record)
        commands = demo.build_commands(root, build_identity(record),
            {name: tools[name].path for name in ("yosys", "nextpnr-mistral")}, audio=True, catch=True)
        options = {"env": invocation.env, "audit_source_root": root} if invocation else {}
        board._run_tool(commands[0], root, output / "yosys.log", output_relative=OUTPUT, **options)
        route = commands[1] + (("--gpu-device", str(gpu_device)) if invocation else ())
        board._run_tool(route, root, output / "nextpnr.log", output_relative=OUTPUT, **options)
        evidence = demo.board_evidence.validate_build_evidence(output, root, audio=True,
            ordinary_resources=demo.ORDINARY_RESOURCES, required_resources=demo.REQUIRED_RESOURCES,
            forbidden_resources=demo.FORBIDDEN_RESOURCES, required_zero_resources=demo.REQUIRED_ZERO_RESOURCES)
        demo.audio_pin_evidence(root, output)
        evidence.update(build_id=build_identity(record), device=board.TARGET, tools=identities,
            top="top", inputs={p: board._sha256(root / p) for p in sorted(PINNED_INPUTS)},
            audio_pins={"status": "pass", "pins": demo.AUDIO_PINS})
        if invocation:
            evidence["execution"] = invocation.inputs
        board._write_atomic(output / "build-summary.json", (json.dumps(evidence, sort_keys=True, indent=2) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities)
        board._write_atomic(output / "manifest.toml", encoded)
        final_tools = _authenticate_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise board.BuildError("authenticated tool identity changed during build")
        if _require_clean_source(root, identity_version=identity_version) != (repository, revision):
            raise board.BuildError("source identity changed during build")
        if invocation:
            invocation.verify()
            if create_build_record(root, repository, revision, identities,
                identity_version=identity_version, execution=invocation.inputs) != record:
                raise board.BuildError("functional source inputs changed during build")
        return export_package(encoded, output / "core.rbf", root / "build/packages")
    except Exception:
        if output is not None:
            board._invalidate_failed_artifact(output)
        raise
    finally:
        if invocation:
            invocation.close()

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(1, 2), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    args = parser.parse_args()
    try:
        print(build(args.root, cache_root=args.cache_root, identity_version=args.identity_version, gpu_device=args.gpu_device))
    except (board.BuildError, ValueError) as exc:
        print(f"FES Catch: {exc}", file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
