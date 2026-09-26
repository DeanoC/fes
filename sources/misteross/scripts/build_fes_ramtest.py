#!/usr/bin/env python3
"""Seal the RAM tester utility through the authenticated HIP lane.

The core speaks fes.application 1.0 and fixed 720p. Memory traffic is local
to the FPGA; the mailbox has no memory opcode. This recipe never programs a kit.
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

QSF = "cores/fes-ramtest/constraints.qsf"
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json",
    "yosys.log", "nextpnr.log", "build-summary.json", "manifest.toml",
)
ORDINARY_RESOURCES = frozenset({
    "MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO",
    "altiobuf_bidir",
})
REQUIRED_RESOURCES = {
    "altera_pll": 1,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
    "cyclonev_hps_interface_fpga2sdram": 1,
}
FORBIDDEN_RESOURCES = frozenset({
    "MISTRAL_M10K", "MISTRAL_MLAB", "MISTRAL_MUL9X9", "MISTRAL_MUL18X18",
    "MISTRAL_MUL18X19", "MISTRAL_MUL18X19_COMBINED", "MISTRAL_MUL27X27",
})
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_ramtest.py"
ABI_DEFINITION = "cores/fes-common/generated/fes_application.vh"
RTL_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_application_gp.v",
    "cores/fes-common/rtl/fes_video_720p.v",
    "cores/fes-ramtest/rtl/mem_channel.v",
    "cores/fes-ramtest/rtl/ram_font.v",
    "cores/fes-ramtest/rtl/ram_display.v",
    "cores/fes-ramtest/rtl/sdram_addon_port.v",
    "cores/fes-ramtest/rtl/hps_ddr_port.v",
    "cores/fes-ramtest/rtl/top.v",
)
RAM_PLL = "cores/fes-ramtest/rtl/ram_pll.v"
PINNED_INPUTS = (
    RECIPE, "scripts/compiler_read_audit.py", "scripts/source_repository.py",
    "scripts/functional_execution.py", "scripts/fes_build_common.py",
    "scripts/fes_de10nano_evidence.py", ABI_DEFINITION, "toolchain.lock",
    QSF, board_evidence.SDC, *RTL_SOURCES,
)
OUTPUT = Path("build/fes-ramtest")
OUTPUT_100 = Path("build/fes-ramtest-100")
OUTPUT_130 = Path("build/fes-ramtest-130")
TOOLCHAIN_LOCK_130 = "toolchains/ramtest-130.lock"
TOOLCHAIN_ROOT_130 = Path("build/toolchain-ramtest-130")
# ramtest-130.lock adds the calibrated placement prediction that closes 130 MHz.
YOSYS_130 = "1bf1ff3d709dc8182cfa61701620d87181516941"
NEXTPNR_130 = "04a233c83edfb74c2cedce03a1864cd0d27ff3bd"
MEMORY_PLL_100 = {
    "duty_cycle0": "00000000000000000000000000110010",
    "duty_cycle1": "00000000000000000000000000110010",
    "fractional_vco_multiplier": "false",
    "number_of_clocks": "00000000000000000000000000000010",
    "operation_mode": "direct",
    "output_clock_frequency0": "100.0 MHz",
    "output_clock_frequency1": "100.0 MHz",
    "phase_shift0": "0 ps",
    "phase_shift1": "5000 ps",
    "reference_clock_frequency": "50.0 MHz",
}
MEMORY_PLL_130 = {**MEMORY_PLL_100,
    "output_clock_frequency0": "130.0 MHz",
    "output_clock_frequency1": "130.0 MHz",
    "phase_shift1": "6538 ps",
}


def high_speed(memory_mhz: int) -> bool:
    return memory_mhz in (100, 130)


def output_for(memory_mhz: int) -> Path:
    return {50: OUTPUT, 100: OUTPUT_100, 130: OUTPUT_130}[memory_mhz]


def seed_for(memory_mhz: int) -> int:
    return {50: 1, 100: 6, 130: 2}[memory_mhz]


def inputs_for(memory_mhz: int) -> tuple[str, ...]:
    if memory_mhz == 130:
        return tuple(path for path in PINNED_INPUTS if path != "toolchain.lock") + (TOOLCHAIN_LOCK_130, RAM_PLL)
    return PINNED_INPUTS + ((RAM_PLL,) if memory_mhz == 100 else ())


def authenticate_for(root: Path, memory_mhz: int, cache_root: Path | None):
    if memory_mhz == 130:
        return board._authenticate_tools(
            root, lock_path=root / TOOLCHAIN_LOCK_130,
            toolchain_root=root / TOOLCHAIN_ROOT_130,
            expected_commits={**board.EXPECTED_TOOL_COMMITS, "yosys": YOSYS_130, "nextpnr": NEXTPNR_130},
            cache_root=cache_root,
        )
    return board._authenticate_tools(root, cache_root=cache_root)


def record_fields(root: Path, repository: str, revision: str, identities: dict[str, str], *, memory_mhz: int = 50) -> dict:
    return {
        "format": 1, "repository": repository, "revision": revision,
        "recipe": RECIPE, "recipe_sha256": board._sha256(board._regular_input(root, RECIPE)),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": board._sha256(board._regular_input(root, ABI_DEFINITION)),
        "dependencies": {}, "tools": identities,
        "parameters": {
            "device": board.TARGET, "gpu_architectures": board.FES_GPU_ARCHITECTURES,
            "gpu_backend": "hip", "router": "gpu", "seed": seed_for(memory_mhz), "top": "top",
            "pixel_clock_hz": 74_250_000, "reference_clock_hz": 50_000_000,
            "memory_clock_hz": memory_mhz * 1_000_000, "pll_fractional_vco_multiplier": True,
        },
    }


@guard_functional_source
def create_build_record(root, repository, revision, identities, *, identity_version=2, execution=None, memory_mhz=50):
    if identity_version != 2:
        raise board.BuildError("unsupported build identity version")
    return encode_build_record(functional_record_fields(
        root, record_fields(root, repository, revision, identities, memory_mhz=memory_mhz),
        source_roots_for_inputs(inputs_for(memory_mhz)),
        execution, pinned_inputs=inputs_for(memory_mhz)))


def build_commands(root: Path, build_id: str, tools: dict[str, Path], *, memory_mhz: int = 50):
    if board.HEX32_RE.fullmatch(build_id) is None:
        raise board.BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise board.BuildError("build commands require authenticated tool paths")
    output = output_for(memory_mhz).as_posix()
    program = (
        "read_verilog -sv "
        + (f"-D RAM_RATE_SWEEP=1 -D RAM_{memory_mhz}_ONLY=1 -D RAM_OSS_HIGH_SPEED=1 " if high_speed(memory_mhz) else "")
        + "-I cores/fes-common/generated "
        + " ".join(RTL_SOURCES + ((RAM_PLL,) if high_speed(memory_mhz) else ()))
        + f"; chparam -set BUILD_ID 128'h{build_id} top; "
        "synth_intel_alm -nobram -nolutram -nodsp -top top; "
        f"stat; write_json {output}/synth.json"
    )
    return (
        (str(tools["yosys"]), "-p", program),
        (str(tools["nextpnr-mistral"]), "--json", f"{output}/synth.json",
         "--device", board.TARGET, "--qsf", QSF, "--sdc", board_evidence.SDC,
         "--freq", "74.25", "--seed", str(seed_for(memory_mhz)), "--router", "gpu",
         "--rbf", f"{output}/core.rbf", "--compress-rbf",
         "--write", f"{output}/routed.json", "--report", f"{output}/timing.json",
         "--detailed-timing-report"),
    )


def manifest(record: bytes, evidence: dict, repository: str, revision: str, identities: dict[str, str], *, memory_mhz: int = 50) -> bytes:
    return encode_manifest({
        "format": 2,
        "core": {
            "id": "fes.ramtest",
            "name": "FES RAM Tester",
            "description": f"Fixed-720p utility that pattern-tests SDRAM at {memory_mhz} MHz and the HPS DDR bridge",
            "version": "1.0.0",
        },
        "target": {"platform": "de10_nano", "device": board.TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", **evidence["rbf"]},
        "abi": {"id": "fes.application", "major": 1, "minor": 0},
        "interfaces": [
            {"id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True},
            {"id": "fes.gamepad", "major": 1, "minor": 0, "required": True},
        ],
        "build": {"id": build_identity(record), "repository": repository, "revision": revision,
                  "recipe_sha256": json.loads(record)["recipe_sha256"],
                  "toolchain": "; ".join(f"{key} {identities[key]}" for key in sorted(identities))},
    })


def require_clean_source(root, pinned_inputs):
    try:
        return board._require_clean_source(root, pinned_inputs=pinned_inputs)
    except ValueError as exc:
        raise board.BuildError(str(exc)) from exc


@guard_functional_source
def build(root: Path = ROOT, package_store=None, *, cache_root: Path | None = None, identity_version=2, gpu_device=0, memory_mhz=50) -> Path:
    root = Path(root).resolve()
    if identity_version != 2:
        raise board.BuildError("unsupported build identity version")
    if memory_mhz not in (50, 100, 130):
        raise board.BuildError("OSS RAM tester supports 50, 100, or 130 MHz")
    pinned_inputs = inputs_for(memory_mhz)
    output_relative = output_for(memory_mhz)
    package_store = root / "build/packages" if package_store is None else Path(package_store).resolve()
    if package_store != root / "build/packages":
        raise board.BuildError("package store must be build/packages")
    repository, revision = require_clean_source(root, pinned_inputs)
    authenticated = authenticate_for(root, memory_mhz, cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated, gpu_device)
    record = create_build_record(root, repository, revision, identities,
                                 execution=invocation.inputs, memory_mhz=memory_mhz)
    output = board._prepare_output(root, relative=output_relative, build_outputs=BUILD_OUTPUTS)
    board._write_atomic(output / "build-inputs.json", record)
    try:
        commands = build_commands(root, build_identity(record),
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
            memory_mhz=memory_mhz)
        board._run_tool(commands[0], root, output / "yosys.log", output_relative=output_relative, env=invocation.env, audit_source_root=root)
        board._run_tool(commands[1] + ("--gpu-device", str(gpu_device)), root, output / "nextpnr.log",
                        output_relative=output_relative, env=invocation.env, audit_source_root=root)
        evidence = board_evidence.validate_build_evidence(
            output, root, memory_clock_mhz=float(memory_mhz),
            capture_clock_mhz=float(memory_mhz) if high_speed(memory_mhz) else None,
            memory_pll_parameters={100: MEMORY_PLL_100, 130: MEMORY_PLL_130}.get(memory_mhz),
            ordinary_resources=ORDINARY_RESOURCES, required_resources=REQUIRED_RESOURCES,
            forbidden_resources=FORBIDDEN_RESOURCES, required_zero_resources=REQUIRED_ZERO_RESOURCES)
        evidence.update({"build_id": build_identity(record), "device": board.TARGET,
                         "inputs": {p: board._sha256(root / p) for p in sorted(pinned_inputs)},
                         "tools": identities, "top": "top", "execution": invocation.inputs})
        board._write_atomic(output / "build-summary.json",
                            (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode())
        encoded = manifest(record, evidence, repository, revision, identities, memory_mhz=memory_mhz)
        board._write_atomic(output / "manifest.toml", encoded)
        final_tools = authenticate_for(root, memory_mhz, cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise board.BuildError("authenticated tool identity changed during build")
        if require_clean_source(root, pinned_inputs) != (repository, revision):
            raise board.BuildError("source identity changed during build")
        invocation.verify()
        if create_build_record(root, repository, revision, identities,
                               execution=invocation.inputs, memory_mhz=memory_mhz) != record:
            raise board.BuildError("functional source inputs changed during build")
        return export_package(encoded, output / "core.rbf", package_store)
    except Exception:
        board._invalidate_failed_artifact(output)
        raise
    finally:
        invocation.close()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--gpu-device", type=int, default=0)
    parser.add_argument("--memory-mhz", type=int, choices=(50, 100, 130), default=50)
    args = parser.parse_args()
    print(build(args.root, cache_root=args.cache_root, gpu_device=args.gpu_device,
                memory_mhz=args.memory_mhz))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
