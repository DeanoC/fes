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
           With --cpu rtl, fes_i386_cpu is the CPU and the oracle runs only
           what it stops on (see machine_tb.cpp); writes boot-rtl/, whose
           report lists the stops by stem against the lockstep's scope.
           --image games boots the games image (HIMEMX, CuteMouse, MEM and
           BLASTER; build_pc386_freedos_image.py) and writes boot-games/.
  rtl-stops
           boots cores/fes-pc386/sim/rtl_stop_test.asm on the oracle and on
           the RTL: what the RTL still stops on as not built (#1721), LOADALL
           in real mode and a far JMP through a call gate in protected mode,
           then that JMP under paging from an aliased linear address, whose
           bytes the bench reads through the page tables and after which the
           oracle runs until paging is off. The RTL run must stop on each,
           record the two protected-mode stretches, and write what the
           oracle run writes.
  a20      runs cores/fes-pc386/sim/a20_test.asm (A20TEST.COM) at the FreeDOS
           prompt (#1386). On the boot image, which has no XMS driver, A20
           must be off at the prompt, INT 15h AH=24h and 87h must turn it on,
           off and back whichever control set it, and a shutdown-byte resume
           must keep it. On the games image, HIMEMX's /VERBOSE banner must
           name a real A20 method, not "Always on", A20 must be off at the
           prompt, and XMS 03h/04h and 05h/06h must turn it on and off again
           and agree with memory. Every check must print ok. With --cpu rtl,
           the RTL must run both images without an oracle step or a stop;
           writes a20-rtl/.
  c2xx     runs cores/fes-pc386/sim/c2xx_test.asm (C2TEST.COM) at the
           FreeDOS prompt (#1330): INT 15h C205h with a key or an aux byte
           already in the 8042's output buffer, one with IRQ1 masked, and
           C205h again and again while the bench types keys held 8 ms. Every
           reset must return, a waiting key must reach INT 16h, and every
           typed key must arrive in order. With --cpu rtl, the RTL must run it
           without an oracle step or a stop; writes c2xx-rtl/.

  wait     runs cores/fes-pc386/sim/wait_test.asm (WAITTEST.COM) at the
           FreeDOS prompt (#1150): INT 15h AH=86h waits from 0 us to 300 ms,
           each timed on PIT counter 0 or the BIOS tick, must return within
           their bounds. The BIOS counts port 61h bit 4's changes, which
           follow PIT counter 1 as POST programs it. Every check must print
           ok. With --cpu rtl, the RTL must run it without an oracle step or
           a stop; writes wait-rtl/.
  speaker  runs cores/fes-pc386/sim/speaker_test.asm (SPKTEST.COM) at the
           FreeDOS prompt (#1457): PIT counter 2 must be in mode 3 with LSB
           then MSB counts, as an AT BIOS's POST leaves it, and a count-only
           write of 60 must give what Ultima VI's port 61h toggles rely on:
           output high after each rising gate, and a 19,886 Hz square wave
           with the gate held. Every check must print ok. With --cpu rtl,
           the RTL must run it without an oracle step or a stop; writes
           speaker-rtl/.
  scroll   runs cores/fes-pc386/sim/scroll_test.asm (SCROLLT.COM) at the
           FreeDOS prompt. INT 10h AH=00h for modes 13h, 93h and 03h must keep
           BX, CX, DX, SI, DI, BP, DS and ES, and AH=0Fh must return the mode
           asked for, as Darklands's MGRAPHIC.EXE needs (#1553). INT 10h
           AH=07h of a window from row 0, in modes 13h, 12h and 04h, must
           return with the picture one character row lower and the top row
           blank, as Ultima VII's scroll needs (#1493).
           Every check must print ok. With --cpu rtl, the RTL must run it
           without an oracle step or a stop; writes scroll-rtl/.
  cpu-profile  boots FreeDOS on the oracle and runs CPUPROF.COM
           (cores/fes-pc386/sim/cpu_profile_test.asm); the RTL takes over at
           its fourth page flip and runs 20 ms under machine_tb --cpu-profile
           (cores/fes-pc386/sim/cpu_profile.h). Every clock of the window must
           be sorted into a cause, the RTL must not stop, and each taken
           branch class and cause the program exercises must appear, and the
           front-end split must charge D's empty clocks to Q
           (scripts/pc386_cpu_profile.py prints the window); writes
           cpu-profile/.
  sound    boots FreeDOS and runs SOUND.COM; checks the speaker and DSP tones,
           OPL2 output and DSP interrupts.
  game     boots a prebuilt game drive (build_pc386_game_drive.py;
           docs/pc386-game-drives.md), types --game's launch command at the
           prompt and sends its keys, timed from the launch; writes the last
           captured frame, and one every --frame-every seconds. --drive names
           the drive by path, SHA-256 or name; without it the store's drive
           that holds the title is used. The drive is read in place and never
           written. Titles the manifest marks opl2 (Wolfenstein 3D, Ultima VI)
           must play the OPL2. With --cpu rtl, fes_i386_cpu is the CPU and the
           oracle runs only on its stops; --rtl-at-prompt boots on the oracle
           and hands over at the prompt. Writes game-<name>[-rtl]/. Runs share
           machine/'s build, so start a second run only once the first has
           built it.
  wfw      Windows for Workgroups 3.11 to Program Manager on the RTL
           (docs/pc386-windows-corpus.md): boots the user-supplied wfw-msdos
           drive (build_pc386_game_drive.py; MS-DOS 6.22, WfW 3.11, 32-bit
           disk access and a permanent swap file) on the oracle, hands over to
           fes_i386_cpu at the DOS prompt and types `win`. It writes a frame
           every --frame-every seconds (0.25 by default), reads each with
           scripts/pc386_wfw.py and ends the run (machine_tb --stop-file) once
           two frames in a row show Program Manager with its group window. It
           passes only then, with the RTL retiring all of Windows itself: no
           stop and no oracle step after the handover. --cpu oracle runs it all
           on Bochs. Without the drive in the store it prints SKIP and passes,
           as CI does. One to two hours of wall time: a periodic check, not a
           per-PR gate. Writes wfw[-rtl]/.
  system   boots FreeDOS on pc386_system (everything below top.sv) at the
           sealed clock, with a host that does the runtime's launch and Stop
           through the mailbox and runs INPUT.COM to test keyboard and mouse.
           After the Stop it sweeps holds across port 2's write bursts (#1372).
  governor boots cores/fes-pc386/sim/governor_test.asm on pc386_system under
           max and governor presets, set in the restored fes.pc386.machine
           record as FogCast writes it: an equivalent of Turbo Pascal's CRT
           delay calibration (delay-loop passes in one timer tick) and the
           passes in 18 more ticks, then a port 61h speaker loop's and a VRAM
           fill's passes in a tick, with the machine's device time. Under a
           preset each must be within 5% of the loop's table time
           (governor.toml, with its ISA bus time) at the preset's frequency,
           and the record saved at Stop must hold the first three in CMOS
           70h-79h. Its
           synthetic boot disk skips the system suite's FreeDOS input exercise.
           --preset runs that one preset beside max, with --frequency (the
           table time follows it) and --turbo (it must run as max does).
  vga      runs the benches, boots
           cores/fes-pc386/sim/vga_test.asm (VGA BIOS text, mode 13h, mode Y,
           mode 12h), checks every marked picture's 720p frame from the
           presenter and the plane against the presenter model, and replays
           the VGA trace against Bochs's VGA (cores/fes-pc386/sim/vga_replay_tb.cpp),
           which must match frames of every marked picture.

boot and vga need the oracle build, the firmware build, the FreeDOS fixture
and NASM; --download fetches what is missing. --preset (with --frequency and
--turbo) runs the system suite's record, or boot's and game's --cpu rtl, under
that governor preset; each report uses `<suite>-preset<n>`, with
`-frequency<n>` and/or `-turbo` appended when set. Its `report.json` lives in
`build/sim/fes-pc386/<report-name>/`; vga also writes the marked frames.

Examples:
  python3 scripts/sim_fes_pc386.py --suite devices
  python3 scripts/sim_fes_pc386.py --suite opl2
  python3 scripts/sim_fes_pc386.py --suite boot --download
  python3 scripts/sim_fes_pc386.py --suite boot --cpu rtl
  python3 scripts/sim_fes_pc386.py --suite boot --image games --cpu rtl --type 'mem|' --expect 'high memory area'
  python3 scripts/sim_fes_pc386.py --suite rtl-stops
  python3 scripts/sim_fes_pc386.py --suite a20 --download
  python3 scripts/sim_fes_pc386.py --suite c2xx --cpu rtl
  python3 scripts/sim_fes_pc386.py --suite wait --cpu rtl
  python3 scripts/sim_fes_pc386.py --suite speaker
  python3 scripts/sim_fes_pc386.py --suite scroll --cpu rtl
  python3 scripts/sim_fes_pc386.py --suite cpu-profile --download
  python3 scripts/sim_fes_pc386.py --suite sound --download
  python3 scripts/sim_fes_pc386.py --suite game --game ultima6
  python3 scripts/sim_fes_pc386.py --suite game --game wolf3d --cpu rtl
  python3 scripts/sim_fes_pc386.py --suite wfw --cpu rtl
  python3 scripts/sim_fes_pc386.py --suite vga
  python3 scripts/sim_fes_pc386.py --suite governor
  python3 scripts/sim_fes_pc386.py --suite system --preset 1
"""
from __future__ import annotations

import argparse
import concurrent.futures
import functools
import json
import math
import os
from pathlib import Path
import re
import shutil
import struct
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_pc386_firmware as firmware
from scripts import build_pc386_freedos_image as freedos
from scripts import build_pc386_game_drive as drives
from scripts import fes_i386_oracle as oracle
from scripts import pc386_wfw as wfw
from scripts import test_fes_i386_lockstep as lockstep
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
    CORE / 'rtl/pc386_hid_keyboard.sv',
    CORE / 'rtl/pc386_kbc.sv',
    CORE / 'rtl/pc386_rtc.sv',
    CORE / 'rtl/pc386_ram.sv',
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
# machine_tb --cpu rtl's CPU.
CPU_RTL = tuple(lockstep.RTL / path for path in lockstep.CPU_RTL)
OPL2_SEEDS = (1, 2, 3)
OPL2_SAMPLES = 20000
# The machine without the CPU: the AT core, the VGA, the presenter, port 2
# and the shared display plane.
MACHINE_RTL = AT_RTL + (
    CORE / 'rtl/pc386_vga.sv',
    CORE / 'rtl/pc386_vga_scanout.sv',
    CORE / 'rtl/pc386_presenter.sv',
    CORE / 'rtl/pc386_plane_bridge.sv',
    CORE / 'rtl/pc386_ems.sv',
    CORE / 'rtl/pc386_port2.sv',
    CORE / 'rtl/pc386_machine.sv',
    COMMON / 'rtl/fes_display_plane.v',
    COMMON / 'rtl/fes_display_cdc.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_control.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_video.v',
    ROOT / 'cores/fes-menu/rtl/fes_menu_reader.v',
)
# The M5 system below the board shell: the fes.computer 1.1 mailbox and its
# storage endpoint, the machine, its audio, fes_i386_cpu, the firmware ROM on
# port 0 and the disk's port 2 master (pc386_system.sv), at the sealed clock.
SYSTEM_RTL = (*dict.fromkeys(CPU_RTL + MACHINE_RTL + AUDIO_RTL), COMMON / 'rtl/fes_computer_mailbox.v',
              COMMON / 'rtl/fes_computer_storage.v', CORE / 'rtl/pc386_rom.sv', CORE / 'rtl/pc386_disk.sv',
              CORE / 'rtl/pc386_snapshot.sv', CORE / 'rtl/pc386_status.sv', CORE / 'rtl/pc386_system.sv')
# I/O completion, request clock to done clock: each device cycle's class time
# (isa8 72 clocks, isa16 36 at 100 MHz) plus one clock to take the request and
# one to report it. 1F0h with a dword is one word cycle and two byte cycles
# (1F2h, 1F3h), as the AT's bus sizing does. A VGA port byte is presented a
# clock after the request; a write is in the VGA FIFO a clock later and the
# decoder leaves a clock between bytes. A write byte waits while the FIFO is
# full: machine_tb counts those clocks (IOWAIT) and takes them out of the
# access's time, which must still be the class time. A read waits until the
# FIFO has drained and takes two clocks more, so it takes at least
# VGA_READ_MINIMUM and at most VGA_READ_BOUND.
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
SERPENT_ISLE_MIN_CONVENTIONAL_BYTES = 573 * 1024
GAMES_CONVENTIONAL_HEADROOM_BYTES = 1024
# Frontier: Elite 2's own check ("approx. 594,000 bytes"), the most a game drive's
# title needs from the games image's default boot entry (#1526).
FRONTIER_MIN_CONVENTIONAL_BYTES = 594_000
PRESENTER_RTL = (CORE / 'rtl/pc386_ram.sv', CORE / 'rtl/pc386_presenter.sv')
EMS_RTL = (CORE / 'rtl/pc386_ems.sv',)
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


@functools.lru_cache(maxsize=None)
def verilator_supports_procassinit(verilator: str) -> bool:
    """Whether this Verilator recognizes PROCASSINIT lint controls."""
    try:
        version = subprocess.run([verilator, '--version'], capture_output=True,
                                 text=True, timeout=30, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise SimError(f'cannot determine Verilator version: {error}') from error
    if version.returncode or not re.search(r'\bVerilator\s+\d+\.\d+', version.stdout):
        raise SimError('cannot determine Verilator version')
    try:
        result = subprocess.run([verilator, '-Wno-PROCASSINIT', '--version'],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                timeout=30, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise SimError(f'cannot probe Verilator PROCASSINIT support: {error}') from error
    return result.returncode == 0


def verilate(verilator: str, name: str, top: str, rtl: tuple[Path, ...], testbench: Path | None,
             linked: list[Path], jobs: int, parameters: dict[str, int] | None = None,
             include: tuple[Path, ...] = (), flags: tuple[str, ...] = (), defines: tuple[str, ...] = ()) -> Path:
    output = OUTPUT / name / ('obj' if testbench else f'obj-{top}')
    output.mkdir(parents=True, exist_ok=True)
    includes = ' '.join(f'-I{path}' for path in (COMMON / 'sim/i386', CORE / 'sim', NUKED, *include))
    procassinit_supported = verilator_supports_procassinit(verilator)
    command = [verilator, '--cc', *(['--exe'] if testbench else []), '--build', '-j', str(jobs), '-O3', '--top-module', top,
               '-Wall', '-Wno-UNUSEDSIGNAL', '-Wno-DECLFILENAME', *flags,
               *(str(CORE / 'sim' / name) for name in ('pc386.vlt', 'pc386_audio.vlt', 'pc386_vga.vlt')),
               *(['-DFES_PC386_VERILATOR_5054', str(CORE / 'sim/pc386_verilator_5054.vlt')]
                 if procassinit_supported else []),
               f'-I{COMMON / "vendor/kf8259"}', f'-I{COMMON / "vendor/kf8253"}', f'-I{COMMON / "generated"}',
               *(f'-G{key}={value}' for key, value in (parameters or {}).items()), *(f'-D{name}' for name in defines),
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
    report = {'post': [], 'debug': [], 'panic': [], 'io': {}, 'io_wait': {}, 'inta': {}, 'screen': [], 'end': None,
              'prompt_cycle': None, 'cpu_resets': 0, 'snaps': {}, 'present': {}, 'vga': {}, 'marks': {},
              'game_launch': None, 'opl2': None, 'opl2_mismatch': None, 'audio_samples': None, 'sb': None, 'mouse': None,
              'ems': None, 'rtl_begin': None}
    rtl = {'stops': [], 'pmode': [], 'gaps': [], 'smc': [], 'notes': [], 'counters': None}
    for line in text.splitlines():
        kind, _, rest = line.partition(' ')
        if kind == 'STOP':
            stop, cycle, site, *fields = rest.split()
            rtl['stops'].append({'kind': stop, 'cycle': int(cycle), 'site': site,
                                 'bytes': fields[0] if fields else None, 'stem': fields[1] if fields else None})
        elif kind == 'PMODE':
            start, end, steps, entry, exit_site = rest.split()
            rtl['pmode'].append({'from': int(start), 'to': int(end), 'steps': int(steps), 'entry': entry,
                                 'exit': exit_site})
        elif kind == 'GAP':
            stop, stem, stops, sites, executed, site, cycle = rest.split()
            rtl['gaps'].append(rtl_gap(stop, stem, int(stops), int(sites), int(executed), site, int(cycle)))
        elif kind == 'SMC':
            event, cycle, site, instruction, address = rest.split()
            rtl['smc'].append({'event': event, 'cycle': int(cycle), 'site': site, 'instruction': instruction,
                               'address': address})
        elif kind == 'NOTE':
            rtl['notes'].append(rest)
        elif kind == 'RTL-BEGIN':
            cycle, flips = rest.split()
            report['rtl_begin'] = {'cycle': int(cycle), 'flips': int(flips.partition('=')[2])}
        elif kind == 'RTL':
            rtl['counters'] = {key: int(value) for key, value in (field.split('=') for field in rest.split())}
        elif kind == 'POST':
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
        elif kind == 'IOWAIT':
            klass, size, clocks, count = rest.split()
            report['io_wait'].setdefault(f'{klass} {size}', {})[clocks] = int(count)
        elif kind == 'INTA':
            cycles, count = rest.split()
            report['inta'][cycles] = int(count)
        elif kind == 'SCREEN':
            report['screen'].append(rest.partition('|')[2][:-1])
        elif kind == 'PROMPT':
            report['prompt_cycle'] = int(rest)
        elif kind == 'GAME-LAUNCH':
            fields = dict(field.split('=', 1) for field in rest.split())
            report['game_launch'] = {'seconds': float(fields['seconds']), 'evidence': fields['evidence']}
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
        elif kind == 'MOUSE':
            report['mouse'] = {key: int(value) for key, value in (field.split('=') for field in rest.split())}
        elif kind == 'EMS':
            report['ems'] = {key: int(value) for key, value in (field.split('=') for field in rest.split())}
        elif kind == 'END':
            fields = rest.split()
            report['end'] = {'reason': fields[0], **{k: float(v) if '.' in v else int(v)
                                                      for k, v in (field.split('=') for field in fields[1:])}}
    if rtl['counters'] is not None:
        rtl['gaps'].sort(key=lambda gap: (-gap['stops'], gap['stem']))
        report['rtl'] = rtl
    return report


# Names for the stems the RTL stops on; the lockstep's SCOPE says which are built.
STEM_NAMES = {
    '0F00.0': 'SLDT', '0F00.1': 'STR', '0F00.2': 'LLDT', '0F00.3': 'LTR', '0F00.4': 'VERR', '0F00.5': 'VERW',
    '0F01.0': 'SGDT', '0F01.1': 'SIDT', '0F01.2': 'LGDT', '0F01.3': 'LIDT', '0F01.4': 'SMSW', '0F01.6': 'LMSW',
    '0F01.7': 'INVLPG', '0F02': 'LAR', '0F03': 'LSL', '0F06': 'CLTS', '0F08': 'INVD', '0F09': 'WBINVD',
    '0F0B': 'UD2', '0F20': 'MOV r32, CRn', '0F21': 'MOV r32, DRn', '0F22': 'MOV CRn, r32', '0F23': 'MOV DRn, r32',
    '0F24': 'MOV r32, TRn', '0F26': 'MOV TRn, r32', '0FA2': 'CPUID', '0FA3': 'BT r/m, r', '0FAB': 'BTS r/m, r',
    '0FB3': 'BTR r/m, r', '0FBB': 'BTC r/m, r', '0FB0': 'CMPXCHG r/m8', '0FB1': 'CMPXCHG r/m', '0FC0': 'XADD r/m8',
    '0FC1': 'XADD r/m', '0FBA.0': 'BT group /0', '0FBA.1': 'BT group /1', '0FBA.2': 'BT group /2',
    '0FBA.3': 'BT group /3', '27': 'DAA', '2F': 'DAS', '37': 'AAA', '3F': 'AAS', '62': 'BOUND', '63': 'ARPL',
    '9B': 'WAIT', 'F0': 'LOCK', 'F1': 'ICEBP', 'D6': 'SALC',
    **{f'{opcode:02X}.{reg}': f'ESC {opcode:02X} /{reg}' for opcode in range(0xD8, 0xE0) for reg in range(8)},
    **{f'0FC{n:X}': 'BSWAP' for n in range(8, 16)},
}


def rtl_gap(kind: str, stem: str, stops: int, sites: int, executed: int, site: str, cycle: int) -> dict:
    """One stem the RTL stopped on, and whether the lockstep counts it as built."""
    bare = re.sub(r'^(67)?(66)?', '', stem)
    return {'kind': kind, 'stem': stem, 'name': STEM_NAMES.get(bare), 'built': lockstep.in_scope(stem),
            'stops': stops, 'sites': sites, 'executed': executed,
            'first_site': site, 'first_cycle': cycle}


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
    # cpu_profile.vlt exposes the signals --cpu-profile reads (cpu_profile.h).
    cpu = verilate(args.verilator, 'machine', 'fes_i386_cpu', CPU_RTL + (CORE / 'sim/cpu_profile.vlt',), None, [],
                   args.jobs)
    write_cpu_profile_layout(cpu.parent)
    return verilate_with_audio(args, 'machine', CORE / 'sim/machine_tb.cpp', [*linked, cpu],
                               top='pc386_machine', rtl=MACHINE_RTL, include=(cpu.parent,))


PIPE_STAGES = ('ag_s', 'm1_s', 'm2_s', 'ex_s', 'wb_s')


def write_cpu_profile_layout(obj: Path) -> Path:
    """cpu_profile_layout.h beside the Verilated CPU: the top bit of its packed
    stage registers (pipe_t), which holds valid, from Verilator's root header."""
    header = (obj / 'Vfes_i386_cpu___024root.h').read_text()
    tops = set()
    for stage in PIPE_STAGES:
        found = re.search(rf'/\*(\d+):0\*/ fes_i386_cpu__DOT__{stage};', header)
        if not found:
            raise SimError(f'the Verilated CPU has no {stage} (cpu_profile.vlt)')
        tops.add(int(found.group(1)))
    if len(tops) != 1:
        raise SimError(f'the stage registers differ in width: {sorted(tops)}')
    layout = obj / 'cpu_profile_layout.h'
    layout.write_text('// Written by scripts/sim_fes_pc386.py from the Verilated CPU.\n'
                      f'#pragma once\n#define CPU_PROFILE_PIPE_MSB {tops.pop()}\n')
    return layout


def verilate_with_audio(args, name: str, testbench: Path, linked: list[Path],
                        parameters: dict[str, int] | None = None, top: str = TOP,
                        rtl: tuple[Path, ...] = AT_RTL, include: tuple[Path, ...] = ()) -> Path:
    """Build the audio model and link it beside the AT core in a testbench."""
    audio = verilate(args.verilator, name, 'pc386_audio', AUDIO_RTL, None, [], args.jobs)
    return verilate(args.verilator, name, top, rtl, testbench,
                    [*linked, audio, NUKED / 'opl3.c'], args.jobs, parameters, (audio.parent, *include))


def run_machine(testbench: Path, suite: str, arguments: list[str], images: dict, watch=None) -> dict:
    """Run machine_tb; watch, if given, is called every few seconds while it runs."""
    directory = OUTPUT / suite
    directory.mkdir(parents=True, exist_ok=True)
    command = [str(testbench), '--bios', str(images['bios']), '--vgabios', str(images['vgabios']), *arguments]
    if watch is None:
        result = subprocess.run(command, cwd=directory, capture_output=True, text=True)
    else:
        out, err = directory / 'machine.stdout', directory / 'machine.stderr'
        with out.open('w') as stdout, err.open('w') as stderr:
            process = subprocess.Popen(command, cwd=directory, stdout=stdout, stderr=stderr, text=True)
            try:
                while process.poll() is None:
                    watch()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        pass
                watch()
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
        result = subprocess.CompletedProcess(command, process.returncode, out.read_text(), err.read_text())
        out.unlink()
        err.unlink()
    (directory / 'machine.log').write_text(result.stdout + result.stderr)
    report = parse_boot(result.stdout)
    report['command'] = [str(Path(arg).relative_to(ROOT)) if arg.startswith(str(ROOT)) else arg for arg in command]
    report['exit'] = result.returncode
    report['failures'] = timing_failures(report)
    if result.returncode != 0:
        report['failures'].append(f'machine_tb exited {result.returncode}: {(report["end"] or {}).get("reason")}'
                                  f' {result.stderr.strip()}')
    if report['mouse'] and report['mouse']['sent'] != report['mouse']['scheduled']:
        report['failures'].append(f'mouse packets accepted: {report["mouse"]["sent"]}/'
                                  f'{report["mouse"]["scheduled"]}')
    if report['panic']:
        report['failures'].append(f'BIOS panic ports written: {report["panic"]}')
    opl2 = report['opl2'] or {}
    if opl2.get('exact') != opl2.get('samples') or report['opl2_mismatch']:
        report['failures'].append(f'OPL2 against Nuked-OPL3: {opl2} {report["opl2_mismatch"] or ""}')
    for counter in ('scanout_underflows', 'capture_drops', 'present_underflows', 'plane_underflows'):
        if report['vga'].get(counter):
            report['failures'].append(f'VGA {counter}: {report["vga"][counter]}')
    report['failures'] += store_failures(report)
    return report


def store_failures(report: dict) -> list[str]:
    """The RTL's stores against the stale-code tracker and their pages."""
    rtl = report.get('rtl') or {}
    counters = rtl.get('counters') or {}
    sites = rtl.get('smc') or []
    failures = []
    # The coherent profile: no instruction runs bytes from before a store into them.
    if counters.get('smc_executed'):
        failures.append(f'the RTL ran {counters["smc_executed"]} instructions over bytes stored after it fetched them: '
                        f'{[site for site in sites if site["event"] == "executed"]}')
    # With paging off, every store byte's CPU page is its address's through
    # A20 (#1595: pages read on the wrong edge put stack stores in code).
    if counters.get('store_page_mismatches'):
        failures.append(f'{counters["store_page_mismatches"]} RTL store bytes reported a physical page other than '
                        f'their address\'s: {[site for site in sites if site["event"] == "page"]}')
    return failures


def rtl_boot_failures(report: dict, cpu: str) -> list[str]:
    """Require the RTL itself to reach the FreeDOS prompt on an RTL boot."""
    if cpu != 'rtl':
        return []

    failures = []
    if report.get('prompt_cycle') is None:
        failures.append('the RTL boot did not report reaching the prompt')
    if (report.get('end') or {}).get('reason') != 'prompt':
        failures.append(f'the RTL boot ended before the prompt: {(report.get("end") or {}).get("reason")}')

    rtl = report.get('rtl')
    if not isinstance(rtl, dict):
        failures.append('the RTL boot report has no RTL CPU counters')
        return failures

    counters = rtl.get('counters') or {}
    retired = counters.get('retired')
    if retired is None:
        failures.append('the RTL boot report is missing the retired counter')
    elif retired <= 0:
        failures.append(f'the RTL retired no instructions before the prompt: {retired}')
    value = counters.get('oracle_steps')
    if value is None:
        failures.append('the RTL boot report is missing the oracle_steps counter')
    elif value != 0:
        failures.append(f'RTL oracle_steps={value}, expected 0 for an RTL-only boot')

    value = counters.get('protected_steps')
    if value is None:
        failures.append('the RTL boot report is missing the protected_steps counter')
    elif value != 0:
        failures.append(f'RTL protected_steps={value}, expected 0 for an RTL-only boot')

    stops = rtl.get('stops', [])
    gaps = rtl.get('gaps', [])
    if stops or gaps:
        details = [f'{stop.get("kind")} {stop.get("stem")} at {stop.get("site")}' for stop in stops]
        details.extend(f'{gap.get("kind")} {gap.get("stem")} ({gap.get("stops")} stops)' for gap in gaps)
        failures.append('the RTL stopped or stalled before the prompt: ' + '; '.join(details))
    return failures


def rtl_failures(report: dict, run: str) -> list[str]:
    """Require the RTL to retire the run without handing work to the oracle."""
    rtl = report.get('rtl')
    if not isinstance(rtl, dict):
        return [f'the {run} report has no RTL CPU counters']

    failures = []
    counters = rtl.get('counters') or {}
    retired = counters.get('retired')
    if retired is None:
        failures.append(f'the {run} report is missing the retired counter')
    elif retired <= 0:
        failures.append(f'the RTL retired no instructions in the {run}: {retired}')
    for counter in ('oracle_steps', 'protected_steps'):
        value = counters.get(counter)
        if value is None:
            failures.append(f'the {run} report is missing the {counter} counter')
        elif value != 0:
            failures.append(f'RTL {counter}={value}, expected 0: the {run} handed instructions to the oracle')

    stops = rtl.get('stops', [])
    gaps = rtl.get('gaps', [])
    if stops or gaps:
        details = [f'{stop.get("kind")} {stop.get("stem")} at {stop.get("site")}' for stop in stops]
        details.extend(f'{gap.get("kind")} {gap.get("stem")} ({gap.get("stops")} stops)' for gap in gaps)
        failures.append(f'the RTL stopped or stalled in the {run}: ' + '; '.join(details))
    return failures


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


def boot_suite(args) -> str:
    """The boot suite's output name: boot, boot-games, boot-rtl, boot-games-rtl,
    with a governed run's -preset suffix."""
    return 'boot' + ('' if args.image == 'boot' else f'-{args.image}') + ('-rtl' if args.cpu == 'rtl' else '') \
        + suffix(args)


def run_boot(args, linked: list[Path]) -> dict:
    images = firmware.images(args.download, args.jobs)
    disk = (freedos.build if args.rebuild_image else freedos.image)(args.download, args.jobs, args.image)
    testbench = machine_testbench(args, linked)
    suite = boot_suite(args)
    trace = OUTPUT / suite / 'vga.trace'
    print(f'Booting FreeDOS in simulation on the {args.cpu} CPU (this takes minutes)', flush=True)
    report = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                            '--type', args.type, '--expect', args.expect, '--trace', str(trace),
                                            '--cpu', args.cpu, '--mouse', '0:0,0,0',
                                            *governor_arguments(args)], images)
    typed_commands = {command.strip().lower() for command in args.type.split('|')}
    if args.image == 'games' and 'mem' in typed_commands:
        report['failures'] += games_memory_failures(report['screen'])
    report['failures'] += rtl_boot_failures(report, args.cpu)
    print('Replaying the boot\'s VGA trace against Bochs\'s VGA', flush=True)
    report['replay'] = run_replay(args, suite, trace)
    report['failures'] += report['replay']['failures']
    return report


def games_memory_failures(screen: list[str]) -> list[str]:
    """Check MEM's exact conventional-memory count against Serpent Isle's and Frontier's requirements."""
    text = '\n'.join(screen)
    match = re.search(r'Largest executable program size\s+\d+K\s+\(\s*([\d,]+) bytes\)', text)
    if not match:
        return ['games image MEM output is missing the largest executable program size']
    available = int(match.group(1).replace(',', ''))
    failures = []
    required = SERPENT_ISLE_MIN_CONVENTIONAL_BYTES + GAMES_CONVENTIONAL_HEADROOM_BYTES
    if available < required:
        failures.append(f'games image has {available:,} conventional bytes free; '
                        f'Serpent Isle needs {SERPENT_ISLE_MIN_CONVENTIONAL_BYTES:,} bytes '
                        f'with {GAMES_CONVENTIONAL_HEADROOM_BYTES:,} bytes of headroom ({required:,} total)')
    if available < FRONTIER_MIN_CONVENTIONAL_BYTES:
        failures.append(f'games image has {available:,} conventional bytes free; '
                        f'Frontier: Elite 2 needs {FRONTIER_MIN_CONVENTIONAL_BYTES:,}')
    return failures


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


# What the RTL must stop on in rtl_stop_test.asm, in order: (stem, CS, the
# instruction's first bytes). LOADALL in real mode, which the oracle runs as
# #UD (Bochs at CPU level 3); far JMP through call gate 18h at CPL 0; through
# gate 28h under paging, from CS 20h, whose base maps to the sector through
# a second page table.
RTL_STOPS = (('0F07', '0000', '0f07'), ('EA', '0008', 'ea00001800'), ('EA', '0020', 'ea00002800'))
# The lines both runs write to port E9h.
RTL_STOP_LINES = ['U', 'G', 'P', 'DONE']
# The stretches the oracle runs with PE set: the JMP through gate 18h alone,
# then from the JMP through gate 28h until paging is off.
RTL_STOP_STRETCHES = (1, 10)


def run_rtl_stops(args, linked: list[Path]) -> dict:
    """The stop path of --cpu rtl, on a boot sector of what the RTL still does not build."""
    nasm = test386.nasm(args.download, args.jobs)
    directory = OUTPUT / 'rtl-stops'
    directory.mkdir(parents=True, exist_ok=True)
    binary = directory / 'rtl_stop_test.bin'
    result = subprocess.run([str(nasm), '-f', 'bin', '-o', str(binary), str(CORE / 'sim/rtl_stop_test.asm')],
                            capture_output=True, text=True)
    if result.returncode:
        raise SimError('nasm failed on rtl_stop_test.asm\n' + result.stderr)
    disk = directory / 'rtl_stop_test.img'
    disk.write_bytes(binary.read_bytes().ljust(1 << 20, b'\0'))
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    runs = {}
    for cpu in ('oracle', 'rtl'):
        print(f'Booting the stop test disk on the {cpu} CPU', flush=True)
        runs[cpu] = run_machine(testbench, f'rtl-stops/{cpu}', ['--disk', str(disk), '--limit-seconds',
                                                                str(args.limit_seconds), '--until-debug', 'DONE',
                                                                '--cpu', cpu], images)
    lines = {cpu: [entry['text'] for entry in run['debug'] if entry['port'] == '0e9'] for cpu, run in runs.items()}
    failures = [f'{cpu}: {failure}' for cpu, run in runs.items() for failure in run['failures']]
    for cpu in runs:
        if lines[cpu] != RTL_STOP_LINES:
            failures.append(f'the {cpu} run wrote {lines[cpu]}, expected {RTL_STOP_LINES}')
    rtl = runs['rtl'].get('rtl') or {'stops': [], 'gaps': [], 'pmode': []}
    stops = [(stop['stem'], stop['site'].partition(':')[0], (stop['bytes'] or '')[:len(want)])
             for stop, (_, _, want) in zip(rtl['stops'], RTL_STOPS)]
    if len(rtl['stops']) != len(RTL_STOPS) or stops != list(RTL_STOPS):
        failures.append(f'the RTL stopped at {[(s["stem"], s["site"], s["bytes"]) for s in rtl["stops"]]}, '
                        f'expected {list(RTL_STOPS)}')
    # Each stop's instruction runs on the oracle; LOADALL raises #UD there,
    # so it does not retire.
    gaps = {gap['stem']: (gap['stops'], gap['sites'], gap['executed']) for gap in rtl['gaps']}
    if gaps != {'0F07': (1, 1, 0), 'EA': (2, 2, 2)}:
        failures.append(f'expected LOADALL once (not retired on the oracle) and EA twice, each at its own site: {gaps}')
    if tuple(stretch['steps'] for stretch in rtl['pmode']) != RTL_STOP_STRETCHES:
        failures.append(f'expected protected-mode stretches of {RTL_STOP_STRETCHES} steps: {rtl["pmode"]}')
    return {'lines': lines, 'runs': runs, 'failures': failures,
            'checks': [f'{cpu}: {" | ".join(lines[cpu])}' for cpu in runs] + [f'RTL {gap}' for gap in rtl['gaps']]}


# A20TEST.COM's checks on each image, and HIMEMX's /VERBOSE line naming its A20 method.
A20_CHECKS = {
    'boot': ('boot', 'query', 'query-2403', 'bios', 'kbc', 'port92', 'move', 'move-kbc', 'resume', 'DONE'),
    'games': ('xms-start', 'xms-query', 'xms-global', 'xms-lock', 'DONE'),
}
HIMEMX_METHOD = re.compile(r'(\S.*?) A20 method used')


def run_a20(args, linked: list[Path]) -> dict:
    """A20TEST.COM at the FreeDOS prompt on the boot and games images."""
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    rtl = '-rtl' if args.cpu == 'rtl' else ''
    profiles = tuple(A20_CHECKS)
    disks = {profile: fixture_with(args, f'a20{rtl}/{profile}', 'a20_test.asm', 'A20TEST.COM',
                                   freedos.image(args.download, args.jobs, profile)) for profile in profiles}

    def run(profile: str) -> dict:
        return run_machine(testbench, f'a20{rtl}/{profile}',
                           ['--disk', str(disks[profile]), '--limit-seconds', str(args.limit_seconds),
                            '--type', 'a20test|', '--expect', 'A20 DONE ok', '--cpu', args.cpu], images)

    print(f'Running A20TEST.COM on the {" and ".join(profiles)} image on the {args.cpu} CPU', flush=True)
    with concurrent.futures.ThreadPoolExecutor(len(profiles)) as pool:
        runs = dict(zip(profiles, pool.map(run, profiles)))
    failures, checks = [], []
    for profile, run in runs.items():
        lines = [entry['text'] for entry in run['debug'] if entry['port'] == '0e9']
        run['lines'] = lines
        failures += [f'{profile}: {failure}' for failure in run['failures']]
        failures += [f'{profile}: {line}' for line in lines if not line.endswith(' ok')]
        names = list(dict.fromkeys(line.split()[1] for line in lines if line.startswith('A20 ')))
        if names != list(A20_CHECKS[profile]):
            failures.append(f'{profile}: A20TEST.COM ran {names}, expected {list(A20_CHECKS[profile])}')
        checks += [f'{profile}: {line}' for line in lines]
        if profile == 'boot' and run['cpu_resets'] != 1:
            failures.append(f'boot: expected the resume check\'s one CPU reset, saw {run["cpu_resets"]}')
        if profile == 'games':
            methods = [match.group(1) for row in run['screen'] if (match := HIMEMX_METHOD.search(row))]
            run['himemx_method'] = methods[0] if methods else None
            checks.append(f'games: HIMEMX: {run["himemx_method"]} A20 method used')
            if len(methods) != 1 or methods[0].lower().startswith('always on'):
                failures.append(f'games: HIMEMX must pick a real A20 method, its banner says {methods}')
        if rtl:
            failures += [f'{profile}: {failure}' for failure in rtl_failures(run, 'A20 run')]
    return {'runs': runs, 'checks': checks, 'failures': failures}


# C2TEST.COM's checks in the order it runs them, and the keys the suite types
# after the command (the program's typed_keys), each held 8 ms as system_tb
# holds them.
C2XX_CHECKS = ('typing', 'stale-key', 'stale-aux', 'masked-key', 'DONE')
C2XX_KEYS = 'abcdefghijklmnopq'


# When a key at the games image's boot menu picks an entry: the menu shows by
# then on either CPU (docs/pc386-game-drives.md).
BOOT_MENU_KEY_SECONDS = 1.2
# EMSTEST.COM's checks in the order it runs them (sim/ems_test.asm).
EMS_CHECKS = ('detect', 'frame', 'counts', 'allocate', 'pattern', 'alias', 'unmap', 'save', 'multiple', 'realloc',
              'names', 'move', 'errors', 'os', 'free', 'DONE')


def run_ems(args, linked: list[Path]) -> dict:
    """EMSTEST.COM at the prompt of the games image's entry 3: the EMS board through FESEMS.SYS. With
    --cpu rtl the oracle boots and fes_i386_cpu runs from the prompt."""
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    rtl = '-rtl' if args.cpu == 'rtl' else ''
    disk = fixture_with(args, f'ems{rtl}', 'ems_test.asm', 'EMSTEST.COM',
                        freedos.image(args.download, args.jobs, 'games'))
    print(f'Running EMSTEST.COM on the games image on the {args.cpu} CPU', flush=True)
    run = run_machine(testbench, f'ems{rtl}', ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                               '--type', 'emstest|', '--expect', 'EMS DONE ok', '--cpu', args.cpu,
                                               '--boot-key', f'{BOOT_MENU_KEY_SECONDS}:3',
                                               # The RTL takes over at the prompt, so EMSTEST.COM and the
                                               # driver's INT 67h run on it; the boot is the oracle's.
                                               *(['--rtl-at-prompt', '1'] if rtl else [])], images)
    lines = [entry['text'] for entry in run['debug'] if entry['port'] == '0e9']
    failures = list(run['failures'])
    checks = [line for line in lines if line.startswith('EMS ')]
    failures += [line for line in checks if not line.endswith(' ok')]
    names = [line.split()[1] for line in checks]
    if names != list(EMS_CHECKS):
        failures.append(f'EMSTEST.COM ran {names}, expected {list(EMS_CHECKS)}')
    # The driver names the one unsupported function the errors check calls, and nothing else.
    unsupported = [line for line in lines if line.startswith('FESEMS: function')]
    if unsupported != ['FESEMS: function 5500']:
        failures.append(f'unexpected FESEMS lines: {unsupported}')
    ems = run.get('ems') or {}
    if not ems.get('reads') or not ems.get('writes'):
        failures.append(f'the board saw no frame traffic: {ems}')
    if rtl:
        failures += rtl_failures(run, 'EMS run')
    return {'run': run, 'checks': checks + [f'EMS board: {ems}'], 'failures': failures}


def run_c2xx(args, linked: list[Path]) -> dict:
    """C2TEST.COM at the FreeDOS prompt: INT 15h C2xx with keyboard bytes at the 8042."""
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    suite = 'c2xx' + ('-rtl' if args.cpu == 'rtl' else '')
    disk = fixture_with(args, suite, 'c2xx_test.asm', 'C2TEST.COM')
    print(f'Running C2TEST.COM on the {args.cpu} CPU', flush=True)
    run = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                         '--type', f'c2test|{C2XX_KEYS}', '--key-gap', '0.008',
                                         '--expect', 'C2 DONE ok', '--cpu', args.cpu], images)
    lines = [entry['text'] for entry in run['debug'] if entry['port'] == '0e9']
    run['lines'] = lines
    failures = run['failures'] + [line for line in lines if not line.endswith(' ok')]
    names = [line.split()[1] for line in lines if line.startswith('C2 ')]
    if names != list(C2XX_CHECKS):
        failures.append(f'C2TEST.COM ran {names}, expected {list(C2XX_CHECKS)}')
    if args.cpu == 'rtl':
        failures += rtl_failures(run, 'C2xx run')

    return {'run': run, 'checks': lines, 'failures': failures}

# WAITTEST.COM's checks, in the order it runs them.
WAIT_CHECKS = ('zero', '15us', '100us', '1ms', '10ms', '50ms', '300ms', 'DONE')


def default_limit_seconds(suite: str) -> int:
    """Return the suite's default simulated-time hang guard."""
    if suite == 'sound':
        return 14
    if suite in ('boot', 'governor', 'wait', 'speaker', 'scroll', 'cpu-profile'):
        return 8
    if suite == 'rtl-stops':
        return 2
    if suite in ('a20', 'c2xx'):
        return 10
    return 12


def run_wait(args, linked: list[Path]) -> dict:
    """WAITTEST.COM at the FreeDOS prompt: INT 15h AH=86h waits against PIT counter 0 and the BIOS tick."""
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    suite = 'wait' + ('-rtl' if args.cpu == 'rtl' else '')
    disk = fixture_with(args, suite, 'wait_test.asm', 'WAITTEST.COM')
    print(f'Running WAITTEST.COM on the {args.cpu} CPU', flush=True)
    run = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                         '--type', 'waittest|', '--expect', 'WAIT DONE ok', '--cpu', args.cpu],
                      images)
    lines = [entry['text'] for entry in run['debug'] if entry['port'] == '0e9']
    run['lines'] = lines
    failures = run['failures'] + [line for line in lines if not line.endswith(' ok')]
    names = [line.split()[1] for line in lines if line.startswith('WAIT ')]
    if names != list(WAIT_CHECKS):
        failures.append(f'WAITTEST.COM ran {names}, expected {list(WAIT_CHECKS)}')
    if args.cpu == 'rtl':
        failures += rtl_failures(run, 'wait run')
    return {'run': run, 'checks': lines, 'failures': failures}


# SPKTEST.COM's checks, in the order it runs them.
SPEAKER_CHECKS = ('post', 'rise', 'carrier', 'DONE')


def run_speaker(args, linked: list[Path]) -> dict:
    """SPKTEST.COM at the FreeDOS prompt: PIT counter 2 as POST leaves it, and the carrier a count-only write gives."""
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    suite = 'speaker' + ('-rtl' if args.cpu == 'rtl' else '')
    disk = fixture_with(args, suite, 'speaker_test.asm', 'SPKTEST.COM')
    print(f'Running SPKTEST.COM on the {args.cpu} CPU', flush=True)
    run = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                         '--type', 'spktest|', '--expect', 'SPK DONE ok', '--cpu', args.cpu],
                      images)
    lines = [entry['text'] for entry in run['debug'] if entry['port'] == '0e9']
    run['lines'] = lines
    failures = run['failures'] + [line for line in lines if not line.endswith(' ok')]
    names = [line.split()[1] for line in lines if line.startswith('SPK ')]
    if names != list(SPEAKER_CHECKS):
        failures.append(f'SPKTEST.COM ran {names}, expected {list(SPEAKER_CHECKS)}')
    if args.cpu == 'rtl':
        failures += rtl_failures(run, 'speaker run')
    return {'run': run, 'checks': lines, 'failures': failures}


# SCROLLT.COM's checks, in the order it runs them.
SCROLL_CHECKS = ('modeset', 'mode13', 'mode12', 'mode04', 'DONE')


def run_scroll(args, linked: list[Path]) -> dict:
    """SCROLLT.COM at the FreeDOS prompt: INT 10h AH=00h keeps the caller's registers, and AH=07h
    from row 0 returns in each graphics memory model."""
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    suite = 'scroll' + ('-rtl' if args.cpu == 'rtl' else '')
    disk = fixture_with(args, suite, 'scroll_test.asm', 'SCROLLT.COM')
    print(f'Running SCROLLT.COM on the {args.cpu} CPU', flush=True)
    run = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                         '--type', 'scrollt|', '--expect', 'SCR DONE ok', '--cpu', args.cpu],
                      images)
    lines = [entry['text'] for entry in run['debug'] if entry['port'] == '0e9']
    run['lines'] = lines
    failures = run['failures'] + [line for line in lines if not line.endswith(' ok')]
    names = [line.split()[1] for line in lines if line.startswith('SCR ')]
    if names != list(SCROLL_CHECKS):
        failures.append(f'SCROLLT.COM ran {names}, expected {list(SCROLL_CHECKS)}')
    if args.cpu == 'rtl':
        failures += rtl_failures(run, 'scroll run')
    return {'run': run, 'checks': lines, 'failures': failures}

# The cpu-profile suite's window: the RTL takes over at CPUPROF.COM's fourth
# page flip and runs this long.
CPU_PROFILE_FLIP = 4
CPU_PROFILE_SECONDS = 0.02
# The instruction classes CPUPROF.COM takes branches through, and the causes
# it must reach in the window.
CPU_PROFILE_TAKEN = ('jcc', 'call', 'call_ind', 'ret', 'far_call', 'retf', 'jmp')
# The share of front-end empty clocks the split may leave unattributed (the
# handover's first two).
CPU_PROFILE_UNATTR = 0.001
CPU_PROFILE_CAUSES = ('issued uops', 'redirect: refill', 'interlock: AGI',
                      'interlock: store-to-load', 'multiply and divide', 'I/O and VGA: I/O')


def run_cpu_profile(args, linked: list[Path]) -> dict:
    """machine_tb --cpu-profile's short window: CPUPROF.COM at the FreeDOS prompt on the oracle, then the
    RTL from its fourth page flip, every clock sorted into a cause (scripts/pc386_cpu_profile.py)."""
    from scripts import pc386_cpu_profile as cpu_profile
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    suite = 'cpu-profile'
    disk = fixture_with(args, suite, 'cpu_profile_test.asm', 'CPUPROF.COM')
    profile = OUTPUT / suite / 'cpu.prof'
    profile.unlink(missing_ok=True)
    print(f'Running CPUPROF.COM on the oracle, then {CPU_PROFILE_SECONDS} s on the RTL from page flip '
          f'{CPU_PROFILE_FLIP}', flush=True)
    run = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                         '--type', 'cpuprof|', '--cpu', 'rtl', '--rtl-at-flip', str(CPU_PROFILE_FLIP),
                                         '--rtl-for', str(CPU_PROFILE_SECONDS), '--cpu-profile', str(profile),
                                         '--cpu-profile-every', str(CPU_PROFILE_SECONDS / 4)], images)
    failures = run['failures'] + rtl_failures(run, 'profile window')
    restarts = ((run.get('rtl') or {}).get('counters') or {}).get('smc_restarts')
    if restarts:
        failures.append(f'{restarts} SMC restarts: CPUPROF.COM must not store near its code')
    if (run['end'] or {}).get('reason') != 'rtl-window':
        failures.append(f'the run ended with {(run["end"] or {}).get("reason")}, not its RTL window')
    begin = run['rtl_begin']
    if not begin or begin['flips'] < CPU_PROFILE_FLIP:
        failures.append(f'the RTL did not take over at page flip {CPU_PROFILE_FLIP}: {begin}')
    window = None
    try:
        snaps = cpu_profile.load(profile.read_text().splitlines())
        window = cpu_profile.window(snaps)
    except (OSError, cpu_profile.ProfileError) as error:
        failures.append(f'the profile: {error}')
    if window:
        if len(snaps) < 4:
            failures.append(f'the profile has {len(snaps)} snapshots, expected at least 4')
        if begin and window['from_cycle'] != begin['cycle']:
            failures.append(f'the first snapshot (cycle {window["from_cycle"]}) is not at RTL-BEGIN ({begin["cycle"]})')
        if window['rtl_clocks'] != window['cycles']:
            failures.append(f'{window["rtl_clocks"]} of the window\'s {window["cycles"]} clocks were sorted; '
                            'the RTL must run all of them')
        if not window['frames']:
            failures.append('no page flip in the RTL window')
        for name in CPU_PROFILE_TAKEN:
            if not window['classes'].get(name, {}).get('taken'):
                failures.append(f'no taken {name} in the window')
        for cause in CPU_PROFILE_CAUSES:
            if not window['causes'].get(cause):
                failures.append(f'no clocks in {cause}')
        # The front-end split charges D's empty clocks to Q two clocks
        # before; one Q issued or held then means the offset is wrong.
        fe = window['front_end']
        unattr = fe['by_reason']['unattr']['clocks'] if fe else None
        if not fe or not fe['clocks']:
            failures.append('no front-end split')
        elif unattr > CPU_PROFILE_UNATTR * fe['clocks']:
            failures.append(f'{unattr} of {fe["clocks"]} front-end empty clocks have no reason in Q two clocks before')
        print(cpu_profile.report(window))
    return {'run': run, 'window': window, 'failures': failures}


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
               'plane': run_bench(args, 'plane', 'plane_top', PLANE_RTL, CORE / 'sim/plane_tb.cpp'),
               'ems': run_bench(args, 'ems', 'pc386_ems', EMS_RTL, CORE / 'sim/ems_tb.cpp')}
    return {'checks': [line for bench in benches.values() for line in bench['checks']],
            'failures': [line for bench in benches.values() for line in bench['failures']], 'benches': benches}


def parse_system(text: str) -> dict:
    """system_tb's lines."""
    report = {'identity': None, 'restore_cycle': None, 'clock_cycle': None, 'crc_rejected': None, 'disk': None,
              'release_cycle': None, 'post': [], 'debug': [], 'counters': [], 'prompt_cycle': None, 'screen': [],
              'status': None, 'save': None, 'record': None, 'roundtrip': None, 'record_checks': [], 'write_test': None,
              'typed': [], 'mouse': [], 'holds': [], 'mailbox': None, 'end': None}
    for line in text.splitlines():
        kind, _, rest = line.partition(' ')
        if kind == 'IDENTITY':
            report['identity'] = [int(word) for word in rest.split()]
        elif kind in ('RESTORE', 'CLOCK', 'RELEASE'):
            report[{'RESTORE': 'restore_cycle', 'CLOCK': 'clock_cycle', 'RELEASE': 'release_cycle'}[kind]] = int(rest)
        elif kind == 'CRC-REJECTED':
            cycle, flags = rest.split()
            report['crc_rejected'] = {'cycle': int(cycle), 'flags': int(flags.split('=')[1])}
        elif kind == 'DISK':
            cycle, *fields = rest.split()
            report['disk'] = {'cycle': int(cycle), **{key: int(value) for key, value in
                                                     (field.split('=') for field in fields)}}
        elif kind == 'POST':
            code, cycle = rest.split()
            report['post'].append({'code': code, 'cycle': int(cycle)})
        elif kind == 'DEBUG':
            report['debug'].append(rest)
        elif kind == 'TYPED':
            cycle, _, text = rest.partition(' ')
            report['typed'].append({'cycle': int(cycle), 'text': text})
        elif kind == 'MOUSE':
            cycle, dx, dy, buttons = (int(value) for value in rest.split())
            report['mouse'].append({'cycle': cycle, 'dx': dx, 'dy': dy, 'buttons': buttons})
        elif kind == 'COUNTER':
            field, cycle = rest.split()
            name, value = field.split('=')
            report['counters'].append({'name': name, 'value': int(value), 'cycle': int(cycle)})
        elif kind == 'PROMPT' and report['prompt_cycle'] is None:
            report['prompt_cycle'] = int(rest)
        elif kind == 'SCREEN':
            report['screen'].append(rest.split('|', 1)[1].rsplit('|', 1)[0])
        elif kind == 'STATUS':
            report['status'] = {key: int(value, 16) if key in ('post', 'cs', 'vector', 'eip') else int(value)
                                for key, value in (field.split('=') for field in rest.split())}
        elif kind == 'SAVE':
            fields = dict(field.split('=') for field in rest.split())
            chunks = lambda value: [] if value == '-' else [int(c) for c in value.split(',')]
            report['save'] = {'dirty': int(fields['dirty']), 'epoch': int(fields['epoch']),
                              'changed_bytes': int(fields['changed_bytes']), 'chunks': chunks(fields['chunks']),
                              'changed_chunks': chunks(fields['changed_chunks'])}
        elif kind == 'WRITE-TEST':
            report['write_test'] = None if rest == 'absent' else int(rest)
        elif kind in ('RECORD', 'ROUNDTRIP'):
            report[kind.lower()] = [int(word) for word in rest.split()]
        elif kind == 'HOLD':
            cycle, owed = rest.split()
            report['holds'].append({'cycle': int(cycle), 'owed': [int(n) for n in owed.split('=')[1].split(',')]})
        elif kind == 'CHECK':
            verdict, _, what = rest.partition(' ')
            report['record_checks'].append({'pass': verdict == 'PASS', 'check': what})
        elif kind == 'MAILBOX':
            report['mailbox'] = rest
        elif kind == 'END':
            reason, *fields = rest.split()
            values = dict(field.split('=') for field in fields)
            report['end'] = {'reason': reason, 'cycles': int(values['cycles']), 'seconds': float(values['seconds'])}
    return report


def system_failures(report: dict) -> list[str]:
    """The runtime's launch must stage the disk through the mailbox and its CRC
    pass, the system must reach the prompt with the CPU alone and every counter
    at zero, and the Stop's disk save and record must check out."""
    failures = []
    if report.get('mailbox'):
        failures.append(f'mailbox: {report["mailbox"]}')
    end = report.get('end') or {}
    if end.get('reason') != 'prompt':
        failures.append(f'the system did not reach the prompt: {end.get("reason")}')
    if report.get('crc_rejected') is None:
        failures.append('a wrong CRC was not rejected')
    if report.get('write_test') is None:
        failures.append("the bench did not give the boot its disk write (the fixture's AUTOEXEC.BAT)")
    status = report.get('status')
    if status is None:
        return failures + ['system_tb printed no status words']
    if status['stopped']:
        failures.append(f'the CPU stopped at {status["cs"]:04x}:{status["eip"]:08x}, vector {status["vector"]:02x}')
    for counter in ('scanout_underflows', 'capture_drops', 'present_underflows', 'plane_underflows'):
        if status[counter]:
            failures.append(f'{counter}={status[counter]}')
    if not status['retired']:
        failures.append('the CPU retired nothing')
    panics = [line for line in report['debug'] if 'PANIC' in line.upper()]
    if panics:
        failures.append(f'BIOS panic: {panics[:4]}')
    if end.get('reason') == 'prompt':
        if report.get('save') is None or report.get('roundtrip') is None:
            failures.append('the Stop did not save the disk and the record')
        failures += [f'record check failed: {check["check"]}' for check in report['record_checks'] if not check['pass']]
    return failures


def governor_arguments(args) -> list[str]:
    """The governor's settings for system_tb or machine_tb, if any."""
    if not (args.preset or args.frequency or args.turbo):
        return []
    return ['--preset', str(args.preset), '--frequency', str(args.frequency), '--turbo', str(args.turbo)]


def system_testbench(args) -> Path:
    """system_tb on pc386_system at the sealed clock and guest rates."""
    from scripts import build_fes_pc386_quartus as producer
    images = firmware.images(args.download, args.jobs)
    rom = firmware.write_rom_hex(images)
    parameters = {**producer.guest_rates(producer.SYSTEM_CLOCK_HZ), 'ROM_HEX': f'"{rom}"'}
    # The OPL2 import declares a timescale and the CPU and KF imports do not.
    return verilate(args.verilator, 'system', 'pc386_system', SYSTEM_RTL, CORE / 'sim/system_tb.cpp', [],
                    args.jobs, parameters, flags=('-Wno-TIMESCALEMOD',))


def suffix(args) -> str:
    """A governed run's report directory suffix, including its overrides."""
    if not governor_arguments(args):
        return ''
    suffix = f'-preset{args.preset}'
    if args.frequency:
        suffix += f'-frequency{args.frequency}'
    if args.turbo:
        suffix += '-turbo'
    return suffix


def fixture_with(args, suite: str, source: str, name: str, base: Path | None = None) -> Path:
    """The FreeDOS fixture with a NASM program from sim/ added as C:\\<name>."""
    work = OUTPUT / suite
    work.mkdir(parents=True, exist_ok=True)
    nasm = test386.nasm(args.download, args.jobs)
    program = work / name
    result = subprocess.run([str(nasm), '-f', 'bin', '-o', str(program), str(CORE / 'sim' / source)],
                            capture_output=True, text=True)
    if result.returncode:
        raise SimError(f'NASM failed on {source}:\n' + result.stderr)
    disk = work / 'disk.img'
    shutil.copyfile(base or freedos.image(args.download, args.jobs), disk)
    env = dict(os.environ, MTOOLS_SKIP_CHECK='1', MTOOLSRC='/dev/null', TZ='UTC')
    result = subprocess.run(['mcopy', '-m', '-i', f'{disk}@@{freedos.PARTITION_START * 512}', str(program),
                             f'::{name}'], capture_output=True, text=True, env=env)
    if result.returncode:
        raise SimError('mcopy failed:\n' + result.stdout + result.stderr)
    return disk


def run_system(args) -> dict:
    """FreeDOS to the prompt on pc386_system, everything below top.sv, with the
    DDR ports on one window model and a host that does the runtime's launch,
    keyboard and mouse input and Stop through the mailbox and the window."""
    from scripts import build_fes_pc386_quartus as producer
    base = freedos.build(args.download, args.jobs) if args.rebuild_image else freedos.image(args.download, args.jobs)
    suite = 'system' + suffix(args)
    disk = fixture_with(args, suite, 'input_test.asm', 'INPUT.COM', base)
    testbench = system_testbench(args)
    directory = OUTPUT / suite
    directory.mkdir(parents=True, exist_ok=True)
    print('Booting FreeDOS on pc386_system (this takes most of an hour)', flush=True)
    command = [str(testbench), '--disk', str(disk), '--clock-hz', str(producer.SYSTEM_CLOCK_HZ),
               '--limit-seconds', str(args.limit_seconds), *governor_arguments(args)]
    # The log fills as the bench runs, so a long boot can be followed.
    log = directory / 'system.log'
    with log.open('w') as stream:
        run = subprocess.run(command, cwd=directory, stdout=stream, stderr=subprocess.PIPE, text=True)
        stream.write(run.stderr)
    report = parse_system(log.read_text())
    report['command'] = [str(Path(arg).relative_to(ROOT)) if arg.startswith(str(ROOT)) else arg for arg in command]
    report['exit'] = run.returncode
    report['governor'] = {'preset': args.preset, 'frequency': args.frequency, 'turbo': args.turbo}
    report['failures'] = system_failures(report)
    governed = bool(args.preset) and not args.turbo
    if report.get('status') and report['status'].get('governed') != int(governed):
        report['failures'].append(f'preset {args.preset}, turbo {args.turbo}: the governor '
                                  + ('never held the CPU' if governed else 'held the CPU'))
    if run.returncode and not report['failures']:
        report['failures'].append(f'system_tb exited {run.returncode}: {run.stderr.strip()[-400:]}')
    report['checks'] = [f'{report["end"]}', f'{report["status"]}', f'{report["save"]}',
                        *(f'{"PASS" if c["pass"] else "FAIL"} {c["check"]}' for c in report['record_checks'])]
    return report


# The governor suite: the presets it runs beside max, and governor_test.asm's
# loops, pass by pass. The delay loop: SUB, SBB, JC falling through, CMP with
# ES:[DI] (one memory bus cycle) and JE taken. The speaker loop: IN and OUT
# of port 61h (an 8-bit ISA cycle each), XOR, ADD, the CMP and JE. The VRAM
# fill: two MOVs, REP STOSW of 512 words into VGA memory (a VGA cycle each),
# ADD, the CMP and JE. A timer tick is 65536 PIT clocks.
GOVERNOR_PRESETS = (1, 2, 3, 5)
GOVERNOR_BUDGET = 0.05
TICK_SECONDS = 65536 / (105_000_000 / 88)


def governor_pass_ops(loop: str) -> list:
    from scripts import test_fes_i386_governor as bench
    compare = [bench.m('cmp_load', 1), bench.taken('jcc')]
    if loop == 'crt':
        return [bench.r('alu_ri'), bench.r('alu_ri'), bench.r('jcc'), *compare]
    if loop == 'speaker':
        return [bench.io('in_imm', 0x61), bench.r('alu_ri'), bench.io('out_imm', 0x61), bench.r('alu_ri'), *compare]
    if loop == 'vram':
        return [bench.r('mov_r'), bench.r('mov_r'), bench.m('rep_stos', 0),
                *[bench.element('rep_stos', 1, vga=(1,))] * 512, bench.r('alu_ri'), *compare]
    raise ValueError(loop)


def governor_pass_clocks(preset, loop: str = 'crt', frequency: int = 0) -> int:
    from scripts import fes_i386_governor as table
    from scripts import test_fes_i386_governor as bench
    loaded = table.load()
    return sum(bench.charge(loaded, preset, op, frequency) for op in governor_pass_ops(loop))


def cmos(record: list[int], byte: int) -> int:
    word = record[8 + byte // 2]
    return word >> 8 if byte & 1 else word & 0xff


def run_governor(args) -> dict:
    """governor_test.asm on pc386_system under max and the presets, each set in
    the restored record (system_tb --preset), in parallel."""
    from concurrent.futures import ThreadPoolExecutor
    from scripts import build_fes_pc386_quartus as producer
    from scripts import fes_i386_governor as table
    nasm = test386.nasm(args.download, args.jobs)
    directory = OUTPUT / ('governor' + suffix(args))
    directory.mkdir(parents=True, exist_ok=True)
    binary = directory / 'governor_test.bin'
    result = subprocess.run([str(nasm), '-f', 'bin', '-o', str(binary), str(CORE / 'sim/governor_test.asm')],
                            capture_output=True, text=True)
    if result.returncode:
        raise SimError('nasm failed on governor_test.asm\n' + result.stderr)
    disk = directory / 'governor_test.img'
    disk.write_bytes(binary.read_bytes().ljust(1 << 20, b'\0'))
    testbench = system_testbench(args)
    presets = {p.id: p for p in table.load().presets}
    chosen = [0, *sorted(set([args.preset] if args.preset else GOVERNOR_PRESETS))]
    print(f'Booting the governor test disk on pc386_system under presets {chosen} (this takes most of an hour)',
          flush=True)

    # The record's words 0, 1 and 3 for each run: --frequency and --turbo go
    # with --preset's run.
    settings = {preset: (preset, args.frequency, args.turbo) if preset and preset == args.preset else (preset, 0, 0)
                for preset in chosen}

    def boot(preset: int) -> dict:
        run_directory = directory / f'preset{preset}'
        run_directory.mkdir(parents=True, exist_ok=True)
        _, frequency, turbo = settings[preset]
        command = [str(testbench), '--disk', str(disk), '--clock-hz', str(producer.SYSTEM_CLOCK_HZ),
                   '--limit-seconds', str(args.limit_seconds), '--prompt', 'GOV DONE', '--exercise-input', '0',
                   '--preset', str(preset),
                   '--frequency', str(frequency), '--turbo', str(turbo)]
        log = run_directory / 'system.log'
        with log.open('w') as stream:
            run = subprocess.run(command, cwd=run_directory, stdout=stream, stderr=subprocess.PIPE, text=True)
            stream.write(run.stderr)
        report = parse_system(log.read_text())
        report['exit'] = run.returncode
        return report

    with ThreadPoolExecutor(max_workers=len(chosen)) as executor:
        runs = dict(zip(chosen, executor.map(boot, chosen)))
    failures, checks, results = [], [], {}
    for preset, report in runs.items():
        _, frequency, turbo = settings[preset]
        name = (presets[preset].name if preset else 'max') + (f' at {frequency / 10:g} MHz' if frequency else '') \
            + (' with turbo' if turbo else '')
        governed = bool(preset) and not turbo
        line = next((text for text in report['debug'] if text.startswith('GOV TP=')), None)
        line2 = next((text for text in report['debug'] if text.startswith('GOV SPK=')), None)
        end = report.get('end') or {}
        if end.get('reason') != 'prompt' or line is None or line2 is None:
            failures.append(f'{name}: the test did not finish ({end.get("reason")}, {report.get("mailbox")})')
            continue
        fields = dict(field.split('=') for field in line.split()[1:] + line2.split()[1:])
        tp, dc, c18 = int(fields['TP'], 16), int(fields['DC'], 16), int(fields['C18'], 16)
        spk, vram = int(fields['SPK'], 16), int(fields['VRAM'], 16)
        record = report['record']
        stored = [cmos(record, byte) for byte in range(0x70, 0x7c)]
        entry = {'tp': tp, 'delay_count': dc, 'c18': c18, 'speaker': spk, 'vram': vram, 'seconds': end['seconds'],
                 'governed': report['status']['governed'], 'record_words': [record[0], record[1], record[3]],
                 'cmos_70_7b': stored}
        failures += [f'{name}: record check failed: {check["check"]}' for check in report['record_checks']
                     if not check['pass']]
        if stored[:10] != list((tp | dc << 32 | c18 << 48).to_bytes(10, 'little')) or stored[11] != 0xa5:
            failures.append(f'{name}: CMOS 70h-7Bh {stored} do not hold TP={tp} DC={dc} C18={c18}')
        if (record[0], record[1], record[3]) != settings[preset] or report['status']['governed'] != int(governed):
            failures.append(f'{name}: record words 0, 1, 3 {(record[0], record[1], record[3])}, '
                            f'governed {report["status"]["governed"]}')
        if dc != tp // 55:
            failures.append(f'{name}: DC {dc} is not TP / 55 ({tp // 55})')
        if governed:
            hz = (frequency or presets[preset].frequency) * 100_000
            clocks = governor_pass_clocks(presets[preset], 'crt', frequency)
            per_tick = TICK_SECONDS * hz / clocks
            spk_clocks = governor_pass_clocks(presets[preset], 'speaker', frequency)
            vram_clocks = governor_pass_clocks(presets[preset], 'vram', frequency)
            spk_tick, vram_tick = TICK_SECONDS * hz / spk_clocks, TICK_SECONDS * hz / vram_clocks
            entry.update(pass_clocks=clocks, table_tp=round(per_tick, 1), table_c18=round(18 * per_tick, 1),
                         tp_error=round(tp / per_tick - 1, 5), c18_error=round(c18 / (18 * per_tick) - 1, 5),
                         speaker_pass_clocks=spk_clocks, table_speaker=round(spk_tick, 1),
                         speaker_error=round(spk / spk_tick - 1, 5), vram_pass_clocks=vram_clocks,
                         table_vram=round(vram_tick, 1), vram_error=round(vram / vram_tick - 1, 5))
            for what, value, want in (('TP', tp, per_tick), ('C18', c18, 18 * per_tick), ('SPK', spk, spk_tick),
                                      ('VRAM', vram, vram_tick)):
                if abs(value / want - 1) > GOVERNOR_BUDGET:
                    failures.append(f'{name}: {what} {value} is {value / want - 1:+.2%} from the table ({want:.0f})')
        results[name] = entry
        checks.append(f'{name}: TP={tp} DC={dc} C18={c18} SPK={spk} VRAM={vram}' + (
            f' table TP {entry["table_tp"]:.0f} ({entry["tp_error"]:+.2%}) C18 {entry["table_c18"]:.0f} '
            f'({entry["c18_error"]:+.2%}) SPK {entry["table_speaker"]:.0f} ({entry["speaker_error"]:+.2%}) '
            f'VRAM {entry["table_vram"]:.0f} ({entry["vram_error"]:+.2%})' if governed else ''))
    # Turbo runs at max: its passes within the budget of max's.
    if args.turbo and 'max' in results and len(results) == 2:
        (name, entry), = [(n, e) for n, e in results.items() if n != 'max']
        error = entry['tp'] / results['max']['tp'] - 1
        entry['tp_error_from_max'] = round(error, 5)
        if abs(error) > GOVERNOR_BUDGET:
            failures.append(f'{name}: TP {entry["tp"]} is {error:+.2%} from max\'s ({results["max"]["tp"]})')
    return {'runs': results, 'checks': checks, 'failures': failures}


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


# The fes.computer 1.1 golden exchanges (mister-packages
# testdata/fes-computer-v1/exchanges-1.1.json) and the endpoint parameter of
# each live capability bit the scenarios advertise. Bit 12 (HPS DDR) comes
# with the session display.
STORAGE_EXCHANGES = COMMON / 'generated/computer-exchanges-1.1.json'
STORAGE_RTL = (COMMON / 'rtl/fes_computer_mailbox.v', COMMON / 'rtl/fes_computer_storage.v',
               COMMON / 'sim/computer_storage_top.sv')
STORAGE_PARAMETERS = {1: 'ENABLE_KEYBOARD', 3: 'ENABLE_AUDIO', 4: 'ENABLE_APPLE2_FLOPPY', 8: 'ENABLE_MOUSE',
                      13: 'ENABLE_SESSION_DISPLAY', 16: 'ENABLE_ATA_DISK', 17: 'ENABLE_PERSISTENCE',
                      18: 'ENABLE_WALL_CLOCK', 19: 'ENABLE_PC386_MACHINE'}


def run_storage(args) -> dict:
    """Replays every scenario of the 1.1 exchanges against fes_computer_mailbox_core
    and fes_computer_storage, compiled with that scenario's interfaces."""
    scenarios = json.loads(STORAGE_EXCHANGES.read_text())['scenarios']
    checks, failures = [], []
    ata_binary = None
    for number, scenario in enumerate(scenarios):
        capabilities = scenario['capabilities']
        parameters = {name: "1'b1" for bit, name in STORAGE_PARAMETERS.items() if capabilities >> bit & 1}
        binary = verilate(args.verilator, f'storage-{number}', 'computer_storage_top', STORAGE_RTL,
                          COMMON / 'sim/computer_storage_tb.cpp', [], args.jobs, parameters)
        result = subprocess.run([str(binary), str(STORAGE_EXCHANGES), str(number)], capture_output=True, text=True)
        (OUTPUT / f'storage-{number}' / 'storage.log').write_text(result.stdout + result.stderr)
        line = f'{scenario["name"]}: {(result.stdout or result.stderr).strip()}'
        (checks if result.returncode == 0 else failures).append(line)
        if scenario['name'] == 'pc386-ata-disk':
            ata_binary = binary
    if ata_binary is not None:
        result = subprocess.run([str(ata_binary), str(STORAGE_EXCHANGES), '--prepare-drain'],
                                capture_output=True, text=True)
        (OUTPUT / 'storage-prepare-drain.log').write_text(result.stdout + result.stderr)
        line = f'prepare-drain-final-write: {(result.stdout or result.stderr).strip()}'
        (checks if result.returncode == 0 else failures).append(line)
    return {'checks': checks, 'failures': failures}


def run_devices(args) -> dict:
    # A fast RTC (one guest second is 2^19 clocks) keeps the update test short.
    # The RAMs poison a read of the word being written, as M10K leaves it.
    testbench = verilate(args.verilator, 'devices', 'pc386_at_core', AT_RTL, CORE / 'sim/devices_tb.cpp', [],
                         args.jobs, {'RTC_N': 1, 'RTC_D': 16}, defines=('PC386_RAM_COLLISION',))
    result = subprocess.run([str(testbench)], cwd=OUTPUT / 'devices', capture_output=True, text=True)
    (OUTPUT / 'devices/devices.log').write_text(result.stdout + result.stderr)
    checks = [line for line in result.stdout.splitlines() if line.startswith(('PASS', 'FAIL'))]
    failures = [line for line in checks if line.startswith('FAIL')]
    if result.returncode and not failures:
        failures.append(f'devices_tb exited {result.returncode}: {result.stderr.strip()}')
    # The host keyboard model alone, against the Linux-derived set 2 reference.
    keyboard = run_bench(args, 'keyboard', 'pc386_hid_keyboard', (CORE / 'rtl/pc386_hid_keyboard.sv',),
                         CORE / 'sim/keyboard_tb.cpp')
    return {'checks': checks + keyboard['checks'], 'failures': failures + keyboard['failures'],
            'exit': result.returncode}


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
    return fixture_with(args, 'sound', 'sound.asm', 'SOUND.COM')


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


def game_drive(args) -> tuple[Path, dict, dict]:
    """The prebuilt drive (build_pc386_game_drive.py) holding --game: --drive by
    path, SHA-256 or name, else the store's drive whose record lists the title."""
    if args.drive:
        image, record = drives.resolve(args.drive)
    else:
        found = [link for link in sorted((drives.STORE / 'by-name').glob('*.json'))
                 if any(title['id'] == args.game for title in json.loads(link.read_text()).get('titles', []))]
        if len(found) != 1:
            raise SimError(f'{len(found)} drives in {drives.STORE}/by-name hold {args.game}; pass --drive '
                           '(build them with build_pc386_game_drive.py)')
        image, record = drives.resolve(found[0].stem)
    try:
        title = drives.title_of(record, args.game)
    except drives.DriveError as error:
        raise SimError(str(error)) from error
    return image, record, title


def game_failures(report: dict, cpu: str, opl2_expected: bool = True) -> list[str]:
    """The game checks, allowing a time-limit exit only after observed launch progress."""
    failures = list(report['failures'])
    launched = bool(report.get('game_launch'))
    if (report.get('end') or {}).get('reason') == 'limit' and launched:
        failures = [failure for failure in failures if not failure.startswith('machine_tb exited 1')]
    if not launched:
        failures.append('the game produced no screen or VGA progress after its launch command')
    opl2 = report['opl2'] or {}
    if opl2_expected and (not opl2.get('writes') or not opl2.get('nonzero')):
        failures.append(f'the game did not play the OPL2: {opl2}')
    if cpu == 'rtl':
        failures += rtl_failures(report, 'game')
    return failures


def run_game(args, linked: list[Path]) -> dict:
    """A title from a prebuilt game drive, typed at the games image's prompt;
    the drive is read in place (machine_tb never writes it back)."""
    disk, record, title = game_drive(args)
    suite = game_suite(args)
    work = OUTPUT / suite
    work.mkdir(parents=True, exist_ok=True)
    args.limit_seconds = args.limit_seconds_given or title.get('limit', 20.0)
    args.type, args.expect = title['launch'], ''
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    wav, opl2_log = work / f'{args.game}.wav', work / f'{args.game}-opl2.log'
    keys = [arg for seconds, text in ([key.split(':', 1) for key in args.game_keys] or title.get('keys', []))
            for arg in ('--keys', f'{seconds}:{text}')]
    frames = work / 'frames'
    shutil.rmtree(frames, ignore_errors=True)
    frames.mkdir(parents=True)
    extra = ['--frame-every', str(args.frame_every)] if args.frame_every else []
    if args.rtl_at_prompt:
        extra += ['--rtl-at-prompt', '1']
    if args.instruction_census:
        extra += ['--census', str(work / 'census.txt')]
    if args.oracle_profile:
        extra += ['--profile', str(work / 'profile.txt')]
    if args.boot_entry != 1:
        extra += ['--boot-key', f'{BOOT_MENU_KEY_SECONDS}:{args.boot_entry}']
    report = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                            '--type', args.type, '--keys-after-type', '1', '--wav', str(wav),
                                            '--opl2-log', str(opl2_log), '--frames', str(frames), *keys, *extra,
                                            '--cpu', args.cpu, '--game-launch', '1', *governor_arguments(args)], images)
    report['game'] = {'name': args.game, 'drive': record.get('drive'), 'drive_sha256': record['sha256'],
                      'title': title['title'], 'bucket': title['bucket'],
                      'source': title.get('source', 'exodos'), 'source_sha256': title['sha256']}
    report['work'] = str(work)
    # A program that ends at the prompt ends the run there; that is not a bench failure.
    if (report.get('end') or {}).get('reason') == 'prompt' and report.get('game_launch'):
        report['failures'] = [failure for failure in report['failures'] if not failure.startswith('machine_tb exited')]
    report['failures'] = game_failures(report, args.cpu, title.get('opl2', False))
    if wav.exists():
        report['audio'] = tone(read_wav(wav))
    return report


def game_suite(args) -> str:
    return f'game-{args.game}' + ('-rtl' if args.cpu == 'rtl' else '') + suffix(args)


WFW_DRIVE, WFW_TITLE = 'wfw-msdos', 'wfw'
WFW_FRAMES_IN_A_ROW = 2


def wfw_suite(args) -> str:
    return 'wfw' + ('-rtl' if args.cpu == 'rtl' else '') + suffix(args)


def wfw_drive(args) -> tuple[Path, dict, dict] | str:
    """The WfW drive and its title, or why the suite skips: the drive is
    user-supplied, so a host without it (CI) skips rather than fails."""
    if not args.drive and not (drives.configured_store() / 'by-name' / f'{WFW_DRIVE}.img').exists():
        return f'no WfW drive: {WFW_DRIVE} is not in {drives.STORE}/by-name (docs/pc386-windows-corpus.md)'
    # A present drive that fails its checks, or a named one that does not resolve, fails.
    try:
        image, record = drives.resolve(args.drive or WFW_DRIVE)
    except drives.DriveError as error:
        raise SimError(str(error)) from None
    try:
        return image, record, drives.title_of(record, WFW_TITLE)
    except drives.DriveError as error:
        raise SimError(str(error)) from None


class ProgramManagerWatch:
    """Reads machine_tb's frames as they are written and creates the stop
    file once WFW_FRAMES_IN_A_ROW frames in a row show Program Manager;
    found is the first frame of that streak."""

    def __init__(self, frames: Path, stop: Path):
        self.frames, self.stop = frames, stop
        self.seen: set[str] = set()
        self.results: list[dict] = []
        self.streak: list[dict] = []
        self.found = None

    def __call__(self):
        if self.found:
            return
        for path in sorted(self.frames.glob('t*.ppm')):
            if path.name in self.seen:
                continue
            try:
                frame = wfw.parse_ppm(path.read_bytes())
            except (OSError, wfw.FrameError):
                return  # still being written: read it next time
            self.seen.add(path.name)
            seconds = float(path.stem[1:])
            found = wfw.program_manager(frame)
            self.results.append({'frame': path.name, 'seconds': seconds, 'program_manager': found['found'],
                                 'reason': found['reason']})
            if not found['found']:
                self.streak = []
                continue
            self.streak.append({'frame': path.name, 'seconds': seconds, 'caption': found['caption'],
                                'group': found['group']})
            if len(self.streak) >= WFW_FRAMES_IN_A_ROW:
                self.found = self.streak[0]
                self.stop.touch()
                return


def run_wfw(args, linked: list[Path], disk: Path, record: dict, title: dict) -> dict:
    """WfW 3.11 from the DOS prompt to Program Manager (docs/pc386-windows-corpus.md)."""
    suite = wfw_suite(args)
    work = OUTPUT / suite
    work.mkdir(parents=True, exist_ok=True)
    args.limit_seconds = args.limit_seconds_given or title.get('limit', 12.0)
    args.type, args.expect = title['launch'], ''
    images = firmware.images(args.download, args.jobs)
    testbench = machine_testbench(args, linked)
    frames, stop = work / 'frames', work / 'stop'
    shutil.rmtree(frames, ignore_errors=True)
    frames.mkdir(parents=True)
    stop.unlink(missing_ok=True)
    every = args.frame_every or 0.25
    extra = ['--rtl-at-prompt', '1'] if args.cpu == 'rtl' else []
    watch = ProgramManagerWatch(frames, stop)
    print(f'Running WfW 3.11 on drive {record.get("drive")} ({record["sha256"][:16]}) to Program Manager on the '
          f'{args.cpu} CPU; frames every {every} s, limit {args.limit_seconds} s', flush=True)
    report = run_machine(testbench, suite, ['--disk', str(disk), '--limit-seconds', str(args.limit_seconds),
                                            '--type', args.type, '--keys-after-type', '1',
                                            '--frames', str(frames), '--frame-every', str(every),
                                            '--stop-file', str(stop), *extra, '--cpu', args.cpu,
                                            '--game-launch', '1', *governor_arguments(args)], images, watch)
    stop.unlink(missing_ok=True)
    launch = (report.get('game_launch') or {}).get('seconds')
    report['wfw'] = {'drive': record.get('drive'), 'drive_sha256': record['sha256'], 'title': title['title'],
                     'launch_seconds': launch, 'program_manager': watch.found, 'frames': watch.results}
    if watch.found and launch is not None:
        report['wfw']['program_manager_after_win'] = round(watch.found['seconds'] - launch, 3)
    report['work'] = str(work)
    report['failures'] = wfw_failures(report, args.cpu)
    report['checks'] = wfw_checks(report)
    return report


def wfw_failures(report: dict, cpu: str) -> list[str]:
    """Program Manager seen, the run ended for it, and on the RTL no stop or oracle step."""
    failures = [failure for failure in report['failures'] if not failure.startswith('OPL2 against')]
    end = (report.get('end') or {}).get('reason')
    if not report.get('game_launch'):
        failures.append('`win` made no visible progress at the DOS prompt')
    if not report['wfw'].get('program_manager'):
        seen = report['wfw']['frames'][-1] if report['wfw']['frames'] else None
        failures.append(f'Program Manager did not appear by {(report.get("end") or {}).get("seconds")} simulated '
                        f'seconds (last frame: {seen})')
    elif end != 'stop-file':
        failures.append(f'the run ended as {end}, not at Program Manager')
    if cpu == 'rtl':
        failures += rtl_failures(report, 'wfw')
    return failures


def wfw_checks(report: dict) -> list[str]:
    found = report['wfw'].get('program_manager')
    checks = [f'WIN typed at {report["wfw"]["launch_seconds"]} s']
    if found:
        checks.append(f'Program Manager at {found["seconds"]} s ({report["wfw"].get("program_manager_after_win")} s '
                      f'after win), frame {found["frame"]}')
    rtl = (report.get('rtl') or {}).get('counters')
    if rtl:
        checks.append(f'RTL retired {rtl.get("retired")} ({rtl.get("pe_retired")} with PE set), '
                      f'oracle steps {rtl.get("oracle_steps")}')
    checks.append(f'END {report.get("end")}')
    return checks


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--suite', choices=('devices', 'storage', 'audio', 'opl2', 'benches', 'boot', 'rtl-stops',
                                            'a20', 'c2xx', 'ems', 'wait', 'speaker', 'scroll', 'cpu-profile',
                                            'sound', 'game', 'wfw', 'vga', 'system', 'governor', 'all'),
                        default='all',
                        help='simulation suite to run')
    parser.add_argument('--download', action='store_true', help='fetch Bochs, dev86, FreeDOS and NASM if missing')
    parser.add_argument('--rebuild-image', action='store_true', help='rebuild the FreeDOS fixture')
    parser.add_argument('--image', choices=sorted(freedos.PROFILES), default='boot',
                        help='boot: the FreeDOS image profile to boot (build_pc386_freedos_image.py)')
    parser.add_argument('--limit-seconds', type=float, default=None, help='simulated seconds before giving up')
    parser.add_argument('--game', default='wolf3d', help="game: the title to start, by its id in the drive's record")
    parser.add_argument('--drive', default=None,
                        help='game and wfw: the prebuilt drive (build_pc386_game_drive.py) by path, SHA-256 or '
                             'name; default: the drive in the store that holds --game, or wfw-msdos')
    parser.add_argument('--frame-every', type=float, default=0,
                        help='game and wfw: write the captured frame every this many simulated seconds '
                             '(wfw: 0.25 by default)')
    parser.add_argument('--rtl-at-prompt', action='store_true',
                        help='game --cpu rtl: boot on the oracle and hand over to the RTL at the prompt')
    parser.add_argument('--instruction-census', action='store_true',
                        help="game: write the oracle's instruction census (machine_tb --census) to census.txt")
    parser.add_argument('--game-keys', action='append', default=[], metavar='SECONDS:TEXT',
                        help="game: keys after the launch in place of the drive record's ('|' is Enter); repeat")
    parser.add_argument('--boot-entry', type=int, default=1, choices=(1, 2, 3),
                        help="game: the games image's boot menu entry (3: with the EMS board's driver)")
    parser.add_argument('--oracle-profile', action='store_true',
                        help="game --cpu oracle: write machine_tb's profile (each real-mode INT by AH, such as "
                             'int67.4f for EMS) to profile.txt')
    parser.add_argument('--update-fixture', action='store_true',
                        help='sound: copy the captured OPL2 port log to sim/fixtures/sound-opl2.log')
    parser.add_argument('--type', default=None, help="keys typed at the prompt; '|' is Enter")
    parser.add_argument('--expect', default=None, help='text the screen must show at the end')
    parser.add_argument('--cpu', choices=('oracle', 'rtl'), default='oracle',
                        help='boot and game: the CPU, the Bochs oracle or fes_i386_cpu (which the oracle backs on '
                             'its stops)')
    parser.add_argument('--preset', type=int, default=0, choices=range(8),
                        help="system, boot and game --cpu rtl: the governor's preset (fes.pc386.machine word 0); "
                             'governor: the one preset beside max')
    parser.add_argument('--frequency', type=int, default=0, help="the governor's frequency override, 100 kHz units")
    parser.add_argument('--turbo', type=int, default=0, choices=(0, 1), help='turbo at launch: 1 runs at max')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    args = parser.parse_args()
    if args.cpu == 'rtl' and args.suite not in ('boot', 'game', 'wfw', 'a20', 'c2xx', 'ems', 'wait', 'speaker',
                                                'scroll'):
        parser.error('--cpu rtl runs the boot, game, wfw, a20, c2xx, ems, wait, speaker and scroll suites only')
    if args.oracle_profile and not (args.suite == 'game' and args.cpu == 'oracle'):
        parser.error('--oracle-profile is for the game suite with --cpu oracle')
    if args.rtl_at_prompt and not (args.suite == 'game' and args.cpu == 'rtl'):
        parser.error('--rtl-at-prompt is for the game suite with --cpu rtl')
    if args.image != 'boot' and args.suite != 'boot':
        parser.error('--image is supported by the boot suite only')
    if (args.preset or args.frequency or args.turbo) and not (
            args.suite in ('system', 'governor') or args.suite in ('boot', 'game', 'wfw') and args.cpu == 'rtl'):
        parser.error('the governor runs on the RTL CPU: system, governor, or boot, game and wfw with --cpu rtl')
    if (args.frequency or args.turbo) and not args.preset:
        parser.error('--frequency and --turbo set a preset: give --preset too')
    if args.frequency and not 10 <= args.frequency <= 1000:
        parser.error('--frequency is 10 to 1000 (100 kHz units), as the record allows')
    limit_seconds, typed, expect = args.limit_seconds, args.type, args.expect
    args.limit_seconds_given = limit_seconds
    if shutil.which(args.verilator) is None:
        print(f'selected Verilator not found: {args.verilator}', file=sys.stderr)
        return 2
    failed = False
    try:
        suites = ('devices', 'storage', 'audio', 'opl2', 'benches', 'boot', 'vga', 'sound') if args.suite == 'all' \
            else (args.suite,)
        for suite in suites:
            args.limit_seconds = limit_seconds or default_limit_seconds(suite)
            args.type = typed or ('sound|' if suite == 'sound' else 'dir|')
            args.expect = expect or ('SOUND DONE' if suite == 'sound' else 'bytes free')
            if suite == 'devices':
                report = run_devices(args)
            elif suite == 'storage':
                report = run_storage(args)
            elif suite == 'audio':
                report = run_audio(args)
            elif suite == 'opl2':
                report = run_opl2(args)
            elif suite == 'benches':
                report = run_benches(args)
            elif suite == 'system':
                report = run_system(args)
            elif suite == 'governor':
                report = run_governor(args)
            elif suite == 'sound':
                report = run_sound(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'game':
                report = run_game(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'wfw':
                found = wfw_drive(args)
                if isinstance(found, str):
                    print(f'SKIP wfw: {found}', flush=True)
                    continue
                report = run_wfw(args, oracle.link_inputs(args.jobs, args.download), *found)
            elif suite == 'rtl-stops':
                report = run_rtl_stops(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'a20':
                report = run_a20(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'c2xx':
                report = run_c2xx(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'ems':
                report = run_ems(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'wait':
                report = run_wait(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'speaker':
                report = run_speaker(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'scroll':
                report = run_scroll(args, oracle.link_inputs(args.jobs, args.download))
            elif suite == 'cpu-profile':
                report = run_cpu_profile(args, oracle.link_inputs(args.jobs, args.download))
            else:
                run = run_boot if suite == 'boot' else run_vga
                report = run(args, oracle.link_inputs(args.jobs, args.download))
            name = (game_suite(args) if suite == 'game' else wfw_suite(args) if suite == 'wfw' else
                    boot_suite(args) if suite == 'boot' else suite + ('-rtl' if args.cpu == 'rtl' else '') + suffix(args))
            (OUTPUT / name).mkdir(parents=True, exist_ok=True)
            (OUTPUT / name / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
            if suite == 'system':
                for row in report['screen']:
                    print('|' + row + '|')
                print('STATUS', report['status'])
                print('DISK', report['disk'], 'SAVE', report['save'])
                for check in report['record_checks']:
                    print('CHECK', 'PASS' if check['pass'] else 'FAIL', check['check'])
                print('END', report['end'])
            elif suite in ('boot', 'vga', 'sound', 'game'):
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
                if 'rtl' in report:
                    print('RTL', report['rtl']['counters'])
                    for gap in report['rtl']['gaps']:
                        print('GAP', gap['kind'], gap['stem'], gap['name'] or '', 'built' if gap['built'] else 'unbuilt',
                              f'stops={gap["stops"]} sites={gap["sites"]} first={gap["first_site"]}')
                print('END', report['end'])
            elif suite != 'cpu-profile':  # run_cpu_profile printed its window
                print('\n'.join(report['checks']))
            for failure in report['failures']:
                print('FAIL', failure)
            failed |= bool(report['failures'])
            print(('FAIL' if report['failures'] else 'PASS') + f' {suite}', flush=True)
    except (SimError, firmware.FirmwareError, freedos.ImageError, drives.DriveError, oracle.OracleError,
            test386.Test386Error) as error:
        print(f'sim_fes_pc386: {error}', file=sys.stderr)
        return 1
    return 1 if failed else 0


if __name__ == '__main__':
    sys.exit(main())
