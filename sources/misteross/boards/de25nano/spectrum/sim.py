#!/usr/bin/env python3
"""Simulate the DE25 ZX Spectrum standard component with the open diagnostic firmware."""
from pathlib import Path
import argparse
import importlib.util
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
BOARD = HERE.parent
ROOT = BOARD.parents[1]
SPECTRUM = ROOT / "cores/fes-spectrum/rtl"
SHARED = BOARD / "shared-rtl"
COMMON = ROOT / "cores/fes-common"
Z80 = COMMON / "rtl/z80"
MODELS = {"48": [], "128": ["-DSPECTRUM_MODEL_128K=1"]}
WAIVERS = ("UNUSEDSIGNAL", "UNUSEDPARAM", "UNOPTFLAT", "CASEINCOMPLETE", "CASEOVERLAP", "WIDTHTRUNC",
           "WIDTHEXPAND", "SYNCASYNCNET", "PINCONNECTEMPTY", "DECLFILENAME", "IMPLICITSTATIC",
           "VARHIDDEN", "CASEX", "BLKSEQ", "MULTITOP")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sources(output):
    """The compiled source set: DE10 machine files as they are, plus the DE25 derivations."""
    derive = load(HERE / "derive.py", "de25_spectrum_derive")
    display = load(BOARD / "display/build.py", "de25_spectrum_display_ram")
    machine = output / "spectrum_machine_de25.sv"
    machine.write_bytes(derive.retarget_machine((SPECTRUM / "spectrum_machine.sv").read_bytes())[0])
    derived = [machine]
    for name in derive.RAM_FILES:
        path = output / name.replace(".v", "_de25.v")
        path.write_bytes(derive.retarget_ram(name, (SPECTRUM / name).read_bytes())[0])
        derived.append(path)
    reader = output / "fes_menu_reader_de25.v"
    reader.write_bytes(display.retarget_menu_ram((SHARED / "fes_menu_reader.v").read_bytes())[0])
    # Only the shared transport is used from the ZX81 computer file.
    computer = (BOARD / "zx81-hps/fes_de25_computer.v").read_bytes()
    marker = b"// Atomic register transport shared by the bring-up and standard-core wrappers.\n"
    if computer.count(marker) != 1:
        raise ValueError("DE25 computer transport boundary changed")
    transport = output / "fes_de25_computer_transport.v"
    transport.write_bytes(b'`include "fes_computer.vh"\n`include "fes_de25_computer.vh"\n' +
                          computer[computer.index(marker):])
    return [HERE / "fes_de25_spectrum_standard.v", transport, BOARD / "ramtest/sdram.v",
            SHARED / "fes_spectrum_core.v", SHARED / "fes_firmware_upload.v",
            SHARED / "fes_media_sdram.v", SHARED / "fes_spectrum_firmware_rom.v",
            SHARED / "fes_computer_mailbox.v", SHARED / "fes_display_plane.v",
            SHARED / "fes_menu_control.v", SHARED / "fes_menu_video.v", reader,
            BOARD / "scanout/fes_de25_session_display.v", BOARD / "scanout/fes_de25_scanout.v",
            COMMON / "rtl/fes_display_cdc.v", *derived,
            *(SPECTRUM / name for name in (
                "spectrum_ula.sv", "spectrum_contention.sv", "spectrum_snapshot.v",
                "spectrum_beta.sv", "spectrum_fdd.sv", "spectrum_upd765.sv",
                "spectrum_keyboard.v", "spectrum_audio.v")),
            ROOT / "cores/fes-zx81/expansions/zonx_ay.v",
            *(Z80 / (name + ".sv") for name in
              ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos"))]


def includes():
    return [SPECTRUM, COMMON / "generated", COMMON / "rtl", SHARED / "generated",
            BOARD / "zx81-hps/generated", BOARD / "scanout", BOARD / "scanout/generated", Z80]


def run(model):
    diagnostic = ROOT / "build/diagnostics/fes-spectrum"
    subprocess.run([sys.executable, str(ROOT / "cores/fes-spectrum/diagnostic/firmware.py"),
                    "--output-dir", str(diagnostic)], cwd=ROOT, check=True)
    parent = ROOT / "build/sim/de25-spectrum"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix=f"standard-{model}-", dir=parent))
    print(f"DE25 Spectrum simulation directory: {output}", flush=True)
    firmware = diagnostic / ("firmware.rom" if model == "48" else "firmware-32k.rom")
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "4", "--public-flat-rw", "-O3",
                    "-Wall", *["-Wno-" + name for name in WAIVERS], *MODELS[model],
                    "--top-module", "fes_de25_spectrum_standard", "--Mdir", str(output),
                    "-GSDRAM_POWERUP_CYCLES=16",
                    "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
                    *["-I" + str(path) for path in includes()],
                    "-CFLAGS", f"-O2 -DSPECTRUM_BANKS={3 if model == '48' else 8}",
                    *map(str, sources(output)), str(HERE / "standard_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_de25_spectrum_standard"), str(firmware),
                    str(diagnostic / "tape.pzx")], cwd=output, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", choices=("all", *MODELS), default="all")
    selected = parser.parse_args().model
    for model in (MODELS if selected == "all" else (selected,)):
        run(model)


if __name__ == "__main__":
    main()
