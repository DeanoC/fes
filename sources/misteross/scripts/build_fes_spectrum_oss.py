#!/usr/bin/env python3
"""Build and seal an OSS HIP nextpnr/Mistral FES ZX Spectrum model package.

One shell builds four packages, chosen by --model: the 48K (fes.spectrum),
128K/+2 (fes.spectrum128), +2A/+3 (fes.spectrum-plus3) and Pentagon 128
(fes.pentagon). Each is format 3: blank 1024x10 firmware lanes (16, 32, 48 or 64)
are described by a sealed ROM map for the model's firmware image that
FogCast links at download time. The shell reserves the edge socket's named
rectangle of `fes.spectrum-bus.sockets/2` and pins its boundary flip-flops and
their verified route-throughs; no other shell cell may sit in the socket and no
firmware destination may fall in its CRAM rectangle.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from pathlib import Path
from dataclasses import dataclass
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import spectrum_slots, rom_map, coleco_expansion
from scripts.compiler_read_audit import guard_functional_source
from scripts import fes_de10nano_evidence
from scripts.fes_de10nano_evidence import PLANE_DSP, plane_dsp_evidence as _plane_dsp_evidence
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import (
    build_identity, encode_build_record, export_package, functional_record_fields,
)
from scripts.fes_build_common import (
    cuda_toolchain_configuration,
    BuildError, _authenticate_tools, _cell_counts, _i2c_evidence, _prepare_output,
    _read_json, _regular_input, _require_gpu_backend, _run_tool, _sha256, _write_atomic,
    reject_async_m10k_reads, validate_timing_resources,
)
from scripts.fes_build_common import _require_clean_source as require_clean_source
from scripts.functional_execution import FunctionalInvocation, source_roots_for_producer
from scripts.search_placer_qor import SearchError, route_after_synth
from scripts import pnr_recipe

ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
RECIPE = "scripts/build_fes_spectrum_oss.py"
SPECTRUM_TOOLCHAIN_LOCK = "cores/fes-spectrum/toolchain.lock"
SPECTRUM_TOOLCHAIN_ROOT = "build/toolchain/fes-spectrum"
SPECTRUM_GPU_BACKEND = "hip"
SPECTRUM_GPU_ROUTER = "HIP"
SPECTRUM_GPU_ARCHITECTURES = "gfx1100;gfx1201"
SPECTRUM_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={SPECTRUM_GPU_ROUTER}; hip-architectures={SPECTRUM_GPU_ARCHITECTURES}"
)
ROM_DATABASE_SHA256 = {
    "data/m10k-mux.txt": "22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe",
    "libmistral/cvd-sx120f.cc": "e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7",
    "libmistral/cyclonev.h": "185c24e2b75385d8e62a7144f12488eb68f7d8159af0a8a603cc4a3de1749ee9",
}
# Firmware lanes in ROM-page order, as placed in spectrum_rom.v.
FIRMWARE_LANES = (tuple((5, row) for row in range(32, 56)) + tuple((5, row) for row in range(73, 81)) +
                  tuple((14, row) for row in range(32, 64)))


@dataclass(frozen=True)
class Model:
    key: str
    core_id: str
    name: str
    description: str
    version: str
    define: str | None
    rom_id: str
    rom_lanes: int
    cpu_clock_hz: int
    frame_tstates: int
    ram_banks: int
    output: Path

    @property
    def lanes(self) -> tuple[tuple[int, int], ...]:
        return FIRMWARE_LANES[:self.rom_lanes]

    @property
    def rom_bytes(self) -> int:
        return 1024 * self.rom_lanes


MODELS = {model.key: model for model in (
    Model("48", "fes.spectrum", "FES ZX Spectrum 48K",
          "ZX Spectrum 48K: NMOS Z80 with ULA contention, linked 16 KiB firmware",
          "0.6.0", None, "spectrum-firmware", 16, 3_500_000, 69_888, 3,
          Path("build/fes-spectrum-oss")),
    Model("128", "fes.spectrum128", "FES ZX Spectrum 128K",
          "ZX Spectrum 128K/+2: NMOS Z80, ULA contention, AY-3-8912, 128 KiB paged RAM, "
          "linked 32 KiB firmware", "0.4.0", "SPECTRUM_MODEL_128K", "spectrum128-firmware", 32,
          3_546_900, 70_908, 8, Path("build/fes-spectrum128-oss")),
    Model("plus3", "fes.spectrum-plus3", "FES ZX Spectrum +2A/+3",
          "ZX Spectrum +2A/+3: gate-array contention, all-RAM paging, AY-3-8912, "
          "uPD765A +3 disk drive, linked 64 KiB firmware", "0.5.0", "SPECTRUM_MODEL_PLUS3",
          "spectrum-plus3-firmware", 64, 3_546_900, 70_908, 8, Path("build/fes-spectrum-plus3-oss")),
    Model("pentagon", "fes.pentagon", "FES Pentagon 128",
          "Pentagon 128: uncontended 71,680 T-state frame, AY-3-8912, 128 KiB paged RAM, "
          "Beta 128 TR-DOS drive, linked 48 KiB firmware", "0.5.0", "SPECTRUM_MODEL_PENTAGON",
          "pentagon-firmware", 48,
          3_500_000, 71_680, 8, Path("build/fes-pentagon-oss")),
)}
# Exact synthesized block counts: (MISTRAL_M10K, MISTRAL_M10K_TDP). M10K is the
# firmware lanes, the three 320x240 frame banks, the display-plane FIFO, the
# tape store's prefetch ring and the deck's pulse tables; TDP is the CPU/ULA
# RAM. The tape image and the Pentagon's and +2A/+3's disk images are in HPS DDR.
# The models with a unit-1 disk: their controller sub-interface and media.
DISK_MODELS = {"pentagon": ("fes.media.spectrum-disk-trdos", ".trd/.scl TR-DOS disk"),
               "plus3": ("fes.media.spectrum-disk-plus3", ".dsk +3 disk")}
EXPECTED_M10K = {"48": (113, 16), "128": (193, 32), "plus3": (225, 32), "pentagon": (209, 32)}
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
DDR_ATOM = "cores/fes-common/rtl/fes_hps_ddr_atom.v"
QSF = "cores/fes-spectrum/constraints-oss.qsf"
SDC = "cores/fes-spectrum/clocks-oss.sdc"
RTL_INCLUDES = (
    "cores/fes-spectrum/rtl/spectrum_bus.vh",
    "cores/fes-spectrum/rtl/spectrum_model.vh",
    "cores/fes-common/generated/fes_application.vh",
    "cores/fes-common/generated/fes_display_plane.vh",
)
RTL_SOURCES = (
    "cores/fes-spectrum/rtl/top.v",
    "cores/fes-spectrum/rtl/spectrum_system_pll.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-common/rtl/fes_display_cdc.v",
    "cores/fes-common/rtl/fes_display_plane.v",
    "cores/fes-menu/rtl/fes_menu_control.v",
    "cores/fes-menu/rtl/fes_menu_video.v",
    "cores/fes-menu/rtl/fes_menu_reader.v",
    "cores/fes-common/rtl/fes_hps_ddr.v",
    "cores/fes-common/rtl/fes_hps_ddr_guard.v",
    "cores/fes-common/rtl/fes_media_ddr.v",
    "cores/fes-spectrum/rtl/spectrum_machine.sv",
    "cores/fes-spectrum/rtl/spectrum_ula.sv",
    "cores/fes-spectrum/rtl/spectrum_contention.sv",
    "cores/fes-spectrum/rtl/spectrum_ram.v",
    "cores/fes-spectrum/rtl/spectrum_rom.v",
    "cores/fes-spectrum/rtl/spectrum_video.v",
    "cores/fes-spectrum/rtl/spectrum_tape.v",
    "cores/fes-spectrum/rtl/spectrum_snapshot.v",
    "cores/fes-spectrum/rtl/spectrum_beta.sv",
    "cores/fes-spectrum/rtl/spectrum_fdd.sv",
    "cores/fes-spectrum/rtl/spectrum_upd765.sv",
    "cores/fes-spectrum/rtl/spectrum_keyboard.v",
    "cores/fes-spectrum/rtl/spectrum_audio.v",
    "cores/fes-spectrum/rtl/spectrum_slot_sockets.v",
    "cores/fes-zx81/expansions/zonx_ay.v",
    "cores/fes-common/rtl/z80/fes_z80_alu.sv",
    "cores/fes-common/rtl/z80/fes_z80_engine.sv",
    "cores/fes-common/rtl/z80/fes_z80_bus.sv",
    "cores/fes-common/rtl/z80/fes_z80_nmos.sv",
)
PINNED_INPUTS = (
    RECIPE, "scripts/spectrum_slots.py", "scripts/coleco_expansion.py", "scripts/compiler_read_audit.py",
    "scripts/source_repository.py", "scripts/fes_build_common.py", "scripts/rom_map.py",
    "scripts/cyclonev_rbf.py", "scripts/search_placer_qor.py", "scripts/fes_de10nano_evidence.py",
    DDR_ATOM,
    ABI_DEFINITION, SPECTRUM_TOOLCHAIN_LOCK, "scripts/pnr_recipe.py", QSF, SDC, *RTL_INCLUDES, *RTL_SOURCES,
)
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log", "nextpnr.log",
    "build-inputs.json", "build-summary.json", "manifest.toml", "rom-map.json",
    "socket.qsf", "qor-ranking.json",
)
DSP_RESOURCES = frozenset({PLANE_DSP})
ORDINARY_RESOURCES = frozenset({
    "MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO",
    "MISTRAL_M10K", "MISTRAL_M10K_TDP",
}) | DSP_RESOURCES
REQUIRED_RESOURCES = {
    "altera_pll": 2,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
    "cyclonev_hps_interface_fpga2sdram": 1,
}
FORBIDDEN_RESOURCES = frozenset({
    "MISTRAL_MLAB", "MISTRAL_MUL18X18", "MISTRAL_MUL18X19",
    "MISTRAL_MUL18X19_COMBINED", "MISTRAL_MUL27X27",
})
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
BEL_RE = re.compile(r"[A-Z0-9_]+\.(\d+)\.(\d+)\.")
LANE_RE = re.compile(r"machine\.rom\.lane(\d+)")


def model_for(key: str) -> Model:
    try:
        return MODELS[key]
    except KeyError:
        raise BuildError(f"Spectrum model must be one of {', '.join(MODELS)}") from None


def pnr_for(model: Model) -> dict:
    """The model's FES recipe row: seed order, weights, clocks and timeout."""
    return pnr_recipe.recipe_for(model.core_id)


def gpu_choice(gpu_router: str) -> tuple[str, str, str]:
    if gpu_router == "HIP":
        return SPECTRUM_GPU_BACKEND, SPECTRUM_GPU_ARCHITECTURES, SPECTRUM_TOOLCHAIN_CONFIGURATION
    if gpu_router == "CUDA":
        return "cuda", "unused", cuda_toolchain_configuration()
    raise BuildError(f"unsupported Spectrum GPU router {gpu_router}")


def _authenticate_spectrum_tools(root: Path, cache_root: Path | None = None,
                                 toolchain_root: Path | None = None,
                                 gpu_router: str = SPECTRUM_GPU_ROUTER,
                                 core_id: str | None = None):
    _backend, architectures, configuration = gpu_choice(gpu_router)
    root_path = root / SPECTRUM_TOOLCHAIN_ROOT if toolchain_root is None else Path(toolchain_root)
    authenticated = _authenticate_tools(
        root,
        lock_path=root / SPECTRUM_TOOLCHAIN_LOCK,
        toolchain_root=root_path,
        expected_configuration={"nextpnr": configuration},
        gpu_router=gpu_router,
        hip_architectures=architectures,
        cache_root=cache_root,
    )
    if core_id is not None:
        try:
            pnr_recipe.validate_nextpnr_tool(core_id, authenticated["nextpnr-mistral"].path)
        except pnr_recipe.RecipeError as exc:
            raise BuildError(f"nextpnr recipe option validation failed: {exc}") from exc
    return authenticated


def _require_clean_source(root: Path, *, identity_version: int = 2, model: str = "48") -> tuple[str, str]:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    model_for(model)
    return require_clean_source(root, pinned_inputs=PINNED_INPUTS, identity_version=identity_version)


@guard_functional_source
def create_build_record(
    root: Path,
    repository: str,
    revision: str,
    tool_identities: Mapping[str, str],
    *,
    identity_version: int = 2,
    execution: dict | None = None,
    model: str = "48",
    max_aluts: int | None = None,
    gpu_backend: str = SPECTRUM_GPU_BACKEND,
    gpu_architectures: str = SPECTRUM_GPU_ARCHITECTURES,
) -> bytes:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    machine = model_for(model)
    pnr = pnr_for(machine)
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
            "core_id": machine.core_id,
            "model": machine.key,
            "model_define": machine.define or "none",
            "device": TARGET,
            "top": TOP,
            "router": pnr["router"],
            "gpu_backend": gpu_backend,
            "gpu_architectures": gpu_architectures,
            "sys_clock_hz": 52_224_000,
            "cpu_implementation": "fes_z80_nmos",
            "cpu_clock_hz": machine.cpu_clock_hz,
            "frame_tstates": machine.frame_tstates,
            "ram_banks": machine.ram_banks,
            "pll_count": 2,
            "output_relative": machine.output.as_posix(),
            "pixel_clock_hz": 74_250_000,
            "audio_clock_hz": 12_288_000,
            "audio_serializer": "fes_audio_output",
            "audio_sample_hz": 48_000,
            "reference_clock_hz": 50_000_000,
            "seed": pnr["seeds"][0],
            "seed_order": ",".join(str(seed) for seed in pnr["seeds"]),
            "placer_heap_timingweight": pnr["timing_weights"][0],
            "placer_heap_critexp": pnr["critexp"],
            "toolchain_lock": SPECTRUM_TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(_regular_input(root, SPECTRUM_TOOLCHAIN_LOCK)),
            "package_format": 3,
            "rom_id": machine.rom_id,
            "rom_role": "firmware",
            "rom_source_size": machine.rom_bytes,
            "rom_encoding": "m10k-1024x10-v1",
            "rom_read_mode": "registered",
            "rom_read_latency_system_ticks": 2,
            "rom_database_sha256": json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(",", ":")),
            "expansion_layout": spectrum_slots.LAYOUT,
            "expansion_sockets": ",".join(s.placement for s in spectrum_slots.SOCKETS),
            "video": "session-display-plane-3x-320x240",
        },
    }
    if max_aluts is not None:
        _area_budget({}, max_aluts)
        fields["parameters"]["max_aluts"] = max_aluts
    fields = functional_record_fields(root, fields, source_roots_for_producer(__name__, PINNED_INPUTS, root),
                                      execution, pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)


def socket_qsf(base: str) -> str:
    if "FES_RESERVED_RECT" in base:
        raise BuildError("base QSF already reserves a rectangle")
    lines = [base.rstrip()]
    for socket in spectrum_slots.SOCKETS:
        lines.append(f'set_global_assignment -name FES_RESERVED_RECT "{socket.placement}"')
    return "\n".join(lines) + "\n"


def build_commands(root: Path, output: Path, build_id: str,
                   tools: Mapping[str, Path], seed: int | None = None, *,
                   model: str = "48") -> tuple[tuple[str, ...], tuple[str, ...]]:
    machine = model_for(model)
    pnr = pnr_for(machine)
    seed = pnr["seeds"][0] if seed is None else seed
    relative = machine.output.as_posix()
    if output != root / machine.output:
        raise BuildError(f"{machine.name} OSS output must be {root / machine.output}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    define = f"-D{machine.define} " if machine.define else ""
    program = (
        f"read_verilog -lib {DDR_ATOM}; "
        f"read_verilog -sv {define}-I cores/fes-spectrum/rtl -I cores/fes-common/generated "
        f"{' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} {TOP}; "
        f"synth_intel_alm -nolutram -top {TOP}; stat; "
        f"write_json {relative}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = (
        str(tools["nextpnr-mistral"]), "--json", f"{relative}/synth.json",
        "--device", TARGET, "--qsf", f"{relative}/socket.qsf",
        "--sdc", SDC, "--freq", "74.25", "--seed", str(seed),
        "--placer-heap-timingweight", str(pnr["timing_weights"][0]),
        "--placer-heap-critexp", str(pnr["critexp"]),
        "--router", pnr["router"], "--timing-allow-fail",
        "--rbf", f"{relative}/core.rbf", "--compress-rbf",
        "--write", f"{relative}/routed.json",
        "--report", f"{relative}/timing.json", "--detailed-timing-report",
    )
    return yosys, route


def validate_routed_shell(
    routed: dict, *, nextpnr_commit: str = pnr_recipe.nextpnr_pin("fes.spectrum")
) -> dict:
    """Every socket holds only pinned boundary FFs and verified paired buffers."""
    top = routed.get("modules", {}).get(TOP, {})
    cells = top.get("cells", {})
    expected = {}
    for socket in spectrum_slots.SOCKETS:
        for name, bel in spectrum_slots.boundary_bels(socket).items():
            expected[socket.instance + name] = bel
    for name, bel in expected.items():
        cell = cells.get(name)
        if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_FF" or \
                cell.get("attributes", {}).get("NEXTPNR_BEL") != bel:
            raise BuildError(f"slot boundary cell {name} is not at {bel}")
    try:
        route_through = coleco_expansion.boundary_route_through_cells(
            top, expected, nextpnr_commit=nextpnr_commit)
        coleco_expansion.validate_clock_anchors(top, {
            name: bel for name, bel in expected.items() if ".clock_coverage_ff_" in name},
            nextpnr_commit=nextpnr_commit)
    except ValueError as exc:
        raise BuildError(str(exc)) from exc
    drivers = {}
    for cell in cells.values():
        for port, bits in cell.get("connections", {}).items():
            if cell.get("port_directions", {}).get(port) == "output":
                for bit in bits:
                    if type(bit) is int:
                        drivers[bit] = drivers.get(bit, 0) + 1
    for port in top.get("ports", {}).values():
        if port.get("direction") == "input":
            for bit in port.get("bits", []):
                if type(bit) is int:
                    drivers[bit] = drivers.get(bit, 0) + 1
    for name in expected:
        companion = name + "$ROUTETHRU"
        data = (cells[companion]["connections"]["A"] if companion in route_through else
                cells[name].get("connections", {}).get("DATAIN"))
        # Older snapshots can disconnect an unused clock-only data input.
        if data == [] and ".clock_coverage_ff_" in name:
            continue
        if not isinstance(data, list) or len(data) != 1 or not (
                (type(data[0]) is int and drivers.get(data[0]) == 1) or
                (type(data[0]) is str and data[0] in ("0", "1"))):
            raise BuildError(f"slot boundary data input has no unique driver: {name}")
    allowed = set(expected) | route_through
    for name, cell in cells.items():
        bel = cell.get("attributes", {}).get("NEXTPNR_BEL", "") if isinstance(cell, dict) else ""
        match = BEL_RE.match(bel)
        if not match or name in allowed:
            continue
        x, y = int(match.group(1)), int(match.group(2))
        for socket in spectrum_slots.SOCKETS:
            if spectrum_slots.COLUMN <= x <= spectrum_slots.COLUMN + 4 and socket.first_row <= y <= socket.last_row:
                raise BuildError(f"shell cell {name} is inside the slot {socket.slot} socket")
    return {"layout": spectrum_slots.LAYOUT, "sockets": [s.slot for s in spectrum_slots.SOCKETS],
            "pinned_boundary_cells": len(expected), "boundary_route_through_cells": len(route_through)}


def _frequency_row(fmax: object, expected: float, label: str) -> tuple[str, float, float]:
    if not isinstance(fmax, dict):
        raise BuildError("timing report has no structured fmax data")
    matches = []
    for name, fields in fmax.items():
        if not isinstance(name, str) or not isinstance(fields, dict):
            continue
        constraint, achieved = fields.get("constraint"), fields.get("achieved")
        if not all(isinstance(v, (int, float)) and not isinstance(v, bool) for v in (constraint, achieved)):
            continue
        if not math.isfinite(float(constraint)) or not math.isfinite(float(achieved)):
            raise BuildError(f"{label} timing frequencies must be finite")
        if abs(float(constraint) - expected) <= max(1e-6, expected * 5e-5):
            matches.append((name, float(constraint), float(achieved)))
    if len(matches) != 1:
        raise BuildError(f"timing report must contain exactly one {label} {expected:g} MHz constraint")
    name, constraint, achieved = matches[0]
    if achieved < constraint:
        raise BuildError(f"{label} timing achieved {achieved:g} MHz, below {constraint:g} MHz")
    return name, constraint, achieved


def validate_firmware_ports(cells: dict, lanes: int) -> None:
    """Require a shared live read clock, enabled reads and disabled ROM writes."""
    clocks = set()
    for lane in range(lanes):
        name = f"machine.rom.lane{lane}"
        pins = cells.get(name, {}).get("connections", {})
        clock = pins.get("CLK1")
        if (not isinstance(clock, list) or len(clock) != 1 or type(clock[0]) is not int or
                pins.get("A1EN") != ["1"] or pins.get("B1EN") != ["1"] or
                pins.get("ACLR0") != ["0"] or pins.get("ACLR1") != ["0"] or
                pins.get("A1BE") or pins.get("CLK2")):
            raise BuildError(f"firmware lane {name} must have one live clock, enabled reads, "
                             "disabled writes, inactive clears and no optional ports")
        clocks.add(clock[0])
    if len(clocks) != 1:
        raise BuildError("firmware lanes must share the system read clock")
    extra = [name for name in cells
             if (match := LANE_RE.fullmatch(name)) and int(match.group(1)) >= lanes]
    if extra:
        raise BuildError(f"unexpected firmware lanes {sorted(extra)}")


def _hps_ddr_evidence(design: dict, label: str, source_root: Path, disk: bool) -> dict:
    """The plane reads launcher frames on fpga2sdram port 0 and the tape store
    reads and writes port 1; port 0 never writes. The Pentagon's and the
    +2A/+3's disk store reads and writes port 2; elsewhere it is unused."""
    normalized = fes_de10nano_evidence.layout_graph_with_constants(design)
    evidence = fes_de10nano_evidence.hps_ddr_layout_evidence(normalized, label, source_root, idle=False)
    cells = normalized["modules"][TOP].get("cells", {})
    memory = next(cell for cell in cells.values()
                  if cell.get("type") == "cyclonev_hps_interface_fpga2sdram")
    ports = memory.get("connections", {})
    # Command 1 and write FIFO 2 are port 1's; FIFOs 0 and 1 are port 0's;
    # command 2 and write FIFO 3 are port 2's.
    unused = tuple(f"cmd_valid_{port}" for port in range(3 if disk else 2, 6)) + \
        ("wr_valid_0", "wr_valid_1") + (() if disk else ("wr_valid_3",))
    grounds = [["0"]] + [cell.get("connections", {}).get("Q") for cell in cells.values()
        if cell.get("type") == "MISTRAL_CONST"
        and set(str(cell.get("parameters", {}).get("LUT", ""))) == {"0"}
        and isinstance(cell.get("connections", {}).get("Q"), list)
        and len(cell["connections"]["Q"]) == 1]
    if any(ports.get(port) not in grounds for port in unused) or ports.get("cmd_data_0", [])[1:2] not in grounds:
        raise BuildError("session display DDR writes and unused ports must be tied low")
    if any(ports.get(port) in grounds for port in ("cmd_valid_1", "wr_valid_2")):
        raise BuildError("the tape store must drive fpga2sdram port 1")
    if disk and any(ports.get(port) in grounds for port in ("cmd_valid_2", "wr_valid_3")):
        raise BuildError("the disk store must drive fpga2sdram port 2")
    for name, cell in cells.items():
        if (name.startswith("display.scanout.reader.") and cell.get("type") == "MISTRAL_M10K"
                and int(str(cell.get("parameters", {}).get("CFG_ASYNC_READ", "0")), 2)):
            raise BuildError("session display FIFO requires synchronous M10K reads")
    return {**evidence, "plane_port": 0, "tape_port": 1, **({"disk_port": 2} if disk else {}),
            "unused_ports_disabled": True}
def validate_synth_evidence(output: Path, *, model: str = "48", source_root: Path = ROOT) -> dict:
    machine = model_for(model)
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    reject_async_m10k_reads(synthesis)
    _i2c_evidence(synthesis, "synthesized")
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    m10k, tdp = EXPECTED_M10K[machine.key]
    if counts.get("MISTRAL_M10K", 0) != m10k or counts.get("MISTRAL_M10K_TDP", 0) != tdp:
        raise BuildError(
            f"synthesis must keep {m10k} M10K (firmware lanes, frame banks, plane FIFO, CPU-only "
            f"RAM banks, tape ring and pulse tables) and {tdp} screen-bank TDP blocks, "
            f"got M10K={counts.get('MISTRAL_M10K', 0)} "
            f"TDP={counts.get('MISTRAL_M10K_TDP', 0)}")
    validate_firmware_ports(synthesis["modules"][TOP]["cells"], machine.rom_lanes)
    _plane_dsp_evidence(synthesis)
    display = _hps_ddr_evidence(synthesis, "synthesized", source_root, machine.key in DISK_MODELS)
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0):
            raise BuildError(f"forbidden synthesis cell {name} is in use")
    return {"status": "pass", "synthesis_cells": {name: counts[name] for name in sorted(counts)},
            "session_display": display}


def validate_build_evidence(output: Path, *, model: str = "48",
                            gpu_backend: str = SPECTRUM_GPU_BACKEND, source_root: Path = ROOT) -> dict:
    routed = _read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    synth = validate_synth_evidence(output, model=model, source_root=source_root)
    reject_async_m10k_reads(routed)
    _i2c_evidence(routed, "routed")
    display = _hps_ddr_evidence(routed, "routed", source_root, model_for(model).key in DISK_MODELS)
    sockets = validate_routed_shell(routed)
    route_text = (output / "nextpnr.log").read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    gpu_backend = _require_gpu_backend(route_text, gpu_backend)
    direct = "50 MHz -> 52.224 MHz" in route_text
    fractional = bool(re.search(
        r"Info: PLL 'system_clock\.pll': 50\.000000 MHz -> VCO [0-9.]+ MHz, fractional-N,",
        route_text) and re.search(
        r"Info: PLL 'system_clock\.pll': fractional-N requested 52224000\.000000 Hz \(output 0\)",
        route_text))
    if not (direct or fractional):
        raise BuildError("route log does not contain the 50-to-52.224 MHz system PLL")
    audio_pll = re.search(r"PLL 'system_clock\.pll': second output 12\.288 MHz", route_text) or re.search(
        r"Info: PLL 'system_clock\.pll': fractional-N requested 12288000\.000000 Hz \(output 1\)", route_text)
    if not audio_pll:
        raise BuildError("route log must prove the selected 12.288 MHz audio PLL")
    timing = _read_json(output / "timing.json", "timing report")
    rows = {label: _frequency_row(timing.get("fmax"), mhz, label)
            for label, mhz in (("system", 52.224), ("pixel", 74.25), ("audio", 12.288))}
    known = ORDINARY_RESOURCES | set(REQUIRED_RESOURCES) | FORBIDDEN_RESOURCES | REQUIRED_ZERO_RESOURCES
    resources = validate_timing_resources(timing.get("utilization"), known)
    rbf = output / "core.rbf"
    if rbf.is_symlink() or not rbf.is_file() or not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise BuildError(f"RBF must be a nonempty bounded regular file: {rbf}")
    return {
        "status": "pass",
        "route": {"status": "pass", "unrouted": False, "gpu_backend": gpu_backend},
        "timing": {label: {"clock": row[0], "constraint_mhz": row[1], "achieved_mhz": row[2],
                           "status": "pass"} for label, row in rows.items()} | {"status": "pass"},
        "resources": resources,
        "audio": {"sample_average_hz": 48_000, "mclk_average_hz": 12_288_000, "mclk_source": "pll"},
        "session_display": display,
        "sockets": sockets,
        "synthesis_cells": synth["synthesis_cells"],
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }


def _area_budget(counts: Mapping[str, int], maximum: int | None, *, require_comb: bool = False) -> dict:
    if maximum is not None and (type(maximum) is not int or maximum <= 0):
        raise BuildError("max ALUTs must be a positive integer")
    if require_comb and maximum is not None:
        comb = counts.get("MISTRAL_COMB")
        if type(comb) is not int or comb < 0:
            raise BuildError("routed utilization missing valid MISTRAL_COMB evidence")
    used = sum(count for name, count in counts.items() if name.startswith("MISTRAL_ALUT") or name == "MISTRAL_COMB")
    evidence = {"used": used, "maximum": maximum, "status": "pass"}
    if maximum is not None and used > maximum:
        raise BuildError(f"area budget exceeded: {used} ALUTs > {maximum}")
    return evidence


def check_firmware_outside_sockets(mapping: dict) -> None:
    width = 7605
    for block in mapping["blocks"]:
        for word in block["word_bits"]:
            for bit in word:
                x, y = bit % width, bit // width
                for socket in spectrum_slots.SOCKETS:
                    x0, y0, x1, y1 = socket.cram
                    if x0 <= x < x1 and y0 <= y < y1:
                        raise BuildError(f"firmware lane {block['bel']} writes CRAM in slot {socket.slot}")


def _manifest(record: bytes, evidence: dict, repository: str, revision: str,
              tools: Mapping[str, str]) -> bytes:
    rbf = evidence["rbf"]
    record_fields = json.loads(record)
    machine = model_for(record_fields["parameters"]["model"])
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    required = [
        ("fes.video.fixed-720p60", 1), ("fes.keyboard.hid", 1), ("fes.gamepad.ports", 1),
        ("fes.audio.pcm-s16-stereo-48k", 1), ("fes.media.spectrum-tape", 2),
        ("fes.memory.hps-ddr", 1), ("fes.video.session-display", 1), ("fes.fabric.video.display-plane", 1),
    ]
    # The cassette unit also runs snapshot restore streams: 48K ones on every
    # model, 128K ones on the paged models and +2A/+3 ones on the +2A/+3.
    snapshots = ["fes.media.spectrum-snapshot"]
    if machine.ram_banks == 8:
        snapshots.append("fes.media.spectrum-snapshot-128")
    if machine.key == "plus3":
        snapshots.append("fes.media.spectrum-snapshot-plus3")
    # The Pentagon's Beta 128 reads the unit-1 raw track disk under TR-DOS,
    # and the +2A/+3's uPD765A reads it under +3DOS.
    media = ".tzx/.tap/.pzx cassette with turbo loading, .z80/.sna snapshots"
    if machine.key in DISK_MODELS:
        required.append(("fes.media.spectrum-disk", 1))
        snapshots.append(DISK_MODELS[machine.key][0])
        media += ", " + DISK_MODELS[machine.key][1]
    fields = {
        "format": 3,
        "core": {
            "id": machine.core_id,
            "name": machine.name,
            "description": machine.description + ", " + media + " and an edge socket",
            "version": machine.version,
        },
        "target": {"platform": "de10_nano", "device": TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{"id": interface, "major": major, "minor": 0, "required": True}
                       for interface, major in required] +
                      [{"id": spectrum_slots.INTERFACE, "major": 1, "minor": 0, "required": False}] +
                      [{"id": interface, "major": 1, "minor": 0, "required": False} for interface in snapshots],
        "build": {
            "id": evidence["build_id"],
            "repository": repository,
            "revision": revision,
            "recipe_sha256": record_fields["recipe_sha256"],
            "toolchain": toolchain,
        },
        "rom": evidence["rom"],
    }
    return encode_manifest(fields)


@guard_functional_source
def build(root: Path = ROOT, package_store: Path | None = None, *, cache_root: Path | None = None,
          identity_version: int = 2, gpu_device: int = 0, model: str = "48",
          max_aluts: int | None = None, gpu_router: str = SPECTRUM_GPU_ROUTER,
          toolchain_root: Path | None = None) -> Path:
    _area_budget({}, max_aluts)
    machine = model_for(model)
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"{machine.name} package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root, identity_version=identity_version, model=model)
    backend, architectures, _configuration = gpu_choice(gpu_router)
    authenticated = _authenticate_spectrum_tools(
        root, cache_root=cache_root, toolchain_root=toolchain_root, gpu_router=gpu_router,
        core_id=machine.core_id)
    pnr = pnr_for(machine)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    database_root = authenticated["mistral"].path.parents[2] / "src/mistral"
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    extra_env = {"CUDA_DEVICE_ORDER": "PCI_BUS_ID"} if gpu_router == "CUDA" else None
    invocation = FunctionalInvocation(authenticated, gpu_device, extra_env=extra_env)
    record = create_build_record(root, repository, revision, identities,
                                 identity_version=identity_version, execution=invocation.inputs, model=model,
                                 max_aluts=max_aluts, gpu_backend=backend, gpu_architectures=architectures)
    output = _prepare_output(root, relative=machine.output, build_outputs=BUILD_OUTPUTS)
    _write_atomic(output / "build-inputs.json", record)
    _write_atomic(output / "socket.qsf", socket_qsf((root / QSF).read_text()).encode())
    try:
        build_id = build_identity(record)
        yosys, _route = build_commands(root, output, build_id, {
            name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, model=model)
        _run_tool(yosys, root, output / "yosys.log", env=invocation.env,
                  audit_source_root=root, output_relative=machine.output)
        if not (output / "synth.json").is_file():
            raise BuildError("Yosys did not produce synthesis evidence")
        synth_evidence = validate_synth_evidence(output, model=model, source_root=root)
        area = _area_budget(synth_evidence["synthesis_cells"], max_aluts)
        try:
            winner = route_after_synth(
                nextpnr=authenticated["nextpnr-mistral"].path,
                fixture=output / "synth.json", dest=output, device=TARGET,
                qsf=output / "socket.qsf", sdc=root / SDC, freq="74.25",
                seeds=tuple(pnr["seeds"]), weights=(pnr["timing_weights"][0],),
                critexp=pnr["critexp"], budget=len(pnr["seeds"]),
                mode="first-pass", extra=(), timeout=pnr["timeout_s"],
                required=pnr_recipe.required_clocks(machine.core_id),
                gpu_devices=(gpu_device,),
                env=invocation.env, audit_source_root=root, core_id=machine.core_id,
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, model=model, gpu_backend=backend, source_root=root)
        evidence["area_budget"] = {"synthesis": area, "routed": _area_budget(
            {name: values["used"] for name, values in evidence["resources"].items()}, max_aluts,
            require_comb=True)}
        evidence["route"].update(placer_seed=winner.seed, placer_heap_timingweight=winner.weight,
                                 placer_qor_mode="first-pass")
        mapping, map_evidence = rom_map.build_rom_map(
            database, (output / "core.rbf").read_bytes(),
            routed=_read_json(output / "routed.json", "routed firmware design"),
            lane_rows=machine.lanes, expected_async_read=0,
        )
        check_firmware_outside_sockets(mapping)
        map_bytes = (json.dumps(mapping, sort_keys=True, separators=(",", ":")) + "\n").encode()
        _write_atomic(output / "rom-map.json", map_bytes)
        evidence["rom"] = {"id": machine.rom_id, "role": "firmware", "source_size": machine.rom_bytes,
                           "file": "rom-map.json", "size": len(map_bytes),
                           "sha256": _sha256(output / "rom-map.json")}
        evidence["rom_map"] = map_evidence
        evidence["execution"] = invocation.inputs
        evidence.update({
            "build_id": build_id, "model": machine.key, "core_id": machine.core_id, "device": TARGET,
            "top": TOP, "tools": identities,
            "inputs": {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)},
        })
        _write_atomic(output / "build-summary.json",
                      (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode())
        manifest = _manifest(record, evidence, repository, revision, identities)
        _write_atomic(output / "manifest.toml", manifest)
        final_tools = _authenticate_spectrum_tools(
            root, cache_root=cache_root, toolchain_root=toolchain_root, gpu_router=gpu_router,
            core_id=machine.core_id)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError("authenticated tool identity changed during build")
        if _require_clean_source(root, identity_version=identity_version, model=model) != (repository, revision):
            raise BuildError("source identity changed during build")
        invocation.verify()
        if rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError("ROM database changed during build")
        if create_build_record(root, repository, revision, identities,
                               identity_version=identity_version, execution=invocation.inputs, model=model,
                               max_aluts=max_aluts, gpu_backend=backend, gpu_architectures=architectures) != record:
            raise BuildError("functional source inputs changed during build")
        return export_package(manifest, output / "core.rbf", package_store,
                              rom_map=output / "rom-map.json")
    except Exception:
        for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
            path = output / name
            if path.is_file() or path.is_symlink():
                path.unlink()
        raise
    finally:
        invocation.close()


def synth(root: Path = ROOT, *, cache_root: Path | None = None, model: str = "48",
          gpu_router: str = SPECTRUM_GPU_ROUTER,
          toolchain_root: Path | None = None) -> dict:
    """Run Yosys only. Does not require a clean tree and does not seal a package."""
    machine = model_for(model)
    root = Path(root).resolve()
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
    authenticated = _authenticate_spectrum_tools(
        root, cache_root=cache_root, toolchain_root=toolchain_root, gpu_router=gpu_router,
        core_id=machine.core_id)
    output = _prepare_output(root, relative=machine.output, build_outputs=BUILD_OUTPUTS)
    yosys, _route = build_commands(root, output, "0" * 32, {
        name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, model=model)
    _run_tool(yosys, root, output / "yosys.log", output_relative=machine.output)
    if not (output / "synth.json").is_file():
        raise BuildError("Yosys did not produce synthesis evidence")
    evidence = validate_synth_evidence(output, model=model, source_root=root)
    evidence.update({"build_id": "0" * 32, "sealed": False, "model": machine.key,
                     "tools": {name: tool.identity for name, tool in authenticated.items()}})
    _write_atomic(output / "build-summary.json",
                  (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode())
    return evidence


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(2,), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    parser.add_argument("--gpu-router", choices=("HIP", "CUDA"), default=SPECTRUM_GPU_ROUTER)
    parser.add_argument("--toolchain-root", type=Path)
    parser.add_argument("--model", choices=tuple(MODELS), default="48",
                        help="48K, 128K/+2, +2A/+3 or Pentagon 128")
    parser.add_argument("--max-aluts", type=int, help="production area cap; reject before routing and sealing")
    parser.add_argument("--synth-only", action="store_true",
                        help="run Yosys only; skip the clean-tree seal and nextpnr")
    arguments = parser.parse_args(argv)
    try:
        if arguments.synth_only:
            if arguments.max_aluts is not None:
                raise BuildError("--max-aluts requires the full production build")
            cells = synth(arguments.root, cache_root=arguments.cache_root, model=arguments.model,
                          gpu_router=arguments.gpu_router,
                          toolchain_root=arguments.toolchain_root)["synthesis_cells"]
            print(f"synth-only MISTRAL_M10K={cells.get('MISTRAL_M10K', 0)} "
                  f"MISTRAL_M10K_TDP={cells.get('MISTRAL_M10K_TDP', 0)} "
                  f"MISTRAL_FF={cells.get('MISTRAL_FF', 0)}")
            return 0
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root,
                    identity_version=arguments.identity_version, gpu_device=arguments.gpu_device,
                    model=arguments.model, max_aluts=arguments.max_aluts, gpu_router=arguments.gpu_router,
                    toolchain_root=arguments.toolchain_root))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-spectrum-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
