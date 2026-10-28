#!/usr/bin/env python3
"""Test DE25 shell with real generic video/reset RTL and modeled board IP."""
from pathlib import Path
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
for mode in ("idle", "menu-pattern"):
    output = ROOT / "build/sim" / ("de25-" + mode)
    output.mkdir(parents=True, exist_ok=True)
    sources = [HERE / "top.v", HERE / "sim_models.v", HERE.parent / "hps/fes_de25_top_reset.v"]
    names = ["fes_splash_core"] if mode == "idle" else ["fes_menu_reader", "fes_menu_video", "fes_menu_pattern_memory"]
    sources += [ROOT / "cores" / ("fes-splash" if mode == "idle" else "fes-menu") / "rtl" / (name + ".v") for name in names]
    subprocess.run(["verilator", "--cc", "--exe", "--build", "--public-flat-rw", "-O3",
        "--top-module", "de25nano_display", "-GMENU_PATTERN=1'b" + str(int(mode == "menu-pattern")),
        "-CFLAGS", "-O2 -DMENU_PATTERN=" + str(int(mode == "menu-pattern")),
        "--Mdir", str(output), *map(str, sources), str(HERE / "display_tb.cpp")], check=True)
    subprocess.run([str(output / "Vde25nano_display")], check=True)
