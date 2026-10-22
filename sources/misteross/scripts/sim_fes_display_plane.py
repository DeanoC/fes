#!/usr/bin/env python3
"""Check the shared display plane's blend, latency, raster lock and fault return."""
import argparse
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default=os.environ.get("VERILATOR", "verilator"))
    args = parser.parse_args()
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT") or os.environ.get("CACHE_ROOT"):
        parser.error("simulation does not use the shared compiler cache")
    output = ROOT / "build/sim/fes-display-plane"
    output.mkdir(parents=True, exist_ok=True)
    sources = (
        "cores/fes-common/sim/display_plane_top.v", "cores/fes-common/rtl/fes_display_plane.v",
        "cores/fes-common/rtl/fes_display_cdc.v", "cores/fes-menu/rtl/fes_menu_control.v",
        "cores/fes-menu/rtl/fes_menu_video.v", "cores/fes-menu/rtl/fes_menu_reader.v",
        "cores/fes-common/rtl/fes_hps_ddr.v", "cores/fes-common/rtl/fes_hps_ddr_guard.v",
        "cores/fes-menu/sim/ddr_model.v",
    )
    subprocess.run([
        args.verilator, "--cc", "--exe", "--build", "--top-module", "display_plane_top",
        "-Wall", "-Wno-UNUSEDSIGNAL", "-Wno-PINCONNECTEMPTY", "-Wno-SYNCASYNCNET", "-Wno-DECLFILENAME",
        "--Mdir", str(output), "-CFLAGS", "-std=c++17 -O2",
        "-I" + str(ROOT / "cores/fes-common/generated"),
        *(str(ROOT / path) for path in sources),
        str(ROOT / "cores/fes-common/sim/display_plane_tb.cpp"),
    ], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vdisplay_plane_top")], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
