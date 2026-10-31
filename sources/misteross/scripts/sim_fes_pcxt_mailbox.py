#!/usr/bin/env python3
"""Exercise the opt-in PC/XT computer mailbox against real HDL."""
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    output = ROOT / 'build/sim/fes-pcxt/mailbox'
    output.mkdir(parents=True, exist_ok=True)
    subprocess.run([os.environ.get('VERILATOR', 'verilator'), '--cc', '--exe', '--build',
        '--top-module', 'fes_computer_mailbox_pcxt', '-Wall',
        '-I' + str(ROOT / 'cores/fes-common/generated'), '--Mdir', str(output),
        '-CFLAGS', '-std=c++17 -O2',
        str(ROOT / 'cores/fes-common/rtl/fes_computer_mailbox.v'),
        str(ROOT / 'cores/fes-common/sim/computer_mailbox_pcxt_tb.cpp')], check=True)
    subprocess.run([str(output / 'Vfes_computer_mailbox_pcxt')], cwd=ROOT, check=True)


if __name__ == '__main__':
    main()
