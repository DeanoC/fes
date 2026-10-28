#!/usr/bin/env python3
"""Boot the real ZX81 on the DE25 shell with digital board-boundary models."""
from pathlib import Path
import subprocess

from build import retarget_ram, retarget_video

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
ZX81 = ROOT / "cores/fes-zx81/rtl"
Z80 = ROOT / "cores/fes-common/rtl/z80"


def main():
    output = ROOT / "build/sim/de25-zx81"
    output.mkdir(parents=True, exist_ok=True)
    ram = output / "zx81_dpram_de25.v"
    video = output / "zx81_video_720p_de25.v"
    ram.write_bytes(retarget_ram((ZX81 / "zx81_dpram.v").read_bytes())[0])
    video.write_bytes(retarget_video((ZX81 / "zx81_video_720p.v").read_bytes())[0])
    (output / "zx8x.hex").write_bytes((ZX81 / "zx8x.hex").read_bytes())
    sources = [HERE / "top.v", HERE / "sim_models.v",
               HERE.parent / "hps/fes_de25_top_reset.v",
               HERE / "keyboard.v", ZX81 / "zx81_machine_clock.v",
               ZX81 / "zx81_machine.sv", ram, video]
    sources += [Z80 / (name + ".sv") for name in
                ("fes_z80_alu", "fes_z80_engine", "fes_z80_bus", "fes_z80_nmos")]
    # QUARTUS is deliberately absent: the real RAM's matching registered-read
    # behavioral branch loads the repository hex ROM. No CPU/firmware model.
    subprocess.run(["verilator", "--cc", "--exe", "--build", "-j", "2",
                    "--public-flat-rw", "-O3", "--top-module", "de25nano_zx81",
                    "-Wno-UNOPTFLAT", "-Wno-CASEINCOMPLETE", "-Wno-WIDTHTRUNC",
                    "-Wno-WIDTHEXPAND", "-Wno-PINCONNECTEMPTY", "-Wno-IMPLICITSTATIC",
                    "-Wno-CASEX", "-I" + str(ZX81), "-I" + str(Z80),
                    "-CFLAGS", "-O2", "--Mdir", str(output),
                    *map(str, sources), str(HERE / "board_tb.cpp")],
                   cwd=ROOT, check=True)
    subprocess.run([str(output / "Vde25nano_zx81")], cwd=output, check=True)


if __name__ == "__main__":
    main()
