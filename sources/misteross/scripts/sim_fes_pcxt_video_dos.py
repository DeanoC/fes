#!/usr/bin/env python3
"""Boot GLaBIOS + FreeDOS in host simulation to verify MDA 8x14 and Hercules 720x348 video."""
from __future__ import annotations

import argparse
import fcntl
import json
import os
from pathlib import Path
import subprocess

import pcxt_bios as bios
import pcxt_fat12 as fat12
import pcxt_video_tests_asm as vasm
import sim_fes_pcxt as sim

ROOT = sim.ROOT
DEFAULT_BIOS = ROOT / 'cores/fes-pcxt/firmware/GLABIOS_0.4.2_8X.ROM'

TINT_CHOICES = {'white': 0, 'green': 1, 'amber': 2}
TINT_PALETTE = {
    0: (bytes((0, 0, 0)), bytes((170, 170, 170)), bytes((255, 255, 255))),  # White
    1: (bytes((0, 0, 0)), bytes((0, 170, 0)), bytes((85, 255, 85))),        # Green P39
    2: (bytes((0, 0, 0)), bytes((204, 119, 0)), bytes((255, 187, 51))),     # Amber P3
}

VIDEO_DOS_SOURCES = (
    tuple(p for p in sim.SYSTEM_SOURCES if not p.endswith('/pcxt_boot.sv')) +
    (
        'cores/fes-common/rtl/fes_computer_mailbox.v',
        'cores/fes-pcxt/rtl/pcxt_hid_keyboard.sv',
        'cores/fes-pcxt/rtl/pcxt_xt_keyboard_bat.sv',
        sim.MEMORY_SOURCES[-1],
        'cores/fes-pcxt/sim/video_dos_top.sv',
        'cores/fes-pcxt/sim/video_dos_tb.cpp',
    )
)


def resolve_bios(bios_arg: Path | None, download: bool) -> Path | None:
    """Prefer an explicit BIOS, then the checked-in GLaBIOS ROM, else --download."""
    if bios_arg is not None:
        return bios_arg
    if DEFAULT_BIOS.is_file():
        return DEFAULT_BIOS
    if download:
        return None
    raise FileNotFoundError(f'checked-in BIOS missing at {DEFAULT_BIOS}; pass --bios or --download')


def prepare_video_dos_sim(build: Path, *, verilator: str = 'verilator',
                          bios_path: Path | None = None, download: bool = False) -> Path:
    """Verify ROMs/fonts/vendor RTL and build Vvideo_dos_top in build/."""
    build.mkdir(parents=True, exist_ok=True)
    selected_bios = resolve_bios(bios_path, download)
    bios.fixture_inputs(build, selected_bios, None, download=download)
    cga_glyphs = sim.cga_font()
    mda_glyphs = sim.mda_font()
    (build / 'cga_font.hex').write_text(''.join(f'{b:02x}\n' for b in cga_glyphs))
    (build / 'mda_font.hex').write_text(''.join(f'{b:02x}\n' for b in mda_glyphs))
    for vendor in sim.CHIP_VENDORS:
        sim.verify_vendor(vendor)
    sim.cpu_roms(build)

    binary = build / 'Vvideo_dos_top'
    subprocess.run([
        verilator, '--cc', '--exe', '--build', '-j', '4', '-Wall',
        '--timescale-override', '1ns/1ps',
        str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt'),
        str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt'),
        '--top-module', 'video_dos_top',
        '--Mdir', str(build),
        '-I' + str(build),
        '-I' + str(ROOT / 'cores/fes-common/generated'),
        *('-I' + str(sim.vendor_dir(vendor)) for vendor in sim.CHIP_VENDORS),
        '-CFLAGS', '-std=c++17 -O2',
        '-I' + str(ROOT / 'cores/fes-pcxt/sim'),
        *(str(ROOT / p) for p in VIDEO_DOS_SOURCES),
    ], check=True)
    return binary


def read_ppm_rgb(path: Path) -> tuple[int, int, bytes]:
    """Read a binary P6 PPM and check it is 1280x720 RGB888."""
    raw = path.read_bytes()
    header = b'P6\n1280 720\n255\n'
    if not raw.startswith(header) or len(raw) != len(header) + 1280 * 720 * 3:
        raise ValueError(f'invalid 1280x720 P6 PPM: {path}')
    return 1280, 720, raw[len(header):]


def _check_pillarbox_black(frame: bytes) -> None:
    """Ensure the 160-px 720p pillarbox bars on both sides are solid black."""
    black = b'\x00\x00\x00'
    for y in range(0, 720, 15):
        for x in (10, 80, 150, 1130, 1200, 1270):
            off = (y * 1280 + x) * 3
            if frame[off:off + 3] != black:
                raise ValueError(f'pillarbox margin not black at ({x}, {y}): {frame[off:off + 3]!r}')


def _mda_attr_colors(attr: int, tint: int) -> tuple[bytes, bytes]:
    """Decode MDA text foreground/background RGB for a given monochrome tint."""
    off_col, norm_col, bright_col = TINT_PALETTE[tint]
    if (attr & 0x77) == 0x70:
        return off_col, norm_col
    if (attr & 0x77) == 0x00:
        return off_col, off_col
    fg = bright_col if (attr & 0x08) else norm_col
    return fg, off_col


def verify_mda_text_frame(frame: bytes, cells: bytes, mda_font: bytes,
                          label: bytes = b'FES MDA PASS', tint: int = 0) -> dict[str, object]:
    """Verify 80x25 MDA text cells rendered at 720x350 -> 960x720 match the 8x14 font."""
    if len(frame) != 1280 * 720 * 3 or len(cells) != 80 * 25 * 2 or len(mda_font) != 4096:
        raise ValueError('incorrect MDA frame, text memory or 8x14 font size')
    if tint not in TINT_PALETTE:
        raise ValueError(f'unsupported mono tint: {tint}')
    rows = [cells[r * 160:(r + 1) * 160:2] for r in range(25)]
    locations = [(r, row.find(label)) for r, row in enumerate(rows) if label in row]
    if len(locations) != 1:
        raise ValueError(f'MDA screen must contain {label!r} on exactly one row, got {locations}')
    row, col = locations[0]

    expected: list[tuple[int, int, bytes, bool]] = []
    for n, ch in enumerate(label):
        attr = cells[2 * (row * 80 + col + n) + 1]
        fg, bg = _mda_attr_colors(attr, tint)
        if fg == bg:
            raise ValueError('expected MDA banner glyph has identical fg and bg')
        for gy in range(14):
            pattern = mda_font[ch * 16 + gy]
            for gx in range(9):
                sx = (col + n) * 9 + gx
                sy = row * 14 + gy
                x = 160 + (sx * 960 + 480) // 720
                y = (sy * 720 + 360) // 350
                if gx < 8:
                    ink = bool(pattern & (0x80 >> gx))
                else:
                    ink = (0xc0 <= ch <= 0xdf) and bool(pattern & 0x01)
                expected.append((x, y, fg if ink else bg, ink))

    inks = sum(1 for _, _, _, ink in expected if ink)
    bgs = len(expected) - inks
    if inks < 16 or bgs < 16:
        raise ValueError('MDA banner glyphs lack distinguishable pixels')

    best: dict[str, object] | None = None
    for dy in range(-3, 4):
        for dx in range(-4, 5):
            good_fg = good_bg = 0
            for x, y, colour, ink in expected:
                nx, ny = x + dx, y + dy
                if not (0 <= nx < 1280 and 0 <= ny < 720):
                    continue
                off = (ny * 1280 + nx) * 3
                if frame[off:off + 3] == colour:
                    if ink:
                        good_fg += 1
                    else:
                        good_bg += 1
            score = min(good_fg / inks, good_bg / bgs)
            if best is None or score > float(best['score']):
                best = {
                    'score': round(score, 4),
                    'dx': dx,
                    'dy': dy,
                    'foreground': good_fg,
                    'background': good_bg,
                    'label': label.decode('ascii', errors='replace'),
                }
    assert best is not None
    if float(best['score']) < 0.90:
        raise ValueError(f'720p frame does not render the 8x14 MDA font and text: {best}')
    _check_pillarbox_black(frame)
    return best


def verify_mda_attributes(frame: bytes, mda_font: bytes, tint: int = 0,
                          dx: int = 0, dy: int = 0) -> dict[str, object]:
    """Verify MDATEST.COM attribute rows (bright, underline, reverse, hidden, 9th-dot)."""
    if len(frame) != 1280 * 720 * 3 or len(mda_font) != 4096:
        raise ValueError('invalid MDA frame or font buffer size')
    off_col, norm_col, bright_col = TINT_PALETTE[tint]

    def sample(sx: int, sy: int) -> bytes:
        x = min(1279, max(0, 160 + (sx * 960 + 480) // 720 + dx))
        y = min(719, max(0, (sy * 720 + 360) // 350 + dy))
        p = (y * 1280 + x) * 3
        return frame[p:p + 3]

    # Row 2: 'BRIGHT NORMAL 0F' (attr 0x0f) -> must contain bright_col pixels, not norm_col
    bright_hits = 0
    for col, ch in enumerate(b'BRIGHT NORMAL 0F'):
        for gy in range(14):
            pat = mda_font[ch * 16 + gy]
            for gx in range(8):
                if pat & (0x80 >> gx):
                    if sample(col * 9 + gx, 2 * 14 + gy) == bright_col:
                        bright_hits += 1
    if bright_hits < 50:
        raise ValueError(f'MDA row 2 bright (0Fh) attribute failed: hits={bright_hits}')

    # Row 3: 'UNDERLINE ATTR 01' (attr 0x01) -> scanline 13 solid underline in norm_col
    ul_hits = sum(1 for sx in range(17 * 9) if sample(sx, 3 * 14 + 13) == norm_col)
    if ul_hits < 120:
        raise ValueError(f'MDA row 3 underline (01h) scanline 13 failed: hits={ul_hits}/{17 * 9}')

    # Row 5: 'REVERSE VIDEO 70' (attr 0x70) -> space char col 7 has background = norm_col
    rev_hits = sum(
        1 for gy in range(14) for gx in range(9)
        if sample(7 * 9 + gx, 5 * 14 + gy) == norm_col
    )
    if rev_hits < 100:
        raise ValueError(f'MDA row 5 reverse (70h) attribute failed: hits={rev_hits}/126')

    # Row 6: 'HIDDEN' (attr 0x00) -> entire 6-cell band black
    hidden_lit = sum(
        1 for gy in range(14) for sx in range(6 * 9)
        if sample(sx, 6 * 14 + gy) != off_col
    )
    if hidden_lit > 0:
        raise ValueError(f'MDA row 6 hidden (00h) attribute leaked {hidden_lit} lit pixels')

    # Row 7: CP437 0xCD * 10 at cols 1..10 -> 9th dot (gx == 8) continues horizontal bars!
    cd_bars = [gy for gy in range(14) if (mda_font[0xcd * 16 + gy] & 0x01)]
    if not cd_bars:
        raise ValueError('8x14 font 0xCD glyph missing LSB horizontal bar')
    dot9_hits = sum(
        1 for col in range(1, 11) for gy in cd_bars
        if sample(col * 9 + 8, 7 * 14 + gy) == norm_col
    )
    if dot9_hits < len(cd_bars) * 8:
        raise ValueError(f'MDA row 7 9th-dot continuation failed: hits={dot9_hits}/{len(cd_bars) * 10}')

    return {
        'bright_hits': bright_hits,
        'underline_hits': ul_hits,
        'reverse_hits': rev_hits,
        'hidden_lit': hidden_lit,
        'ninth_dot_hits': dot9_hits,
    }


def verify_hgc_page_frame(frame: bytes, page: int, tint: int = 0) -> dict[str, object]:
    """Verify HGCTEST.COM Page 0 or Page 1 720x348 Hercules graphics capture."""
    if len(frame) != 1280 * 720 * 3:
        raise ValueError('invalid HGC 720p frame size')
    if page not in (0, 1):
        raise ValueError(f'page must be 0 or 1, got {page}')
    off_col, norm_col, _ = TINT_PALETTE[tint]
    _check_pillarbox_black(frame)

    def sample(sx: int, sy: int) -> bytes:
        x = min(1279, max(0, 160 + (sx * 960 + 480) // 720))
        y = min(719, max(0, (sy * 720 + 360) // 348))
        p = (y * 1280 + x) * 3
        return frame[p:p + 3]

    def ratio_lit(xs: range, ys: range) -> float:
        total = len(xs) * len(ys)
        lit = sum(1 for sy in ys for sx in xs if sample(sx, sy) == norm_col)
        return lit / total

    top_ratio = ratio_lit(range(16, 704, 4), range(0, 1))
    bot_ratio = ratio_lit(range(16, 704, 4), range(347, 348))
    p0_ratio = ratio_lit(range(72, 184, 4), range(48, 132, 4))
    p1_ratio = ratio_lit(range(456, 568, 4), range(208, 292, 4))

    if top_ratio < 0.90 or bot_ratio < 0.90:
        raise ValueError(f'HGC page {page} border missing: top={top_ratio:.2f} bot={bot_ratio:.2f}')

    if page == 0:
        if p0_ratio < 0.90 or p1_ratio > 0.05:
            raise ValueError(f'HGC Page 0 block check failed: p0={p0_ratio:.2f} p1={p1_ratio:.2f}')
        bank_ratios = [
            round(ratio_lit(range(0, 720), range(sy, sy + 1)), 3)
            for sy in (1, 2, 3)
        ]
        for idx, r in enumerate(bank_ratios, start=1):
            if not (0.35 <= r <= 0.65):
                raise ValueError(f'HGC Page 0 bank {idx} interleave line ratio={r} out of [0.35, 0.65]')
    else:
        if p1_ratio < 0.90 or p0_ratio > 0.05:
            raise ValueError(f'HGC Page 1 block check failed: p0={p0_ratio:.2f} p1={p1_ratio:.2f}')
        bank_ratios = []

    return {
        'page': page,
        'top_border': round(top_ratio, 3),
        'bottom_border': round(bot_ratio, 3),
        'page0_block': round(p0_ratio, 3),
        'page1_block': round(p1_ratio, 3),
        'bank_ratios': bank_ratios,
    }


def build_test_floppy(com_name: str, com_bytes: bytes) -> bytes:
    """Create a bootable 360 KiB FreeDOS floppy running com_name cleanly via AUTOEXEC.BAT."""
    base = fat12.checked_base(ROOT)
    bat = f'@ECHO OFF\r\nPROMPT $P$G\r\n{com_name}\r\n'.encode('ascii')
    return fat12.inject_files(base, {
        'AUTOEXEC.BAT': bat,
        com_name: com_bytes,
    })


def run(args: argparse.Namespace) -> dict[str, object]:
    build = ROOT / 'build/sim/fes-pcxt/video-dos'
    build.mkdir(parents=True, exist_ok=True)
    tint = TINT_CHOICES[args.tint]
    out_dir = Path(args.out).resolve() if args.out else build / 'artifacts'
    out_dir.mkdir(parents=True, exist_ok=True)

    binary = prepare_video_dos_sim(
        build,
        verilator=args.verilator,
        bios_path=args.bios,
        download=args.download,
    )
    if args.prepare_only:
        print('Inputs and Vvideo_dos_top compilation complete; no simulation run.', flush=True)
        return {'prepared': True, 'binary': str(binary)}

    mda_img = build / 'mda.img'
    hgc_img = build / 'hgc.img'
    mda_img.write_bytes(build_test_floppy('MDATEST.COM', vasm.build_mdatest_com()))
    hgc_img.write_bytes(build_test_floppy('HGCTEST.COM', vasm.build_hgctest_com()))

    mda_out = out_dir / 'mda'
    hgc_out = out_dir / 'hgc'
    mda_out.mkdir(parents=True, exist_ok=True)
    hgc_out.mkdir(parents=True, exist_ok=True)

    mda_cmd = [str(binary), str(build / 'firmware.bin'), str(mda_img), '1', str(tint), 'test-mda', str(mda_out), '45']
    hgc_cmd = [str(binary), str(build / 'firmware.bin'), str(hgc_img), '2', str(tint), 'test-hgc', str(hgc_out), '60']
    procs = [
        (mda_cmd, subprocess.Popen(mda_cmd, cwd=build)),
        (hgc_cmd, subprocess.Popen(hgc_cmd, cwd=build)),
    ]
    try:
        for cmd, proc in procs:
            rc = proc.wait()
            if rc != 0:
                raise subprocess.CalledProcessError(rc, cmd)
    finally:
        for _, proc in procs:
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill()

    mda_font = sim.mda_font()
    _, _, mda_rgb = fat12.ppm_to_png(mda_out / 'mda-pass.ppm', mda_out / 'mda-pass.png')
    mda_cells = (mda_out / 'screen.bin').read_bytes()
    mda_text_check = verify_mda_text_frame(mda_rgb, mda_cells, mda_font, b'FES MDA PASS', tint=tint)
    mda_attr_check = verify_mda_attributes(
        mda_rgb, mda_font, tint=tint,
        dx=int(mda_text_check['dx']), dy=int(mda_text_check['dy']),
    )

    _, _, p0_rgb = fat12.ppm_to_png(hgc_out / 'hgc-page0.ppm', hgc_out / 'hgc-page0.png')
    _, _, p1_rgb = fat12.ppm_to_png(hgc_out / 'hgc-page1.ppm', hgc_out / 'hgc-page1.png')
    _, _, hgc_text_rgb = fat12.ppm_to_png(hgc_out / 'hgc-text.ppm', hgc_out / 'hgc-text.png')
    hgc_p0_check = verify_hgc_page_frame(p0_rgb, page=0, tint=tint)
    hgc_p1_check = verify_hgc_page_frame(p1_rgb, page=1, tint=tint)
    hgc_cells = (hgc_out / 'screen.bin').read_bytes()
    hgc_text_check = verify_mda_text_frame(hgc_text_rgb, hgc_cells, mda_font, b'FES HGC PASS', tint=tint)

    summary = {
        'tint': args.tint,
        'mda': {'text': mda_text_check, 'attributes': mda_attr_check, 'png': str(mda_out / 'mda-pass.png')},
        'hgc': {
            'page0': hgc_p0_check,
            'page1': hgc_p1_check,
            'text': hgc_text_check,
            'pngs': [str(hgc_out / n) for n in ('hgc-page0.png', 'hgc-page1.png', 'hgc-text.png')],
        },
    }
    (out_dir / 'result.json').write_text(json.dumps(summary, indent=2) + '\n')
    print('PASS sim-fes-pcxt-video:', json.dumps(summary), flush=True)
    return summary


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--tint', choices=tuple(TINT_CHOICES), default='white')
    parser.add_argument('--out', type=Path, help='output directory for PNG screenshots and result.json')
    parser.add_argument('--bios', type=Path, help='optional path to GLABIOS_0.4.2_8X.ROM')
    parser.add_argument('--download', action='store_true', help='download BIOS if checked-in ROM absent')
    parser.add_argument('--prepare-only', action='store_true', help='compile Vvideo_dos_top without running')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    parent = ROOT / 'build/sim/fes-pcxt'
    parent.mkdir(parents=True, exist_ok=True)
    with (parent / '.video-dos.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        run(args)


if __name__ == '__main__':
    main()
