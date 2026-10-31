#!/usr/bin/env python3
"""Exercise the real PC/XT runtime mailbox and SDRAM media path."""
import os
from pathlib import Path
import subprocess
import sim_fes_pcxt as sim

ROOT = Path(__file__).resolve().parents[1]


def main():
    build = ROOT / 'build/sim/fes-pcxt/media-runtime'
    build.mkdir(parents=True, exist_ok=True)
    sources = (
        'cores/fes-common/rtl/fes_computer_mailbox.v',
        'cores/fes-ramtest/rtl/sdram_addon_port.v',
        'cores/fes-pcxt/rtl/pcxt_memory.sv',
        'cores/fes-pcxt/rtl/pcxt_disk_loader.sv',
        'cores/fes-pcxt/rtl/pcxt_media_writer.sv',
        'cores/fes-pcxt/rtl/pcxt_media.sv',
        'cores/fes-pcxt/sim/memory_top.sv',
        'cores/fes-pcxt/sim/media_runtime_top.sv',
        'cores/fes-pcxt/sim/media_runtime_tb.cpp',
    )
    subprocess.run([os.environ.get('VERILATOR', 'verilator'), '--cc', '--exe', '--build',
        '--top-module', 'media_runtime_top', '-Wall', '--Mdir', str(build),
        '-I' + str(ROOT / 'cores/fes-common/generated'), '-CFLAGS', '-std=c++17 -O2',
        *(str(ROOT / p) for p in sources)], check=True)
    subprocess.run([str(build / 'Vmedia_runtime_top')], cwd=build, check=True)
    # Exercise Release with a real assembled 8088, complete firmware window,
    # and no disk. This profile has no external BIOS or download dependency.
    empty = ROOT / 'build/sim/fes-pcxt/media-empty'
    empty.mkdir(parents=True, exist_ok=True)
    for name in sim.CHIP_VENDORS:
        sim.verify_vendor(name)
    sim.cpu_roms(empty)
    (empty / 'font.hex').write_text('00\n' * 4096)
    machine_sources = tuple(p for p in sim.SYSTEM_SOURCES if not p.endswith('/pcxt_boot.sv')) + (
        'cores/fes-common/rtl/fes_computer_mailbox.v', sim.MEMORY_SOURCES[-1],
        'cores/fes-pcxt/sim/bios_top.sv', 'cores/fes-pcxt/sim/media_empty_tb.cpp')
    subprocess.run([os.environ.get('VERILATOR', 'verilator'), '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps', str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'),
        str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'), "-GWITH_FLOPPY=1'b1", "-GWITH_RUNTIME_MEDIA=1'b1",
        '--top-module', 'bios_top', '--Mdir', str(empty), '-I' + str(empty),
        '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(name)) for name in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2', '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / p) for p in machine_sources)], check=True)
    subprocess.run([str(empty / 'Vbios_top')], cwd=empty, check=True)


if __name__ == '__main__':
    main()
