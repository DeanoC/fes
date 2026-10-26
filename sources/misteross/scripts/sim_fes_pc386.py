#!/usr/bin/env python3
"""Simulate the pc386 AT core: directed device benches and the oracle-CPU boot.

Host-only checks; no kit, package or routed result. See
docs/pc386-machine-design.md, "Verification" and phase M1.

  devices  cores/fes-pc386/sim/devices_tb.cpp drives the AT core's I/O port
           directly: PIC cascade and acknowledge, 8254 rate and read-back,
           DMA registers, pages and a transfer, 8042 commands, A20 and reset,
           RTC update and interrupts, the ATA command set, and the I/O timing
           classes. Needs only Verilator.
  audio    cores/fes-pc386/sim/audio_tb.cpp drives the AT core and the audio
           block together: the Sound Blaster DSP's commands, DMA and IRQ at
           every base and IRQ choice, the OPL2's AdLib timers, mixer levels.
  opl2     cores/fes-pc386/sim/opl2_tb.cpp checks the OPL2 sample for sample
           against Nuked-OPL3 on random register programs and on the port
           writes captured from the sound suite (sim/fixtures/*-opl2.log).
  boot     cores/fes-pc386/sim/machine_tb.cpp runs the Bochs BIOS, the LGPL
           VGABios and FreeDOS on the AT core with the Bochs oracle as the CPU
           and a text VGA stand-in, types a command at the prompt and checks
           the answer and the I/O completion times. Needs the oracle build,
           the firmware build and the FreeDOS fixture; --download fetches what
           is missing.
  sound    the boot, then SOUND.COM (sim/sound.asm) at the prompt: AdLib and
           Sound Blaster detection, speaker, OPL2 and DSP DMA, as games start.
           Checks the program's findings, every OPL2 sample against Nuked-OPL3,
           and each section's tone in the captured 48 kHz output, and writes
           build/sim/fes-pc386/sound/sound.wav to listen to. --update-fixture
           copies its OPL2 port log to sim/fixtures/sound-opl2.log.
  game     the boot, then a user-supplied game's start-up (--game wolf3d or
           ultima6, from the eXoDOS zips under $FES_PC386_GAMES), with
           scheduled keys. Checks every OPL2 sample against Nuked-OPL3 and
           reports the Sound Blaster's activity. The game files, the disk
           image, the WAV and the OPL2 log stay in --games-work, outside the
           repository: they reproduce the game. Only measurements go into
           report.json.

Writes build/sim/fes-pc386/<suite>/report.json.

Examples:
  python3 scripts/sim_fes_pc386.py --suite devices
  python3 scripts/sim_fes_pc386.py --suite boot --download
  python3 scripts/sim_fes_pc386.py --suite sound
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import hashlib
import math
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


def verilate(verilator: str, name: str, testbench: Path | None, linked: list[Path], jobs: int,
             parameters: dict[str, int] | None = None, top: str = TOP, rtl: tuple[Path, ...] = AT_RTL,
             include: tuple[Path, ...] = ()) -> Path:
    """Builds a model. Without a testbench it is a library for another model's bench to link."""
    output = OUTPUT / name / ('obj' if testbench else f'obj-{top}')
    output.mkdir(parents=True, exist_ok=True)
    includes = ' '.join(f'-I{path}' for path in (COMMON / 'sim/i386', NUKED, *include))
    command = [verilator, '--cc', *(['--exe'] if testbench else []), '--build', '-j', str(jobs), '-O3',
               '--top-module', top, '-Wall', '-Wno-UNUSEDSIGNAL', '-Wno-DECLFILENAME',
               str(CORE / 'sim/pc386.vlt'), str(CORE / 'sim/pc386_audio.vlt'),
               f'-I{COMMON / "vendor/kf8259"}', f'-I{COMMON / "vendor/kf8253"}',
               *(f'-G{key}={value}' for key, value in (parameters or {}).items()),
               '--Mdir', str(output), '-MAKEFLAGS', 'OPT_FAST=-O3', '-CFLAGS', f'-std=c++17 -O2 {includes}', '-LDFLAGS', '-pthread',
               *map(str, rtl), *([str(testbench)] if testbench else []), *map(str, linked)]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise SimError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + top) if testbench else output / f'V{top}__ALL.a'


def parse_boot(text: str) -> dict:
    report = {'post': [], 'debug': [], 'panic': [], 'io': {}, 'inta': {}, 'screen': [], 'end': None,
              'prompt_cycle': None, 'cpu_resets': 0, 'marks': {}, 'opl2': None, 'opl2_mismatch': None,
              'audio_samples': None, 'sb': None}
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
        elif kind == 'MARK':
            sample, _, name = rest.partition(' ')
            report['marks'][name] = int(sample)
        elif kind == 'OPL2':
            report['opl2'] = {k: int(v) for k, v in (field.split('=') for field in rest.split())}
        elif kind == 'OPL2-MISMATCH':
            report['opl2_mismatch'] = rest
        elif kind == 'AUDIO':
            report['audio_samples'] = int(rest.partition('=')[2])
        elif kind == 'SB':
            report['sb'] = {k: int(v) for k, v in (field.split('=') for field in rest.split())}
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


def run_boot(args, linked: list[Path], suite: str = 'boot', disk: Path | None = None,
             extra: tuple[str, ...] = ()) -> dict:
    images = firmware.images(args.download, args.jobs)
    if disk is None:
        disk = freedos.build(args.download, args.jobs) if args.rebuild_image else freedos.image(args.download, args.jobs)
    # boot, sound and game share one machine build.
    testbench = verilate_with_audio(args, 'machine', CORE / 'sim/machine_tb.cpp', linked)
    (OUTPUT / suite).mkdir(parents=True, exist_ok=True)
    command = [str(testbench), '--bios', str(images['bios']), '--vgabios', str(images['vgabios']),
               '--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
               '--type', args.type, '--expect', args.expect, *extra]
    print('Booting FreeDOS in simulation (this takes minutes)', flush=True)
    result = subprocess.run(command, cwd=OUTPUT / suite, capture_output=True, text=True)
    (OUTPUT / suite / 'machine.log').write_text(result.stdout + result.stderr)
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
    return report


# The sound suite: SOUND.COM's sections in order, and what each must sound like.
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
    result = subprocess.run(['mcopy', '-m', '-i', f'{disk}@@{freedos.PARTITION_START * 512}', str(program), '::SOUND.COM'],
                            capture_output=True, text=True, env=env)
    if result.returncode:
        raise SimError('mcopy failed:\n' + result.stdout + result.stderr)
    return disk


def read_wav(path: Path) -> list[int]:
    data = path.read_bytes()
    if data[:4] != b'RIFF' or data[36:40] != b'data':
        raise SimError(f'{path} is not the bench\'s WAV')
    count = struct.unpack_from('<I', data, 40)[0] // 2
    return list(struct.unpack_from(f'<{count}h', data, 44))


def tone(samples: list[int]) -> dict:
    """Frequency from rising zero crossings, and RMS and peak, of a stretch of 48 kHz output."""
    rises = [n for n in range(1, len(samples)) if samples[n - 1] < 0 <= samples[n]]
    hz = 48000 * (len(rises) - 1) / (rises[-1] - rises[0]) if len(rises) > 2 else 0.0
    rms = math.sqrt(sum(v * v for v in samples) / len(samples)) if samples else 0.0
    return {'hz': round(hz, 2), 'rms': round(rms, 1), 'peak': max((abs(v) for v in samples), default=0),
            'samples': len(samples)}


def run_sound(args, linked: list[Path]) -> dict:
    wav = OUTPUT / 'sound/sound.wav'
    opl2_log = OUTPUT / 'sound/sound-opl2.log'
    report = run_boot(args, linked, 'sound', sound_disk(args), ('--wav', str(wav), '--opl2-log', str(opl2_log)))
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
        # Skip the first 10 ms of each section, while the last one decays.
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
        fixtures = CORE / 'sim/fixtures'
        fixtures.mkdir(exist_ok=True)
        shutil.copyfile(opl2_log, fixtures / 'sound-opl2.log')
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


def verilate_with_audio(args, name: str, testbench: Path, linked: list[Path],
                        parameters: dict[str, int] | None = None) -> Path:
    """The AT core with pc386_audio as a second model, joined by sim/audio_link.h."""
    audio = verilate(args.verilator, name, None, [], args.jobs, top='pc386_audio', rtl=AUDIO_RTL)
    return verilate(args.verilator, name, testbench, [*linked, audio, NUKED / 'opl3.c'], args.jobs, parameters,
                    include=(audio.parent,))


def run_audio(args) -> dict:
    testbench = verilate_with_audio(args, 'audio', CORE / 'sim/audio_tb.cpp', [])
    log = OUTPUT / 'audio/audio.log'
    log.write_text('')
    checks, failures, _ = run_checks([str(testbench)], OUTPUT / 'audio', log)
    return {'checks': checks, 'failures': failures}


def run_opl2(args) -> dict:
    testbench = verilate(args.verilator, 'opl2', CORE / 'sim/opl2_tb.cpp', [NUKED / 'opl3.c'], args.jobs,
                         top='pc386_opl2', rtl=OPL2_RTL)
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


# Games: the eXoDOS zip, the folder in it that is the game, the directory on
# C:, what to type at the prompt, keys as (seconds, keys) and the time limit.
# Wolfenstein 3D's keys go through the signon's "press a key", the PG-13 and
# title screens and the menus into E1M1 (Enter each 1.5 s), then fire the
# pistol (Ctrl) and open doors (space): digitized sounds through the DSP.
GAMES_DIR = Path(os.environ.get('FES_PC386_GAMES',
                                '/mnt/emuhd/Games/PC DOS/Collections/eXoDOS 6.04 Full/eXoDOS/eXo/eXoDOS'))
GAMES = {
    'wolf3d': {'zip': 'Wolfenstein 3D (1992).zip', 'folder': 'Wolf3D/WOLF3D/WOLF3D/', 'directory': 'WOLF3D',
               'type': 'cd wolf3d|wolf3d|', 'limit': 24.0,
               'keys': [(5.0 + 1.5 * n, '|') for n in range(8)] + [(17.0 + 0.8 * n, '^ ') for n in range(8)]},
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
    result = subprocess.run(['mcopy', '-s', '-m', '-i', volume, str(files), '::' + game['directory']],
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
    keys = [arg for seconds, text in game['keys'] for arg in ('--keys', f'{seconds}:{text}')]
    wav, opl2_log = work / f'{args.game}.wav', work / f'{args.game}-opl2.log'
    report = run_boot(args, linked, f'game-{args.game}', disk, ('--wav', str(wav), '--opl2-log', str(opl2_log), *keys))
    report['game'] = {'name': args.game, **record}
    report['work'] = str(work)
    # The run ends at the limit, not at a prompt: that is not a failure here.
    report['failures'] = [failure for failure in report['failures'] if not failure.startswith('machine_tb exited 1')]
    opl2 = report['opl2'] or {}
    if not opl2.get('writes') or not opl2.get('nonzero'):
        report['failures'].append(f'the game did not play the OPL2: {opl2}')
    if wav.exists():
        report['audio'] = tone(read_wav(wav))
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--suite', choices=('devices', 'audio', 'opl2', 'boot', 'sound', 'game', 'all'), default='all')
    parser.add_argument('--download', action='store_true', help='fetch Bochs, dev86, FreeDOS and NASM if missing')
    parser.add_argument('--rebuild-image', action='store_true', help='rebuild the FreeDOS fixture')
    parser.add_argument('--limit-seconds', type=float, default=None,
                        help='simulated seconds before giving up (boot 8, sound 14)')
    parser.add_argument('--game', choices=sorted(GAMES), default='wolf3d', help='game: which game to start')
    parser.add_argument('--games-work', default=str(Path(tempfile.gettempdir()) / 'fes-pc386-games'),
                        help='game: scratch directory outside the repository for the game disk and captures')
    parser.add_argument('--update-fixture', action='store_true',
                        help='sound: copy the captured OPL2 port log to sim/fixtures/sound-opl2.log')
    parser.add_argument('--type', default=None, help="keys typed at the prompt; '|' is Enter (boot: dir, sound: sound)")
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
        for suite in ('devices', 'audio', 'opl2', 'boot', 'sound') if args.suite == 'all' else (args.suite,):
            sound = suite == 'sound'
            args.limit_seconds = limit_seconds or (14 if sound else 8)
            args.type = typed or ('sound|' if sound else 'dir|')
            args.expect = expect or ('SOUND DONE' if sound else 'bytes free')
            if suite == 'devices':
                report = run_devices(args)
            elif suite == 'audio':
                report = run_audio(args)
            elif suite == 'opl2':
                report = run_opl2(args)
            elif suite == 'boot':
                report = run_boot(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'game':
                report = run_game(args, oracle.link_inputs(args.jobs, args.download))
            else:
                report = run_sound(args, oracle.link_inputs(args.jobs, args.download))
            name = f'game-{args.game}' if suite == 'game' else suite
            (OUTPUT / name / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
            if suite in ('boot', 'sound', 'game'):
                for row in report['screen']:
                    print('|' + row + '|')
                print('OPL2', report['opl2'])
                for name, section in report.get('sections', {}).items():
                    print('SECTION', name, section)
                print('SB', report.get('sb'), 'AUDIO', report.get('audio'))
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
