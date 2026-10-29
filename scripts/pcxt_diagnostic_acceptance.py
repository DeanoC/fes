#!/usr/bin/env python3
"""Check PC/XT CGA diagnostic captures, including the final completion block.

The ROM intentionally halts with a static picture. Animation, keyboard input
and private debug opcodes are not acceptance criteria for this package.
ffmpeg/ffprobe decode captures; comparison uses only the Python standard library.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def expected_row(width, *, marker=False):
    row = bytearray(width)
    for x in range(width):
        native = x * 1280 // width
        sx = (native - 160) * 640 // 960
        if 160 <= native < 1120:
            ink = sx % 2 == 0 if sx % 16 < 8 else sx % 2 == 1
            row[x] = 255 if ink or (marker and sx < 16) else 0
    return row


def check_rgb(width, height, rgb, *, historical_pattern=False):
    if (width < 1280 or height < 720 or width * 9 != height * 16
            or len(rgb) != width * height * 3):
        raise ValueError('capture must be a complete 16:9 RGB frame of at least 1280x720')
    ordinary, complete = expected_row(width), expected_row(width, marker=True)
    # Capture hardware resamples 720p to 1080p. Account for edge interpolation,
    # but check every pixel and every row, not just brightness/frame equality.
    worst_row, total_error, colour_error = 0, 0, 0
    marker_sum = marker_count = 0
    for y in range(height):
        is_marker = y * 200 // height < 8
        expected = complete if is_marker and not historical_pattern else ordinary
        error = 0
        start = y * width * 3
        for x, wanted in enumerate(expected):
            offset = start + x * 3
            r, g, b = rgb[offset:offset + 3]
            value = (r + g + b) // 3
            error += abs(value - wanted)
            colour_error += max(r, g, b) - min(r, g, b)
            # Interior of the completion signature avoids resampling edges.
            if (2 <= y * 200 // height < 6 and
                    162 * width // 1280 <= x < 180 * width // 1280):
                marker_sum += value
                marker_count += 1
        total_error += error
        worst_row = max(worst_row, error / width)
    mean_error = total_error / (width * height)
    monochrome_error = colour_error / (width * height)
    marker_mean = marker_sum / marker_count
    if mean_error > 32 or worst_row > 40 or monochrome_error > 8:
        raise ValueError(f'wrong diagnostic geometry/pixels: mean error {mean_error:.3f}, '
                         f'worst row {worst_row:.3f}, colour error {monochrome_error:.3f}')
    if not historical_pattern and marker_mean < 235:
        raise ValueError(f'firmware completion block is absent: mean {marker_mean:.3f}')
    return {'pattern_matches': True, 'width': width, 'height': height,
            'mean_pixel_error': round(mean_error, 3), 'worst_row_error': round(worst_row, 3),
            'completion_signature_verified': not historical_pattern,
            'classification': ('historical diagnostic picture; final firmware completion unverified'
                               if historical_pattern else 'completed PC/XT hardware diagnostic picture'),
            'static_picture_expected': True}


def check_capture(path, *, historical_pattern=False):
    path = Path(path)
    info = json.loads(subprocess.check_output([
        'ffprobe', '-v', 'error', '-select_streams', 'v:0', '-show_entries',
        'stream=width,height', '-of', 'json', str(path)]))['streams']
    if len(info) != 1:
        raise ValueError('capture needs exactly one video stream')
    width, height = info[0]['width'], info[0]['height']
    if width > 3840 or height > 2160:
        raise ValueError('capture exceeds the diagnostic size bound')
    rgb = subprocess.check_output(['ffmpeg', '-v', 'error', '-i', str(path),
        '-frames:v', '1', '-threads', '1', '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-'])
    result = check_rgb(width, height, rgb, historical_pattern=historical_pattern)
    result['capture_sha256'] = hashlib.sha256(path.read_bytes()).hexdigest()
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('captures', nargs='+', type=Path)
    parser.add_argument('--historical-pattern', action='store_true',
                        help='inspect the older ROM picture; does not verify final completion')
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    try:
        result = {'captures': [check_capture(p, historical_pattern=args.historical_pattern)
                               for p in args.captures], 'whole_board_qualified': False}
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, str(error) + '\n')
    encoded = json.dumps(result, indent=2) + '\n'
    if args.output:
        args.output.write_text(encoded)
    print(encoded, end='')


if __name__ == '__main__':
    main()
