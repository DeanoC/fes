#!/usr/bin/env python3
"""Boot the unmodified, pinned 8088 XT BIOS through the real SDRAM machine."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.request

import sim_fes_pcxt as sim

ROOT = sim.ROOT
LOCK = ROOT / 'cores/fes-pcxt/sim/bios.lock'


def firmware_window(rom):
    """Top-align a supported system ROM; unpopulated F0000 bytes read FF."""
    if len(rom) not in (8192, 16384, 32768, 65536):
        raise ValueError('XT BIOS must be 8, 16, 32 or 64 KiB')
    return bytes([255]) * (65536 - len(rom)) + rom


def checked_file(path, spec):
    data = path.read_bytes()
    if len(data) != spec['size'] or hashlib.sha256(data).hexdigest() != spec['sha256']:
        raise ValueError(f'BIOS differs from selected fixture: {path}')
    return data


def fixture_inputs(build, bios=None, font=None, download=False):
    spec = json.loads(LOCK.read_text())
    path = Path(bios) if bios else build / spec['bios']['filename']
    if not path.exists():
        if bios or not download:
            raise ValueError('BIOS missing: provide --bios or explicitly allow --download')
        with urllib.request.urlopen(spec['bios']['url'], timeout=60) as response:
            data = response.read(spec['bios']['size'] + 1)
        if len(data) != spec['bios']['size'] or hashlib.sha256(data).hexdigest() != spec['bios']['sha256']:
            raise ValueError('Downloaded BIOS differs from selected fixture')
        path.write_bytes(data)
    rom = checked_file(path, spec['bios'])
    image = firmware_window(rom)
    if font:
        glyphs = Path(font).read_bytes()
        if len(glyphs) != 4096:
            raise ValueError('Font must be normalized 256 characters x 16 bytes')
    else:
        # The stock BIOS includes the PC's lower 128 8x8 graphics characters.
        # Reuse those exact licensed glyphs for readable POST ASCII; upper
        # characters and rows 8..15 are blank. This is not a full CP437 font.
        glyphs = bytearray(4096)
        start = spec['ascii_font']['rom_offset']
        for char in range(128):
            glyphs[char * 16:char * 16 + 8] = rom[start + char * 8:start + char * 8 + 8]
    (build / 'firmware.bin').write_bytes(image)
    (build / 'font.hex').write_text(''.join(f'{byte:02x}\n' for byte in glyphs))
    receipt = {'bios': spec['bios'], 'mapping': 'top aligned in F0000-FFFFF; FF fill',
               'firmware_sha256': hashlib.sha256(image).hexdigest(),
               'font_sha256': hashlib.sha256(glyphs).hexdigest(),
               'classification': 'host simulation only'}
    (build / 'inputs.json').write_text(json.dumps(receipt, indent=2) + '\n')
    return spec


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--download', action='store_true', help='fetch the hash-pinned BIOS if absent')
    parser.add_argument('--bios', type=Path, help='local copy of the BIOS selected by bios.lock')
    parser.add_argument('--font', type=Path, help='external normalized 4096-byte CGA font')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    build = ROOT / 'build/sim/fes-pcxt/bios'
    build.mkdir(parents=True, exist_ok=True)
    fixture_inputs(build, args.bios, args.font, args.download)
    for name in sim.CHIP_VENDORS:
        sim.verify_vendor(name)
    sim.cpu_roms(build)
    sources = tuple(p for p in sim.SYSTEM_SOURCES if not p.endswith('/pcxt_boot.sv')) + (
        sim.MEMORY_SOURCES[-1], 'cores/fes-pcxt/sim/bios_top.sv', 'cores/fes-pcxt/sim/bios_tb.cpp')
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps',
        str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'), str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'),
        '--top-module', 'bios_top', '--Mdir', str(build), '-I' + str(build),
        '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(name)) for name in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2', '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / path) for path in sources)], check=True)
    subprocess.run([str(build / 'Vbios_top'), str(build / 'firmware.bin')], cwd=build, check=True)


if __name__ == '__main__':
    main()
