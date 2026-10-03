#!/usr/bin/env python3
"""Check native indexed video capture, asynchronous transfer and HDMI scanout."""
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
    output = ROOT / "build/sim/fes-native-video"
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run([
        args.verilator, "--cc", "--exe", "--build", "--top-module", "native_video_top",
        "-Wall", "--Mdir", str(output), "-CFLAGS", "-std=c++17 -O2",
        "-I" + str(ROOT / "cores/fes-common/generated"),
        str(ROOT / "cores/fes-common/rtl/coleco_video_dpram.v"),
        str(ROOT / "cores/fes-common/rtl/fes_native_video.v"),
        str(ROOT / "cores/fes-common/rtl/fes_native_cdc.v"),
        str(ROOT / "cores/fes-common/rtl/coleco_native_video.v"),
        str(ROOT / "cores/fes-common/sim/native_video_top.v"),
        str(ROOT / "cores/fes-common/sim/native_video_tb.cpp"),
    ], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vnative_video_top")], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
