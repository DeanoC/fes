#!/usr/bin/env python3
"""Test the original RV32I CPU, the fes.riscv system and its board shell through Verilator.

No downloads, compiler cache or hardware access."""
import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/riscv'
SIM = ROOT / 'cores/fes-common/sim/riscv'
CORE = ROOT / 'cores/fes-riscv'
CASES = ('alu', 'cpu', 'firmware', 'system', 'faults', 'board')
SYSTEM_SOURCES = ('cores/fes-common/rtl/fes_video_720p.v', 'cores/fes-common/rtl/riscv/fes_rv32_alu.sv',
                  'cores/fes-common/rtl/riscv/fes_rv32_csr.sv', 'cores/fes-common/rtl/riscv/fes_rv32_cpu.sv',
                  'cores/fes-riscv/rtl/fes_riscv_lane_ram.sv', 'cores/fes-riscv/rtl/fes_riscv_system.sv')


def fault_firmware(output):
    """Assemble the bus-fault test program into lane images and return -G overrides."""
    import importlib.util
    spec = importlib.util.spec_from_file_location('fes_riscv_assemble', CORE / 'firmware/assemble.py')
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    program = module.assemble((CORE / 'sim/fault_test.S').read_text())
    _, lanes = module.images(program)
    options = []
    for lane, text in enumerate(lanes):
        path = output / f'fault_test.lane{lane}.hex'
        path.write_text(text)
        options.append(f'-GFIRMWARE_LANE{lane}="{path}"')
    return options


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=2)
    parser.add_argument('--case', choices=CASES, action='append')
    parser.add_argument('--seeds', type=int, default=6, help='random instruction stream seeds')
    args = parser.parse_args()
    if not 1 <= args.jobs <= 32:
        parser.error('--jobs must be between 1 and 32')
    if not 1 <= args.seeds <= 1000:
        parser.error('--seeds must be between 1 and 1000')
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    for case in args.case or CASES:
        if case == 'firmware':
            result = subprocess.run([sys.executable, str(CORE / 'firmware/assemble.py'), '--check'], cwd=ROOT)
            if result.returncode:
                return result.returncode
            continue
        run_args = []
        if case == 'alu':
            top, tb = 'fes_rv32_alu', SIM / 'alu_tb.cpp'
            files = [RTL / 'fes_rv32_alu.sv']
            options = []
        elif case == 'cpu':
            top, tb = 'fes_rv32_cpu', SIM / 'cpu_tb.cpp'
            files = [RTL / name for name in ('fes_rv32_alu.sv', 'fes_rv32_csr.sv', 'fes_rv32_cpu.sv')]
            options = ['--public-flat-rw']
            run_args = [f'--seeds={args.seeds}']
        elif case == 'faults':
            top, tb = 'fes_riscv_system', CORE / 'sim/faults_tb.cpp'
            files = [ROOT / name for name in SYSTEM_SOURCES]
            output = ROOT / 'build/sim/fes-riscv' / case
            output.mkdir(parents=True, exist_ok=True)
            options = ['-Wno-UNUSEDSIGNAL', '--public-flat-rw', *fault_firmware(output)]
        elif case == 'system':
            top, tb = 'fes_riscv_system', CORE / 'sim/system_tb.cpp'
            files = [ROOT / name for name in SYSTEM_SOURCES]
            options = ['-Wno-UNUSEDSIGNAL']
        else:
            top, tb = 'top', CORE / 'sim/board_tb.cpp'
            files = [ROOT / 'cores/fes-pong/sim/board_models.v', ROOT / 'cores/fes-riscv/rtl/top.v',
                     ROOT / 'cores/fes-common/rtl/fes_application_gp.v', *(ROOT / name for name in SYSTEM_SOURCES)]
            options = ['--public-flat-rw', '-Wno-UNUSEDSIGNAL', '-Icores/fes-common/generated']
        output = ROOT / 'build/sim/fes-riscv' / case
        output.mkdir(parents=True, exist_ok=True)
        command = [args.verilator, '--cc', '--exe', '--build', '-j', str(args.jobs),
                   '--top-module', top, '-Wall', '--Mdir', str(output),
                   '-CFLAGS', '-std=c++17 -O2', *options, *(str(path) for path in files), str(tb)]
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
    print('RV32I RTL checks passed (host simulation only).')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
