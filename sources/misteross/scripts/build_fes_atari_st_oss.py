#!/usr/bin/env python3
"""Build and seal the native HIP nextpnr/Mistral Atari 520ST package.

192 synchronous, blank 1024x10 M10K lanes describe the linked 192 KiB
firmware. The ST expansion connector has a reserved, registered boundary.
Synthesis diagnostics are unsealed and never export a package.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import re
import sys
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import atari_st_slot, atari_st_video_parts, rom_map
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import (
    build_identity, encode_build_record, export_package, functional_record_fields,
)
from scripts.fes_build_common import (
    BuildError, _authenticate_tools, _cell_counts, _i2c_evidence, _prepare_output,
    _read_json, _regular_input, _require_gpu_backend, _run_tool, _sha256, _write_atomic,
    reject_async_m10k_reads, validate_timing_resources,
)
from scripts.fes_build_common import _require_clean_source as require_clean_source
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
ROUTER = "gpu"
RECIPE = "scripts/build_fes_atari_st_oss.py"
OUTPUT_RELATIVE = Path("build/fes-atari-st-oss")
DIAGNOSTIC_OUTPUT = Path("build/fes-atari-st-oss-diagnostic")
ST_TOOLCHAIN_LOCK = "toolchains/atari-st.lock"
ST_TOOLCHAIN_ROOT = "build/toolchain/fes-atari-st"
ST_GPU_BACKEND = "hip"
ST_GPU_ROUTER = "HIP"
ST_GPU_ARCHITECTURES = "gfx1100;gfx1201"
ST_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={ST_GPU_ROUTER}; hip-architectures={ST_GPU_ARCHITECTURES}"
)
ST_TOOL_COMMITS = {
    "mistral": "7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039",
    "nextpnr": "3d4a5b352b4edb478b744b82cc61333353751a80",
    "yosys": "886afa63953e97407153e9f4aae25fcedb639696",
}
# First passing route wins; the order is part of the build identity. Seed 4
# completed the initial diagnostic route; seed 5 exceeded its old placement bound.
PLACER_SEEDS = (4, 5, 2, 1, 3, 6, 7, 8, 9, 10)
PLACER_WEIGHT = 2000
PLACER_CRITICALITY_EXPONENT = 5
# The initial 22.5k-cell route takes about nine minutes before timing repair.
# Bound each Atari attempt separately from smaller cores' shared search default.
PLACER_TIMEOUT_SECONDS = 1800
PLACER_QOR_CLOCKS = ((None, 52.224), (None, 74.25), (None, 12.288))
ROM_DATABASE_SHA256 = {
    "data/m10k-mux.txt": "22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe",
    "libmistral/cvd-sx120f.cc": "e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7",
    "libmistral/cyclonev.h": "48c0acadd2d1dc47398d7e7ab8ad840e98cb3fda489c3197eace6f3ba59e6f21",
}
FIRMWARE_LANE_ROWS = (tuple((5, row) for row in (*range(1, 15), *range(32, 56), *range(73, 81))) +
                      tuple((14, row) for row in range(1, 81)) +
                      tuple((38, row) for row in range(1, 67)))
FIRMWARE_ID = "atari-st-firmware"
FIRMWARE_BYTES = 196608
# RAM tile coordinates differ from LAB coordinates. M10K(26,19)'s INIT and
# control bits occupy the socket CRAM even though its BEL is beyond LAB row18.
CACHE_BELS = {"video.cache0.0.0.0": "MISTRAL_M10K.26.20.0",
              "video.cache1.0.0.0": "MISTRAL_M10K.26.21.0"}
RAM_GUARD_RESERVATION = "ram_guard 26 19 26 19"
VIDEO_RAM_GUARD_RESERVATIONS = ("video_ram_low 26 40 26 40", "video_ram_high 26 59 26 59")
RAM_CONFIG_POLICY = "m10k-bmux-configuration-bounds-v1"
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
QSF = "cores/fes-atari-st/constraints/constraints-oss.qsf"
SDC = "cores/fes-atari-st/constraints/clocks-oss.sdc"
CPU_VENDOR = "cores/fes-common/rtl/fx68k"
CPU_COMMIT = "0602ee4627b10f301298f2673d826cdd6baa9327"
CPU_ADAPTER_POLICY = "legacy-simulation-translate-directives-v1"
RTL_INCLUDES = ("cores/fes-common/generated/fes_video_part.vh",
                "cores/fes-common/generated/fes_atari_st_bus.vh")
RTL_SOURCES = (
    f"{CPU_VENDOR}/fx68k.sv", f"{CPU_VENDOR}/fx68kAlu.sv", f"{CPU_VENDOR}/uaddrPla.sv",
    "cores/fes-atari-st/rtl/fes_atari_st_top.v",
    "cores/fes-c64/rtl/c64_system_pll.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-common/rtl/fes_video_part_direct.v",
    "cores/fes-common/rtl/fes_video_part_scanlines.v",
    *(f"cores/fes-atari-st/rtl/{name}" for name in (
        "st_cpu.sv", "st_machine.sv", "st_system.sv", "st_io.sv", "st_memory.sv",
        "st_rom.v", "st_native_border.sv", "st_native_low_video.sv", "st_video.sv", "st_video_adapter.sv", "st_media_writer.sv", "st_mfp.sv",
        "st_floppy.sv", "st_floppy_writer.sv", "st_media_port.sv", "st_video_socket.sv", "st_acia.sv", "st_ikbd.sv", "st_ym2149.sv", "st_ym_mixer.sv", "st_ym_mix_rom.sv", "st_expansion_socket.sv")),
    "cores/fes-zx81/expansions/zonx_ay.v", "cores/fes-ramtest/rtl/sdram_addon_port.v",
)
PINNED_INPUTS = (
    RECIPE, "scripts/atari_st_slot.py", "scripts/atari_st_video_parts.py", "scripts/coleco_expansion.py", "scripts/compiler_read_audit.py",
    "scripts/source_repository.py", "scripts/fes_build_common.py", "scripts/rom_map.py",
    "scripts/cyclonev_rbf.py", "scripts/search_placer_qor.py",
    "scripts/generate_st_ym_mix_rom.py",
    "cores/fes-atari-st/data/ym2149_fixed_vol.h", "cores/fes-atari-st/data/source.json",
    "cores/fes-atari-st/data/GPL-2.0.txt",
    ABI_DEFINITION, ST_TOOLCHAIN_LOCK, QSF, SDC, *RTL_INCLUDES, *RTL_SOURCES,
    *(f"{CPU_VENDOR}/{name}" for name in ("source.json", "microrom.mem", "nanorom.mem", "fx68k.txt", "LICENSE")),
)
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log", "nextpnr.log",
    "build-inputs.json", "build-summary.json", "manifest.toml", "rom-map.json",
    "socket.qsf", "qor-ranking.json", "fx68k-slang.sv", "microrom.mem", "nanorom.mem",
)
ORDINARY_RESOURCES = frozenset({
    "MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO",
    "MISTRAL_M10K", "MISTRAL_M10K_TDP", "altiobuf_bidir",
})
REQUIRED_RESOURCES = {
    "altera_pll": 2,
    "cyclonev_hps_interface_mpu_general_purpose": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
}
FORBIDDEN_RESOURCES = frozenset({
    "MISTRAL_MLAB", "MISTRAL_MUL9X9", "MISTRAL_MUL18X18", "MISTRAL_MUL18X19",
    "MISTRAL_MUL18X19_COMBINED", "MISTRAL_MUL27X27",
})
REQUIRED_ZERO_RESOURCES = frozenset({"cyclonev_oscillator"})
HEX32_RE = re.compile(r"[0-9a-f]{32}\Z")
BEL_RE = re.compile(r"[A-Z0-9_]+\.(\d+)\.(\d+)\.")


def _authenticate_atari_st_tools(root: Path, cache_root: Path | None = None):
    return _authenticate_tools(
        root,
        lock_path=root / ST_TOOLCHAIN_LOCK,
        toolchain_root=root / ST_TOOLCHAIN_ROOT,
        expected_commits=ST_TOOL_COMMITS,
        expected_configuration={"nextpnr": ST_TOOLCHAIN_CONFIGURATION},
        gpu_router=ST_GPU_ROUTER,
        hip_architectures=ST_GPU_ARCHITECTURES,
        cache_root=cache_root,
    )


def adapted_cpu_source(root: Path) -> bytes:
    """Authenticate every vendor byte and adapt only two simulation pragmas."""
    metadata = _read_json(_regular_input(root, f"{CPU_VENDOR}/source.json"), "FX68K provenance")
    if metadata.get("commit") != CPU_COMMIT or metadata.get("local_changes") != []:
        raise BuildError("FX68K provenance changed")
    entries = metadata.get("files")
    if not isinstance(entries, list) or any(not isinstance(entry, dict) or
            set(entry) != {"path", "sha256"} for entry in entries):
        raise BuildError("FX68K vendor file set changed")
    files = {entry["path"]: entry["sha256"] for entry in entries}
    if len(files) != len(entries) or set(files) != {
            "fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv", "microrom.mem", "nanorom.mem", "fx68k.txt", "LICENSE"}:
        raise BuildError("FX68K vendor file set changed")
    for name, digest in files.items():
        if _sha256(_regular_input(root, f"{CPU_VENDOR}/{name}")) != digest:
            raise BuildError(f"FX68K vendor digest changed: {name}")
    source = _regular_input(root, f"{CPU_VENDOR}/fx68k.sv").read_bytes()
    for old, new in ((b"// synthesis translate off", b"// synthesis translate_off"),
                     (b"// synthesis translate on", b"// synthesis translate_on")):
        if source.count(old) != 1:
            raise BuildError("FX68K simulation pragma count changed")
        source = source.replace(old, new)
    return source


def prepare_cpu_inputs(root: Path, output: Path) -> None:
    _write_atomic(output / "fx68k-slang.sv", adapted_cpu_source(root))
    for name in ("microrom.mem", "nanorom.mem"):
        _write_atomic(output / name, _regular_input(root, f"{CPU_VENDOR}/{name}").read_bytes())


def clock_read_only_memories(path: Path) -> list[str]:
    """Supply the unused write clock only for proven read-only CPU ROMs."""
    design = _read_json(path, "ST synthesized design")
    reject_async_m10k_reads(design)
    repaired = []
    prefixes = ("machine.system.machine.cpu.cpu.nanoRom.nRam.",
                "machine.system.machine.cpu.cpu.uRom.uRam.")
    for name, cell in design["modules"][TOP]["cells"].items():
        if cell["type"] != "MISTRAL_M10K" or cell["connections"].get("CLK1") != ["x"]:
            continue
        pins, parameters = cell["connections"], cell["parameters"]
        def number(key, default=0):
            value = parameters.get(key, default)
            return int(value, 2) if isinstance(value, str) and re.fullmatch("[01]+", value) else value
        inactive_write = "0" if number("CFG_BYTE_ENABLE") or number("CFG_DBITS") == 40 else "1"
        if (not name.startswith(prefixes) or number("CFG_DUAL_CLOCK") != 1 or
                pins.get("A1EN") != [inactive_write] or len(pins.get("CLK2", [])) != 1 or
                type(pins["CLK2"][0]) is not int):
            raise BuildError(f"unexpected disconnected ST M10K write clock: {name}")
        pins["CLK1"] = pins["CLK2"][:]
        repaired.append(name)
    _write_atomic(path, (json.dumps(design, separators=(",", ":")) + "\n").encode())
    return sorted(repaired)


def _require_clean_source(root: Path, *, identity_version: int = 2) -> tuple[str, str]:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
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
    video_output: str = "direct",
) -> bytes:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    if video_output != "direct":
        raise BuildError("ST shell uses built-in Direct; select Scanlines through a sealed video part")
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
            "top": TOP,
            "router": ROUTER,
            "gpu_backend": ST_GPU_BACKEND,
            "gpu_architectures": ST_GPU_ARCHITECTURES,
            "sys_clock_hz": 52_224_000,
            "pixel_clock_hz": 74_250_000,
            "audio_clock_hz": 12_288_000,
            "audio_sample_hz": 48_000,
            "reference_clock_hz": 50_000_000,
            "seed": PLACER_SEEDS[0],
            "seed_order": ",".join(str(seed) for seed in PLACER_SEEDS),
            "placer_heap_timingweight": PLACER_WEIGHT,
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT,
            "placer_timeout_seconds": PLACER_TIMEOUT_SECONDS,
            "toolchain_lock": ST_TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(_regular_input(root, ST_TOOLCHAIN_LOCK)),
            "package_format": 3,
            "rom_id": FIRMWARE_ID,
            "rom_role": "firmware",
            "rom_source_size": FIRMWARE_BYTES,
            "rom_encoding": "m10k-1024x10-v1",
            "rom_database_sha256": json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(",", ":")),
            "video_output": video_output,
            "video_layout": atari_st_video_parts.LAYOUT,
            "video_map": atari_st_video_parts.MAP,
            "video_socket": atari_st_video_parts.PLACEMENT,
            "rom_async_read": 0,
            "cpu_adapter_policy": CPU_ADAPTER_POLICY,
            "cpu_adapter_sha256": hashlib.sha256(adapted_cpu_source(root)).hexdigest(),
            "cpu_rom_clock_policy": "disabled-write-port-clock-v1",
            "video_cache_bels": json.dumps(CACHE_BELS, sort_keys=True, separators=(",", ":")),
            "ram_guard_reservation": RAM_GUARD_RESERVATION,
            "video_ram_guard_reservations": ",".join(VIDEO_RAM_GUARD_RESERVATIONS),
            "ram_socket_configuration_policy": RAM_CONFIG_POLICY,
            "expansion_layout": atari_st_slot.LAYOUT,
            "expansion_sockets": ",".join(s.placement for s in atari_st_slot.SOCKETS),
        },
    }
    fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS),
                                      execution, pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)


def socket_qsf(base: str) -> str:
    if "FES_RESERVED_RECT" in base:
        raise BuildError("base QSF already reserves a rectangle")
    lines = [base.rstrip()]
    for socket in atari_st_slot.SOCKETS:
        lines.append(f'set_global_assignment -name FES_RESERVED_RECT "{socket.placement}"')
    lines.append(f'set_global_assignment -name FES_RESERVED_RECT "{RAM_GUARD_RESERVATION}"')
    for rectangle in VIDEO_RAM_GUARD_RESERVATIONS:
        lines.append(f'set_global_assignment -name FES_RESERVED_RECT "{rectangle}"')
    return atari_st_video_parts.shell_qsf("\n".join(lines) + "\n")


def build_commands(root: Path, output: Path, build_id: str,
                   tools: Mapping[str, Path], seed: int = PLACER_SEEDS[0], *,
                   video_output: str = "direct") -> tuple[tuple[str, ...], tuple[str, ...]]:
    if output not in (root / OUTPUT_RELATIVE, root / DIAGNOSTIC_OUTPUT):
        raise BuildError("Atari ST output must be the selected private build directory")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if video_output != "direct":
        raise BuildError("ST shell uses built-in Direct; select Scanlines through a sealed video part")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    # Slang reads the unchanged vendor declarations in one unit. The private
    # working directory contains the vendor microcode and the bounded adapter.
    verilog_sources = {
        "cores/fes-atari-st/rtl/fes_atari_st_top.v",
        "cores/fes-c64/rtl/c64_system_pll.v",
        "cores/fes-common/rtl/fes_computer_mailbox.v",
        "cores/fes-common/rtl/fes_audio_output.v", "cores/fes-common/rtl/fes_audio_i2s.v",
        "cores/fes-common/rtl/fes_video_part_direct.v",
        "cores/fes-common/rtl/fes_video_part_scanlines.v",
        "cores/fes-atari-st/rtl/st_rom.v", "cores/fes-atari-st/rtl/st_expansion_socket.sv",
        "cores/fes-atari-st/rtl/st_video_socket.sv", "cores/fes-atari-st/rtl/st_media_port.sv",
    }
    legacy = " ".join(f"../../{name}" for name in RTL_SOURCES if name in verilog_sources)
    sources = " ".join("fx68k-slang.sv" if name == f"{CPU_VENDOR}/fx68k.sv"
                       else f"../../{name}" for name in RTL_SOURCES if name not in verilog_sources)
    megafunctions = tools["yosys"].parents[1] / "share/yosys/intel_alm/common/megafunction_bb.v"
    cache_constraints = " ".join(f'setattr -set BEL "{bel}" {TOP}/{name};'
                                  for name, bel in CACHE_BELS.items())
    program = (
        f"read_verilog -sv -DFES_ST_SLANG_IMPORT=1 -I ../../cores/fes-common/generated {legacy}; "
        "read_slang --single-unit --ignore-timing --empty-blackboxes "
        "-I ../../cores/fes-common/generated --top st_system -G ENABLE_FLOPPY_WRITE=1 -G EARLY_RAM_WRITE_COMPLETION=1 "
        "--top st_memory -G PHASE_SLOTS=1 -G POSTED_CPU_WRITES=1 "
        "-G SINGLE_RANK_REFRESH=1 -G SLOT_REFRESH=1 "
        f"--top st_video_adapter --top st_media_writer {megafunctions} {sources}; "
        f"chparam -set BUILD_ID 128'h{build_id} -set VIDEO_SCANLINES {int(video_output == 'scanlines')} {TOP}; "
        f"synth_intel_alm -nolutram -nodsp -top {TOP}; {cache_constraints} stat; write_json synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = (
        str(tools["nextpnr-mistral"]), "--json", str(output / "synth.json"),
        "--device", TARGET, "--qsf", str(output / "socket.qsf"),
        "--sdc", str(root / SDC), "--freq", "74.25", "--seed", str(seed),
        "--placer-heap-timingweight", str(PLACER_WEIGHT),
        "--placer-heap-critexp", str(PLACER_CRITICALITY_EXPONENT),
        "--router", ROUTER, "--timing-allow-fail", "--rbf", str(output / "core.rbf"),
        "--compress-rbf", "--write", str(output / "routed.json"),
        "--report", str(output / "timing.json"), "--detailed-timing-report",
    )
    return yosys, route


def boundary_route_buffer_bel(flip_flop_bel: str) -> str:
    """Dedicated input LUT beside a boundary FF in this pinned Cyclone V layout."""
    match = re.fullmatch(r"MISTRAL_FF\.(24|28)\.(\d+)\.(\d+)", flip_flop_bel)
    if match is None or int(match[3]) % 6 not in (2, 4):
        raise BuildError("unsupported slot boundary flip-flop site")
    column, row, index = map(int, match.groups())
    family = "MISTRAL_COMB" if column == 24 else "MISTRAL_MCOMB"
    lut_index = index // 6 * 6 + (1 if index % 6 == 4 else 0)
    return f"{family}.{column}.{row}.{lut_index}"


def validate_routed_shell(routed: dict) -> dict:
    """Every socket holds its pinned boundary FFs and dedicated input buffers."""
    cells = routed.get("modules", {}).get(TOP, {}).get("cells", {})
    expected = {}
    for socket in atari_st_slot.SOCKETS:
        for name, bel in atari_st_slot.boundary_bels(socket).items():
            expected[socket.instance + name] = bel
    for name, bel in expected.items():
        cell = cells.get(name)
        if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_FF" or \
                cell.get("attributes", {}).get("NEXTPNR_BEL") != bel:
            raise BuildError(f"slot boundary cell {name} is not at {bel}")
    buffers = set()
    outputs = set()
    for name, bel in expected.items():
        buffer_name = name + "$ROUTETHRU"
        cell = cells.get(buffer_name)
        pins = cell.get("connections", {}) if isinstance(cell, dict) else {}
        input_bits, output_bits = pins.get("A"), pins.get("Q")
        if (not isinstance(cell, dict) or cell.get("type") != "MISTRAL_BUF" or
                cell.get("attributes", {}).get("NEXTPNR_BEL") != boundary_route_buffer_bel(bel) or
                set(pins) != {"A", "Q"} or
                cell.get("port_directions") != {"A": "input", "Q": "output"} or
                not isinstance(input_bits, list) or len(input_bits) != 1 or type(input_bits[0]) is not int or
                not isinstance(output_bits, list) or len(output_bits) != 1 or type(output_bits[0]) is not int or
                input_bits == output_bits or
                output_bits != cells[name].get("connections", {}).get("DATAIN") or
                output_bits[0] in outputs):
            raise BuildError(f"slot boundary route buffer {buffer_name} changed")
        outputs.add(output_bits[0])
        buffers.add(buffer_name)
    for name, cell in cells.items():
        bel = cell.get("attributes", {}).get("NEXTPNR_BEL", "") if isinstance(cell, dict) else ""
        match = BEL_RE.match(bel)
        if not match or name in expected or name in buffers:
            continue
        x, y = int(match.group(1)), int(match.group(2))
        for socket in atari_st_slot.SOCKETS:
            if atari_st_slot.COLUMN <= x <= atari_st_slot.COLUMN + 4 and socket.first_row <= y <= socket.last_row:
                raise BuildError(f"shell cell {name} is inside the slot {socket.slot} socket")
    return {"layout": atari_st_slot.LAYOUT, "sockets": [s.slot for s in atari_st_slot.SOCKETS],
            "pinned_boundary_cells": len(expected), "pinned_boundary_route_buffers": len(buffers)}


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


def validate_synth_evidence(output: Path) -> dict:
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    reject_async_m10k_reads(synthesis)
    _i2c_evidence(synthesis, "synthesized")
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    placement = json.loads(json.dumps(synthesis))
    for cell in placement["modules"][TOP]["cells"].values():
        attributes = cell.get("attributes", {})
        if "BEL" in attributes:
            attributes["NEXTPNR_BEL"] = attributes["BEL"]
    rom_map.validate_routed_rom(placement, FIRMWARE_LANE_ROWS, expected_async_read=0)
    validate_firmware_ports(synthesis["modules"][TOP]["cells"])
    validate_cache_placements(synthesis["modules"][TOP]["cells"], routed=False)
    validate_sector_memory(synthesis["modules"][TOP]["cells"])
    atari_st_video_parts.validate_boundary(synthesis["modules"][TOP], routed=False)
    # Microcode and video-cache memories are counted from real synthesis.
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0):
            raise BuildError(f"forbidden synthesis cell {name} is in use")
    return {"status": "pass", "synthesis_cells": {name: counts[name] for name in sorted(counts)}}


def validate_firmware_ports(cells: dict) -> None:
    """The linked ROM has one live read clock and permanently disabled writes."""
    clocks = set()
    for lane in range(len(FIRMWARE_LANE_ROWS)):
        name = f"machine.rom.lane{lane}"
        pins = cells.get(name, {}).get("connections", {})
        clock = pins.get("CLK1", [])
        if (len(clock) != 1 or type(clock[0]) is not int or
                pins.get("A1EN") != ["1"] or pins.get("B1EN") != ["1"] or
                pins.get("A1BE") or pins.get("CLK2")):
            raise BuildError(f"firmware lane {name} must have one live clock, disabled writes and no optional ports")
        clocks.add(clock[0])
    if len(clocks) != 1:
        raise BuildError("firmware lanes must share the system read clock")


def validate_cache_placements(cells: dict, *, routed: bool) -> dict:
    """Require the two actual inferred caches at the selected safe RAM sites."""
    attribute = "NEXTPNR_BEL" if routed else "BEL"
    caches = {name: cell for name, cell in cells.items()
              if name.startswith("video.cache") and cell.get("type") in ("MISTRAL_M10K", "MISTRAL_M10K_TDP")}
    if set(caches) != set(CACHE_BELS):
        raise BuildError("ST video must infer exactly the two selected M10K line caches")
    for name, bel in CACHE_BELS.items():
        if caches[name].get("type") != "MISTRAL_M10K" or caches[name].get("attributes", {}).get(attribute) != bel:
            raise BuildError(f"ST video cache {name} must occupy {bel}")
    return dict(CACHE_BELS)


def validate_sector_memory(cells: dict) -> dict:
    """The writable sector stage must be one synchronous RAM on the system clock."""
    name = "machine.system.io.floppy.writer.sector.0.0.0"
    prefix = "machine.system.io.floppy.writer.sector"
    memories = {key: cell for key, cell in cells.items()
                if key.startswith(prefix) and cell.get("type") in ("MISTRAL_M10K", "MISTRAL_M10K_TDP")}
    array_flops = any(key.startswith(prefix) and cell.get("type") == "MISTRAL_FF"
                      for key, cell in cells.items())
    if set(memories) != {name} or memories[name].get("type") != "MISTRAL_M10K" or array_flops:
        raise BuildError("ST writable sector stage must infer exactly one M10K, with no array flip-flops")
    cell = memories[name]
    pins, parameters = cell.get("connections", {}), cell.get("parameters", {})
    for key, expected in (("CFG_ABITS", 9), ("CFG_DBITS", 20),
                          ("CFG_BYTE_ENABLE", 1), ("CFG_DUAL_CLOCK", 1)):
        value = parameters.get(key)
        if isinstance(value, str) and re.fullmatch("[01]+", value):
            value = int(value, 2)
        if type(value) is not int or value != expected:
            raise BuildError(f"ST sector RAM has incorrect {key}")
    clock = cells.get("machine.rom.lane0", {}).get("connections", {}).get("CLK1")
    if (not isinstance(clock, list) or len(clock) != 1 or type(clock[0]) is not int or
            pins.get("CLK1") != clock or pins.get("CLK2") != clock):
        raise BuildError("ST sector RAM read and write must share the live firmware system clock")
    for port in ("A1ADDR", "B1ADDR"):
        value = pins.get(port, [])
        if not isinstance(value, list) or len(value) != 9:
            raise BuildError("ST sector RAM must address exactly 256 words")
    for port in ("A1DATA", "B1DATA"):
        value = pins.get(port, [])
        if not isinstance(value, list) or len(value) != 20:
            raise BuildError("ST sector RAM must have a live 16-bit data path")
    for port in ("A1EN", "B1EN"):
        value = pins.get(port, [])
        if not isinstance(value, list) or len(value) != 1:
            raise BuildError("ST sector RAM needs live synchronous read and write enables")
    if pins.get("A1BE") != pins["A1EN"] * 2:
        raise BuildError("ST sector RAM byte enables must follow its write enable")

    # Packing replaces literal zero ties with a shared MISTRAL_CONST output.
    # Resolve only that primitive, with a complete, unique driver census. An
    # integer net ID alone proves neither a zero tie nor a live RAM input.
    checked_ports = ("A1ADDR", "B1ADDR", "A1DATA", "B1DATA", "A1EN", "B1EN", "CLK1", "CLK2")
    relevant = {bit for port in checked_ports for bit in pins[port] if type(bit) is int}
    drivers = {bit: [] for bit in relevant}
    for source_name, source in cells.items():
        for port, bits in source.get("connections", {}).items():
            direction = source.get("port_directions", {}).get(port)
            for bit in bits:
                if type(bit) is int and bit in relevant:
                    if direction == "output":
                        drivers[bit].append((source_name, port))
                    elif direction != "input":
                        raise BuildError("ST sector RAM net has an unknown port direction")

    def constant_value(bit):
        if type(bit) is not int:
            if bit in ("0", "1"):
                return int(bit)
            raise BuildError("ST sector RAM net has an unknown value")
        sources = drivers[bit]
        if len(sources) != 1:
            raise BuildError("ST sector RAM net must have exactly one driver")
        source_name, port = sources[0]
        source = cells[source_name]
        if source.get("type") != "MISTRAL_CONST":
            return None
        lut = source.get("parameters", {}).get("LUT")
        if (port != "Q" or source.get("connections", {}).get("Q") != [bit] or
                source.get("port_directions", {}).get("Q") != "output" or
                not isinstance(lut, str) or not re.fullmatch("[01]{32}", lut) or
                int(lut, 2) not in (0, 1)):
            raise BuildError("ST sector RAM constant driver is not a proven zero or one")
        return int(lut, 2)

    def require_live(bits, message):
        for bit in bits:
            if type(bit) is not int or constant_value(bit) is not None:
                raise BuildError(message)

    require_live(clock, "ST sector RAM read and write must share the live firmware system clock")
    for port in ("A1ADDR", "B1ADDR"):
        require_live(pins[port][:8], "ST sector RAM must have eight live address bits")
        if constant_value(pins[port][8]) != 0:
            raise BuildError("ST sector RAM must address exactly 256 words")
    for port in ("A1DATA", "B1DATA"):
        require_live(pins[port][:16], "ST sector RAM must have a live 16-bit data path")
    if any(constant_value(bit) != 0 for bit in pins["A1DATA"][16:]):
        raise BuildError("ST sector RAM unused write data must be zero")
    for port in ("A1EN", "B1EN"):
        require_live(pins[port], "ST sector RAM needs live synchronous read and write enables")
    return {"status": "pass", "cell": name, "words": 256, "bits_per_word": 16}


def m10k_configuration_bounds(database: Mapping[str, bytes]) -> tuple[int, int, int, int]:
    """Bound every known M10K BM_CRAM bit, including INIT and all controls.

    The pinned table generates bm_m10k: 256x40 INIT bits and 75 global fields
    containing 126 mode/control/port-selector bits. All use pos2bit plus their
    table (dx,dy). The bounding rectangle conservatively includes its holes.
    Routing PIPs are outside this query's coverage.
    """
    if set(database) != set(ROM_DATABASE_SHA256) or any(
            hashlib.sha256(database[name]).hexdigest() != digest
            for name, digest in ROM_DATABASE_SHA256.items()):
        raise BuildError("M10K configuration geometry differs from pinned Mistral database")
    text = database["data/m10k-mux.txt"].decode()
    init = {point for word in rom_map.parse_ram_offsets(text) for point in word}
    controls = set()
    fields = 0
    for line in text.splitlines():
        tokens = line.split()
        if not tokens or tokens[0] != "g":
            continue
        fields += 1
        count = int(tokens[2].split(":")[1]) if ":" in tokens[2] else 1
        if len(tokens) != 3 + count:
            raise BuildError("M10K global configuration field cardinality changed")
        for token in tokens[3:]:
            if not re.fullmatch(r"[0-9]+\.[0-9]+", token):
                raise BuildError("M10K global configuration coordinate changed")
            point = tuple(map(int, token.split(".")))
            if point in controls:
                raise BuildError("duplicate M10K global configuration coordinate")
            controls.add(point)
    if fields != 75 or len(init) != 10240 or len(controls) != 126 or init & controls:
        raise BuildError("M10K INIT/control configuration cardinality changed")
    points = init | controls
    if any(not 0 <= x < 300 or not 0 <= y < 86 for x, y in points):
        raise BuildError("M10K configuration coordinate is outside its tile")
    die = database["libmistral/cvd-sx120f.cc"].decode()
    columns = tuple(map(int, re.findall(r"\d+", rom_map._section(die, "x to bit x"))))
    if columns != rom_map.SX120F.x_to_bx or not re.search(r"7605\s*,\s*7024\s*,\s*// cram size", die):
        raise BuildError("M10K die configuration geometry differs from codec")
    if not re.search(r"y\s*=\s*2\s*\+\s*86\s*\*\s*(?:pos2y\(pos\)|pos\.y\(\))",
                     database["libmistral/cyclonev.h"].decode()):
        raise BuildError("M10K configuration row geometry changed")
    return min(x for x, _ in points), min(y for _, y in points), \
        max(x for x, _ in points) + 1, max(y for _, y in points) + 1


def validate_m10k_configurations(routed: dict, database: Mapping[str, bytes]) -> dict:
    """Reject any ordinary RAM's local configuration inside a socket CRAM."""
    dx0, dy0, dx1, dy1 = m10k_configuration_bounds(database)
    kinds = re.findall(r"T_\w+", rom_map._section(database["libmistral/cvd-sx120f.cc"].decode(), "column types"))
    cells = routed.get("modules", {}).get(TOP, {}).get("cells", {})
    checked = {}
    for name, cell in cells.items():
        if cell.get("type") not in ("MISTRAL_M10K", "MISTRAL_M10K_TDP"):
            continue
        bel = cell.get("attributes", {}).get("NEXTPNR_BEL", "")
        match = re.fullmatch(r"MISTRAL_M10K\.(\d+)\.(\d+)\.0", bel)
        if not match:
            raise BuildError(f"M10K configuration BEL is missing or invalid: {name}")
        column, row = map(int, match.groups())
        if column >= len(kinds) or kinds[column] != "T_M10K" or not 1 <= row <= 80:
            raise BuildError(f"M10K configuration BEL is outside the selected die: {name} at {bel}")
        xbase, ybase = rom_map.SX120F.x_to_bx[column], 2 + 86 * row
        bounds = (xbase + dx0, ybase + dy0, xbase + dx1, ybase + dy1)
        for socket in atari_st_slot.SOCKETS:
            sx0, sy0, sx1, sy1 = socket.cram
            if bounds[0] < sx1 and sx0 < bounds[2] and bounds[1] < sy1 and sy0 < bounds[3]:
                raise BuildError(f"M10K configuration footprint {name} at {bel} overlaps slot {socket.slot} CRAM")
        vx0, vy0, vx1, vy1 = atari_st_video_parts.CRAM
        if bounds[0] < vx1 and vx0 < bounds[2] and bounds[1] < vy1 and vy0 < bounds[3]:
            raise BuildError(f"M10K configuration footprint {name} at {bel} overlaps video CRAM")
        checked[name] = {"bel": bel, "configuration_bounds": list(bounds)}
    if not checked:
        raise BuildError("routed ST design has no M10K configuration evidence")
    return {"status": "pass", "policy": RAM_CONFIG_POLICY, "checked_cells": len(checked),
            "init_bits_per_cell": 10240, "mode_control_bits_per_cell": 126,
            "configuration_bounds_offsets": [dx0, dy0, dx1, dy1], "cells": checked,
            "routing_coverage": "RAM-local BM_CRAM only; routing PIPs use the frozen shell/card containment contract"}


def validate_build_evidence(output: Path, *, ram_database: Mapping[str, bytes]) -> dict:
    routed = _read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    synth = validate_synth_evidence(output)
    reject_async_m10k_reads(routed)
    _i2c_evidence(routed, "routed")
    sockets = validate_routed_shell(routed)
    cache_placements = validate_cache_placements(routed["modules"][TOP]["cells"], routed=True)
    ram_configuration = validate_m10k_configurations(routed, ram_database)
    sector_memory = validate_sector_memory(routed["modules"][TOP]["cells"])
    atari_st_video_parts.validate_boundary(routed["modules"][TOP], routed=True)
    route_text = (output / "nextpnr.log").read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    gpu_backend = _require_gpu_backend(route_text)
    if "50 MHz -> 52.224 MHz" not in route_text:
        raise BuildError("route log does not contain the 50-to-52.224 MHz system PLL")
    if not re.search(r"PLL 'system_clock.pll': second output 12\.288 MHz", route_text):
        raise BuildError("route log must prove the 12.288 MHz audio PLL")
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
        "sockets": sockets,
        "video_socket": {"status": "pass", "layout": atari_st_video_parts.LAYOUT,
                         "map": atari_st_video_parts.MAP, "cram": list(atari_st_video_parts.CRAM),
                         "pinned_boundary_cells": len(atari_st_video_parts.boundary_bels()),
                         "pinned_request_egress_cells": len(atari_st_video_parts.request_egress_bels())},
        "video_cache_placements": cache_placements,
        "floppy_sector_memory": sector_memory,
        "ram_socket_configuration": ram_configuration,
        "synthesis_cells": synth["synthesis_cells"],
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }


def check_firmware_outside_sockets(mapping: dict) -> None:
    width = 7605
    for block in mapping["blocks"]:
        for word in block["word_bits"]:
            for bit in word:
                x, y = bit % width, bit // width
                for socket in atari_st_slot.SOCKETS:
                    x0, y0, x1, y1 = socket.cram
                    if x0 <= x < x1 and y0 <= y < y1:
                        raise BuildError(f"firmware lane {block['bel']} writes CRAM in slot {socket.slot}")
                vx0, vy0, vx1, vy1 = atari_st_video_parts.CRAM
                if vx0 <= x < vx1 and vy0 <= y < vy1:
                    raise BuildError(f"firmware lane {block['bel']} writes video CRAM")


def _manifest(record: bytes, evidence: dict, repository: str, revision: str,
              tools: Mapping[str, str]) -> bytes:
    rbf = evidence["rbf"]
    record_fields = json.loads(record)
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    required = [
        "fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports",
        "fes.audio.pcm-s16-stereo-48k", "fes.media.atari-st-floppy",
        "fes.mouse.relative", "fes.media.atari-st-floppy-write", "fes.media.atari-st-floppy-geometry",
    ]
    fields = {
        "format": 3,
        "core": {
            "id": "fes.atari-st",
            "name": "FES Atari 520ST",
            "description": "Atari 520ST with linked 192 KiB firmware, 512 KiB SDRAM, "
                           "a writable variable-geometry floppy, relative mouse, and ST expansion/video sockets",
            "version": "0.2.0",
        },
        "target": {"platform": "de10_nano", "device": TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{"id": interface, "major": 1, "minor": 0, "required": True} for interface in required] +
                      [{"id": interface, "major": 1, "minor": 0, "required": False}
                       for interface in (atari_st_slot.INTERFACE, atari_st_video_parts.INTERFACE)],
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
          identity_version: int = 2, gpu_device: int = 0, video_output: str = "direct") -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES Atari 520ST package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    authenticated = _authenticate_atari_st_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    database_root = authenticated["mistral"].path.parents[2] / "src/mistral"
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    invocation = FunctionalInvocation(authenticated, gpu_device)
    output = None
    try:
        record = create_build_record(root, repository, revision, identities,
                                     identity_version=identity_version, execution=invocation.inputs, video_output=video_output)
        output = _prepare_output(root, relative=OUTPUT_RELATIVE, build_outputs=BUILD_OUTPUTS)
        prepare_cpu_inputs(root, output)
        _write_atomic(output / "build-inputs.json", record)
        _write_atomic(output / "socket.qsf", socket_qsf((root / QSF).read_text()).encode())
        build_id = build_identity(record)
        yosys, _route = build_commands(root, output, build_id, {
            name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, video_output=video_output)
        _run_tool(yosys, output, output / "yosys.log", env=invocation.env,
                  audit_source_root=root, output_relative=Path("."))
        if not (output / "synth.json").is_file():
            raise BuildError("Yosys did not produce synthesis evidence")
        repaired = clock_read_only_memories(output / "synth.json")
        validate_synth_evidence(output)
        try:
            winner = route_after_synth(
                nextpnr=authenticated["nextpnr-mistral"].path,
                fixture=output / "synth.json", dest=output, device=TARGET,
                qsf=output / "socket.qsf", sdc=root / SDC, freq="74.25",
                seeds=PLACER_SEEDS, weights=(PLACER_WEIGHT,),
                critexp=PLACER_CRITICALITY_EXPONENT, budget=len(PLACER_SEEDS),
                mode="first-pass", extra=("--router", ROUTER),
                timeout=PLACER_TIMEOUT_SECONDS,
                required=PLACER_QOR_CLOCKS, gpu_devices=(gpu_device,),
                env=invocation.env, audit_source_root=root,
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, ram_database=database)
        evidence["cpu_rom_clock_repairs"] = repaired
        evidence["route"].update(placer_seed=winner.seed, placer_heap_timingweight=winner.weight,
                                 placer_qor_mode="first-pass")
        mapping, map_evidence = rom_map.build_rom_map(
            database, (output / "core.rbf").read_bytes(),
            routed=_read_json(output / "routed.json", "routed firmware design"),
            lane_rows=FIRMWARE_LANE_ROWS, expected_async_read=0,
        )
        check_firmware_outside_sockets(mapping)
        map_bytes = (json.dumps(mapping, sort_keys=True, separators=(",", ":")) + "\n").encode()
        _write_atomic(output / "rom-map.json", map_bytes)
        evidence["rom"] = {"id": FIRMWARE_ID, "role": "firmware", "source_size": FIRMWARE_BYTES,
                           "file": "rom-map.json", "size": len(map_bytes),
                           "sha256": _sha256(output / "rom-map.json")}
        evidence["rom_map"] = map_evidence
        evidence["execution"] = invocation.inputs
        evidence.update({
            "build_id": build_id, "device": TARGET, "top": TOP, "tools": identities,
            "inputs": {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)},
        })
        _write_atomic(output / "build-summary.json",
                      (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode())
        manifest = _manifest(record, evidence, repository, revision, identities)
        _write_atomic(output / "manifest.toml", manifest)
        final_tools = _authenticate_atari_st_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError("authenticated tool identity changed during build")
        if _require_clean_source(root, identity_version=identity_version) != (repository, revision):
            raise BuildError("source identity changed during build")
        invocation.verify()
        if rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError("ROM database changed during build")
        if create_build_record(root, repository, revision, identities,
                               identity_version=identity_version, execution=invocation.inputs, video_output=video_output) != record:
            raise BuildError("functional source inputs changed during build")
        return export_package(manifest, output / "core.rbf", package_store,
                              rom_map=output / "rom-map.json")
    except BaseException:
        if output is not None:
            for name in ("core.rbf", "manifest.toml", "build-summary.json", "rom-map.json"):
                path = output / name
                if path.is_file() or path.is_symlink():
                    path.unlink()
        raise
    finally:
        invocation.close()


def synth(root: Path = ROOT, *, cache_root: Path | None = None,
          video_output: str = "direct") -> dict:
    """Run authenticated Yosys without a clean-tree seal or package export."""
    root = Path(root).resolve()
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
    authenticated = _authenticate_atari_st_tools(root, cache_root=cache_root)
    invocation = FunctionalInvocation(authenticated, 0)
    try:
        output = _prepare_output(root, relative=DIAGNOSTIC_OUTPUT, build_outputs=BUILD_OUTPUTS)
        prepare_cpu_inputs(root, output)
        yosys, _route = build_commands(root, output, "0" * 32, {
            name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
            video_output=video_output)
        _run_tool(yosys, output, output / "yosys.log", env=invocation.env,
                  audit_source_root=root, output_relative=Path("."))
        repaired = clock_read_only_memories(output / "synth.json")
        evidence = validate_synth_evidence(output)
        evidence["cpu_rom_clock_repairs"] = repaired
        evidence.update({"build_id": "0" * 32, "sealed": False, "video_output": video_output,
            "video_layout": atari_st_video_parts.LAYOUT,
            "video_map": atari_st_video_parts.MAP,
            "video_socket": atari_st_video_parts.PLACEMENT,
                         "tools": {name: tool.identity for name, tool in authenticated.items()}})
        invocation.verify()
        _write_atomic(output / "build-summary.json",
                      (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode())
        return evidence
    finally:
        invocation.close()


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--identity-version", type=int, choices=(2,), default=2)
    parser.add_argument("--gpu-device", type=int, default=0)
    parser.add_argument("--video-output", choices=("direct", "scanlines"), default="direct")
    parser.add_argument("--synth-only", action="store_true",
                        help="run Yosys only; skip the clean-tree seal and nextpnr")
    arguments = parser.parse_args(argv)
    try:
        if arguments.synth_only:
            cells = synth(arguments.root, cache_root=arguments.cache_root, video_output=arguments.video_output)["synthesis_cells"]
            print(f"synth-only MISTRAL_M10K={cells.get('MISTRAL_M10K', 0)} "
                  f"MISTRAL_M10K_TDP={cells.get('MISTRAL_M10K_TDP', 0)} "
                  f"MISTRAL_FF={cells.get('MISTRAL_FF', 0)}")
            return 0
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root,
                    identity_version=arguments.identity_version, gpu_device=arguments.gpu_device,
                    video_output=arguments.video_output))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-atari-st-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
