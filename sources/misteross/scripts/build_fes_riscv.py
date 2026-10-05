#!/usr/bin/env python3
"""Build and seal the fes.riscv package through the authenticated HIP lane.

The original RV32I system (CPU, 32 KiB M10K RAM with the assembled firmware,
160x120 framebuffer, gamepad and timer I/O) shares the fixed-raster
fes.application shell with Pong and the demos. This recipe owns the RTL list,
firmware images, resource policy and manifest; the board/tool/electrical
evidence checks are the shared ones. It never programs hardware.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.compiler_read_audit import guard_functional_source
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts import fes_build_common as board
from scripts import fes_de10nano_evidence as board_evidence
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_riscv.py"
OUTPUT = Path("build/fes-riscv")
QSF = "cores/fes-pong/constraints.qsf"
ABI_DEFINITION = "cores/fes-common/generated/fes_application.vh"
CPU_SOURCES = (
    "cores/fes-common/rtl/riscv/fes_rv32_alu.sv",
    "cores/fes-common/rtl/riscv/fes_rv32_csr.sv",
    "cores/fes-common/rtl/riscv/fes_rv32_cpu.sv",
)
RTL_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_application_gp.v",
    "cores/fes-common/rtl/fes_video_720p.v",
    *CPU_SOURCES,
    "cores/fes-riscv/rtl/fes_riscv_lane_ram.sv",
    "cores/fes-riscv/rtl/fes_riscv_system.sv",
    "cores/fes-riscv/rtl/top.v",
)
FIRMWARE_SOURCE = "cores/fes-riscv/firmware/firmware.S"
FIRMWARE_ASSEMBLER = "cores/fes-riscv/firmware/assemble.py"
FIRMWARE_IMAGES = (
    "cores/fes-riscv/firmware/firmware.hex",
    "cores/fes-riscv/firmware/firmware.lane0.hex",
    "cores/fes-riscv/firmware/firmware.lane1.hex",
    "cores/fes-riscv/firmware/firmware.lane2.hex",
    "cores/fes-riscv/firmware/firmware.lane3.hex",
)
PINNED_INPUTS = (
    RECIPE, "scripts/compiler_read_audit.py", "scripts/source_repository.py",
    "scripts/functional_execution.py", "scripts/fes_build_common.py",
    "scripts/fes_de10nano_evidence.py", "scripts/search_placer_qor.py",
    ABI_DEFINITION, "toolchain.lock", QSF, board_evidence.SDC,
    *RTL_SOURCES, FIRMWARE_SOURCE, FIRMWARE_ASSEMBLER, *FIRMWARE_IMAGES,
)
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log", "nextpnr.log",
    "build-summary.json", "manifest.toml", "qor-ranking.json",
)
ORDINARY_RESOURCES = frozenset(
    {"MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO", "MISTRAL_M10K",
     "MISTRAL_M10K_TDP"}
)
REQUIRED_RESOURCES = {
    "altera_pll": 1,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
}
FORBIDDEN_RESOURCES = frozenset(
    {"MISTRAL_MLAB", "MISTRAL_MUL9X9", "MISTRAL_MUL18X18", "MISTRAL_MUL18X19",
     "MISTRAL_MUL18X19_COMBINED", "MISTRAL_MUL27X27", "cyclonev_hps_interface_fpga2sdram"}
)
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
PIXEL_CLOCK_MHZ = 74.25
# First placement that closes the pixel clock wins; the order is the recipe.
PLACER_SEEDS = (1, 2, 3, 4, 5, 6, 7, 8)
PLACER_TIMING_WEIGHT = 10
PLACER_CRITICALITY_EXPONENT = 2
PLACER_QOR_CLOCKS = ((None, PIXEL_CLOCK_MHZ),)
ROUTE_TIMEOUT_SECONDS = 1800
CORE = {"id": "fes.riscv", "name": "FES RISC-V", "version": "0.1.0",
        "description": "Original RV32I system: 32 KiB RAM, 160x120 framebuffer, "
                       "gamepad and timer-interrupt demonstration firmware"}
INTERFACES = ("fes.video.fixed-720p60", "fes.gamepad")

_authenticate_tools = board._authenticate_tools


def require_clean_source(root, *, identity_version=2):
    try:
        return board._require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)
    except ValueError as exc:
        raise board.BuildError(str(exc)) from exc


def require_current_firmware(root: Path) -> None:
    """The checked-in lane images must be what firmware.S assembles to."""
    import importlib.util
    spec = importlib.util.spec_from_file_location("fes_riscv_assemble", root / FIRMWARE_ASSEMBLER)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    try:
        program = module.assemble((root / FIRMWARE_SOURCE).read_text())
    except module.AssemblyError as exc:
        raise board.BuildError(f"firmware does not assemble: {exc}") from exc
    word_image, lane_images = module.images(program)
    for relative, text in zip(FIRMWARE_IMAGES, (word_image, *lane_images)):
        if board._regular_input(root, relative).read_text() != text:
            raise board.BuildError(f"stale firmware image {relative}; run {FIRMWARE_ASSEMBLER}")


def record_fields(root: Path, repository: str, revision: str, identities: dict[str, str]) -> dict:
    return {
        "format": 1, "repository": repository, "revision": revision,
        "recipe": RECIPE, "recipe_sha256": board._sha256(board._regular_input(root, RECIPE)),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": board._sha256(board._regular_input(root, ABI_DEFINITION)),
        "dependencies": {}, "tools": identities,
        "parameters": {
            "device": board.TARGET, "gpu_architectures": board.FES_GPU_ARCHITECTURES,
            "gpu_backend": "hip", "router": "gpu", "seed": PLACER_SEEDS[0],
            "seed_order": ",".join(str(seed) for seed in PLACER_SEEDS),
            "placer_heap_timingweight": PLACER_TIMING_WEIGHT,
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT, "top": "top",
            "pixel_clock_hz": 74_250_000, "reference_clock_hz": 50_000_000,
            "pll_fractional_vco_multiplier": True,
            "enable_gamepad": True, "cpu": "fes_rv32_cpu", "ram_bytes": 32768,
            "framebuffer": "160x120 RGB332", "firmware": FIRMWARE_SOURCE,
        },
    }


@guard_functional_source
def create_build_record(root, repository, revision, identities, *, identity_version=2, execution=None):
    if identity_version != 2:
        raise board.BuildError("unsupported build identity version")
    fields = record_fields(root, repository, revision, identities)
    return encode_build_record(functional_record_fields(root, fields,
        source_roots_for_inputs(PINNED_INPUTS), execution, pinned_inputs=PINNED_INPUTS))


def build_commands(build_id: str, tools: dict[str, Path], *, gpu_device: int = 0):
    """Synthesis and the first (reference) route; the search reuses the route shape."""
    if board.HEX32_RE.fullmatch(build_id) is None:
        raise board.BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise board.BuildError("build commands require authenticated tool paths")
    output = OUTPUT.as_posix()
    program = (
        f"read_verilog -sv -I cores/fes-common/generated {' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} top; "
        "synth_intel_alm -nolutram -nodsp -top top; "
        f"stat; write_json {output}/synth.json"
    )
    return (
        (str(tools["yosys"]), "-p", program),
        (str(tools["nextpnr-mistral"]), "--json", f"{output}/synth.json",
         "--device", board.TARGET, "--qsf", QSF, "--sdc", board_evidence.SDC,
         "--freq", f"{PIXEL_CLOCK_MHZ:g}", "--seed", str(PLACER_SEEDS[0]),
         "--router", "gpu", "--gpu-device", str(gpu_device),
         "--rbf", f"{output}/core.rbf", "--compress-rbf",
         "--write", f"{output}/routed.json", "--report", f"{output}/timing.json",
         "--detailed-timing-report"),
    )


def route_placement(root: Path, output: Path, nextpnr: Path, invocation, gpu_device: int):
    return route_after_synth(
        nextpnr=nextpnr, fixture=output / "synth.json", dest=output, device=board.TARGET,
        qsf=root / QSF, sdc=root / board_evidence.SDC, freq=f"{PIXEL_CLOCK_MHZ:g}",
        seeds=PLACER_SEEDS, weights=(PLACER_TIMING_WEIGHT,), critexp=PLACER_CRITICALITY_EXPONENT,
        budget=len(PLACER_SEEDS), mode="first-pass",
        extra=("--router", "gpu", "--gpu-device", str(gpu_device)),
        required=PLACER_QOR_CLOCKS, timeout=ROUTE_TIMEOUT_SECONDS,
        env=invocation.env, audit_source_root=root)


def manifest(record: bytes, evidence: dict, repository: str, revision: str, identities: dict[str, str]) -> bytes:
    return encode_manifest({
        "format": 2,
        "core": dict(CORE),
        "target": {"platform": "de10_nano", "device": board.TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", **evidence["rbf"]},
        "abi": {"id": "fes.application", "major": 1, "minor": 0},
        "interfaces": [{"id": name, "major": 1, "minor": 0, "required": True} for name in INTERFACES],
        "build": {"id": build_identity(record), "repository": repository, "revision": revision,
                  "recipe_sha256": json.loads(record)["recipe_sha256"],
                  "toolchain": "; ".join(f"{key} {identities[key]}" for key in sorted(identities))},
    })


@guard_functional_source
def build(root: Path = ROOT, package_store=None, *, cache_root: Path | None = None,
          identity_version=2, gpu_device=0) -> Path:
    root = Path(root).resolve()
    if identity_version != 2:
        raise board.BuildError("unsupported build identity version")
    package_store = root / "build/packages" if package_store is None else Path(package_store).resolve()
    if package_store != root / "build/packages":
        raise board.BuildError("fes.riscv package store must be build/packages")
    repository, revision = require_clean_source(root, identity_version=identity_version)
    require_current_firmware(root)
    authenticated = _authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated, gpu_device)
    output = None
    try:
        record = create_build_record(root, repository, revision, identities,
                                     identity_version=identity_version, execution=invocation.inputs)
        output = board._prepare_output(root, relative=OUTPUT, build_outputs=BUILD_OUTPUTS)
        board._write_atomic(output / "build-inputs.json", record)
        commands = build_commands(build_identity(record),
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, gpu_device=gpu_device)
        board._run_tool(commands[0], root, output / "yosys.log", output_relative=OUTPUT,
                        env=invocation.env, audit_source_root=root)
        try:
            winner = route_placement(root, output, authenticated["nextpnr-mistral"].path, invocation, gpu_device)
        except SearchError as exc:
            raise board.BuildError(str(exc)) from exc
        evidence = board_evidence.validate_build_evidence(output, root,
            ordinary_resources=ORDINARY_RESOURCES, required_resources=REQUIRED_RESOURCES,
            forbidden_resources=FORBIDDEN_RESOURCES, required_zero_resources=REQUIRED_ZERO_RESOURCES)
        evidence["route"]["placer_seed"] = winner.seed
        evidence["route"]["placer_heap_timingweight"] = winner.weight
        evidence.update({"build_id": build_identity(record), "device": board.TARGET,
                         "inputs": {p: board._sha256(root / p) for p in sorted(PINNED_INPUTS)},
                         "tools": identities, "top": "top", "execution": invocation.inputs})
        board._write_atomic(output / "build-summary.json",
                            (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities)
        board._write_atomic(output / "manifest.toml", encoded)
        final_tools = _authenticate_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise board.BuildError("authenticated tool identity changed during build")
        if require_clean_source(root, identity_version=identity_version) != (repository, revision):
            raise board.BuildError("source identity changed during build")
        invocation.verify()
        if create_build_record(root, repository, revision, identities,
                               identity_version=identity_version, execution=invocation.inputs) != record:
            raise board.BuildError("functional source inputs changed during build")
        return export_package(encoded, output / "core.rbf", package_store)
    except Exception:
        if output is not None:
            board._invalidate_failed_artifact(output)
        raise
    finally:
        invocation.close()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(2,), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    args = parser.parse_args()
    try:
        print(build(args.root, args.package_output, cache_root=args.cache_root,
                    identity_version=args.identity_version, gpu_device=args.gpu_device))
    except (board.BuildError, ValueError) as exc:
        print(f"FES RISC-V: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
