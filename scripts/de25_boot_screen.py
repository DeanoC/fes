#!/usr/bin/env python3
"""Manually recognise a core's boot screen in a DE25 HIL capture.

The Powerboat D25 smoke runner calls this utility for known cores. It checks
whether supplied frames show the screen a core should boot to. OCR cannot read the Atari ST desktop: its menu
text is large and pixelated and the desktop is a dithered fill.

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


def ramtest_screen(raw: bytes, width: int = WIDTH, height: int = HEIGHT) -> tuple[bool, str]:
    """Check the exact managed SDRAM result and both untested-bank markers.

    Coordinates are in its 320x240 playfield, centred and scaled 3x in 720p.
    A median over each flat marker tolerates capture scaling and colour drift.
    A blank, stale, running, stopped or failed frame is never a pass.
    """
    px = _pixels(raw, width, height)

    def marker(x, y):
        cx = round((160 + 3*x) * width / 1280)
        cy = round(3*y * height / 720)
        samples = [px[yy][xx] for yy in range(cy-2, cy+3) for xx in range(cx-2, cx+3)]
        return tuple(sorted(p[c] for p in samples)[len(samples)//2] for c in range(3))

    def close(actual, expected, tolerance=60):
        return all(abs(a-b) <= tolerance for a, b in zip(actual, expected))

    samples = [marker(276, y) for y in (28, 36, 76, 116, 156)]
    identity = ((208, 32, 208), (32, 208, 208))
    if not all(close(samples[i], identity[i]) for i in range(2)):
        return False, f"diagnostic identity marker missing: {samples[:2]}"
    if not close(samples[3], (96, 96, 96)) or not close(samples[4], (96, 96, 96)):
        return False, f"LPDDR untested markers missing: {samples[3:]}"
    states = {"waiting": (32, 32, 224), "running": (224, 160, 0),
              "pass": (0, 224, 0), "fail": (224, 0, 0), "stopped": (224, 224, 224)}
    matches = [name for name, color in states.items() if close(samples[2], color)]
    if len(matches) != 1:
        return False, f"ambiguous SDRAM result: {samples[2]}"
    return matches[0] == "pass", f"SDRAM 128 MiB {matches[0]}; FPGA LPDDR and HPS LPDDR not tested"


def ramtest_transition(running_seen, stable, ok, detail):
    """Require a RUNNING observation before two consecutive passing frames."""
    if detail.startswith("SDRAM 128 MiB running;"):
        return True, 0
    return running_seen, stable + 1 if running_seen and ok else 0


def ramtest_session_bound(before, after, game, package):
    """Verify both observations name the same active core generation."""
    def identity(status):
        core = status.get("core_package") or {}
        return (status.get("game_id"), core.get("package_id"), core.get("generation"), core.get("build_id"))
    first, last = identity(before), identity(after)
    return (before.get("state") == after.get("state") == "active" and first == last and
            first[0] == game and first[1] == package and bool(first[2]) and bool(first[3]))


CHECKS = {"fes.atari-st": gem_desktop, "fes.ramtest": ramtest_screen}


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
