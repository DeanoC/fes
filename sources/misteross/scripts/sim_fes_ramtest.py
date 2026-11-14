# SPDX-License-Identifier: GPL-2.0-or-later
"""Simulate the RAM tester against the SDRAM addon and HPS DDR models."""

import os
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / "cores" / "fes-ramtest"
COMMON = ROOT / "cores" / "fes-common"
VERILATOR = os.environ.get("VERILATOR", "verilator")


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
            VERILATOR, "--cc", "--exe", "--build", "--top-module", "pattern_count_bench",
            "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY",
            "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ",
            "-Mdir", str(output),
            "-o", "sim",
            *map(str, sources),
        ],
        check=True,
    )
    subprocess.run([str(output / "sim")], check=True)


def latency_stats() -> None:
    """Latency percentiles match a reference model of the histogram."""
    output = ROOT / "build" / "fes-ramtest-latency-stats"
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        [
            VERILATOR, "--cc", "--exe", "--build", "--top-module", "lat_stats",
            "-GSAMPLES=16'd100",
            "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY",
            "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ",
            "-Mdir", str(output),
            "-o", "sim",
            str(CORE / "rtl" / "lat_stats.v"), str(CORE / "sim" / "lat_stats_tb.cpp"),
        ],
        check=True,
    )
    subprocess.run([str(output / "sim")], check=True)


def guard_regression() -> str:
    """Run the shared guard test with stable tool paths and no silent skip."""
    env = os.environ.copy()
    # The unittest runs from ROOT; preserve relative PATH entries across chdir.
    env["PATH"] = os.pathsep.join(str(Path(part or ".").resolve())
                                  for part in env.get("PATH", "").split(os.pathsep))
    configured = env.get("VERILATOR", "verilator")
    resolved = shutil.which(configured, path=env["PATH"])
    if not resolved:
        raise SystemExit(f"simulation requires executable: {configured}")
    for tool in ("make", "c++"):
        if not shutil.which(tool, path=env["PATH"]):
            raise SystemExit(f"simulation requires executable: {tool}")
    env["VERILATOR"] = str(Path(resolved).resolve())
    env["FES_HPS_DDR_GUARD_REQUIRED"] = "1"
    subprocess.run([sys.executable, "-m", "unittest", "-v", "tests.test_fes_hps_ddr_guard"],
                   cwd=ROOT, env=env, check=True)
    return env["VERILATOR"]


def main() -> None:
    if os.environ.get("FES_TOOLCHAIN_CACHE_ROOT"):
        raise SystemExit("simulation refuses a shared toolchain cache")
    global VERILATOR
    VERILATOR = guard_regression()
    pattern_counts()
    latency_stats()
    output = ROOT / "build" / "fes-ramtest-sim"
    output.mkdir(parents=True, exist_ok=True)
    byte_output = output / "byte"
    byte_output.mkdir(exist_ok=True)
    subprocess.run([
        VERILATOR, "--cc", "--exe", "--build", "--top-module", "byte_bench",
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
        CORE / "rtl" / "lat_stats.v",
        CORE / "rtl" / "lat_monitor.v",
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
    # The shipped 100 MHz OSS path uses output registers and fabric DQ
    # capture. This idealized PLL checks edge order, not physical timing.
    variants = (
        (output, (), False),
        (output.with_name(output.name + "-io-registers"), ("+define+RAM_SDRAM_IO_REGISTERS",), False),
        (output.with_name(output.name + "-oss-100"),
         ("+define+RAM_RATE_SWEEP", "+define+RAM_100_ONLY", "+define+RAM_OSS_HIGH_SPEED"), False),
        (output.with_name(output.name + "-oss-100-no-capture-shift"),
         ("+define+RAM_RATE_SWEEP", "+define+RAM_100_ONLY", "+define+RAM_OSS_HIGH_SPEED"), True),
    )
    if os.environ.get("FES_RAMTEST_130_DIAGNOSTIC") == "1":
        variants += ((output.with_name(output.name + "-oss-130"),
                      ("+define+RAM_RATE_SWEEP", "+define+RAM_130_ONLY", "+define+RAM_OSS_HIGH_SPEED"), False),)
    for build, defines, negative in variants:
        build.mkdir(parents=True, exist_ok=True)
        test_env = os.environ.copy()
        test_env.pop("RAMTEST_DDR_FAULT_CASES", None)
        if "+define+RAM_OSS_HIGH_SPEED" in defines and "+define+RAM_100_ONLY" in defines and not negative:
            test_env["RAMTEST_DDR_FAULT_CASES"] = "1"
        build_sources = sources
        if negative:
            controller = CORE / "rtl" / "sdram_addon_port.v"
            text = controller.read_text()
            original = "if (wait_count == OUT_LATENCY + (rate == 2'd1 ?"
            due = "wait_count == 14'd4 + OUT_LATENCY"
            if text.count(original) != 1 or text.count(due) != 1:
                raise RuntimeError("cannot construct capture-shift negative control")
            mutated = build / "sdram_addon_port.v"
            mutated.write_text(text.replace(original, "if (wait_count == 14'd0 + (rate == 2'd1 ?")
                               .replace(due, "wait_count == 14'd4"))
            build_sources = [mutated if source == controller else source for source in sources]
        subprocess.run(
            [
                VERILATOR, "--cc", "--exe", "--build", "--top-module", "bench",             "+define+SIM",
                *defines,
                "-Wall", "-Wno-DECLFILENAME", "-Wno-PINCONNECTEMPTY",
                "-Wno-UNUSEDSIGNAL", "-Wno-UNUSEDPARAM", "-Wno-BLKSEQ",
                f"-I{COMMON / 'generated'}",
                "-Mdir", str(build),
                "-o", "sim",
                *map(str, build_sources),
            ],
            check=True,
        )
        if negative:
            result = subprocess.run([str(build / "sim")], capture_output=True, text=True, env=test_env)
            if result.returncode == 0 or "RAMTEST_BYTE_PREFLIGHT_READ_MISMATCH" not in result.stderr:
                raise RuntimeError(f"capture-shift negative control did not fail as expected: {result.stderr}")
            print("PASS: 100 MHz OSS functional model rejects missing read-capture shift", flush=True)
        elif "+define+RAM_130_ONLY" in defines:
            result = subprocess.run([str(build / "sim")], capture_output=True, text=True, env=test_env)
            print(result.stdout, end="", flush=True)
            print(result.stderr, end="", flush=True)
            raise SystemExit(
                "AMBIGUOUS: the 130 MHz zero-delay SDRAM model does not establish "
                f"the DQ valid window (simulation exit {result.returncode}); "
                "this diagnostic cannot pass without a bounded timing model"
            )
        elif "RAMTEST_DDR_FAULT_CASES" in test_env:
            result = subprocess.run([str(build / "sim")], capture_output=True, text=True, env=test_env)
            print(result.stdout, end="", flush=True)
            print(result.stderr, end="", file=sys.stderr, flush=True)
            result.check_returncode()
            if "PASS: twelve DDR latency fault and stop cases" not in result.stdout:
                raise RuntimeError("OSS-100 simulation did not run the DDR fault cases")
        else:
            subprocess.run([str(build / "sim")], check=True, env=test_env)


if __name__ == "__main__":
    main()
