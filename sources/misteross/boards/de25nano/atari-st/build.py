#!/usr/bin/env python3
"""Integrate the standard Atari ST into the managed DE25 GHRD (see ../machine/board.py)."""
from pathlib import Path
import importlib.util
import sys

HERE = Path(__file__).resolve().parent
BOARD = "sources/misteross/boards/de25nano/"
CORE = "sources/misteross/cores/fes-atari-st/rtl/"
COMMON = "sources/misteross/cores/fes-common/"
CPU = COMMON + "rtl/fx68k/"
SDRAM = "sources/misteross/cores/fes-ramtest/rtl/sdram_addon_port.v"
NAME = "Atari ST"
DIRECTORY = "atari-st"
CORE_ID = "fes.atari-st"
COMPONENT = "fes_de25_atari_st_standard"
SDC = BOARD + "atari-st/atari-st-clocks.sdc"
MODELS = {"520st": None}
# The exact image the upload endpoint takes.
FIRMWARE_BYTES = {"520st": 196608}
# Synthesis choices in unchanged DE10 sources that the signoff lint refuses:
# the machine reset is one register for some eight thousand endpoints; the
# 68000's ALU rows take one instruction bit as a synchronous clear on both
# polarities; and one SDRAM address register takes the reset as a clock enable
# where every other register takes it as a synchronous clear.
ASSIGNMENTS = (
    ("MAX_FANOUT", "256", "core|machine_reset"),
    ("ALLOW_SYNCH_CTRL_USAGE", "OFF", "core|system|machine|cpu|cpu|excUnit|alu|row[*]"),
    ("AUTO_CLOCK_ENABLE_RECOGNITION", "OFF", "core|memory|controller|sdram_a_r[10]"),
)
# Qsys writes the same peripheral subsystem as for the Spectrum component:
# the two components have the same interfaces. See ../spectrum/build.py.
PARENT_SHA256 = "a08edf0b098e0ddb583a98963efa6bcce1735fc41a0a28c4a9019e2eb27f1ca8"
# ST machine sources compiled as they are on the DE10.
MACHINE_FILES = ("st_system.sv", "st_cpu.sv", "st_machine.sv", "st_io.sv", "st_mfp.sv", "st_acia.sv",
                 "st_ikbd.sv", "st_ym2149.sv", "st_ym_filter.sv", "st_ym_mixer.sv", "st_ym_mix_rom.sv",
                 "st_floppy.sv", "st_floppy_writer.sv", "st_native_border.sv",
                 "st_video.sv", "st_media_writer.sv", "st_media_port.sv")
CPU_FILES = ("fx68k.sv", "fx68kAlu.sv", "uaddrPla.sv", "microrom.mem", "nanorom.mem")
SHARED_FILES = ("fes_atari_st_core.v", "fes_st_rom_sdram.v")
LOCAL_FILES = ("fes_de25_atari_st_standard.v", "fes_de25_atari_st_standard_hw.tcl",
               "atari-st-clocks.sdc", "derive.py", "build.py")
SOURCES = ({BOARD + "atari-st/" + name for name in LOCAL_FILES} |
           {BOARD + "shared-rtl/" + name for name in SHARED_FILES} |
           {CORE + name for name in MACHINE_FILES + ("st_native_low_video.sv", "st_video_adapter.sv", "st_memory.sv")} | {CPU + name for name in CPU_FILES} |
           {SDRAM, COMMON + "rtl/fes_video_part_direct.v", COMMON + "generated/fes_video_part.vh",
            "sources/misteross/cores/fes-zx81/expansions/zonx_ay.v"})


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


board = load(HERE.parent / "machine/board.py", "de25_atari_st_machine_board")


def component_files(sources):
    """The machine's own component files; the board adds the shared shell."""
    derive = board.module(sources, BOARD + "atari-st/derive.py", "atari_st_selected_derive")
    files, derived = {}, {}
    for name in ("fes_de25_atari_st_standard.v", "fes_de25_atari_st_standard_hw.tcl"):
        files[name] = sources[BOARD + "atari-st/" + name]
    for name in SHARED_FILES:
        files[name] = sources[BOARD + "shared-rtl/" + name]
    for name in MACHINE_FILES:
        files[name] = sources[CORE + name]
    for name in CPU_FILES:
        files[name] = sources[CPU + name]
    files["zonx_ay.v"] = sources["sources/misteross/cores/fes-zx81/expansions/zonx_ay.v"]
    files["fes_video_part_direct.v"] = sources[COMMON + "rtl/fes_video_part_direct.v"]
    files["generated/fes_video_part.vh"] = sources[COMMON + "generated/fes_video_part.vh"]
    files["st_native_low_video_de25.sv"], derived["st_native_low_video_de25.sv"] = derive.retarget_native_video(
        sources[CORE + "st_native_low_video.sv"])
    files["st_video_adapter_de25.sv"], derived["st_video_adapter_de25.sv"] = derive.retarget_video_adapter(
        sources[CORE + "st_video_adapter.sv"])
    files["st_memory_de25.sv"], derived["st_memory_de25.sv"] = derive.retarget_memory(sources[CORE + "st_memory.sv"])
    files["sdram_addon_port_de25.v"], derived["sdram_addon_port_de25.v"] = derive.retarget_sdram(sources[SDRAM])
    files["fes_de25_computer_transport.v"], derived["fes_de25_computer_transport.v"] = derive.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, derived


def project_files(sources):
    """The 68000 microcode beside the project, where elaboration reads it by name."""
    return {name: sources[CPU + name] for name in ("microrom.mem", "nanorom.mem")}


if __name__ == "__main__":
    board.main(sys.modules[__name__])
