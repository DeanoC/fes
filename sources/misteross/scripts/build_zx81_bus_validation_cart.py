#!/usr/bin/env python3
"""Build a RAM or Zon X AY validation cart against a sealed ZX81 bus shell.

The shell is never placed or routed here. Launch-time composition uses the
misteross Go linker and requires neither this script nor the compiler.

These are bounded bus consumers, not the shell's public interface. Zon X implements the original write-only AY8912 board.
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
from scripts import build_fes_zx81_oss as shell_recipe
from scripts.core_package import read_package
from scripts.fes_build_common import _authenticate_tools, _prepare_output, _require_clean_source
from scripts.cyclonev_rbf import rbf_load, rbf_save, overlay_cram, classify_cram_diff, CramRect

ROOT = Path(__file__).resolve().parents[1]
SOURCES = ("cores/fes-zx81/rtl/zx81_dpram.v", "cores/fes-zx81/rtl/zx81_ram_pack.v", "cores/fes-zx81/expansions/ram16k.v")
INPUTS = SOURCES + ("cores/fes-zx81/rtl/zx81_bus_pack.vh", "scripts/build_zx81_bus_validation_cart.py", "toolchains/zx81-expansion.lock", "scripts/cyclonev_rbf.py", "scripts/core_package.py", "scripts/rom_map.py", "scripts/fes_build_common.py", "scripts/build_fes_zx81_oss.py", shell_recipe.SDC)
CART_SOURCES = {"ram16k": SOURCES, "zonx": ("cores/fes-zx81/expansions/zonx.v", "cores/fes-zx81/expansions/zonx_ay.v")}
BUILD_OUTPUTS = ("cart.json", "cart.rbf", "cart-routed.json", "timing.json", "scaffold.json",
                 "linked.rbf", "build-summary.json", "synthesis.log", "route.log", "clocks.sdc")
PLACER_SEED = 2
REQUIRED_CLOCKS_MHZ = {"clk_sys": 52.224, "pixel_clk": 74.25, "audio_clk": 12.288}
CRAM_REGION = (1769, 32, 2806, 7024)  # fes.zx81-bus.socket/2, half-open

def digest(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()

def cart_clock_constraints(root: Path) -> bytes:
    # --no-pack restores the routed nets but does not derive PLL constraints.
    # These are the declared shell frequencies, never its achieved Fmax.
    text = (root / shell_recipe.SDC).read_text() + "\n# Frozen ZX81 PLL output clocks.\n"
    for name, frequency in REQUIRED_CLOCKS_MHZ.items():
        text += f"create_clock -name {name} -period {1000 / frequency:.12f} [get_nets {{{name}}}]\n"
    return text.encode()

def validate_cart_timing(timing: dict) -> None:
    fmax = timing.get("fmax")
    if not isinstance(fmax, dict) or set(fmax) != set(REQUIRED_CLOCKS_MHZ):
        raise ValueError("cart timing must report exactly the system, pixel and audio clocks")
    for name, expected in REQUIRED_CLOCKS_MHZ.items():
        # Shared validation accounts for nextpnr's picosecond quantization,
        # rejects non-finite fields and requires achieved >= reported constraint.
        _, _, achieved = shell_recipe._frequency_row(fmax, expected, name, name)
        if achieved < expected:
            raise ValueError(f"cart {name} timing is below required {expected:g} MHz")

def prepare_scaffold(source: bytes) -> bytes:
    """Restore the second physical PLL output omitted from routed JSON.

    The frozen pin map contains the old scalar outclk[0] alias, but nextpnr's
    no-pack replay expects the actual outclk[1] audio branch as a connection.
    This is the same bounded replay repair used by Apple II and Coleco carts.
    """
    design = json.loads(source)
    top = design['modules']['top']
    cells = top['cells']
    pll = cells['system_clock.pll']
    if pll.get('type') != 'altera_pll' or set(pll.get('connections', {})) != {'outclk', 'locked', 'refclk'}:
        raise ValueError('ZX81 frozen system/audio PLL connection contract changed')
    mapping = json.loads(bytes.fromhex(pll['attributes']['FES_PINMAP_V1']))
    aliases = {f'outclk[{bit}]': [0, f'outclk[{bit}]'] for bit in range(2)}
    if mapping.get('count') != 6 or any(mapping['pins'].get(name) != value for name, value in aliases.items()):
        raise ValueError('ZX81 frozen system/audio PLL pin map changed')
    output1 = top['netnames'].get('system_clock.pll_outclk_1', {}).get('bits')
    audio_clock = top['netnames'].get('audio_clk', {}).get('bits')
    buffers = [cell for cell in cells.values() if cell.get('type') == 'MISTRAL_CLKBUF'
               and cell.get('connections', {}).get('Q') == audio_clock]
    if (not isinstance(output1, list) or len(output1) != 1 or type(output1[0]) is not int
            or len(buffers) != 1 or buffers[0]['connections'].get('A') != output1):
        raise ValueError('ZX81 frozen audio PLL net changed')
    pll['connections']['outclk[1]'] = output1
    pll['port_directions']['outclk[1]'] = 'output'
    del mapping['pins']['outclk[0]']
    mapping['count'] = len(mapping['pins'])
    pll['attributes']['FES_PINMAP_V1'] = json.dumps(mapping, sort_keys=True).encode().hex()
    return (json.dumps(design, sort_keys=True, separators=(',', ':')) + '\n').encode()

def build(root: Path, shell: Path, package_path: Path, gpu: int, *, cache_root: Path | None = None,
          cart_kind: str = "ram16k") -> Path:
    if cart_kind not in CART_SOURCES:
        raise ValueError(f"unknown ZX81 cart: {cart_kind}")
    sources = CART_SOURCES[cart_kind]
    inputs = sources + INPUTS[len(SOURCES):]
    root, shell = root.resolve(), shell.resolve()
    _, revision = _require_clean_source(root, pinned_inputs=inputs, identity_version=2)
    package = read_package(package_path)
    if (shell / "manifest.toml").read_bytes() != package.manifest_bytes or (shell / "core.rbf").read_bytes() != package.payload_bytes:
        raise ValueError("frozen producer output differs from sealed shell package")
    shell_members = ("routed.json", "socket.qsf", "manifest.toml", "core.rbf")
    if package.fields["format"] == 3:
        if (shell / "rom-map.json").read_bytes() != package.rom_map_bytes:
            raise ValueError("frozen producer ROM map differs from sealed shell package")
        shell_members += ("rom-map.json",)
    slot = [item for item in package.fields["interfaces"] if item["id"] == "fes.expansion.zx81-bus"]
    if len(slot) != 1 or slot[0]["major"] != 2 or slot[0]["minor"] != 0 or slot[0]["required"]:
        raise ValueError("shell must declare the optional ZX81 expansion bus 2.0")
    for name in ("routed.json", "socket.qsf"):
        if not (shell / name).is_file():
            raise ValueError(f"shell producer directory requires {name}")
    tools = _authenticate_tools(root, cache_root=cache_root, lock_path=root / shell_recipe.SOCKET_TOOLCHAIN_LOCK,
        expected_commits=shell_recipe.SOCKET_TOOL_COMMITS, toolchain_root=root / "build/toolchain/zx81-expansion")
    identities = {name: tool.identity for name, tool in tools.items()}
    closure = {path: digest((root / path).read_bytes()) for path in inputs}
    closure.update({"shell/" + name: digest((shell / name).read_bytes()) for name in shell_members})
    clock_constraints = cart_clock_constraints(root)
    scaffold = prepare_scaffold((shell / 'routed.json').read_bytes())
    recipe = {"cart_kind": cart_kind, "inputs": closure, "tools": identities, "slot_clock": "clk_sys", "map": "fes.zx81-bus.socket/2",
              "placer_seed": PLACER_SEED, "required_clocks_mhz": REQUIRED_CLOCKS_MHZ,
              "cram_region": CRAM_REGION,
              "clock_constraints_sha256": digest(clock_constraints),
              "scaffold_sha256": digest(scaffold)}
    recipe_sha = digest(json.dumps(recipe, sort_keys=True, separators=(",", ":")).encode())
    output_name = "zx81-bus-validation-cart" if cart_kind == "ram16k" else "zx81-zonx-cart"
    output = root / "build" / output_name / recipe_sha
    # A recipe directory can be retried. Remove both intermediate evidence and
    # prior publications before invoking either compiler, never after failure.
    publications = tuple(path.name for path in output.glob("*.tar"))
    _prepare_output(root, relative=output.relative_to(root),
                    build_outputs=BUILD_OUTPUTS + publications)
    (output / "clocks.sdc").write_bytes(clock_constraints)
    (output / "scaffold.json").write_bytes(scaffold)
    env = dict(os.environ, HIP_VISIBLE_DEVICES=str(gpu))
    commands = [
        [str(tools["yosys"].path), "-p", f"read_verilog -sv -DSYNTHESIS=1 -I cores/fes-zx81/rtl {' '.join(sources)}; synth_intel_alm -nolutram -nodsp -top cart; write_json {output / 'cart.json'}"],
        [str(tools["nextpnr-mistral"].path), "--json", str(output / "scaffold.json"), "--device", "5CSEBA6U23I7",
         "--qsf", str(shell / "socket.qsf"), "--sdc", str(output / "clocks.sdc"), "--freq", "52.224",
         "--fes-scaffold", "--fes-cart", str(output / "cart.json"), "--fes-slot-clock", "clk_sys",
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
    if (output / "scaffold.json").read_bytes() != scaffold:
        raise ValueError("frozen scaffold changed during cart build")
    cart = (output / "cart.rbf").read_bytes()
    base, placed = rbf_load(package.payload_bytes), rbf_load(cart)
    rect = CramRect(*CRAM_REGION)
    if base.header != placed.header:
        raise ValueError("cart changes shell ORAM/PRAM header")
    changes = classify_cram_diff(base, placed, rect)
    if changes["bits_outside_slot"]:
        raise ValueError(f"cart changes outside reserved slot: {changes}")
    (output / "linked.rbf").write_bytes(rbf_save(overlay_cram(base, placed, rect), compressed=True))
    manifest = {"cart_sha256": digest(cart), "cart_size": len(cart), "device": "5CSEBA6U23I7", "format": 1,
        "map": "fes.zx81-bus.socket/2", "recipe_sha256": recipe_sha, "revision": revision,
        "shell_build_id": package.fields["build"]["id"], "shell_package_id": package.package_id,
        "shell_sha256": digest(package.payload_bytes), "slot": "fes.expansion.zx81-bus", "slot_major": 2, "slot_minor": 0}
    encoded = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    expansion_id = digest(b"fes-expansion-v1\0" + encoded)
    _, final_revision = _require_clean_source(root, pinned_inputs=inputs, identity_version=2)
    if final_revision != revision or any(digest((root / path).read_bytes()) != closure[path] for path in inputs):
        raise ValueError("source changed during cart build")
    for name in shell_members:
        if digest((shell / name).read_bytes()) != closure["shell/" + name]:
            raise ValueError("frozen shell changed during cart build")
    final_tools = _authenticate_tools(root, cache_root=cache_root, lock_path=root / shell_recipe.SOCKET_TOOLCHAIN_LOCK,
        expected_commits=shell_recipe.SOCKET_TOOL_COMMITS, toolchain_root=root / "build/toolchain/zx81-expansion")
    if {name: tool.identity for name, tool in final_tools.items()} != identities:
        raise ValueError("authenticated compiler changed during cart build")
    destination = output / (expansion_id + ".tar")
    with tarfile.open(destination, "w", format=tarfile.USTAR_FORMAT) as archive:
        for name, data in (("manifest.json", encoded), ("cart.rbf", cart)):
            info = tarfile.TarInfo(name); info.size = len(data); info.mode = 0o600
            archive.addfile(info, io.BytesIO(data))
    (output / "build-summary.json").write_text(json.dumps({"recipe": recipe, "expansion_id": expansion_id, "manifest": manifest, "cram_diff": changes}, sort_keys=True, indent=2) + "\n")
    return destination

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--shell", type=Path, required=True, help="socket producer output with sealed package and routed netlist")
    parser.add_argument("--package", type=Path, required=True, help="exact sealed shell package")
    parser.add_argument("--cache-root", type=Path, help="shared authenticated compiler cache")
    parser.add_argument("--gpu", type=int, default=0)
    parser.add_argument("--cart", choices=tuple(CART_SOURCES), default="ram16k",
                        help="bounded bus consumer; zonx is the write-only AY8912 sound board")
    args = parser.parse_args()
    print(build(args.root, args.shell, args.package, args.gpu, cache_root=args.cache_root, cart_kind=args.cart))
