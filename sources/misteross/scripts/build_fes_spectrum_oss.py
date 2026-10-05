#!/usr/bin/env python3
"""Build and seal the OSS HIP nextpnr/Mistral FES ZX Spectrum package.

The package is format 3: the sixteen blank 1024x10 firmware lanes (column 5,
rows 32-47) are described by a sealed ROM map for the 16,384-byte
`spectrum-firmware` image that FogCast links at download time. The shell
reserves the four named socket rectangles of `fes.spectrum-bus.sockets/1`
and pins each socket's boundary flip-flops and their verified route-throughs; no other shell cell may sit in a
socket and no firmware destination may fall in a socket's CRAM rectangle.
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

from scripts import spectrum_slots, rom_map, coleco_expansion
from scripts.compiler_read_audit import guard_functional_source
from scripts.core_package import MAX_PAYLOAD_SIZE, encode_manifest
from scripts.export_core_package import (
    build_identity, encode_build_record, export_package, functional_record_fields,
)
from scripts.fes_build_common import (
    BuildError, _authenticate_tools, _cell_counts, _i2c_evidence, _prepare_output,
    _read_json, _regular_input, _require_gpu_backend, _run_tool, _sha256, _write_atomic,
    validate_timing_resources,
)
from scripts.fes_build_common import _require_clean_source as require_clean_source
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.search_placer_qor import SearchError, route_after_synth

ROOT = Path(__file__).resolve().parents[1]
TARGET = "5CSEBA6U23I7"
TOP = "top"
ROUTER = "gpu"
RECIPE = "scripts/build_fes_spectrum_oss.py"
OUTPUT_RELATIVE = Path("build/fes-spectrum-oss")
FAST_OUTPUT_RELATIVE = Path("build/fes-spectrum-fast-oss")
SPECTRUM_TOOLCHAIN_LOCK = "toolchains/spectrum.lock"
SPECTRUM_TOOLCHAIN_ROOT = "build/toolchain/fes-spectrum"
SPECTRUM_GPU_BACKEND = "hip"
SPECTRUM_GPU_ROUTER = "HIP"
SPECTRUM_GPU_ARCHITECTURES = "gfx1100;gfx1201"
SPECTRUM_TOOLCHAIN_CONFIGURATION = (
    f"gpu-router={SPECTRUM_GPU_ROUTER}; hip-architectures={SPECTRUM_GPU_ARCHITECTURES}"
)
SPECTRUM_TOOL_COMMITS = {
    "mistral": "7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039",
    "nextpnr": "0259c6dc1c46dd46fe79f3923a17ad36d2513421",
    "yosys": "e2d425dee148cc60c50f4e9b354a10d90eab15f4",
}
# First passing route wins; the order is part of the build identity. This is
# the same first-pass order as the Apple II shell until a Spectrum seal
# records its own.
PLACER_SEEDS = (5, 4, 2, 1, 3, 6, 7, 8, 9, 10)
PLACER_WEIGHT = 2000
PLACER_CRITICALITY_EXPONENT = 5
PLACER_QOR_CLOCKS = ((None, 52.224), (None, 74.25), (None, 12.288))
ROM_DATABASE_SHA256 = {
    "data/m10k-mux.txt": "22bb99e4b9f2bbe6b8dc7122d8ebf212a8b5610d46e59ce72d5b58b4b05631fe",
    "libmistral/cvd-sx120f.cc": "e3be2df0ff77a628a7b31447897488bfb2bb70fbaa0f1ef550bc36c32094faf7",
    "libmistral/cyclonev.h": "48c0acadd2d1dc47398d7e7ab8ad840e98cb3fda489c3197eace6f3ba59e6f21",
}
FIRMWARE_LANE_ROWS = tuple(range(32, 48))
FIRMWARE_ID = "spectrum-firmware"
FIRMWARE_BYTES = 16384
ABI_DEFINITION = "cores/fes-common/generated/fes_computer.vh"
QSF = "cores/fes-spectrum/constraints-oss.qsf"
SDC = "cores/fes-spectrum/clocks-oss.sdc"
RTL_INCLUDES = (
    "cores/fes-spectrum/rtl/spectrum_bus.vh",
)
RTL_SOURCES = (
    "cores/fes-spectrum/rtl/top.v",
    "cores/fes-spectrum/rtl/spectrum_system_pll.v",
    "cores/fes-common/rtl/pixel_pll.v",
    "cores/fes-common/rtl/fes_computer_mailbox.v",
    "cores/fes-common/rtl/fes_audio_output.v",
    "cores/fes-common/rtl/fes_audio_i2s.v",
    "cores/fes-spectrum/rtl/spectrum_machine.sv",
    "cores/fes-spectrum/rtl/spectrum_ram.v",
    "cores/fes-spectrum/rtl/spectrum_rom.v",
    "cores/fes-spectrum/rtl/spectrum_video.v",
    "cores/fes-spectrum/rtl/spectrum_tape.v",
    "cores/fes-spectrum/rtl/spectrum_keyboard.v",
    "cores/fes-spectrum/rtl/spectrum_audio.v",
    "cores/fes-spectrum/rtl/spectrum_slot_sockets.v",
    "cores/fes-spectrum/rtl/spectrum_fast_bus.sv",
    "cores/fes-spectrum/rtl/spectrum_fast_audio.sv",
    "cores/fes-common/rtl/z80/fes_z80_alu.sv",
    "cores/fes-common/rtl/z80/fes_z80_engine.sv",
    "cores/fes-common/rtl/z80/fes_z80_bus.sv",
    "cores/fes-common/rtl/z80/fes_z80_nmos.sv",
    "cores/fes-common/rtl/z80/fes_z80_fast.sv",
)
PINNED_INPUTS = (
    RECIPE, "scripts/spectrum_slots.py", "scripts/coleco_expansion.py", "scripts/compiler_read_audit.py",
    "scripts/source_repository.py", "scripts/fes_build_common.py", "scripts/rom_map.py",
    "scripts/cyclonev_rbf.py", "scripts/search_placer_qor.py",
    ABI_DEFINITION, SPECTRUM_TOOLCHAIN_LOCK, QSF, SDC, *RTL_INCLUDES, *RTL_SOURCES,
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


def _cpu_parameters(cpu: str) -> tuple[Path, float, int]:
    if cpu == "nmos":
        return OUTPUT_RELATIVE, 52.224, 2
    if cpu == "fast":
        return FAST_OUTPUT_RELATIVE, 56.0, 2
    raise BuildError("CPU must be nmos or fast")


def _authenticate_spectrum_tools(root: Path, cache_root: Path | None = None):
    return _authenticate_tools(
        root,
        lock_path=root / SPECTRUM_TOOLCHAIN_LOCK,
        toolchain_root=root / SPECTRUM_TOOLCHAIN_ROOT,
        expected_commits=SPECTRUM_TOOL_COMMITS,
        expected_configuration={"nextpnr": SPECTRUM_TOOLCHAIN_CONFIGURATION},
        gpu_router=SPECTRUM_GPU_ROUTER,
        hip_architectures=SPECTRUM_GPU_ARCHITECTURES,
        cache_root=cache_root,
    )


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
    cpu: str = "nmos",
) -> bytes:
    if identity_version != 2:
        raise BuildError("unsupported build identity version")
    output_relative, sys_mhz, pll_count = _cpu_parameters(cpu)
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
            "gpu_backend": SPECTRUM_GPU_BACKEND,
            "gpu_architectures": SPECTRUM_GPU_ARCHITECTURES,
            "sys_clock_hz": int(sys_mhz * 1_000_000),
            "cpu": cpu,
            "cpu_implementation": "fes_z80_nmos" if cpu == "nmos" else "fes_z80_fast",
            "peripheral_clock_hz": 3_500_000,
            "pll_count": pll_count,
            "output_relative": output_relative.as_posix(),
            "pixel_clock_hz": 74_250_000,
            "audio_clock_hz": 12_288_000 if cpu == "nmos" else 56_000_000,
            "audio_serializer": "fes_audio_output" if cpu == "nmos" else "spectrum_fast_audio",
            "audio_mclk_average_hz": 12_288_000,
            "audio_mclk_source": "pll" if cpu == "nmos" else "system-clock-rational-enable",
            "audio_mclk_toggle_numerator": 1 if cpu == "nmos" else 384,
            "audio_mclk_toggle_denominator": 1 if cpu == "nmos" else 875,
            "audio_mclk_half_period_min_system_ticks": 0 if cpu == "nmos" else 2,
            "audio_mclk_half_period_max_system_ticks": 0 if cpu == "nmos" else 3,
            "audio_sample_hz": 48_000,
            "reference_clock_hz": 50_000_000,
            "seed": PLACER_SEEDS[0],
            "seed_order": ",".join(str(seed) for seed in PLACER_SEEDS),
            "placer_heap_timingweight": PLACER_WEIGHT,
            "placer_heap_critexp": PLACER_CRITICALITY_EXPONENT,
            "toolchain_lock": SPECTRUM_TOOLCHAIN_LOCK,
            "toolchain_lock_sha256": _sha256(_regular_input(root, SPECTRUM_TOOLCHAIN_LOCK)),
            "package_format": 3,
            "rom_id": FIRMWARE_ID,
            "rom_role": "firmware",
            "rom_source_size": FIRMWARE_BYTES,
            "rom_encoding": "m10k-1024x10-v1",
            "rom_read_mode": "registered",
            "rom_read_latency_system_ticks": 2,
            "rom_database_sha256": json.dumps(ROM_DATABASE_SHA256, sort_keys=True, separators=(",", ":")),
            "expansion_layout": spectrum_slots.LAYOUT,
            "expansion_sockets": ",".join(s.placement for s in spectrum_slots.SOCKETS),
        },
    }
    fields = functional_record_fields(root, fields, source_roots_for_inputs(PINNED_INPUTS),
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
                   tools: Mapping[str, Path], seed: int = PLACER_SEEDS[0], *, cpu: str = "nmos") -> tuple[tuple[str, ...], tuple[str, ...]]:
    output_relative, _sys_mhz, _pll_count = _cpu_parameters(cpu)
    if output != root / output_relative:
        raise BuildError(f"FES ZX Spectrum OSS output must be {root / output_relative}")
    if HEX32_RE.fullmatch(build_id) is None:
        raise BuildError("build ID must be 32 lowercase hexadecimal characters")
    if set(tools) != {"yosys", "nextpnr-mistral"}:
        raise BuildError("build commands require authenticated Yosys and nextpnr-mistral paths")
    program = (
        "read_verilog -sv -I cores/fes-spectrum/rtl -I cores/fes-common/generated "
        f"{' '.join(RTL_SOURCES)}; "
        f"chparam -set BUILD_ID 128'h{build_id} -set FAST_CPU {int(cpu == 'fast')} {TOP}; "
        f"synth_intel_alm -nolutram -nodsp -top {TOP}; stat; "
        f"write_json {output_relative.as_posix()}/synth.json"
    )
    yosys = (str(tools["yosys"]), "-p", program)
    route = (
        str(tools["nextpnr-mistral"]), "--json", f"{output_relative.as_posix()}/synth.json",
        "--device", TARGET, "--qsf", f"{output_relative.as_posix()}/socket.qsf",
        "--sdc", SDC, "--freq", "74.25", "--seed", str(seed),
        "--placer-heap-timingweight", str(PLACER_WEIGHT),
        "--placer-heap-critexp", str(PLACER_CRITICALITY_EXPONENT),
        "--router", ROUTER, "--timing-allow-fail",
        "--rbf", f"{output_relative.as_posix()}/core.rbf", "--compress-rbf",
        "--write", f"{output_relative.as_posix()}/routed.json",
        "--report", f"{output_relative.as_posix()}/timing.json", "--detailed-timing-report",
    )
    return yosys, route


def validate_routed_shell(routed: dict) -> dict:
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


def validate_synth_evidence(output: Path, *, cpu: str = "nmos") -> dict:
    _output, _sys_mhz, pll_count = _cpu_parameters(cpu)
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    _i2c_evidence(synthesis, "synthesized")
    counts = _cell_counts(synthesis)
    for name, expected in (REQUIRED_RESOURCES | {"altera_pll": pll_count}).items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    # Sixteen firmware lanes are explicit M10Ks. The 64 KiB CPU/video RAM and
    # the 64 KiB tape image are 1024x8 TDP blocks: 64 + 64.
    if counts.get("MISTRAL_M10K", 0) != 16 or counts.get("MISTRAL_M10K_TDP", 0) != 128:
        raise BuildError(
            "synthesis must keep 16 firmware M10K lanes and 128 RAM/tape TDP blocks, "
            f"got M10K={counts.get('MISTRAL_M10K', 0)} "
            f"TDP={counts.get('MISTRAL_M10K_TDP', 0)}")
    validate_firmware_ports(synthesis["modules"][TOP]["cells"])
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0):
            raise BuildError(f"forbidden synthesis cell {name} is in use")
    return {"status": "pass", "synthesis_cells": {name: counts[name] for name in sorted(counts)}}


def validate_build_evidence(output: Path, *, cpu: str = "nmos") -> dict:
    _output, sys_mhz, _pll_count = _cpu_parameters(cpu)
    routed = _read_json(output / "routed.json", "routed design")
    if not isinstance(routed.get("modules"), dict) or not isinstance(routed["modules"].get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    synth = validate_synth_evidence(output, cpu=cpu)
    _i2c_evidence(routed, "routed")
    sockets = validate_routed_shell(routed)
    route_text = (output / "nextpnr.log").read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    gpu_backend = _require_gpu_backend(route_text)
    if f"50 MHz -> {sys_mhz:g} MHz" not in route_text:
        raise BuildError(f"route log does not contain the 50-to-{sys_mhz:g} MHz system PLL")
    if cpu == "nmos" and not re.search(
            r"PLL 'system_clock.faithful.pll': second output 12\.288 MHz", route_text):
        raise BuildError("route log must prove the selected 12.288 MHz audio PLL")
    timing = _read_json(output / "timing.json", "timing report")
    clocks = (("system", sys_mhz), ("pixel", 74.25))
    if cpu == "nmos":
        clocks += (("audio", 12.288),)
    rows = {label: _frequency_row(timing.get("fmax"), mhz, label) for label, mhz in clocks}
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
        "audio": {"sample_average_hz": 48_000, "mclk_average_hz": 12_288_000,
                  "clock_domain_mhz": 12.288 if cpu == "nmos" else 56.0,
                  "mclk_source": "pll" if cpu == "nmos" else "system-clock-rational-enable"},
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
                for socket in spectrum_slots.SOCKETS:
                    x0, y0, x1, y1 = socket.cram
                    if x0 <= x < x1 and y0 <= y < y1:
                        raise BuildError(f"firmware lane {block['bel']} writes CRAM in slot {socket.slot}")


def _manifest(record: bytes, evidence: dict, repository: str, revision: str,
              tools: Mapping[str, str]) -> bytes:
    rbf = evidence["rbf"]
    record_fields = json.loads(record)
    cpu = record_fields["parameters"]["cpu"]
    _cpu_parameters(cpu)
    toolchain = "; ".join(f"{name} {tools[name]}" for name in sorted(tools))
    required = [
        "fes.video.fixed-720p60", "fes.keyboard.hid", "fes.gamepad.ports",
        "fes.audio.pcm-s16-stereo-48k", "fes.media.spectrum-tape",
    ]
    fields = {
        "format": 3,
        "core": {
            "id": "fes.spectrum",
            "name": "FES ZX Spectrum",
            "description": ("ZX Spectrum 48K with native NMOS Z80" if cpu == "nmos" else
                            "Development ZX Spectrum 48K with documented-only Z80 at 56 MHz") +
                           ", linked 16 KiB firmware, .tap cassette and four edge sockets",
            "version": "0.2.0",
        },
        "target": {"platform": "de10_nano", "device": TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", "size": rbf["size"], "sha256": rbf["sha256"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 0},
        "interfaces": [{"id": interface, "major": 1, "minor": 0, "required": True} for interface in required] +
                      [{"id": spectrum_slots.INTERFACE, "major": 1, "minor": 0, "required": False}],
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
          identity_version: int = 2, gpu_device: int = 0, cpu: str = "nmos") -> Path:
    output_relative, sys_mhz, _pll_count = _cpu_parameters(cpu)
    root = Path(root).resolve()
    package_store = (root / "build/packages" if package_store is None else Path(package_store)).resolve()
    if package_store != root / "build/packages":
        raise BuildError(f"FES ZX Spectrum package store must be {root / 'build/packages'}")
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    authenticated = _authenticate_spectrum_tools(root, cache_root=cache_root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    database_root = authenticated["mistral"].path.parents[2] / "src/mistral"
    database = rom_map.read_database(database_root, ROM_DATABASE_SHA256)
    invocation = FunctionalInvocation(authenticated, gpu_device)
    record = create_build_record(root, repository, revision, identities,
                                 identity_version=identity_version, execution=invocation.inputs, cpu=cpu)
    output = _prepare_output(root, relative=output_relative, build_outputs=BUILD_OUTPUTS)
    _write_atomic(output / "build-inputs.json", record)
    _write_atomic(output / "socket.qsf", socket_qsf((root / QSF).read_text()).encode())
    try:
        build_id = build_identity(record)
        yosys, _route = build_commands(root, output, build_id, {
            name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, cpu=cpu)
        _run_tool(yosys, root, output / "yosys.log", env=invocation.env,
                  audit_source_root=root, output_relative=output_relative)
        if not (output / "synth.json").is_file():
            raise BuildError("Yosys did not produce synthesis evidence")
        try:
            winner = route_after_synth(
                nextpnr=authenticated["nextpnr-mistral"].path,
                fixture=output / "synth.json", dest=output, device=TARGET,
                qsf=output / "socket.qsf", sdc=root / SDC, freq="74.25",
                seeds=PLACER_SEEDS, weights=(PLACER_WEIGHT,),
                critexp=PLACER_CRITICALITY_EXPONENT, budget=len(PLACER_SEEDS),
                mode="first-pass", extra=("--router", ROUTER),
                required=((None, sys_mhz), (None, 74.25)) + (((None, 12.288),) if cpu == "nmos" else ()),
                gpu_devices=(gpu_device,),
                env=invocation.env, audit_source_root=root,
            )
        except SearchError as exc:
            raise BuildError(str(exc)) from exc
        evidence = validate_build_evidence(output, cpu=cpu)
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
            "build_id": build_id, "cpu": cpu, "device": TARGET, "top": TOP, "tools": identities,
            "inputs": {relative: _sha256(root / relative) for relative in sorted(PINNED_INPUTS)},
        })
        _write_atomic(output / "build-summary.json",
                      (json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode())
        manifest = _manifest(record, evidence, repository, revision, identities)
        _write_atomic(output / "manifest.toml", manifest)
        final_tools = _authenticate_spectrum_tools(root, cache_root=cache_root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise BuildError("authenticated tool identity changed during build")
        if _require_clean_source(root, identity_version=identity_version) != (repository, revision):
            raise BuildError("source identity changed during build")
        invocation.verify()
        if rom_map.read_database(database_root, ROM_DATABASE_SHA256) != database:
            raise BuildError("ROM database changed during build")
        if create_build_record(root, repository, revision, identities,
                               identity_version=identity_version, execution=invocation.inputs, cpu=cpu) != record:
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


def synth(root: Path = ROOT, *, cache_root: Path | None = None, cpu: str = "nmos") -> dict:
    """Run Yosys only. Does not require a clean tree and does not seal a package."""
    output_relative, _sys_mhz, _pll_count = _cpu_parameters(cpu)
    root = Path(root).resolve()
    for relative in PINNED_INPUTS:
        _regular_input(root, relative)
    authenticated = _authenticate_spectrum_tools(root, cache_root=cache_root)
    output = _prepare_output(root, relative=output_relative, build_outputs=BUILD_OUTPUTS)
    yosys, _route = build_commands(root, output, "0" * 32, {
        name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")}, cpu=cpu)
    _run_tool(yosys, root, output / "yosys.log", output_relative=output_relative)
    if not (output / "synth.json").is_file():
        raise BuildError("Yosys did not produce synthesis evidence")
    evidence = validate_synth_evidence(output, cpu=cpu)
    evidence.update({"build_id": "0" * 32, "sealed": False, "cpu": cpu,
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
    parser.add_argument("--cpu", choices=("nmos", "fast"), default="nmos",
                        help="native NMOS default or documented-only 56 MHz development variant")
    parser.add_argument("--synth-only", action="store_true",
                        help="run Yosys only; skip the clean-tree seal and nextpnr")
    arguments = parser.parse_args(argv)
    try:
        if arguments.synth_only:
            cells = synth(arguments.root, cache_root=arguments.cache_root, cpu=arguments.cpu)["synthesis_cells"]
            print(f"synth-only MISTRAL_M10K={cells.get('MISTRAL_M10K', 0)} "
                  f"MISTRAL_M10K_TDP={cells.get('MISTRAL_M10K_TDP', 0)} "
                  f"MISTRAL_FF={cells.get('MISTRAL_FF', 0)}")
            return 0
        print(build(arguments.root, arguments.package_output, cache_root=arguments.cache_root,
                    identity_version=arguments.identity_version, gpu_device=arguments.gpu_device, cpu=arguments.cpu))
    except (BuildError, OSError, ValueError) as exc:
        print(f"build-fes-spectrum-oss: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
