#!/usr/bin/env python3
"""Check the DOS profile's real boot ROM and embedded floppy SDRAM loader."""
import hashlib
import json
import os
from pathlib import Path
import subprocess

import sim_fes_pcxt as sim
from sim_fes_pcxt_bios import firmware_window

ROOT = sim.ROOT


def main():
    build = ROOT / 'build/sim/fes-pcxt/embedded-boot'
    build.mkdir(parents=True, exist_ok=True)
    for name in sim.CHIP_VENDORS:
        sim.verify_vendor(name)
    sim.cpu_roms(build)
    firmware = ROOT / 'cores/fes-pcxt/firmware'
    metadata = json.loads((firmware / 'hardware-dos.json').read_text())
    assets = {}
    for kind in ('bios', 'floppy'):
        descriptor = metadata[kind]
        data = (ROOT / 'cores/fes-pcxt' / descriptor['file']).read_bytes()
        if len(data) != descriptor['size'] or hashlib.sha256(data).hexdigest() != descriptor['sha256']:
            raise ValueError(f'{kind} does not match the hardware DOS input lock')
        assets[kind] = data
    sim.boot_roms(build, firmware_window(assets['bios']), prefix='pcxt_glabios')
    (build / 'pcxt_freedos_disk.hex').write_text(''.join(f'{b:02x}\n' for b in assets['floppy']))
    sources = sim.SYSTEM_SOURCES + (sim.MEMORY_SOURCES[-1], 'cores/fes-pcxt/rtl/pcxt_disk_boot.sv',
        'cores/fes-pcxt/sim/embedded_boot_top.sv', 'cores/fes-pcxt/sim/embedded_boot_tb.cpp')
    subprocess.run([os.environ.get('VERILATOR', 'verilator'), '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps', str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'),
        str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'), '--top-module', 'embedded_boot_top',
        '--Mdir', str(build), '-I' + str(build), '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(name)) for name in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2', '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / p) for p in sources)], check=True)
    subprocess.run([str(build / 'Vembedded_boot_top'),
                    str(firmware / Path(metadata['floppy']['file']).name)], cwd=build, check=True)


if __name__ == '__main__':
    main()
