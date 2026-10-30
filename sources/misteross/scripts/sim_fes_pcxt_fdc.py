#!/usr/bin/env python3
"""Simulate the XT floppy card through the real registered ISA fabric."""
import argparse
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default='verilator')
    args = parser.parse_args()
    work = ROOT / 'build/sim/fes-pcxt/fdc'
    work.mkdir(parents=True, exist_ok=True)
    sources = ('cores/fes-pcxt/rtl/pcxt_isa_endpoint.sv',
               'cores/fes-pcxt/rtl/pcxt_isa_boundary.sv',
               'cores/fes-pcxt/rtl/pcxt_isa_bridge.sv',
               'cores/fes-pcxt/expansions/pcxt_fdc_card.sv',
               'cores/fes-pcxt/sim/fdc_top.sv', 'cores/fes-pcxt/sim/fdc_tb.cpp')
    subprocess.run([args.verilator, '--cc', '--exe', '--build', '-Wall',
                    '--top-module', 'fdc_top', '--Mdir', str(work),
                    '-I' + str(ROOT / 'cores/fes-common/generated'),
                    '-CFLAGS', '-std=c++17 -O2',
                    *(str(ROOT / source) for source in sources)], check=True)
    subprocess.run([str(work / 'Vfdc_top')], cwd=work, check=True)


if __name__ == '__main__':
    main()
