#!/usr/bin/env python3
"""Run the menu framebuffer reader/video simulations; never program hardware."""
from pathlib import Path
import argparse
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", choices=("reader", "video", "all"), default="all")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args()
    for case in ("reader", "video"):
        if args.case not in (case, "all"):
            continue
        top = f"fes_menu_{case}"
        output = ROOT / "build/sim" / f"fes-menu-{case}"
        output.mkdir(parents=True, exist_ok=True)
        sources = [ROOT / "cores/fes-menu/rtl/fes_menu_reader.v"]
        if case == "video":
            sources.append(ROOT / "cores/fes-menu/rtl/fes_menu_video.v")
        subprocess.run([
            args.verilator, "--cc", "--exe", "--build", "--top-module", top,
            "-Wall", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL", "--Mdir", str(output), "-GWINDOW_BASE=805306368",
            *map(str, sources), str(ROOT / f"cores/fes-menu/sim/{case}_tb.cpp"),
        ], cwd=ROOT, check=True)
        subprocess.run([str(output / f"V{top}"), *([str(output)] if case == "video" else [])], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
