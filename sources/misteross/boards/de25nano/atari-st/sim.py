#!/usr/bin/env python3
"""Simulate the DE25 Atari ST standard component with the open diagnostic firmware."""
from pathlib import Path
import importlib.util
import shutil
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
BOARD = HERE.parent
ROOT = BOARD.parents[1]
ST = ROOT / "cores/fes-atari-st/rtl"
CPU = ROOT / "cores/fes-common/rtl/fx68k"
SHARED = BOARD / "shared-rtl"
COMMON = ROOT / "cores/fes-common"
# Upstream FX68K uses unpacked structures with independently assigned fields;
# these suppressions match the qualified DE10 simulations.
WAIVERS = ("WIDTH", "TIMESCALEMOD", "MULTIDRIVEN", "INITIALDLY", "ALWCOMBORDER", "UNOPTFLAT",
           "BLKANDNBLK", "CASEINCOMPLETE", "CASEOVERLAP", "PINCONNECTEMPTY", "DECLFILENAME",
           "UNUSEDSIGNAL", "UNUSEDPARAM", "SYNCASYNCNET", "IMPLICITSTATIC", "VARHIDDEN", "CASEX",
           "BLKSEQ", "MULTITOP", "PINMISSING", "UNDRIVEN", "GENUNNAMED")
MACHINE = ("st_system.sv", "st_cpu.sv", "st_machine.sv", "st_io.sv", "st_mfp.sv", "st_acia.sv",
           "st_ikbd.sv", "st_ym2149.sv", "st_ym_filter.sv", "st_ym_mixer.sv", "st_ym_mix_rom.sv",
           "st_floppy.sv", "st_floppy_writer.sv", "st_native_border.sv", "st_native_low_video.sv",
           "st_video.sv", "st_video_adapter.sv", "st_memory.sv", "st_media_writer.sv", "st_media_port.sv")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def sources(output):
    """The compiled source set: DE10 machine files as they are, plus the DE25 derivations."""
    derive = load(HERE / "derive.py", "de25_atari_st_derive")
    display = load(BOARD / "display/build.py", "de25_atari_st_display_ram")
    sdram = output / "sdram_addon_port_de25.v"
    sdram.write_bytes(derive.retarget_sdram((ROOT / "cores/fes-ramtest/rtl/sdram_addon_port.v").read_bytes())[0])
    reader = output / "fes_menu_reader_de25.v"
    reader.write_bytes(display.retarget_menu_ram((SHARED / "fes_menu_reader.v").read_bytes())[0])
    transport = output / "fes_de25_computer_transport.v"
    transport.write_bytes(derive.transport((BOARD / "zx81-hps/fes_de25_computer.v").read_bytes())[0])
    return [HERE / "fes_de25_atari_st_standard.v", transport, sdram,
            SHARED / "fes_atari_st_core.v", SHARED / "fes_gp_crossing.v", SHARED / "fes_firmware_upload.v",
            SHARED / "fes_st_firmware_rom.v", SHARED / "fes_computer_mailbox.v",
            SHARED / "fes_display_plane.v", SHARED / "fes_menu_control.v", SHARED / "fes_menu_video.v",
            reader, BOARD / "scanout/fes_de25_session_display.v", BOARD / "scanout/fes_de25_scanout.v",
            COMMON / "rtl/fes_display_cdc.v", COMMON / "rtl/fes_video_part_direct.v",
            *(ST / name for name in MACHINE), ROOT / "cores/fes-zx81/expansions/zonx_ay.v",
            *(CPU / name for name in ("fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv"))]


def includes():
    return [ST, COMMON / "generated", COMMON / "rtl", SHARED / "generated", BOARD / "zx81-hps",
            BOARD / "scanout", BOARD / "scanout/generated", CPU]


def run():
    subprocess.run([sys.executable, str(ROOT / "cores/fes-atari-st/sim/check_sources.py")], cwd=ROOT, check=True)
    diagnostic = ROOT / "build/diagnostics/fes-atari-st"
    subprocess.run([sys.executable, str(ROOT / "cores/fes-atari-st/diagnostic/firmware.py"),
                    "--out", str(diagnostic)], cwd=ROOT, check=True)
    parent = ROOT / "build/sim/de25-atari-st"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="standard-", dir=parent))
    print(f"DE25 Atari ST simulation directory: {output}", flush=True)
    for name in ("microrom.mem", "nanorom.mem"):
        shutil.copy(CPU / name, output / name)
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "4", "--public-flat-rw", "-O3",
                    *["-Wno-" + name for name in WAIVERS],
                    "--top-module", "fes_de25_atari_st_standard", "--Mdir", str(output),
                    "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
                    *["-I" + str(path) for path in includes()],
                    "-CFLAGS", "-O2 -std=c++17", *map(str, sources(output)),
                    str(HERE / "standard_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_de25_atari_st_standard"), str(diagnostic / "firmware.rom")],
                   cwd=output, check=True)


if __name__ == "__main__":
    run()
