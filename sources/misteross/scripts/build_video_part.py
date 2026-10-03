#!/usr/bin/env python3
"""Build direct/scanline video parts against one sealed Coleco video shell."""
from __future__ import annotations

import argparse
import io
import json
from pathlib import Path
import re
import shutil
import sys
import tarfile

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import build_coleco_sgm as sgm, build_fes_coleco_socket_v2 as shell_recipe
from scripts import coleco_expansion, video_parts, native_video_parts, native_video_clock
from scripts.core_package import read_package
from scripts.cyclonev_rbf import CramRect, classify_cram_diff, overlay_cram, rbf_load, rbf_save
from scripts.fes_build_common import _prepare_output, _require_clean_source, _run_tool, _write_atomic
from scripts.fes_build_common import _cell_counts, validate_timing_resources
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.export_core_package import source_input_closure, POLICY

ROOT = Path(__file__).resolve().parents[1]
SOURCES = ("cores/fes-common/rtl/fes_video_part_cart.v",
           "cores/fes-common/rtl/fes_video_part_direct.v",
           "cores/fes-common/rtl/fes_video_part_scanlines.v")
INPUTS = (*SOURCES, "cores/fes-common/generated/fes_video_part.vh",
          "scripts/build_video_part.py", "scripts/video_parts.py", *sgm.INPUTS)
NATIVE_SOURCES = ("cores/fes-common/rtl/fes_native_video_cart.v",
                  "cores/fes-common/rtl/fes_native_video.v",
                  "cores/fes-common/rtl/coleco_video_dpram.v")
NATIVE_INPUTS = (*NATIVE_SOURCES, "cores/fes-common/generated/fes_native_video.vh",
                 "scripts/build_video_part.py", "scripts/native_video_parts.py",
                 "scripts/native_video_clock.py", *sgm.INPUTS)
OUTPUTS = ("cart.json", "cart-synth.json", "cart.rbf", "cart-routed.json", "timing.json", "linked.rbf",
           "build-summary.json", "synthesis.log", "route.log", "clocks.sdc",
           "scaffold.json", "cart.qsf", "cram-diff.json")
# Match expansion/rbf.go crcCompanionColumn, used by ComposePartsContext.
# The native fence crosses the first range; the small raster fence does not.
PARTS_CRC_COMPANION_RANGES = ((3488, 3847), (3921, 3980), (4171, 4471))


def write_cram_report(output: Path, cart: bytes, changes: dict, *, part_id: str | None = None, layout=video_parts) -> Path:
    outside = changes.get("bits_outside_slot", 0)
    if part_id is not None and outside:
        raise ValueError("cannot publish a video part with outside-region changes")
    path = output / "cram-diff.json"
    _write_atomic(path, (json.dumps({"archive_published": part_id is not None,
        "cart_sha256": sgm.digest(cart), "cram_diff": changes,
        "cram_region": layout.CRAM, "map": layout.MAP,
        "part_id": part_id, "route_contract": "failed" if outside else "passed"},
        sort_keys=True, indent=2) + "\n").encode())
    return path


def publish_archive(output: Path, part_id: str, encoded: bytes, cart: bytes,
                    changes: dict, summary: dict, *, layout=video_parts) -> Path:
    destination = output / (part_id + ".tar")
    temporary = output / (part_id + ".tar.tmp")
    summary_path = output / "build-summary.json"
    try:
        with tarfile.open(temporary, "w", format=tarfile.USTAR_FORMAT) as archive:
            for name, data in (("manifest.json", encoded), ("cart.rbf", cart)):
                info = tarfile.TarInfo(name)
                info.size, info.mode = len(data), 0o600
                archive.addfile(info, io.BytesIO(data))
        temporary.replace(destination)
        write_cram_report(output, cart, changes, part_id=part_id, layout=layout)
        _write_atomic(summary_path, (json.dumps(summary, sort_keys=True, indent=2) + "\n").encode())
    except BaseException:
        temporary.unlink(missing_ok=True)
        destination.unlink(missing_ok=True)
        summary_path.unlink(missing_ok=True)
        write_cram_report(output, cart, changes, layout=layout)
        raise
    return destination


def prepare_scaffold(source: Path, destination: Path, *, layout=video_parts) -> bytes:
    # Reuse the qualified PLL metadata repair without removing the CPU socket's
    # clock anchor: its configuration lies outside the video CRAM fence.
    original = json.loads(source.read_bytes())
    repaired = json.loads(sgm.prepare_scaffold(source, destination))
    cells = repaired["modules"]["top"]["cells"]
    cells[coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL] = original["modules"]["top"]["cells"][
        coleco_expansion.SOCKET_CLOCK_COVERAGE_CELL]
    result = layout.prepare_scaffold(json.dumps(repaired).encode())
    destination.write_bytes(result)
    return result


def validate_clocks(routed: dict, *, layout=video_parts) -> int:
    top = routed["modules"]["top"]
    clock = top["netnames"][layout.CLOCK]["bits"]
    checked = 0
    for name, cell in top["cells"].items():
        if not name.startswith("fes_cart$"):
            continue
        pins = {"MISTRAL_FF": ("CLK",), "MISTRAL_M10K": ("CLK1", "CLK2"),
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2")}.get(cell["type"], ())
        for pin in pins:
            bits = cell["connections"].get(pin)
            if bits and bits != clock:
                raise ValueError(f"video part {name}.{pin} is not on the pixel clock")
            checked += bool(bits)
    return checked


def require_output(path: Path, maximum: int) -> None:
    if path.is_symlink() or not path.is_file() or not 0 < path.stat().st_size <= maximum:
        raise ValueError(f"compiler output must be a bounded regular file: {path}")


def package_profile(package):
    profiles = [layout for layout in (video_parts, native_video_parts)
                if {"id": layout.INTERFACE, "major": 1, "minor": 0, "required": False}
                in package.fields["interfaces"]]
    video_markers = [row for row in package.fields["interfaces"]
                     if row["id"] in (video_parts.INTERFACE, native_video_parts.INTERFACE)]
    if package.fields["format"] != 2 or len(profiles) != 1 or len(video_markers) != 1:
        raise ValueError("video parts require one exact sealed format-2 video shell profile")
    return profiles[0]


def build(root: Path, shell: Path, package_path: Path, variant: str, *,
          cache_root: Path | None = None, gpu: int = 0) -> Path:
    if variant not in ("direct", "scanlines"):
        raise ValueError("unknown video variant")
    root, shell = root.resolve(), shell.resolve()
    package = read_package(package_path)
    layout = package_profile(package)
    native = layout is native_video_parts
    inputs, sources = (NATIVE_INPUTS, NATIVE_SOURCES) if native else (INPUTS, SOURCES)
    _, revision = _require_clean_source(root, pinned_inputs=inputs, identity_version=2)
    members = ("manifest.toml", "core.rbf", "routed.json", "socket.qsf")
    if (shell / members[0]).read_bytes() != package.manifest_bytes or \
            (shell / members[1]).read_bytes() != package.payload_bytes:
        raise ValueError("frozen producer output differs from sealed shell package")
    tools = shell_recipe.authenticate_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    roots = source_roots_for_inputs(inputs)
    closure = source_input_closure(root, roots, policy=POLICY)
    if any(path not in closure for path in inputs):
        raise ValueError("video part input is outside the tracked functional source closure")
    shell_closure = {name: sgm.digest((shell / name).read_bytes()) for name in members}
    clocks = sgm.cart_clock_constraints(root)
    invocation = FunctionalInvocation(tools, gpu)
    try:
        recipe = {"inputs": closure, "source_roots": roots, "source_closure_policy": POLICY,
                  "shell": shell_closure, "tools": identities,
                  "execution": invocation.inputs, "variant": variant,
                  "slot_clock": layout.CLOCK, "map": layout.MAP,
                  "cram_region": layout.CRAM, "required_clocks_mhz": sgm.REQUIRED_CLOCKS_MHZ,
                  "clock_constraints_sha256": sgm.digest(clocks)}
        recipe_sha = sgm.digest(json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode())
        relative = Path("build/video-parts") / variant / recipe_sha
        output = root / relative
        publications = tuple(path.name for path in output.glob("*.tar*"))
        _prepare_output(root, relative=relative, build_outputs=OUTPUTS + publications)
        (output / "clocks.sdc").write_bytes(clocks)
        qsf = (shell / "socket.qsf").read_bytes()
        if qsf.count(f'FES_RESERVED_RECT "{layout.PLACEMENT}"'.encode()) != 1:
            raise ValueError("frozen shell has no unique video placement reservation")
        (output / "cart.qsf").write_bytes(qsf)
        scaffold = prepare_scaffold(shell / "routed.json", output / "scaffold.json", layout=layout)
        define = "-DFES_VIDEO_SCANLINES=1 " if variant == "scanlines" else ""
        commands = (
            (str(tools["yosys"].path), "-p", f"read_verilog -sv {define}-I cores/fes-common/generated {' '.join(sources)}; synth_intel_alm -nolutram -nodsp -top cart; write_json {output / 'cart.json'}"),
            (str(tools["nextpnr-mistral"].path), "--json", str(output / "scaffold.json"),
             "--device", "5CSEBA6U23I7", "--qsf", str(output / "cart.qsf"),
             "--sdc", str(output / "clocks.sdc"), "--freq", "74.25",
             "--fes-scaffold", "--fes-cart", str(output / "cart.json"),
             "--fes-cart-region", layout.REGION, "--fes-slot-clock", layout.CLOCK,
             "--fes-cram-region", ",".join(map(str, layout.CRAM)), "--no-pack",
             "--seed", "3", "--router", "gpu", "--placer-heap-timingweight", "300",
             "--rbf", str(output / "cart.rbf"), "--compress-rbf",
             "--write", str(output / "cart-routed.json"), "--report", str(output / "timing.json")),
        )
        clock_boundary = None
        for name, command in zip(("synthesis", "route"), commands):
            _run_tool(command, root, output / (name + ".log"), env=invocation.env,
                      audit_source_root=root, output_relative=relative)
            if re.search(r"^\s*(?:ERROR|FATAL)\b", (output / (name + ".log")).read_text(), re.MULTILINE):
                raise ValueError(f"video {name} failed; see {output}")
            required = ("cart.json",) if name == "synthesis" else ("cart.rbf", "cart-routed.json", "timing.json")
            for member in required:
                require_output(output / member, 32 * 1024 * 1024 if member.endswith(".rbf") else 128 * 1024 * 1024)
            if native and name == "synthesis":
                # The packed importer removes transparent clock buffers. Its
                # M10K second port needs the declared input-buffer net so both
                # RAM clocks resolve to the same imported pixel clock.
                shutil.copyfile(output / "cart.json", output / "cart-synth.json")
                clock_boundary = native_video_clock.prepare_native_clock(output / "cart.json")
        route_text = (output / "route.log").read_text()
        if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
            raise ValueError("video route log does not prove a complete routed design")
        gpu_backend = shell_recipe.factory._require_gpu_backend(route_text)
        timing = json.loads((output / "timing.json").read_bytes())
        sgm.validate_cart_timing(timing)
        factory = shell_recipe.factory
        resources = validate_timing_resources(timing.get("utilization"),
            factory.ORDINARY_RESOURCES | set(factory.REQUIRED_RESOURCES) |
            factory.FORBIDDEN_RESOURCES | factory.REQUIRED_ZERO_RESOURCES)
        measured = {name: factory._frequency_row(timing["fmax"], frequency, name, name)
                    for name, frequency in sgm.REQUIRED_CLOCKS_MHZ.items()}
        synthesis_counts = _cell_counts(json.loads((output / "cart.json").read_bytes()))
        if any(synthesis_counts.get(name, 0) for name in factory.FORBIDDEN_RESOURCES | set(factory.REQUIRED_RESOURCES)):
            raise ValueError("video part must not own DSP, PLL or HPS resources")
        checked_clocks = validate_clocks(json.loads((output / "cart-routed.json").read_bytes()), layout=layout)
        if (native or variant == "scanlines") and not checked_clocks:
            raise ValueError("scanline state did not survive synthesis")
        if native and sum(synthesis_counts.get(kind, 0) for kind in ("MISTRAL_M10K", "MISTRAL_M10K_TDP")) != 48:
            raise ValueError("native part must own exactly 48 M10K frame-buffer blocks")
        cart = (output / "cart.rbf").read_bytes()
        base, placed = rbf_load(package.payload_bytes), rbf_load(cart)
        if base.header != placed.header:
            raise ValueError("video part changes shell ORAM/PRAM header")
        changes = classify_cram_diff(base, placed, CramRect(*layout.CRAM), include_outside_coordinates=True)
        report = write_cram_report(output, cart, changes, layout=layout)
        sgm.enforce_cram_region(changes, report)
        (output / "linked.rbf").write_bytes(rbf_save(overlay_cram(base, placed, CramRect(*layout.CRAM),
            preserve_x_ranges=PARTS_CRC_COMPANION_RANGES), compressed=True))
        manifest = {"cart_sha256": sgm.digest(cart), "cart_size": len(cart), "device": "5CSEBA6U23I7", "format": 1,
                    "map": layout.MAP, "recipe_sha256": recipe_sha, "revision": revision,
                    "shell_build_id": package.fields["build"]["id"], "shell_package_id": package.package_id,
                    "shell_sha256": sgm.digest(package.payload_bytes), "slot": layout.INTERFACE,
                    "slot_major": 1, "slot_minor": 0}
        encoded = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
        part_id = sgm.digest(b"fes-expansion-v1\0" + encoded)
        if _require_clean_source(root, pinned_inputs=inputs, identity_version=2)[1] != revision or \
                source_input_closure(root, roots, policy=POLICY) != closure or \
                any(sgm.digest((shell / name).read_bytes()) != value for name, value in shell_closure.items()):
            raise ValueError("video source or frozen shell changed during build")
        if {name: tool.identity for name, tool in shell_recipe.authenticate_tools(root, cache_root).items()} != identities:
            raise ValueError("video compiler identity changed")
        invocation.verify()
        if (output / "clocks.sdc").read_bytes() != clocks or (output / "cart.qsf").read_bytes() != qsf or \
                (output / "scaffold.json").read_bytes() != scaffold:
            raise ValueError("video generated compiler inputs changed")
        return publish_archive(output, part_id, encoded, cart, changes,
            {"recipe": recipe, "part_id": part_id,
                "manifest": manifest, "cram_diff": changes, "checked_clock_pins": checked_clocks,
                "timing": measured, "resources": resources, "synthesis_cells": synthesis_counts,
                **({"native_clock_boundary": clock_boundary} if native else {}),
                "route": {"complete": True, "gpu_backend": gpu_backend}}, layout=layout)
    finally:
        invocation.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--shell", type=Path, required=True)
    parser.add_argument("--package", type=Path, required=True)
    parser.add_argument("--variant", choices=("direct", "scanlines"), required=True)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--gpu", type=int, default=0)
    args = parser.parse_args()
    print(build(args.root, args.shell, args.package, args.variant, cache_root=args.cache_root, gpu=args.gpu))
