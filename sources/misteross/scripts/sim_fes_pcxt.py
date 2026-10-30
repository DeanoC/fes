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
    'cores/fes-pcxt/rtl/pcxt_logic_operand1_mux.sv',
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

CPU_MUX_MAP = 'cores/fes-pcxt/synth/logic_operand1_map.v'

CHIPSET_SOURCES = (
    'cores/fes-common/vendor/kf8259/KF8259.sv',
    'cores/fes-common/vendor/kf8259/KF8259_Bus_Control_Logic.sv',
    'cores/fes-common/vendor/kf8259/KF8259_Control_Logic.sv',
    'cores/fes-common/vendor/kf8259/KF8259_In_Service.sv',
    'cores/fes-common/vendor/kf8259/KF8259_Interrupt_Request.sv',
    'cores/fes-common/vendor/kf8259/KF8259_Priority_Resolver.sv',
    'cores/fes-common/vendor/kf8253/KF8253.sv',
    'cores/fes-common/vendor/kf8253/KF8253_Control_Logic.sv',
    'cores/fes-common/vendor/kf8253/KF8253_Counter.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Control_Logic.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Group.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Port.sv',
    'cores/fes-pcxt/vendor/kf8255/KF8255_Port_C.sv',
    'cores/fes-pcxt/rtl/pcxt_xtio.sv',
    'cores/fes-pcxt/rtl/pcxt_xt_keyboard.sv',
    'cores/fes-pcxt/rtl/pcxt_speaker.sv',
    'cores/fes-common/rtl/fes_dma8237.sv',
    'cores/fes-pcxt/rtl/pcxt_motherboard.sv',
    'cores/fes-pcxt/sim/motherboard_top.sv',
)


DISPLAY_SOURCES = (
    'cores/fes-pcxt/expansions/pcxt_6845.sv',
    'cores/fes-pcxt/expansions/pcxt_display_card.sv',
    'cores/fes-pcxt/expansions/pcxt_cga_card.sv',
    'cores/fes-pcxt/expansions/pcxt_mda_card.sv',
    'cores/fes-pcxt/expansions/pcxt_fdc_card.sv',
    'cores/fes-pcxt/rtl/pcxt_display_bus.sv',
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


SYSTEM_SOURCES = (SOURCES[:4] + DISPLAY_SOURCES[:-1] + CHIPSET_SOURCES[:-1] +
                  tuple(s for s in CPU_SOURCES if s.endswith('.sv') and not s.endswith('/cpu_top.sv')) +
                  MEMORY_SOURCES[:2] + VIDEO_SOURCES[:-1] +
                  ('cores/fes-pcxt/rtl/pcxt_machine.sv', 'cores/fes-pcxt/rtl/pcxt_disk_loader.sv', 'cores/fes-pcxt/rtl/pcxt_system.sv',
                   'cores/fes-pcxt/rtl/pcxt_media_writer.sv', 'cores/fes-pcxt/rtl/pcxt_media.sv',
                   'cores/fes-pcxt/rtl/pcxt_boot.sv'))


def boot_roms(output, data):
    """Emit eight contiguous 8 KiB banks for the local-address boot ROMs."""
    if len(data) != 65536:
        raise ValueError('PC/XT diagnostic must be exactly 64 KiB')
    for bank in range(8):
        payload = data[bank * 8192:(bank + 1) * 8192]
        (output / f'pcxt_diagnostic.bank{bank}.hex').write_text(
            ''.join(f'{byte:02x}\n' for byte in payload))


def board_inputs(output):
    cpu_inputs(output, '../firmware/diagnostic')
    boot_roms(output, (output / 'diagnostic.bin').read_bytes())


# The PIC and PIT imports are shared with pc386 in fes-common; the rest are
# the PC/XT's own.
CHIP_VENDORS = ('kf8259', 'kf8253', 'kf8255')
SHARED_VENDORS = ('kf8259', 'kf8253')


def vendor_dir(name):
    return ROOT / ('cores/fes-common/vendor' if name in SHARED_VENDORS else 'cores/fes-pcxt/vendor') / name


def verify_vendor(name):
    vendor = vendor_dir(name)
    manifest = json.loads((vendor / 'source.json').read_text())
    for item in manifest['files']:
        if hashlib.sha256((vendor / item['path']).read_bytes()).hexdigest() != item['sha256']:
            raise ValueError('vendored ' + name + ' source hash mismatch: ' + item['path'])
    return vendor


def verify_cpu_sources():
    return verify_vendor("mcl86")


def cpu_roms(output):
    vendor = verify_cpu_sources()
    source = vendor / 'mcl86_ucode.mem'
    words = [int(word, 16) for word in source.read_text().split()]
    if len(words) != 4096 or any(word < 0 or word > 0xffffffff for word in words):
        raise ValueError('MCL86 microcode must contain exactly 4096 32-bit words')
    shutil.copyfile(source, output / 'mcl86_ucode.mem')
    # Four byte lanes retain every bit of the unchanged control image.
    for lane in range(4):
        (output / f'mcl86_ucode_byte{lane}.mem').write_text(
            ''.join(f'{(word >> (8 * lane)) & 255:02x}\n' for word in words))
    # Conservative facts over every control-ROM word, independent of guest
    # firmware/data. A different pinned image automatically expands the masks
    # and retains the reserved jump-source hold behavior when it is used.
    add_masks = [0, 0]
    jump7 = False
    for word in words:
        kind = (word >> 28) & 7
        if kind == 2:
            for index, shift in enumerate((20, 16)):
                add_masks[index] |= 1 << ((word >> shift) & 15)
        jump7 |= kind == 1 and ((word >> 20) & 7) == 7
    (output / 'mcl86_facts.vh').write_text(
        '`line 1 "mcl86_facts.vh" 0\n'
        '// Generated from the verified MCL86 control ROM.\n' +
        ''.join(f"localparam [15:0] FES_MC_ADD_OP{i}_MASK = 16'h{mask:04x};\n"
                for i, mask in enumerate(add_masks)) +
        f"localparam [31:0] FES_MC_RESET_WORD = 32'h{words[0x20]:08x};\n" +
        f"localparam FES_MC_JUMP7_USED = 1'b{int(jump7)};\n")


def cpu_inputs(output, diagnostic="diagnostic"):
    cpu_roms(output)
    subprocess.run(['as', '--32', '-o', str(output / 'diagnostic.o'),
                    str(ROOT / ('cores/fes-pcxt/sim/' + diagnostic + '.S'))], check=True)
    subprocess.run(['ld', '-m', 'elf_i386', '-Ttext', '0', '--oformat', 'binary',
                    '-e', '0', '-o', str(output / 'diagnostic.bin'),
                    str(output / 'diagnostic.o')], check=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--suite', choices=('all', 'system', 'system-mda', 'system-board50', 'system-board50-mda', 'memory', 'memory-machine', 'isa', 'cpu', 'cpu-timing', 'keyboard', 'motherboard', 'dma', 'machine', 'display', 'display-machine', 'video', 'video-mda', 'video-machine', 'clock-bridge', 'speaker100', 'speaker50'), default='all')
    args = parser.parse_args()
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    output = ROOT / 'build/sim/fes-pcxt'
    output.mkdir(parents=True, exist_ok=True)
    for suite in ('system', 'system-mda', 'system-board50', 'system-board50-mda', 'memory', 'memory-machine', 'isa', 'cpu', 'cpu-timing', 'keyboard', 'motherboard', 'dma', 'machine', 'display', 'display-machine', 'video', 'video-mda', 'video-machine', 'clock-bridge', 'speaker100', 'speaker50') if args.suite == 'all' else (args.suite,):
        build = output / suite
        build.mkdir(parents=True, exist_ok=True)
        sources = SOURCES if suite == 'isa' else tuple(s for s in SOURCES if s.endswith('.sv')) + CPU_SOURCES
        if suite == 'memory':
            sources = MEMORY_SOURCES + ('cores/fes-pcxt/sim/memory_tb.cpp',)
            model_test = build / 'memory_model_test'
            subprocess.run(['c++', '-std=c++17', '-O2',
                            str(ROOT / 'cores/fes-pcxt/sim/memory_model_tb.cpp'),
                            '-o', str(model_test)], check=True)
            subprocess.run([str(model_test), 'clean'], check=True)
            for overlap in ('nop', 'deselected'):
                result = subprocess.run([str(model_test), overlap], capture_output=True, text=True)
                if result.returncode != 1 or 'READ return has output data contention' not in result.stderr:
                    raise RuntimeError(f'CAS-2 overlap was not rejected: {overlap}: {result}')
            print('PASS memory model: two late-driver overlaps rejected', flush=True)
        if suite in ('video', 'video-mda'):
            sources = VIDEO_SOURCES + ('cores/fes-pcxt/sim/video_tb.cpp',)
        if suite == 'display':
            sources = SOURCES[:3] + DISPLAY_SOURCES + ('cores/fes-pcxt/sim/display_tb.cpp',)
        if suite == 'clock-bridge':
            sources = ('cores/fes-pcxt/rtl/pcxt_machine.sv', 'cores/fes-pcxt/sim/clock_bridge_tb.cpp')
        if suite in ('speaker100', 'speaker50'):
            sources = ('cores/fes-pcxt/rtl/pcxt_speaker.sv', 'cores/fes-pcxt/sim/speaker_tb.cpp')
        if suite == 'keyboard':
            sources = ('cores/fes-pcxt/rtl/pcxt_xt_keyboard.sv', 'cores/fes-pcxt/sim/keyboard_tb.cpp')
        if suite in ('motherboard', 'dma', 'machine'):
            for name in CHIP_VENDORS:
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
            for name in CHIP_VENDORS:
                verify_vendor(name)
            sources = (SOURCES[:3] + DISPLAY_SOURCES + CHIPSET_SOURCES[:-1] +
                       tuple(s for s in CPU_SOURCES if s.endswith('.sv') and not s.endswith('/cpu_top.sv')) +
                       ('cores/fes-pcxt/rtl/pcxt_machine.sv', 'cores/fes-pcxt/sim/display_machine_top.sv', 'cores/fes-pcxt/sim/display_machine_tb.cpp'))
        if suite == 'memory-machine':
            sources = sources[:-1] + MEMORY_SOURCES + ('cores/fes-pcxt/sim/memory_machine_top.sv', 'cores/fes-pcxt/sim/memory_machine_tb.cpp')
        if suite == 'video-machine':
            sources = sources[:-1] + VIDEO_SOURCES[:-1] + ('cores/fes-pcxt/sim/video_machine_top.sv', 'cores/fes-pcxt/sim/video_machine_tb.cpp')
        if suite in ('cpu', 'cpu-timing', 'machine', 'display-machine', 'video-machine', 'memory-machine'):
            cpu_inputs(build, 'display' if suite in ('display-machine', 'video-machine', 'memory-machine') else 'motherboard' if suite == 'machine' else 'eu_timing' if suite == 'cpu-timing' else 'diagnostic')
        if suite in ('system', 'system-mda', 'system-board50', 'system-board50-mda'):
            for name in CHIP_VENDORS:
                verify_vendor(name)
            board_inputs(build)
            sources = SYSTEM_SOURCES + (MEMORY_SOURCES[-1], 'cores/fes-pcxt/sim/system_top.sv', 'cores/fes-pcxt/sim/system_tb.cpp')
        top = 'pcxt_cpu_clock_bridge' if suite == 'clock-bridge' else 'pcxt_speaker' if suite in ('speaker100', 'speaker50') else 'cpu_top' if suite == 'cpu-timing' else 'system_top' if suite in ('system', 'system-mda', 'system-board50', 'system-board50-mda') else 'video_top' if suite in ('video', 'video-mda') else 'pcxt_xt_keyboard' if suite == 'keyboard' else 'motherboard_top' if suite == 'dma' else suite.replace('-', '_') + '_top'
        subprocess.run([args.verilator, '--cc', '--exe', '--build', '-Wall',
                        *(['--timescale-override', '1ns/1ps',
                           str(ROOT / 'cores/fes-pcxt/sim/cpu.vlt')] if suite != 'isa' else []),
                        *([str(ROOT / 'cores/fes-pcxt/sim/chipset.vlt')] if suite in ('motherboard', 'dma', 'machine', 'display-machine', 'video-machine', 'memory-machine', 'system', 'system-mda', 'system-board50', 'system-board50-mda') else []),
                        *('-I' + str(vendor_dir(name)) for name in CHIP_VENDORS),
                        *(["-GMDA=1'b1"] if suite in ('video-mda', 'system-mda', 'system-board50-mda') else []),
                        *(["-GSCANLINES=1'b1"] if suite in ('system-mda', 'system-board50-mda') else []),
                        *(["-GHALF_RATE_BOARD=1'b1"] if suite in ('system-board50', 'system-board50-mda') else []),
                        '--top-module', top, '--Mdir', str(build),
                        '-I' + str(ROOT / 'cores/fes-common/generated'),
                        '-I' + str(build),
                        *(['-GCLOCK_HZ=' + ('50000000' if suite == 'speaker50' else '100000000')] if suite in ('speaker100', 'speaker50') else []),
                        '-CFLAGS', '-std=c++17 -O2' + (' -DRATE=' + ('50000000' if suite == 'speaker50' else '100000000') if suite in ('speaker100', 'speaker50') else '') + (' -DHALF_RATE_BOARD=1' if suite in ('system-board50', 'system-board50-mda') else ''),
                        *(['-I' + str(ROOT / 'cores/fes-pcxt/sim')] if suite in ('memory', 'memory-machine', 'system', 'system-mda', 'system-board50', 'system-board50-mda') else []),
                        *[str(ROOT / source) for source in sources]], cwd=ROOT, check=True)
        for delay in (0, 1, 2) if suite in ('memory', 'memory-machine', 'system', 'system-mda', 'system-board50', 'system-board50-mda') else (None,):
            subprocess.run([str(build / ('V' + top)),
                            *(['mda'] if suite == 'video-mda' else []),
                            *([str(build / 'diagnostic.bin')] if suite in ('cpu', 'cpu-timing', 'machine', 'display-machine', 'video-machine', 'memory-machine') else []),
                            *(['timing'] if suite == 'cpu-timing' else []),
                            *([str(delay)] if delay is not None else []),
                            *(['mda'] if suite in ('system-mda', 'system-board50-mda') else [])],
                           cwd=build, check=True)


if __name__ == '__main__':
    main()
