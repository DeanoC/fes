#!/usr/bin/env python3
"""Simulate the DE25 Commodore 64 standard component with the open diagnostic firmware and disk."""
import argparse
from pathlib import Path
import importlib.util
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
BOARD = HERE.parent
ROOT = BOARD.parents[1]
C64 = ROOT / "cores/fes-c64/rtl"
SHARED = BOARD / "shared-rtl"
COMMON = ROOT / "cores/fes-common"
CPU = COMMON / "rtl/cpu6502"
WAIVERS = ("UNUSEDSIGNAL", "UNUSEDPARAM", "UNOPTFLAT", "CASEINCOMPLETE", "CASEOVERLAP", "WIDTHTRUNC",
           "WIDTHEXPAND", "SYNCASYNCNET", "PINCONNECTEMPTY", "DECLFILENAME", "IMPLICITSTATIC",
           "VARHIDDEN", "CASEX", "BLKSEQ", "MULTITOP", "UNDRIVEN")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sources(output, native):
    """The compiled source set: DE10 machine files as they are, plus the DE25 derivations."""
    build = load(HERE / "build.py", "de25_c64_build_sources")
    if native:
        captured = build.board.capture(build, native=True)
        staged, _, _ = build.board.component_files(build, captured, native=True)
        paths = []
        for name, data in staged.items():
            path = output / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
            if path.suffix in (".sv", ".v"):
                paths.append(path)
        return paths
    derive = load(HERE / "derive.py", "de25_c64_derive")
    display = load(BOARD / "display/build.py", "de25_c64_display_ram")
    machine = output / "c64_machine_de25.sv"
    machine.write_bytes(derive.retarget_machine((C64 / "c64_machine.sv").read_bytes())[0])
    derived = [machine]
    for name in derive.RAM_FILES:
        path = output / name.replace(".v", "_de25.v")
        path.write_bytes(derive.retarget_ram(name, (C64 / name).read_bytes())[0])
        derived.append(path)
    disk = output / "c64_disk_store_de25.v"
    disk.write_bytes(derive.retarget_disk_store((C64 / "c64_disk_store.v").read_bytes())[0])
    derived.append(disk)
    store = output / "c64_frame_store_de25.sv"
    store.write_bytes(derive.retarget_frame_store((C64 / "c64_frame_store.sv").read_bytes())[0])
    derived.append(store)
    reader = output / "fes_menu_reader_de25.v"
    reader.write_bytes(display.retarget_menu_ram((SHARED / "fes_menu_reader.v").read_bytes())[0])
    transport = output / "fes_de25_computer_transport.v"
    transport.write_bytes(derive.transport((BOARD / "zx81-hps/fes_de25_computer.v").read_bytes())[0])
    return [HERE / "fes_de25_c64_standard.v", transport,
            SHARED / "fes_c64_core.v", SHARED / "fes_gp_crossing.v", SHARED / "fes_firmware_upload.v",
            SHARED / "fes_c64_firmware_rom.v",
            SHARED / "fes_computer_mailbox.v", SHARED / "fes_display_plane.v",
            SHARED / "fes_menu_control.v", SHARED / "fes_menu_video.v", reader,
            BOARD / "scanout/fes_de25_session_display.v", BOARD / "scanout/fes_de25_scanout.v",
            COMMON / "rtl/fes_display_cdc.v", *derived,
            *(C64 / name for name in build.MACHINE_FILES if not name.endswith(".vh")),
            *(CPU / name for name in build.CPU_FILES)]


def includes():
    return [C64, COMMON / "generated", COMMON / "rtl", SHARED / "generated",
            BOARD / "zx81-hps", BOARD / "scanout", BOARD / "scanout/generated"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--native", action="store_true")
    native = parser.parse_args().native
    diagnostic = ROOT / "build/diagnostics/fes-c64-de25"
    diagnostic.mkdir(parents=True, exist_ok=True)
    # The DE25 sockets are vacant, so the firmware skips its cartridge stages.
    subprocess.run([sys.executable, str(ROOT / "cores/fes-c64/diagnostic/firmware.py"),
                    "--output-dir", str(diagnostic), "--without-cartridges"], cwd=ROOT, check=True)
    subprocess.run([sys.executable, str(ROOT / "cores/fes-c64/diagnostic/disk.py"),
                    "--output", str(diagnostic / "disk.d64"), "--hex-output", str(diagnostic / "disk.hex")],
                   cwd=ROOT, check=True)
    parent = ROOT / "build/sim/de25-c64"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="standard-", dir=parent))
    print(f"DE25 C64 simulation directory: {output}", flush=True)
    link, defines, flags = [], [], "-O2"
    if native:
        firmware = ROOT.parents[1] / "platform/embedded/firmware"
        flags += f" -DC64_NATIVE -I{firmware}"
        defines = ["-DFES_DE25_NATIVE_C64=1", "-Wno-PINMISSING"]
        objects = []
        for name in ("nano_fabric_stop", "nano_probe_stop"):
            obj = output / (name + ".o")
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-I", str(firmware),
                            "-c", str(firmware / (name + ".c")), "-o", str(obj)], check=True)
            objects.append(str(obj))
        link = ["-LDFLAGS", " ".join(objects)]
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "4", "--public-flat-rw", "-O3",
                    "-Wall", *["-Wno-" + name for name in WAIVERS],
                    *defines, "--top-module", "fes_de25_c64_standard", "--Mdir", str(output),
                    "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
                    *["-I" + str(path) for path in ([output] + includes())],
                    "-CFLAGS", flags, *link,
                    *map(str, sources(output, native)), str(HERE / "standard_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_de25_c64_standard"), str(diagnostic / "firmware.bin"),
                    str(diagnostic / "disk.d64")], cwd=output, check=True)
    # The DE10 frame-store bench against the derived store: page handoff at
    # several producer rates, reader coherence and every stored address.
    frame = output / "frame"
    frame.mkdir()
    derive = load(HERE / "derive.py", "de25_c64_frame_derive")
    store = frame / "c64_frame_store.sv"
    store.write_bytes(derive.retarget_frame_store((C64 / "c64_frame_store.sv").read_bytes())[0])
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-O2", "-Wall",
                    *["-Wno-" + name for name in WAIVERS], "-Wno-MULTIDRIVEN",
                    "--top-module", "c64_frame_store", "--Mdir", str(frame), str(store),
                    str(ROOT / "cores/fes-c64/sim/frame_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(frame / "Vc64_frame_store")], cwd=frame, check=True)


if __name__ == "__main__":
    main()
