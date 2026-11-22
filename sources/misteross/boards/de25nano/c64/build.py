#!/usr/bin/env python3
"""Integrate the standard Commodore 64 into the managed DE25 GHRD (see ../machine/board.py)."""
from pathlib import Path
import importlib.util
import sys

HERE = Path(__file__).resolve().parent
BOARD = "sources/misteross/boards/de25nano/"
CORE = "sources/misteross/cores/fes-c64/rtl/"
COMMON = "sources/misteross/cores/fes-common/"
CPU = COMMON + "rtl/cpu6502/"
NAME = "C64"
DIRECTORY = "c64"
CORE_ID = "fes.c64"
COMPONENT = "fes_de25_c64_standard"
SDC = BOARD + "c64/c64-clocks.sdc"
MODELS = {"pal": None}
# The exact image the upload endpoint takes: BASIC, KERNAL, character and
# 1541 ROMs in c64_rom's order.
FIRMWARE_BYTES = {"pal": 36864}
# Synthesis choices in unchanged DE10 sources that the signoff lint refuses.
# The SID keeps state through a warm reset (the oscillators' phase, as reSID
# does), so its reset is also a hold; mapped onto register clock enables, its
# one reset register drives both synchronous clears and enables (LNT-30010).
# The SID's holds are built in logic instead.
ASSIGNMENTS = (
    ("AUTO_CLOCK_ENABLE_RECOGNITION", "OFF", "core|machine|sid"),
)
# Qsys writes the same peripheral subsystem as for the Spectrum component:
# the two components have the same interfaces. See ../spectrum/build.py.
PARENT_SHA256 = "a08edf0b098e0ddb583a98963efa6bcce1735fc41a0a28c4a9019e2eb27f1ca8"
# C64 machine sources compiled as they are on the DE10. The opt-in reSID
# filter chain is not selected by the package (RESID_FILTER is off), so its
# Cyclone V ROM primitives are not part of the design.
MACHINE_FILES = ("c64_vic.v", "c64_vic_graphics_pixels.sv", "c64_vic_sprite_pixels.sv",
                 "c64_vic_collisions.sv", "c64_vic_fetch.sv", "c64_vic_sprite_fetch.sv",
                 "c64_sid.v", "c64_sid_envelope.sv", "c64_sid_oscillator.sv", "c64_sid_wave_tables.sv",
                 "c64_sid_svf.sv", "c64_cia.v", "c64_drive1541.sv", "c64_via.v", "c64_gcr_read.v",
                 "c64_d64_medium.sv", "c64_keyboard.v",
                 "c64_bus.vh", "c64_sid_combined_waves.vh", "c64_sid_8580_waves.vh", "c64_sid_svf_tables.vh")
CPU_FILES = ("cpu6502.v", "cpu6502_alu.v")
SHARED_FILES = ("fes_c64_core.v", "fes_c64_firmware_rom.v")
LOCAL_FILES = ("fes_de25_c64_standard.v", "fes_de25_c64_standard_hw.tcl", "c64-clocks.sdc",
               "derive.py", "build.py")
SOURCES = ({BOARD + "c64/" + name for name in LOCAL_FILES} |
           {BOARD + "shared-rtl/" + name for name in SHARED_FILES} |
           {CORE + name for name in MACHINE_FILES + ("c64_machine.sv", "c64_ram.v", "c64_color_ram.v",
                                                    "c64_disk_store.v", "c64_frame_store.sv")} |
           {CPU + name for name in CPU_FILES})


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


board = load(HERE.parent / "machine/board.py", "de25_c64_machine_board")


def component_files(sources):
    """The machine's own component files; the board adds the shared shell."""
    derive = board.module(sources, BOARD + "c64/derive.py", "c64_selected_derive")
    files, derived = {}, {}
    for name in ("fes_de25_c64_standard.v", "fes_de25_c64_standard_hw.tcl"):
        files[name] = sources[BOARD + "c64/" + name]
    for name in SHARED_FILES:
        files[name] = sources[BOARD + "shared-rtl/" + name]
    for name in MACHINE_FILES:
        files[name] = sources[CORE + name]
    for name in CPU_FILES:
        files[name] = sources[CPU + name]
    files["c64_machine_de25.sv"], derived["c64_machine_de25.sv"] = derive.retarget_machine(
        sources[CORE + "c64_machine.sv"])
    for name in derive.RAM_FILES:
        target = name.replace(".v", "_de25.v")
        files[target], derived[target] = derive.retarget_ram(name, sources[CORE + name])
    files["c64_disk_store_de25.v"], derived["c64_disk_store_de25.v"] = derive.retarget_disk_store(
        sources[CORE + "c64_disk_store.v"])
    files["c64_frame_store_de25.sv"], derived["c64_frame_store_de25.sv"] = derive.retarget_frame_store(
        sources[CORE + "c64_frame_store.sv"])
    files["fes_de25_computer_transport.v"], derived["fes_de25_computer_transport.v"] = derive.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, derived


def project_files(sources):
    return {}


if __name__ == "__main__":
    board.main(sys.modules[__name__])
