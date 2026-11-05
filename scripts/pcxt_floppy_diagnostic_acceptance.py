#!/usr/bin/env python3
"""Check a fresh PCXT floppy guest diagnostic's four captured frames."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts.pcxt_diagnostic_acceptance import check_rgb


def inspect_rgb(width, height, rgb, *, marker, reference=None):
    if reference is not None and len(reference) != len(rgb):
        raise ValueError('baseline frame dimensions differ')
    result = check_rgb(width, height, rgb)
    # Native CGA x=64..127, y=0..31.  Skip the interpolated perimeter;
    # each remaining scanline and column must match, including the center.
    x0 = (160 * width // 1280) + (65 * 960 * width // (640 * 1280))
    x1 = (160 * width // 1280) + (127 * 960 * width // (640 * 1280))
    y0, y1 = height // 200, 31 * height // 200
    if x1 <= x0 or y1 <= y0:
        raise ValueError('invalid marker interior')
    rows, columns, bad, n = [], [0] * (x1 - x0), 0, 0
    brightness, dark = 0, 0
    for y in range(y0, y1):
        row = 0
        for x in range(x0, x1):
            idx = (y * width + x) * 3
            value = sum(rgb[idx:idx + 3]) / 3
            brightness += value
            dark += value < 128
            expected = 255 if marker else (sum(reference[idx:idx + 3]) / 3 if reference is not None else value)
            delta = abs(value - expected)
            row += delta
            columns[x - x0] += delta
            bad += delta > 80
            n += 1
        rows.append(row / (x1 - x0))
    columns = [e / (y1 - y0) for e in columns]
    mean = sum(rows) / len(rows)
    bad_fraction = bad / n
    # Brightness limits allow edge filtering and capture noise without
    # accepting a stripe, a missing scanline, or a partial-height/width block.
    if not marker and reference is None and (brightness / n > 170 or dark / n < 0.25):
        raise ValueError('baseline marker region does not contain the expected dark stripes')
    if (marker or reference is not None) and (mean > 32 or max(rows) > 55 or max(columns) > 55 or bad_fraction > 0.06):
        raise ValueError(f"{'matching' if marker else 'empty/wrong/ejected'} disk marker mismatch: "
                         f'mean={mean:.2f}, worst_row={max(rows):.2f}, '
                         f'worst_column={max(columns):.2f}, bad_fraction={bad_fraction:.4f}')
    result.update(marker_expected=marker, marker_roi=[x0, y0, x1, y1],
                  marker_mean_error=round(mean, 3),
                  marker_worst_row_error=round(max(rows), 3),
                  marker_worst_column_error=round(max(columns), 3),
                  marker_bad_fraction=round(bad_fraction, 5),
                  marker_mean_brightness=round(brightness / n, 3),
                  marker_dark_fraction=round(dark / n, 5),
                  compared_with_baseline=reference is not None)
    return result


def capture(path, marker, reference_path=None):
    path = Path(path)
    info = json.loads(subprocess.check_output(['ffprobe', '-v', 'error', '-select_streams',
        'v:0', '-show_entries', 'stream=width,height', '-of', 'json', str(path)]))['streams']
    if len(info) != 1:
        raise ValueError('capture must contain exactly one video stream')
    w, h = info[0]['width'], info[0]['height']
    if w > 3840 or h > 2160:
        raise ValueError('capture exceeds size bound')
    rgb = subprocess.check_output(['ffmpeg', '-v', 'error', '-i', str(path), '-frames:v', '1',
                                   '-threads', '1', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'])
    reference = None
    if reference_path is not None:
        reference = subprocess.check_output(['ffmpeg', '-v', 'error', '-i', str(reference_path), '-frames:v', '1',
            '-threads', '1', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'])
        if len(reference) != len(rgb):
            raise ValueError('baseline frame dimensions differ')
    result = inspect_rgb(w, h, rgb, marker=marker, reference=reference)
    result.update(path=str(path.resolve()), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
    return result


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for stage in ('baseline', 'wrong', 'matching', 'ejected'):
        p.add_argument('--' + stage, required=True, type=Path)
    p.add_argument('--output', required=True, type=Path)
    a = p.parse_args()
    try:
        result = {'baseline': capture(a.baseline, False), 'wrong': capture(a.wrong, False, a.baseline),
                  'matching': capture(a.matching, True), 'ejected': capture(a.ejected, False, a.baseline),
                  'classification': 'guest sector read diagnostic, not DOS boot or write acceptance'}
        a.output.write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(result, indent=2))
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        p.exit(1, str(exc) + '\n')

if __name__ == '__main__':
    main()
