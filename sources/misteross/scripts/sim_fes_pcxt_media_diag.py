#!/usr/bin/env python3
"""Prove the PC/XT diagnostic detects a real mailbox-loaded sector through FDC/DMA."""
import os
from pathlib import Path
import subprocess
import sim_fes_pcxt as sim

ROOT = Path(__file__).resolve().parents[1]

def main():
    build = ROOT / 'build/sim/fes-pcxt/media-diag'
    build.mkdir(parents=True, exist_ok=True)
    for name in sim.CHIP_VENDORS:
        sim.verify_vendor(name)
    sim.board_inputs(build)
    (build / 'font.hex').write_text('81\n' * 4096)
    sources = tuple(p for p in sim.SYSTEM_SOURCES if not p.endswith('/pcxt_boot.sv')) + (
        'cores/fes-common/rtl/fes_computer_mailbox.v', sim.MEMORY_SOURCES[-1],
        'cores/fes-pcxt/sim/media_diag_top.sv', 'cores/fes-pcxt/sim/media_diag_tb.cpp')
    subprocess.run([os.environ.get('VERILATOR', 'verilator'), '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps', str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'),
        str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'), "-GWITH_FLOPPY=1'b1", "-GWITH_RUNTIME_MEDIA=1'b1",
        '--top-module', 'media_diag_top', '--Mdir', str(build), '-I' + str(build),
        '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(name)) for name in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2', '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / p) for p in sources)], check=True)
    subprocess.run([str(build / 'Vmedia_diag_top'), str(build / 'diagnostic.bin')], cwd=build, check=True)

if __name__ == '__main__':
    main()
