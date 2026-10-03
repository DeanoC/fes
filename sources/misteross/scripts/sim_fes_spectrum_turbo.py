#!/usr/bin/env python3
"""Host regressions and transaction-throughput measurements for both Z80 modes."""
from __future__ import annotations
import argparse
import concurrent.futures
import json
import hashlib
from pathlib import Path
import re
import shlex
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
if __package__ in (None, ""):
    sys.path.insert(0, str(ROOT))
from scripts.build_fes_spectrum_oss import RTL_SOURCES

FLAGS = ("-Wall", "-Wno-UNUSEDSIGNAL", "-Wno-DECLFILENAME", "-Wno-WIDTHEXPAND",
         "-Wno-WIDTHTRUNC", "-Wno-PINCONNECTEMPTY", "-Wno-UNUSEDPARAM",
         "-Wno-CASEINCOMPLETE", "-Wno-SYNCASYNCNET", "-Wno-UNOPTFLAT", "-Wno-BLKSEQ",
         "-Icores/fes-spectrum/rtl", "-Icores/fes-common/generated", "-Icores/fes-common/rtl")
SIM_RTL = tuple(p for p in RTL_SOURCES if p not in (
    "cores/fes-spectrum/rtl/top.v", "cores/fes-spectrum/rtl/spectrum_system_pll.v",
    "cores/fes-common/rtl/pixel_pll.v"))


def firmware(scenario: str) -> bytes:
    """Open, manually encoded documented instructions; no external ROM bytes."""
    rom = bytearray(16384)
    rom[:3] = bytes((0xc3, 0x00, 0x01))
    rom[0x66:0x6f] = bytes((0xf5, 0x3e, 0x77, 0x32, 0x04, 0x80, 0xf1, 0xed, 0x45))
    program = bytearray((0xf3, 0x31, 0xff, 0xff))  # DI; LD SP,FFFF
    def emit(*values: int) -> None:
        program.extend(values)
    def marker(value: int) -> None:
        emit(0x3e, value, 0x32, 0x00, 0x80)
    def loop(body: tuple[int, ...]) -> None:
        emit(*body, 0x10, (-len(body) - 2) & 255)
    if scenario == "wait":
        emit(0x01, 0xe1, 0x00, 0x3e, 0x5a, 0xed, 0x79)  # OUT (C),A
        emit(0x32, 0x00, 0x81, 0x3a, 0x00, 0x20, 0x32, 0x01, 0x80)
        emit(0x3a, 0x00, 0x81, 0x32, 0x02, 0x80)
        marker(0x5a)
    else:
        marker(1)
        emit(0x3e, 0x00, 0x16, 0x03, 0x1e, 0x05, 0x06, 0x80)
        loop((0x3c, 0x82, 0xab))  # INC A; ADD A,D; XOR E; DJNZ
        emit(0x32, 0x01, 0x80)
        marker(2)
        emit(0x21, 0x00, 0x81, 0x06, 0x80, 0x3e, 0x00)
        loop((0x77, 0x34, 0x7e))  # LD (HL),A; INC (HL); LD A,(HL)
        emit(0x32, 0x02, 0x80)
        marker(3)
        emit(0x01, 0xe1, 0x80, 0x3e, 0x00)
        loop((0xed, 0x79, 0xed, 0x78, 0x3c))  # OUT/IN (C); INC A
        emit(0x32, 0x03, 0x80)
        marker(4)
    halt_address = 0x100 + len(program)
    emit(0x76, 0xc3, halt_address & 255, halt_address >> 8)
    rom[0x100:0x100+len(program)] = program
    return bytes(rom)


def run(command: list[str], root: Path, log: Path) -> str:
    completed = subprocess.run(command, cwd=root, text=True, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT)
    log.parent.mkdir(parents=True, exist_ok=True)
    log.write_text(completed.stdout)
    if completed.returncode:
        raise RuntimeError(f"{' '.join(command[:4])} exited {completed.returncode}; "
                           f"log {log}\n{completed.stdout[-6000:]}")
    return completed.stdout


def compile_mode(root: Path, verilator: list[str], mode: str, machine_board: bool) -> dict[str, Path]:
    result = {}
    jobs = [("benchmark", "spectrum_benchmark_top", "spectrum_benchmark_top.sv", "benchmark_tb.cpp")]
    if machine_board:
        jobs += [("machine", "spectrum_sim_top", "spectrum_sim_top.sv", "machine_tb.cpp"),
                 ("board", "top", "board_models.v", "board_tb.cpp")]
    if mode == "fast":
        output = root / "build/sim/fes-spectrum-turbo/fast/bus"
        output.mkdir(parents=True, exist_ok=True)
        command = [*verilator, "--cc", "--exe", "--build", "-j", "2", "-O2",
                   "--top-module", "spectrum_fast_bus", *FLAGS, "--Mdir", str(output),
                   "cores/fes-spectrum/rtl/spectrum_fast_bus.sv",
                   str(root / "cores/fes-spectrum/sim/fast_bus_tb.cpp")]
        run(command, root, output / "compile.log")
        result["bus"] = output / "Vspectrum_fast_bus"
        output = root / "build/sim/fes-spectrum-turbo/fast/audio"
        output.mkdir(parents=True, exist_ok=True)
        command = [*verilator, "--cc", "--exe", "--build", "-j", "2", "-O2",
                   "--top-module", "spectrum_fast_audio", *FLAGS, "--Mdir", str(output),
                   "cores/fes-spectrum/rtl/spectrum_fast_audio.sv",
                   str(root / "cores/fes-spectrum/sim/fast_audio_tb.cpp")]
        run(command, root, output / "compile.log")
        result["audio"] = output / "Vspectrum_fast_audio"
    for label, top, wrapper, tb in jobs:
        output = root / "build/sim/fes-spectrum-turbo" / mode / label
        output.mkdir(parents=True, exist_ok=True)
        sources = [f"cores/fes-spectrum/sim/{wrapper}", *SIM_RTL]
        if label == "board":
            sources.insert(1, "cores/fes-spectrum/rtl/top.v")
        if label == "machine":
            sources.append("cores/fes-spectrum/expansions/probe.v")
        command = [*verilator, "--cc", "--exe", "--build", "-j", "2", "-O2", "--top-module", top,
                   *FLAGS, f"-GFAST_CPU={int(mode == 'fast')}", "--Mdir", str(output),
                   "-CFLAGS", f"-DSPECTRUM_FAST_CPU={int(mode == 'fast')}"]
        if label == "board":
            command.append("--public-flat-rw")
        command += [*sources, str(root / f"cores/fes-spectrum/sim/{tb}")]
        run(command, root, output / "compile.log")
        result[label] = output / f"V{top}"
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--verilator", default="verilator")
    parser.add_argument("--cpu", choices=("nmos", "fast", "both"), default="both")
    parser.add_argument("--skip-machine-board", action="store_true")
    args = parser.parse_args()
    root = args.root.resolve()
    modes = ("nmos", "fast") if args.cpu == "both" else (args.cpu,)
    run([sys.executable, "cores/fes-spectrum/diagnostic/firmware.py", "--output-dir",
         "build/diagnostics/fes-spectrum"], root, root / "build/sim/fes-spectrum-turbo/firmware.log")
    try:
        source_paths = sorted(set(RTL_SOURCES) | {
            "scripts/sim_fes_spectrum_turbo.py", "cores/fes-spectrum/diagnostic/firmware.py",
            "cores/fes-spectrum/sim/spectrum_benchmark_top.sv", "cores/fes-spectrum/sim/benchmark_tb.cpp",
            "cores/fes-spectrum/sim/spectrum_sim_top.sv", "cores/fes-spectrum/sim/machine_tb.cpp",
            "cores/fes-spectrum/sim/board_models.v", "cores/fes-spectrum/sim/board_tb.cpp",
            "cores/fes-spectrum/sim/fast_audio_tb.cpp", "cores/fes-spectrum/sim/fast_bus_tb.cpp",
            "cores/fes-spectrum/expansions/probe.v", "cores/fes-spectrum/rtl/spectrum_bus.vh",
            "cores/fes-common/generated/fes_computer.vh"})
        def source_hashes():
            return {path: hashlib.sha256((root/path).read_bytes()).hexdigest() for path in source_paths}
        inputs = source_hashes()
        tool_version = run([*shlex.split(args.verilator), "--version"], root,
                           root / "build/sim/fes-spectrum-turbo/verilator.log").strip()
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            futures = {mode: pool.submit(compile_mode, root, shlex.split(args.verilator), mode,
                                        not args.skip_machine_board) for mode in modes}
            binaries = {mode: future.result() for mode, future in futures.items()}
        for mode in modes:
            for label in ("machine", "board", "bus", "audio"):
                if label in binaries[mode]:
                    print(run([str(binaries[mode][label])], root,
                              binaries[mode][label].parent / "run.log").strip())
        summary = {"classification": "host simulation; no placement, route or hardware acceptance",
                   "iterations_per_workload": 128, "modes": {},
                   "source_sha256": inputs, "verilator_version": tool_version,
                   "benchmark_firmware_sha256": hashlib.sha256(firmware("benchmark")).hexdigest(),
                   "fast_audio": {"clock_domain_hz": 56_000_000, "mclk_average_hz": 12_288_000,
                                  "bclk_average_hz": 3_072_000, "sample_average_hz": 48_000,
                                  "mclk_half_period_ticks": [2, 3], "bclk_half_period_ticks": [9, 10],
                                  "lrclk_half_period_ticks": [583, 584]}}
        for scenario in ("benchmark", "wait"):
            image = firmware(scenario)
            (root / "build/diagnostics/fes-spectrum/benchmark.hex").write_text(
                "".join(f"{byte:02x}\n" for byte in image))
            for mode in modes:
                binary = binaries[mode]["benchmark"]
                output = run([str(binary), mode, scenario], root,
                             binary.parent / f"{scenario}.log")
                print(output.strip())
                if scenario == "benchmark":
                    match = re.search(r"clocks compute=(\d+) memory=(\d+) io=(\d+)", output)
                    if not match:
                        raise RuntimeError("benchmark did not report all elapsed clock counts")
                    hz = 56_000_000 if mode == "fast" else 52_224_000
                    summary["modes"][mode] = {"system_clock_hz": hz,
                        "workloads": {name: {"system_clocks": int(count),
                                              "elapsed_us": int(count) * 1_000_000 / hz}
                                      for name, count in zip(("compute", "memory", "io"), match.groups())}}
        if len(modes) == 2:
            summary["elapsed_speedup"] = {name:
                summary["modes"]["nmos"]["workloads"][name]["elapsed_us"] /
                summary["modes"]["fast"]["workloads"][name]["elapsed_us"]
                for name in ("compute", "memory", "io")}
        if source_hashes() != inputs:
            raise RuntimeError("simulation source changed during the run; rerun the selected sources")
        target = root / "build/sim/fes-spectrum-turbo/summary.json"
        target.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
        print(f"Spectrum host benchmark evidence: {target}")
    except (OSError, RuntimeError, ValueError) as exc:
        print(exc, file=sys.stderr)
        return 1
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
