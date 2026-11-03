#!/usr/bin/env python3
"""Exercise PC/XT sector staging and shared disk-port arbitration."""
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def main():
    for name, rtl in (('sector_writer', 'pcxt_sector_writer'),
                      ('media_arbiter', 'pcxt_media_arbiter')):
        output = ROOT / 'build/sim/fes-pcxt' / name
        output.mkdir(parents=True, exist_ok=True)
        top = name + '_tb'
        subprocess.run([os.environ.get('VERILATOR', 'verilator'), '--binary', '--timing',
                        '-Wall', '--top-module', top, '--Mdir', str(output),
                        str(ROOT / f'cores/fes-pcxt/rtl/{rtl}.sv'),
                        str(ROOT / f'cores/fes-pcxt/sim/{top}.sv')], check=True)
        subprocess.run([str(output / ('V' + top))], cwd=ROOT, check=True)


if __name__ == '__main__':
    main()
