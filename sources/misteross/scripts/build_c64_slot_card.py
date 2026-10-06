#!/usr/bin/env python3
"""Build one Commodore 64 card for one physical socket of a sealed FES C64 shell.

The shell is never placed or routed here. A copy of its frozen routed netlist
becomes the scaffold: only the chosen slot's boundary flip-flops are renamed
to the canonical plug cells nextpnr merges a cart onto, so the card is placed
in that slot's named region (`--fes-cart-region slotN`) and routed inside its
CRAM rectangle while the other sockets stay untouched. The result is a
two-member expansion archive (canonical `manifest.json` with `slot_index`, and
the placed `cart.rbf`) bound to the exact shell. Launch-time composition of
any set of cards uses the misteross Go linker, not this script.
"""
from __future__ import annotations

import argparse
import hashlib
import io
import json
import math
import os
import re
import subprocess
import sys
import tarfile
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from scripts import c64_slots
from scripts import build_fes_c64_oss as shell_recipe
from scripts.core_package import read_package
from scripts.cyclonev_rbf import CramRect, classify_cram_diff, overlay_cram, rbf_load, rbf_save
from scripts.fes_build_common import _prepare_output, _require_clean_source

ROOT = Path(__file__).resolve().parents[1]
CARDS = {
    "probe": ("cores/fes-c64/expansions/probe.v",),
}
CARD_INCLUDES = ("cores/fes-c64/rtl/c64_bus.vh",)
TOOL_INPUTS = (
    "scripts/build_c64_slot_card.py", "scripts/c64_slots.py", "scripts/build_fes_c64_oss.py",
    "scripts/cyclonev_rbf.py", "scripts/core_package.py", "scripts/fes_build_common.py",
    shell_recipe.C64_TOOLCHAIN_LOCK, shell_recipe.SDC,
)
BUILD_OUTPUTS = ("cart.json", "cart.rbf", "cart-routed.json", "timing.json", "linked.rbf",
                 "build-summary.json", "synthesis.log", "route.log", "clocks.sdc",
                 "scaffold.json", "cart.qsf", "cram-diff.json")
PLACER_SEED = 3
SLOT_CLOCK = "system_clock.clocks[0]"
REQUIRED_CLOCKS_MHZ = {"system_clock.clocks[0]": 52.224, "machine.main_ram.clk_b": 74.25,
                       "system_clock.clocks[1]": 12.288}


def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def socket_for(slot: int) -> c64_slots.Socket:
    for socket in c64_slots.SOCKETS:
        if socket.slot == slot:
            return socket
    raise ValueError(f"slot {slot} is not a physical socket of {c64_slots.LAYOUT}")


def card_inputs(card: str) -> tuple[str, ...]:
    if card not in CARDS:
        raise ValueError(f"unknown Commodore 64 card {card!r}")
    return CARDS[card] + CARD_INCLUDES + TOOL_INPUTS


def prepare_scaffold(source: bytes, slot: int) -> bytes:
    """Frozen shell netlist with the chosen socket exposed as the cart plugs.

    Legacy routed JSON omits the PLL's second output connection: reattach
    its authenticated audio net and drop the obsolete `outclk[0]` alias.
    Current routed JSON already exposes physical C6/C7 outputs; validate
    and preserve those connections and their pin map without rewriting. Every
    socket's boundary must still be at its pinned BEL. The chosen socket's
    request/response flip-flops are renamed plug_addr_ff_N / plug_rdata_ff_N
    and its clock-coverage flip-flops are removed (their routed clock
    branches stay frozen for the card to extend inside its fence); the other
    sockets keep their instance names.
    """
    design = json.loads(source)
    top = design["modules"]["top"]
    cells = top["cells"]
    pll = cells["system_clock.pll"]
    ports = {"outclk", "refclk", "locked"}
    physical_outputs = set(pll["connections"]) == ports | {"outclk[1]"}
    if pll["type"] != "altera_pll" or set(pll["connections"]) not in (ports, ports | {"outclk[1]"}):
        raise ValueError("Commodore 64 system PLL connection contract changed")
    mapping = json.loads(bytes.fromhex(pll["attributes"]["FES_PINMAP_V1"]).decode())
    aliases = {f"outclk[{bit}]": [0, f"outclk[{bit}]"] for bit in range(2)}
    physical_pins = {"locked": [0, "locked"], "outclk": [0, "C6"],
                     "outclk[1]": [0, "C7"], "refclk": [0, "refclk"], "rst": [0, "rst"]}
    if physical_outputs:
        if mapping != {"count": 5, "pins": physical_pins} or pll["port_directions"] != {
                "outclk": "output", "outclk[1]": "output", "refclk": "input", "locked": "output"}:
            raise ValueError("Commodore 64 system PLL physical pin map changed")
    elif mapping.get("count") != 6 or any(mapping["pins"].get(k) != v for k, v in aliases.items()):
        raise ValueError("Commodore 64 system PLL frozen pin map changed")
    output1 = top["netnames"].get("system_clock.pll_outclk_1", {}).get("bits")
    clock1 = top["netnames"].get("system_clock.clocks[1]", {}).get("bits")
    buffers = [c for c in cells.values() if c.get("type") == "MISTRAL_CLKBUF" and
               c.get("connections", {}).get("Q") == clock1]
    if not isinstance(output1, list) or len(output1) != 1 or len(buffers) != 1 or \
            buffers[0]["connections"].get("A") != output1:
        raise ValueError("Commodore 64 frozen audio PLL net changed")
    if physical_outputs:
        if pll["connections"]["outclk[1]"] != output1:
            raise ValueError("Commodore 64 frozen audio PLL connection changed")
    else:
        pll["connections"]["outclk[1]"] = output1
        pll["port_directions"]["outclk[1]"] = "output"
        del mapping["pins"]["outclk[0]"]
        mapping["count"] = len(mapping["pins"])
        pll["attributes"]["FES_PINMAP_V1"] = json.dumps(mapping, sort_keys=True).encode().hex()

    if any(name.startswith(("plug_addr_ff_", "plug_rdata_ff_")) for name in cells):
        raise ValueError("frozen shell already exposes canonical plug cells")
    target = socket_for(slot)
    for socket in c64_slots.SOCKETS:
        for name, bel in c64_slots.boundary_bels(socket).items():
            cell = cells.get(socket.instance + name)
            if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_FF" or \
                    cell.get("attributes", {}).get("NEXTPNR_BEL") != bel:
                raise ValueError(f"frozen slot boundary changed: {socket.instance}{name}")
    for bit in range(c64_slots.REQUEST_BITS):
        cells[f"plug_addr_ff_{bit}"] = cells.pop(f"{target.instance}plug_request_ff_{bit}")
    for bit in range(c64_slots.RESPONSE_BITS):
        cells[f"plug_rdata_ff_{bit}"] = cells.pop(f"{target.instance}plug_response_ff_{bit}")
    for name in [n for n in cells if n.startswith(f"{target.instance}clock_coverage_ff_")]:
        del cells[name]
    return (json.dumps(design, separators=(",", ":")) + "\n").encode()


def cart_clock_constraints(root: Path) -> bytes:
    # --no-pack restores routed nets but does not derive PLL constraints. These
    # are the declared shell frequencies, never its achieved Fmax.
    # --no-pack imports routed nets, not top-level ports in the SDC context.
    # Constrain the same physical input net rather than an empty port query.
    text = (root / shell_recipe.SDC).read_text().replace(
        "[get_ports {FPGA_CLK1_50}]", "[get_nets {FPGA_CLK1_50}]")
    text += "\n# Frozen Commodore 64 shell clocks.\n"
    for name, frequency in REQUIRED_CLOCKS_MHZ.items():
        text += f"create_clock -name {{{name}}} -period {1000 / frequency:.12f} [get_nets {{{name}}}]\n"
    return text.encode()


def validate_cart_timing(timing: dict) -> dict:
    fmax = timing.get("fmax")
    if not isinstance(fmax, dict) or set(fmax) != set(REQUIRED_CLOCKS_MHZ):
        raise ValueError("cart timing must report exactly the system, pixel and audio clocks")
    result = {}
    for name, expected in REQUIRED_CLOCKS_MHZ.items():
        _, _, achieved = shell_recipe._frequency_row({name: fmax[name]}, expected, name)
        result[name] = achieved
    summary = timing.get("timing_summary", {})
    clocks = summary.get("clocks") if isinstance(summary, dict) else None
    if not isinstance(summary, dict) or summary.get("final_analogue_model") is not True or not isinstance(clocks, dict) or \
            set(clocks) != set(REQUIRED_CLOCKS_MHZ):
        raise ValueError("cart requires final analogue timing on every required clock")
    for name, fields in clocks.items():
        for key in ("setup_wns_ns", "hold_wns_ns"):
            value = fields.get(key) if isinstance(fields, dict) else None
            if isinstance(value, bool) or not isinstance(value, (int, float)) or \
                    not math.isfinite(value) or value < 0:
                raise ValueError(f"cart {name} requires non-negative finite {key}")
    return result


CARD_CLOCK_PORTS = {"MISTRAL_FF": ("CLK",), "MISTRAL_M10K": ("CLK1", "CLK2"),
                    "MISTRAL_M10K_TDP": ("CLK1", "CLK2")}


def validate_cart_clocks(routed: dict, *, allow_combinational: bool = False) -> int:
    """Every connected clock pin of a merged card cell must be the shell socket clock.

    The cart merge drops the card's clock buffer and reconnects the pins it
    knows; a pin it misses (for example the read clock of a dual-clock M10K)
    is left on an undriven net, which timing and CRAM checks cannot see.
    """
    top = routed["modules"]["top"]
    clock_bits = set(top["netnames"][SLOT_CLOCK]["bits"])
    checked = 0
    for name, cell in top["cells"].items():
        if not name.startswith("fes_cart$"):
            continue
        for port in CARD_CLOCK_PORTS.get(cell["type"], ()):
            bits = cell.get("connections", {}).get(port)
            if not bits:
                continue
            if any(bit not in clock_bits for bit in bits):
                raise ValueError(f"card cell {name} pin {port} is not on the socket clock {SLOT_CLOCK}")
            checked += 1
    # The ROM probe is deliberately combinational after MODE specialization.
    # Require actual card logic even when no clock pins remain to validate.
    if not checked and not (allow_combinational and any(
            name.startswith("fes_cart$") for name in top["cells"])):
        raise ValueError("routed card has no clocked cells on the socket clock")
    return checked


def card_manifest(package, slot: int, cart: bytes, recipe_sha: str, revision: str) -> bytes:
    manifest = {
        "cart_sha256": digest(cart), "cart_size": len(cart), "device": "5CSEBA6U23I7", "format": 1,
        "map": c64_slots.LAYOUT, "recipe_sha256": recipe_sha, "revision": revision,
        "shell_build_id": package.fields["build"]["id"], "shell_package_id": package.package_id,
        "shell_sha256": digest(package.payload_bytes), "slot": c64_slots.INTERFACE,
        "slot_index": slot, "slot_major": 1, "slot_minor": 0,
    }
    return json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()


def card_synthesis_script(card: str, slot: int, output: Path) -> str:
    socket_for(slot)
    card_inputs(card)
    sources = " ".join(CARDS[card])
    return (f"read_verilog -sv -I cores/fes-c64/rtl -I cores/fes-c64/expansions {sources}; "
            f"chparam -set MODE {slot - 1} cart; "
            f"synth_intel_alm -nolutram -nodsp -top cart; write_json {output / 'cart.json'}")


def materialize_response_drivers(source: bytes) -> bytes:
    """Give each socket response an independent physical driver.

    The pinned cart merger skips constant OB inputs and maps an aliased OB
    input onto only its last sink. Identity LUTs preserve constants and fanout
    without changing the card's truth table or rewriting the frozen shell.
    """
    design = json.loads(source)
    top = design["modules"]["cart"]
    pads = top["ports"]["plug_rdata"]["bits"]
    if len(pads) != 28 or any(type(bit) is not int for bit in pads):
        raise ValueError("card response port must expose 28 physical outputs")
    cells = top["cells"]
    signals = [bit for cell in cells.values() for bits in cell["connections"].values()
               for bit in bits if type(bit) is int]
    signals += [bit for port in top["ports"].values() for bit in port["bits"] if type(bit) is int]
    next_bit = max(signals) + 1
    for index, pad in enumerate(pads):
        outputs = [cell for cell in cells.values() if cell["type"] == "MISTRAL_OB"
                   and cell["connections"].get("PAD") == [pad]]
        name = f"fes_response_driver_{index}"
        if len(outputs) != 1 or name in cells or len(outputs[0]["connections"].get("I", [])) != 1:
            raise ValueError("card response output buffer contract changed")
        cell = outputs[0]
        # ALUT2 is the smallest routable primitive in the pinned architecture.
        cells[name] = {"type": "MISTRAL_ALUT2", "parameters": {"LUT": "1010"}, "attributes": {},
                       "port_directions": {"A": "input", "B": "input", "Q": "output"},
                       "connections": {"A": cell["connections"]["I"], "B": ["0"], "Q": [next_bit]}}
        cell["connections"]["I"] = [next_bit]
        next_bit += 1
    return (json.dumps(design, separators=(",", ":")) + "\n").encode()


def build(root: Path, shell: Path, package_path: Path, slot: int, card: str, gpu: int, *,
          cache_root: Path | None = None) -> Path:
    root, shell = root.resolve(), shell.resolve()
    inputs = card_inputs(card)
    socket = socket_for(slot)
    _, revision = _require_clean_source(root, pinned_inputs=inputs, identity_version=2)
    package = read_package(package_path)
    if (shell / "manifest.toml").read_bytes() != package.manifest_bytes or \
            (shell / "core.rbf").read_bytes() != package.payload_bytes or \
            (shell / "rom-map.json").read_bytes() != package.rom_map_bytes:
        raise ValueError("frozen producer output differs from the sealed shell package")
    bus = [i for i in package.fields["interfaces"] if i["id"] == c64_slots.INTERFACE]
    if len(bus) != 1 or (bus[0]["major"], bus[0]["minor"], bus[0]["required"]) != (1, 0, False):
        raise ValueError("shell must declare the optional Commodore 64 cartridge bus 1.0")
    shell_members = ("routed.json", "socket.qsf", "manifest.toml", "core.rbf", "rom-map.json")
    tools = shell_recipe._authenticate_c64_tools(root, cache_root)
    identities = {name: tool.identity for name, tool in tools.items()}
    closure = {path: digest((root / path).read_bytes()) for path in inputs}
    closure.update({"shell/" + name: digest((shell / name).read_bytes()) for name in shell_members})
    clocks = cart_clock_constraints(root)
    recipe = {"inputs": closure, "tools": identities, "card": card, "slot": slot,
              "region": socket.region, "cram_region": list(socket.cram), "map": c64_slots.LAYOUT,
              "slot_clock": SLOT_CLOCK, "probe_mode": slot - 1, "placer_seed": PLACER_SEED,
              "required_clocks_mhz": REQUIRED_CLOCKS_MHZ, "clock_constraints_sha256": digest(clocks)}
    recipe_sha = digest(json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode())
    output = root / "build/c64-cards" / recipe_sha
    publications = tuple(path.name for path in output.glob("*.tar"))
    _prepare_output(root, relative=output.relative_to(root), build_outputs=BUILD_OUTPUTS + publications)
    (output / "clocks.sdc").write_bytes(clocks)
    qsf = (shell / "socket.qsf").read_bytes()
    if qsf.count(f'FES_RESERVED_RECT "{socket.placement}"'.encode()) != 1:
        raise ValueError(f"shell QSF does not reserve {socket.region}")
    (output / "cart.qsf").write_bytes(qsf)
    scaffold = prepare_scaffold((shell / "routed.json").read_bytes(), slot)
    (output / "scaffold.json").write_bytes(scaffold)
    env = dict(os.environ, HIP_VISIBLE_DEVICES=str(gpu))
    commands = [
        [str(tools["yosys"].path), "-p", card_synthesis_script(card, slot, output)],
        [str(tools["nextpnr-mistral"].path), "--json", str(output / "scaffold.json"),
         "--device", "5CSEBA6U23I7", "--qsf", str(output / "cart.qsf"), "--sdc", str(output / "clocks.sdc"),
         "--freq", "52.224", "--fes-scaffold", "--fes-cart", str(output / "cart.json"),
         "--fes-cart-region", socket.region, "--fes-slot-clock", SLOT_CLOCK,
         "--fes-cram-region", ",".join(str(v) for v in socket.cram),
         "--no-pack", "--seed", str(PLACER_SEED), "--router", "gpu", "--placer-heap-timingweight", "300",
         "--rbf", str(output / "cart.rbf"), "--compress-rbf",
         "--write", str(output / "cart-routed.json"), "--report", str(output / "timing.json")],
    ]
    for name, command in zip(("synthesis", "route"), commands):
        log_path = output / (name + ".log")
        with log_path.open("w") as log:
            try:
                subprocess.run(command, cwd=root, env=env, stdout=log, stderr=subprocess.STDOUT,
                               check=True, timeout=900)
            except subprocess.TimeoutExpired as exc:
                raise ValueError(f"{name} timed out; see {log_path}") from exc
            except subprocess.CalledProcessError as exc:
                raise ValueError(f"{name} failed; see {log_path}") from exc
        if re.search(r"^\s*(?:ERROR|FATAL)\b", log_path.read_text(errors="replace"), re.MULTILINE | re.IGNORECASE):
            raise ValueError(f"{name} reported an error; see {log_path}")
        required = ("cart.json",) if name == "synthesis" else ("cart.rbf", "cart-routed.json", "timing.json")
        for artifact in required:
            path = output / artifact
            if path.is_symlink() or not path.is_file() or path.stat().st_size == 0:
                raise ValueError(f"{name} did not produce nonempty {artifact}")
        if name == "synthesis":
            mapped = output / "cart.json"
            (output / "cart-synthesized.json").write_bytes(mapped.read_bytes())
            mapped.write_bytes(materialize_response_drivers(mapped.read_bytes()))
    achieved = validate_cart_timing(json.loads((output / "timing.json").read_text()))
    validate_cart_clocks(json.loads((output / "cart-routed.json").read_text()),
                         allow_combinational=card == "probe" and slot == 1)
    if (output / "scaffold.json").read_bytes() != scaffold or (output / "cart.qsf").read_bytes() != qsf:
        raise ValueError("card scaffold or placement constraints changed during build")
    cart = (output / "cart.rbf").read_bytes()
    base, placed = rbf_load(package.payload_bytes), rbf_load(cart)
    if base.header != placed.header:
        raise ValueError("card changes shell ORAM/PRAM header")
    rect = CramRect(*socket.cram)
    changes = classify_cram_diff(base, placed, rect, include_outside_coordinates=True)
    report = {"cart_sha256": digest(cart), "cram_diff": changes, "cram_region": list(socket.cram),
              "slot": slot, "format": 1}
    (output / "cram-diff.json").write_text(json.dumps(report, sort_keys=True, indent=2) + "\n")
    if int(changes.get("bits_outside_slot", 0)):
        raise ValueError(f"card changes {changes['bits_outside_slot']} CRAM bits outside the slot {slot} "
                         f"socket; see {output / 'cram-diff.json'}")
    (output / "linked.rbf").write_bytes(rbf_save(overlay_cram(base, placed, rect), compressed=True))
    encoded = card_manifest(package, slot, cart, recipe_sha, revision)
    expansion_id = digest(b"fes-expansion-v1\0" + encoded)
    _, final_revision = _require_clean_source(root, pinned_inputs=inputs, identity_version=2)
    if final_revision != revision or any(digest((root / path).read_bytes()) != closure[path] for path in inputs):
        raise ValueError("source changed during card build")
    if any(digest((shell / name).read_bytes()) != closure["shell/" + name] for name in shell_members):
        raise ValueError("frozen shell changed during card build")
    if {n: t.identity for n, t in shell_recipe._authenticate_c64_tools(root, cache_root).items()} != identities:
        raise ValueError("authenticated compiler changed during card build")
    destination = output / (expansion_id + ".tar")
    with tarfile.open(destination, "w", format=tarfile.USTAR_FORMAT) as archive:
        for name, data in (("manifest.json", encoded), ("cart.rbf", cart)):
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(data), 0o600
            archive.addfile(info, io.BytesIO(data))
    (output / "build-summary.json").write_text(json.dumps(
        {"recipe": recipe, "expansion_id": expansion_id, "manifest": json.loads(encoded),
         "timing_mhz": achieved, "cram_diff": changes}, sort_keys=True, indent=2) + "\n")
    return destination


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--shell", type=Path, required=True, help="Commodore 64 producer output (build/fes-c64-oss)")
    parser.add_argument("--package", type=Path, required=True, help="exact sealed shell package directory")
    parser.add_argument("--slot", type=int, required=True, choices=[s.slot for s in c64_slots.SOCKETS])
    parser.add_argument("--card", default="probe", choices=sorted(CARDS))
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--gpu", type=int, default=0)
    args = parser.parse_args()
    try:
        print(build(args.root, args.shell, args.package, args.slot, args.card, args.gpu,
                    cache_root=args.cache_root))
    except (ValueError, OSError) as exc:
        print(f"build-c64-slot-card: {exc}", file=sys.stderr)
        raise SystemExit(1)
