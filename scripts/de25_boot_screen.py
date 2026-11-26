#!/usr/bin/env python3
"""Recognise a core's boot screen in a DE25 HIL capture.

`scripts/powerboat/hil-de25-core.sh` runs this on the two core frames of every
D25 package smoke and adds a `boot_screen` check when the core has one. Each
check recognises the screen a core shows after reset by its layout, not by a
stored image or OCR, so it holds for any position or scale of the machine's
picture:

- `fes.zx81`: white paper, empty but for the inverse K cursor at the bottom left.
- `fes.spectrum`: 48K grey paper with only the copyright line at the bottom.
- `fes.spectrum128`: the 128K menu, a cyan highlight bar under the rainbow title.
- `fes.c64`: the READY screen, a dark blue field in a light blue border with
  text only in its top rows.
- `fes.pcxt-dos`: the GLaBIOS POST, grey and coloured text at the top left of a
  black screen.
- `fes.atari-st`: the GEM desktop.

Cores without a check (Spectrum +2A/+3, Pentagon, pc386 and the ZX81 family's
other models) exit 3 and add no check. OCR cannot read the Atari ST desktop:
its menu text is large and pixelated and the desktop is a dithered fill.

The Atari ST check recognises a GEM desktop by its layout, not by a stored
image, so it holds for EmuTOS and TOS, any desktop colour, and any position or
scale of the ST picture: a large field of one mid-tone colour, a dark rule
along the top of that field, and a light menu strip above the rule with text
in it. Frames are reduced with ffmpeg's area filter first, which averages the
dither into a flat colour.

Usage: de25_boot_screen.py --core fes.atari-st FRAME [FRAME ...]
Prints one JSON object to stdout. Exit 0 when every frame matches, 1 when any
does not, 2 for a bad frame or usage, and 3 when the core has no boot-screen
check. It does not write the D25 smoke result files.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from collections import Counter

WIDTH, HEIGHT = 480, 270


def luma(p):
    return (299 * p[0] + 587 * p[1] + 114 * p[2]) // 1000


def decode(path: str, width: int = WIDTH, height: int = HEIGHT) -> bytes:
    """A frame as rgb24 at width x height, area-averaged."""
    raw = subprocess.run(
        ["ffmpeg", "-loglevel", "error", "-i", path, "-vf", f"scale={width}:{height}:flags=area",
         "-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "rgb24", "-"],
        capture_output=True, check=True, timeout=60).stdout
    if len(raw) != width * height * 3:
        raise ValueError(f"{path}: decoded {len(raw)} bytes, expected {width * height * 3}")
    return raw


def _pixels(raw: bytes, width: int, height: int):
    if len(raw) != width * height * 3:
        raise ValueError("frame size does not match its dimensions")
    return [[tuple(raw[(y * width + x) * 3:(y * width + x) * 3 + 3]) for x in range(width)]
            for y in range(height)]


def _span(values, threshold):
    """First and last index whose value reaches threshold, or None."""
    hits = [i for i, v in enumerate(values) if v >= threshold]
    return (hits[0], hits[-1]) if hits else None


def gem_desktop(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """Whether an rgb24 frame shows a GEM desktop, and why."""
    px = _pixels(raw, width, height)

    # The machine's picture: the rows and columns that are mostly not black.
    rows = _span([sum(luma(p) > 40 for p in row) / width for row in px], 0.25)
    cols = _span([sum(luma(px[y][x]) > 40 for y in range(height)) / height for x in range(width)], 0.25)
    if not rows or not cols:
        return False, "no picture"
    top, bottom = rows
    left, right = cols
    area = (bottom - top + 1) * (right - left + 1)
    if area < 0.08 * width * height:
        return False, f"picture too small ({area} cells)"

    # The desktop colour: the commonest mid-tone colour in the picture.
    def quantise(p):
        return tuple(c // 24 for c in p)
    counts = Counter(quantise(px[y][x]) for y in range(top, bottom + 1) for x in range(left, right + 1)
                     if 50 <= luma(px[y][x]) <= 225)
    if not counts:
        return False, "no mid-tone colour"
    key, _ = counts.most_common(1)[0]
    centre = tuple(c * 24 + 12 for c in key)

    def desk(p):
        return sum(abs(a - b) for a, b in zip(p, centre)) <= 60
    mask = [[desk(px[y][x]) for x in range(width)] for y in range(height)]
    drows = _span([sum(r[left:right + 1]) / (right - left + 1) for r in mask], 0.5)
    dcols = _span([sum(mask[y][x] for y in range(top, bottom + 1)) / (bottom - top + 1)
                   for x in range(width)], 0.3)
    if not drows or not dcols:
        return False, f"no desktop field of colour {centre}"
    dt, db = drows
    dl, dr = dcols
    dwidth, dheight = dr - dl + 1, db - dt + 1
    field = sum(mask[y][x] for y in range(dt, db + 1) for x in range(dl, dr + 1))
    if dwidth * dheight < 0.35 * area:
        return False, f"desktop field covers {dwidth * dheight}/{area} cells of the picture"
    if field < 0.75 * dwidth * dheight:
        return False, f"desktop field is {field}/{dwidth * dheight} desktop colour"

    # The menu bar's rule: a row across the field just above it, clearly
    # darker than the field. The rule is a few pixels thick, so after the
    # area filter it is a blend of black and its neighbours: judge it
    # against the field's own brightness rather than as black.
    limit = luma(centre) - 30

    def dark(y):
        return sum(luma(px[y][x]) < limit for x in range(dl, dr + 1)) / dwidth
    rule = next((y for y in range(dt - 1, max(top, dt - 4) - 1, -1) if dark(y) >= 0.8), None)
    if rule is None:
        return False, "no dark rule above the desktop field"

    # The menu strip: light, with text, above the rule.
    strip_height = max(3, dheight // 20)
    strip = [px[y][x] for y in range(max(top, rule - strip_height), rule) for x in range(dl, dr + 1)]
    if not strip:
        return False, "no menu strip above the rule"
    light = sum(luma(p) > 170 for p in strip) / len(strip)
    ink = sum(luma(p) < 140 for p in strip) / len(strip)
    if light < 0.5 or not 0.02 <= ink <= 0.45:
        return False, f"menu strip is {light:.2f} light with {ink:.2f} ink"
    return True, (f"GEM desktop: field {dwidth}x{dheight} colour {centre}, "
                  f"rule at row {rule}, menu strip {light:.2f} light {ink:.2f} ink")


def _picture(px, width, height):
    """The machine's picture: (top, bottom, left, right) of the mostly not-black area, or None."""
    rows = _span([sum(luma(p) > 40 for p in row) / width for row in px], 0.25)
    cols = _span([sum(luma(px[y][x]) > 40 for y in range(height)) / height for x in range(width)], 0.25)
    if not rows or not cols:
        return None
    return rows[0], rows[1], cols[0], cols[1]


def _cells(px, box):
    top, bottom, left, right = box
    return [(x, y, px[y][x]) for y in range(top, bottom + 1) for x in range(left, right + 1)]


def _light_picture(px, width, height, minimum):
    """The picture box and its cells when at least `minimum` of it is light paper."""
    box = _picture(px, width, height)
    if box is None:
        return None, None, "no picture"
    top, bottom, left, right = box
    # The same floor as the GEM desktop check: below it the picture is too
    # coarse for the layout tests, which are all relative to the picture.
    if (bottom - top + 1) * (right - left + 1) < 0.08 * width * height:
        return None, None, "picture too small"
    # Drop the picture's outer two cells: a scaled capture blends its edge
    # with the black around it, and that blend is not ink.
    box = (top + 2, bottom - 2, left + 2, right - 2)
    cells = _cells(px, box)
    light = sum(luma(p) > 180 for _, _, p in cells) / len(cells)
    if light < minimum:
        return None, None, f"paper is {light:.2f} light"
    return box, cells, ""


def _ink_band(cells, box, threshold=120):
    """The rows holding dark ink, and the ink's horizontal span, relative to the picture."""
    top, bottom, left, right = box
    ink = [(x, y) for x, y, p in cells if luma(p) < threshold]
    if not ink:
        return None
    ys = [y for _, y in ink]
    xs = [x for x, _ in ink]
    height, width = bottom - top + 1, right - left + 1
    return {"count": len(ink), "top": (min(ys) - top) / height, "bottom": (max(ys) - top) / height,
            "left": (min(xs) - left) / width, "right": (max(xs) - left) / width}


def zx81_k_cursor(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """ZX81 after reset: white paper, empty but for the inverse K cursor at the bottom left."""
    px = _pixels(raw, width, height)
    box, cells, why = _light_picture(px, width, height, 0.97)
    if box is None:
        return False, why
    ink = _ink_band(cells, box)
    if ink is None or ink["count"] < 4:
        return False, "no cursor"
    if ink["right"] - ink["left"] > 0.05 or ink["bottom"] - ink["top"] > 0.06:
        return False, "ink is more than one cursor cell"
    if ink["top"] < 0.7 or ink["right"] > 0.25:
        return False, f"cursor is not at the bottom left ({ink['left']:.2f}, {ink['top']:.2f})"
    return True, f"ZX81 K cursor at ({ink['left']:.2f}, {ink['top']:.2f}) of the picture"


def _copyright_line(cells, box) -> tuple[bool, str]:
    """A Spectrum copyright line: one line of dark ink near the bottom, across much of the picture."""
    top, bottom, left, right = box
    height = bottom - top + 1
    rows = sorted({y for x, y, p in cells if luma(p) < 120 and (y - top) / height >= 0.8})
    if not rows:
        return False, "no copyright line"
    if (rows[-1] - rows[0] + 1) / height > 0.06:
        return False, "bottom ink is taller than one line"
    xs = [x for x, y, p in cells if luma(p) < 120 and rows[0] <= y <= rows[-1]]
    span = (max(xs) - min(xs)) / (right - left + 1)
    if span < 0.4:
        return False, f"bottom line spans {span:.2f} of the picture"
    return True, f"copyright line spans {span:.2f}"


def spectrum_48k(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """48K Spectrum after reset: grey paper, black ink only in the copyright line at the bottom."""
    px = _pixels(raw, width, height)
    box, cells, why = _light_picture(px, width, height, 0.9)
    if box is None:
        return False, why
    colour = sum(max(p) - min(p) > 40 for _, _, p in cells) / len(cells)
    if colour > 0.002:
        return False, f"picture has colour ({colour:.3f})"
    ink = _ink_band(cells, box)
    if ink is None or ink["top"] < 0.8:
        return False, "ink above the copyright line"
    ok, detail = _copyright_line(cells, box)
    return ok, ("48K Spectrum: " + detail) if ok else detail


def spectrum_128k_menu(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """128K Spectrum boot menu: a cyan highlight bar under a title with the rainbow stripes."""
    px = _pixels(raw, width, height)
    box, cells, why = _light_picture(px, width, height, 0.6)
    if box is None:
        return False, why
    top, bottom, left, right = box
    pwidth, pheight = right - left + 1, bottom - top + 1

    def cyan(p):
        return p[1] > 150 and p[2] > 150 and p[0] < 110
    bar = [y for y in range(top, bottom + 1)
           if sum(cyan(px[y][x]) for x in range(left, right + 1)) >= 0.25 * pwidth]
    if not bar:
        return False, "no highlight bar"
    if (bar[-1] - bar[0] + 1) / pheight > 0.08 or not 0.2 <= (bar[0] - top) / pheight <= 0.55:
        return False, "highlight bar is the wrong size or place"
    hues = {
        "red": lambda p: p[0] > 150 and p[1] < 110 and p[2] < 110,
        "yellow": lambda p: p[0] > 150 and p[1] > 150 and p[2] < 110,
        "green": lambda p: p[1] > 150 and p[0] < 110 and p[2] < 110,
    }
    title = [px[y][x] for y in range(max(top, bar[0] - 8), bar[0]) for x in range(left, right + 1)]
    missing = [name for name, test in hues.items() if sum(map(test, title)) < 4]
    if missing:
        return False, "title has no " + ", ".join(missing) + " stripe"
    ok, detail = _copyright_line(cells, box)
    return ok, ("128K menu: " + detail) if ok else detail


def c64_basic_ready(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """C64 after reset: a dark blue field in a light blue border, text only in its top rows."""
    px = _pixels(raw, width, height)
    box = _picture(px, width, height)
    if box is None:
        return False, "no picture"
    top, bottom, left, right = box
    pwidth, pheight = right - left + 1, bottom - top + 1
    cells = _cells(px, box)

    def blue(p):
        return p[2] >= 120 and p[2] - max(p[0], p[1]) >= 50
    if sum(blue(p) for _, _, p in cells) / len(cells) < 0.85:
        return False, "picture is not blue"
    field = [(x, y) for x, y, p in cells if blue(p) and luma(p) < 70]
    if not field:
        return False, "no dark field"
    fl, fr = min(x for x, _ in field), max(x for x, _ in field)
    ft, fb = min(y for _, y in field), max(y for _, y in field)
    share = (fr - fl + 1) * (fb - ft + 1) / (pwidth * pheight)
    margins = ((fl - left) / pwidth, (right - fr) / pwidth, (ft - top) / pheight, (bottom - fb) / pheight)
    if not 0.45 <= share <= 0.9 or min(margins) < 0.03:
        return False, f"field covers {share:.2f} of the picture with margins {min(margins):.2f}"
    ink = [y for y in range(ft, fb + 1) if sum(luma(px[y][x]) >= 90 for x in range(fl, fr + 1)) >= 2]
    if not ink:
        return False, "no text in the field"
    depth = (ink[-1] - ft) / (fb - ft + 1)
    lines = 1 + sum(1 for a, b in zip(ink, ink[1:]) if b - a > 1)
    if depth > 0.3 or (ink[0] - ft) / (fb - ft + 1) > 0.1 or lines < 3:
        return False, f"text is {lines} lines down to {depth:.2f} of the field"
    return True, f"C64 READY screen: field {share:.2f} of the picture, {lines} text lines in the top {depth:.2f}"


def pcxt_glabios_post(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """PC/XT GLaBIOS POST: grey and coloured text at the top left of a black screen."""
    px = _pixels(raw, width, height)
    every = [(x, y, px[y][x]) for y in range(height) for x in range(width)]
    black = sum(luma(p) < 30 for _, _, p in every) / len(every)
    if black < 0.85:
        return False, f"screen is {black:.2f} black"
    text = [(x, y, p) for x, y, p in every if luma(p) >= 50]
    if len(text) < 0.01 * width * height:
        return False, "no text"
    if max(x for x, _, _ in text) > 0.65 * width or max(y for _, y, _ in text) > 0.6 * height:
        return False, "text outside the top-left POST area"

    def hue(p):
        r, g, b = p
        if g >= r + 40 and b >= r + 40:
            return "cyan"
        if r >= b + 60 and g >= b + 60:
            return "yellow"
        if g >= r + 40 and g >= b + 40:
            return "green"
        if max(p) - min(p) < 30:
            return "grey"
        return "other"
    counts = Counter(hue(p) for _, _, p in text)
    share = {name: counts[name] / len(text) for name in ("grey", "cyan", "green", "yellow")}
    low = [name for name, value in share.items() if value < 0.03]
    if low:
        return False, "POST text has too little " + ", ".join(low)
    return True, "GLaBIOS POST: " + ", ".join(f"{name} {value:.2f}" for name, value in share.items())


CHECKS = {
    "fes.atari-st": gem_desktop,
    "fes.zx81": zx81_k_cursor,
    "fes.spectrum": spectrum_48k,
    "fes.spectrum128": spectrum_128k_menu,
    "fes.c64": c64_basic_ready,
    "fes.pcxt-dos": pcxt_glabios_post,
}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--core", required=True)
    parser.add_argument("frames", nargs="+")
    args = parser.parse_args(argv)
    check = CHECKS.get(args.core)
    if check is None:
        print(json.dumps({"core": args.core, "checked": False}))
        return 3
    results = {}
    try:
        for frame in args.frames:
            ok, detail = check(decode(frame))
            results[frame] = {"ok": ok, "detail": detail}
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(json.dumps({"core": args.core, "checked": False, "error": str(error)[:300]}))
        return 2
    ok = all(r["ok"] for r in results.values())
    print(json.dumps({"core": args.core, "checked": True, "ok": ok, "frames": results}))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
