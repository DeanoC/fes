#!/usr/bin/env python3
"""Exercise the native physical boundary RTL and actual part producer wrapper."""
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
    for variant in ("direct", "scanlines"):
        output = ROOT / "build/sim/fes-native-socket" / variant
        output.mkdir(parents=True, exist_ok=True)
        define = ["-DFES_VIDEO_SCANLINES=1"] if variant == "scanlines" else []
        sources = ["cores/fes-coleco/rtl/coleco_native_video_socket.v",
                   "cores/fes-common/rtl/coleco_video_dpram.v",
                   "cores/fes-common/rtl/fes_native_video.v",
                   "cores/fes-common/rtl/fes_native_video_cart.v",
                   "cores/fes-common/sim/native_socket_top.v",
                   "cores/fes-common/sim/native_socket_tb.cpp"]
        subprocess.run([args.verilator, "--cc", "--exe", "--build", "-Wall",
                        "--top-module", "native_socket_top", "--Mdir", str(output),
                        "-I" + str(ROOT / "cores/fes-common/generated"), *define,
                        "-CFLAGS", "-std=c++17 -O2 " + " ".join(define),
                        *[str(ROOT / path) for path in sources]], cwd=ROOT, check=True)
        subprocess.run([str(output / "Vnative_socket_top")], check=True)


if __name__ == "__main__":
    main()
