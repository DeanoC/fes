#!/usr/bin/env python3
"""Simulate the pc386 machine through phases M1-M3.

Host-only checks; no kit, package or routed result. See
docs/pc386-machine-design.md, "Verification" and phases M1-M3.

  devices  cores/fes-pc386/sim/devices_tb.cpp drives the AT core's I/O port
           directly: PIC cascade and acknowledge, 8254 rate and read-back,
           DMA registers, pages and a transfer, 8042 commands, A20 and reset,
           RTC update and interrupts, the ATA command set, and the I/O timing
           classes. Needs only Verilator.
  audio    cores/fes-pc386/sim/audio_tb.cpp checks the speaker, Sound Blaster
           DSP commands and DMA, OPL2 timers and the 48 kHz mixer.
  opl2     cores/fes-pc386/sim/opl2_tb.cpp compares the OPL2 sample for sample
           with Nuked-OPL3 on random programs and captured sound logs.
  benches  cores/fes-pc386/sim/presenter_tb.cpp and plane_tb.cpp: the presenter
           against its model, and the shared display plane through the port 2
           bridge and arbiter. Needs only Verilator; under a minute.
  boot     cores/fes-pc386/sim/machine_tb.cpp runs the Bochs BIOS, the LGPL
           VGABios and FreeDOS on the machine (pc386_machine: the AT core and
           the RTL VGA) with the Bochs oracle as the CPU, types a command at
           the prompt, checks the answer read from VRAM and the I/O completion
           times, and replays the boot's VGA trace against Bochs's VGA.
  sound    boots FreeDOS and runs SOUND.COM; checks the speaker and DSP tones,
           OPL2 output and DSP interrupts.
  game     starts Wolfenstein 3D or Ultima VI from user-supplied eXoDOS files
           and checks the game's OPL2 output. Supply the games outside the repo.
  vga      runs the benches, boots
           cores/fes-pc386/sim/vga_test.asm (VGA BIOS text, mode 13h, mode Y,
           mode 12h), checks every marked picture's 720p frame from the
           presenter and the plane against the presenter model, and replays
           the VGA trace against Bochs's VGA (cores/fes-pc386/sim/vga_replay_tb.cpp),
           which must match frames of every marked picture.

boot and vga need the oracle build, the firmware build, the FreeDOS fixture
and NASM; --download fetches what is missing. Writes
build/sim/fes-pc386/<suite>/report.json; vga also writes the marked frames.

Examples:
  python3 scripts/sim_fes_pc386.py --suite devices
  python3 scripts/sim_fes_pc386.py --suite opl2
  python3 scripts/sim_fes_pc386.py --suite boot --download
  python3 scripts/sim_fes_pc386.py --suite sound --download
  python3 scripts/sim_fes_pc386.py --suite game --game ultima6
  python3 scripts/sim_fes_pc386.py --suite vga
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys
import tempfile
import zipfile

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_pc386_firmware as firmware
from scripts import build_pc386_freedos_image as freedos
from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_test386 as test386

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / 'cores/fes-pc386'
COMMON = ROOT / 'cores/fes-common'
OUTPUT = ROOT / 'build/sim/fes-pc386'
TOP = 'pc386_at_core'
OPL2_VENDOR = CORE / 'vendor/opl2_fpga'
NUKED = CORE / 'vendor/nuked-opl3'
AT_RTL = (
    COMMON / 'vendor/kf8259/KF8259.sv',
    COMMON / 'vendor/kf8259/KF8259_Bus_Control_Logic.sv',
    COMMON / 'vendor/kf8259/KF8259_Control_Logic.sv',
    COMMON / 'vendor/kf8259/KF8259_In_Service.sv',
    COMMON / 'vendor/kf8259/KF8259_Interrupt_Request.sv',
    COMMON / 'vendor/kf8259/KF8259_Priority_Resolver.sv',
    COMMON / 'vendor/kf8253/KF8253.sv',
    COMMON / 'vendor/kf8253/KF8253_Control_Logic.sv',
    COMMON / 'vendor/kf8253/KF8253_Counter.sv',
    COMMON / 'rtl/fes_dma8237.sv',
    CORE / 'rtl/pc386_rate.sv',
    CORE / 'rtl/pc386_io.sv',
    CORE / 'rtl/pc386_pic.sv',
    CORE / 'rtl/pc386_sysctl.sv',
    CORE / 'rtl/pc386_kbc.sv',
    CORE / 'rtl/pc386_rtc.sv',
    CORE / 'rtl/pc386_ata.sv',
    CORE / 'rtl/pc386_at_core.sv',
)
OPL2_RTL = (
    OPL2_VENDOR / 'opl2_pkg.sv',
    *(OPL2_VENDOR / f'{name}.sv' for name in (
        'pipeline_sr', 'mem_simple_dual_port', 'mem_simple_dual_port_async_read', 'mem_single_bank',
        'mem_single_bank_reset', 'opl2_log_sine_lut', 'opl2_exp_lut', 'ksl_add_rom', 'tremolo', 'vibrato',
        'calc_phase_inc', 'calc_envelope_shift', 'calc_rhythm_phase', 'envelope_generator', 'phase_generator',
        'operator', 'control_operators', 'dac_prep', 'channels')),
    CORE / 'rtl/pc386_rate.sv',
    CORE / 'rtl/pc386_opl2.sv',
)
AUDIO_RTL = (*OPL2_RTL, CORE / 'rtl/pc386_sb.sv', CORE / 'rtl/pc386_mixer.sv', CORE / 'rtl/pc386_audio.sv')
OPL2_SEEDS = (1, 2, 3)
OPL2_SAMPLES = 20000
# The machine without the CPU: the AT core, the VGA, the presenter, port 2
# and the shared display plane.
MACHINE_RTL = AT_RTL + (
    CORE / 'rtl/pc386_ram.sv',
    CORE / 'rtl/pc386_vga.sv',
    CORE / 'rtl/pc386_vga_scanout.sv',
    CORE / 'rtl/pc386_presenter.sv',
    CORE / 'rtl/pc386_plane_bridge.sv',
    CORE / 'rtl/pc386_port2.sv',
    CORE / 'rtl/pc386_machine.sv',
    COMMON / 'rtl/fes_display_plane.v',
    COMMON / 'rtl/fes_display_cdc.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_control.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_video.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_reader.v',
)
# The M4 diagnostic machine: the machine, its audio and the bus master that
# stands in for the CPU (pc386_diag.sv).
DIAG_RTL = (*dict.fromkeys(MACHINE_RTL + AUDIO_RTL), CORE / 'rtl/pc386_bus_master.sv', CORE / 'rtl/pc386_diag.sv')
DIAG_CLOCK_HZ = 61_440_000
DIAG_PROGRAMS = ('smoke', 'mode13', 'sound', 'ata')
# I/O completion, request clock to done clock: each device cycle's class time
# (isa8 72 clocks, isa16 36 at 100 MHz) plus one clock to take the request and
# one to report it. 1F0h with a dword is one word cycle and two byte cycles
# (1F2h, 1F3h), as the AT's bus sizing does. A VGA port byte is presented a
# clock after the request; a write is in the VGA FIFO a clock later and the
# decoder leaves a clock between bytes. A read waits until the FIFO has
# drained and takes two clocks more, so it takes at least VGA_READ_MINIMUM
# and at most VGA_READ_BOUND.
ISA8, ISA16, OVERHEAD = 72, 36, 2
EXPECTED_IO = {
    ('isa8', 1): ISA8 + OVERHEAD, ('isa8', 2): 2 * ISA8 + OVERHEAD, ('isa8', 4): 4 * ISA8 + OVERHEAD,
    ('isa16', 1): ISA16 + OVERHEAD, ('isa16', 2): ISA16 + OVERHEAD, ('isa16', 4): ISA16 + 2 * ISA8 + OVERHEAD,
    ('vga-out', 1): 4, ('vga-out', 2): 7, ('vga-out', 4): 13,
}
VGA_READ_MINIMUM = {1: 6, 2: 11, 4: 21}
VGA_READ_BOUND = 200
EXPECTED_INTA = 2 * ISA8 + OVERHEAD
VGA_RTL = (
    CORE / 'rtl/pc386_rate.sv',
    CORE / 'rtl/pc386_io.sv',
    CORE / 'rtl/pc386_ram.sv',
    CORE / 'rtl/pc386_vga.sv',
    CORE / 'rtl/pc386_vga_scanout.sv',
    CORE / 'sim/vga_replay_top.sv',
)
PRESENTER_RTL = (CORE / 'rtl/pc386_ram.sv', CORE / 'rtl/pc386_presenter.sv')
CAPTURE_RTL = (
    CORE / 'rtl/pc386_rate.sv', CORE / 'rtl/pc386_ram.sv', CORE / 'rtl/pc386_vga_scanout.sv',
    CORE / 'sim/vga_capture_top.sv',
)
PLANE_RTL = PRESENTER_RTL + (
    CORE / 'rtl/pc386_plane_bridge.sv',
    CORE / 'rtl/pc386_port2.sv',
    COMMON / 'rtl/fes_display_plane.v',
    COMMON / 'rtl/fes_display_cdc.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_control.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_video.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_reader.v',
    CORE / 'sim/plane_top.sv',
)
VGA_SNAPS = ('text', 'text-font', 'text-bright', 'mode13', 'mode13-cycle', 'modey', 'modey-flip',
             'modey-split', 'modey-fade', 'mode12', 'final')


class SimError(RuntimeError):
    pass


def verilate(verilator: str, name: str, top: str, rtl: tuple[Path, ...], testbench: Path | None,
             linked: list[Path], jobs: int, parameters: dict[str, int] | None = None,
             include: tuple[Path, ...] = (), flags: tuple[str, ...] = ()) -> Path:
    output = OUTPUT / name / ('obj' if testbench else f'obj-{top}')
    output.mkdir(parents=True, exist_ok=True)
    includes = ' '.join(f'-I{path}' for path in (COMMON / 'sim/i386', CORE / 'sim', NUKED, *include))
    command = [verilator, '--cc', *(['--exe'] if testbench else []), '--build', '-j', str(jobs), '-O3', '--top-module', top,
               '-Wall', '-Wno-UNUSEDSIGNAL', '-Wno-DECLFILENAME', *flags,
               str(CORE / 'sim/pc386.vlt'), str(CORE / 'sim/pc386_audio.vlt'),
               f'-I{COMMON / "vendor/kf8259"}', f'-I{COMMON / "vendor/kf8253"}', f'-I{COMMON / "generated"}',
               *(f'-G{key}={value}' for key, value in (parameters or {}).items()),
               '--Mdir', str(output), '-MAKEFLAGS', 'OPT_FAST=-O3',
               '-CFLAGS', f'-std=c++17 -O2 {includes}', '-LDFLAGS', '-pthread',
               *map(str, rtl), *([str(testbench)] if testbench else []), *map(str, linked)]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise SimError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + top if testbench else f'V{top}__ALL.a')


def parse_boot(text: str) -> dict:
    report = {'post': [], 'debug': [], 'panic': [], 'io': {}, 'inta': {}, 'screen': [], 'end': None,
              'prompt_cycle': None, 'cpu_resets': 0, 'snaps': {}, 'present': {}, 'vga': {}, 'marks': {},
              'opl2': None, 'opl2_mismatch': None, 'audio_samples': None, 'sb': None}
    for line in text.splitlines():
        kind, _, rest = line.partition(' ')
        if kind == 'POST':
            code, cycle = rest.split()
            report['post'].append({'code': code, 'cycle': int(cycle)})
        elif kind == 'DEBUG':
            port, _, message = rest.partition(' ')
            report['debug'].append({'port': port, 'text': message})
        elif kind == 'PANIC':
            report['panic'].append(rest)
        elif kind == 'IO':
            klass, size, cycles, count = rest.split()
            report['io'].setdefault(f'{klass} {size}', {})[cycles] = int(count)
        elif kind == 'INTA':
            cycles, count = rest.split()
            report['inta'][cycles] = int(count)
        elif kind == 'SCREEN':
            report['screen'].append(rest.partition('|')[2][:-1])
        elif kind == 'PROMPT':
            report['prompt_cycle'] = int(rest)
        elif kind == 'CPU-RESET':
            report['cpu_resets'] += 1
        elif kind == 'SNAP':
            name, width, height, *file = rest.split()
            report['snaps'][name] = {'width': int(width), 'height': int(height),
                                     'file': file[0] if file else None}
        elif kind == 'PRESENT':
            name, *fields = rest.split()
            report['present'][name] = {'file': fields[0] if len(fields) > 1 else None, 'mismatches': int(fields[-1])}
        elif kind == 'VGA':
            name, value = rest.split()
            report['vga'][name] = int(value)
        elif kind == 'MARK':
            sample, _, name = rest.partition(' ')
            report['marks'][name] = int(sample)
        elif kind == 'OPL2':
            report['opl2'] = {key: int(value) for key, value in (field.split('=') for field in rest.split())}
        elif kind == 'OPL2-MISMATCH':
            report['opl2_mismatch'] = rest
        elif kind == 'AUDIO':
            report['audio_samples'] = int(rest.partition('=')[2])
        elif kind == 'SB':
            report['sb'] = {key: int(value) for key, value in (field.split('=') for field in rest.split())}
        elif kind == 'END':
            fields = rest.split()
            report['end'] = {'reason': fields[0], **{k: float(v) if '.' in v else int(v)
                                                      for k, v in (field.split('=') for field in fields[1:])}}
    return report


def timing_failures(report: dict) -> list[str]:
    failures = []
    for key, histogram in report['io'].items():
        klass, size = key.split()
        if klass == 'vga-in':
            times = [int(t) for t in histogram]
            if min(times) < VGA_READ_MINIMUM[int(size)] or max(times) > VGA_READ_BOUND:
                failures.append(f'IO {key}: {min(times)}-{max(times)} clocks, expected '
                                f'{VGA_READ_MINIMUM[int(size)]}-{VGA_READ_BOUND}')
            continue
        expected = EXPECTED_IO.get((klass, int(size)))
        if expected is None or set(histogram) != {str(expected)}:
            failures.append(f'IO {key}: {histogram}, expected every access at {expected} clocks')
    if report['inta'] and set(report['inta']) != {str(EXPECTED_INTA)}:
        failures.append(f'INTA: {report["inta"]}, expected {EXPECTED_INTA} clocks')
    return failures


def machine_testbench(args, linked: list[Path]) -> Path:
    return verilate_with_audio(args, 'machine', CORE / 'sim/machine_tb.cpp', linked,
                               top='pc386_machine', rtl=MACHINE_RTL)


def verilate_with_audio(args, name: str, testbench: Path, linked: list[Path],
                        parameters: dict[str, int] | None = None, top: str = TOP,
                        rtl: tuple[Path, ...] = AT_RTL) -> Path:
    """Build the audio model and link it beside the AT core in a testbench."""
    audio = verilate(args.verilator, name, 'pc386_audio', AUDIO_RTL, None, [], args.jobs)
    return verilate(args.verilator, name, top, rtl, testbench,
                    [*linked, audio, NUKED / 'opl3.c'], args.jobs, parameters, (audio.parent,))


def run_machine(testbench: Path, suite: str, arguments: list[str], images: dict) -> dict:
    directory = OUTPUT / suite
    directory.mkdir(parents=True, exist_ok=True)
    command = [str(testbench), '--bios', str(images['bios']), '--vgabios', str(images['vgabios']), *arguments]
    result = subprocess.run(command, cwd=directory, capture_output=True, text=True)
    (directory / 'machine.log').write_text(result.stdout + result.stderr)
    report = parse_boot(result.stdout)
    report['command'] = [str(Path(arg).relative_to(ROOT)) if arg.startswith(str(ROOT)) else arg for arg in command]
    report['exit'] = result.returncode
    report['failures'] = timing_failures(report)
    if result.returncode != 0:
        report['failures'].append(f'machine_tb exited {result.returncode}: {(report["end"] or {}).get("reason")}'
                                  f' {result.stderr.strip()}')
    if report['panic']:
        report['failures'].append(f'BIOS panic ports written: {report["panic"]}')
    opl2 = report['opl2'] or {}
    if opl2.get('exact') != opl2.get('samples') or report['opl2_mismatch']:
        report['failures'].append(f'OPL2 against Nuked-OPL3: {opl2} {report["opl2_mismatch"] or ""}')
    for counter in ('scanout_underflows', 'capture_drops', 'present_underflows', 'plane_underflows'):
        if report['vga'].get(counter):
            report['failures'].append(f'VGA {counter}: {report["vga"][counter]}')
    return report


def vga_reference_objects(args) -> list[Path]:
    """Bochs's VGA and GUI renderer from the oracle's configured checkout, and the adapter."""
    bochs = oracle.build_bochs(oracle.bochs_source(args.download), args.jobs)
    output = OUTPUT / 'vga-reference'
    output.mkdir(parents=True, exist_ok=True)
    flags = ['-std=c++17', '-O2', f'-I{bochs}', f'-I{bochs}/instrument/fes', f'-I{bochs}/gui', f'-I{bochs}/iodev',
             f'-I{CORE / "sim"}']
    sources = {'vgacore.o': bochs / 'iodev/display/vgacore.cc', 'gui.o': bochs / 'gui/gui.cc',
               'vga_reference.o': CORE / 'sim/vga_reference.cpp'}
    inputs = [CORE / 'sim/vga_reference.h', CORE / 'sim/pc386_presenter_model.h']
    objects = []
    for name, source in sources.items():
        target = output / name
        newest = max(path.stat().st_mtime for path in (source, *inputs))
        if not target.is_file() or target.stat().st_mtime < newest:
            log = output / (name + '.log')
            with log.open('w') as stream:
                result = subprocess.run([oracle._compiler(), *flags, '-c', str(source), '-o', str(target)],
                                        cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
            if result.returncode:
                raise SimError(f'compiling {source.name} failed; see {log}')
        objects.append(target)
    return objects + [bochs / 'fes-paramtree.o']


def run_replay(args, suite: str, trace: Path) -> dict:
    testbench = verilate(args.verilator, 'vga-replay', 'vga_replay_top', VGA_RTL, CORE / 'sim/vga_replay_tb.cpp',
                         vga_reference_objects(args), args.jobs)
    frames = OUTPUT / suite / 'replay-frames'
    shutil.rmtree(frames, ignore_errors=True)
    frames.mkdir(parents=True)
    result = subprocess.run([str(testbench), '--trace', str(trace), '--frames', str(frames)],
                            cwd=OUTPUT / suite, capture_output=True, text=True)
    (OUTPUT / suite / 'replay.log').write_text(result.stdout + result.stderr)
    report = {'exit': result.returncode, 'counts': {}, 'mismatches': [], 'summary': None}
    for line in result.stdout.splitlines():
        kind, _, rest = line.partition(' ')
        if kind == 'REPLAY':
            name, value = rest.split()
            report['counts'][name] = int(value)
        elif kind in ('FRAME', 'READ-MISMATCH', 'VRAM-MISMATCH'):
            report['mismatches'].append(line)
        elif kind == 'SUMMARY':
            report['summary'] = rest
    report['failures'] = [] if result.returncode == 0 else [
        f'trace replay: {report["summary"] or result.stderr.strip()}']
    if not report['counts'].get('frames-matched'):
        report['failures'].append('trace replay compared no frame')
    return report


def run_boot(args, linked: list[Path]) -> dict:
    images = firmware.images(args.download, args.jobs)
    disk = freedos.build(args.download, args.jobs) if args.rebuild_image else freedos.image(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    trace = OUTPUT / 'boot/vga.trace'
    print('Booting FreeDOS in simulation (this takes minutes)', flush=True)
    report = run_machine(testbench, 'boot', ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                             '--type', args.type, '--expect', args.expect, '--trace', str(trace)],
                         images)
    print('Replaying the boot\'s VGA trace against Bochs\'s VGA', flush=True)
    report['replay'] = run_replay(args, 'boot', trace)
    report['failures'] += report['replay']['failures']
    return report


def vga_test_disk(args) -> Path:
    """The VGA test program on a 1 MiB disk."""
    nasm = test386.nasm(args.download, args.jobs)
    directory = OUTPUT / 'vga'
    directory.mkdir(parents=True, exist_ok=True)
    binary = directory / 'vga_test.bin'
    result = subprocess.run([str(nasm), '-f', 'bin', '-o', str(binary), str(CORE / 'sim/vga_test.asm')],
                            capture_output=True, text=True)
    if result.returncode:
        raise SimError('nasm failed on vga_test.asm\n' + result.stderr)
    data = binary.read_bytes()
    disk = directory / 'vga_test.img'
    disk.write_bytes(data + bytes((1 << 20) - len(data)))
    return disk


def run_bench(args, name: str, top: str, rtl: tuple[Path, ...], testbench: Path) -> dict:
    """A PASS/FAIL bench without the oracle."""
    binary = verilate(args.verilator, name, top, rtl, testbench, [], args.jobs)
    result = subprocess.run([str(binary)], cwd=OUTPUT / name, capture_output=True, text=True)
    (OUTPUT / name / f'{name}.log').write_text(result.stdout + result.stderr)
    checks = [line for line in result.stdout.splitlines() if line.startswith(('PASS', 'FAIL'))]
    failures = [line for line in checks if line.startswith('FAIL')]
    if result.returncode and not failures:
        failures.append(f'{testbench.name} exited {result.returncode}: {result.stderr.strip()}')
    return {'checks': checks, 'failures': failures}


def run_benches(args) -> dict:
    benches = {'presenter': run_bench(args, 'presenter', 'pc386_presenter', PRESENTER_RTL, CORE / 'sim/presenter_tb.cpp'),
               'capture': run_bench(args, 'capture', 'vga_capture_top', CAPTURE_RTL, CORE / 'sim/capture_tb.cpp'),
               'plane': run_bench(args, 'plane', 'plane_top', PLANE_RTL, CORE / 'sim/plane_tb.cpp')}
    return {'checks': [line for bench in benches.values() for line in bench['checks']],
            'failures': [line for bench in benches.values() for line in bench['failures']], 'benches': benches}


def run_diag(args) -> dict:
    """The M4 diagnostic programs on pc386_diag through the command ring.
    sound runs shortened; vga-trace runs when --trace names a VGA trace."""
    from scripts import pc386_diag
    parameters = pc386_diag.rates(DIAG_CLOCK_HZ)
    # The OPL2 import declares a timescale and the KF imports do not.
    testbench = verilate(args.verilator, 'diag', 'pc386_diag', DIAG_RTL, CORE / 'sim/diag_tb.cpp', [], args.jobs,
                         parameters, flags=('-Wno-TIMESCALEMOD',))
    report = {'clock_hz': DIAG_CLOCK_HZ, 'programs': {}, 'checks': [], 'failures': []}
    programs = list(DIAG_PROGRAMS) + (['vga-trace'] if args.trace else [])
    for name in programs:
        if name == 'sound':
            program = pc386_diag.sound(DIAG_CLOCK_HZ, speaker_seconds=0.01, auto_seconds=0.004, block=64)
        else:
            program = pc386_diag.build_program(name, DIAG_CLOCK_HZ, trace=Path(args.trace) if args.trace else None)
        directory = OUTPUT / 'diag' / name
        directory.mkdir(parents=True, exist_ok=True)
        (directory / 'program.p386').write_bytes(program.encode())
        results = directory / 'results.r386'
        limit = args.limit_seconds_given or (60 if name == 'vga-trace' else 4)
        run = subprocess.run([str(testbench), '--program', str(directory / 'program.p386'), '--results', str(results),
                              '--limit-seconds', str(limit)], cwd=directory, capture_output=True, text=True)
        (directory / 'diag.log').write_text(run.stdout + run.stderr)
        if run.returncode:
            report['failures'].append(f'{name}: diag_tb exited {run.returncode}: {run.stderr.strip()[-400:]}')
            continue
        decoded = pc386_diag.decode_results(results.read_bytes())
        checked = pc386_diag.check(program, decoded)
        checked['failures'] += pc386_diag.program_checks(name, program, decoded)
        frames = directory / 'frames'
        frames.mkdir(exist_ok=True)
        for n, frame in enumerate(decoded.frames):
            pc386_diag.write_frame_ppm(frame, frames / f'{n:02d}.ppm')
        report['programs'][name] = checked
        report['checks'].append(f'{name}: {checked["commands"]} commands, {checked["seconds"]} simulated s, '
                                f'{len(decoded.frames)} frames')
        report['failures'] += [f'{name}: {failure}' for failure in checked['failures']]
    return report


def run_vga(args, linked: list[Path]) -> dict:
    print('Presenter and port 2 benches', flush=True)
    benches = run_benches(args)['benches']
    for bench in benches.values():
        print('\n'.join(bench['checks']), flush=True)
    images = firmware.images(args.download, args.jobs)
    disk = vga_test_disk(args)
    testbench = machine_testbench(args, linked)
    frames = OUTPUT / 'vga/frames'
    shutil.rmtree(frames, ignore_errors=True)
    frames.mkdir(parents=True)
    trace = OUTPUT / 'vga/vga.trace'
    print('Running the VGA test disk (this takes minutes)', flush=True)
    report = run_machine(testbench, 'vga', ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                            '--until-debug', 'DONE', '--trace', str(trace), '--frames', str(frames)],
                         images)
    for name in VGA_SNAPS:
        if name not in report['snaps']:
            report['failures'].append(f'no capture at marker {name}')
        elif name not in report['present']:
            report['failures'].append(f'no 720p frame at marker {name}')
        elif report['present'][name]['mismatches']:
            report['failures'].append(f'720p frame at {name}: {report["present"][name]["mismatches"]} pixels '
                                      'differ from the presenter model')
    print('Replaying the VGA trace against Bochs\'s VGA', flush=True)
    report['replay'] = run_replay(args, 'vga', trace)
    report['failures'] += report['replay']['failures']
    # The replay must have compared, and matched, frames of every picture.
    for name in VGA_SNAPS:
        if not report['replay']['counts'].get(f'frames-matched-at-{name}'):
            report['failures'].append(f'trace replay matched no frame of {name}')
    report['benches'] = benches
    for bench in benches.values():
        report['failures'] += bench['failures']
    return report


def run_devices(args) -> dict:
    # A fast RTC (one guest second is 2^19 clocks) keeps the update test short.
    testbench = verilate(args.verilator, 'devices', 'pc386_at_core', AT_RTL, CORE / 'sim/devices_tb.cpp', [],
                         args.jobs, {'RTC_N': 1, 'RTC_D': 16})
    result = subprocess.run([str(testbench)], cwd=OUTPUT / 'devices', capture_output=True, text=True)
    (OUTPUT / 'devices/devices.log').write_text(result.stdout + result.stderr)
    checks = [line for line in result.stdout.splitlines() if line.startswith(('PASS', 'FAIL'))]
    failures = [line for line in checks if line.startswith('FAIL')]
    if result.returncode and not failures:
        failures.append(f'devices_tb exited {result.returncode}: {result.stderr.strip()}')
    return {'checks': checks, 'failures': failures, 'exit': result.returncode}


def run_checks(command: list[str], cwd: Path, log: Path) -> tuple[list[str], list[str], str]:
    """Runs a bench that prints PASS/FAIL lines; returns (checks, failures, stdout)."""
    cwd.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(command, cwd=cwd, capture_output=True, text=True)
    with log.open('a') as stream:
        stream.write(f'$ {" ".join(command)}\n{result.stdout}{result.stderr}')
    checks = [line for line in result.stdout.splitlines() if line.startswith(('PASS', 'FAIL'))]
    failures = [line for line in checks if line.startswith('FAIL')]
    if result.returncode and not failures:
        failures.append(f'{Path(command[0]).name} exited {result.returncode}: {result.stderr.strip()}')
    return checks, failures, result.stdout


def run_audio(args) -> dict:
    testbench = verilate_with_audio(args, 'audio', CORE / 'sim/audio_tb.cpp', [])
    log = OUTPUT / 'audio/audio.log'
    log.write_text('')
    checks, failures, _ = run_checks([str(testbench)], OUTPUT / 'audio', log)
    return {'checks': checks, 'failures': failures}


def run_opl2(args) -> dict:
    testbench = verilate(args.verilator, 'opl2', 'pc386_opl2', OPL2_RTL, CORE / 'sim/opl2_tb.cpp',
                         [NUKED / 'opl3.c'], args.jobs)
    log = OUTPUT / 'opl2/opl2.log'
    log.write_text('')
    report = {'checks': [], 'failures': [], 'runs': []}
    runs = [['--random', str(seed), str(OPL2_SAMPLES)] for seed in OPL2_SEEDS]
    runs += [['--log', str(path)] for path in sorted((CORE / 'sim/fixtures').glob('*-opl2.log'))]
    for arguments in runs:
        checks, failures, stdout = run_checks([str(testbench), *arguments], OUTPUT / 'opl2', log)
        stats = re.search(r'OPL2 samples=(\d+) exact=(\d+) nonzero=(\d+) writes=(\d+)', stdout)
        report['runs'].append({'arguments': [str(Path(arg).relative_to(ROOT)) if arg.startswith(str(ROOT)) else arg
                                             for arg in arguments],
                               **({key: int(value) for key, value in zip(('samples', 'exact', 'nonzero', 'writes'),
                                                                          stats.groups())} if stats else {})})
        report['checks'] += checks
        report['failures'] += failures
    return report


SOUND_SECTIONS = ('start', 'speaker', 'cpu-speaker', 'opl2', 'sb-single', 'sb-auto', 'end')
SOUND_TONES = {'speaker': 880.0, 'sb-single': 1000.0, 'sb-auto': 1000.0}
SOUND_SCREEN = ('ADLIB OK', 'SB DSP 2.01', 'SB IRQS 6', 'SOUND DONE')


def sound_disk(args) -> Path:
    """The FreeDOS fixture with SOUND.COM added."""
    work = OUTPUT / 'sound'
    work.mkdir(parents=True, exist_ok=True)
    nasm = test386.nasm(args.download, args.jobs)
    program = work / 'SOUND.COM'
    result = subprocess.run([str(nasm), '-f', 'bin', '-o', str(program), str(CORE / 'sim/sound.asm')],
                            capture_output=True, text=True)
    if result.returncode:
        raise SimError('NASM failed on sound.asm:\n' + result.stderr)
    disk = work / 'disk.img'
    shutil.copyfile(freedos.image(args.download, args.jobs), disk)
    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', TZ='UTC')
    result = subprocess.run(['mcopy', '-m', '-i', f'{disk}@@{freedos.PARTITION_START * 512}', str(program),
                             '::SOUND.COM'], capture_output=True, text=True, env=env)
    if result.returncode:
        raise SimError('mcopy failed:\n' + result.stdout + result.stderr)
    return disk


def read_wav(path: Path) -> list[int]:
    data = path.read_bytes()
    if data[:4] != b'RIFF' or data[36:40] != b'data':
        raise SimError(f'{path} is not the bench WAV')
    count = struct.unpack_from('<I', data, 40)[0] // 2
    return list(struct.unpack_from(f'<{count}h', data, 44))


def tone(samples: list[int]) -> dict:
    """Frequency from rising zero crossings, and RMS and peak, of 48 kHz output."""
    rises = [n for n in range(1, len(samples)) if samples[n - 1] < 0 <= samples[n]]
    hz = 48000 * (len(rises) - 1) / (rises[-1] - rises[0]) if len(rises) > 2 else 0.0
    rms = math.sqrt(sum(value * value for value in samples) / len(samples)) if samples else 0.0
    return {'hz': round(hz, 2), 'rms': round(rms, 1), 'peak': max((abs(value) for value in samples), default=0),
            'samples': len(samples)}


def run_sound(args, linked: list[Path]) -> dict:
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    wav = OUTPUT / 'sound/sound.wav'
    opl2_log = OUTPUT / 'sound/sound-opl2.log'
    report = run_machine(testbench, 'sound', ['--disk', str(sound_disk(args)),
                                              '--limit-seconds', str(args.limit_seconds), '--type', args.type,
                                              '--expect', args.expect, '--wav', str(wav), '--opl2-log', str(opl2_log)],
                         images)
    screen = '\n'.join(report['screen'])
    for text in SOUND_SCREEN:
        if text not in screen:
            report['failures'].append(f'SOUND.COM did not print {text!r}')
    marks = report['marks']
    missing = [name for name in SOUND_SECTIONS if name not in marks]
    if missing:
        report['failures'].append(f'SOUND.COM sections missing: {missing}')
        return report
    samples = read_wav(wav)
    report['sections'] = {}
    for name, end in zip(SOUND_SECTIONS[1:-1], SOUND_SECTIONS[2:]):
        section = tone(samples[marks[name] + 480:marks[end]])
        report['sections'][name] = section
        expected = SOUND_TONES.get(name)
        if expected and abs(section['hz'] - expected) > expected * 0.005:
            report['failures'].append(f'{name}: {section["hz"]} Hz, expected {expected}')
        if section['rms'] < 500:
            report['failures'].append(f'{name}: silent (RMS {section["rms"]})')
    opl2 = report['opl2'] or {}
    if not opl2.get('writes') or not opl2.get('nonzero'):
        report['failures'].append(f'the OPL2 did not play: {opl2}')
    sb = report['sb'] or {}
    if sb.get('dma', 0) < 6000 or sb.get('irqs', 0) < 6:
        report['failures'].append(f'the DSP did not play SOUND.COM\'s 6000 DMA bytes with 6 IRQs: {sb}')
    if args.update_fixture:
        shutil.copyfile(opl2_log, CORE / 'sim/fixtures/sound-opl2.log')
    return report


GAMES_DIR = Path(os.environ.get('FES_PC386_GAMES',
                                '/mnt/emuhd/Games/PC DOS/Collections/eXoDOS 6.04 Full/eXoDOS/eXo/eXoDOS'))
GAMES = {
    'wolf3d': {'zip': 'Wolfenstein 3D (1992).zip', 'folder': 'Wolf3D/WOLF3D/WOLF3D/', 'directory': '',
               'shell': 'WOLF3D.EXE', 'type': '', 'limit': 24.0,
               'keys': [(4.0 + 1.5 * n, '|') for n in range(9)] + [(17.5 + 0.8 * n, '^ ') for n in range(8)]},
    'ultima6': {'zip': 'Ultima VI - The False Prophet (1990).zip', 'folder': 'ultima6/', 'directory': 'ULTIMA6',
                'type': 'cd ultima6|ultima6|', 'limit': 16.0, 'keys': []},
}


def game_disk(args, game: dict, work: Path) -> tuple[Path, dict]:
    """The FreeDOS fixture with the game in C:\\<directory>, built in work."""
    source = GAMES_DIR / game['zip']
    if not source.is_file():
        raise SimError(f'{source} not found; set FES_PC386_GAMES to the eXoDOS game folder')
    record = {'zip': str(source), 'sha256': hashlib.sha256(source.read_bytes()).hexdigest()}
    files = work / 'files'
    shutil.rmtree(files, ignore_errors=True)
    with zipfile.ZipFile(source) as archive:
        for item in archive.infolist():
            if item.is_dir() or not item.filename.startswith(game['folder']):
                continue
            target = files / item.filename[len(game['folder']):]
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(archive.read(item))
    record['files'] = sum(1 for path in files.rglob('*') if path.is_file())
    disk = work / 'disk.img'
    shutil.copyfile(freedos.image(args.download, args.jobs), disk)
    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', TZ='UTC')
    volume = f'{disk}@@{freedos.PARTITION_START * 512}'
    if game.get('shell'):
        (files / 'FDCONFIG.SYS').write_bytes(
            f'FILES=20\r\nBUFFERS=20\r\nSHELL=C:\\{game["shell"]}\r\n'.encode())
    sources = sorted(files.iterdir()) if not game['directory'] else [files]
    result = subprocess.run(['mcopy', '-s', '-m', '-o', '-i', volume, *map(str, sources), '::' + game['directory']],
                            capture_output=True, text=True, env=env)
    if result.returncode:
        raise SimError('mcopy failed:\n' + result.stdout + result.stderr)
    shutil.rmtree(files)
    return disk, record


def run_game(args, linked: list[Path]) -> dict:
    game = GAMES[args.game]
    work = Path(args.games_work) / args.game
    work.mkdir(parents=True, exist_ok=True)
    disk, record = game_disk(args, game, work)
    args.limit_seconds = args.limit_seconds_given or game['limit']
    args.type, args.expect = game['type'], ''
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    wav, opl2_log = work / f'{args.game}.wav', work / f'{args.game}-opl2.log'
    keys = [arg for seconds, text in game['keys'] for arg in ('--keys', f'{seconds}:{text}')]
    report = run_machine(testbench, f'game-{args.game}', ['--disk', str(disk), '--limit-seconds',
                                  str(args.limit_seconds), '--type', args.type, '--expect', args.expect,
                                  '--wav', str(wav), '--opl2-log', str(opl2_log), *keys], images)
    report['game'] = {'name': args.game, **record}
    report['work'] = str(work)
    report['failures'] = [failure for failure in report['failures'] if not failure.startswith('machine_tb exited 1')]
    opl2 = report['opl2'] or {}
    if not opl2.get('writes') or not opl2.get('nonzero'):
        report['failures'].append(f'the game did not play the OPL2: {opl2}')
    if wav.exists():
        report['audio'] = tone(read_wav(wav))
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--suite', choices=('devices', 'audio', 'opl2', 'benches', 'boot', 'sound', 'game', 'vga',
                                            'diag', 'all'), default='all', help='simulation suite to run')
    parser.add_argument('--trace', default=None, help='diag: also replay this VGA trace (from the vga suite)')
    parser.add_argument('--download', action='store_true', help='fetch Bochs, dev86, FreeDOS and NASM if missing')
    parser.add_argument('--rebuild-image', action='store_true', help='rebuild the FreeDOS fixture')
    parser.add_argument('--limit-seconds', type=float, default=None, help='simulated seconds before giving up')
    parser.add_argument('--game', choices=sorted(GAMES), default='wolf3d', help='game: which game to start')
    parser.add_argument('--games-work', default=str(Path(tempfile.gettempdir()) / 'fes-pc386-games'),
                        help='game: scratch directory outside the repository for the game disk and captures')
    parser.add_argument('--update-fixture', action='store_true',
                        help='sound: copy the captured OPL2 port log to sim/fixtures/sound-opl2.log')
    parser.add_argument('--type', default=None, help="keys typed at the prompt; '|' is Enter")
    parser.add_argument('--expect', default=None, help='text the screen must show at the end')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    args = parser.parse_args()
    limit_seconds, typed, expect = args.limit_seconds, args.type, args.expect
    args.limit_seconds_given = limit_seconds
    if shutil.which(args.verilator) is None:
        print(f'selected Verilator not found: {args.verilator}', file=sys.stderr)
        return 2
    failed = False
    try:
        suites = ('devices', 'audio', 'opl2', 'benches', 'diag', 'boot', 'vga', 'sound') if args.suite == 'all' else (args.suite,)
        for suite in suites:
            args.limit_seconds = limit_seconds or (14 if suite == 'sound' else 8 if suite == 'boot' else 12)
            args.type = typed or ('sound|' if suite == 'sound' else 'dir|')
            args.expect = expect or ('SOUND DONE' if suite == 'sound' else 'bytes free')
            if suite == 'devices':
                report = run_devices(args)
            elif suite == 'audio':
                report = run_audio(args)
            elif suite == 'opl2':
                report = run_opl2(args)
            elif suite == 'benches':
                report = run_benches(args)
            elif suite == 'diag':
                report = run_diag(args)
            elif suite == 'sound':
                report = run_sound(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'game':
                report = run_game(args, oracle.link_inputs(args.jobs, args.download))
            else:
                run = run_boot if suite == 'boot' else run_vga
                report = run(args, oracle.link_inputs(args.jobs, args.download))
            name = f'game-{args.game}' if suite == 'game' else suite
            (OUTPUT / name).mkdir(parents=True, exist_ok=True)
            (OUTPUT / name / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
            if suite in ('boot', 'vga', 'sound', 'game'):
                for row in report['screen']:
                    print('|' + row + '|')
                for name, present in report['present'].items():
                    print(f'720p {name}: {present["mismatches"]} pixels differ from the presenter model')
                if 'replay' in report:
                    print('REPLAY', report['replay']['summary'])
                print('OPL2', report['opl2'])
                for name, section in report.get('sections', {}).items():
                    print('SECTION', name, section)
                print('SB', report['sb'], 'AUDIO', report['audio_samples'])
                print('END', report['end'])
            else:
                print('\n'.join(report['checks']))
            for failure in report['failures']:
                print('FAIL', failure)
            failed |= bool(report['failures'])
            print(('FAIL' if report['failures'] else 'PASS') + f' {suite}', flush=True)
    except (SimError, firmware.FirmwareError, freedos.ImageError, oracle.OracleError, test386.Test386Error) as error:
        print(f'sim_fes_pc386: {error}', file=sys.stderr)
        return 1
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
