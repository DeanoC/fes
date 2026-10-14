#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Observe real C64 firmware on the RTL machine; never implies acceptance."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--basic', type=Path, required=True)
    parser.add_argument('--kernal', type=Path, required=True)
    parser.add_argument('--disk', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cycles', type=int, default=3 * 52224000)
    parser.add_argument('--type-command', default='', help='native C64 text, starting at three seconds')
    args = parser.parse_args()
    if not 1044480 <= args.cycles <= 60 * 52224000:
        parser.error('cycles must cover 0.02 to 60 machine seconds')
    inputs = {}
    blobs = {}
    for name, size in [('basic', 8192), ('kernal', 8192), ('disk', 174848)]:
        path = getattr(args, name).resolve()
        blob = path.read_bytes()
        if len(blob) != size:
            parser.error(f'{name}: expected {size} bytes, got {len(blob)}')
        blobs[name] = blob
        inputs[name] = {'path': str(path), 'bytes': size,
                        'sha256': hashlib.sha256(blob).hexdigest()}
    module = Path(__file__).resolve().parents[1]
    binary = module / 'build/sim/fes-c64-boot/Vc64_boot_sim_top'
    if not binary.is_file():
        parser.error('run make sim-fes-c64-boot-build first')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    for name, blob in [('firmware', blobs['basic'] + blobs['kernal']), ('disk', blobs['disk'])]:
        (output / f'{name}.hex').write_text(''.join(f'{b:02x}\n' for b in blob))
    key_events = []
    event_cycle = 3 * 52224000
    key_period = 52224000 // 20  # 50 ms held, 50 ms released
    for char in args.type_command.upper():
        shift = False
        if 'A' <= char <= 'Z':
            usage = ord(char) - ord('A') + 4
        elif char in '1234567890':
            usage = '1234567890'.index(char) + 0x1e
        elif char == '"':
            usage, shift = 0x1f, True  # native C64 Shift+2
        else:
            mapping = {' ': 0x2c, ',': 0x36, '.': 0x37, '*': 0x30,
                       '\r': 0x28, '\n': 0x28, ':': 0x34, ';': 0x33,
                       '=': 0x2e, '+': 0x57, '-': 0x2d, '/': 0x38, '@': 0x2f}
            if char not in mapping:
                parser.error(f'unsupported native C64 typing character: {char!r}')
            usage = mapping[char]
        if shift:
            key_events.append((event_cycle, 0xe1, 1))
        key_events.append((event_cycle, usage, 1))
        event_cycle += key_period
        key_events.append((event_cycle, usage, 0))
        if shift:
            key_events.append((event_cycle, 0xe1, 0))
        event_cycle += key_period
    if key_events and event_cycle >= args.cycles:
        parser.error('cycle budget must extend beyond the last typed key')
    if key_events:
        (output / 'keyboard.txt').write_text(''.join(f'{cycle} {usage} {down}\n'
            for cycle, usage, down in key_events))
    command = [str(binary), f'+C64_FIRMWARE_HEX={output / "firmware.hex"}',
               f'+C64_DISK_HEX={output / "disk.hex"}', f'+C64_SYS_CYCLES={args.cycles}']
    if key_events:
        command.append(f'+C64_KEY_EVENTS={output / "keyboard.txt"}')
    with (output / 'boot.log').open('w') as log:
        subprocess.run(command, cwd=output, stdout=log, stderr=subprocess.STDOUT, check=True)
    outputs = {name: hashlib.sha256((output / name).read_bytes()).hexdigest()
               for name in ['boot.log', 'boot-ram.bin', 'boot.ppm']}
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=module, text=True).strip()
    source_diff = subprocess.check_output(['git', 'diff', 'HEAD', '--',
        'cores/fes-c64', 'Makefile', 'scripts/c64_boot_observe.py'], cwd=module)
    receipt = {'source_commit': revision, 'source_diff_sha256': hashlib.sha256(source_diff).hexdigest(),
               'evidence': 'host-only observation', 'simulated_seconds': args.cycles / 52224000,
               'system_clock_hz': 52224000, 'pixel_clock_hz': 74250000,
               'cpu_clock_hz': 1022727, 'inputs': inputs, 'outputs': outputs,
               'simulator_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
               'typed_command': args.type_command, 'key_event_count': len(key_events),
               'boot_acceptance': False, 'demo_acceptance': False}
    (output / 'observation.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print((output / 'boot.log').read_text(), end='')


if __name__ == '__main__':
    main()
