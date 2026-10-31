#!/usr/bin/env python3
"""Boot a real 8088 XT BIOS and read-only FreeDOS floppy in host simulation."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import subprocess

import sim_fes_pcxt as sim
import build_pcxt_freedos_image as freedos
import pcxt_bios as bios

ROOT = sim.ROOT


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--disk', type=Path, help='previously built exact FreeDOS fixture image')
    parser.add_argument('--bios', type=Path, help='local copy of the BIOS selected by bios.lock')
    parser.add_argument('--font', type=Path, help='external normalized 4096-byte CGA font')
    parser.add_argument('--download', action='store_true', help='fetch pinned BIOS, FreeDOS sources and assembler if absent')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    parent = ROOT / 'build/sim/fes-pcxt'
    parent.mkdir(parents=True, exist_ok=True)
    # A simulator and its frame checker share a build directory. Serialize the
    # entire run, including publication, so evidence cannot mix across runs.
    with (parent / '.dos.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        run(args)


def run(args):
    build = ROOT / 'build/sim/fes-pcxt/dos'
    build.mkdir(parents=True, exist_ok=True)
    for name in ('result.json', 'result-pending.json', 'dos.ppm', 'screen.bin', 'screen.txt',
                 'inputs.json', 'bus.log'):
        (build / name).unlink(missing_ok=True)
    bios.fixture_inputs(build, args.bios, args.font, args.download)
    # Validate the exact bytes supplied to the device against the pinned boot
    # code, geometry and all 8086 FreeDOS files, while allowing harmless
    # unallocated-sector differences between image-building tool versions.
    disk_path = args.disk.resolve() if args.disk else freedos.image(download=args.download)
    disk = disk_path.read_bytes()
    disk_identity = freedos.validate_selected_image(disk)
    (build / 'floppy.img').write_bytes(disk)
    inputs_path = build / 'inputs.json'
    inputs = json.loads(inputs_path.read_text())
    inputs['floppy_sha256'] = disk_identity['image_sha256']
    inputs['floppy_contents'] = disk_identity['files']
    inputs['floppy_bytes'] = len(disk)
    inputs_path.write_text(json.dumps(inputs, indent=2) + '\n')
    for name in sim.CHIP_VENDORS:
        sim.verify_vendor(name)
    sim.cpu_roms(build)
    sources = tuple(p for p in sim.SYSTEM_SOURCES if not p.endswith('/pcxt_boot.sv')) + (
        'cores/fes-common/rtl/fes_computer_mailbox.v',
        sim.MEMORY_SOURCES[-1], 'cores/fes-pcxt/sim/bios_top.sv', 'cores/fes-pcxt/sim/dos_tb.cpp')
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps',
        str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'), str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'),
        "-GWITH_FLOPPY=1'b1", "-GWITH_RUNTIME_MEDIA=1'b1", "-GWAIT_FOR_MEDIA=1'b1", '--top-module', 'bios_top', '--Mdir', str(build), '-I' + str(build),
        '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(name)) for name in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2', '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / path) for path in sources)], check=True)
    subprocess.run([str(build / 'Vbios_top'), str(build / 'firmware.bin'), str(build / 'floppy.img')],
                   cwd=build, check=True)
    ppm = (build / 'dos.ppm').read_bytes()
    header = b'P6\n1280 720\n255\n'
    if not ppm.startswith(header):
        raise ValueError('DOS image has invalid PPM header')
    font = bytes.fromhex((build / 'font.hex').read_text())
    cells = (build / 'screen.bin').read_bytes()
    rows = [cells[r*160:(r+1)*160:2] for r in range(25)]
    target_rows = [r for r, row in enumerate(rows) if b'FESXT' in row and b'TXT' in row]
    if len(target_rows) != 1:
        raise ValueError('DOS listing must contain exactly one FESXT.TXT row')
    proof = bios.verify_post_frame(ppm[len(header):], cells, font, b'FESXT', target_rows[0])
    pending = build / 'result-pending.json'
    result = json.loads(pending.read_text())
    result['frame_check'] = proof
    pending.write_text(json.dumps(result, indent=2) + '\n')
    pending.replace(build / 'result.json')
    print('PASS 720p DOS directory glyphs:', proof, flush=True)


if __name__ == '__main__':
    main()
