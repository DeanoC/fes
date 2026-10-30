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


def cga_colour(index):
    """IBM RGBI palette independently decoded from a text attribute."""
    bright = 85 if index & 8 else 0
    r = (170 if index & 4 else 0) + bright
    g = (170 if index & 2 else 0) + bright
    b = (170 if index & 1 else 0) + bright
    if index == 6:
        g = 85
    return bytes((r, g, b))


def verify_post_frame(frame, cells, font):
    """Compare POST glyph pixels, including foreground and background.

    The CGA source is 640x200, nearest-neighbour scaled into the centred
    960x720 viewport. Allow a small fixed capture alignment shift; never
    select different offsets for individual glyphs or pixels.
    """
    if len(frame) != 1280 * 720 * 3 or len(cells) != 80 * 25 * 2 or len(font) != 4096:
        raise ValueError('incorrect POST frame, text memory or font size')
    label = b'GLaBIOS'
    rows = [cells[r * 160:(r + 1) * 160:2] for r in range(25)]
    locations = [(r, row.find(label)) for r, row in enumerate(rows) if label in row]
    if len(locations) != 1:
        raise ValueError('POST text must contain one GLaBIOS banner')
    row, col = locations[0]
    expected = []
    for n, char in enumerate(label):
        attr = cells[2 * (row * 80 + col + n) + 1]
        foreground = cga_colour(attr & 15)
        background = cga_colour((attr >> 4) & 7)
        if foreground == background:
            raise ValueError('POST glyph foreground and background are identical')
        for gy in range(8):
            pattern = font[char * 16 + gy]
            for gx in range(8):
                sx, sy = (col + n) * 8 + gx, row * 8 + gy
                x = 160 + (sx * 960 + 480) // 640
                y = (sy * 720 + 360) // 200
                ink = bool(pattern & (0x80 >> gx))
                expected.append((x, y, foreground if ink else background, ink))
    inks = sum(ink for _, _, _, ink in expected)
    if inks < 16 or len(expected) - inks < 16:
        raise ValueError('POST font lacks distinguishable glyphs')
    best = None
    for dy in range(-3, 4):
        for dx in range(-4, 5):
            good_fg = good_bg = 0
            for x, y, colour, ink in expected:
                if not (0 <= x + dx < 1280 and 0 <= y + dy < 720):
                    continue
                offset = ((y + dy) * 1280 + x + dx) * 3
                if frame[offset:offset + 3] == colour:
                    if ink:
                        good_fg += 1
                    else:
                        good_bg += 1
            score = min(good_fg / inks, good_bg / (len(expected) - inks))
            if best is None or score > best['score']:
                best = {'score': score, 'dx': dx, 'dy': dy, 'foreground': good_fg,
                        'background': good_bg, 'label': label.decode('ascii')}
    if best['score'] < 0.90:
        raise ValueError('720p frame does not render the BIOS font and text: ' + str(best))
    return best


def check_outputs(build):
    ppm = (build / 'post.ppm').read_bytes()
    header = b'P6\n1280 720\n255\n'
    if not ppm.startswith(header):
        raise ValueError('POST image has invalid PPM header')
    font = bytes.fromhex((build / 'font.hex').read_text())
    proof = verify_post_frame(ppm[len(header):], (build / 'screen.bin').read_bytes(), font)
    result_path = build / 'result.json'
    result = json.loads(result_path.read_text())
    result['frame_check'] = proof
    result_path.write_text(json.dumps(result, indent=2) + '\n')
    print('PASS 720p BIOS glyphs:', proof, flush=True)


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
    check_outputs(build)


if __name__ == '__main__':
    main()
