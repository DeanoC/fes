#!/usr/bin/env python3
"""Simulate the pc386 machine: device benches, the oracle-CPU boot and the VGA.

Host-only checks; no kit, package or routed result. See
docs/pc386-machine-design.md, "Verification" and phases M1 and M2.

  devices  cores/fes-pc386/sim/devices_tb.cpp drives the AT core's I/O port
           directly: PIC cascade and acknowledge, 8254 rate and read-back,
           DMA registers, pages and a transfer, 8042 commands, A20 and reset,
           RTC update and interrupts, the ATA command set, and the I/O timing
           classes. Needs only Verilator.
  benches  cores/fes-pc386/sim/presenter_tb.cpp and plane_tb.cpp: the presenter
           against its model, and the shared display plane through the port 2
           bridge and arbiter. Needs only Verilator; under a minute.
  boot     cores/fes-pc386/sim/machine_tb.cpp runs the Bochs BIOS, the LGPL
           VGABios and FreeDOS on the machine (pc386_machine: the AT core and
           the RTL VGA) with the Bochs oracle as the CPU, types a command at
           the prompt, checks the answer read from VRAM and the I/O completion
           times, and replays the boot's VGA trace against Bochs's VGA.
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
  python3 scripts/sim_fes_pc386.py --suite boot --download
  python3 scripts/sim_fes_pc386.py --suite vga
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

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


def verilate(verilator: str, name: str, top: str, rtl: tuple[Path, ...], testbench: Path, linked: list[Path],
             jobs: int, parameters: dict[str, int] | None = None) -> Path:
    output = OUTPUT / name / 'obj'
    output.mkdir(parents=True, exist_ok=True)
    command = [verilator, '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', top,
               '-Wall', '-Wno-UNUSEDSIGNAL', '-Wno-DECLFILENAME',
               str(CORE / 'sim/pc386.vlt'),
               f'-I{COMMON / "vendor/kf8259"}', f'-I{COMMON / "vendor/kf8253"}', f'-I{COMMON / "generated"}',
               *(f'-G{key}={value}' for key, value in (parameters or {}).items()),
               '--Mdir', str(output), '-MAKEFLAGS', 'OPT_FAST=-O3',
               '-CFLAGS', f'-std=c++17 -O2 -I{COMMON / "sim/i386"} -I{CORE / "sim"}', '-LDFLAGS', '-pthread',
               *map(str, rtl), str(testbench), *map(str, linked)]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise SimError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + top)


def parse_boot(text: str) -> dict:
    report = {'post': [], 'debug': [], 'panic': [], 'io': {}, 'inta': {}, 'screen': [], 'end': None,
              'prompt_cycle': None, 'cpu_resets': 0, 'snaps': {}, 'present': {}, 'vga': {}}
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
    return verilate(args.verilator, 'machine', 'pc386_machine', MACHINE_RTL, CORE / 'sim/machine_tb.cpp', linked,
                    args.jobs)


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
               'plane': run_bench(args, 'plane', 'plane_top', PLANE_RTL, CORE / 'sim/plane_tb.cpp')}
    return {'checks': [line for bench in benches.values() for line in bench['checks']],
            'failures': [line for bench in benches.values() for line in bench['failures']], 'benches': benches}


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


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--suite', choices=('devices', 'benches', 'boot', 'vga', 'all'), default='all')
    parser.add_argument('--download', action='store_true', help='fetch Bochs, dev86, FreeDOS and NASM if missing')
    parser.add_argument('--rebuild-image', action='store_true', help='rebuild the FreeDOS fixture')
    parser.add_argument('--limit-seconds', type=float, default=None,
                        help='simulated seconds before giving up (boot 8, vga 12)')
    parser.add_argument('--type', default='dir|', help="keys typed at the prompt; '|' is Enter")
    parser.add_argument('--expect', default='bytes free', help='text the screen must show at the end')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    args = parser.parse_args()
    if shutil.which(args.verilator) is None:
        print(f'selected Verilator not found: {args.verilator}', file=sys.stderr)
        return 2
    failed = False
    try:
        limit = args.limit_seconds
        for suite in ('devices', 'boot', 'vga') if args.suite == 'all' else (args.suite,):
            if suite == 'devices':
                report = run_devices(args)
            elif suite == 'benches':
                report = run_benches(args)
            else:
                args.limit_seconds = limit if limit is not None else 8 if suite == 'boot' else 12
                run = run_boot if suite == 'boot' else run_vga
                report = run(args, oracle.link_inputs(args.jobs, args.download))
            (OUTPUT / suite).mkdir(parents=True, exist_ok=True)
            (OUTPUT / suite / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
            if suite not in ('devices', 'benches'):
                for row in report['screen']:
                    print('|' + row + '|')
                for name, present in report['present'].items():
                    print(f'720p {name}: {present["mismatches"]} pixels differ from the presenter model')
                print('REPLAY', report['replay']['summary'])
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
