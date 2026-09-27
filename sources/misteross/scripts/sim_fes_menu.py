#!/usr/bin/env python3
"""Run the menu framebuffer reader/video simulations; never program hardware."""
from pathlib import Path
import argparse
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--case", choices=("reader", "video", "pattern", "board", "ddr", "ddr-board", "ddr-disabled", "all"), default="all")
    parser.add_argument("--verilator", default="verilator")
    args = parser.parse_args()
    for case in ("reader", "video", "pattern", "board", "ddr", "ddr-board", "ddr-disabled"):
        if args.case not in (case, "all"):
            continue
        top = "fes_menu_ddr" if case == "ddr" else "top" if case in ("board", "ddr-board", "ddr-disabled") else ("fes_menu_video" if case == "pattern" else f"fes_menu_{case}")
        output = ROOT / "build/sim" / f"fes-menu-{case}"
        output.mkdir(parents=True, exist_ok=True)
        sources = [ROOT / "cores/fes-menu/rtl/fes_menu_reader.v"]
        if case in ("video", "pattern", "board", "ddr", "ddr-board", "ddr-disabled"):
            sources.append(ROOT / "cores/fes-menu/rtl/fes_menu_video.v")
            sources.append(ROOT / "cores/fes-menu/rtl/fes_menu_pattern_memory.v")
        if case in ("board", "ddr-board", "ddr-disabled"):
            sources.extend([ROOT / "cores/fes-menu/rtl/top.v", ROOT / "cores/fes-menu/sim/board_models.v"])
        if case in ("ddr", "ddr-board", "ddr-disabled"):
            sources.extend([ROOT / "cores/fes-common/rtl/fes_hps_ddr.v",
                            ROOT / "cores/fes-common/rtl/fes_hps_ddr_guard.v",
                            ROOT / "cores/fes-menu/rtl/fes_menu_ddr.v",
                            ROOT / "cores/fes-menu/sim/ddr_model.v"])
        subprocess.run([
            args.verilator, "--cc", "--exe", "--build", "--top-module", top,
            "-Wall", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL", "--Mdir", str(output),
            *(["--public-flat-rw", "-I" + str(ROOT / "cores/fes-common/generated"), "-Wno-DECLFILENAME"] if case in ("board", "ddr", "ddr-board", "ddr-disabled") else ["-GWINDOW_BASE=805306368"]),
            *(["-GTEST_PATTERN=1'b0"] if case in ("ddr-board", "ddr-disabled") else []),
            *(["-GDIAGNOSTIC_ENABLE=1'b0"] if case == "ddr-disabled" else []),
            *(["-GTEST_PATTERN=1'b1"] if case == "pattern" else []),
            *map(str, sources), str(ROOT / f"cores/fes-menu/sim/{'ddr_board' if case in ('ddr-board', 'ddr-disabled') else case}_tb.cpp"),
        ], cwd=ROOT, check=True)
        subprocess.run([str(output / f"V{top}"), *([str(output)] if case == "video" else ["--disabled"] if case == "ddr-disabled" else [])], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
