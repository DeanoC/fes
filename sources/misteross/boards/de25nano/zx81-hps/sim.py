#!/usr/bin/env python3
"""Exercise DE25 computer MMIO, real HID/CRC mailbox and ZX81 ROM together."""
from pathlib import Path
import argparse
import subprocess
import tempfile
import time

from build import load, retarget_ram, retarget_tape

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
ZX81 = ROOT / "cores/fes-zx81/rtl"
COMMON = ROOT / "cores/fes-common"
Z80 = COMMON / "rtl/z80"


def prepare(bench, output=None):
    if output is None:
        parent = ROOT / "build/sim/de25-zx81-hps"
        parent.mkdir(parents=True, exist_ok=True)
        output = Path(tempfile.mkdtemp(prefix=bench + "-", dir=parent))
    else:
        output = Path(output).resolve()
        output.mkdir(parents=True, exist_ok=False)
    print(f"DE25 computer simulation directory: {output}", flush=True)
    adapted = []
    for filename, derive in (("zx81_dpram.v", retarget_ram),
                             ("zx81_tape_store.v", retarget_tape)):
        path = output / filename
        path.write_bytes(derive((ZX81 / filename).read_bytes())[0])
        adapted.append(path)
    (output / "zx8x.hex").write_bytes((ZX81 / "zx8x.hex").read_bytes())
    runtime = ROOT.parent / "libmister-runtime/src/native/generated/fes_computer.hpp"
    (output / "fes_computer.hpp").write_bytes(runtime.read_bytes())
    sources = [HERE / "fes_de25_computer.v", HERE / "video.v",
               COMMON / "rtl/fes_computer_mailbox.v",
               ZX81 / "zx81_keyboard.v", ZX81 / "zx81_machine_clock.v",
               ZX81 / "zx81_machine.sv", *adapted]
    sources += [Z80 / (name + ".sv") for name in
                ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos")]
    return output, sources


def run(bench):
    if bench == "standard-expansion":
        from standard_expansion_sim import run as expansion
        expansion()
        return
    if bench == "standard":
        run_standard()
        return
    if bench == "board":
        run_board()
        return
    output, sources = prepare(bench)
    # These benches drive top-level pins and only read internal RTL state.
    # Writable internal visibility needlessly makes every signal an input to eval.
    observers = output / "read-only-observers.vlt"
    observers.write_text('`verilator_config\npublic_flat_rd -module "*" -var "*"\n')
    parameters = ["-GBUILD_ID=128'h00112233445566778899aabbccddeeff"] if bench == "endpoint" else []
    testbench = HERE / ("endpoint_tb.cpp" if bench == "endpoint" else "computer_tb.cpp")
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "2",
                    str(observers), "-O3", "--top-module", "fes_de25_computer",
                    "-Wno-UNOPTFLAT", "-Wno-CASEINCOMPLETE", "-Wno-WIDTHTRUNC",
                    "-Wno-WIDTHEXPAND", "-Wno-PINCONNECTEMPTY", "-Wno-IMPLICITSTATIC",
                    "-Wno-CASEX", "-I" + str(HERE), "-I" + str(HERE / "generated"),
                    "-I" + str(COMMON / "generated"), "-I" + str(ZX81), "-I" + str(Z80),
                    "-MAKEFLAGS", "OPT_FAST=-O3",
                    "-CFLAGS", "-O3 -I" + str(output) + " -I" + str(HERE) + " -I" + str(HERE / "generated"),
                    "--Mdir", str(output), *parameters, *map(str, sources), str(testbench)],
                   cwd=ROOT, check=True)
    started = time.monotonic()
    subprocess.run([str(output / "Vfes_de25_computer")], cwd=output, check=True)
    print(f"DE25 computer {bench} simulation elapsed: {time.monotonic() - started:.3f} seconds; output {output}", flush=True)


def run_board():
    parent = ROOT / "build/sim/de25-zx81-hps"
    parent.mkdir(parents=True, exist_ok=True)
    output = Path(tempfile.mkdtemp(prefix="board-", dir=parent))
    print(f"DE25 board simulation directory: {output}", flush=True)
    board = HERE / "board.v"
    display_models = ROOT / "boards/de25nano/display/sim_models.v"
    reset = ROOT / "boards/de25nano/hps/fes_de25_top_reset.v"
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-O2",
                    "--public-flat-rw", "-Wno-DECLFILENAME",
                    "--top-module", "fes_de25_zx81_board", "--Mdir", str(output),
                    str(board), str(display_models), str(reset), str(HERE / "board_tb.cpp")],
                   cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_de25_zx81_board")], cwd=output, check=True)


def run_standard():
    output, sources = prepare("standard")
    scanout = HERE.parent / "scanout"
    display = load(HERE.parent / "display/build.py", "standard_session_display_ram")
    reader = output / "fes_menu_reader_de25.v"
    reader.write_bytes(display.retarget_menu_ram((ROOT / "cores/fes-menu/rtl/fes_menu_reader.v").read_bytes())[0])
    # DI; marker=5a at5000; continuously increment5001 without firmware services.
    program = bytes.fromhex("f3 31 ff 7f 3e 5a 32 00 50 3a 01 50 3c 32 01 50 c3 09 00")
    (output / "standard-session-rom.hex").write_text("".join(f"{b:02x}\n" for b in program + bytes(8192-len(program))))
    sources += [HERE / "fes_de25_zx81_standard.v", HERE / "standard_session_harness.v",
        ZX81 / "fes_zx81_core.v", ZX81 / "zx81_video_720p.v",
        scanout / "fes_de25_session_display.v", scanout / "fes_de25_scanout.v",
        COMMON / "rtl/fes_display_plane.v", COMMON / "rtl/fes_display_cdc.v",
        ROOT / "cores/fes-menu/rtl/fes_menu_control.v", ROOT / "cores/fes-menu/rtl/fes_menu_video.v", reader]
    waivers = ("UNUSEDSIGNAL", "UNUSEDPARAM", "UNOPTFLAT", "CASEINCOMPLETE", "WIDTHTRUNC",
        "WIDTHEXPAND", "SYNCASYNCNET", "PINCONNECTEMPTY", "DECLFILENAME", "IMPLICITSTATIC", "VARHIDDEN", "CASEX", "BLKSEQ")
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "2", "--public-flat-rw",
        "-O3", "-Wall", *["-Wno-" + name for name in waivers],
        "--top-module", "fes_de25_standard_session_harness", "--Mdir", str(output),
        "-I" + str(HERE), "-I" + str(COMMON / "generated"), "-I" + str(ZX81),
        "-I" + str(Z80), "-I" + str(scanout),
        "-CFLAGS", "-O2 -DFES_DE25_STANDARD_CORE -I" + str(scanout),
        *map(str, sources), str(scanout / "session_tb.cpp")], cwd=ROOT, check=True)
    subprocess.run([str(output / "Vfes_de25_standard_session_harness")], cwd=output, check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bench", choices=("all", "endpoint", "machine", "board", "standard", "standard-expansion"), default="all")
    selected = parser.parse_args().bench
    for bench in (("endpoint", "machine", "board", "standard", "standard-expansion") if selected == "all" else (selected,)):
        run(bench)


if __name__ == "__main__":
    main()
