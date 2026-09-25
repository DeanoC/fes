#!/usr/bin/env python3
"""Build Opcode SGM against a sealed Coleco v2 development socket shell.

The shell is never placed or routed here. Launch-time composition uses the
misteross Go linker and requires neither this script nor the compiler.

This is the v2 SGM consumer; the v1 diagnostic remains separate.
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
from scripts import build_fes_coleco_socket_v2_dev as shell_recipe
from scripts import build_fes_coleco_oss as factory, coleco_expansion
from scripts.core_package import read_package
from scripts.fes_build_common import _prepare_output, _require_clean_source
from scripts.cyclonev_rbf import (
    rbf_load, rbf_save, overlay_cram, classify_cram_diff, CramRect,
)

ROOT = Path(__file__).resolve().parents[1]
SOURCES = ("cores/fes-coleco/expansions/sgm.v", "cores/fes-coleco/expansions/sgm_control.v", "cores/fes-coleco/expansions/sgm_ay.v")
INPUTS = SOURCES + ("cores/fes-coleco/rtl/coleco_bus_v2_pack.vh", "scripts/build_coleco_sgm.py", "toolchains/coleco-sgm.lock", "scripts/coleco_expansion.py", "scripts/cyclonev_rbf.py", "scripts/core_package.py", "scripts/rom_map.py", "scripts/fes_build_common.py", "scripts/build_fes_coleco_socket_v2_dev.py", factory.SDC)
BUILD_OUTPUTS = ("cart.json", "cart.rbf", "cart-routed.json", "timing.json",
                 "linked.rbf", "build-summary.json", "synthesis.log", "route.log",
                 "clocks.sdc", "scaffold.json", "cart.qsf", "cram-diff.json")
PLACER_SEED = 3
REQUIRED_CLOCKS_MHZ = {"system_clock.clocks[0]": 52.224, "pixel_clk": 74.25, "system_clock.clocks[1]": 12.288}
CRAM_REGION = (1769, 32, 2806, 1800)  # fes.coleco-bus.socket/2, half-open
# The measured v2 shell/cart route needs no outside-rectangle exception.

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
    for name, bel in coleco_expansion.socket_bels_v2().items():
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
    original = f'set_global_assignment -name FES_RESERVED_RECT "{coleco_expansion.SOCKET_RECT_V2}"'.encode()
    if shell_qsf.count(original) != 1:
        raise ValueError("Coleco shell QSF has no unique socket rectangle")
    # Both boundaries occupy only the first three LABs of the left edge.
    # The rest of that column remains available to the cart.
    return shell_qsf

def validate_cart_timing(timing: dict) -> None:
    fmax = timing.get("fmax")
    accepted_clocks = ({"system_clock.clocks[0]", "pixel_clk", "system_clock.clocks[1]"},)
    if not isinstance(fmax, dict) or set(fmax) not in accepted_clocks:
        raise ValueError("cart timing must report exactly the system, pixel and audio clocks")
    for name, expected in REQUIRED_CLOCKS_MHZ.items():
        # Shared validation accounts for nextpnr's picosecond quantization,
        # rejects non-finite fields and requires achieved >= reported constraint.
        _, _, achieved = factory._frequency_row(fmax, expected, name, name)
        if achieved < expected:
            raise ValueError(f"cart {name} timing is below required {expected:g} MHz")

def write_cram_diff_report(
    output: Path, cart: bytes, changes: dict[str, object], *,
    archive_published: bool = False, expansion_id: str | None = None,
) -> Path:
    """Persist CRAM-fence evidence, including on a rejected route."""
    outside = int(changes.get("bits_outside_slot", 0))
    if archive_published and outside:
        raise ValueError("cannot publish a cart with undeclared CRAM changes outside the reserved slot")
    report = {
        "archive_published": archive_published,
        "cart_sha256": digest(cart),
        "cram_diff": changes,
        "cram_region": list(CRAM_REGION),
        "format": 1,
        "route_contract": "failed" if outside else "passed",
    }
    if expansion_id is not None:
        report["expansion_id"] = expansion_id
    path = output / "cram-diff.json"
    path.write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    return path

def enforce_cram_region(changes: dict[str, object], report_path: Path) -> None:
    outside = int(changes.get("bits_outside_slot", 0))
    if outside == 0:
        return None
    raise ValueError(
        f"cart changes {outside} non-ECC CRAM bits outside the socket; "
        f"cart not published; CRAM diff report saved to {report_path}"
    )

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
    if len(slot) != 1 or slot[0]["major"] != 2 or slot[0]["minor"] != 0 or slot[0]["required"]:
        raise ValueError("shell must declare the optional Coleco expansion bus 2.0")
    for name in ("routed.json", "socket.qsf"):
        if not (shell / name).is_file():
            raise ValueError(f"shell producer directory requires {name}")
    tools = shell_recipe.authenticate_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    closure = {path: digest((root / path).read_bytes()) for path in INPUTS}
    closure.update({"shell/" + name: digest((shell / name).read_bytes()) for name in shell_members})
    clock_constraints = cart_clock_constraints(root)
    recipe = {"inputs": closure, "tools": identities, "slot_clock": "system_clock.clocks[0]", "map": "fes.coleco-bus.socket/2",
              "placer_seed": PLACER_SEED, "required_clocks_mhz": REQUIRED_CLOCKS_MHZ,
              "cram_region": CRAM_REGION,
              "scaffold_metadata_repair": "coleco-dual-pll-output-reload-v2",
              "clock_constraints_sha256": digest(clock_constraints)}
    recipe_sha = digest(json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode())
    output = root / "build/coleco-sgm" / recipe_sha
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
         "--no-pack", "--seed", str(PLACER_SEED), "--router", "gpu", "--placer-heap-timingweight", "300", "--rbf", str(output / "cart.rbf"), "--compress-rbf",
         "--write", str(output / "cart-routed.json"), "--report", str(output / "timing.json")],
    ]
    for name, command in zip(("synthesis", "route"), commands):
        log_path = output / (name + ".log")
        with log_path.open("w") as log:
            try:
                subprocess.run(command, cwd=root, env=env, stdout=log,
                               stderr=subprocess.STDOUT, check=True, timeout=600)
            except subprocess.TimeoutExpired as exc:
                raise ValueError(f"{name} timed out; see {log_path}") from exc
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
    enforce_cram_region(changes, cram_report)
    linked = overlay_cram(base, placed, rect)
    (output / "linked.rbf").write_bytes(rbf_save(linked, compressed=True))
    manifest = {"cart_sha256": digest(cart), "cart_size": len(cart), "device": "5CSEBA6U23I7", "format": 1,
        "map": "fes.coleco-bus.socket/2", "recipe_sha256": recipe_sha, "revision": revision,
        "shell_build_id": package.fields["build"]["id"], "shell_package_id": package.package_id,
        "shell_sha256": digest(package.payload_bytes), "slot": "fes.expansion.coleco-bus", "slot_major": 2, "slot_minor": 0}
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
    write_cram_diff_report(output, cart, changes, archive_published=True, expansion_id=expansion_id)
    (output / "build-summary.json").write_text(json.dumps({"recipe": recipe, "expansion_id": expansion_id, "manifest": manifest, "cram_diff": changes}, sort_keys=True, indent=2) + "\n")
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
