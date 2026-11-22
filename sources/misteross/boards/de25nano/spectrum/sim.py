#!/usr/bin/env python3
"""Simulate the DE25 ZX Spectrum standard component with the open diagnostic firmware."""
from pathlib import Path
import argparse
import hashlib
import importlib.util
import os
import re
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
NATIVE_BUILD_ID = "00112233445566778899aabbccddeeff"
MODELS = {"48": [], "128": ["-DSPECTRUM_MODEL_128K=1"], "plus3": ["-DSPECTRUM_MODEL_PLUS3=1"],
          "pentagon": ["-DSPECTRUM_MODEL_PENTAGON=1"]}
# Each model's diagnostic image, RAM banks and disk controller for the bench.
FIRMWARE = {"48": "firmware.rom", "128": "firmware-32k.rom", "plus3": "firmware-64k.rom",
            "pentagon": "firmware-48k.rom"}
BENCH = {"48": "-DSPECTRUM_BANKS=3", "128": "-DSPECTRUM_BANKS=8",
         "plus3": "-DSPECTRUM_BANKS=8 -DSPECTRUM_DISK_UPD765=1",
         "pentagon": "-DSPECTRUM_BANKS=8 -DSPECTRUM_DISK_BETA=1"}
WAIVERS = ("UNUSEDSIGNAL", "UNUSEDPARAM", "UNOPTFLAT", "CASEINCOMPLETE", "CASEOVERLAP", "WIDTHTRUNC",
           "WIDTHEXPAND", "SYNCASYNCNET", "PINCONNECTEMPTY", "DECLFILENAME", "IMPLICITSTATIC",
           "VARHIDDEN", "CASEX", "BLKSEQ", "MULTITOP")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sources(output, native_mailbox):
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
    transport = output / "fes_de25_computer_transport.v"
    transport.write_bytes(derive.transport((BOARD / "zx81-hps/fes_de25_computer.v").read_bytes(), native=native_mailbox)[0])
    (output / "generated").mkdir()
    native_header = ROOT / "prototypes/de25_board_control/generated/fes_de25_computer.vh"
    (output / "generated/fes_native_de25_computer.vh").write_bytes(native_header.read_bytes())
    return [HERE / "fes_de25_spectrum_standard.v", transport, BOARD / "ramtest/sdram.v",
            SHARED / "fes_spectrum_core.v", SHARED / "fes_gp_crossing.v", SHARED / "fes_firmware_upload.v",
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
            BOARD / "zx81-hps", BOARD / "scanout", BOARD / "scanout/generated", Z80]


def run(model, native_mailbox=False, build_id=NATIVE_BUILD_ID):
    diagnostic = ROOT / "build/diagnostics/fes-spectrum"
    subprocess.run([sys.executable, str(ROOT / "cores/fes-spectrum/diagnostic/firmware.py"),
                    "--output-dir", str(diagnostic)], cwd=ROOT, check=True)
    parent = ROOT / "build/sim/de25-spectrum"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix=f"standard-{model}-", dir=parent))
    print(f"DE25 Spectrum simulation directory: {output}", flush=True)
    firmware = diagnostic / FIRMWARE[model]
    flags = f"-O2 {BENCH[model]}"
    for i in range(4):
        offset = 32 - (i + 1) * 8
        flags += f" -DSPECTRUM_BUILD_{i}=0x{build_id[offset:offset+8]}u"
    link = []
    if native_mailbox:
        firmware_root = ROOT.parents[1] / "platform/embedded/firmware"
        flags += f" -DSPECTRUM_NATIVE48 -I{firmware_root}"
        objects = []
        for name in ("nano_fabric_stop", "nano_probe_stop"):
            obj = output / (name + ".o")
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-I", str(firmware_root),
                            "-c", str(firmware_root / (name + ".c")), "-o", str(obj)], check=True)
            objects.append(str(obj))
        link = ["-LDFLAGS", " ".join(objects)]
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "4", "--public-flat-rw", "-O3",
                    "-Wall", *["-Wno-" + name for name in WAIVERS], *MODELS[model],
                    *(["-DFES_DE25_NATIVE_SPECTRUM48=1"] if native_mailbox else []),
                    "--top-module", "fes_de25_spectrum_standard", "--Mdir", str(output),
                    "-GSDRAM_POWERUP_CYCLES=16",
                    f"-GBUILD_ID=128'h{build_id}",
                    *["-I" + str(path) for path in [output, *includes()]],
                    "-CFLAGS", flags, *link,
                    *map(str, sources(output, native_mailbox)), str(HERE / "standard_tb.cpp")], cwd=ROOT, check=True)
    executable = output / "Vfes_de25_spectrum_standard"
    subprocess.run([str(executable), str(firmware),
                    str(diagnostic / "tape.pzx")], cwd=output, check=True)
    if native_mailbox:
        fes = ROOT.parents[1]
        tamago = fes / "platform/embedded/tamago"
        payload = (fes / "sources/FogCast/corepackage/testdata/core-bundle-v3/payloads/fes-fixture.rbf").read_bytes()
        controls = output / "native-controls.json"
        controls.write_bytes(load(HERE / "build.py", "spectrum_native_controls").native_controls(
            build_id, hashlib.sha256(payload).hexdigest()))
        env = dict(os.environ, GOWORK="off",
                   FES_SPECTRUM48_MAILBOX=str(executable), FES_SPECTRUM48_FIRMWARE=str(firmware),
                   FES_SPECTRUM48_TAPE=str(diagnostic / "tape.pzx"), FES_SPECTRUM48_CONTROLS=str(controls),
                   FES_SPECTRUM48_BUILD_ID=build_id)
        for test in ("Tape", "Firmware", "SecureStop", "Tenfoot"):
            subprocess.run(["go", "test", "-v", "-count=1", "-timeout=120s", "./computer",
                            "-run", f"^TestSpectrum48{test}RTL$"], cwd=tamago, check=True, env=env)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", choices=("all", *MODELS), default="all")
    parser.add_argument("--native-mailbox", action="store_true",
                        help="also run the native Go firmware proof for the 48K model")
    parser.add_argument("--native-build-id", help="independent 128-bit identity for the native simulation")
    args = parser.parse_args()
    if args.native_build_id is not None and (not args.native_mailbox or
            not re.fullmatch("[0-9a-f]{32}", args.native_build_id) or int(args.native_build_id, 16) == 0):
        parser.error("--native-build-id requires native mailbox mode and a nonzero lowercase 32-digit hex ID")
    if args.native_mailbox and args.model not in ("48", "all"):
        parser.error("--native-mailbox requires model 48 or all")
    for model in (MODELS if args.model == "all" else (args.model,)):
        run(model, args.native_mailbox and model == "48", args.native_build_id or NATIVE_BUILD_ID)


if __name__ == "__main__":
    main()
