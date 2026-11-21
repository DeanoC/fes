#!/usr/bin/env python3
"""Integrate the standard PC/XT DOS machine into the managed DE25 GHRD (see ../machine/board.py)."""
from pathlib import Path
import importlib.util
import sys

HERE = Path(__file__).resolve().parent
BOARD = "sources/misteross/boards/de25nano/"
CORE = "sources/misteross/cores/fes-pcxt/"
COMMON = "sources/misteross/cores/fes-common/"
RAMTEST = "sources/misteross/cores/fes-ramtest/rtl/"
NAME = "PC/XT"
DIRECTORY = "pcxt"
CORE_ID = "fes.pcxt-dos"
COMPONENT = "fes_de25_pcxt_standard"
SDC = BOARD + "pcxt/pcxt-clocks.sdc"
MODELS = {"xt": None}
# The exact image the upload endpoint takes: the 64 KiB system ROM window
# (F0000-FFFFF) and the 4 KiB character ROM.
FIRMWARE_BYTES = {"xt": 69632}
# The DE10 machine's clocks: one 100 MHz for the CPU, motherboard and SDRAM,
# with the forwarded SDRAM clock +1.25 ns, the CAS-3 read-word handoff and
# the DQ capture clock beside it (fes_pcxt_top's memory PLL), and the audio
# master on the same PLL. The handoff is +9.25 ns, the nearest phase of this
# PLL to the DE10's +9.375 ns.
MACHINE_HZ = 100000000
PLL_IP = BOARD + "pcxt/machine_pll.ip"
PLL_PARAMETERS = {"gui_number_of_clocks": "5", "gui_output_clock_frequency0": "100.0",
                  "gui_output_clock_frequency1": "12.295082", "gui_output_clock_frequency2": "100.0",
                  "gui_phase_shift2": "1250.0", "gui_output_clock_frequency3": "100.0",
                  "gui_phase_shift3": "9250.0", "gui_output_clock_frequency4": "100.0",
                  "gui_phase_shift4": "0.0"}
SDRAM_CLOCKS = (("clk_forward", 2), ("clk_handoff", 3), ("clk_capture", 4))
PLL_COMMENT = """// One PLL on CLOCK1_50 in place of the stock audio PLL, whose pin can feed no
// second one: x 30, then /15 for the machine's 100 MHz and its three SDRAM
// phases, as the DE10 PC/XT board makes them, and /122 for the audio master
// clock (12.295082 MHz, +564 ppm, unrelated to the machine as on the DE10).
"""
# Synthesis choices: none beyond the shared board's.
ASSIGNMENTS = ()
# Qsys writes the peripheral subsystem for the component. Reviewed against
# the Atari ST's (a08edf0b...): the component's name, and the SDRAM conduit's
# three clock inputs exported beside its pins; nothing else.
PARENT_SHA256 = "b9565f14391f812239e70a07163c4aa887e2db06573e3adacf942b82d9c9039e"
# PC/XT machine sources compiled as they are on the DE10.
MACHINE_FILES = (
    "rtl/pcxt_isa_bridge.sv", "rtl/pcxt_isa_boundary.sv", "rtl/pcxt_isa_endpoint.sv",
    "expansions/pcxt_isa_probe.sv", "expansions/pcxt_6845.sv", "expansions/pcxt_display_card.sv",
    "expansions/pcxt_cga_card.sv", "expansions/pcxt_mda_card.sv", "expansions/pcxt_fdc_card.sv",
    "rtl/pcxt_display_bus.sv", "vendor/kf8255/KF8255.sv", "vendor/kf8255/KF8255_Control_Logic.sv",
    "vendor/kf8255/KF8255_Group.sv", "vendor/kf8255/KF8255_Port.sv", "vendor/kf8255/KF8255_Port_C.sv",
    "rtl/pcxt_xtio.sv", "rtl/pcxt_xt_keyboard.sv", "rtl/pcxt_speaker.sv", "rtl/pcxt_motherboard.sv",
    "rtl/pcxt_logic_operand1_mux.sv", "vendor/mcl86/mcl86_adder.sv", "vendor/mcl86/mcl86_eu_core.sv",
    "vendor/mcl86/mcl86_biu_max.sv", "vendor/mcl86/mcl86_ucode.sv", "vendor/mcl86/i8088.sv",
    "rtl/pcxt_cpu_bus.sv", "rtl/pcxt_cpu.sv", "rtl/pcxt_memory.sv", "rtl/pcxt_video.sv",
    "rtl/pcxt_video_output.sv", "rtl/pcxt_machine.sv", "rtl/pcxt_disk_loader.sv",
    "rtl/pcxt_sector_writer.sv", "rtl/pcxt_media_arbiter.sv", "rtl/pcxt_system.sv",
    "rtl/pcxt_media_writer.sv", "rtl/pcxt_media.sv", "rtl/pcxt_boot_media_gate.sv",
    "rtl/pcxt_hid_keyboard.sv", "rtl/pcxt_xt_keyboard_bat.sv")
COMMON_FILES = (
    "vendor/kf8259/KF8259.sv", "vendor/kf8259/KF8259_Bus_Control_Logic.sv",
    "vendor/kf8259/KF8259_Control_Logic.sv", "vendor/kf8259/KF8259_In_Service.sv",
    "vendor/kf8259/KF8259_Interrupt_Request.sv", "vendor/kf8259/KF8259_Priority_Resolver.sv",
    "vendor/kf8253/KF8253.sv", "vendor/kf8253/KF8253_Control_Logic.sv", "vendor/kf8253/KF8253_Counter.sv",
    "rtl/fes_dma8237.sv", "rtl/fes_video_part_direct.v")
UCODE = CORE + "vendor/mcl86/mcl86_ucode.mem"
# The vendored chips' own headers, included by name beside their sources.
HEADERS = (CORE + "vendor/kf8255/KF8255_Definitions.svh", COMMON + "vendor/kf8253/KF8253_Definitions.svh",
           COMMON + "vendor/kf8259/KF8259_Common_Package.svh")
SHARED_FILES = ("fes_pcxt_core.v", "fes_pcxt_firmware.v")
LOCAL_FILES = ("fes_de25_pcxt_standard.v", "fes_de25_pcxt_standard_hw.tcl", "pcxt-clocks.sdc",
               "machine_pll.ip", "derive.py", "build.py")
SOURCES = ({BOARD + "pcxt/" + name for name in LOCAL_FILES} |
           {BOARD + "shared-rtl/" + name for name in SHARED_FILES} |
           {CORE + name for name in MACHINE_FILES} | {COMMON + name for name in COMMON_FILES} |
           set(HEADERS) | {UCODE, COMMON + "generated/fes_pcxt_isa.vh",
            COMMON + "generated/fes_video_part.vh", COMMON + "rtl/coleco_video_dpram.v",
            RAMTEST + "sdram_addon_port.v", RAMTEST + "sdram_cas3_capture.v",
            BOARD + "atari-st/derive.py"})


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


board = load(HERE.parent / "machine/board.py", "de25_pcxt_machine_board")


def component_files(sources):
    """The machine's own component files; the board adds the shared shell."""
    derive = board.module(sources, BOARD + "pcxt/derive.py", "pcxt_selected_derive")
    st = board.module(sources, BOARD + "atari-st/derive.py", "pcxt_selected_st_derive")
    files, derived = {}, {}
    for name in ("fes_de25_pcxt_standard.v", "fes_de25_pcxt_standard_hw.tcl"):
        files[name] = sources[BOARD + "pcxt/" + name]
    for name in SHARED_FILES:
        files[name] = sources[BOARD + "shared-rtl/" + name]
    for name in MACHINE_FILES:
        files[Path(name).name] = sources[CORE + name]
    for name in COMMON_FILES:
        files[Path(name).name] = sources[COMMON + name]
    files["sdram_cas3_capture.v"] = sources[RAMTEST + "sdram_cas3_capture.v"]
    for path in HEADERS:
        files[Path(path).name] = sources[path]
    for name in ("fes_pcxt_isa.vh", "fes_video_part.vh"):
        files["generated/" + name] = sources[COMMON + "generated/" + name]
    files.update(derive.control_rom(sources[UCODE]))
    files["coleco_video_dpram_de25.v"], derived["coleco_video_dpram_de25.v"] = derive.retarget_video_ram(
        sources[COMMON + "rtl/coleco_video_dpram.v"])
    files["sdram_addon_port_de25.v"], derived["sdram_addon_port_de25.v"] = st.retarget_sdram(
        sources[RAMTEST + "sdram_addon_port.v"])
    files["fes_de25_computer_transport.v"], derived["fes_de25_computer_transport.v"] = st.transport(
        sources[BOARD + "zx81-hps/fes_de25_computer.v"])
    return files, derived


def project_files(sources):
    """The 8088 control ROM's byte lanes beside the project, where elaboration reads them by name."""
    derive = board.module(sources, BOARD + "pcxt/derive.py", "pcxt_selected_derive")
    return {name: data for name, data in derive.control_rom(sources[UCODE]).items() if name.endswith(".mem")}


if __name__ == "__main__":
    board.main(sys.modules[__name__])
