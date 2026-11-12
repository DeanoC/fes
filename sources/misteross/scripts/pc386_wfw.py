#!/usr/bin/env python3
"""Windows for Workgroups 3.11 screens, read from a frame, for the pc386
corpus checks (docs/pc386-windows-corpus.md): sim_fes_pc386.py --suite wfw
and pc386_wfw_kit_acceptance.py.

A frame is the 640x480 VGA capture machine_tb writes (a PPM), or an HDMI grab
of a kit at any size, whose 4:3 picture is cut out of its black borders and
sampled back to 640x480. The checks are structural, so they need no Windows
bytes or pictures in the repository. They assume the standard VGA driver and
the "Windows Default" colours that WfW's SETUP leaves:

  program_manager  Program Manager's window: a dark blue active title bar at
                   least 300 pixels wide, a white menu bar under it, and an
                   open group window's dark blue title bar inside it, on the
                   grey desktop unless the window is maximized. The WfW logo
                   screen (bright blue, black border), the grey desktop with
                   the hourglass, a dialog box and a text screen all fail.
  text_screen      a text-mode screen (the DOS prompt, or a full-screen MS-DOS
                   Prompt): mostly black, with no desktop grey or title-bar
                   blue.

Run on frames to see what the checks find:

  python3 scripts/pc386_wfw.py build/sim/fes-pc386/wfw-rtl/frames/t*.ppm
"""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import json
from pathlib import Path
import subprocess
import sys

WIDTH, HEIGHT = 640, 480


class FrameError(ValueError):
    pass


@dataclass(frozen=True)
class Frame:
    width: int
    height: int
    rgb: bytes

    def pixel(self, x: int, y: int) -> tuple[int, int, int]:
        at = (y * self.width + x) * 3
        return self.rgb[at], self.rgb[at + 1], self.rgb[at + 2]


def parse_ppm(data: bytes) -> Frame:
    """A binary (P6) PPM with a maxval of 255, as pc386::write_ppm writes it."""
    fields, at = [], 0
    while len(fields) < 4:
        while at < len(data) and data[at:at + 1].isspace():
            at += 1
        if data[at:at + 1] == b'#':
            at = data.index(b'\n', at) + 1
            continue
        end = at
        while end < len(data) and not data[end:end + 1].isspace():
            end += 1
        if end == at:
            raise FrameError('truncated PPM header')
        fields.append(data[at:end])
        at = end
    if fields[0] != b'P6' or fields[3] != b'255':
        raise FrameError('not a binary 8-bit PPM')
    width, height = int(fields[1]), int(fields[2])
    pixels = data[at + 1:at + 1 + width * height * 3]
    if len(pixels) != width * height * 3:
        raise FrameError(f'PPM has {len(pixels)} of {width * height * 3} pixel bytes (still being written?)')
    return Frame(width, height, pixels)


def load(path: Path) -> Frame:
    """A PPM directly; any other picture (a kit's PNG grab) through ffmpeg."""
    path = Path(path)
    data = path.read_bytes()
    if data[:2] == b'P6':
        return parse_ppm(data)
    probe = subprocess.run(['ffprobe', '-v', 'error', '-select_streams', 'v:0', '-show_entries',
                            'stream=width,height', '-of', 'csv=p=0', str(path)],
                           capture_output=True, text=True, check=False)
    try:
        width, height = (int(value) for value in probe.stdout.strip().split(','))
    except ValueError:
        raise FrameError(f'ffprobe cannot read {path}') from None
    raw = subprocess.run(['ffmpeg', '-loglevel', 'error', '-i', str(path), '-f', 'rawvideo', '-pix_fmt', 'rgb24',
                          '-frames:v', '1', '-'], capture_output=True, check=False).stdout
    if len(raw) != width * height * 3:
        raise FrameError(f'ffmpeg decoded {len(raw)} bytes from {path}, expected {width * height * 3}')
    return Frame(width, height, raw)


def black(pixel) -> bool:
    return max(pixel) <= 40


def navy(pixel) -> bool:
    """The active title bar: VGA dark blue (0,0,170), (0,0,161) through HDMI capture."""
    r, g, b = pixel
    return r < 60 and g < 60 and 120 <= b <= 200


def desktop(pixel) -> bool:
    """The default desktop and window-frame light grey (195,199,203), not
    the text-mode light grey (170,170,170)."""
    return 178 <= min(pixel) and max(pixel) <= 215 and max(pixel) - min(pixel) <= 20


def white(pixel) -> bool:
    return min(pixel) >= 235


def picture(frame: Frame) -> Frame:
    """The 640x480 picture: a capture as it is, or a kit grab's non-black
    area sampled back to 640x480."""
    if (frame.width, frame.height) == (WIDTH, HEIGHT):
        return frame
    step = max(1, min(frame.width, frame.height) // 270)
    xs, ys = [], []
    for y in range(0, frame.height, step):
        for x in range(0, frame.width, step):
            if sum(frame.pixel(x, y)) > 90:
                xs.append(x)
                ys.append(y)
    if not xs:
        return Frame(WIDTH, HEIGHT, bytes(WIDTH * HEIGHT * 3))
    x0, x1 = min(xs), min(frame.width - 1, max(xs) + step - 1)
    y0, y1 = min(ys), min(frame.height - 1, max(ys) + step - 1)
    # A 4:3 picture only: a dark edge of the picture must not shrink the box.
    width, height = x1 - x0 + 1, y1 - y0 + 1
    if width * 3 < height * 4:
        grow = (height * 4 // 3) - width
        x0 = max(0, x0 - grow // 2)
        width = min(frame.width - x0, height * 4 // 3)
    rows = []
    for y in range(HEIGHT):
        source_y = y0 + y * height // HEIGHT
        row = bytearray()
        for x in range(WIDTH):
            row += bytes(frame.pixel(x0 + x * width // WIDTH, source_y))
        rows.append(bytes(row))
    return Frame(WIDTH, HEIGHT, b''.join(rows))


def longest_run(frame: Frame, y: int, test, gap: int = 0) -> tuple[int, int]:
    """The longest run of pixels in row y passing test, as (x0, x1) inclusive,
    or (0, -1). Runs closer than gap failing pixels join, so a title bar's
    caption text does not split it."""
    best, start, last = (0, -1), None, None
    for x in range(frame.width + 1):
        inside = x < frame.width and test(frame.pixel(x, y))
        if inside:
            if start is None or x - last - 1 > gap:
                if start is not None and last - start > best[1] - best[0]:
                    best = (start, last)
                start = x
            last = x
    if start is not None and last - start > best[1] - best[0]:
        best = (start, last)
    return best


def caption_bands(frame: Frame, minimum: int = 120) -> list[dict]:
    """Title bars: runs of rows whose longest dark blue run (caption text
    bridged) is at least minimum pixels and starts and ends within 3 pixels
    of the band's first row."""
    bands, current = [], None
    for y in range(frame.height):
        x0, x1 = longest_run(frame, y, navy, gap=16)
        wide = x1 - x0 + 1 >= minimum
        if wide and current and abs(x0 - current['x0']) <= 3 and abs(x1 - current['x1']) <= 3:
            current['y1'] = y
            continue
        if current:
            bands.append(current)
            current = None
        if wide:
            current = {'x0': x0, 'x1': x1, 'y0': y, 'y1': y}
    if current:
        bands.append(current)
    return [band for band in bands if 10 <= band['y1'] - band['y0'] + 1 <= 30]


def fraction(frame: Frame, test, box=None) -> float:
    x0, y0, x1, y1 = box or (0, 0, frame.width - 1, frame.height - 1)
    total = hits = 0
    for y in range(y0, y1 + 1, 2):
        for x in range(x0, x1 + 1, 2):
            total += 1
            hits += test(frame.pixel(x, y))
    return hits / total if total else 0.0


def program_manager(frame: Frame) -> dict:
    """Whether the frame shows Program Manager with a group window open."""
    frame = picture(frame)
    bands = caption_bands(frame)
    result = {'found': False, 'caption': None, 'group': None, 'maximized': False, 'reason': ''}
    windows = [band for band in bands if band['x1'] - band['x0'] + 1 >= 300]
    if not windows:
        result['reason'] = f'no title bar 300 pixels wide ({len(bands)} narrower)'
        return result
    caption = windows[0]
    result['caption'] = caption
    inside = [band for band in bands if band['y0'] > caption['y1'] + 10
              and band['x0'] >= caption['x0'] - 2 and band['x1'] <= caption['x1'] + 2
              and band['x1'] - band['x0'] + 1 >= 150]
    if not inside:
        result['reason'] = 'no group window title bar inside the window'
        return result
    group = inside[0]
    result['group'] = group
    width = caption['x1'] - caption['x0'] + 1
    menu = [y for y in range(caption['y1'] + 1, group['y0'])
            if (lambda run: run[1] - run[0] + 1)(longest_run(frame, y, white)) >= width * 6 // 10]
    if len(menu) < 8:
        result['reason'] = f'no menu bar under the title bar ({len(menu)} white rows)'
        return result
    result['maximized'] = width >= 540  # maximized: the screen less the system box and two buttons
    if not result['maximized']:
        outside = fraction(frame, desktop, (0, frame.height - 60, frame.width - 1, frame.height - 1))
        if outside < 0.5:
            result['reason'] = f'no desktop below the window ({outside:.2f} grey)'
            return result
    result['found'] = True
    result['reason'] = 'Program Manager'
    return result


def text_screen(frame: Frame) -> dict:
    """Whether the frame is a text-mode screen: mostly black, no desktop or title bars."""
    black_share = fraction(frame, black)
    grey_share = fraction(frame, desktop)
    blue_share = fraction(frame, navy)
    found = black_share >= 0.6 and grey_share < 0.01 and blue_share < 0.01
    return {'found': found, 'black': round(black_share, 3), 'desktop': round(grey_share, 3),
            'navy': round(blue_share, 3)}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('frames', nargs='+', type=Path)
    args = parser.parse_args(argv)
    for path in args.frames:
        frame = load(path)
        print(path, json.dumps({'program_manager': program_manager(frame), 'text_screen': text_screen(frame)}))
    return 0


if __name__ == '__main__':
    sys.exit(main())
