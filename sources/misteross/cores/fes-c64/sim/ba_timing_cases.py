#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Replay BA preparation at native, boundary, fractional and fast clock ratios."""
import argparse
from pathlib import Path
import subprocess

CASES = (
    ("native", 52224000, 985248),
    ("boundary29", 29000000, 1000000),
    ("fraction29", 29500000, 1000000),
    ("fallback28", 28000000, 1000000),
    ("fraction28", 28500000, 1000000),
    ("fallback27", 27000000, 1000000),
    ("fast", 10000000, 1700000),
    ("half", 10000000, 5000000),
    ("adjacent", 10000000, 6000000),
    ("full", 10000000, 10000000),
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default="verilator")
    parser.add_argument("--build-dir", type=Path, required=True)
    parser.add_argument("--jobs", type=int, default=2)
    parser.add_argument("sources_and_flags", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    inputs = args.sources_and_flags
    if inputs and inputs[0] == "--":
        inputs = inputs[1:]
    if not inputs:
        parser.error("provide the normal C64 RTL sources and Verilator flags after --")
    sim = Path(__file__).resolve().parent
    for name, system, cpu in CASES:
        build = (args.build_dir / name).resolve()
        build.mkdir(parents=True, exist_ok=True)
        command = [args.verilator, "--cc", "--exe", "--build", "-O2", "-j", str(args.jobs),
                   "--top-module", "ba_timing_sim_top", "--public-flat-rw",
                   f"-GSYS={system}", f"-GCPU={cpu}", "--Mdir", str(build),
                   str(sim / "ba_timing_sim_top.sv"), *inputs,
                   str(sim / "ba_timing_tb.cpp"), "-CFLAGS",
                   f"-O2 -std=c++17 -DTEST_SYS={system} -DTEST_CPU={cpu}"]
        with (build / "build.log").open("w") as log:
            result = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT)
        if result.returncode:
            raise SystemExit(f"{name}: build failed; see {build / 'build.log'}")
        result = subprocess.run([str(build / "Vba_timing_sim_top"), str(build / "firmware.hex")],
                                capture_output=True, text=True)
        (build / "result.log").write_text(result.stdout + result.stderr)
        print(result.stdout + result.stderr, end="", flush=True)
        if result.returncode:
            raise SystemExit(f"{name}: timing regression failed ({result.returncode})")


if __name__ == "__main__":
    main()
