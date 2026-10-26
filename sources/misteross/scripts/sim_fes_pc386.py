#!/usr/bin/env python3
"""Simulate the pc386 AT core: directed device benches and the oracle-CPU boot.

Host-only checks; no kit, package or routed result. See
docs/pc386-machine-design.md, "Verification" and phase M1.

  devices  cores/fes-pc386/sim/devices_tb.cpp drives the AT core's I/O port
           directly: PIC cascade and acknowledge, 8254 rate and read-back,
           DMA registers, pages and a transfer, 8042 commands, A20 and reset,
           RTC update and interrupts, the ATA command set, and the I/O timing
           classes. Needs only Verilator.
  boot     cores/fes-pc386/sim/machine_tb.cpp runs the Bochs BIOS, the LGPL
           VGABios and FreeDOS on the AT core with the Bochs oracle as the CPU
           and a text VGA stand-in, types a command at the prompt and checks
           the answer and the I/O completion times. Needs the oracle build,
           the firmware build and the FreeDOS fixture; --download fetches what
           is missing.

Writes build/sim/fes-pc386/<suite>/report.json.

Examples:
  python3 scripts/sim_fes_pc386.py --suite devices
  python3 scripts/sim_fes_pc386.py --suite boot --download
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

ROOT = Path(__file__).resolve().parents[1]
CORE = ROOT / 'cores/fes-pc386'
COMMON = ROOT / 'cores/fes-common'
OUTPUT = ROOT / 'build/sim/fes-pc386'
TOP = 'pc386_at_core'
RTL = (
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
# I/O completion, request clock to done clock: each device cycle's class time
# (isa8 72 clocks, isa16 36 at 100 MHz) plus one clock to take the request and
# one to report it. 1F0h with a dword is one word cycle and two byte cycles
# (1F2h, 1F3h), as the AT's bus sizing does. The VGA stand-in answers each
# byte in the clock after the request, with a clock between bytes.
ISA8, ISA16, OVERHEAD = 72, 36, 2
EXPECTED_IO = {
    ('isa8', 1): ISA8 + OVERHEAD, ('isa8', 2): 2 * ISA8 + OVERHEAD, ('isa8', 4): 4 * ISA8 + OVERHEAD,
    ('isa16', 1): ISA16 + OVERHEAD, ('isa16', 2): ISA16 + OVERHEAD, ('isa16', 4): ISA16 + 2 * ISA8 + OVERHEAD,
    ('vga', 1): 3, ('vga', 2): 5, ('vga', 4): 9,
}
EXPECTED_INTA = 2 * ISA8 + OVERHEAD


class SimError(RuntimeError):
    pass


def verilate(verilator: str, name: str, testbench: Path, linked: list[Path], jobs: int,
             parameters: dict[str, int] | None = None) -> Path:
    output = OUTPUT / name / 'obj'
    output.mkdir(parents=True, exist_ok=True)
    command = [verilator, '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', TOP,
               '-Wall', '-Wno-UNUSEDSIGNAL', '-Wno-DECLFILENAME',
               str(CORE / 'sim/pc386.vlt'),
               f'-I{COMMON / "vendor/kf8259"}', f'-I{COMMON / "vendor/kf8253"}',
               *(f'-G{key}={value}' for key, value in (parameters or {}).items()),
               '--Mdir', str(output), '-MAKEFLAGS', 'OPT_FAST=-O3', '-CFLAGS', f'-std=c++17 -O2 -I{COMMON / "sim/i386"}', '-LDFLAGS', '-pthread',
               *map(str, RTL), str(testbench), *map(str, linked)]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise SimError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + TOP)


def parse_boot(text: str) -> dict:
    report = {'post': [], 'debug': [], 'panic': [], 'io': {}, 'inta': {}, 'screen': [], 'end': None,
              'prompt_cycle': None, 'cpu_resets': 0}
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
        elif kind == 'END':
            fields = rest.split()
            report['end'] = {'reason': fields[0], **{k: float(v) if '.' in v else int(v)
                                                      for k, v in (field.split('=') for field in fields[1:])}}
    return report


def timing_failures(report: dict) -> list[str]:
    failures = []
    for key, histogram in report['io'].items():
        klass, size = key.split()
        expected = EXPECTED_IO.get((klass, int(size)))
        if expected is None or set(histogram) != {str(expected)}:
            failures.append(f'IO {key}: {histogram}, expected every access at {expected} clocks')
    if report['inta'] and set(report['inta']) != {str(EXPECTED_INTA)}:
        failures.append(f'INTA: {report["inta"]}, expected {EXPECTED_INTA} clocks')
    return failures


def run_boot(args, linked: list[Path]) -> dict:
    images = firmware.images(args.download, args.jobs)
    disk = (freedos.IMAGE if freedos.image_is_current() and not args.rebuild_image
            else freedos.build(args.download, args.jobs))
    testbench = verilate(args.verilator, 'boot', CORE / 'sim/machine_tb.cpp', linked, args.jobs)
    command = [str(testbench), '--bios', str(images['bios']), '--vgabios', str(images['vgabios']),
               '--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
               '--type', args.type, '--expect', args.expect]
    print('Booting FreeDOS in simulation (this takes minutes)', flush=True)
    result = subprocess.run(command, cwd=OUTPUT / 'boot', capture_output=True, text=True)
    (OUTPUT / 'boot/machine.log').write_text(result.stdout + result.stderr)
    report = parse_boot(result.stdout)
    report['command'] = [str(Path(arg).relative_to(ROOT)) if arg.startswith(str(ROOT)) else arg for arg in command]
    report['exit'] = result.returncode
    report['failures'] = timing_failures(report)
    if result.returncode != 0:
        report['failures'].append(f'machine_tb exited {result.returncode}: {(report["end"] or {}).get("reason")}'
                                  f' {result.stderr.strip()}')
    if report['panic']:
        report['failures'].append(f'BIOS panic ports written: {report["panic"]}')
    return report


def run_devices(args) -> dict:
    # A fast RTC (one guest second is 2^19 clocks) keeps the update test short.
    testbench = verilate(args.verilator, 'devices', CORE / 'sim/devices_tb.cpp', [], args.jobs,
                         {'RTC_N': 1, 'RTC_D': 16})
    result = subprocess.run([str(testbench)], cwd=OUTPUT / 'devices', capture_output=True, text=True)
    (OUTPUT / 'devices/devices.log').write_text(result.stdout + result.stderr)
    checks = [line for line in result.stdout.splitlines() if line.startswith(('PASS', 'FAIL'))]
    failures = [line for line in checks if line.startswith('FAIL')]
    if result.returncode and not failures:
        failures.append(f'devices_tb exited {result.returncode}: {result.stderr.strip()}')
    return {'checks': checks, 'failures': failures, 'exit': result.returncode}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--suite', choices=('devices', 'boot', 'all'), default='all')
    parser.add_argument('--download', action='store_true', help='fetch Bochs, dev86, FreeDOS and NASM if missing')
    parser.add_argument('--rebuild-image', action='store_true', help='rebuild the FreeDOS fixture')
    parser.add_argument('--limit-seconds', type=float, default=8, help='simulated seconds before giving up')
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
        for suite in ('devices', 'boot') if args.suite == 'all' else (args.suite,):
            report = run_devices(args) if suite == 'devices' else run_boot(args, oracle.link_inputs(args.jobs, args.download))
            (OUTPUT / suite / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
            if suite == 'boot':
                for row in report['screen']:
                    print('|' + row + '|')
                print('END', report['end'])
            else:
                print('\n'.join(report['checks']))
            for failure in report['failures']:
                print('FAIL', failure)
            failed |= bool(report['failures'])
            print(('FAIL' if report['failures'] else 'PASS') + f' {suite}', flush=True)
    except (SimError, firmware.FirmwareError, freedos.ImageError, oracle.OracleError) as error:
        print(f'sim_fes_pc386: {error}', file=sys.stderr)
        return 1
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
