#!/usr/bin/env python3
"""Simulate the DE25 PC/XT standard component with GLaBIOS and the FreeDOS floppy.

The default run first runs the DE10 video benches on the derived mailbox
crossings, then checks the upload, the BIOS POST from SDRAM and the floppy
upload; `--dos` boots FreeDOS to its A:\\> prompt, which takes far longer.
"""
from pathlib import Path
import argparse
import importlib.util
import subprocess
import sys
import tempfile

HERE = Path(__file__).resolve().parent
BOARD = HERE.parent
ROOT = BOARD.parents[1]
SHARED = BOARD / "shared-rtl"
COMMON = ROOT / "cores/fes-common"
PCXT = ROOT / "cores/fes-pcxt"
WAIVERS = ("WIDTH", "TIMESCALEMOD", "MULTIDRIVEN", "INITIALDLY", "ALWCOMBORDER", "UNOPTFLAT",
           "BLKANDNBLK", "CASEINCOMPLETE", "CASEOVERLAP", "PINCONNECTEMPTY", "DECLFILENAME",
           "UNUSEDSIGNAL", "UNUSEDPARAM", "SYNCASYNCNET", "IMPLICITSTATIC", "VARHIDDEN", "CASEX",
           "BLKSEQ", "MULTITOP", "PINMISSING", "UNDRIVEN", "GENUNNAMED", "SELRANGE", "CMPCONST")


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def firmware_image():
    """The upload image: GLaBIOS top-aligned in the F0000 window, then the font."""
    sys.path.insert(0, str(ROOT / "scripts"))
    import pcxt_bios
    import sim_fes_pcxt
    lock = pcxt_bios.json.loads((PCXT / "firmware/hardware-dos.json").read_text())
    bios = pcxt_bios.checked_file(PCXT / lock["bios"]["file"], lock["bios"])
    return pcxt_bios.firmware_window(bios) + sim_fes_pcxt.cga_font()


def floppy():
    sys.path.insert(0, str(ROOT / "scripts"))
    import pcxt_bios
    lock = pcxt_bios.json.loads((PCXT / "firmware/hardware-dos.json").read_text())
    return pcxt_bios.checked_file(PCXT / lock["floppy"]["file"], lock["floppy"])


def sources(output):
    """The compiled source set: DE10 machine files as they are, plus the DE25 derivations."""
    build = load(HERE / "build.py", "de25_pcxt_build")
    derive = load(HERE / "derive.py", "de25_pcxt_derive")
    st = load(BOARD / "atari-st/derive.py", "de25_pcxt_st_derive")
    display = load(BOARD / "display/build.py", "de25_pcxt_display_ram")
    for name, data in derive.control_rom((ROOT.parent.parent / build.UCODE).read_bytes()).items():
        (output / name).write_bytes(data)
    sdram = output / "sdram_addon_port_de25.v"
    sdram.write_bytes(st.retarget_sdram((ROOT / "cores/fes-ramtest/rtl/sdram_addon_port.v").read_bytes())[0])
    video = output / "pcxt_video_de25.sv"
    video.write_bytes(derive.retarget_video((PCXT / "rtl/pcxt_video.sv").read_bytes())[0])
    ram = output / "coleco_video_dpram_de25.v"
    ram.write_bytes(derive.retarget_video_ram((COMMON / "rtl/coleco_video_dpram.v").read_bytes())[0])
    reader = output / "fes_menu_reader_de25.v"
    reader.write_bytes(display.retarget_menu_ram((SHARED / "fes_menu_reader.v").read_bytes())[0])
    transport = output / "fes_de25_computer_transport.v"
    transport.write_bytes(st.transport((BOARD / "zx81-hps/fes_de25_computer.v").read_bytes())[0])
    return [HERE / "fes_de25_pcxt_standard.v", transport, sdram, video, ram, reader,
            ROOT / "cores/fes-ramtest/rtl/sdram_cas3_capture.v",
            SHARED / "fes_pcxt_core.v", SHARED / "fes_pcxt_firmware.v", SHARED / "fes_gp_crossing.v",
            SHARED / "fes_firmware_upload.v", SHARED / "fes_computer_mailbox.v",
            SHARED / "fes_display_plane.v", SHARED / "fes_menu_control.v", SHARED / "fes_menu_video.v",
            BOARD / "scanout/fes_de25_session_display.v", BOARD / "scanout/fes_de25_scanout.v",
            COMMON / "rtl/fes_display_cdc.v", PCXT / "sim/chipset.vlt", PCXT / "sim/cpu.vlt", PCXT / "sim/de25_video.vlt",
            *(PCXT / name for name in build.MACHINE_FILES),
            *(COMMON / name for name in build.COMMON_FILES)]


def includes(output):
    return [output, COMMON / "generated", COMMON / "rtl", SHARED / "generated", BOARD / "zx81-hps",
            BOARD / "scanout", BOARD / "scanout/generated", PCXT / "vendor/kf8255",
            COMMON / "vendor/kf8253", COMMON / "vendor/kf8259"]


def run_video():
    """The DE10 video benches, CGA and MDA, on the derived mailbox crossings."""
    derive = load(HERE / "derive.py", "de25_pcxt_derive")
    parent = ROOT / "build/sim/de25-pcxt"
    parent.mkdir(parents=True, exist_ok=True)
    for mda in (False, True):
        output = Path(tempfile.mkdtemp(prefix="video-", dir=parent))
        video = output / "pcxt_video_de25.sv"
        video.write_bytes(derive.retarget_video((PCXT / "rtl/pcxt_video.sv").read_bytes())[0])
        # The DE10 source trips WIDTHEXPAND on Verilator 5.020 as it does in its
        # own bench; the derived file keeps the module name pcxt_video.
        subprocess.run(["verilator", "--cc", "--exe", "--build", "-Wall", "-Wno-WIDTHEXPAND", "-Wno-DECLFILENAME", "--timescale-override", "1ns/1ps",
                        str(PCXT / "sim/cpu.vlt"), str(PCXT / "sim/de25_video.vlt"), *(["-GMDA=1'b1"] if mda else []),
                        "--top-module", "video_top", "--Mdir", str(output), "-I" + str(COMMON / "generated"),
                        "-CFLAGS", "-std=c++17 -O2",
                        str(COMMON / "rtl/coleco_video_dpram.v"), str(COMMON / "rtl/fes_video_part_direct.v"),
                        str(COMMON / "rtl/fes_video_part_scanlines.v"), str(video),
                        str(PCXT / "rtl/pcxt_video_output.sv"), str(PCXT / "sim/video_top.sv"),
                        str(PCXT / "sim/video_tb.cpp")], cwd=ROOT, check=True)
        subprocess.run([str(output / "Vvideo_top"), *(["mda"] if mda else [])], cwd=output, check=True)


def run(dos=False):
    parent = ROOT / "build/sim/de25-pcxt"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="standard-", dir=parent))
    print(f"DE25 PC/XT simulation directory: {output}", flush=True)
    (output / "firmware.bin").write_bytes(firmware_image())
    (output / "floppy.img").write_bytes(floppy())
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "4", "--public-flat-rw", "-O3",
                    *["-Wno-" + name for name in WAIVERS],
                    "--top-module", "fes_de25_pcxt_standard", "--Mdir", str(output),
                    "-GBUILD_ID=128'h00112233445566778899aabbccddeeff",
                    *["-I" + str(path) for path in includes(output)],
                    "-CFLAGS", "-O2 -std=c++17", *map(str, sources(output)),
                    str(HERE / "standard_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_de25_pcxt_standard"), str(output / "firmware.bin"),
                    str(output / "floppy.img"), *(["dos"] if dos else [])], cwd=output, check=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dos", action="store_true", help="boot FreeDOS to its prompt")
    arguments = parser.parse_args()
    run_video()
    run(arguments.dos)
