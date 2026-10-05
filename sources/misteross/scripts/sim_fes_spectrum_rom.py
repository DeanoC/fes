#!/usr/bin/env python3
"""Compare production M10K wiring with a two-stage read using real Yosys models."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mem-sim', type=Path, required=True,
                        help='selected Yosys intel_alm/common/mem_sim.v')
    parser.add_argument('--verilator', default='verilator')
    args = parser.parse_args()
    model = args.mem_sim.resolve(strict=True)
    if not model.is_file():
        parser.error('--mem-sim must name the selected Yosys memory model file')
    output = ROOT / 'build/sim/fes-spectrum-rom'
    output.mkdir(parents=True, exist_ok=True)
    (output / 'summary.json').unlink(missing_ok=True)
    sources = [ROOT / 'cores/fes-spectrum/rtl/spectrum_rom.v',
               ROOT / 'cores/fes-spectrum/sim/rom_primitive_top.sv',
               ROOT / 'cores/fes-spectrum/sim/rom_primitive_tb.cpp', model]
    command = [args.verilator, '--cc', '--exe', '--build', '-j', '4', '-O2',
               '--top-module', 'rom_primitive_top', '-UVERILATOR',
               '-Wno-PINMISSING', '--Mdir', str(output), *map(str, sources)]
    # The actual primitive model is external compiler input; preserve its digest.
    inputs = {str(p): hashlib.sha256(p.read_bytes()).hexdigest() for p in sources + [Path(__file__)]}
    with (output / 'build.log').open('w') as log:
        subprocess.run(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=180)
    result = subprocess.run([str(output / 'Vrom_primitive_top')], cwd=ROOT,
                            capture_output=True, text=True, check=True, timeout=60)
    (output / 'run.log').write_text(result.stdout + result.stderr)
    if not result.stdout.startswith('PASS production M10K ROM:'):
        raise ValueError('ROM regression did not report completed comparisons')
    for p in sources + [Path(__file__)]:
        if hashlib.sha256(p.read_bytes()).hexdigest() != inputs[str(p)]:
            raise ValueError('ROM regression input changed during simulation')
    (output / 'summary.json').write_text(json.dumps({
        'scope': 'production_primitive_host_simulation', 'hardware_acceptance': False,
        'status': 'pass', 'inputs': inputs, 'build_command': command,
        'result': result.stdout.strip()}, indent=2, sort_keys=True) + '\n')
    print(result.stdout.strip())


if __name__ == '__main__':
    main()
