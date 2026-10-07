#!/usr/bin/env python3
"""Test original Z80 RTL through Verilator; no downloads or hardware access."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/z80'
SIM = ROOT / 'cores/fes-common/sim/z80'
CASES = ('alu-nmos', 'alu-docs', 'engine-nmos', 'fast', 'bus-nmos', 'bus-fast', 'nmos')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=2)
    parser.add_argument('--case', choices=CASES, action='append')
    args = parser.parse_args()
    if not 1 <= args.jobs <= 32:
        parser.error('--jobs must be between 1 and 32')
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    for case in args.case or CASES:
        options, run_args = [], []
        flags = '-std=c++17 -O2'
        if case.startswith('alu-'):
            top, files, tb = 'fes_z80_alu', ['fes_z80_alu.sv'], 'alu_tb.cpp'
            nmos = case == 'alu-nmos'
            options = ["-GNMOS=1'b1" if nmos else "-GNMOS=1'b0"]
            flags += ' -DZ80_ALU_NMOS=' + str(int(nmos))
        elif case.startswith('bus-'):
            top, files, tb = 'fes_z80_bus', ['fes_z80_bus.sv'], 'bus_tb.cpp'
            fast = case == 'bus-fast'
            options = ["-GFAST=1'b1" if fast else "-GFAST=1'b0"]
            if fast:
                run_args = ['--fast']
        else:
            files = ['fes_z80_alu.sv', 'fes_z80_engine.sv']
            tb = 'cpu_tb.cpp'
            if case == 'fast':
                top = 'fes_z80_fast'
                files += ['fes_z80_fast.sv']
                flags += ' -DDOCS_ONLY'
            elif case == 'nmos':
                top = 'fes_z80_nmos'
                files += ['fes_z80_bus.sv', 'fes_z80_nmos.sv']
                tb = 'nmos_tb.cpp'
            else:
                top = 'fes_z80_engine'
        output = ROOT / 'build/sim/fes-z80' / case
        output.mkdir(parents=True, exist_ok=True)
        command = [args.verilator, '--cc', '--exe', '--build', '-j', str(args.jobs),
                   '--top-module', top, '-Wall', '-Wno-UNUSEDSIGNAL', '--Mdir', str(output),
                   '-CFLAGS', flags, *options, *(str(RTL / name) for name in files), str(SIM / tb)]
        log = output / 'build.log'
        with log.open('w') as stream:
            result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
        if result.returncode:
            sys.stderr.write(log.read_text())
            return result.returncode
        print(f'{case}:', flush=True)
        result = subprocess.run([str(output / ('V' + top)), *run_args], cwd=ROOT)
        if result.returncode:
            return result.returncode
    print('Z80 RTL checks passed (host simulation only).')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
