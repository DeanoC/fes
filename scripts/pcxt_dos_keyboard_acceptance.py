#!/usr/bin/env python3
"""Preflight host input and check PC/XT DOS directory captures.

The visual check uses the locked GLaBIOS font and before/after captures. It
does not establish the package identity, prove input delivery, or operate a
kit; those remain the responsibility of the owning HIL run.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import tomllib

if __package__:
    from . import pcxt_dos_hil_acceptance as visual
else:
    import pcxt_dos_hil_acceptance as visual

ENTRY = re.compile(r'\bFESXT\s+TXT\b', re.IGNORECASE)


def check_host_input_config(path):
    """Fail closed without exposing the contents of a private TOML config."""
    try:
        data = Path(path).read_bytes()
        if len(data) > 1024 * 1024:
            raise ValueError
        config = tomllib.loads(data.decode('utf-8'))
    except (OSError, UnicodeError, ValueError, tomllib.TOMLDecodeError):
        raise ValueError('host input configuration cannot be read or parsed') from None
    remote = config.get('remote_input')
    if not isinstance(remote, dict) or remote.get('enabled') is not True:
        raise ValueError('host input configuration must enable remote input')
    return {'remote_input_enabled': True}


def read_text(frame, font):
    """Decode CGA characters using prompt alignment and locked ROM glyphs."""
    width, height, rgb = frame
    anchor = visual.find_prompt(width, height, rgb, font)
    fg, bg = anchor['foreground_rgb'], anchor['background_rgb']
    dx, dy = anchor['alignment_dx'], anchor['alignment_dy']
    glyphs = {code: int.from_bytes(font[code * 16:code * 16 + 8], 'big')
              for code in range(32, 127)}
    lines = []
    for row in range(25):
        chars = []
        for col in range(80):
            observed = 0
            for gy in range(8):
                for gx in range(8):
                    sx, sy = col * 8 + gx, row * 8 + gy
                    x = round((160 + (sx + 0.5) * 1.5) * width / 1280) + dx
                    y = round((sy + 0.5) * height / 200) + dy
                    value = visual._pixel(rgb, width, x, y)
                    df = sum((a - b) ** 2 for a, b in zip(value, fg))
                    db = sum((a - b) ** 2 for a, b in zip(value, bg))
                    observed = (observed << 1) | (df < db)
            distances = [(observed ^ pattern).bit_count() for pattern in glyphs.values()]
            best = min(distances)
            matches = [code for code, distance in zip(glyphs, distances) if distance == best]
            chars.append(chr(matches[0]) if best <= 3 and len(matches) == 1 else '?')
        lines.append(''.join(chars))
    return {'lines': lines, 'alignment_dx': dx, 'alignment_dy': dy}


def find_entry(lines):
    for row, line in enumerate(lines):
        if ENTRY.search(line):
            return row
    return None


def find_listing(lines):
    """Require the expected 8.3 filename and a later prompt on the screen."""
    for row, line in enumerate(lines):
        if not ENTRY.search(line):
            continue
        for later in range(row + 1, len(lines)):
            if 'A:\\>' in lines[later].upper():
                return row, later
    return None


def check_captures(before, after, font):
    if before[:2] != after[:2]:
        raise ValueError('before and after capture dimensions differ')
    initial = read_text(before, font)
    if find_entry(initial['lines']) is not None:
        raise ValueError('before-input capture already contains the listing')
    result = read_text(after, font)
    listing = find_listing(result['lines'])
    if not listing:
        raise ValueError('after-input capture lacks the directory listing and later prompt')
    return {'classification': 'PC/XT DOS directory capture check',
            'entry_row': listing[0], 'prompt_row': listing[1],
            # Do not publish unrelated on-screen text from either capture.
            'entry': 'FESXT.TXT', 'prompt': 'A:\\>',
            'alignment_dx': result['alignment_dx'], 'alignment_dy': result['alignment_dy'],
            'whole_board_qualified': False}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    preflight = commands.add_parser('preflight', help='check private host input configuration')
    preflight.add_argument('--config', type=Path, required=True)
    captures = commands.add_parser('captures', help='check before/after DOS captures')
    captures.add_argument('--before', type=Path, required=True)
    captures.add_argument('--after', type=Path, required=True)
    captures.add_argument('--output', type=Path)
    args = parser.parse_args(argv)
    try:
        if args.command == 'preflight':
            result = check_host_input_config(args.config)
        else:
            if args.output and args.output.exists():
                raise ValueError('output already exists')
            before = visual.decode_capture(args.before)
            after = visual.decode_capture(args.after)
            result = check_captures(before[:3], after[:3], visual.load_font())
            result['captures'] = [{'role': 'before', 'sha256': before[3]},
                                  {'role': 'after', 'sha256': after[3]}]
        encoded = json.dumps(result, indent=2) + '\n'
        if args.command == 'captures' and args.output:
            with args.output.open('x') as output:
                output.write(encoded)
        print(encoded, end='')
    except (ValueError, OSError, subprocess.CalledProcessError, json.JSONDecodeError) as exc:
        parser.exit(1, str(exc) + '\n')


if __name__ == '__main__':
    main()
