#!/usr/bin/env python3
"""Build and seal the deterministic standalone FES Pong package."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields


from scripts.fes_build_common import (
    AuthenticatedTool,
    BuildError,
    EXPECTED_TOOL_COMMITS,
    FES_GPU_ARCHITECTURES,
    FES_GPU_BACKEND,
    FES_GPU_ROUTER,
    FES_TOOLCHAIN_CONFIGURATION,
    HEX32_RE,
    HEX40_RE,
    HEX64_RE,
    TARGET,
    TOP,
    _authenticate_shared_tools,
    _authenticate_tools,
    _cell_counts,
    _contains_symlink,
    _fes_hip_local_provision_hint,
    _git,
    _i2c_evidence,
    _invalidate_failed_artifact,
    _prepare_output,
    _probe_authenticated_tool,
    _read_evidence,
    _read_json,
    _regular_input,
    _require_clean_source as require_clean_source,
    _require_gpu_backend,
    _run_tool,
    _sha256,
    _write_atomic,
)
from scripts.fes_de10nano_evidence import SDC, PLL_PARAMETERS, REFERENCE_SDC_BYTES, REFERENCE_CONSTRAINT_LOG, PLL_ROUTE_LOG
from scripts.fes_de10nano_evidence import validate_build_evidence as validate_board_evidence

ROOT = Path(__file__).resolve().parents[1]
OUTPUT_RELATIVE = Path("build/fes-pong")
QSF = "cores/fes-pong/constraints.qsf"
RECIPE = "scripts/build_fes_pong.py"
ABI_DEFINITION = "cores/fes-pong/generated/fes_gp.vh"
RTL_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-pong/rtl/top.v",
    "cores/fes-pong/rtl/fes_gp.v",
    "cores/fes-common/rtl/fes_video_720p.v",
    "cores/pong/rtl/pong_game.sv",
)
PINNED_INPUTS = (
    RECIPE, "scripts/source_repository.py",
    "scripts/fes_build_common.py",
    "scripts/fes_de10nano_evidence.py",
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
        "MISTRAL_MUL18X19",
        "MISTRAL_MUL18X19_COMBINED",
        "MISTRAL_MUL27X27",
    }
)
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})


def _require_clean_source(root: Path, *, pinned_inputs=PINNED_INPUTS, identity_version: int = 1):
    return require_clean_source(root, pinned_inputs=pinned_inputs, identity_version=identity_version)


def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
    *,
    identity_version: int = 1,
    execution: dict | None = None,
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
            "gpu_architectures": FES_GPU_ARCHITECTURES,
            "gpu_backend": FES_GPU_BACKEND,
            "pixel_clock_hz": 74_250_000,
            "pll_fractional_vco_multiplier": True,
            "reference_clock_hz": 50_000_000,
            "router": "gpu",
            "seed": 1,
            "top": TOP,
        },
    }
    if identity_version == 2:
        fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS), execution)
    elif identity_version != 1:
        raise BuildError("unsupported build identity version")
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
        "--router", "gpu",
        "--rbf", f"{OUTPUT_RELATIVE.as_posix()}/core.rbf",
        "--compress-rbf",
        "--write", f"{OUTPUT_RELATIVE.as_posix()}/routed.json",
        "--report", f"{OUTPUT_RELATIVE.as_posix()}/timing.json",
        "--detailed-timing-report",
    )
    return yosys, nextpnr


def validate_build_evidence(output: Path, source_root: Path = ROOT, *, audio: bool = False) -> dict:
    return validate_board_evidence(output, source_root, audio=audio,
        ordinary_resources=ORDINARY_RESOURCES, required_resources=REQUIRED_RESOURCES,
        forbidden_resources=FORBIDDEN_RESOURCES, required_zero_resources=REQUIRED_ZERO_RESOURCES)


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
            "version": "1.1.0",
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
            {"id": "fes.persistence.words", "major": 1, "minor": 0, "required": True},
            {"id": "fes.pong.progress", "major": 1, "minor": 0, "required": True},
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
    cache_root: Path | None = None,
    invocation=None,
    identity_version: int = 1,
) -> Path:
    build_id = build_identity(record)
    commands = build_commands(
        root,
        output,
        build_id,
        {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
    )
    _run_tool(commands[0], root, output / "yosys.log", **({"env": invocation.env} if invocation else {}), output_relative=OUTPUT_RELATIVE)
    if not (output / "synth.json").is_file():
        raise BuildError("Yosys did not produce synthesis evidence")
    _run_tool(commands[1] + (("--gpu-device", str(invocation.gpu_device)) if invocation else ()), root, output / "nextpnr.log", **({"env": invocation.env} if invocation else {}), output_relative=OUTPUT_RELATIVE)
    evidence = validate_build_evidence(output, root)
    if invocation:
        evidence["execution"] = invocation.inputs
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
    final_tools = _authenticate_tools(root, cache_root=cache_root)
    if {name: tool.identity for name, tool in final_tools.items()} != identities:
        raise BuildError("authenticated tool identity changed during build")
    final_repository, final_revision = _require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)
    if (final_repository, final_revision) != (repository, revision):
        raise BuildError("source identity changed during build")
    if invocation:
        invocation.verify()
        if create_build_record(root, repository, revision, identities,
            identity_version=identity_version, execution=invocation.inputs) != record:
            raise BuildError("functional source inputs changed during build")
    return export_package(manifest, output / "core.rbf", package_store)


def build(root: Path = ROOT, package_store: Path | None = None, *, cache_root: Path | None = None, identity_version: int = 1, gpu_device: int = 0) -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES Pong package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)
    authenticated = _authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated, gpu_device) if identity_version == 2 else None
    record = create_build_record(root, repository, revision, identities,
        identity_version=identity_version, execution=invocation.inputs if invocation else None)
    output = _prepare_output(root, relative=OUTPUT_RELATIVE, build_outputs=BUILD_OUTPUTS)
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
            cache_root=cache_root,
            **({"invocation": invocation, "identity_version": identity_version} if invocation else {}),
        )
    except Exception:
        _invalidate_failed_artifact(output)
        raise
    finally:
        if invocation:
            invocation.close()


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(1, 2), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    arguments = _parser().parse_args(argv)
    try:
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root,
                    identity_version=arguments.identity_version, gpu_device=arguments.gpu_device))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-pong: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
