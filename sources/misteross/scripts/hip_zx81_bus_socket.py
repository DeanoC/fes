#!/usr/bin/env python3
"""Diagnostic HIP of the socketed ZX81 Z80-like edge plus 16K pack.

Does not authenticate source identity and does not seal a package. Use the
locked zx81-expansion compiler (nextpnr 74f26cc1). GPU 0 only.
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
from scripts import build_zx81_ram_expansion as cart
from scripts.cyclonev_rbf import CramRect, classify_cram_diff, overlay_cram, rbf_load, rbf_save
from scripts.fes_build_common import _require_gpu_backend
from scripts.search_placer_qor import route_after_synth
from scripts import zx81_expansion as expansion

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "build/fes-zx81-socket-bus"
CART_OUT = ROOT / "build/zx81-ram-expansion-bus"
BUILD_ID = "0123456789abcdef0123456789abcdef"
CRAM = CramRect(*cart.CRAM_REGION)
WIDE_CRAM = CramRect(cart.CRAM_REGION[0], cart.CRAM_REGION[1], 3356, cart.CRAM_REGION[3])


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


def synthesize_shell(tools: dict[str, Path], env: dict[str, str]) -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    sources = " ".join(shell.RTL_SOURCES)
    program = (
        f"read_verilog -sv -DTV80_REFRESH=1 -I cores/fes-zx81/generated -I cores/fes-zx81/rtl {sources}; "
        f"chparam -set BUILD_ID 128'h{BUILD_ID} top; "
        f"chparam -set EXPANSION_SOCKET 1 top; "
        f"synth_intel_alm -nolutram -nodsp -top top; "
        f"stat; write_json {OUT.relative_to(ROOT).as_posix()}/synth.json"
    )
    run([str(tools["yosys"]), "-p", program], OUT / "yosys.log", env)
    if not (OUT / "synth.json").is_file():
        raise SystemExit("yosys did not write synth.json")
    expansion.prepare_shell_netlist(OUT / "synth.json")
    (OUT / "socket.qsf").write_text(expansion.shell_qsf((ROOT / shell.QSF).read_text()))
    print("shell synthesis and socket rename OK", flush=True)


def route_shell(tools: dict[str, Path], env: dict[str, str]) -> None:
    winner = route_after_synth(
        nextpnr=tools["nextpnr"],
        fixture=OUT / "synth.json",
        dest=OUT,
        device=shell.TARGET,
        qsf=OUT / "socket.qsf",
        sdc=ROOT / shell.SDC,
        freq="74.25",
        seeds=shell.PLACER_SEEDS,
        weights=shell.PLACER_FIRST_PASS_WEIGHTS,
        critexp=shell.PLACER_CRITICALITY_EXPONENT,
        budget=len(shell.PLACER_SEEDS) * len(shell.PLACER_FIRST_PASS_WEIGHTS),
        mode="first-pass",
        extra=("--router", "gpu"),
        timeout=1800,
        required=shell.PLACER_QOR_CLOCKS,
        gpu_devices=(0,),
        env=env,
    )
    _require_gpu_backend((OUT / "nextpnr.log").read_text(errors="replace"))
    print(
        f"shell route PASS seed={winner.seed} weight={winner.weight} "
        f"worst_ratio={winner.worst_ratio:.4f}",
        flush=True,
    )


def validate_overlay_timing(timing: dict) -> None:
    # Diagnostic HIP only. Official sealer still requires the net name pixel_clk.
    # Flattening zx81_hdmi_i2s leaves the 74.25 MHz net as hdmi_i2s.pixel_clk.
    fmax = timing.get("fmax")
    if not isinstance(fmax, dict):
        raise ValueError("overlay timing has no fmax table")
    names = set(fmax)
    if names != {"clk_sys", "pixel_clk"} and names != {"clk_sys", "hdmi_i2s.pixel_clk"}:
        raise ValueError(f"unexpected overlay clocks: {sorted(names)}")
    _, _, sys_hz = shell._frequency_row(fmax, 52.0, "system clock", "clk_sys")
    _, _, pix_hz = shell._frequency_row(fmax, 74.25, "pixel clock", "pixel_clk")
    if sys_hz < 52.0 or pix_hz < 74.25:
        raise ValueError("overlay clocks below required 52/74.25 MHz")


def compose_cart(tools: dict[str, Path], env: dict[str, str]) -> None:
    if CART_OUT.exists():
        shutil.rmtree(CART_OUT)
    CART_OUT.mkdir(parents=True)
    clock_constraints = cart.cart_clock_constraints(ROOT)
    (CART_OUT / "clocks.sdc").write_bytes(clock_constraints)
    sources = " ".join(cart.SOURCES)
    run(
        [
            str(tools["yosys"]),
            "-p",
            f"read_verilog -sv -I cores/fes-zx81/rtl {sources}; "
            f"synth_intel_alm -nolutram -nodsp -top cart; "
            f"write_json {CART_OUT.relative_to(ROOT).as_posix()}/cart.json",
        ],
        CART_OUT / "synthesis.log",
        env,
    )
    run(
        [
            str(tools["nextpnr"]),
            "--json", str(OUT / "routed.json"),
            "--device", shell.TARGET,
            "--qsf", str(OUT / "socket.qsf"),
            "--sdc", str(CART_OUT / "clocks.sdc"),
            "--freq", "52",
            "--fes-scaffold",
            "--fes-cart", str(CART_OUT / "cart.json"),
            "--fes-slot-clock", "clk_sys",
            "--fes-cram-region", ",".join(str(value) for value in cart.CRAM_REGION),
            "--no-pack",
            "--seed", str(cart.PLACER_SEED),
            "--router", "gpu",
            "--gpu-device", "0",
            "--rbf", str(CART_OUT / "cart.rbf"),
            "--compress-rbf",
            "--write", str(CART_OUT / "cart-routed.json"),
            "--report", str(CART_OUT / "timing.json"),
        ],
        CART_OUT / "route.log",
        env,
    )
    _require_gpu_backend((CART_OUT / "route.log").read_text(errors="replace"))
    validate_overlay_timing(json.loads((CART_OUT / "timing.json").read_text()))
    base = rbf_load((OUT / "core.rbf").read_bytes())
    placed = rbf_load((CART_OUT / "cart.rbf").read_bytes())
    if base.header != placed.header:
        raise SystemExit("cart changed shell ORAM/PRAM header")
    changes = classify_cram_diff(base, placed, CRAM)
    rect = CRAM
    if changes["bits_outside_slot"]:
        wide = classify_cram_diff(base, placed, WIDE_CRAM)
        print(
            f"16K library CRAM outside={changes['bits_outside_slot']} "
            f"wide outside={wide['bits_outside_slot']}",
            flush=True,
        )
        if wide["bits_outside_slot"]:
            raise SystemExit(f"cart changes outside diagnostic slot: {wide}")
        rect = WIDE_CRAM
        changes = wide
    (CART_OUT / "linked.rbf").write_bytes(rbf_save(overlay_cram(base, placed, rect), compressed=True))
    (CART_OUT / "cram-diff.json").write_text(json.dumps(changes, indent=2, sort_keys=True) + "\n")
    print(f"cart overlay PASS outside={changes['bits_outside_slot']} linked={CART_OUT / 'linked.rbf'}", flush=True)


def main() -> int:
    tools = {
        "yosys": Path(sys.argv[1]),
        "nextpnr": Path(sys.argv[2]),
    }
    env = dict(os.environ)
    env["HIP_VISIBLE_DEVICES"] = "0"
    env.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
    env.pop("CACHE_ROOT", None)
    synthesize_shell(tools, env)
    route_shell(tools, env)
    compose_cart(tools, env)
    print("HIP_ZX81_BUS_SOCKET_OK", flush=True)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
