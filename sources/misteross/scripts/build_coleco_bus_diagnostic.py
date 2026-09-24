#!/usr/bin/env python3
"""Build the CPU-bus diagnostic against a sealed Coleco development socket shell.

The shell is never placed or routed here. Launch-time composition uses the
misteross Go linker and requires neither this script nor the compiler.

This is a bus validation consumer, not the shell's public interface. Future
ROM, RAM and peripheral carts use the same bus slot and map.
"""
from __future__ import annotations
import argparse
import hashlib
import io
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import tarfile

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import build_fes_coleco_socket_dev as shell_recipe
from scripts import build_fes_coleco_oss as factory, coleco_expansion
from scripts.core_package import read_package
from scripts.fes_build_common import _prepare_output, _require_clean_source
from scripts.cyclonev_rbf import (
    rbf_load, rbf_save, overlay_cram, classify_cram_diff, CramRect,
    cram_get, cram_set,
)

ROOT = Path(__file__).resolve().parents[1]
SOURCES = ("cores/fes-coleco/expansions/diagnostic.v",)
INPUTS = SOURCES + ("cores/fes-coleco/rtl/coleco_bus_pack.vh", "scripts/build_coleco_bus_diagnostic.py", "toolchains/coleco-expansion.lock", "scripts/coleco_expansion.py", "scripts/cyclonev_rbf.py", "scripts/core_package.py", "scripts/rom_map.py", "scripts/fes_build_common.py", "scripts/build_fes_coleco_socket_dev.py", factory.SDC)
BUILD_OUTPUTS = ("cart.json", "cart.rbf", "cart-routed.json", "timing.json",
                 "linked.rbf", "build-summary.json", "synthesis.log", "route.log",
                 "clocks.sdc", "scaffold.json", "cart.qsf", "cram-diff.json")
PLACER_SEED = 4
REQUIRED_CLOCKS_MHZ = {"system_clock.clocks[0]": 52.224, "pixel_clk": 74.25, "system_clock.clocks[1]": 12.288}
CRAM_REGION = (1769, 32, 2806, 1034)  # fes.coleco-bus.socket/1, half-open
RESPONSE_BOUNDARY_CONTRACT = "fes.coleco.response-boundary/2"
RESPONSE_BOUNDARY_COORDINATES = (
    (2429, 1100), (2430, 1101),
)

def boundary_patch_for(placed) -> dict[str, object]:
    return {
        "bits": [
            {"value": cram_get(placed.cram, placed.die, x, y), "x": x, "y": y}
            for x, y in RESPONSE_BOUNDARY_COORDINATES
        ],
        "contract": RESPONSE_BOUNDARY_CONTRACT,
    }

def valid_boundary_patch(boundary_patch: dict[str, object] | None) -> bool:
    if not isinstance(boundary_patch, dict) or set(boundary_patch) != {"bits", "contract"} or \
            boundary_patch.get("contract") != RESPONSE_BOUNDARY_CONTRACT:
        return False
    bits = boundary_patch.get("bits")
    if not isinstance(bits, list) or len(bits) != len(RESPONSE_BOUNDARY_COORDINATES):
        return False
    for bit, (x, y) in zip(bits, RESPONSE_BOUNDARY_COORDINATES):
        if not isinstance(bit, dict) or set(bit) != {"value", "x", "y"} or \
                type(bit.get("x")) is not int or bit["x"] != x or \
                type(bit.get("y")) is not int or bit["y"] != y or \
                type(bit.get("value")) is not int or bit["value"] not in (0, 1):
            return False
    return True

def boundary_coordinates_match(changes: dict[str, object]) -> bool:
    if int(changes.get("bits_outside_slot", 0)) != len(RESPONSE_BOUNDARY_COORDINATES) or \
            changes.get("outside_slot_coordinates_truncated", True):
        return False
    coordinates = changes.get("outside_slot_coordinates")
    if not isinstance(coordinates, list) or len(coordinates) != len(RESPONSE_BOUNDARY_COORDINATES):
        return False
    if any(not isinstance(point, list) or len(point) != 2 or
           any(type(value) is not int for value in point) for point in coordinates):
        return False
    return sorted(tuple(point) for point in coordinates) == list(RESPONSE_BOUNDARY_COORDINATES)

def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()

def prepare_scaffold(source: Path, destination: Path) -> bytes:
    """Drop unconnected PLL aliases rejected by the frozen-pin loader.

    The producer's routed JSON omits the second PLL output connection while
    retaining its physical pin map and routed net. Reattach that exact net to
    the existing physical `outclk[1]` pin. `outclk[0]` is an obsolete alias
    of `outclk` after packing and is the only pin-map entry removed.
    """
    design = json.loads(source.read_text())
    top = design["modules"]["top"]
    cell = top["cells"]["system_clock.pll"]
    if cell["type"] != "altera_pll" or set(cell["connections"]) != {"outclk", "refclk", "locked"} \
            or any(len(bits) != 1 for bits in cell["connections"].values()):
        raise ValueError("Coleco PLL connection contract changed")
    attr = cell["attributes"]["FES_PINMAP_V1"]
    mapping = json.loads(bytes.fromhex(attr).decode())
    aliases = {f"outclk[{bit}]": [0, f"outclk[{bit}]"] for bit in range(2)}
    if mapping.get("count") != 6 or set(mapping.get("pins", {})) != {
            "locked", "outclk", "refclk", "rst", *aliases} or any(
            mapping["pins"].get(name) != value for name, value in aliases.items()):
        raise ValueError("Coleco PLL frozen pin map changed")
    output1 = top["netnames"].get("system_clock.pll_outclk_1", {}).get("bits")
    clock1 = top["cells"].get("system_clock.clocks_MISTRAL_CLKBUF_Q_1", {})
    if not isinstance(output1, list) or len(output1) != 1 or \
            clock1.get("type") != "MISTRAL_CLKBUF" or \
            clock1.get("connections", {}).get("A") != output1 or \
            cell.get("port_directions", {}).get("outclk") != "output":
        raise ValueError("Coleco frozen audio PLL net changed")
    cell["connections"]["outclk[1]"] = output1
    cell["port_directions"]["outclk[1]"] = "output"
    del mapping["pins"]["outclk[0]"]
    mapping["count"] = len(mapping["pins"])
    # json11 validates its own canonical dump, including spaces after tokens.
    cell["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping, sort_keys=True).encode().hex()
    for name, bel in coleco_expansion.socket_bels().items():
        boundary = design["modules"]["top"]["cells"].get(name)
        if not isinstance(boundary, dict) or boundary.get("type") != "MISTRAL_FF" or \
                boundary.get("attributes", {}).get("NEXTPNR_BEL") != bel:
            raise ValueError(f"Coleco frozen boundary changed: {name}")
    encoded = (json.dumps(design, separators=(",", ":")) + "\n").encode()
    destination.write_bytes(encoded)
    return encoded

def cart_clock_constraints(root: Path) -> bytes:
    # --no-pack restores the routed nets but does not derive PLL constraints.
    # These are the declared shell frequencies, never its achieved Fmax.
    text = (root / factory.SDC).read_text() + "\n# Frozen Coleco PLL output clocks.\n"
    for name, frequency in REQUIRED_CLOCKS_MHZ.items():
        text += f"create_clock -name {{{name}}} -period {1000 / frequency:.12f} [get_nets {{{name}}}]\n"
    return text.encode()

def cart_qsf(shell_qsf: bytes) -> bytes:
    original = f'set_global_assignment -name FES_RESERVED_RECT "{coleco_expansion.SOCKET_RECT}"'.encode()
    if shell_qsf.count(original) != 1:
        raise ValueError("Coleco shell QSF has no unique socket rectangle")
    # The two edge columns contain frozen request/response FFs. The cart only
    # needs the empty interior; keeping frozen cells outside its placement
    # reservation avoids a nextpnr self-rebinding error at those BELs.
    return shell_qsf.replace(original, b'set_global_assignment -name FES_RESERVED_RECT "25 1 27 11"')

def validate_cart_timing(timing: dict) -> None:
    fmax = timing.get("fmax")
    accepted_clocks = ({"system_clock.clocks[0]", "pixel_clk", "system_clock.clocks[1]"},)
    if not isinstance(fmax, dict) or set(fmax) not in accepted_clocks:
        raise ValueError("cart timing must report exactly the system and pixel clocks")
    for name, expected in REQUIRED_CLOCKS_MHZ.items():
        # Shared validation accounts for nextpnr's picosecond quantization,
        # rejects non-finite fields and requires achieved >= reported constraint.
        _, _, achieved = factory._frequency_row(fmax, expected, name, name)
        if achieved < expected:
            raise ValueError(f"cart {name} timing is below required {expected:g} MHz")

def write_cram_diff_report(
    output: Path, cart: bytes, changes: dict[str, object], *,
    archive_published: bool = False, expansion_id: str | None = None,
    boundary_patch: dict[str, object] | None = None,
) -> Path:
    """Persist CRAM-fence evidence, including on a rejected route."""
    outside = int(changes.get("bits_outside_slot", 0))
    patch_matches = valid_boundary_patch(boundary_patch) and boundary_coordinates_match(changes)
    if archive_published and outside and not patch_matches:
        raise ValueError("cannot publish a cart with undeclared CRAM changes outside the reserved slot")
    if boundary_patch is not None and (not patch_matches or outside != len(RESPONSE_BOUNDARY_COORDINATES)):
        raise ValueError("invalid Coleco response boundary patch evidence")
    report = {
        "archive_published": archive_published,
        "cart_sha256": digest(cart),
        "cram_diff": changes,
        "cram_region": list(CRAM_REGION),
        "format": 1,
        "route_contract": (
            "failed" if outside and not patch_matches else
            "passed_with_response_boundary_patch" if patch_matches else "passed"
        ),
    }
    if boundary_patch is not None:
        report["boundary_patch"] = boundary_patch
    if expansion_id is not None:
        report["expansion_id"] = expansion_id
    path = output / "cram-diff.json"
    path.write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    return path

def enforce_cram_region(
    changes: dict[str, object], report_path: Path, base=None, placed=None,
) -> dict[str, object] | None:
    outside = int(changes.get("bits_outside_slot", 0))
    if outside == 0:
        return None
    valid_coordinates = boundary_coordinates_match(changes)
    valid_values = base is not None and placed is not None and all(
        cram_get(base.cram, base.die, x, y) != cram_get(placed.cram, placed.die, x, y)
        for x, y in RESPONSE_BOUNDARY_COORDINATES
    )
    if not valid_coordinates or not valid_values:
        raise ValueError(
            f"cart changes {outside} non-ECC CRAM bits outside the socket and declared response patch; "
            f"cart not published; CRAM diff report saved to {report_path}"
        )
    return boundary_patch_for(placed)

def build(root: Path, shell: Path, package_path: Path, gpu: int, *, cache_root: Path | None = None) -> Path:
    root, shell = root.resolve(), shell.resolve()
    _, revision = _require_clean_source(root, pinned_inputs=INPUTS, identity_version=2)
    package = read_package(package_path)
    if (shell / "manifest.toml").read_bytes() != package.manifest_bytes or (shell / "core.rbf").read_bytes() != package.payload_bytes:
        raise ValueError("frozen producer output differs from sealed shell package")
    shell_members = ("routed.json", "socket.qsf", "manifest.toml", "core.rbf")
    if package.fields["format"] == 3:
        if (shell / "rom-map.json").read_bytes() != package.rom_map_bytes:
            raise ValueError("frozen producer ROM map differs from sealed shell package")
        shell_members += ("rom-map.json",)
    slot = [item for item in package.fields["interfaces"] if item["id"] == "fes.expansion.coleco-bus"]
    if len(slot) != 1 or slot[0]["major"] != 1 or slot[0]["minor"] != 0 or slot[0]["required"]:
        raise ValueError("shell must declare the optional Coleco expansion bus 1.0")
    for name in ("routed.json", "socket.qsf"):
        if not (shell / name).is_file():
            raise ValueError(f"shell producer directory requires {name}")
    tools = shell_recipe.authenticate_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    closure = {path: digest((root / path).read_bytes()) for path in INPUTS}
    closure.update({"shell/" + name: digest((shell / name).read_bytes()) for name in shell_members})
    clock_constraints = cart_clock_constraints(root)
    recipe = {"inputs": closure, "tools": identities, "slot_clock": "system_clock.clocks[0]", "map": "fes.coleco-bus.socket/1",
              "placer_seed": PLACER_SEED, "required_clocks_mhz": REQUIRED_CLOCKS_MHZ,
              "cram_region": CRAM_REGION,
              "scaffold_metadata_repair": "coleco-dual-pll-output-reload-v2",
              "clock_constraints_sha256": digest(clock_constraints)}
    recipe_sha = digest(json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode())
    output = root / "build/coleco-bus-diagnostic" / recipe_sha
    # A recipe directory can be retried. Remove both intermediate evidence and
    # prior publications before invoking either compiler, never after failure.
    publications = tuple(path.name for path in output.glob("*.tar"))
    _prepare_output(root, relative=output.relative_to(root),
                    build_outputs=BUILD_OUTPUTS + publications)
    (output / "clocks.sdc").write_bytes(clock_constraints)
    qsf = cart_qsf((shell / "socket.qsf").read_bytes())
    (output / "cart.qsf").write_bytes(qsf)
    scaffold = prepare_scaffold(shell / "routed.json", output / "scaffold.json")
    env = dict(os.environ, HIP_VISIBLE_DEVICES=str(gpu))
    commands = [
        [str(tools["yosys"].path), "-p", f"read_verilog -sv -I cores/fes-coleco/rtl {' '.join(SOURCES)}; synth_intel_alm -nolutram -nodsp -top cart; write_json {output / 'cart.json'}"],
        [str(tools["nextpnr-mistral"].path), "--json", str(output / "scaffold.json"), "--device", "5CSEBA6U23I7",
         "--qsf", str(output / "cart.qsf"), "--sdc", str(output / "clocks.sdc"), "--freq", "52.224",
         "--fes-scaffold", "--fes-cart", str(output / "cart.json"), "--fes-slot-clock", "system_clock.clocks[0]",
         "--fes-cram-region", ",".join(str(value) for value in CRAM_REGION),
         "--no-pack", "--seed", str(PLACER_SEED), "--router", "gpu", "--rbf", str(output / "cart.rbf"), "--compress-rbf",
         "--write", str(output / "cart-routed.json"), "--report", str(output / "timing.json")],
    ]
    for name, command in zip(("synthesis", "route"), commands):
        log_path = output / (name + ".log")
        with log_path.open("w") as log:
            subprocess.run(command, cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
        # Some nextpnr failures return zero. Exit status alone cannot admit an
        # artifact; keep the failing log but never publish its output.
        if re.search(r"^\s*(?:ERROR|FATAL)\b", log_path.read_text(errors="replace"), re.MULTILINE | re.IGNORECASE):
            raise ValueError(f"{name} reported an error; see {log_path}")
        required = ("cart.json",) if name == "synthesis" else ("cart.rbf", "cart-routed.json", "timing.json")
        for artifact in required:
            path = output / artifact
            if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
                raise ValueError(f"{name} did not produce nonempty {artifact}")
    timing = json.loads((output / "timing.json").read_text())
    validate_cart_timing(timing)
    if (output / "clocks.sdc").read_bytes() != clock_constraints:
        raise ValueError("cart clock constraints changed during build")
    if (output / "cart.qsf").read_bytes() != qsf:
        raise ValueError("cart placement constraints changed during build")
    if (output / "scaffold.json").read_bytes() != scaffold:
        raise ValueError("cart scaffold metadata changed during build")
    cart = (output / "cart.rbf").read_bytes()
    base, placed = rbf_load(package.payload_bytes), rbf_load(cart)
    rect = CramRect(*CRAM_REGION)
    if base.header != placed.header:
        raise ValueError("cart changes shell ORAM/PRAM header")
    changes = classify_cram_diff(base, placed, rect, include_outside_coordinates=True)
    cram_report = write_cram_diff_report(output, cart, changes)
    boundary_patch = enforce_cram_region(changes, cram_report, base, placed)
    cram_report = write_cram_diff_report(output, cart, changes, boundary_patch=boundary_patch)
    linked = overlay_cram(base, placed, rect)
    if boundary_patch is not None:
        for bit in boundary_patch["bits"]:
            cram_set(linked.cram, linked.die, int(bit["x"]), int(bit["y"]), int(bit["value"]))
    (output / "linked.rbf").write_bytes(rbf_save(linked, compressed=True))
    manifest = {"cart_sha256": digest(cart), "cart_size": len(cart), "device": "5CSEBA6U23I7", "format": 1,
        "map": "fes.coleco-bus.socket/1", "recipe_sha256": recipe_sha, "revision": revision,
        "shell_build_id": package.fields["build"]["id"], "shell_package_id": package.package_id,
        "shell_sha256": digest(package.payload_bytes), "slot": "fes.expansion.coleco-bus", "slot_major": 1, "slot_minor": 0}
    if boundary_patch is not None:
        manifest["boundary_patch"] = boundary_patch
    encoded = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    expansion_id = digest(b"fes-expansion-v1\0" + encoded)
    _, final_revision = _require_clean_source(root, pinned_inputs=INPUTS, identity_version=2)
    if final_revision != revision or any(digest((root / path).read_bytes()) != closure[path] for path in INPUTS):
        raise ValueError("source changed during cart build")
    for name in shell_members:
        if digest((shell / name).read_bytes()) != closure["shell/" + name]:
            raise ValueError("frozen shell changed during cart build")
    final_tools = shell_recipe.authenticate_tools(root, cache_root)
    if {name: tool.identity for name, tool in final_tools.items()} != identities:
        raise ValueError("authenticated compiler changed during cart build")
    destination = output / (expansion_id + ".tar")
    with tarfile.open(destination, "w", format=tarfile.USTAR_FORMAT) as archive:
        for name, data in (("manifest.json", encoded), ("cart.rbf", cart)):
            info = tarfile.TarInfo(name); info.size = len(data); info.mode = 0o600
            archive.addfile(info, io.BytesIO(data))
    write_cram_diff_report(output, cart, changes, archive_published=True, expansion_id=expansion_id,
                           boundary_patch=boundary_patch)
    (output / "build-summary.json").write_text(json.dumps({"recipe": recipe, "expansion_id": expansion_id, "manifest": manifest, "cram_diff": changes, "boundary_patch": boundary_patch}, sort_keys=True, indent=2) + "\n")
    return destination

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--shell", type=Path, required=True, help="socket producer output with sealed package and routed netlist")
    parser.add_argument("--package", type=Path, required=True, help="exact sealed shell package")
    parser.add_argument("--cache-root", type=Path, help="shared authenticated compiler cache")
    parser.add_argument("--gpu", type=int, default=0)
    args = parser.parse_args()
    print(build(args.root, args.shell, args.package, args.gpu, cache_root=args.cache_root))
