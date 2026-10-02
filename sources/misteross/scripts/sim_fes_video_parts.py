#!/usr/bin/env python3
"""Simulate timed video parts through both shell boundaries; never use hardware."""
from pathlib import Path
import argparse
import os
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default=os.environ.get("VERILATOR", "verilator"))
    args = parser.parse_args()
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT") or os.environ.get("CACHE_ROOT"):
        parser.error("simulation does not use the shared compiler cache")
    output = ROOT / "build/sim/fes-video-parts"
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run([
        args.verilator, "--cc", "--exe", "--build", "--top-module", "video_part_top",
        "-Wall", "--Mdir", str(output),
        "-I" + str(ROOT / "cores/fes-common/generated"),
        str(ROOT / "cores/fes-common/rtl/fes_video_part_direct.v"),
        str(ROOT / "cores/fes-common/rtl/fes_video_part_scanlines.v"),
        str(ROOT / "cores/fes-coleco/rtl/coleco_video_socket.v"),
        str(ROOT / "cores/fes-common/sim/video_part_top.v"),
        str(ROOT / "cores/fes-common/sim/video_part_tb.cpp"),
    ], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vvideo_part_top")], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
