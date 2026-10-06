#!/usr/bin/env python3
"""Build and seal the OSS HIP nextpnr/Mistral FES Commodore 64 package.

The package is format 3: the sixteen blank 1024x10 firmware lanes (column 5,
rows 32-47) are described by a sealed ROM map for the 16,384-byte
`c64-firmware` image that FogCast links at download time. The shell reserves
the two named cartridge-socket rectangles of `fes.c64-bus.sockets/1` and pins
each socket's boundary flip-flops and their verified route-throughs; no other
shell cell may sit in a socket and no firmware destination may fall in a socket's CRAM rectangle.

Memory-cell totals are not pinned here. A seal records the synthesized M10K
counts; guessing them before that run would reject a correct netlist.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from pathlib import Path
from typing import Mapping, Sequence

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import c64_slots, coleco_expansion, rom_map
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
RECIPE = "scripts/build_fes_c64_oss.py"
OUTPUT_RELATIVE = Path("build/fes-c64-oss")
C64_TOOLCHAIN_LOCK = "toolchains/c64.lock"
C64_TOOLCHAIN_ROOT = "build/toolchain/fes-c64"
C64_GPU_BACKEND = "hip"
C64_GPU_ROUTER = "HIP"
C64_GPU_ARCHITECTURES = "gfx1100;gfx1201"
C64_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={C64_GPU_ROUTER}; hip-architectures={C64_GPU_ARCHITECTURES}"
)
C64_TOOL_COMMITS = {
    "mistral": "8fcc4cb41c51f8918f1d3ad70def2febcbf20d8f",
    "nextpnr": "1656e473e1442f9b734ff5f4cdfddfd013846b9e",
    "yosys": "5391eeb1e91b38a3d0e96d04f24cf921743c9c78",
}
# First passing route wins; the order is part of the build identity. This
# core has not been sealed, so the search starts with the Apple II order.
PLACER_SEEDS = (5, 4, 2, 1, 3, 6, 7, 8, 9, 10)
PLACER_WEIGHT = 2000
PLACER_CRITICALITY_EXPONENT = 5
PLACER_QOR_CLOCKS = ((None, 52.224), (None, 74.25), (None, 12.288))
# nextpnr #136 (1656e473) logs ref->VCO + fractional-N + M/N/K + counters and
# one requested/achieved line per output. VCO 417.792 MHz: C6 /8 = 52.224 MHz
# system, C7 /34 = 12.288 MHz audio. The PLL BEL is placement, not identity.
SYSTEM_PLL_ROUTE_RE = (
    r"Info: PLL 'system_clock\.pll': 50\.000000 MHz -> VCO 417\.792000 MHz, fractional-N, "
    r"M=8 N=1 K=1528321163, counters C6,7, bel altera_pll\.[0-9.]+"
)
SYSTEM_PLL_OUTPUT_RE = r"Info: PLL 'system_clock\.pll': fractional-N requested 52224000\.000000 Hz \(output 0\)"
AUDIO_PLL_OUTPUT_RE = r"Info: PLL 'system_clock\.pll': fractional-N requested 12288000\.000000 Hz \(output 1\)"
ROM_DATABASE_SHA256 = {
    "data/m10k-mux.txt": "22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe",
    "libmistral/cvd-sx120f.cc": "e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7",
    "libmistral/cyclonev.h": "185c24e2b75385d8e62a7144f12488eb68f7d8159af0a8a603cc4a3de1749ee9",
}
FIRMWARE_LANE_ROWS = tuple(range(32, 48))
FIRMWARE_ID = "c64-firmware"
FIRMWARE_BYTES = 16384
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
QSF = "cores/fes-c64/constraints-oss.qsf"
SDC = "cores/fes-c64/clocks-oss.sdc"
RTL_INCLUDES = (
    "cores/fes-c64/rtl/c64_bus.vh",
    "cores/fes-c64/rtl/c64_font.vh",
)
RTL_SOURCES = (
    "cores/fes-c64/rtl/top.v",
    "cores/fes-c64/rtl/c64_system_pll.v",
    "cores/fes-common/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-c64/rtl/c64_machine.sv",
    "cores/fes-c64/rtl/c64_ram.v",
    "cores/fes-c64/rtl/c64_rom.v",
    "cores/fes-c64/rtl/c64_vic.v",
    "cores/fes-c64/rtl/c64_sid.v",
    "cores/fes-c64/rtl/c64_cia.v",
    "cores/fes-c64/rtl/c64_iec.sv",
    "cores/fes-c64/rtl/c64_disk_store.v",
    "cores/fes-c64/rtl/c64_keyboard.v",
    "cores/fes-c64/rtl/c64_slot_sockets.v",
    "cores/fes-common/rtl/cpu6502/cpu6502.v",
    "cores/fes-common/rtl/cpu6502/cpu6502_alu.v",
)
PINNED_INPUTS = (
    RECIPE, "scripts/c64_slots.py", "scripts/coleco_expansion.py", "scripts/compiler_read_audit.py",
    "scripts/source_repository.py", "scripts/fes_build_common.py", "scripts/rom_map.py",
    "scripts/cyclonev_rbf.py", "scripts/search_placer_qor.py",
    ABI_DEFINITION, C64_TOOLCHAIN_LOCK, QSF, SDC, *RTL_INCLUDES, *RTL_SOURCES,
)
BUILD_OUTPUTS = (
    "synth.json", "routed.json", "core.rbf", "timing.json", "yosys.log", "nextpnr.log",
    "build-inputs.json", "build-summary.json", "manifest.toml", "rom-map.json",
    "socket.qsf", "qor-ranking.json",
)
ORDINARY_RESOURCES = frozenset({
    "MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO",
    "MISTRAL_M10K", "MISTRAL_M10K_TDP",
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


def _authenticate_c64_tools(root: Path, cache_root: Path | None = None):
    return _authenticate_tools(
        root,
        lock_path=root / C64_TOOLCHAIN_LOCK,
        toolchain_root=root / C64_TOOLCHAIN_ROOT,
        expected_commits=C64_TOOL_COMMITS,
        expected_configuration={"nextpnr": C64_TOOLCHAIN_CONFIGURATION},
        gpu_router=C64_GPU_ROUTER,
        hip_architectures=C64_GPU_ARCHITECTURES,
        cache_root=cache_root,
    )


def clock_read_only_memories(path: Path) -> None:
    """Give inferred read-only M10Ks a live, otherwise unused port-A clock."""
    design = _read_json(path, "C64 synthesized design")
    reject_async_m10k_reads(design)
    cells = design["modules"][TOP]["cells"]
    expected = ("machine.iec.file_track_", "machine.vic.code_q_")
    found = set()
    for name, cell in cells.items():
        if cell["type"] != "MISTRAL_M10K":
            continue
        pins = cell["connections"]
        if pins.get("CLK1") != ["x"]:
            continue
        match = next((prefix for prefix in expected if name.startswith(prefix)), None)
        # Autoname runs after ABC and can name either read-only ROM from a
        # connected net instead of its RTL prefix. The VIC glyph ROM has been
        # named from a main_ram address net; the IEC track ROM can lose
        # machine.iec.file_track_ the same way. A disconnected read-only M10K
        # is accepted as whichever of those two roles no other disconnected
        # cell still claims by name.
        if match is None:
            match = next((prefix for prefix in expected
                          if prefix not in found and not any(
                              n.startswith(prefix) and c["type"] == "MISTRAL_M10K"
                              and c["connections"].get("CLK1") == ["x"]
                              for n, c in cells.items())), None)
        if (match is None or match in found or pins.get("A1EN") != ["0"]
                or pins.get("B1EN") != ["1"] or len(pins.get("CLK2", [])) != 1
                or not isinstance(pins["CLK2"][0], int)):
            raise BuildError(f"unexpected disconnected C64 M10K clock: {name}")
        pins["CLK1"] = pins["CLK2"][:]
        found.add(match)
    if found != set(expected):
        raise BuildError(f"C64 read-only M10K clock set changed: {sorted(found)}")
    _write_atomic(path, (json.dumps(design, separators=(",", ":")) + "\n").encode())


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
) -> bytes:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
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
            "gpu_backend": C64_GPU_BACKEND,
            "gpu_architectures": C64_GPU_ARCHITECTURES,
            "sys_clock_hz": 52_224_000,
            "pixel_clock_hz": 74_250_000,
            "audio_clock_hz": 12_288_000,
            "audio_sample_hz": 48_000,
            "reference_clock_hz": 50_000_000,
            "seed": PLACER_SEEDS[0],
            "seed_order": ",".join(str(seed) for seed in PLACER_SEEDS),
            "placer_heap_timingweight": PLACER_WEIGHT,
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT,
            "toolchain_lock": C64_TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(_regular_input(root, C64_TOOLCHAIN_LOCK)),
            "package_format": 3,
            "rom_id": FIRMWARE_ID,
            "rom_role": "firmware",
            "rom_source_size": FIRMWARE_BYTES,
            "rom_encoding": "m10k-1024x10-v1",
            "rom_database_sha256": json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(",", ":")),
            "expansion_layout": c64_slots.LAYOUT,
            "expansion_sockets": ",".join(s.placement for s in c64_slots.SOCKETS),
        },
    }
    fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS),
                                      execution, pinned_inputs=PINNED_INPUTS)
    return encode_build_record(fields)


def socket_qsf(base: str) -> str:
    if "FES_RESERVED_RECT" in base:
        raise BuildError("base QSF already reserves a rectangle")
    lines = [base.rstrip()]
    for socket in c64_slots.SOCKETS:
        lines.append(f'set_global_assignment -name FES_RESERVED_RECT "{socket.placement}"')
    return "\n".join(lines) + "\n"


def build_commands(root: Path, output: Path, build_id: str,
                   tools: Mapping[str, Path], seed: int = PLACER_SEEDS[0]) -> tuple[tuple[str, ...], tuple[str, ...]]:
    if output != root / OUTPUT_RELATIVE:
        raise BuildError(f"FES Commodore 64 OSS output must be {root / OUTPUT_RELATIVE}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    program = (
        "read_verilog -sv -I cores/fes-c64/rtl -I cores/fes-common/generated "
        f"{' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} {TOP}; "
        f"synth_intel_alm -nolutram -nodsp -top {TOP}; stat; "
        f"write_json {OUTPUT_RELATIVE.as_posix()}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = (
        str(tools["nextpnr-mistral"]), "--json", f"{OUTPUT_RELATIVE.as_posix()}/synth.json",
        "--device", TARGET, "--qsf", f"{OUTPUT_RELATIVE.as_posix()}/socket.qsf",
        "--sdc", SDC, "--freq", "74.25", "--seed", str(seed),
        "--placer-heap-timingweight", str(PLACER_WEIGHT),
        "--placer-heap-critexp", str(PLACER_CRITICALITY_EXPONENT),
        "--router", ROUTER, "--timing-allow-fail",
        "--rbf", f"{OUTPUT_RELATIVE.as_posix()}/core.rbf", "--compress-rbf",
        "--write", f"{OUTPUT_RELATIVE.as_posix()}/routed.json",
        "--report", f"{OUTPUT_RELATIVE.as_posix()}/timing.json", "--detailed-timing-report",
    )
    return yosys, route


def validate_routed_shell(routed: dict) -> dict:
    """Every socket holds only pinned boundary FFs and verified paired buffers."""
    top = routed.get("modules", {}).get(TOP, {})
    cells = top.get("cells", {})
    expected = {}
    for socket in c64_slots.SOCKETS:
        for name, bel in c64_slots.boundary_bels(socket).items():
            expected[socket.instance + name] = bel
    for name, bel in expected.items():
        cell = cells.get(name)
        if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_FF" or \
                cell.get("attributes", {}).get("NEXTPNR_BEL") != bel:
            raise BuildError(f"slot boundary cell {name} is not at {bel}")
    try:
        route_through = coleco_expansion.boundary_route_through_cells(top, expected)
        coleco_expansion.validate_clock_anchors(top, {
            name: bel for name, bel in expected.items() if ".clock_coverage_ff_" in name})
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
        for socket in c64_slots.SOCKETS:
            if c64_slots.COLUMN <= x <= c64_slots.COLUMN + 4 and socket.first_row <= y <= socket.last_row:
                raise BuildError(f"shell cell {name} is inside the slot {socket.slot} socket")
    return {"layout": c64_slots.LAYOUT, "sockets": [s.slot for s in c64_slots.SOCKETS],
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


def validate_firmware_ports(cells: dict) -> None:
    """Require a shared live read clock, enabled reads and disabled ROM writes."""
    clocks = set()
    for lane in range(len(FIRMWARE_LANE_ROWS)):
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


def validate_synth_evidence(output: Path) -> dict:
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    reject_async_m10k_reads(synthesis)
    validate_firmware_ports(synthesis["modules"][TOP]["cells"])
    _i2c_evidence(synthesis, "synthesized")
    counts = _cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    # Firmware lanes are sixteen explicit M10Ks. RAM, the D64 store and the
    # font are also memories, but their cell totals are whatever synthesis
    # reports; they are not guessed ahead of a seal.
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0):
            raise BuildError(f"forbidden synthesis cell {name} is in use")
    return {"status": "pass", "synthesis_cells": {name: counts[name] for name in sorted(counts)}}


def validate_build_evidence(output: Path) -> dict:
    routed = _read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    synth = validate_synth_evidence(output)
    reject_async_m10k_reads(routed)
    _i2c_evidence(routed, "routed")
    sockets = validate_routed_shell(routed)
    route_text = (output / "nextpnr.log").read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    gpu_backend = _require_gpu_backend(route_text)
    if not re.search(SYSTEM_PLL_ROUTE_RE, route_text) or not re.search(SYSTEM_PLL_OUTPUT_RE, route_text):
        raise BuildError("route log does not contain the 50-to-52.224 MHz system PLL")
    if not re.search(AUDIO_PLL_OUTPUT_RE, route_text):
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
        "synthesis_cells": synth["synthesis_cells"],
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }


def check_firmware_outside_sockets(mapping: dict) -> None:
    width = 7605
    for block in mapping["blocks"]:
        for word in block["word_bits"]:
            for bit in word:
                x, y = bit % width, bit // width
                for socket in c64_slots.SOCKETS:
                    x0, y0, x1, y1 = socket.cram
                    if x0 <= x < x1 and y0 <= y < y1:
                        raise BuildError(f"firmware lane {block['bel']} writes CRAM in slot {socket.slot}")


def _manifest(record: bytes, evidence: dict, repository: str, revision: str,
              tools: Mapping[str, str]) -> bytes:
    rbf = evidence["rbf"]
    record_fields = json.loads(record)
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    required = [
        "fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports",
        "fes.audio.pcm-s16-stereo-48k", "fes.media.c64-disk",
    ]
    fields = {
        "format": 3,
        "core": {
            "id": "fes.c64",
            "name": "FES Commodore 64",
            "description": "Commodore 64 pathfinder with a linked 16 KiB firmware image, "
                           "a read-only 1541 and two cartridge sockets",
            "version": "0.1.0",
        },
        "target": {"platform": "de10_nano", "device": TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{"id": interface, "major": 1, "minor": 0, "required": True} for interface in required] +
                      [{"id": c64_slots.INTERFACE, "major": 1, "minor": 0, "required": False}],
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
          identity_version: int = 2, gpu_device: int = 0) -> Path:
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES Commodore 64 package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    authenticated = _authenticate_c64_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    database_root = authenticated["mistral"].path.parents[2] / "src/mistral"
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    invocation = FunctionalInvocation(authenticated, gpu_device)
    record = create_build_record(root, repository, revision, identities,
                                 identity_version=identity_version, execution=invocation.inputs)
    output = _prepare_output(root, relative=OUTPUT_RELATIVE, build_outputs=BUILD_OUTPUTS)
    _write_atomic(output / "build-inputs.json", record)
    _write_atomic(output / "socket.qsf", socket_qsf((root / QSF).read_text()).encode())
    try:
        build_id = build_identity(record)
        yosys, _route = build_commands(root, output, build_id, {
            name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")})
        _run_tool(yosys, root, output / "yosys.log", env=invocation.env,
                  audit_source_root=root, output_relative=OUTPUT_RELATIVE)
        if not (output / "synth.json").is_file():
            raise BuildError("Yosys did not produce synthesis evidence")
        clock_read_only_memories(output / "synth.json")
        try:
            winner = route_after_synth(
                nextpnr=authenticated["nextpnr-mistral"].path,
                fixture=output / "synth.json", dest=output, device=TARGET,
                qsf=output / "socket.qsf", sdc=root / SDC, freq="74.25",
                seeds=PLACER_SEEDS, weights=(PLACER_WEIGHT,),
                critexp=PLACER_CRITICALITY_EXPONENT, budget=len(PLACER_SEEDS),
                mode="first-pass", extra=("--router", ROUTER),
                required=PLACER_QOR_CLOCKS, gpu_devices=(gpu_device,),
                env=invocation.env, audit_source_root=root,
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output)
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
        final_tools = _authenticate_c64_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError("authenticated tool identity changed during build")
        if _require_clean_source(root, identity_version=identity_version) != (repository, revision):
            raise BuildError("source identity changed during build")
        invocation.verify()
        if rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError("ROM database changed during build")
        if create_build_record(root, repository, revision, identities,
                               identity_version=identity_version, execution=invocation.inputs) != record:
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


def synth(root: Path = ROOT, *, cache_root: Path | None = None) -> dict:
    """Run Yosys only. Does not require a clean tree and does not seal a package."""
    root = Path(root).resolve()
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
    authenticated = _authenticate_c64_tools(root, cache_root=cache_root)
    output = _prepare_output(root, relative=OUTPUT_RELATIVE, build_outputs=BUILD_OUTPUTS)
    yosys, _route = build_commands(root, output, "0" * 32, {
        name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")})
    _run_tool(yosys, root, output / "yosys.log", output_relative=OUTPUT_RELATIVE)
    if not (output / "synth.json").is_file():
        raise BuildError("Yosys did not produce synthesis evidence")
    evidence = validate_synth_evidence(output)
    evidence.update({"build_id": "0" * 32, "sealed": False,
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
    parser.add_argument("--synth-only", action="store_true",
                        help="run Yosys only; skip the clean-tree seal and nextpnr")
    arguments = parser.parse_args(argv)
    try:
        if arguments.synth_only:
            cells = synth(arguments.root, cache_root=arguments.cache_root)["synthesis_cells"]
            print(f"synth-only MISTRAL_M10K={cells.get('MISTRAL_M10K', 0)} "
                  f"MISTRAL_M10K_TDP={cells.get('MISTRAL_M10K_TDP', 0)} "
                  f"MISTRAL_FF={cells.get('MISTRAL_FF', 0)}")
            return 0
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root,
                    identity_version=arguments.identity_version, gpu_device=arguments.gpu_device))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-c64-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
