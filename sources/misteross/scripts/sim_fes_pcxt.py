#!/usr/bin/env python3
"""Simulate the PC/XT ISA fabric, 8088 and motherboard; no board or kit claim."""
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

CHIPSET_SOURCES = (
    'cores/fes-pcxt/vendor/kf8259/KF8259.sv',
    'cores/fes-pcxt/vendor/kf8259/KF8259_Bus_Control_Logic.sv',
    'cores/fes-pcxt/vendor/kf8259/KF8259_Control_Logic.sv',
    'cores/fes-pcxt/vendor/kf8259/KF8259_In_Service.sv',
    'cores/fes-pcxt/vendor/kf8259/KF8259_Interrupt_Request.sv',
    'cores/fes-pcxt/vendor/kf8259/KF8259_Priority_Resolver.sv',
    'cores/fes-pcxt/vendor/kf8253/KF8253.sv',
    'cores/fes-pcxt/vendor/kf8253/KF8253_Control_Logic.sv',
    'cores/fes-pcxt/vendor/kf8253/KF8253_Counter.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Control_Logic.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Group.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Port.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Port_C.sv',
    'cores/fes-pcxt/rtl/pcxt_xtio.sv',
    'cores/fes-pcxt/rtl/pcxt_xt_keyboard.sv',
    'cores/fes-pcxt/rtl/pcxt_speaker.sv',
    'cores/fes-pcxt/rtl/pcxt_dma.sv',
    'cores/fes-pcxt/rtl/pcxt_motherboard.sv',
    'cores/fes-pcxt/sim/motherboard_top.sv',
)


DISPLAY_SOURCES = (
    'cores/fes-pcxt/expansions/pcxt_6845.sv',
    'cores/fes-pcxt/expansions/pcxt_display_card.sv',
    'cores/fes-pcxt/expansions/pcxt_cga_card.sv',
    'cores/fes-pcxt/expansions/pcxt_mda_card.sv',
    'cores/fes-pcxt/sim/display_top.sv',
)


VIDEO_SOURCES = (
    'cores/fes-common/rtl/coleco_video_dpram.v',
    'cores/fes-common/rtl/fes_video_part_direct.v',
    'cores/fes-common/rtl/fes_video_part_scanlines.v',
    'cores/fes-pcxt/rtl/pcxt_video.sv',
    'cores/fes-pcxt/rtl/pcxt_video_output.sv',
    'cores/fes-pcxt/sim/video_top.sv',
)


MEMORY_SOURCES = (
    'cores/fes-ramtest/rtl/sdram_addon_port.v',
    'cores/fes-pcxt/rtl/pcxt_memory.sv',
    'cores/fes-pcxt/sim/memory_top.sv',
)


def verify_vendor(name):
    vendor = ROOT / 'cores/fes-pcxt/vendor' / name
    manifest = json.loads((vendor / 'source.json').read_text())
    for item in manifest['files']:
        if hashlib.sha256((vendor / item['path']).read_bytes()).hexdigest() != item['sha256']:
            raise ValueError('vendored ' + name + ' source hash mismatch: ' + item['path'])
    return vendor


def verify_cpu_sources():
    return verify_vendor("mcl86")


def cpu_inputs(output, diagnostic="diagnostic"):
    vendor = verify_cpu_sources()
    shutil.copyfile(vendor / 'mcl86_ucode.mem', output / 'mcl86_ucode.mem')
    subprocess.run(['as', '--32', '-o', str(output / 'diagnostic.o'),
                    str(ROOT / ('cores/fes-pcxt/sim/' + diagnostic + '.S'))], check=True)
    subprocess.run(['ld', '-m', 'elf_i386', '-Ttext', '0', '--oformat', 'binary',
                    '-e', '0', '-o', str(output / 'diagnostic.bin'),
                    str(output / 'diagnostic.o')], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--suite', choices=('all', 'memory', 'memory-machine', 'isa', 'cpu', 'keyboard', 'motherboard', 'dma', 'machine', 'display', 'display-machine', 'video', 'video-mda', 'video-machine'), default='all')
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    output = ROOT / 'build/sim/fes-pcxt'
    output.mkdir(parents=True, exist_ok=True)
    for suite in ('memory', 'memory-machine', 'isa', 'cpu', 'keyboard', 'motherboard', 'dma', 'machine', 'display', 'display-machine', 'video', 'video-mda', 'video-machine') if args.suite == 'all' else (args.suite,):
        build = output / suite
        build.mkdir(parents=True, exist_ok=True)
        sources = SOURCES if suite == 'isa' else tuple(s for s in SOURCES if s.endswith('.sv')) + CPU_SOURCES
        if suite == 'memory':
            sources = MEMORY_SOURCES + ('cores/fes-pcxt/sim/memory_tb.cpp',)
        if suite in ('video', 'video-mda'):
            sources = VIDEO_SOURCES + ('cores/fes-pcxt/sim/video_tb.cpp',)
        if suite == 'display':
            sources = SOURCES[:3] + DISPLAY_SOURCES + ('cores/fes-pcxt/sim/display_tb.cpp',)
        if suite == 'keyboard':
            sources = ('cores/fes-pcxt/rtl/pcxt_xt_keyboard.sv', 'cores/fes-pcxt/sim/keyboard_tb.cpp')
        if suite in ('motherboard', 'dma', 'machine'):
            for name in ('kf8259', 'kf8253', 'kf8255'):
                verify_vendor(name)
            sources = (tuple(s for s in SOURCES if s.endswith('.sv')) + CHIPSET_SOURCES +
                       ('cores/fes-pcxt/sim/motherboard_tb.cpp',))
        if suite == 'dma':
            sources = tuple(s for s in sources if s.endswith('.sv')) + ('cores/fes-pcxt/sim/dma_tb.cpp',)
        if suite == 'machine':
            sources = (tuple(s for s in sources if s.endswith('.sv')) +
                       tuple(s for s in CPU_SOURCES
                             if s.endswith('.sv') and not s.endswith('/cpu_top.sv')) +
                       ('cores/fes-pcxt/sim/machine_top.sv', 'cores/fes-pcxt/sim/machine_tb.cpp'))
        if suite in ('display-machine', 'video-machine', 'memory-machine'):
            for name in ('kf8259', 'kf8253', 'kf8255'):
                verify_vendor(name)
            sources = (SOURCES[:3] + DISPLAY_SOURCES + CHIPSET_SOURCES[:-1] +
                       tuple(s for s in CPU_SOURCES if s.endswith('.sv') and not s.endswith('/cpu_top.sv')) +
                       ('cores/fes-pcxt/sim/display_machine_top.sv', 'cores/fes-pcxt/sim/display_machine_tb.cpp'))
        if suite == 'memory-machine':
            sources = sources[:-1] + MEMORY_SOURCES + ('cores/fes-pcxt/sim/memory_machine_top.sv', 'cores/fes-pcxt/sim/memory_machine_tb.cpp')
        if suite == 'video-machine':
            sources = sources[:-1] + VIDEO_SOURCES[:-1] + ('cores/fes-pcxt/sim/video_machine_top.sv', 'cores/fes-pcxt/sim/video_machine_tb.cpp')
        if suite in ('cpu', 'machine', 'display-machine', 'video-machine', 'memory-machine'):
            cpu_inputs(build, 'display' if suite in ('display-machine', 'video-machine', 'memory-machine') else 'motherboard' if suite == 'machine' else 'diagnostic')
        top = 'video_top' if suite in ('video', 'video-mda') else 'pcxt_xt_keyboard' if suite == 'keyboard' else 'motherboard_top' if suite == 'dma' else suite.replace('-', '_') + '_top'
        subprocess.run([args.verilator, '--cc', '--exe', '--build', '-Wall',
                        *(['--timescale-override', '1ns/1ps',
                           str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt')] if suite != 'isa' else []),
                        *([str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt')] if suite in ('motherboard', 'dma', 'machine', 'display-machine', 'video-machine', 'memory-machine') else []),
                        '-I' + str(ROOT / 'cores/fes-pcxt/vendor/kf8259'),
                        '-I' + str(ROOT / 'cores/fes-pcxt/vendor/kf8253'),
                        '-I' + str(ROOT / 'cores/fes-pcxt/vendor/kf8255'),
                        *(["-GMDA=1'b1"] if suite == 'video-mda' else []),
                        '--top-module', top, '--Mdir', str(build),
                        '-I' + str(ROOT / 'cores/fes-common/generated'),
                        '-CFLAGS', '-std=c++17 -O2',
                        *(['-I' + str(ROOT / 'cores/fes-pcxt/sim')] if suite in ('memory', 'memory-machine') else []),
                        *[str(ROOT / source) for source in sources]], cwd=ROOT, check=True)
        for delay in (0, 1, 2) if suite in ('memory', 'memory-machine') else (None,):
            subprocess.run([str(build / ('V' + top)),
                            *(['mda'] if suite == 'video-mda' else []),
                            *([str(build / 'diagnostic.bin')] if suite in ('cpu', 'machine', 'display-machine', 'video-machine', 'memory-machine') else []),
                            *([str(delay)] if delay is not None else [])],
                           cwd=build, check=True)


if __name__ == '__main__':
    main()
