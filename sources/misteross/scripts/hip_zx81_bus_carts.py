#!/usr/bin/env python3
"""Diagnostic HIP of Zon X and QS carts onto the already-routed ZX81 bus shell.

Reuses build/fes-zx81-socket-bus. Does not reseal fes.zx81. GPU 0 only.
A two-bit leak outside the library CRAM rect uses the same diagnostic x1=3356
as the 16K pack; that is not a library map bump.
"""
from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_zx81_oss as shell
from scripts import build_zx81_ram_expansion as ram
from scripts.cyclonev_rbf import CramRect, classify_cram_diff, overlay_cram, rbf_load, rbf_save
from scripts.fes_build_common import _require_gpu_backend
from scripts.hip_zx81_bus_socket import validate_overlay_timing

ROOT = Path(__file__).resolve().parents[1]
SHELL_OUT = ROOT / "build/fes-zx81-socket-bus"
LIBRARY_CRAM = CramRect(*ram.CRAM_REGION)
WIDE_CRAM = CramRect(ram.CRAM_REGION[0], ram.CRAM_REGION[1], 3356, ram.CRAM_REGION[3])

CARTS = {
    "zonx": {
        "out": ROOT / "build/zx81-zonx-bus",
        "sources": ("cores/fes-zx81/expansions/zonx.v",),
    },
    "qs": {
        "out": ROOT / "build/zx81-qs-bus",
        "sources": (
            "cores/fes-zx81/rtl/zx81_dpram.v",
            "cores/fes-zx81/expansions/qs_chrs.v",
        ),
    },
}


def run(command: list[str], log: Path, env: dict[str, str] | None = None) -> None:
    log.parent.mkdir(parents=True, exist_ok=True)
    print("+", " ".join(command), flush=True)
    with log.open("w") as handle:
        result = subprocess.run(
            command, cwd=ROOT, env=env, stdout=handle, stderr=subprocess.STDOUT, check=False
        )
    text = log.read_text(errors="replace")
    if result.returncode != 0 or "ERROR" in text.split("Program finished", 1)[0]:
        raise SystemExit(f"command failed ({result.returncode}); see {log}")


def compose_cart(name: str, tools: dict[str, Path], env: dict[str, str]) -> None:
    spec = CARTS[name]
    out: Path = spec["out"]
    if out.exists():
        shutil.rmtree(out)
    out.mkdir(parents=True)
    (out / "clocks.sdc").write_bytes(ram.cart_clock_constraints(ROOT))
    sources = " ".join(spec["sources"])
    run(
        [
            str(tools["yosys"]),
            "-p",
            f"read_verilog -sv -DSYNTHESIS=1 -I cores/fes-zx81/rtl -I cores/fes-zx81/expansions {sources}; "
            f"synth_intel_alm -nolutram -nodsp -top cart; "
            f"write_json {out.relative_to(ROOT).as_posix()}/cart.json",
        ],
        out / "synthesis.log",
        env,
    )
    run(
        [
            str(tools["nextpnr"]),
            "--json", str(SHELL_OUT / "routed.json"),
            "--device", shell.TARGET,
            "--qsf", str(SHELL_OUT / "socket.qsf"),
            "--sdc", str(out / "clocks.sdc"),
            "--freq", "52",
            "--fes-scaffold",
            "--fes-cart", str(out / "cart.json"),
            "--fes-slot-clock", "clk_sys",
            "--fes-cram-region", ",".join(str(value) for value in ram.CRAM_REGION),
            "--no-pack",
            "--seed", str(ram.PLACER_SEED),
            "--router", "gpu",
            "--gpu-device", "0",
            "--rbf", str(out / "cart.rbf"),
            "--compress-rbf",
            "--write", str(out / "cart-routed.json"),
            "--report", str(out / "timing.json"),
        ],
        out / "route.log",
        env,
    )
    _require_gpu_backend((out / "route.log").read_text(errors="replace"))
    validate_overlay_timing(json.loads((out / "timing.json").read_text()))
    base = rbf_load((SHELL_OUT / "core.rbf").read_bytes())
    placed = rbf_load((out / "cart.rbf").read_bytes())
    if base.header != placed.header:
        raise SystemExit(f"{name} cart changed shell ORAM/PRAM header")
    changes = classify_cram_diff(base, placed, LIBRARY_CRAM)
    rect = LIBRARY_CRAM
    if changes["bits_outside_slot"]:
        wide = classify_cram_diff(base, placed, WIDE_CRAM)
        print(
            f"{name} library CRAM outside={changes['bits_outside_slot']} "
            f"wide outside={wide['bits_outside_slot']}",
            flush=True,
        )
        if wide["bits_outside_slot"]:
            raise SystemExit(f"{name} cart changes outside diagnostic slot: {wide}")
        rect = WIDE_CRAM
        changes = wide
    (out / "linked.rbf").write_bytes(rbf_save(overlay_cram(base, placed, rect), compressed=True))
    (out / "cram-diff.json").write_text(json.dumps(changes, indent=2, sort_keys=True) + "\n")
    print(
        f"{name} overlay PASS outside={changes['bits_outside_slot']} "
        f"rect={rect} linked={out / 'linked.rbf'}",
        flush=True,
    )


def main() -> int:
    tools = {"yosys": Path(sys.argv[1]), "nextpnr": Path(sys.argv[2])}
    names = sys.argv[3:] or list(CARTS)
    for name in names:
        if name not in CARTS:
            raise SystemExit(f"unknown cart {name}")
    if not (SHELL_OUT / "routed.json").is_file() or not (SHELL_OUT / "core.rbf").is_file():
        raise SystemExit(f"missing routed ZX81 bus shell in {SHELL_OUT}")
    env = dict(os.environ)
    env["HIP_VISIBLE_DEVICES"] = "0"
    env.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
    env.pop("CACHE_ROOT", None)
    for name in names:
        compose_cart(name, tools, env)
    print("HIP_ZX81_BUS_CARTS_OK", " ".join(names), flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
