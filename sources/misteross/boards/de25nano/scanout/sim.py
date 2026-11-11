#!/usr/bin/env python3
"""Actual endpoint/video RTL tests; no hardware, fake production mapping or HPS model."""
from pathlib import Path
import argparse
import subprocess
from build import load

HERE = Path(__file__).resolve().parent
MISTEROSS = HERE.parents[2]
DISPLAY = load(HERE.parent / "display/build.py", "scanout_sim_display")
ROOT = MISTEROSS / "build/sim/de25-scanout"


def run(case, top, rtl, bench, extra=(), shared=True, arguments=()):
    output = ROOT / case; output.mkdir(parents=True, exist_ok=True)
    reader, _ = DISPLAY.retarget_menu_ram((MISTEROSS / "boards/de25nano/shared-rtl/fes_menu_reader.v").read_bytes())
    adapted = output / "fes_menu_reader_de25.v"; adapted.write_bytes(reader)
    subprocess.run(["verilator", "--cc", "--exe", "--build", "--public-flat-rw", "-O3", "-Wall",
        "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL", "-Wno-DECLFILENAME", "--top-module", top,
        # Qualification can assert between independent domain clock edges.
        "-Wno-SYNCASYNCNET",
        "--Mdir", str(output), "-I" + str(HERE), "-CFLAGS", "-O2 -I" + str(HERE),
        *extra, *([str(adapted), str(MISTEROSS / "boards/de25nano/shared-rtl/fes_menu_video.v")] if shared else []),
        *map(str, rtl), str(HERE / bench)], check=True, timeout=180)
    subprocess.run([str(output / ("V" + top)), *arguments], check=True, timeout=30)


parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--case", choices=("frame-ack", "endpoint", "latency", "board", "guard", "session", "all"), default="all")
parser.add_argument("--output-root", type=Path, default=ROOT,
                    help="private simulation build directory (default: normal ignored build/sim path)")
args = parser.parse_args()
ROOT = args.output_root.resolve()
if args.case in ("frame-ack", "all"):
    run("frame-ack", "fes_menu_video", [MISTEROSS / "cores/fes-menu/rtl/fes_menu_pattern_memory.v"],
        "frame_ack_tb.cpp", ("-GTEST_PATTERN=1'b1", "-GCOMPLETE_FRAME_ACK=1'b1"))
if args.case in ("endpoint", "all"):
    run("endpoint", "fes_de25_scanout", [HERE / "fes_de25_scanout.v"], "endpoint_tb.cpp", ("-GBUILD_ID=128'h0123456789abcdeffedcba9876543210",))
if args.case in ("latency", "all"):
    identity = ("-GBUILD_ID=128'h0123456789abcdeffedcba9876543210",)
    run("latency-old-window", "fes_de25_scanout", [HERE / "fes_de25_scanout.v"], "endpoint_tb.cpp",
        (*identity, "-GMAX_BURST_BEATS=8'd16", "-GMAX_OUTSTANDING_BEATS=9'd256"), arguments=("--latency-old",))
    if args.case == "latency":
        run("latency-new-window", "fes_de25_scanout", [HERE / "fes_de25_scanout.v"], "endpoint_tb.cpp",
            identity, arguments=("--latency-new",))
if args.case in ("board", "all"):
    run("board", "fes_de25_scanout_board", [HERE / "fes_de25_scanout_board.v",
        HERE / "board_sim_models.v",
        HERE.parent / "display/sim_models.v", HERE.parent / "hps/fes_de25_top_reset.v"], "board_tb.cpp", shared=False)

if args.case in ("guard", "all"):
    run("guard", "fes_de25_scanout_guard", [HERE / "fes_de25_scanout_guard.v"],
        "guard_tb.cpp", ("-GTIMEOUT_CYCLES=16",), shared=False)

if args.case in ("session", "all"):
    common = MISTEROSS / "cores/fes-common"
    run("session", "fes_de25_session_display", [HERE / "fes_de25_session_display.v",
        HERE / "fes_de25_scanout.v", MISTEROSS / "boards/de25nano/shared-rtl/fes_display_plane.v",
        common / "rtl/fes_display_cdc.v", MISTEROSS / "boards/de25nano/shared-rtl/fes_menu_control.v"],
        "session_tb.cpp", ("-I" + str(common / "generated"),))
