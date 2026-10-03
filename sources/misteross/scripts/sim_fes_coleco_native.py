#!/usr/bin/env python3
"""Simulate the real OSS VDP native source and lint the optional board prototype."""
import argparse
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
COMMON = ROOT / "cores/fes-common"
COLECO = ROOT / "cores/fes-coleco"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default=os.environ.get("VERILATOR", "verilator"))
    args = parser.parse_args()
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT") or os.environ.get("CACHE_ROOT"):
        parser.error("simulation does not use the shared compiler cache")
    output = ROOT / "build/sim/fes-coleco-native"
    output.mkdir(parents=True, exist_ok=True)
    flags = ["-Wall", "-DFES_COLECO_OSS=1", "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM",
             "-Wno-WIDTHTRUNC", "-Wno-WIDTHEXPAND", "-I" + str(COMMON / "generated")]
    native = [COMMON / "rtl" / name for name in ["coleco_vdp.sv", "coleco_dpram.v",
              "coleco_video_dpram.v", "coleco_native_video.v", "fes_native_cdc.v", "fes_native_video.v"]]
    subprocess.run([args.verilator, "--cc", "--exe", "--build", "--top-module", "native_vdp_top",
                    *flags, "--Mdir", str(output), "-CFLAGS", "-std=c++17 -O2",
                    *map(str, native), str(COLECO / "sim/native_vdp_top.sv"),
                    str(COLECO / "sim/native_vdp_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vnative_vdp_top")], cwd=ROOT, check=True)
    board = [COLECO / "sim/board_models.v", COLECO / "rtl/top.v",
             COLECO / "rtl/coleco_application_gp.v", COLECO / "rtl/coleco_machine.sv"]
    board += [COMMON / "rtl" / name for name in ["fes_application_gp.v", "fes_sn76489.sv",
               "fes_audio_i2s.v", "fes_audio_output.v", "fes_z80_ce.sv", "t80pa.v"]]
    board += [COMMON / "rtl/tv80" / name for name in ["tv80_core.v", "tv80_alu.v", "tv80_mcode.v", "tv80_reg.v"]]
    # Same inherited TV80 warning classes as sim-fes-coleco-board-build.
    # Keep warnings fatal for the real VDP/native test above.
    board_flags = ["-Wno-BLKSEQ", "-Wno-SYNCASYNCNET", "-Wno-DECLFILENAME"]
    # Lint the unchanged default branch as well as both optional prototypes.
    for profile in ("legacy", "direct", "scanlines"):
        selected = ["-DFES_COLECO_NATIVE_VIDEO_DEV=1"] if profile != "legacy" else []
        if profile == "scanlines":
            selected += ["-DFES_NATIVE_SCANLINES=1"]
        subprocess.run([args.verilator, "--lint-only", "--top-module", "top", *flags, *board_flags,
                        "-Wno-PINCONNECTEMPTY", "-DTV80_REFRESH=1", *selected,
                        *map(str, native + board + [COMMON / "rtl/coleco_video_720p.v"])],
                       cwd=ROOT, check=True)
    print("Coleco legacy and native inline Direct/Scanlines board elaboration passed; no sealed producer or hardware exercised")


if __name__ == "__main__":
    main()
