#!/usr/bin/env python3
"""Simulate PC/XT ISA transaction RTL; no CPU, board bitstream or kit claim."""
import argparse
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
SOURCES = (
    'cores/fes-pcxt/rtl/pcxt_isa_bridge.sv',
    'cores/fes-pcxt/rtl/pcxt_isa_boundary.sv',
    'cores/fes-pcxt/rtl/pcxt_isa_endpoint.sv',
    'cores/fes-pcxt/expansions/pcxt_isa_probe.sv',
    'cores/fes-pcxt/sim/isa_top.sv',
    'cores/fes-pcxt/sim/isa_tb.cpp',
)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    output = ROOT / 'build/sim/fes-pcxt'
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '-Wall',
                    '--top-module', 'isa_top', '--Mdir', str(output),
                    '-I' + str(ROOT / 'cores/fes-common/generated'),
                    '-CFLAGS', '-std=c++17 -O2',
                    *[str(ROOT / source) for source in SOURCES]], cwd=ROOT, check=True)
    subprocess.run([str(output / 'Visa_top')], check=True)


if __name__ == '__main__':
    main()
