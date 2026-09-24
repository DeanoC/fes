# SPDX-License-Identifier: GPL-2.0-or-later
"""Simulate the RAM tester against the SDRAM addon and HPS DDR models."""

import os
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-ramtest"
COMMON = ROOT / "cores" / "fes-common"


def main() -> None:
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT"):
        raise SystemExit("simulation refuses a shared toolchain cache")
    output = ROOT / "build" / "fes-ramtest-sim"
    output.mkdir(parents=True, exist_ok=True)
    sources = [
        CORE / "rtl" / "top.v",
        CORE / "rtl" / "mem_channel.v",
        CORE / "rtl" / "ram_font.v",
        CORE / "rtl" / "ram_display.v",
        CORE / "rtl" / "sdram_addon_port.v",
        CORE / "rtl" / "hps_ddr_port.v",
        COMMON / "rtl" / "fes_application_gp.v",
        COMMON / "rtl" / "fes_video_720p.v",
        CORE / "sim" / "bench.v",
        CORE / "sim" / "board_models.v",
        CORE / "sim" / "sdram_model.v",
        CORE / "sim" / "altiobuf_model.v",
        CORE / "sim" / "hps_ddr_model.v",
        CORE / "sim" / "tb.cpp",
    ]
    subprocess.run(
        [
            "verilator", "--cc", "--exe", "--build", "--top-module", "bench",             "+define+SIM",
            "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY",
            "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ",
            f"-I{COMMON / 'generated'}",
            "-Mdir", str(output),
            "-o", "sim",
            *map(str, sources),
        ],
        check=True,
    )
    subprocess.run([str(output / "sim")], check=True)


if __name__ == "__main__":
    main()
