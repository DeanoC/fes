#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Observe external 1541 ROM on the native motherboard, with no spinning disk."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rom', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--cycles', type=int, default=3 * 52224000)
    args = parser.parse_args()
    if not 522240 <= args.cycles <= 300 * 52224000:
        parser.error('cycles must cover 0.01 to 300 machine seconds')
    data = args.rom.read_bytes()
    if len(data) != 16384:
        parser.error(f'expected a 16384-byte drive ROM, got {len(data)}')
    module = Path(__file__).resolve().parents[1]
    binary = module / 'build/sim/fes-c64-drive/Vc64_drive_sim_top'
    if not binary.is_file():
        parser.error('run make sim-fes-c64-drive-build first')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    (output/'firmware.hex').write_text(''.join(f'{v:02x}\n' for v in bytes(20480)+data))
    revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=module, text=True).strip()
    # The executable is frozen. The revision describes this invocation, not
    # the compiler inputs; this observation does not claim sealed provenance.
    with tempfile.TemporaryDirectory(prefix='drive-simulator-', dir=output) as temp:
        frozen = Path(temp) / binary.name
        shutil.copy2(binary, frozen)
        binary_hash = hashlib.sha256(frozen.read_bytes()).hexdigest()
        with (output/'drive.log').open('w') as log:
            subprocess.run([str(frozen), f'+C64_FIRMWARE_HEX={output / "firmware.hex"}',
                            f'+C64_DRIVE_CYCLES={args.cycles}'], cwd=output,
                           stdout=log, stderr=subprocess.STDOUT, check=True)
    receipt = {'evidence': 'host-only development drive observation',
               'checkout_commit_at_start': revision, 'simulator_sha256': binary_hash,
               'rom': {'path': str(args.rom.resolve()), 'bytes': len(data),
                       'sha256': hashlib.sha256(data).hexdigest()},
               'system_clock_hz': 52224000, 'cpu_clock_hz': 1000000,
               'cycles': args.cycles, 'disk_electronics': 'inactive stub pins',
               'drive_boot_acceptance': False, 'demo_acceptance': False,
               'outputs': {name: hashlib.sha256((output/name).read_bytes()).hexdigest()
                           for name in ['drive.log', 'drive-ram.bin']}}
    (output/'observation.json').write_text(json.dumps(receipt, indent=2)+'\n')
    print((output/'drive.log').read_text(), end='')


if __name__ == '__main__':
    main()
