#!/usr/bin/env python3
"""Spectrum host simulations: every model's machine, snapshot restore and the
frame-bank video.

The machine simulation runs the open diagnostic firmware on the 48K, 128K,
+2A/+3 and Pentagon builds (one Verilator model per SPECTRUM_MODEL_* define).
The snapshot simulation restores a synthetic .z80 snapshot on each model
through FogCast's restore stream (built with Go) and checks the machine state
at the first fetch from real memory. The Beta 128 simulation drives the
Pentagon's FD1793 through its ports against a synthetic .trd converted to the
raw track image (Go). The video simulation streams 50 Hz frames into the
720p60 frame banks.
Optionally (--fuse-tests DIR or --download) it also replays Fuse's Z80 core
tests through the NMOS Z80 and the Sinclair contention classifier.
"""
from __future__ import annotations

import argparse
import hashlib
import random
import struct
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
    f"{CORE}/rtl/spectrum_tape.v", f"{CORE}/rtl/spectrum_snapshot.v", f"{CORE}/rtl/spectrum_beta.sv",
    f"{CORE}/rtl/spectrum_slot_sockets.v",
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


def verilate(verilator: str, top: str, build: Path, sources, tb: str, defines=(), public=False,
             parameters=()) -> Path:
    build.mkdir(parents=True, exist_ok=True)
    command = [verilator, "--cc", "--exe", "--build", "-O2", "-j", "0", "--top-module", top, "-Wall",
               *(f"-Wno-{name}" for name in WAIVERS),
               f"-I{CORE}/rtl", "-Icores/fes-common/generated", "-Icores/fes-common/rtl",
               f"-I{CORE}/expansions", *(f"-D{define}" for define in defines),
               *(["--public-flat-rw"] if public else []), *(f"-G{p}" for p in parameters),
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


# Snapshot cases: (snapshot machine, .z80 v3 hardware mode, $7FFD, $1FFD,
# IFF1, IM, PC, the bank and offset the program's marker lands in).
# The 48K snapshot runs on the 48K and the Pentagon (paging locked to 48
# BASIC); the 128K one enables interrupts in IM 2 with its own vector table;
# the +2A/+3 one runs in the all-RAM {4,5,6,7} mode.
SNAPSHOTS = {
    "48": ("48", 0, 0x00, 0x00, False, 1, 0x8100, 2, 0x1000),
    "128": ("128", 4, 0x13, 0x00, True, 2, 0x8100, 2, 0x1000),
    "plus3": ("plus3", 7, 0x07, 0x03, False, 1, 0x8100, 6, 0x1000),
    "pentagon": ("48", 0, 0x00, 0x00, False, 0, 0x8100, 2, 0x1000),
}


def snapshot_case(key: str, out: Path) -> tuple[Path, Path, Path]:
    """Writes a synthetic .z80, its expected state and its RAM banks."""
    machine, mode, p7ffd, p1ffd, iff1, im, pc, marker_bank, marker_offset = SNAPSHOTS[key]
    rng = random.Random(f"fes-1084-{key}")
    order = [5, 2, 0] if machine == "48" else list(range(8))
    banks = {b: bytearray(rng.randbytes(16384)) for b in order}
    # The program at PC writes $5A to $9000 and loops; the IM 2 vector table
    # at $9100 points at EI; RET.
    pc_bank = 6 if machine == "plus3" else 2
    banks[pc_bank][pc & 0x3fff:(pc & 0x3fff) + 7] = bytes([0x3e, 0x5a, 0x32, 0x00, 0x90, 0x18, 0xfe])
    if im == 2:
        for offset in range(0x1100, 0x1202):
            banks[2][offset] = 0x82
        banks[2][0x0282:0x0284] = bytes([0xfb, 0xc9])
    regs = dict(af=0x12d7, bc=0x3456, de=0x789a, hl=0xbcde, af2=0x217d, bc2=0x4365, de2=0x87a9,
                hl2=0xcbed, ix=0x1357, iy=0x2468, sp=0xff40, pc=pc, i=0x91 if im == 2 else 0x3f,
                r=0x85, im=im, iff1=int(iff1), border=5)
    ay = [(0x10 + 7 * r) & 0xff for r in range(16)]
    header = bytearray(30)
    struct.pack_into("<BBHHHHBBB", header, 0, regs["af"] >> 8, regs["af"] & 0xff, regs["bc"], regs["hl"],
                     0, regs["sp"], regs["i"], regs["r"] & 0x7f, regs["r"] >> 7 | regs["border"] << 1)
    struct.pack_into("<HHHHBBHHBBB", header, 13, regs["de"], regs["bc2"], regs["de2"], regs["hl2"],
                     regs["af2"] >> 8, regs["af2"] & 0xff, regs["iy"], regs["ix"], int(iff1), int(iff1), im)
    extra = bytearray(55 if machine == "plus3" else 54)
    struct.pack_into("<HBB", extra, 0, pc, mode, p7ffd)
    if machine != "48":
        extra[5], extra[6] = 4, 7
        extra[7:23] = bytes(ay)
    if machine == "plus3":
        extra[54] = p1ffd
    data = bytes(header) + struct.pack("<H", len(extra)) + bytes(extra)
    pages = {5: 8, 2: 4, 0: 5} if machine == "48" else {b: b + 3 for b in order}
    for b in order:
        data += struct.pack("<HB", 0xffff, pages[b]) + bytes(banks[b])
    out.mkdir(parents=True, exist_ok=True)
    (out / "snapshot.z80").write_bytes(data)
    (out / "banks.bin").write_bytes(b"".join(bytes(banks[b]) for b in order))
    # The core's paging after the restore.
    if machine == "48":
        p7ffd, p1ffd = 0x30, 0x04
    lines = [f"{name} {value:x}" for name, value in regs.items()]
    if key != "48":
        lines.append(f"p7ffd {p7ffd:x}")
    if key == "plus3":
        lines.append(f"p1ffd {p1ffd:x}")
    if machine != "48":
        # The AY-3-8912 keeps only the bits each register has, and no R15.
        masks = (0xff, 0x0f, 0xff, 0x0f, 0xff, 0x0f, 0x1f, 0xff, 0x1f, 0x1f, 0x1f, 0xff, 0xff, 0x0f, 0xff, 0x00)
        lines += [f"ay{r} {v & masks[r]:x}" for r, v in enumerate(ay)]
    lines.append("banks " + " ".join(str(b) for b in order))
    lines.append(f"marker {marker_bank:x} {marker_offset:x} 5a")
    (out / "expect.txt").write_text("\n".join(lines) + "\n")
    return out / "snapshot.z80", out / "expect.txt", out / "banks.bin"


def snapshot(verilator: str, key: str, go: str) -> None:
    define, name, _ = MODELS[key]
    defines = [define] if define else []
    binary = verilate(verilator, "spectrum_sim_top", ROOT / f"build/sim/fes-spectrum-snapshot-{key}",
                      (f"{CORE}/sim/spectrum_sim_top.sv", *MACHINE_RTL, f"{CORE}/expansions/probe.v"),
                      f"{CORE}/sim/snapshot_tb.cpp", public=True,
                      defines=[*defines, f'SPECTRUM_MODEL_NAME="\\"{name}\\""'])
    work = ROOT / f"build/sim/fes-spectrum-snapshot-{key}/case"
    z80, expect, banks = snapshot_case(key, work)
    stream = work / "stream.bin"
    subprocess.run([go, "run", "./cmd/fes-spectrum-restore", "-out", str(stream), str(z80)],
                   cwd=ROOT.parent / "FogCast", check=True)
    subprocess.run([str(binary), str(stream), str(expect), str(banks)], cwd=ROOT, check=True)
    # A model that cannot run a class leaves its stream to the deck.
    refused = {"48": "128", "128": "plus3", "pentagon": "plus3"}.get(key)
    if refused:
        other = ROOT / f"build/sim/fes-spectrum-snapshot-{key}/refused"
        z80, _, _ = snapshot_case(refused, other)
        subprocess.run([go, "run", "./cmd/fes-spectrum-restore", "-out", str(other / "stream.bin"), str(z80)],
                       cwd=ROOT.parent / "FogCast", check=True)
        subprocess.run([str(binary), "--refuse", str(other / "stream.bin")], cwd=ROOT, check=True)


def trd() -> bytes:
    """An 80-cylinder double-sided TR-DOS image whose bytes encode their
    logical track, sector and offset, as beta_tb.cpp expects."""
    data = bytearray((track * 7 + sector * 3 + offset) & 0xFF
                     for track in range(160) for sector in range(16) for offset in range(256))
    data[8 * 256 + 227] = 0x16
    data[8 * 256 + 231] = 0x10
    return bytes(data)


def beta(verilator: str, go: str) -> None:
    build = ROOT / "build/sim/fes-spectrum-beta"
    binary = verilate(verilator, "spectrum_beta", build, (f"{CORE}/rtl/spectrum_beta.sv",),
                      f"{CORE}/sim/beta_tb.cpp")
    (build / "disk.trd").write_bytes(trd())
    subprocess.run([go, "run", "./cmd/fes-spectrum-disk", "-out", str(build / "disk.img"),
                    str(build / "disk.trd")], cwd=ROOT.parent / "FogCast", check=True)
    subprocess.run([str(binary), str(build / "disk.img")], cwd=ROOT, check=True)


def mailbox_units(verilator: str) -> None:
    """The Pentagon's mailbox: the cassette on unit 0 and the disk on unit 1."""
    binary = verilate(verilator, "fes_computer_mailbox_core", ROOT / "build/sim/fes-spectrum-mailbox-units",
                      ("cores/fes-common/rtl/fes_computer_mailbox.v",), "cores/fes-common/sim/computer_mailbox_units_tb.cpp",
                      parameters=("ENABLE_SPECTRUM_TAPE=1'b1", "ENABLE_SPECTRUM_DISK=1'b1", "MEDIA_AW=24",
                                  "UNIT0_MIN=10", "UNIT0_MAX=16777216"))
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
    parser.add_argument("--go", default="go", help="Go toolchain for FogCast's restore stream and disk writers")
    args = parser.parse_args()
    subprocess.run([sys.executable, f"{CORE}/diagnostic/firmware.py", "--output-dir",
                    "build/diagnostics/fes-spectrum"], cwd=ROOT, check=True)
    for key in args.model or MODELS:
        machine(args.verilator, key)
    for key in args.model or MODELS:
        snapshot(args.verilator, key, args.go)
    if "pentagon" in (args.model or MODELS):
        beta(args.verilator, args.go)
        mailbox_units(args.verilator)
    video(args.verilator)
    if args.fuse_tests or args.download:
        contention(args.verilator, fuse_tests(args.fuse_tests, args.download))


if __name__ == "__main__":
    main()
