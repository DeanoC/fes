#!/usr/bin/env python3
"""Build direct/scanline video parts against one sealed Atari ST raster video shell."""
from __future__ import annotations

import argparse
import io
import json
from pathlib import Path
import re
import sys
import tarfile

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import build_atari_st_slot_card as slot_recipe, build_fes_atari_st_oss as shell_recipe
from scripts import atari_st_video_parts as video_parts
from scripts.core_package import read_package
from scripts.cyclonev_rbf import CramRect, classify_cram_diff, overlay_cram, rbf_load, rbf_save
from scripts.fes_build_common import _prepare_output, _require_clean_source, _run_tool, _write_atomic
from scripts.fes_build_common import _cell_counts, validate_timing_resources, _require_gpu_backend
from scripts.functional_execution import FunctionalInvocation, source_roots_for_inputs
from scripts.export_core_package import source_input_closure, POLICY

ROOT = Path(__file__).resolve().parents[1]
SOURCES = ("cores/fes-common/rtl/fes_video_part_cart.v",
           "cores/fes-common/rtl/fes_video_part_direct.v",
           "cores/fes-common/rtl/fes_video_part_scanlines.v")
INPUTS = (*SOURCES, "cores/fes-common/generated/fes_video_part.vh",
          "scripts/build_atari_st_video_part.py", "scripts/atari_st_video_parts.py",
          "scripts/coleco_expansion.py", "scripts/functional_execution.py",
          "scripts/export_core_package.py", "scripts/compiler_read_audit.py",
          *slot_recipe.TOOL_INPUTS)
PLACER_SEED = 4
OUTPUTS = ("cart.json", "cart-synth.json", "cart.rbf", "cart-routed.json", "timing.json", "linked.rbf",
           "build-summary.json", "synthesis.log", "route.log", "clocks.sdc",
           "scaffold.json", "cart.qsf", "cram-diff.json")


def write_cram_report(output: Path, cart: bytes, changes: dict, *, part_id: str | None = None, layout=video_parts) -> Path:
    outside = changes.get("bits_outside_slot", 0)
    if part_id is not None and outside:
        raise ValueError("cannot publish a video part with outside-region changes")
    path = output / "cram-diff.json"
    _write_atomic(path, (json.dumps({"archive_published": part_id is not None,
        "cart_sha256": slot_recipe.digest(cart), "cram_diff": changes,
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


def prepare_scaffold(source: Path, destination: Path) -> bytes:
    frozen = source.read_bytes()
    shell_recipe.validate_routed_shell(json.loads(frozen))
    result = video_parts.prepare_scaffold(frozen)
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
    marker = {"id": video_parts.INTERFACE, "major": 1, "minor": 0, "required": False}
    markers = [i for i in package.fields["interfaces"] if i["id"].startswith("fes.fabric.video.")]
    cpu = {"id": "fes.expansion.atari-st-bus", "major": 1, "minor": 0, "required": False}
    if package.fields["format"] != 3 or package.fields["core"]["id"] != "fes.atari-st" or \
            package.fields.get("abi") != {"id": "fes.computer", "major": 1, "minor": 0} or \
            package.fields.get("rom", {}).get("role") != "firmware" or \
            markers != [marker] or cpu not in package.fields["interfaces"]:
        raise ValueError("video part requires the exact sealed format-3 ST raster profile")
    return video_parts


def build(root: Path, shell: Path, package_path: Path, variant: str, *,
          cache_root: Path | None = None, gpu: int = 0, seed: int = PLACER_SEED) -> Path:
    if variant not in ("direct", "scanlines"):
        raise ValueError("unknown video variant")
    if type(seed) is not int or not 1 <= seed <= 10:
        raise ValueError("video placer seed must be an integer from 1 to 10")
    root, shell = root.resolve(), shell.resolve()
    package = read_package(package_path)
    layout = package_profile(package)
    inputs, sources = INPUTS, SOURCES
    _, revision = _require_clean_source(root, pinned_inputs=inputs, identity_version=2)
    members = ("manifest.toml", "core.rbf", "routed.json", "socket.qsf", "rom-map.json")
    if (shell / members[0]).read_bytes() != package.manifest_bytes or \
            (shell / members[1]).read_bytes() != package.payload_bytes or \
            (shell / "rom-map.json").read_bytes() != package.rom_map_bytes:
        raise ValueError("frozen producer output differs from sealed shell package")
    tools = shell_recipe._authenticate_atari_st_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    roots = source_roots_for_inputs(inputs)
    closure = source_input_closure(root, roots, policy=POLICY)
    if any(path not in closure for path in inputs):
        raise ValueError("video part input is outside the tracked functional source closure")
    shell_closure = {name: slot_recipe.digest((shell / name).read_bytes()) for name in members}
    clocks = slot_recipe.cart_clock_constraints(root)
    invocation = FunctionalInvocation(tools, gpu)
    try:
        recipe = {"inputs": closure, "source_roots": roots, "source_closure_policy": POLICY,
                  "shell": shell_closure, "tools": identities,
                  "execution": invocation.inputs, "variant": variant, "placer_seed": seed,
                  "slot_clock": layout.CLOCK, "map": layout.MAP,
                  "cram_region": layout.CRAM, "required_clocks_mhz": slot_recipe.REQUIRED_CLOCKS_MHZ,
                  "clock_constraints_sha256": slot_recipe.digest(clocks)}
        recipe_sha = slot_recipe.digest(json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode())
        relative = Path("build/atari-st-video-parts") / variant / recipe_sha
        output = root / relative
        publications = tuple(path.name for path in output.glob("*.tar*"))
        _prepare_output(root, relative=relative, build_outputs=OUTPUTS + publications)
        (output / "clocks.sdc").write_bytes(clocks)
        qsf = (shell / "socket.qsf").read_bytes()
        if qsf.count(f'FES_RESERVED_RECT "{layout.PLACEMENT}"'.encode()) != 1:
            raise ValueError("frozen shell has no unique video placement reservation")
        (output / "cart.qsf").write_bytes(qsf)
        scaffold = prepare_scaffold(shell / "routed.json", output / "scaffold.json")
        define = "-DFES_VIDEO_SCANLINES=1 " if variant == "scanlines" else ""
        commands = (
            (str(tools["yosys"].path), "-p", f"read_verilog -sv {define}-I cores/fes-common/generated {' '.join(sources)}; synth_intel_alm -nolutram -nodsp -top cart; write_json {output / 'cart.json'}"),
            (str(tools["nextpnr-mistral"].path), "--json", str(output / "scaffold.json"),
             "--device", "5CSEBA6U23I7", "--qsf", str(output / "cart.qsf"),
             "--sdc", str(output / "clocks.sdc"), "--freq", "74.25",
             "--fes-scaffold", "--fes-cart", str(output / "cart.json"),
             "--fes-cart-region", layout.REGION, "--fes-slot-clock", layout.CLOCK,
             "--fes-cram-region", ",".join(map(str, layout.CRAM)), "--no-pack",
             "--seed", str(seed), "--router", "gpu", "--placer-heap-timingweight", "300",
             "--rbf", str(output / "cart.rbf"), "--compress-rbf",
             "--write", str(output / "cart-routed.json"), "--report", str(output / "timing.json")),
        )
        for name, command in zip(("synthesis", "route"), commands):
            _run_tool(command, root, output / (name + ".log"), env=invocation.env,
                      audit_source_root=root, output_relative=relative)
            if re.search(r"^\s*(?:ERROR|FATAL)\b", (output / (name + ".log")).read_text(), re.MULTILINE):
                raise ValueError(f"video {name} failed; see {output}")
            required = ("cart.json",) if name == "synthesis" else ("cart.rbf", "cart-routed.json", "timing.json")
            for member in required:
                require_output(output / member, 32 * 1024 * 1024 if member.endswith(".rbf") else 128 * 1024 * 1024)
        route_text = (output / "route.log").read_text()
        if "Info: Program finished normally." not in route_text or "unrouted" in route_text.lower():
            raise ValueError("video route log does not prove a complete routed design")
        gpu_backend = _require_gpu_backend(route_text)
        timing = json.loads((output / "timing.json").read_bytes())
        measured = slot_recipe.validate_cart_timing(timing)
        factory = shell_recipe
        resources = validate_timing_resources(timing.get("utilization"),
            factory.ORDINARY_RESOURCES | set(factory.REQUIRED_RESOURCES) |
            factory.FORBIDDEN_RESOURCES | factory.REQUIRED_ZERO_RESOURCES)
        synthesis_counts = _cell_counts(json.loads((output / "cart.json").read_bytes()))
        if any(synthesis_counts.get(name, 0) for name in factory.FORBIDDEN_RESOURCES | set(factory.REQUIRED_RESOURCES)):
            raise ValueError("video part must not own DSP, PLL or HPS resources")
        checked_clocks = validate_clocks(json.loads((output / "cart-routed.json").read_bytes()), layout=layout)
        if variant == "scanlines" and not checked_clocks:
            raise ValueError("scanline state did not survive synthesis")
        if any(synthesis_counts.get(kind, 0) for kind in ("MISTRAL_M10K", "MISTRAL_M10K_TDP")):
            raise ValueError("raster presentation part must not allocate RAM")
        cart = (output / "cart.rbf").read_bytes()
        base, placed = rbf_load(package.payload_bytes), rbf_load(cart)
        if base.header != placed.header:
            raise ValueError("video part changes shell ORAM/PRAM header")
        changes = classify_cram_diff(base, placed, CramRect(*layout.CRAM),
            include_outside_coordinates=True, ignore_ecc_columns=False)
        report = write_cram_report(output, cart, changes, layout=layout)
        if changes["bits_outside_slot"]:
            raise ValueError(f"video part changes CRAM outside its exact socket; see {report}")
        preview = overlay_cram(base, placed, CramRect(*layout.CRAM))
        # Compare every decoded CRAM bit, including legacy companion columns.
        if preview.cram != placed.cram:
            raise ValueError("ST video preview does not preserve every routed CRAM bit")
        (output / "linked.rbf").write_bytes(rbf_save(preview, compressed=True))
        manifest = {"cart_sha256": slot_recipe.digest(cart), "cart_size": len(cart), "device": "5CSEBA6U23I7", "format": 1,
                    "map": layout.MAP, "recipe_sha256": recipe_sha, "revision": revision,
                    "shell_build_id": package.fields["build"]["id"], "shell_package_id": package.package_id,
                    "shell_sha256": slot_recipe.digest(package.payload_bytes), "slot": layout.INTERFACE,
                    "slot_major": 1, "slot_minor": 0}
        encoded = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
        part_id = slot_recipe.digest(b"fes-expansion-v1\0" + encoded)
        if _require_clean_source(root, pinned_inputs=inputs, identity_version=2)[1] != revision or \
                source_input_closure(root, roots, policy=POLICY) != closure or \
                any(slot_recipe.digest((shell / name).read_bytes()) != value for name, value in shell_closure.items()):
            raise ValueError("video source or frozen shell changed during build")
        if {name: tool.identity for name, tool in shell_recipe._authenticate_atari_st_tools(root, cache_root).items()} != identities:
            raise ValueError("video compiler identity changed")
        invocation.verify()
        if (output / "clocks.sdc").read_bytes() != clocks or (output / "cart.qsf").read_bytes() != qsf or \
                (output / "scaffold.json").read_bytes() != scaffold:
            raise ValueError("video generated compiler inputs changed")
        return publish_archive(output, part_id, encoded, cart, changes,
            {"recipe": recipe, "part_id": part_id,
                "manifest": manifest, "cram_diff": changes, "checked_clock_pins": checked_clocks,
                "cram_policy": "strict-rectangle-v1",
                "preview_matches_routed_cram": preview.cram == placed.cram,
                "timing": measured, "resources": resources, "synthesis_cells": synthesis_counts,
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
    parser.add_argument("--seed", type=int, choices=range(1, 11), default=PLACER_SEED)
    args = parser.parse_args()
    print(build(args.root, args.shell, args.package, args.variant, cache_root=args.cache_root, gpu=args.gpu, seed=args.seed))
