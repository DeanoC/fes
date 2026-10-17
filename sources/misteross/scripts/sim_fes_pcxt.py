#!/usr/bin/env python3
"""Simulate the PC/XT ISA fabric and 8088 bring-up; no board or kit claim."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
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
CPU_SOURCES = (
    'cores/fes-pcxt/vendor/mcl86/mcl86_adder.sv',
    'cores/fes-pcxt/vendor/mcl86/mcl86_eu_core.sv',
    'cores/fes-pcxt/vendor/mcl86/mcl86_biu_max.sv',
    'cores/fes-pcxt/vendor/mcl86/mcl86_ucode.sv',
    'cores/fes-pcxt/vendor/mcl86/i8088.sv',
    'cores/fes-pcxt/rtl/pcxt_cpu_bus.sv',
    'cores/fes-pcxt/rtl/pcxt_cpu.sv',
    'cores/fes-pcxt/sim/cpu_top.sv',
    'cores/fes-pcxt/sim/cpu_tb.cpp',
)


def verify_cpu_sources():
    vendor = ROOT / 'cores/fes-pcxt/vendor/mcl86'
    manifest = json.loads((vendor / 'source.json').read_text())
    for item in manifest['files']:
        if hashlib.sha256((vendor / item['path']).read_bytes()).hexdigest() != item['sha256']:
            raise ValueError('vendored MCL86 source hash mismatch: ' + item['path'])
    return vendor


def cpu_inputs(output):
    vendor = verify_cpu_sources()
    shutil.copyfile(vendor / 'mcl86_ucode.mem', output / 'mcl86_ucode.mem')
    subprocess.run(['as', '--32', '-o', str(output / 'diagnostic.o'),
                    str(ROOT / 'cores/fes-pcxt/sim/diagnostic.S')], check=True)
    subprocess.run(['ld', '-m', 'elf_i386', '-Ttext', '0', '--oformat', 'binary',
                    '-e', '0', '-o', str(output / 'diagnostic.bin'),
                    str(output / 'diagnostic.o')], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--suite', choices=('all', 'isa', 'cpu'), default='all')
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    output = ROOT / 'build/sim/fes-pcxt'
    output.mkdir(parents=True, exist_ok=True)
    for suite in ('isa', 'cpu') if args.suite == 'all' else (args.suite,):
        build = output / suite
        build.mkdir(parents=True, exist_ok=True)
        sources = SOURCES if suite == 'isa' else tuple(s for s in SOURCES if s.endswith('.sv')) + CPU_SOURCES
        if suite == 'cpu':
            cpu_inputs(build)
        subprocess.run([args.verilator, '--cc', '--exe', '--build', '-Wall',
                        *(['--timescale-override', '1ns/1ps',
                           str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt')] if suite == 'cpu' else []),
                        '--top-module', suite + '_top', '--Mdir', str(build),
                        '-I' + str(ROOT / 'cores/fes-common/generated'),
                        '-CFLAGS', '-std=c++17 -O2',
                        *[str(ROOT / source) for source in sources]], cwd=ROOT, check=True)
        subprocess.run([str(build / ('V' + suite + '_top')),
                        *([str(build / 'diagnostic.bin')] if suite == 'cpu' else [])],
                       cwd=build, check=True)


if __name__ == '__main__':
    main()
