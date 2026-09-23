"""Fixed DE10-Nano video/audio evidence with caller-owned resource expectations."""
from __future__ import annotations

import math
import re
from pathlib import Path
from scripts.core_package import MAX_PAYLOAD_SIZE
from scripts.fes_build_common import (
    BuildError,
    TOP,
    _cell_counts,
    _i2c_evidence,
    _read_json,
    _regular_input,
    _require_gpu_backend,
    _sha256,
)

SDC = "boards/de10nano/clocks.sdc"
REFERENCE_SDC_BYTES = (
    b"# 50 MHz DE10-Nano input clock.\n"
    b"create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]\n"
)
PLL_PARAMETERS = {
    "duty_cycle0": "00000000000000000000000000110010",
    "fractional_vco_multiplier": "true",
    "number_of_clocks": "00000000000000000000000000000001",
    "operation_mode": "direct",
    "output_clock_frequency0": "74.25 MHz",
    "phase_shift0": "0 ps",
    "reference_clock_frequency": "50.0 MHz",
}
REFERENCE_CONSTRAINT_LOG = "Info: constraining clock net 'FPGA_CLK1_50' to 50.00 MHz"
PLL_ROUTE_LOG = (
    "Info: PLL 'video_clock.pll': 50 MHz -> 74.25 MHz, direct, "
    "M=8 N=1 C6=6, bel altera_pll.0.14.0"
)


def _frequency_rows(fmax: object, expected: float, label: str, *, clock_name: str | None = None) -> tuple[str, float, float]:
    if not isinstance(fmax, dict):
        raise BuildError("timing report has no structured fmax data")
    matches: list[tuple[str, float, float]] = []
    for name, fields in fmax.items():
        if not isinstance(name, str) or not isinstance(fields, dict):
            continue
        if clock_name is not None and name != clock_name:
            continue
        constraint = fields.get("constraint")
        achieved = fields.get("achieved")
        if (
            isinstance(constraint, bool)
            or not isinstance(constraint, (int, float))
            or isinstance(achieved, bool)
            or not isinstance(achieved, (int, float))
        ):
            continue
        if not math.isfinite(float(constraint)) or not math.isfinite(float(achieved)):
            raise BuildError(f"{label} timing frequencies must be finite")
        tolerance = max(1e-6, expected * 5e-5)
        if abs(float(constraint) - expected) <= tolerance:
            matches.append((name, float(constraint), float(achieved)))
    if len(matches) != 1:
        raise BuildError(f"timing report must contain exactly one {label} {expected:g} MHz constraint")
    name, constraint, achieved = matches[0]
    if achieved < constraint:
        raise BuildError(
            f"{label} timing achieved {achieved:g} MHz, below reported constraint {constraint!r} MHz"
        )
    return name, constraint, achieved


def _pll_cell_parameters(design: dict, label: str, *, audio: bool = False,
                         memory_pll_parameters: dict[str, str] | None = None) -> None:
    modules = design.get("modules")
    top = modules.get(TOP) if isinstance(modules, dict) else None
    cells = top.get("cells") if isinstance(top, dict) else None
    matches = [
        cell
        for cell in cells.values()
        if isinstance(cell, dict) and cell.get("type") == "altera_pll"
    ] if isinstance(cells, dict) else []
    expected = [PLL_PARAMETERS]
    if audio:
        expected.append({**PLL_PARAMETERS, "output_clock_frequency0": "12.288 MHz"})
    if memory_pll_parameters is not None:
        expected.append(memory_pll_parameters)
    frequency = lambda item: item.get("output_clock_frequency0", "")
    if sorted((cell.get("parameters", {}) for cell in matches), key=frequency) != sorted(expected, key=frequency):
        raise BuildError(f"{label} PLL parameters do not match the fixed 50-to-74.25 MHz profile")


def _reference_clock_evidence(source_root: Path, route_text: str, *, audio: bool = False,
                              memory_pll: bool = False) -> dict[str, object]:
    sdc = _regular_input(source_root, SDC)
    if sdc.read_bytes() != REFERENCE_SDC_BYTES:
        raise BuildError("tracked SDC does not contain the exact FPGA_CLK1_50 20.000 ns constraint")
    if route_text.count(REFERENCE_CONSTRAINT_LOG) != 1:
        raise BuildError("route log must apply the FPGA_CLK1_50 50.00 MHz constraint exactly once")
    if memory_pll:
        video_routes = re.findall(
            r"Info: PLL 'video_clock\.pll': 50 MHz -> 74\.25 MHz, direct, "
            r"M=8 N=1 C6=6, bel altera_pll\.[0-9.]+", route_text,
        )
        if len(video_routes) != 1:
            raise BuildError("route log does not contain the expected fractional video PLL mapping")
    elif route_text.count(PLL_ROUTE_LOG) != 1:
        raise BuildError("route log does not contain the expected fixed fractional PLL mapping")
    if audio and len(re.findall(r"Info: PLL 'audio_clock.pll': 50 MHz -> 12\.288 MHz, direct, .*bel altera_pll\.[0-9.]+", route_text)) != 1:
        raise BuildError("route log must contain the exact 12.288 MHz audio PLL mapping")
    return {
        "clock": "FPGA_CLK1_50",
        "constraint_mhz": 50.0,
        "evidence": "boards/de10nano/clocks.sdc and routed PLL",
        "requested_mhz": 50.0,
        "status": "pass",
    }


def validate_build_evidence(output: Path, source_root: Path, *, ordinary_resources, required_resources, forbidden_resources, required_zero_resources, audio: bool = False, memory_clock_mhz: float | None = None, capture_clock_mhz: float | None = None, memory_pll_parameters: dict[str, str] | None = None) -> dict:
    output = Path(output)
    source_root = Path(source_root)
    synthesis = _read_json(output / "synth.json", "synthesis evidence")
    routed = _read_json(output / "routed.json", "routed design")
    routed_modules = routed.get("modules")
    if not isinstance(routed_modules, dict) or not isinstance(routed_modules.get(TOP), dict):
        raise BuildError("routed design does not contain the top module")
    if (capture_clock_mhz is None) != (memory_pll_parameters is None):
        raise BuildError("capture clock and memory PLL evidence must be configured together")
    _pll_cell_parameters(synthesis, "synthesized", audio=audio,
                         memory_pll_parameters=memory_pll_parameters)
    _pll_cell_parameters(routed, "routed", audio=audio,
                         memory_pll_parameters=memory_pll_parameters)
    _i2c_evidence(synthesis, "synthesized")
    _i2c_evidence(routed, "routed")
    counts = _cell_counts(synthesis)
    required_resources = {**required_resources, "altera_pll": 1 + int(audio) + int(memory_pll_parameters is not None)}
    for name, expected in required_resources.items():
        if counts.get(name, 0) != expected:
            raise BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    for name in forbidden_resources:
        if counts.get(name, 0) != 0:
            raise BuildError(f"forbidden synthesis cell {name} is in use")

    route_log = output / "nextpnr.log"
    if route_log.is_symlink() or not route_log.is_file():
        raise BuildError(f"missing route log: {route_log}")
    route_text = route_log.read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise BuildError("route log does not prove a complete routed design")
    gpu_backend = _require_gpu_backend(route_text)
    reference = _reference_clock_evidence(source_root, route_text, audio=audio,
                                          memory_pll=memory_pll_parameters is not None)
    if memory_pll_parameters is not None:
        expected_route = f"PLL 'ram_clock.pll': 50 MHz -> {memory_clock_mhz:g} MHz, direct,"
        if route_text.count(expected_route) != 1:
            raise BuildError("route log lacks the selected memory PLL mapping")

    timing = _read_json(output / "timing.json", "timing report")
    fmax = timing.get("fmax")
    expected_domains = (2 if audio else 1) + (0 if memory_clock_mhz is None else 1)
    if capture_clock_mhz is not None:
        expected_domains += 2  # independent 50 MHz HPS, memory and capture clocks
    if not isinstance(fmax, dict) or len(fmax) != expected_domains:
        if memory_clock_mhz is not None:
            raise BuildError("timing report must contain the pixel, memory, and audio sequential domains" if audio
                             else "timing report must contain the pixel and memory sequential domains")
        raise BuildError("timing report must contain exactly the pixel and audio sequential domains" if audio
                         else "timing report must contain the single pixel sequential domain")
    pixel = _frequency_rows(fmax, 74.25, "pixel clock")
    audio_timing = _frequency_rows(fmax, 12.288, "audio clock") if audio else None
    memory_timing = _frequency_rows(
        fmax, memory_clock_mhz, "memory clock",
        clock_name="ram_clock.clocks[0]" if capture_clock_mhz is not None else None,
    ) if memory_clock_mhz is not None else None
    capture_timing = _frequency_rows(
        fmax, capture_clock_mhz, "capture clock", clock_name="ram_clock.clocks[1]"
    ) if capture_clock_mhz is not None else None
    reference_fabric = _frequency_rows(
        fmax, 50.0, "50 MHz fabric clock", clock_name="hps_ddr.clk"
    ) if capture_clock_mhz is not None else None
    utilization = timing.get("utilization")
    if not isinstance(utilization, dict):
        raise BuildError("timing report has no structured utilization data")
    resources: dict[str, dict[str, int]] = {}
    known = (
        ordinary_resources
        | set(required_resources)
        | forbidden_resources
        | required_zero_resources
    )
    unknown = sorted(set(utilization) - known)
    if unknown:
        raise BuildError("timing report contains unknown resources: " + ", ".join(unknown))
    for name, fields in sorted(utilization.items()):
        if not isinstance(fields, dict):
            raise BuildError(f"malformed resource evidence: {name}")
        used = fields.get("used")
        available = fields.get("available")
        if (
            isinstance(used, bool)
            or not isinstance(used, int)
            or used < 0
            or isinstance(available, bool)
            or not isinstance(available, int)
            or available < 0
        ):
            raise BuildError(f"malformed resource counts: {name}")
        resources[name] = {"available": available, "used": used}
    for name, expected in required_resources.items():
        if name not in resources or resources[name]["used"] != expected:
            actual = "missing" if name not in resources else str(resources[name]["used"])
            raise BuildError(f"resource {name} must be exactly {expected}, got {actual}")
    for name in required_zero_resources:
        if name not in resources or resources[name]["used"] != 0:
            actual = "missing" if name not in resources else str(resources[name]["used"])
            raise BuildError(f"resource {name} must be exactly 0, got {actual}")
    for name in forbidden_resources:
        if name in resources and resources[name]["used"] != 0:
            raise BuildError(f"forbidden resource {name} is in use")

    rbf = output / "core.rbf"
    if rbf.is_symlink() or not rbf.is_file() or not 1 <= rbf.stat().st_size <= MAX_PAYLOAD_SIZE:
        raise BuildError(f"RBF must be a nonempty bounded regular file: {rbf}")
    return {
        "status": "pass",
        "route": {"status": "pass", "unrouted": False, "gpu_backend": gpu_backend},
        "timing": {
            **({"audio": {"clock": audio_timing[0], "constraint_mhz": audio_timing[1],
                           "requested_mhz": 12.288, "achieved_mhz": audio_timing[2],
                           "status": "pass"}} if audio_timing else {}),
            **({"memory": {"clock": memory_timing[0], "constraint_mhz": memory_timing[1],
                           "requested_mhz": memory_clock_mhz, "achieved_mhz": memory_timing[2],
                           "status": "pass"}} if memory_timing else {}),
            **({"capture": {"clock": capture_timing[0], "constraint_mhz": capture_timing[1],
                            "requested_mhz": capture_clock_mhz, "achieved_mhz": capture_timing[2],
                            "status": "pass"}} if capture_timing else {}),
            **({"reference_fabric": {"clock": reference_fabric[0],
                                     "constraint_mhz": reference_fabric[1],
                                     "achieved_mhz": reference_fabric[2],
                                     "status": "pass"}} if reference_fabric else {}),
            "pixel": {
                "clock": pixel[0],
                "constraint_mhz": pixel[1],
                "requested_mhz": 74.25,
                "achieved_mhz": pixel[2],
                "status": "pass",
            },
            "reference": reference,
            "status": "pass",
        },
        "resources": resources,
        "synthesis_cells": {name: counts[name] for name in sorted(counts)},
        "rbf": {"sha256": _sha256(rbf), "size": rbf.stat().st_size},
    }
