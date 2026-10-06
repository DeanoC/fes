# SPDX-License-Identifier: GPL-2.0-or-later
"""Simulate the RAM tester against the SDRAM addon and HPS DDR models."""

import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-ramtest"
COMMON = ROOT / "cores" / "fes-common"


def pattern_counts() -> None:
    """The pattern table includes a mismatch on the final word."""
    output = ROOT / "build" / "fes-ramtest-pattern-count"
    output.mkdir(parents=True, exist_ok=True)
    sources = [
        CORE / "rtl" / "mem_channel.v",
        CORE / "rtl" / "pattern_latch.v",
        CORE / "sim" / "pattern_count_bench.v",
        CORE / "sim" / "pattern_count_tb.cpp",
    ]
    subprocess.run(
        [
            "verilator", "--cc", "--exe", "--build", "--top-module", "pattern_count_bench",
            "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY",
            "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ",
            "-Mdir", str(output),
            "-o", "sim",
            *map(str, sources),
        ],
        check=True,
    )
    subprocess.run([str(output / "sim")], check=True)


def main() -> None:
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT"):
        raise SystemExit("simulation refuses a shared toolchain cache")
    pattern_counts()
    output = ROOT / "build" / "fes-ramtest-sim"
    output.mkdir(parents=True, exist_ok=True)
    byte_output = output / "byte"
    byte_output.mkdir(exist_ok=True)
    subprocess.run([
        "verilator", "--cc", "--exe", "--build", "--top-module", "byte_bench",
        "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDSIGNAL",
        "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ", "-Mdir", str(byte_output), "-o", "sim",
        *map(str, [CORE / "rtl" / "sdram_byte_lane.v", CORE / "rtl" / "sdram_addon_port.v",
                   CORE / "sim" / "sdram_model.v", CORE / "sim" / "board_models.v",
                   CORE / "sim" / "byte_bench.v", CORE / "sim" / "byte_tb.cpp"]),
    ], check=True)
    subprocess.run([str(byte_output / "sim")], check=True)
    sources = [
        CORE / "rtl" / "top.v",
        CORE / "rtl" / "mem_channel.v",
        CORE / "rtl" / "sdram_byte_lane.v",
        CORE / "rtl" / "pattern_latch.v",
        CORE / "rtl" / "ram_font.v",
        CORE / "rtl" / "ram_display.v",
        CORE / "rtl" / "sdram_addon_port.v",
        CORE / "rtl" / "ddr_channel.v",
        CORE / "rtl" / "ddr_rates.v",
        COMMON / "rtl" / "fes_hps_ddr.v",
        COMMON / "rtl" / "fes_hps_ddr_guard.v",
        COMMON / "rtl" / "fes_application_gp.v",
        COMMON / "rtl" / "fes_video_720p.v",
        CORE / "sim" / "bench.v",
        CORE / "sim" / "board_models.v",
        CORE / "sim" / "sdram_model.v",
        CORE / "sim" / "altiobuf_model.v",
        CORE / "sim" / "hps_ddr_model.v",
        CORE / "sim" / "tb.cpp",
    ]
    # The second build adds the native flow's SDRAM IO output registers: one
    # more cycle to the chip and a later read capture (DeanoC/nextpnr#135).
    for build, defines in ((output, ()), (output.with_name(output.name + "-io-registers"), ("+define+RAM_SDRAM_IO_REGISTERS",))):
        build.mkdir(parents=True, exist_ok=True)
        subprocess.run(
            [
                "verilator", "--cc", "--exe", "--build", "--top-module", "bench",             "+define+SIM",
                *defines,
                "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY",
                "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ",
                f"-I{COMMON / 'generated'}",
                "-Mdir", str(build),
                "-o", "sim",
                *map(str, sources),
            ],
            check=True,
        )
        subprocess.run([str(build / "sim")], check=True)


if __name__ == "__main__":
    main()
