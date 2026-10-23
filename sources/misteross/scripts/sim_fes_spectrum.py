#!/usr/bin/env python3
"""Spectrum host simulations: every model's machine and the frame-bank video.

The machine simulation runs the open diagnostic firmware on the 48K, 128K,
+2A/+3 and Pentagon builds (one Verilator model per SPECTRUM_MODEL_* define).
The video simulation streams 50 Hz frames into the 720p60 frame banks.
Optionally (--fuse-tests DIR or --download) it also replays Fuse's Z80 core
tests through the NMOS Z80 and the Sinclair contention classifier.
"""
from __future__ import annotations

import argparse
import hashlib
import subprocess
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CORE = "cores/fes-spectrum"
WAIVERS = ("UNUSEDSIGNAL", "DECLFILENAME", "WIDTHEXPAND", "WIDTHTRUNC", "CASEX", "PINCONNECTEMPTY",
           "UNUSEDPARAM", "CASEINCOMPLETE", "CASEOVERLAP", "SYNCASYNCNET", "UNOPTFLAT", "BLKSEQ")
Z80 = tuple(f"cores/fes-common/rtl/z80/{name}" for name in
            ("fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv"))
MACHINE_RTL = (
    f"{CORE}/rtl/spectrum_machine.sv", f"{CORE}/rtl/spectrum_ula.sv",
    f"{CORE}/rtl/spectrum_contention.sv", f"{CORE}/rtl/spectrum_ram.v", f"{CORE}/rtl/spectrum_rom.v",
    f"{CORE}/rtl/spectrum_tape.v", f"{CORE}/rtl/spectrum_slot_sockets.v",
    "cores/fes-zx81/expansions/zonx_ay.v", *Z80,
)
# Model define, report name and the frame T state at which the diagnostic's
# timing marker reaches RAM (its write cycle's T2). Fuse 1.6.0 running the same
# firmware puts that write's T1 one T state earlier on every model.
MODELS = {
    "48": (None, "48K", 14950),
    "128": ("SPECTRUM_MODEL_128K", "128K", 14960),
    "plus3": ("SPECTRUM_MODEL_PLUS3", "+2A/+3", 14955),
    "pentagon": ("SPECTRUM_MODEL_PENTAGON", "Pentagon 128", 14946),
}
# Fuse's Z80 core tests (GPL-2.0-or-later), read at a fixed revision.
FUSE_REVISION = "e997e2bc32c888348f862f69f2c53babfedf7791"
FUSE_URL = f"https://raw.githubusercontent.com/libretro/fuse-libretro/{FUSE_REVISION}/fuse/z80/tests/"
FUSE_FILES = {
    "tests.in": "9f36e866f22e72ff1f8bf2100bf70ffbf58edd97b453500aab60acf1f403ebbb",
    "tests.expected": "15a6946f4addcf97e137b5bdd1d5fdb08124ff91f1b169f36a8bf4afe4bab6e4",
}


def verilate(verilator: str, top: str, build: Path, sources, tb: str, defines=(), public=False) -> Path:
    build.mkdir(parents=True, exist_ok=True)
    command = [verilator, "--cc", "--exe", "--build", "-O2", "-j", "0", "--top-module", top, "-Wall",
               *(f"-Wno-{name}" for name in WAIVERS),
               f"-I{CORE}/rtl", "-Icores/fes-common/generated", "-Icores/fes-common/rtl",
               f"-I{CORE}/expansions", *(f"-D{define}" for define in defines),
               *(["--public-flat-rw"] if public else []),
               "-CFLAGS", "-std=c++17 -O2 " + " ".join(f"-D{d}" for d in defines),
               "--Mdir", str(build), *sources, str(ROOT / tb)]
    subprocess.run(command, cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
    return build / f"V{top}"


def machine(verilator: str, key: str) -> None:
    define, name, marker = MODELS[key]
    defines = [define] if define else []
    binary = verilate(verilator, "spectrum_sim_top", ROOT / f"build/sim/fes-spectrum-machine-{key}",
                      (f"{CORE}/sim/spectrum_sim_top.sv", *MACHINE_RTL, f"{CORE}/expansions/probe.v"),
                      f"{CORE}/sim/machine_tb.cpp", public=True,
                      defines=[*defines, f'SPECTRUM_MODEL_NAME="\\"{name}\\""', f"SPECTRUM_MARKER_T={marker}"])
    subprocess.run([str(binary)], cwd=ROOT, check=True)


def video(verilator: str) -> None:
    binary = verilate(verilator, "spectrum_video", ROOT / "build/sim/fes-spectrum-video",
                      (f"{CORE}/rtl/spectrum_video.v",), f"{CORE}/sim/video_tb.cpp")
    subprocess.run([str(binary)], cwd=ROOT, check=True)


def fuse_tests(directory: Path | None, download: bool) -> Path:
    target = ROOT / "build/sim/fes-spectrum-contention/fuse"
    source = target if directory is None else directory
    target.mkdir(parents=True, exist_ok=True)
    for name, digest in FUSE_FILES.items():
        path = source / name
        if download and directory is None and not path.is_file():
            request = urllib.request.Request(FUSE_URL + name, headers={"User-Agent": "FES-Spectrum-tests"})
            with urllib.request.urlopen(request, timeout=60) as response:
                path.write_bytes(response.read(8 * 1024 * 1024))
        if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise SystemExit(f"{path} is not the pinned Fuse fixture")
    return source


def contention(verilator: str, directory: Path) -> None:
    binary = verilate(verilator, "contention_top", ROOT / "build/sim/fes-spectrum-contention",
                      (f"{CORE}/sim/contention_top.sv", f"{CORE}/rtl/spectrum_contention.sv", *Z80),
                      f"{CORE}/sim/contention_tb.cpp", public=True)
    subprocess.run([str(binary), str(directory / "tests.in"), str(directory / "tests.expected")],
                   cwd=ROOT, check=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--verilator", default="verilator")
    parser.add_argument("--model", choices=tuple(MODELS), action="append",
                        help="machine models to simulate (default all)")
    parser.add_argument("--fuse-tests", type=Path, help="directory holding Fuse tests.in/tests.expected")
    parser.add_argument("--download", action="store_true", help="fetch the pinned Fuse Z80 tests")
    args = parser.parse_args()
    subprocess.run([sys.executable, f"{CORE}/diagnostic/firmware.py", "--output-dir",
                    "build/diagnostics/fes-spectrum"], cwd=ROOT, check=True)
    for key in args.model or MODELS:
        machine(args.verilator, key)
    video(args.verilator)
    if args.fuse_tests or args.download:
        contention(args.verilator, fuse_tests(args.fuse_tests, args.download))


if __name__ == "__main__":
    main()
