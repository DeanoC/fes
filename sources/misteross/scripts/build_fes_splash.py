#!/usr/bin/env python3
"""Produce the board-firmware splash RBF on the generic OSS lane.

This is not a format-2 play package. The sealed outputs are the RBF plus
provenance for FES native-inputs (`splash_rbf` / `idle_rbf`) to pin later.
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_pong as board
from scripts import fes_de10nano_evidence as board_evidence
from scripts.export_core_package import build_identity, encode_build_record

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/build_fes_splash.py"
CONTRACT = "cores/fes-splash/idle-contract.toml"
QSF = "cores/fes-splash/constraints.qsf"
SDC = "boards/de10nano/clocks.sdc"
OUTPUT_RELATIVE = Path("build/fes-splash")
RTL_SOURCES = (
    "cores/fes-pong/rtl/pixel_pll.v",
    "cores/fes-splash/rtl/fes_splash_core.v",
    "cores/fes-splash/rtl/top.v",
)
PINNED_INPUTS = (
    RECIPE,
    CONTRACT,
    "scripts/fes_build_common.py",
    "scripts/fes_de10nano_evidence.py",
    "toolchain.lock",
    QSF,
    SDC,
    *RTL_SOURCES,
)
# FES checkout root is the parent of sources/misteross. Version 1 requires
# those paths to be identical and rejects a normal in-tree splash build.
IDENTITY_VERSION = 2
BUILD_OUTPUTS = (
    "synth.json",
    "routed.json",
    "core.rbf",
    "timing.json",
    "yosys.log",
    "nextpnr.log",
    "build-summary.json",
    "build-inputs.json",
    "native-inputs-snippet.toml",
)
OSS_CONFIGURATION = "gpu-router=OFF; hip-architectures=unused"
REQUIRED_RESOURCES = {
    "altera_pll": 1,
    "cyclonev_hps_interface_peripheral_i2c": 1,
}
FORBIDDEN_RESOURCES = board.FORBIDDEN_RESOURCES | {
    "cyclonev_hps_interface_mpu_general_purpose",
}
USER_IO_MARKERS = (
    "user_io",
    "0x0014",
    "0x002f",
    "hps_interface_mpu_general_purpose",
    "SPI_SS",
    "SPI_MOSI",
)
HARD_NEED = (
    "HARD_NEED: repository-local OSS toolchain (`make toolchain`, GPU-router OFF). "
    "This recipe does not use Quartus and does not seal a format-2 play package. "
    "FES slice 4 pins build/fes-splash/core.rbf after that lane writes a sealed RBF."
)


def output_relative() -> Path:
    return OUTPUT_RELATIVE


def source_forbids_user_io(root: Path) -> None:
    text = "\n".join((root / path).read_text(encoding="utf-8") for path in RTL_SOURCES)
    lowered = text.lower()
    for marker in USER_IO_MARKERS:
        if marker.lower() in lowered:
            raise board.BuildError(f"splash RTL must not speak MiSTer user-io ({marker})")


def create_build_record(root: Path, repository: str, revision: str,
                        identities: dict[str, str]) -> bytes:
    return encode_build_record({
        "format": 1,
        "repository": repository,
        "revision": revision,
        "recipe": RECIPE,
        "recipe_sha256": board._sha256(board._regular_input(root, RECIPE)),
        "abi_definition": CONTRACT,
        "abi_definition_sha256": board._sha256(board._regular_input(root, CONTRACT)),
        "dependencies": {},
        "tools": identities,
        "parameters": {
            "device": board.TARGET,
            "format2_package": False,
            "gpu_router": "OFF",
            "pixel_clock_hz": 74_250_000,
            "pll_fractional_vco_multiplier": True,
            "probe": False,
            "reference_clock_hz": 50_000_000,
            "router": "default",
            "seed": 1,
            "top": "top",
            "user_io": False,
        },
    })


def build_commands(root: Path, tools: dict[str, Path], *, synth_only: bool = False):
    if set(tools) != {"yosys"} | (set() if synth_only else {"nextpnr-mistral"}):
        raise board.BuildError("build commands require authenticated OSS tool paths")
    output = OUTPUT_RELATIVE.as_posix()
    program = (
        f"read_verilog -sv {' '.join(RTL_SOURCES)}; "
        "synth_intel_alm -nobram -nolutram -nodsp -top top; "
        f"stat; write_json {output}/synth.json"
    )
    commands = [(str(tools["yosys"]), "-p", program)]
    if synth_only:
        return tuple(commands)
    commands.append((
        str(tools["nextpnr-mistral"]), "--json", f"{output}/synth.json",
        "--device", board.TARGET, "--qsf", QSF, "--sdc", SDC,
        "--freq", "74.25", "--seed", "1",
        "--rbf", f"{output}/core.rbf", "--compress-rbf",
        "--write", f"{output}/routed.json", "--report", f"{output}/timing.json",
        "--detailed-timing-report",
    ))
    return tuple(commands)


def authenticate_oss_tools(root: Path) -> dict[str, board.AuthenticatedTool]:
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT"):
        raise board.BuildError(
            "splash uses the local OSS lane; unset FES_TOOLCHAIN_CACHE_ROOT"
        )
    try:
        return board._authenticate_tools(
            root,
            expected_configuration={"nextpnr": OSS_CONFIGURATION},
            gpu_router="OFF",
        )
    except board.BuildError as exc:
        raise board.BuildError(f"{exc}; {HARD_NEED}") from exc


def native_inputs_snippet(repository: str, revision: str, evidence: dict | None = None) -> str:
    payload = evidence.get("rbf") if evidence else None
    digest = payload["sha256"] if payload else "SEALED_RBF_SHA256"
    size = payload["size"] if payload else 0
    comment = (
        "# FES slice 4 pin candidate. Filename stays /menu.rbf until a U-Boot reseal.\n"
        "# Stop idle reuses these splash bytes until a second bitstream exists.\n"
        "# IdleRecipe must omit Probe (0x0014) and HPS fb (0x002f).\n"
    )
    if payload is None:
        comment += "# sha256/size are placeholders until make build-fes-splash seals an RBF.\n"
    return (
        f"{comment}"
        "[splash_rbf]\n"
        f"repository = {repository!r}\n"
        f"commit = {revision!r}\n"
        "path = 'build/fes-splash/core.rbf'\n"
        f"sha256 = {digest!r}\n"
        f"size = {size}\n"
        "fat_destination = '/menu.rbf'\n"
        "\n"
        "[idle_rbf]\n"
        f"repository = {repository!r}\n"
        f"commit = {revision!r}\n"
        "path = 'build/fes-splash/core.rbf'\n"
        f"sha256 = {digest!r}\n"
        f"size = {size}\n"
        "install_path = '/usr/share/mister-runtime/idle.rbf'\n"
    )


def validate_build_evidence(output: Path, source_root: Path = ROOT) -> dict:
    output = Path(output)
    source_root = Path(source_root)
    synthesis = board._read_json(output / "synth.json", "synthesis evidence")
    routed = board._read_json(output / "routed.json", "routed design")
    board_evidence._pll_cell_parameters(synthesis, "synthesized")
    board_evidence._pll_cell_parameters(routed, "routed")
    board._i2c_evidence(synthesis, "synthesized")
    board._i2c_evidence(routed, "routed")
    counts = board._cell_counts(synthesis)
    for name, expected in REQUIRED_RESOURCES.items():
        if counts.get(name, 0) != expected:
            raise board.BuildError(f"synthesis must contain exactly {expected} {name}, got {counts.get(name, 0)}")
    for name in FORBIDDEN_RESOURCES:
        if counts.get(name, 0) != 0:
            raise board.BuildError(f"forbidden synthesis cell {name} is in use")

    route_log = output / "nextpnr.log"
    if route_log.is_symlink() or not route_log.is_file():
        raise board.BuildError(f"missing route log: {route_log}")
    route_text = route_log.read_text(encoding="utf-8", errors="replace")
    if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
        raise board.BuildError("route log does not prove a complete routed design")
    if "falling back to the cpu reference backend" in route_text.lower():
        raise board.BuildError("route log shows a GPU-router fallback; splash uses the default CPU router")
    reference = board_evidence._reference_clock_evidence(source_root, route_text)

    timing = board._read_json(output / "timing.json", "timing report")
    fmax = timing.get("fmax")
    if not isinstance(fmax, dict) or len(fmax) != 1:
        raise board.BuildError("timing report must contain the single pixel sequential domain")
    pixel = board_evidence._frequency_rows(fmax, 74.25, "pixel clock")
    utilization = timing.get("utilization")
    if not isinstance(utilization, dict):
        raise board.BuildError("timing report has no structured utilization data")
    resources: dict[str, dict[str, int]] = {}
    known = (
        board.ORDINARY_RESOURCES
        | set(REQUIRED_RESOURCES)
        | FORBIDDEN_RESOURCES
        | board.REQUIRED_ZERO_RESOURCES
    )
    unknown = sorted(set(utilization) - known)
    if unknown:
        raise board.BuildError("timing report contains unknown resources: " + ", ".join(unknown))
    for name, fields in sorted(utilization.items()):
        if not isinstance(fields, dict):
            raise board.BuildError(f"malformed resource evidence: {name}")
        used = fields.get("used")
        available = fields.get("available")
        if (
            isinstance(used, bool) or not isinstance(used, int) or used < 0
            or isinstance(available, bool) or not isinstance(available, int) or available < 0
        ):
            raise board.BuildError(f"malformed resource counts: {name}")
        resources[name] = {"available": available, "used": used}
    for name, expected in REQUIRED_RESOURCES.items():
        if name not in resources or resources[name]["used"] != expected:
            actual = "missing" if name not in resources else str(resources[name]["used"])
            raise board.BuildError(f"resource {name} must be exactly {expected}, got {actual}")
    for name in board.REQUIRED_ZERO_RESOURCES:
        if name not in resources or resources[name]["used"] != 0:
            actual = "missing" if name not in resources else str(resources[name]["used"])
            raise board.BuildError(f"resource {name} must be exactly 0, got {actual}")
    for name in FORBIDDEN_RESOURCES:
        if name in resources and resources[name]["used"] != 0:
            raise board.BuildError(f"forbidden resource {name} is in use")

    rbf = output / "core.rbf"
    if rbf.is_symlink() or not rbf.is_file() or not 1 <= rbf.stat().st_size <= board.MAX_PAYLOAD_SIZE:
        raise board.BuildError(f"RBF must be a nonempty bounded regular file: {rbf}")
    return {
        "status": "pass",
        "route": {"status": "pass", "unrouted": False, "router": "default"},
        "timing": {
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
        "user_io": {"probe": False, "hps_fb": False, "core_id": ""},
        "rbf": {"sha256": board._sha256(rbf), "size": rbf.stat().st_size},
    }


def _prepare_output(root: Path) -> Path:
    output = root / OUTPUT_RELATIVE
    build_root = root / "build"
    if build_root.is_symlink() or (build_root.exists() and not build_root.is_dir()):
        raise board.BuildError(f"build root must be a non-symlink directory: {build_root}")
    if output.is_symlink() or (output.exists() and not output.is_dir()):
        raise board.BuildError(f"splash output must be a non-symlink directory: {output}")
    output.mkdir(parents=True, exist_ok=True)
    for name in BUILD_OUTPUTS:
        path = output / name
        if path.is_symlink() or (path.exists() and not path.is_file()):
            raise board.BuildError(f"build output must be a regular file: {path}")
        if path.exists():
            path.unlink()
    return output


def _invalidate(output: Path) -> None:
    for name in ("core.rbf", "build-summary.json", "native-inputs-snippet.toml"):
        path = output / name
        if path.is_symlink() or path.is_file():
            path.unlink()
        elif path.exists():
            raise board.BuildError(f"cannot invalidate non-file failed build output: {path}")


def build(root: Path = ROOT, *, synth_only: bool = False) -> Path:
    root = Path(root).resolve()
    source_forbids_user_io(root)
    if synth_only:
        authenticated = authenticate_oss_tools(root)
        output = _prepare_output(root)
        commands = build_commands(root, {"yosys": authenticated["yosys"].path}, synth_only=True)
        try:
            board._run_tool(commands[0], root, output / "yosys.log", output_relative=OUTPUT_RELATIVE)
        except Exception:
            _invalidate(output)
            raise
        return output / "synth.json"

    repository, revision = board._require_clean_source(
        root, pinned_inputs=PINNED_INPUTS, identity_version=IDENTITY_VERSION
    )
    authenticated = authenticate_oss_tools(root)
    identities = {name: tool.identity for name, tool in authenticated.items()}
    record = create_build_record(root, repository, revision, identities)
    output = _prepare_output(root)
    board._write_atomic(output / "build-inputs.json", record)
    try:
        commands = build_commands(
            root,
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
        )
        board._run_tool(commands[0], root, output / "yosys.log", output_relative=OUTPUT_RELATIVE)
        board._run_tool(commands[1], root, output / "nextpnr.log", output_relative=OUTPUT_RELATIVE)
        evidence = validate_build_evidence(output, root)
        evidence.update({
            "build_id": build_identity(record),
            "device": board.TARGET,
            "inputs": {path: board._sha256(root / path) for path in sorted(PINNED_INPUTS)},
            "tools": identities,
            "top": "top",
            "format2_package": False,
        })
        board._write_atomic(
            output / "build-summary.json",
            (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode(),
        )
        board._write_atomic(
            output / "native-inputs-snippet.toml",
            native_inputs_snippet(repository, revision, evidence).encode(),
        )
        final_tools = authenticate_oss_tools(root)
        if {name: tool.identity for name, tool in final_tools.items()} != identities:
            raise board.BuildError("authenticated tool identity changed during build")
        if board._require_clean_source(
            root, pinned_inputs=PINNED_INPUTS, identity_version=IDENTITY_VERSION
        ) != (repository, revision):
            raise board.BuildError("source identity changed during build")
        return output / "core.rbf"
    except Exception:
        _invalidate(output)
        raise


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--synth-only", action="store_true",
                        help="dirty-tree Yosys probe; does not seal an RBF")
    args = parser.parse_args()
    try:
        print(build(args.root, synth_only=args.synth_only))
    except (board.BuildError, ValueError) as exc:
        print(f"FES splash: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
