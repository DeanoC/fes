#!/usr/bin/env python3
"""Exercise DE25 computer MMIO, real HID/CRC mailbox and ZX81 ROM together."""
from pathlib import Path
import argparse
import subprocess
import tempfile
import time

from build import retarget_ram, retarget_tape

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
    if bench == "board":
        run_board()
        return
    output, sources = prepare(bench)
    parameters = ["-GBUILD_ID=128'h00112233445566778899aabbccddeeff"] if bench == "endpoint" else []
    testbench = HERE / ("endpoint_tb.cpp" if bench == "endpoint" else "computer_tb.cpp")
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "2",
                    "--public-flat-rw", "-O3", "--top-module", "fes_de25_computer",
                    "-Wno-UNOPTFLAT", "-Wno-CASEINCOMPLETE", "-Wno-WIDTHTRUNC",
                    "-Wno-WIDTHEXPAND", "-Wno-PINCONNECTEMPTY", "-Wno-IMPLICITSTATIC",
                    "-Wno-CASEX", "-I" + str(HERE), "-I" + str(HERE / "generated"),
                    "-I" + str(COMMON / "generated"), "-I" + str(ZX81), "-I" + str(Z80),
                    "-CFLAGS", "-O2 -I" + str(output) + " -I" + str(HERE) + " -I" + str(HERE / "generated"),
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bench", choices=("all", "endpoint", "machine", "board"), default="all")
    selected = parser.parse_args().bench
    for bench in (("endpoint", "machine", "board") if selected == "all" else (selected,)):
        run(bench)


if __name__ == "__main__":
    main()
