#!/usr/bin/env python3
"""Check changing PC/XT DOS boot captures and the final FreeDOS A:\\> prompt.

Supply at least three complete HDMI captures taken from launch through boot.
The check requires a substantial inter-frame change and matches the prompt's
GLaBIOS-derived CGA glyphs in the final framebuffer. It uses ffmpeg/ffprobe and
the Python standard library only.
"""
import argparse
from collections import Counter
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / 'sources/misteross/cores/fes-pcxt'
PROMPT = b'A:\\>'


def _median_color(samples):
    """Use the weighted median in each channel for a sampled color class."""
    total = sum(samples.values())
    if not total:
        raise ValueError('empty color class')
    result = []
    for channel in range(3):
        levels = Counter()
        for color, count in samples.items():
            levels[color[channel]] += count
        cumulative = 0
        for level, count in sorted(levels.items()):
            cumulative += count
            if cumulative > total // 2:
                result.append(level)
                break
    return tuple(result)


def load_font(root=ROOT):
    """Extract the exact 8x8 ASCII glyphs embedded in the locked BIOS ROM."""
    core = Path(root) / 'sources/misteross/cores/fes-pcxt'
    assets = json.loads((core / 'firmware/hardware-dos.json').read_text())
    spec = json.loads((core / 'sim/bios.lock').read_text())
    bios = assets['bios']
    path = core / bios['file']
    data = path.read_bytes()
    if len(data) != bios['size'] or hashlib.sha256(data).hexdigest() != bios['sha256']:
        raise ValueError('GLaBIOS font source differs from the package lock')
    if spec['bios']['sha256'] != bios['sha256']:
        raise ValueError('GLaBIOS ROM differs from the BIOS simulation lock')
    font = bytearray(4096)
    offset = spec['ascii_font']['rom_offset']
    for code in range(128):
        font[code * 16:code * 16 + 8] = data[offset + code * 8:offset + code * 8 + 8]
    return bytes(font)


def _pixel(rgb, width, x, y, dx=0, dy=0):
    x, y = x + dx, y + dy
    offset = (y * width + x) * 3
    return tuple(rgb[offset:offset + 3])


def find_prompt(width, height, rgb, font):
    """Return a strong 4-glyph CGA match for A:\\> anywhere in the 80x25 text grid."""
    if len(font) != 4096 or len(rgb) != width * height * 3:
        raise ValueError('invalid framebuffer or CGA font size')
    if width < 1280 or height < 720 or width * 9 != height * 16:
        raise ValueError('capture must be a complete 16:9 frame of at least 1280x720')
    glyphs = [font[character * 16:character * 16 + 8] for character in PROMPT]
    printable = {code: int.from_bytes(font[code * 16:code * 16 + 8], 'big')
                 for code in range(32, 127)}
    offsets = [(0, 0)] + [(dx, dy) for dy in range(-1, 2) for dx in range(-1, 2)
                         if (dx, dy) != (0, 0)]
    if len(set(rgb)) < 2:
        raise ValueError('final framebuffer does not contain the FreeDOS A:\\> CGA glyph sequence')
    best = None
    # The CGA image is 640x200, centered in the core's 960x720 viewport.
    for dx, dy in offsets:
        for row in range(25):
            for col in range(80 - len(PROMPT) + 1):
                foreground, background = Counter(), Counter()
                ink_count = blank_count = 0
                samples = [[] for _ in glyphs]
                for index, glyph in enumerate(glyphs):
                    cell_x = col + index
                    for gy in range(8):
                        bits = glyph[gy]
                        for gx in range(8):
                            sx = cell_x * 8 + gx
                            sy = row * 8 + gy
                            x = round((160 + (sx + 0.5) * 1.5) * width / 1280)
                            y = round((sy + 0.5) * height / 200)
                            if not (0 <= x + dx < width and 0 <= y + dy < height):
                                continue
                            value = _pixel(rgb, width, x, y, dx, dy)
                            samples[index].append((value, bool(bits & (0x80 >> gx))))
                            if bits & (0x80 >> gx):
                                foreground[value] += 1
                                ink_count += 1
                            else:
                                background[value] += 1
                                blank_count += 1
                if not ink_count or not blank_count:
                    continue
                bg = background.most_common(1)[0][0]
                # If no sampled ink channel is far enough from the background,
                # its median cannot meet the existing contrast gate either.
                if not any(max(abs(a - b) for a, b in zip(color, bg)) >= 64
                           for color in foreground):
                    continue
                # Scaling and capture color conversion can make the most
                # common ink shade unusually bright. A weighted median keeps
                # the representative color within the central ink range,
                # without changing the proximity or glyph-shape thresholds.
                fg = _median_color(foreground)
                # HDMI capture can quantize adjacent pixels of one flat color
                # differently. Compare each class with its representative RGB,
                # while still requiring a strong color separation and the
                # original high spatial agreement with all four ROM glyphs.
                def near(color, reference):
                    return max(abs(a - b) for a, b in zip(color, reference)) <= 40
                fg_hits = sum(count for color, count in foreground.items() if near(color, fg))
                bg_hits = sum(count for color, count in background.items() if near(color, bg))
                contrast = max(abs(a - b) for a, b in zip(fg, bg))
                score = (fg_hits + bg_hits) / (ink_count + blank_count)
                if contrast >= 64 and fg_hits / ink_count >= 0.92 and bg_hits / blank_count >= 0.94:
                    # Enforce agreement on each character as well as across
                    # the line. Then require the expected locked-ROM glyph
                    # to be strictly closer than every other printable glyph.
                    # The colon and semicolon (and > and )) differ by only
                    # two pixels, which aggregate scores can easily hide.
                    matched = True
                    for index, sample in enumerate(samples):
                        ink = [color for color, bit in sample if bit]
                        blank = [color for color, bit in sample if not bit]
                        if (len(sample) != 64 or not ink or not blank
                                or sum(near(color, fg) for color in ink) / len(ink) < 0.92
                                or sum(near(color, bg) for color in blank) / len(blank) < 0.94):
                            matched = False
                            break
                        observed = 0
                        for color, _ in sample:
                            fg_distance = sum((a - b) ** 2 for a, b in zip(color, fg))
                            bg_distance = sum((a - b) ** 2 for a, b in zip(color, bg))
                            observed = (observed << 1) | (fg_distance < bg_distance)
                        expected = PROMPT[index]
                        distance = (observed ^ printable[expected]).bit_count()
                        if any((observed ^ pattern).bit_count() <= distance
                               for code, pattern in printable.items() if code != expected):
                            matched = False
                            break
                    if not matched:
                        continue
                    proof = {'row': row, 'column': col, 'score': round(score, 4),
                             'foreground_rgb': list(fg), 'background_rgb': list(bg),
                             'alignment_dx': dx, 'alignment_dy': dy}
                    if best is None or score > best['score']:
                        best = {**proof, 'score': score}
                        if score >= 0.99:
                            best['score'] = round(score, 4)
                            best['text'] = PROMPT.decode('ascii')
                            return best
    if best is None:
        raise ValueError('final framebuffer does not contain the FreeDOS A:\\> CGA glyph sequence')
    best['score'] = round(best['score'], 4)
    best['text'] = PROMPT.decode('ascii')
    return best


def measure_frame_change(first, second, width, height):
    """Measure a meaningful screen change, ignoring isolated capture noise."""
    if len(first) != len(second) or len(first) != width * height * 3:
        raise ValueError('boot captures have inconsistent framebuffer sizes')
    changed = total = 0
    delta_sum = 0
    # A 4x4 sample grid keeps the bounded checker quick on 1080p captures.
    step_x = max(1, width // 320)
    step_y = max(1, height // 180)
    for y in range(0, height, step_y):
        for x in range(0, width, step_x):
            offset = (y * width + x) * 3
            delta = max(abs(first[offset + channel] - second[offset + channel])
                        for channel in range(3))
            delta_sum += delta
            changed += delta >= 32
            total += 1
    fraction = changed / total
    mean = delta_sum / total
    return {'changed_sample_fraction': round(fraction, 4), 'mean_max_channel_delta': round(mean, 2),
            'significant': fraction >= 0.01 and mean >= 2.0}


def check_rgb_frames(frames, font):
    if len(frames) < 3:
        raise ValueError('at least three captures spanning boot are required')
    dimensions = {(width, height) for width, height, _ in frames}
    if len(dimensions) != 1:
        raise ValueError('boot captures must have identical dimensions')
    width, height = next(iter(dimensions))
    for _, _, rgb in frames:
        if len(rgb) != width * height * 3:
            raise ValueError('capture is not a complete RGB frame')
    changes = [measure_frame_change(frames[index][2], frames[index + 1][2], width, height)
               for index in range(len(frames) - 1)]
    if not any(change['significant'] for change in changes):
        raise ValueError('boot captures did not show a meaningful screen change')
    prompt = find_prompt(width, height, frames[-1][2], font)
    return {'classification': 'PC/XT FreeDOS prompt capture diagnostic',
            'width': width, 'height': height, 'capture_count': len(frames),
            'boot_frames_changed': True, 'adjacent_changes': changes,
            'prompt': prompt, 'static_signature_rejected': True,
            'whole_board_qualified': False}


def decode_capture(path):
    path = Path(path)
    streams = json.loads(subprocess.check_output([
        'ffprobe', '-v', 'error', '-select_streams', 'v:0', '-show_entries',
        'stream=width,height', '-of', 'json', str(path)]))['streams']
    if len(streams) != 1:
        raise ValueError(f'{path} must contain exactly one video stream')
    width, height = streams[0]['width'], streams[0]['height']
    if width > 3840 or height > 2160:
        raise ValueError(f'{path} exceeds the capture size bound')
    rgb = subprocess.check_output(['ffmpeg', '-v', 'error', '-i', str(path),
                                   '-frames:v', '1', '-threads', '1', '-f',
                                   'rawvideo', '-pix_fmt', 'rgb24', '-'])
    return width, height, rgb, hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('captures', nargs='+', type=Path)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    try:
        decoded = [decode_capture(path) for path in args.captures]
        result = check_rgb_frames([(width, height, rgb) for width, height, rgb, _ in decoded], load_font())
        result['captures'] = [{'path': path.name, 'sha256': digest}
                              for path, (_, _, _, digest) in zip(args.captures, decoded)]
    except (ValueError, OSError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        parser.exit(1, str(error) + '\n')
    encoded = json.dumps(result, indent=2) + '\n'
    if args.output:
        args.output.write_text(encoded)
    print(encoded, end='')


if __name__ == '__main__':
    main()
