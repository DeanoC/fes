#!/usr/bin/env python3
"""Build and seal fes.pc386-diag, the pc386 M4 diagnostic package.

docs/pc386-machine-design.md, phase M4: the pc386 machine (AT core, VGA,
capture, presenter, the display plane on DDR port 2, OPL2, Sound Blaster DSP,
mixer) with pc386_bus_master in the CPU's place, behind fes.computer 1.0 with
the session-display plane, HPS DDR and 48 kHz audio. The bus master takes
its commands from a ring in the DDR window; scripts/pc386_diag.py writes the
programs and cores/fes-pc386/diagnostic runs them on a kit. The system clock
is sealed at SYSTEM_CLOCK_HZ and every guest time base is a rational enable
of it, set here from that frequency. The package is a development package:
it is not registered in the FES recipe table. This recipe never programs
hardware.

  python3 scripts/build_fes_pc386_oss.py --cache-root /abs/out/cache/misteross-toolchains
"""
from __future__ import annotations

import argparse
import json
import math
from pathlib import Path
import re
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts.compiler_read_audit import guard_functional_source
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts import fes_build_common as board
from scripts import fes_de10nano_evidence as board_evidence
from scripts import pc386_diag
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package, functional_record_fields
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_pc386_oss.py"
OUTPUT = Path("build/fes-pc386-diag")
LOCK = "cores/fes-pc386/toolchain.lock"
TOOLCHAIN_ROOT = "build/toolchain/fes-pc386"
QSF = "cores/fes-pc386/constraints-oss.qsf"
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
DDR_ATOM = "cores/fes-common/rtl/fes_hps_ddr_atom.v"
SYSTEM_CLOCK_HZ = 61_440_000
SYSTEM_CLOCK = "61.44 MHz"
PIXEL_CLOCK_MHZ = 74.25
AUDIO_CLOCK_MHZ = 12.288
INCLUDES = (
    "cores/fes-common/generated/fes_application.vh",
    "cores/fes-common/generated/fes_computer.vh",
    "cores/fes-common/generated/fes_display_plane.vh",
    "cores/fes-common/vendor/kf8253/KF8253_Definitions.svh",
    "cores/fes-common/vendor/kf8259/KF8259_Common_Package.svh",
)
INCLUDE_DIRS = ("cores/fes-common/generated", "cores/fes-common/vendor/kf8259", "cores/fes-common/vendor/kf8253")
OPL2 = "cores/fes-pc386/vendor/opl2_fpga"
RTL_SOURCES = (
    *(f"cores/fes-common/vendor/kf8259/{name}.sv" for name in (
        "KF8259", "KF8259_Bus_Control_Logic", "KF8259_Control_Logic", "KF8259_In_Service",
        "KF8259_Interrupt_Request", "KF8259_Priority_Resolver")),
    *(f"cores/fes-common/vendor/kf8253/{name}.sv" for name in ("KF8253", "KF8253_Control_Logic", "KF8253_Counter")),
    "cores/fes-common/rtl/fes_dma8237.sv",
    *(f"cores/fes-pc386/rtl/{name}.sv" for name in (
        "pc386_rate", "pc386_io", "pc386_pic", "pc386_sysctl", "pc386_kbc", "pc386_rtc", "pc386_ata",
        "pc386_at_core", "pc386_ram", "pc386_vga", "pc386_vga_scanout", "pc386_presenter", "pc386_plane_bridge",
        "pc386_port2", "pc386_machine")),
    "cores/fes-common/rtl/fes_display_plane.v",
    "cores/fes-common/rtl/fes_display_cdc.v",
    "cores/fes-menu/rtl/fes_menu_control.v",
    "cores/fes-menu/rtl/fes_menu_video.v",
    "cores/fes-menu/rtl/fes_menu_reader.v",
    f"{OPL2}/opl2_pkg.sv",
    *(f"{OPL2}/{name}.sv" for name in (
        "pipeline_sr", "mem_simple_dual_port", "mem_simple_dual_port_async_read", "mem_single_bank",
        "mem_single_bank_reset", "opl2_log_sine_lut", "opl2_exp_lut", "ksl_add_rom", "tremolo", "vibrato",
        "calc_phase_inc", "calc_envelope_shift", "calc_rhythm_phase", "envelope_generator", "phase_generator",
        "operator", "control_operators", "dac_prep", "channels")),
    *(f"cores/fes-pc386/rtl/{name}.sv" for name in (
        "pc386_opl2", "pc386_sb", "pc386_mixer", "pc386_audio", "pc386_bus_master", "pc386_diag")),
    "cores/fes-pc386/rtl/pc386_system_pll.v",
    "cores/fes-common/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-common/rtl/fes_hps_ddr.v",
    "cores/fes-common/rtl/fes_hps_ddr_guard.v",
    "cores/fes-pc386/rtl/top.sv",
)
PINNED_INPUTS = (
    RECIPE, "scripts/pc386_diag.py", "scripts/compiler_read_audit.py", "scripts/source_repository.py",
    "scripts/functional_execution.py", "scripts/fes_build_common.py", "scripts/fes_de10nano_evidence.py",
    "scripts/search_placer_qor.py", "scripts/pnr_recipe.py", "scripts/nextpnr_lease.py",
    ABI_DEFINITION, LOCK, QSF, board_evidence.SDC, DDR_ATOM, *INCLUDES, *RTL_SOURCES,
)
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log", "nextpnr.log",
    "build-inputs.json", "build-summary.json", "manifest.toml", "qor-ranking.json",
)
# The presenter's blends and weights and the plane's blend use DSP blocks;
# the CPU's recipe permits them. No LUT RAM: async reads become registers.
ORDINARY_RESOURCES = frozenset({
    "MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO", "MISTRAL_M10K",
    "MISTRAL_M10K_TDP", "MISTRAL_MUL9X9", "MISTRAL_MUL18X18", "MISTRAL_MUL18X19", "MISTRAL_MUL18X19_COMBINED",
    "MISTRAL_MUL27X27",
})
REQUIRED_RESOURCES = {
    "altera_pll": 2,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
    "cyclonev_hps_interface_fpga2sdram": 1,
}
FORBIDDEN_RESOURCES = frozenset({"MISTRAL_MLAB"})
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
# First placement that closes every clock wins, in this order.
PLACER_SEEDS = (1, 2, 3, 4, 5, 6, 7, 8)
PLACER_TIMING_WEIGHT = 10
PLACER_CRITICALITY_EXPONENT = 2
ROUTE_TIMEOUT_SECONDS = 3600
CORE = {"id": "fes.pc386-diag", "name": "FES pc386 M4 diagnostic", "version": "0.1.0",
        "description": "The pc386 machine without its CPU: AT core, VGA, 720p presenter, OPL2 and Sound "
                       "Blaster 2.0, driven by a bus master that runs diagnostic commands from the HPS DDR "
                       "window. Development only; it boots nothing on its own."}
INTERFACES = ("fes.video.fixed-720p60", "fes.audio.pcm-s16-stereo-48k", "fes.memory.hps-ddr",
              "fes.video.session-display", "fes.fabric.video.display-plane")


def _authenticate_tools(root, *, cache_root=None):
    return board._authenticate_tools(root, lock_path=root / LOCK, toolchain_root=root / TOOLCHAIN_ROOT,
                                     cache_root=cache_root)


def require_clean_source(root, *, identity_version=2):
    try:
        return board._require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)
    except ValueError as exc:
        raise board.BuildError(str(exc)) from exc


def placement_seeds(single_seed=None):
    if single_seed is None:
        return PLACER_SEEDS
    if type(single_seed) is not int or not 1 <= single_seed <= 0x7fffffff:
        raise board.BuildError("single placement seed must be an integer from 1 to 2147483647")
    return (single_seed,)


def guest_rates() -> dict[str, int]:
    """The N/D pairs for the sealed clock; the PLL string must name the same frequency."""
    match = re.fullmatch(r"(\d+(?:\.\d+)?) MHz", SYSTEM_CLOCK)
    if match is None or round(float(match.group(1)) * 1e6) != SYSTEM_CLOCK_HZ:
        raise board.BuildError("SYSTEM_CLOCK and SYSTEM_CLOCK_HZ disagree")
    return pc386_diag.rates(SYSTEM_CLOCK_HZ)


def record_fields(root: Path, repository: str, revision: str, identities: dict[str, str], *, single_seed=None) -> dict:
    seeds = placement_seeds(single_seed)
    return {
        "format": 1, "repository": repository, "revision": revision,
        "recipe": RECIPE, "recipe_sha256": board._sha256(board._regular_input(root, RECIPE)),
        "abi_definition": ABI_DEFINITION,
        "abi_definition_sha256": board._sha256(board._regular_input(root, ABI_DEFINITION)),
        "dependencies": {}, "tools": identities,
        "parameters": {
            "device": board.TARGET, "top": board.TOP,
            "gpu_architectures": board.FES_GPU_ARCHITECTURES, "gpu_backend": "hip", "router": "gpu",
            "seed": seeds[0], "seed_order": ",".join(str(seed) for seed in seeds),
            "placer_heap_timingweight": PLACER_TIMING_WEIGHT,
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT,
            "toolchain_lock": LOCK, "toolchain_lock_sha256": board._sha256(board._regular_input(root, LOCK)),
            "system_clock_hz": SYSTEM_CLOCK_HZ, "pixel_clock_hz": 74_250_000, "audio_clock_hz": 12_288_000,
            "reference_clock_hz": 50_000_000, "pll_count": 2, "audio_sample_hz": 48_000,
            "guest_rates": json.dumps(guest_rates(), sort_keys=True, separators=(",", ":")),
            "bus_master": "pc386_bus_master DDR command ring at window offset 0x01000000",
            "synthesis": "synth_intel_alm -nolutram",
        },
    }


@guard_functional_source
def create_build_record(root, repository, revision, identities, *, identity_version=2, execution=None,
                        single_seed=None):
    if identity_version != 2:
        raise board.BuildError("unsupported build identity version")
    fields = record_fields(root, repository, revision, identities, single_seed=single_seed)
    return encode_build_record(functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS),
                                                        execution, pinned_inputs=PINNED_INPUTS))


def synthesis_command(build_id: str, yosys: Path) -> tuple[str, ...]:
    if board.HEX32_RE.fullmatch(build_id) is None:
        raise board.BuildError("build ID must be 32 lowercase hexadecimal characters")
    parameters = " ".join(f"-set {name} {value}" for name, value in guest_rates().items())
    program = (
        f"read_verilog -lib {DDR_ATOM}; "
        f"read_verilog -sv {' '.join(f'-I {path}' for path in INCLUDE_DIRS)} {' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} -set SYSTEM_CLOCK \"{SYSTEM_CLOCK}\" {parameters} {board.TOP}; "
        f"synth_intel_alm -nolutram -top {board.TOP}; stat; write_json {OUTPUT.as_posix()}/synth.json"
    )
    return (str(yosys), "-p", program)


def route_placement(root: Path, output: Path, nextpnr: Path, invocation, gpu_device: int, *, single_seed=None):
    seeds = placement_seeds(single_seed)
    return route_after_synth(
        nextpnr=nextpnr, fixture=output / "synth.json", dest=output, device=board.TARGET,
        qsf=root / QSF, sdc=root / board_evidence.SDC, freq=f"{PIXEL_CLOCK_MHZ:g}",
        seeds=seeds, weights=(PLACER_TIMING_WEIGHT,), critexp=PLACER_CRITICALITY_EXPONENT,
        budget=len(seeds), mode="first-pass", extra=("--router", "gpu"), gpu_devices=(gpu_device,),
        required=((None, SYSTEM_CLOCK_HZ / 1e6), (None, PIXEL_CLOCK_MHZ), (None, AUDIO_CLOCK_MHZ)),
        timeout=ROUTE_TIMEOUT_SECONDS, env=invocation.env, audit_source_root=root)


def _frequency_row(fmax: object, expected: float, label: str) -> dict:
    if not isinstance(fmax, dict):
        raise board.BuildError("timing report has no structured fmax data")
    matches = []
    for name, fields in fmax.items():
        if not isinstance(name, str) or not isinstance(fields, dict):
            continue
        constraint, achieved = fields.get("constraint"), fields.get("achieved")
        if not all(isinstance(v, (int, float)) and not isinstance(v, bool) for v in (constraint, achieved)):
            continue
        if not math.isfinite(float(constraint)) or not math.isfinite(float(achieved)):
            raise board.BuildError(f"{label} timing frequencies must be finite")
        if abs(float(constraint) - expected) <= max(1e-6, expected * 5e-5):
            matches.append((name, float(constraint), float(achieved)))
    if len(matches) != 1:
        raise board.BuildError(f"timing report must contain exactly one {label} {expected:g} MHz constraint")
    name, constraint, achieved = matches[0]
    if achieved < constraint:
        raise board.BuildError(f"{label} timing achieved {achieved:g} MHz, below {constraint:g} MHz")
    return {"clock": name, "constraint_mhz": constraint, "achieved_mhz": achieved, "status": "pass"}


def _hps_ddr_evidence(design: dict, label: str, root: Path) -> dict:
    """The layout U-Boot latched, and the three ports this shell drives: the
    bus master on port 0, VRAM on port 1, the display on port 2."""
    normalized = board_evidence.layout_graph_with_constants(design)
    evidence = board_evidence.hps_ddr_layout_evidence(normalized, label, root, idle=False)
    cells = normalized["modules"][board.TOP].get("cells", {})
    memory = next(cell for cell in cells.values() if cell.get("type") == "cyclonev_hps_interface_fpga2sdram")
    ports = memory.get("connections", {})
    grounds = [["0"]] + [cell.get("connections", {}).get("Q") for cell in cells.values()
                         if cell.get("type") == "MISTRAL_CONST"
                         and set(str(cell.get("parameters", {}).get("LUT", ""))) == {"0"}
                         and isinstance(cell.get("connections", {}).get("Q"), list)
                         and len(cell["connections"]["Q"]) == 1]
    if any(ports.get(f"cmd_valid_{port}") in grounds for port in range(3)):
        raise board.BuildError(f"{label} design must drive fpga2sdram ports 0, 1 and 2")
    if any(ports.get(f"cmd_valid_{port}") not in grounds for port in range(3, 6)):
        raise board.BuildError(f"{label} design must tie fpga2sdram ports 3-5 low")
    return {**evidence, "bus_master_port": 0, "vram_port": 1, "display_port": 2}


def validate_build_evidence(output: Path, root: Path) -> dict:
    synthesis = board._read_json(output / "synth.json", "synthesis evidence")
    routed = board._read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(board.TOP), dict):
        raise board.BuildError("routed design does not contain the top module")
    for design, label in ((synthesis, "synthesized"), (routed, "routed")):
        board.reject_async_m10k_reads(design)
        board._i2c_evidence(design, label)
    counts = board._cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise board.BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0):
            raise board.BuildError(f"forbidden synthesis cell {name} is in use")
    memory = _hps_ddr_evidence(synthesis, "synthesized", root)
    _hps_ddr_evidence(routed, "routed", root)
    route_text = (output / "nextpnr.log").read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise board.BuildError("route log does not prove a complete routed design")
    gpu_backend = board._require_gpu_backend(route_text, "hip")
    for output_index, hz in ((0, SYSTEM_CLOCK_HZ), (1, 12_288_000)):
        if not re.search(rf"Info: PLL 'system_clock\.pll': fractional-N requested {hz}\.000000 Hz "
                         rf"\(output {output_index}\)", route_text):
            raise board.BuildError(f"route log lacks the system PLL's {hz} Hz output {output_index}")
    if not re.search(board_evidence.VIDEO_PLL_ROUTE_RE, route_text):
        raise board.BuildError("route log lacks the 74.25 MHz video PLL")
    timing = board._read_json(output / "timing.json", "timing report")
    rows = {label: _frequency_row(timing.get("fmax"), mhz, label)
            for label, mhz in (("system", SYSTEM_CLOCK_HZ / 1e6), ("pixel", PIXEL_CLOCK_MHZ),
                               ("audio", AUDIO_CLOCK_MHZ))}
    known = ORDINARY_RESOURCES | set(REQUIRED_RESOURCES) | FORBIDDEN_RESOURCES | REQUIRED_ZERO_RESOURCES
    resources = board.validate_timing_resources(timing.get("utilization"), known)
    for name, expected in REQUIRED_RESOURCES.items():
        if name not in resources or resources[name]["used"] != expected:
            raise board.BuildError(f"resource {name} must be exactly {expected}")
    for name in REQUIRED_ZERO_RESOURCES:
        if name not in resources or resources[name]["used"] != 0:
            raise board.BuildError(f"resource {name} must be exactly 0")
    rbf = output / "core.rbf"
    if rbf.is_symlink() or not rbf.is_file() or not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise board.BuildError(f"RBF must be a nonempty bounded regular file: {rbf}")
    return {
        "status": "pass",
        "route": {"status": "pass", "unrouted": False, "gpu_backend": gpu_backend},
        "timing": {**rows, "status": "pass"},
        "resources": resources,
        "synthesis_cells": {name: counts[name] for name in sorted(counts)},
        "hps_ddr": memory,
        "guest_rates": guest_rates(),
        "rbf": {"sha256": board._sha256(rbf), "size": rbf.stat().st_size},
    }


def manifest(record: bytes, evidence: dict, repository: str, revision: str, identities: dict[str, str]) -> bytes:
    return encode_manifest({
        "format": 2,
        "core": dict(CORE),
        "target": {"platform": "de10_nano", "device": board.TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", **evidence["rbf"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{"id": name, "major": 1, "minor": 0, "required": True} for name in INTERFACES],
        "build": {"id": build_identity(record), "repository": repository, "revision": revision,
                  "recipe_sha256": json.loads(record)["recipe_sha256"],
                  "toolchain": "; ".join(f"{key} {identities[key]}" for key in sorted(identities))},
    })


@guard_functional_source
def build(root: Path = ROOT, package_store=None, *, cache_root: Path | None = None,
          identity_version=2, gpu_device=0, single_seed=None) -> Path:
    placement_seeds(single_seed)
    root = Path(root).resolve()
    if identity_version != 2:
        raise board.BuildError("unsupported build identity version")
    package_store = root / "build/packages" if package_store is None else Path(package_store).resolve()
    if package_store != root / "build/packages":
        raise board.BuildError("fes.pc386-diag package store must be build/packages")
    repository, revision = require_clean_source(root, identity_version=identity_version)
    authenticated = _authenticate_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    invocation = FunctionalInvocation(authenticated, gpu_device)
    output = None
    try:
        record = create_build_record(root, repository, revision, identities, identity_version=identity_version,
                                     execution=invocation.inputs, single_seed=single_seed)
        output = board._prepare_output(root, relative=OUTPUT, build_outputs=BUILD_OUTPUTS)
        board._write_atomic(output / "build-inputs.json", record)
        board._run_tool(synthesis_command(build_identity(record), authenticated["yosys"].path), root,
                        output / "yosys.log", output_relative=OUTPUT, env=invocation.env, audit_source_root=root)
        try:
            winner = route_placement(root, output, authenticated["nextpnr-mistral"].path, invocation, gpu_device,
                                     single_seed=single_seed)
        except SearchError as exc:
            raise board.BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, root)
        evidence["route"]["placer_seed"] = winner.seed
        evidence["route"]["placer_heap_timingweight"] = winner.weight
        evidence.update({"build_id": build_identity(record), "device": board.TARGET,
                         "inputs": {p: board._sha256(root / p) for p in sorted(PINNED_INPUTS)},
                         "tools": identities, "top": board.TOP, "execution": invocation.inputs})
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
        if create_build_record(root, repository, revision, identities, identity_version=identity_version,
                               execution=invocation.inputs, single_seed=single_seed) != record:
            raise board.BuildError("functional source inputs changed during build")
        return export_package(encoded, output / "core.rbf", package_store)
    except Exception:
        if output is not None:
            board._invalidate_failed_artifact(output)
        raise
    finally:
        invocation.close()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(2,), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    parser.add_argument("--single-seed", type=int,
                        help="route only this placement seed; a timing miss fails without fallback")
    args = parser.parse_args()
    try:
        print(build(args.root, args.package_output, cache_root=args.cache_root,
                    identity_version=args.identity_version, gpu_device=args.gpu_device,
                    single_seed=args.single_seed))
    except (board.BuildError, ValueError) as exc:
        print(f"FES pc386 diagnostic: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
