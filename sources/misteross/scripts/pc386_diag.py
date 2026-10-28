#!/usr/bin/env python3
"""Programs and results of the pc386 M4 diagnostic (docs/pc386-machine-design.md, phase M4).

pc386_bus_master runs commands from a ring in the DDR window. A program is
the list of those commands plus what the runner does between them: stage
bytes into the window, mark a point (and hold there for an HDMI grab), dump
window bytes, dump the newest VGA capture. The same program file runs on the
host bench (cores/fes-pc386/sim/diag_tb.cpp) and on a kit
(cores/fes-pc386/diagnostic, the pc386-diag tool), and both write the same
results file, which this module checks.

Program file, little-endian: b"P386PRG1", u32 system clock in Hz, records:
  C  u8 op, u8 flags, u16 count, u32 address, u32 data, u32 mask, u32 value
     (a command; its result data must satisfy data & mask == value)
  S  u32 window offset, u32 length, bytes          (stage, after a drain)
  M  u8 length, name, u32 hold milliseconds          (mark, after a drain)
  D  u32 window offset, u32 length                   (dump, after a drain)
  F  the newest capture, using the two results before it (READ 6, READ 7)
  E  end
Results file: b"P386RES1", records:
  R  the 16-byte result beat: data, clocks, status, epoch + sequence
  S  u32 offset, u32 length, u64 nanoseconds the stage took
  M  u8 length, name, u64 nanoseconds since the start, u32 commands done
  D  u32 offset, u32 length, bytes
  F  u32 width, u32 height, u32 buffer, u32 frame count, width x height
     32-bit pixels (0x00RRGGBB)
  E  u64 nanoseconds in all

  python3 scripts/pc386_diag.py write --program smoke --clock-hz 61440000 out.p386
  python3 scripts/pc386_diag.py check out.p386 out.r386 [--reference sim.r386]
"""
from __future__ import annotations

import argparse
from dataclasses import dataclass, field
from fractions import Fraction
import json
import math
from pathlib import Path
import struct
import sys

PROGRAM_MAGIC = b'P386PRG1'
RESULT_MAGIC = b'P386RES1'
WINDOW_BYTES = 0x10000000
STAGING = 0x01100000            # the runner's staging area for COPY_IN
GUEST = 0x04000000              # guest RAM at the 8237's address
IMAGE = 0x08000000              # ATA image
CAPTURE = 0x00a00000
CAPTURE_BYTES = 0x00200000
CAPTURE_STRIDE = 3200

OPS = {name: number for number, name in enumerate((
    'NOP', 'OUT', 'IN', 'MMIO_WR', 'MMIO_RD', 'FILL', 'COPY_IN', 'COPY_OUT', 'INTA', 'WAIT_INTR',
    'DELAY', 'POLL_IN', 'CONFIG', 'READ', 'KEY'))}
SIZES = {1: 0, 2: 1, 4: 2}
# READ indices (pc386_diag.sv).
CAPTURE_DROPS, PRESENT_UNDERFLOWS, PLANE_UNDERFLOWS, SCANOUT_UNDERFLOWS, FRAMES_SHOWN, STATUS, \
    CAPTURE_SIZE, CAPTURE_FRAME, DIRTY_WORD, MIXER_SAMPLE, MIXER_SAMPLES, OPL2_SAMPLES, DMA_CYCLES, \
    DSP_ACKS, DEBUG_BYTES, RESETS, CLOCK = range(17)
COUNTERS = {'capture_drops': CAPTURE_DROPS, 'present_underflows': PRESENT_UNDERFLOWS,
            'plane_underflows': PLANE_UNDERFLOWS, 'scanout_underflows': SCANOUT_UNDERFLOWS}
# CONFIG indices.
HOLD, BASE_KIB, EXTENDED_KIB, ABOVE16M, SB_BASE_HIGH, SB_IRQ, SB_DMA, DISK_PRESENT, DISK_SECTORS, \
    DISK_CYLINDERS, DISK_HEADS, DISK_SPT, DIRTY_INDEX = range(13)


class DiagError(Exception):
    pass


# Every guest time base as a rational enable of the system clock
# (docs/pc386-machine-design.md, "Clocks and timing"): N/D = rate / clock.
GUEST_RATES = {
    'ISA': Fraction(25_000_000, 3),           # 8.33 MHz ISA clock
    'PIT': 2 * Fraction(105_000_000, 88),     # both edges of the 1,193,181.8 Hz PIT clock
    'RTC': Fraction(32_768),
    'US': Fraction(1_000_000),                # 8042 pacing, DSP time base
    'DMA': Fraction(25_000_000, 6),           # 4.17 MHz DMA clock
    'DOT0': Fraction(25_175_000),
    'DOT1': Fraction(28_322_000),
    'OPL2': Fraction(315_000_000, 88 * 72),   # 49,715.9 Hz OPL2 samples
    'AUDIO': Fraction(48_000),                # the mixer's frame request
}
ATA_RESET_SECONDS = Fraction(1, 100_000)      # 10 us


def rates(clock_hz: int) -> dict[str, int]:
    """pc386_diag's N/D parameters and ATA_RESET_CLOCKS for a system clock."""
    if clock_hz < 2 * GUEST_RATES['DOT1']:
        raise DiagError('the system clock must be at least twice the 28.322 MHz dot clock')
    parameters = {}
    for name, rate in GUEST_RATES.items():
        ratio = rate / clock_hz
        parameters[f'{name}_N'] = ratio.numerator
        parameters[f'{name}_D'] = ratio.denominator
    parameters['ATA_RESET_CLOCKS'] = math.ceil(ATA_RESET_SECONDS * clock_hz)
    return parameters


@dataclass
class Command:
    op: int
    flags: int = 0
    count: int = 0
    address: int = 0
    data: int = 0
    mask: int = 0
    value: int = 0
    note: str = ''


@dataclass
class Program:
    clock_hz: int
    records: list = field(default_factory=list)

    # ---- Commands ---------------------------------------------------------
    def command(self, op: str, *, flags=0, count=0, address=0, data=0, mask=0, value=0, note=''):
        self.records.append(Command(OPS[op], flags, count, address & 0xffffffff, data & 0xffffffff,
                                    mask & 0xffffffff, value & 0xffffffff, note))

    def out(self, port: int, value: int, size: int = 1):
        self.command('OUT', flags=SIZES[size], count=port, data=value)

    def inp(self, port: int, size: int = 1, expect: int | None = None, mask: int | None = None, note=''):
        mask = ((1 << (8 * size)) - 1 if mask is None else mask) if expect is not None else 0
        self.command('IN', flags=SIZES[size], count=port, mask=mask, value=0 if expect is None else expect & mask,
                     note=note)

    def poll(self, port: int, mask: int, value: int, size: int = 1):
        self.command('POLL_IN', flags=SIZES[size], count=port, address=mask, data=value)

    def mmio_write(self, address: int, value: int, enables: int = 0xf):
        self.command('MMIO_WR', flags=enables, address=address, data=value)

    def mmio_read(self, address: int, expect: int | None = None, mask: int = 0xffffffff):
        self.command('MMIO_RD', address=address, mask=0 if expect is None else mask,
                     value=0 if expect is None else expect & mask)

    def fill(self, address: int, value: int, dwords: int):
        for start in range(0, dwords, 0xffff):
            n = min(0xffff, dwords - start)
            self.command('FILL', count=n, address=address + 4 * start, data=value)

    def copy_in(self, address: int, data: bytes):
        """Stage data and copy it to MMIO, 0xfff0 dwords (a whole number of beats) at a time."""
        if len(data) % 4:
            raise DiagError('COPY_IN data must be whole dwords')
        self.stage(STAGING, data)
        dwords = len(data) // 4
        for start in range(0, dwords, 0xfff0):
            n = min(0xfff0, dwords - start)
            self.command('COPY_IN', count=n, address=address + 4 * start, data=STAGING + 4 * start)

    def copy_out(self, address: int, dwords: int, offset: int):
        self.command('COPY_OUT', count=dwords, address=address, data=offset)

    def inta(self, expect: int | None = None):
        self.command('INTA', mask=0 if expect is None else 0xff, value=0 if expect is None else expect)

    def wait_intr(self, seconds: float, expect: bool = True):
        self.command('WAIT_INTR', data=self.clocks(seconds), mask=1 if expect else 0, value=1 if expect else 0)

    def delay(self, seconds: float):
        clocks = self.clocks(seconds)
        while clocks:
            n = min(clocks, 0xffffffff)
            self.command('DELAY', data=n)
            clocks -= n

    def delay_clocks(self, clocks: int):
        if clocks:
            self.command('DELAY', data=clocks)

    def config(self, index: int, value: int):
        self.command('CONFIG', address=index, data=value)

    def read(self, index: int, expect: int | None = None, mask: int = 0xffffffff, note=''):
        self.command('READ', address=index, mask=0 if expect is None else mask,
                     value=0 if expect is None else expect & mask, note=note)

    def key(self, code: int):
        self.command('KEY', data=code)

    # ---- Runner records ---------------------------------------------------
    def stage(self, offset: int, data: bytes):
        if offset % 16 or len(data) % 4 or offset + len(data) > WINDOW_BYTES:
            raise DiagError('a stage is beat-aligned, whole dwords and inside the window')
        self.records.append(('S', offset, bytes(data)))

    def mark(self, name: str, hold_ms: int = 0):
        self.records.append(('M', name, hold_ms))

    def dump(self, offset: int, length: int):
        if offset % 4 or length % 4:
            raise DiagError('a dump is whole dwords')
        self.records.append(('D', offset, length))

    def frame(self):
        self.read(CAPTURE_SIZE)
        self.read(CAPTURE_FRAME)
        self.records.append(('F',))

    def counters(self, zero: bool = True):
        """The port 1 and port 2 underflow and drop counters, required zero."""
        for name, index in COUNTERS.items():
            self.read(index, expect=0 if zero else None, note=name)

    def clocks(self, seconds: float) -> int:
        return max(1, round(seconds * self.clock_hz))

    # ---- Machine setup ----------------------------------------------------
    def reset(self, *, disk: tuple[int, int, int, int] | None = None, sb_irq: int = 7, colour: int | None = None):
        """Hold the machine in reset, set the straps, release it. With a
        colour, set mode 13h and fill the screen with that palette index, as a
        BIOS would set a mode: the VGA's reset registers are no raster, and its
        scanout counts underflows on them."""
        self.config(HOLD, 1)
        self.config(SB_IRQ, sb_irq)
        self.config(DISK_PRESENT, int(disk is not None))
        if disk is not None:
            sectors, cylinders, heads, spt = disk
            self.config(DISK_SECTORS, sectors)
            self.config(DISK_CYLINDERS, cylinders)
            self.config(DISK_HEADS, heads)
            self.config(DISK_SPT, spt)
        self.delay_clocks(64)
        self.config(HOLD, 0)
        self.delay(20e-6)
        if colour is not None:
            set_mode13(self)
            self.fill(0xa0000, colour * 0x01010101, 16000)

    def pic_init(self, unmask: int):
        self.out(0x20, 0x11); self.out(0x21, 0x08); self.out(0x21, 0x04); self.out(0x21, 0x01)
        self.out(0xa0, 0x11); self.out(0xa1, 0x70); self.out(0xa1, 0x02); self.out(0xa1, 0x01)
        self.out(0x21, ~(1 << unmask) & 0xff)
        self.out(0xa1, 0xff)

    def encode(self) -> bytes:
        out = bytearray(PROGRAM_MAGIC + struct.pack('<I', self.clock_hz))
        for record in self.records:
            if isinstance(record, Command):
                out += b'C' + struct.pack('<BBHIIII', record.op, record.flags, record.count, record.address,
                                          record.data, record.mask, record.value)
            elif record[0] == 'S':
                out += b'S' + struct.pack('<II', record[1], len(record[2])) + record[2]
            elif record[0] == 'M':
                name = record[1].encode()
                out += b'M' + struct.pack('<B', len(name)) + name + struct.pack('<I', record[2])
            elif record[0] == 'D':
                out += b'D' + struct.pack('<II', record[1], record[2])
            else:
                out += b'F'
        return bytes(out + b'E')

    def commands(self) -> list[Command]:
        return [record for record in self.records if isinstance(record, Command)]


def decode_program(data: bytes) -> Program:
    if data[:8] != PROGRAM_MAGIC:
        raise DiagError('not a pc386 diagnostic program')
    program = Program(struct.unpack_from('<I', data, 8)[0])
    at = 12
    while True:
        kind = data[at:at + 1]; at += 1
        if kind == b'C':
            program.records.append(Command(*struct.unpack_from('<BBHIIII', data, at))); at += 20
        elif kind == b'S':
            offset, length = struct.unpack_from('<II', data, at); at += 8
            program.records.append(('S', offset, data[at:at + length])); at += length
        elif kind == b'M':
            length = data[at]; at += 1
            name = data[at:at + length].decode(); at += length
            program.records.append(('M', name, struct.unpack_from('<I', data, at)[0])); at += 4
        elif kind == b'D':
            program.records.append(('D', *struct.unpack_from('<II', data, at))); at += 8
        elif kind == b'F':
            program.records.append(('F',))
        elif kind == b'E':
            return program
        else:
            raise DiagError(f'bad program record {kind!r} at {at - 1}')


@dataclass
class Results:
    results: list = field(default_factory=list)      # (data, clocks, status, sequence)
    stages: list = field(default_factory=list)       # (offset, length, ns)
    marks: list = field(default_factory=list)        # (name, ns, done)
    dumps: list = field(default_factory=list)        # (offset, bytes)
    frames: list = field(default_factory=list)       # (width, height, buffer, count, bytes)
    total_ns: int = 0


def decode_results(data: bytes) -> Results:
    if data[:8] != RESULT_MAGIC:
        raise DiagError('not a pc386 diagnostic results file')
    results = Results()
    at = 8
    while at < len(data):
        kind = data[at:at + 1]; at += 1
        if kind == b'R':
            results.results.append(struct.unpack_from('<IIII', data, at)); at += 16
        elif kind == b'S':
            results.stages.append(struct.unpack_from('<IIQ', data, at)); at += 16
        elif kind == b'M':
            length = data[at]; at += 1
            name = data[at:at + length].decode(); at += length
            ns, done = struct.unpack_from('<QI', data, at); at += 12
            results.marks.append((name, ns, done))
        elif kind == b'D':
            offset, length = struct.unpack_from('<II', data, at); at += 8
            results.dumps.append((offset, data[at:at + length])); at += length
        elif kind == b'F':
            width, height, buffer, count = struct.unpack_from('<IIII', data, at); at += 16
            size = width * height * 4
            results.frames.append((width, height, buffer, count, data[at:at + size])); at += size
        elif kind == b'E':
            results.total_ns = struct.unpack_from('<Q', data, at)[0]
            return results
        else:
            raise DiagError(f'bad results record {kind!r} at {at - 1}')
    raise DiagError('results file ends without its end record')


# ---- Programs ---------------------------------------------------------------

def smoke(clock_hz: int) -> Program:
    """Devices behind the I/O port answer, and the isa8 cycle takes its 720 ns."""
    p = Program(clock_hz)
    p.reset(colour=0x20)
    p.out(0x80, 0x5a)
    p.read(STATUS, expect=0x5a00, mask=0xff00, note='POST code 5Ah')
    p.pic_init(7)
    p.out(0x21, 0xa5)
    p.inp(0x21, expect=0xa5, note='PIC 1 mask register')
    p.out(0xa1, 0x3c)
    p.inp(0xa1, expect=0x3c, note='PIC 2 mask register')
    p.out(0x70, 0x40); p.out(0x71, 0x6b)
    p.out(0x70, 0x40); p.inp(0x71, expect=0x6b, note='CMOS byte 40h')
    p.out(0x43, 0xb6); p.out(0x42, 0x34); p.out(0x42, 0x12)
    p.out(0x43, 0xe8)                                   # read-back status of counter 2
    p.inp(0x42, expect=0x36, mask=0x3f, note='8254 read-back status, counter 2')
    p.inp(0x3cc, note='VGA misc output')
    p.inp(0x1234, expect=0xff, note='an unclaimed port reads FFh')
    p.inp(0x22e, expect=0x00, mask=0x80, note='DSP read status: no data before a reset')
    for port in (0x80, 0x61, 0x3c4):
        p.out(port, 0x00)
        p.read(CLOCK)
    p.counters()
    return p


MODE13 = {
    'misc': 0x63,
    'sequencer': (0x03, 0x01, 0x0f, 0x00, 0x0e),
    'crtc': (0x5f, 0x4f, 0x50, 0x82, 0x54, 0x80, 0xbf, 0x1f, 0x00, 0x41, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
             0x9c, 0x0e, 0x8f, 0x28, 0x40, 0x96, 0xb9, 0xa3, 0xff),
    'graphics': (0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x05, 0x0f, 0xff),
    'attribute': tuple(range(16)) + (0x41, 0x00, 0x0f, 0x00, 0x00),
}


def mode13_palette(index: int) -> tuple[int, int, int]:
    """An injective 6-bit DAC palette: red and green together name the index."""
    return index >> 2, (index & 3) * 21, (index * 5) & 63


def mode13_pattern(x: int, y: int) -> int:
    return 255 if x in (0, 319) or y in (0, 199) else (x + 2 * y) & 255


def set_mode13(p: Program):
    p.out(0x3c2, MODE13['misc'])
    for index, value in enumerate(MODE13['sequencer']):
        p.out(0x3c4, index | value << 8, 2)
    p.out(0x3d4, 0x11 | 0x0e << 8, 2)                  # unlock CRTC 0-7
    for index, value in enumerate(MODE13['crtc']):
        p.out(0x3d4, index | value << 8, 2)
    for index, value in enumerate(MODE13['graphics']):
        p.out(0x3ce, index | value << 8, 2)
    p.inp(0x3da)
    for index, value in enumerate(MODE13['attribute']):
        p.out(0x3c0, index); p.out(0x3c0, value)
    p.out(0x3c0, 0x20)
    p.out(0x3c8, 0)
    for index in range(256):
        for component in mode13_palette(index):
            p.out(0x3c9, component)


def mode13(clock_hz: int) -> Program:
    """Mode 13h from registers, a staged picture copied into VRAM, read back,
    captured and shown."""
    p = Program(clock_hz)
    p.reset()
    set_mode13(p)
    picture = bytes(mode13_pattern(x, y) for y in range(200) for x in range(320))
    p.copy_in(0xa0000, picture)
    for address in (0xa0000, 0xa0000 + 4 * 1000, 0xa0000 + 63996):
        offset = address - 0xa0000
        p.mmio_read(address, expect=int.from_bytes(picture[offset:offset + 4], 'little'))
    p.copy_out(0xa0000, 16000, STAGING + 0x100000)
    p.dump(STAGING + 0x100000, 64000)
    p.delay(0.1)
    p.mark('mode13', 2500)
    p.frame()
    p.counters()
    return p


def check_mode13(results: Results, frame) -> list[str]:
    """The capture is the picture double-scanned, through one injective map."""
    width, height, _buffer, _count, pixels = frame
    if (width, height) != (320, 400):
        return [f'mode 13h capture is {width}x{height}, not 320x400']
    colours = {}
    for y in range(400):
        row = pixels[y * 1280:(y + 1) * 1280]
        for x in range(320):
            index = mode13_pattern(x, y // 2)
            colour = int.from_bytes(row[4 * x:4 * x + 4], 'little') & 0xffffff
            if colours.setdefault(index, colour) != colour:
                return [f'mode 13h capture pixel ({x}, {y}) is {colour:06x}, index {index} was {colours[index]:06x}']
    if len(set(colours.values())) != len(colours):
        return ['mode 13h capture maps two palette indices to one colour']
    return []


def opl2(clock_hz: int, log: Path) -> Program:
    """The OPL2 phrase SOUND.COM played, at its recorded timing (100 MHz cycles)."""
    p = Program(clock_hz)
    p.reset(colour=0x80)
    lines = [line.split() for line in log.read_text().splitlines() if line.strip()]
    start = int(lines[0][0])
    p.mark('opl2')
    previous = start
    for cycle, a0, value in lines:
        cycle = int(cycle)
        p.delay_clocks(round((cycle - previous) * clock_hz / 100_000_000))
        p.out(0x388 + int(a0), int(value, 16))
        previous = cycle
    p.delay(1.0)
    p.mark('opl2-end')
    p.read(OPL2_SAMPLES)
    p.counters()
    return p


def dsp_reset(p: Program, base: int = 0x220):
    p.out(base + 6, 1)
    p.delay(3e-6)
    p.out(base + 6, 0)
    p.poll(base + 0xe, 0x80, 0x80)
    p.inp(base + 0xa, expect=0xaa, note='DSP reset answers AAh')


def dsp_write(p: Program, value: int, base: int = 0x220):
    p.poll(base + 0xc, 0x80, 0x00)
    p.out(base + 0xc, value)


def dma1(p: Program, address: int, count_minus_one: int, mode: int):
    p.out(0x0a, 0x05); p.out(0x0c, 0)
    p.out(0x02, address & 0xff); p.out(0x02, address >> 8 & 0xff); p.out(0x83, address >> 16 & 0xff)
    p.out(0x03, count_minus_one & 0xff); p.out(0x03, count_minus_one >> 8)
    p.out(0x0b, mode); p.out(0x0a, 0x01)


def sine(samples: int, rate: float, frequency: float, amplitude: int = 100) -> bytes:
    return bytes(128 + round(amplitude * math.sin(2 * math.pi * frequency * n / rate)) for n in range(samples))


DSP_TIME_CONSTANT = 0xd3                     # 256 - 1e6 / 22222
DSP_RATE = 1e6 / (256 - DSP_TIME_CONSTANT)
DSP_TONE_HZ = 1000.0
SPEAKER_COUNT = 2712                         # 1,193,182 / 2712 = 440 Hz
SPEAKER_HZ = 1_193_181.8 / SPEAKER_COUNT


def sound(clock_hz: int, *, speaker_seconds: float = 1.5, auto_seconds: float = 3.0, block: int = 2048) -> Program:
    """PC speaker 440 Hz, then DSP single-cycle and auto-init DMA of a 1 kHz sine.
    The bench runs it shortened."""
    p = Program(clock_hz)
    p.reset(colour=0x40)
    p.mark('speaker')
    p.out(0x43, 0xb6); p.out(0x42, SPEAKER_COUNT & 0xff); p.out(0x42, SPEAKER_COUNT >> 8)
    p.out(0x61, 0x03)
    p.delay(speaker_seconds)
    p.out(0x61, 0x00)
    p.mark('speaker-end')
    dsp_reset(p)
    dsp_write(p, 0xe1)
    p.poll(0x22e, 0x80, 0x80); p.inp(0x22a, expect=0x02, note='DSP version major')
    p.poll(0x22e, 0x80, 0x80); p.inp(0x22a, expect=0x01, note='DSP version minor')
    p.pic_init(7)
    dsp_write(p, 0xd1)
    p.stage(GUEST + 0x20000, sine(2 * block, DSP_RATE, DSP_TONE_HZ))
    last = block - 1
    # Single cycle: one block, one IRQ 7 (vector 0Fh) at its end.
    dma1(p, 0x20000, last, 0x49)
    dsp_write(p, 0x40); dsp_write(p, DSP_TIME_CONSTANT)
    p.mark('dsp-single')
    dsp_write(p, 0x14); dsp_write(p, last & 0xff); dsp_write(p, last >> 8)
    p.wait_intr(block / DSP_RATE + 0.05)
    p.inta(expect=0x0f)
    p.inp(0x22e)
    p.out(0x20, 0x20)
    p.read(DSP_ACKS, expect=block, note='single cycle plays one block')
    # Auto-init: a two-block ring, an IRQ per block, then DAh ends it.
    dma1(p, 0x20000, 2 * block - 1, 0x59)
    dsp_write(p, 0x48); dsp_write(p, last & 0xff); dsp_write(p, last >> 8)
    p.mark('dsp-auto')
    dsp_write(p, 0x1c)
    blocks = max(1, round(auto_seconds * DSP_RATE / block))
    for n in range(blocks + 1):
        if n == blocks:
            dsp_write(p, 0xda)
        p.wait_intr(block / DSP_RATE + 0.05)
        p.inta(expect=0x0f)
        p.inp(0x22e)
        p.out(0x20, 0x20)
    dsp_write(p, 0xd3)
    p.mark('dsp-end')
    p.read(DSP_ACKS, expect=block * (blocks + 2), note='auto-init plays every block')
    p.read(MIXER_SAMPLES)
    p.read(CLOCK)
    p.counters()
    return p


DISK = (16 * 16 * 63, 16, 16, 63)            # sectors, cylinders, heads, sectors per track


def disk_image(sectors: int) -> bytes:
    image = bytearray()
    for lba in range(sectors):
        sector = bytearray((lba * 7 + i * 13 + (i >> 8)) & 0xff for i in range(512))
        sector[0:4] = lba.to_bytes(4, 'little')
        image += sector
    return bytes(image)


def written_sector(lba: int) -> bytes:
    return bytes((lba + 3 * i) & 0xff ^ 0xa5 for i in range(512))


ATA_READ_LBA, ATA_WRITE_LBA = 1000, 4000


def ata(clock_hz: int, image: bytes | None = None) -> Program:
    """Load an image into the window, then IDENTIFY, READ SECTORS and WRITE
    SECTORS through the ATA ports, and the dirty map."""
    sectors, cylinders, heads, spt = DISK
    image = disk_image(sectors) if image is None else image
    p = Program(clock_hz)
    p.config(HOLD, 1)
    p.mark('disk-load')
    p.stage(IMAGE, image)
    p.mark('disk-loaded')
    p.reset(disk=DISK, colour=0xc0)
    p.delay(100e-6)
    p.poll(0x1f7, 0x80, 0x00)
    p.out(0x1f6, 0xa0); p.out(0x1f7, 0xec)
    p.poll(0x1f7, 0x88, 0x08)
    identify = {1: cylinders, 3: heads, 6: spt, 60: sectors & 0xffff, 61: sectors >> 16}
    for word in range(256):
        p.inp(0x1f0, 2, expect=identify.get(word), note=f'IDENTIFY word {word}')
    p.out(0x1f2, 2); p.out(0x1f3, ATA_READ_LBA & 0xff); p.out(0x1f4, ATA_READ_LBA >> 8); p.out(0x1f5, 0)
    p.out(0x1f6, 0xe0); p.out(0x1f7, 0x20)
    for n in range(2):
        p.poll(0x1f7, 0x88, 0x08)
        sector = image[(ATA_READ_LBA + n) * 512:(ATA_READ_LBA + n + 1) * 512]
        for word in range(256):
            p.inp(0x1f0, 2, expect=int.from_bytes(sector[2 * word:2 * word + 2], 'little'),
                  note=f'READ SECTORS LBA {ATA_READ_LBA + n} word {word}')
    data = written_sector(ATA_WRITE_LBA)
    p.out(0x1f2, 1); p.out(0x1f3, ATA_WRITE_LBA & 0xff); p.out(0x1f4, ATA_WRITE_LBA >> 8); p.out(0x1f5, 0)
    p.out(0x1f6, 0xe0); p.out(0x1f7, 0x30)
    p.poll(0x1f7, 0x88, 0x08)
    for word in range(256):
        p.out(0x1f0, int.from_bytes(data[2 * word:2 * word + 2], 'little'), 2)
    p.poll(0x1f7, 0x88, 0x00)
    p.delay(200e-6)
    p.inp(0x1f7, expect=0x50, mask=0xf9, note='status after WRITE SECTORS')
    p.config(DIRTY_INDEX, ATA_WRITE_LBA >> 11)
    p.delay_clocks(8)
    bit = 1 << (ATA_WRITE_LBA >> 7 & 15)
    p.read(DIRTY_WORD, expect=bit, mask=bit, note='the written chunk is dirty')
    p.dump(IMAGE + ATA_WRITE_LBA * 512, 512)
    p.dump(IMAGE + (ATA_WRITE_LBA + 1) * 512, 512)
    return p


def check_ata(program: Program, results: Results) -> list[str]:
    errors = []
    if len(results.dumps) != 2:
        return ['ATA program results lack the image dumps']
    if results.dumps[0][1] != written_sector(ATA_WRITE_LBA):
        errors.append(f'the image does not hold the sector written to LBA {ATA_WRITE_LBA}')
    expected = disk_image(ATA_WRITE_LBA + 2)[(ATA_WRITE_LBA + 1) * 512:]
    if results.dumps[1][1] != expected:
        errors.append(f'WRITE SECTORS changed LBA {ATA_WRITE_LBA + 1}')
    return errors


def vga_trace(clock_hz: int, trace: Path, hold_ms: int = 2500) -> Program:
    """A VGA trace from the vga or boot suite (sim/machine_tb.cpp --vga-trace):
    every VGA port and MMIO cycle in order. A run of reads of one port that the
    program polled becomes POLL_IN commands on the bit that changed, so retrace
    waits stay waits. At each mark the runner holds and dumps the capture."""
    p = Program(clock_hz)
    p.reset()
    polls: list[tuple[int, int, int]] = []          # port, size, value

    def flush():
        if not polls:
            return
        port, size, _value = polls[0]
        values = [value for _port, _size, value in polls]
        changed = 0
        for a, b in zip(values, values[1:]):
            changed |= a ^ b
        # Vertical retrace (bit 3) decides a 3DAh wait if it moved; otherwise
        # the display-enable bit does.
        bit = 0x08 if changed & 0x08 else changed & -changed
        if bit == 0:
            p.inp(port, size)
        else:
            for a, b in zip(values, values[1:]):
                if (a ^ b) & bit:
                    p.poll(port, bit, b & bit, size)
        polls.clear()

    run: list[tuple[int, int]] = []                  # contiguous full-dword writes: address, value

    def flush_writes():
        if not run:
            return
        if len(run) < 16:
            for address, value in run:
                p.mmio_write(0xa0000 + address, value)
        elif all(value == run[0][1] for _address, value in run):
            p.fill(0xa0000 + run[0][0], run[0][1], len(run))
        else:
            p.copy_in(0xa0000 + run[0][0], b''.join(value.to_bytes(4, 'little') for _address, value in run))
        run.clear()

    with trace.open() as stream:
        for line in stream:
            fields = line.split()
            if not fields or fields[0] == '#':
                continue
            kind = fields[0]
            if kind == 'I' and fields[2] in ('3da', '3ba') and (not polls or polls[0][0] == int(fields[2], 16)):
                flush_writes()
                polls.append((int(fields[2], 16), int(fields[3]), int(fields[4], 16)))
                continue
            flush()
            if kind == 'W':
                address, enables, value = int(fields[2], 16), int(fields[3], 16), int(fields[4], 16)
                if enables == 0xf and (not run or run[-1][0] + 4 == address) and len(run) < 0x40000:
                    run.append((address, value))
                    continue
                flush_writes()
                if enables == 0xf:
                    run.append((address, value))
                else:
                    p.mmio_write(0xa0000 + address, value, enables)
                continue
            flush_writes()
            if kind == 'O':
                p.out(int(fields[2], 16), int(fields[4], 16), int(fields[3]))
            elif kind == 'I':
                port, size, value = int(fields[2], 16), int(fields[3]), int(fields[4], 16)
                if port in (0x3da, 0x3ba):
                    polls.append((port, size, value))
                else:
                    p.inp(port, size, expect=value, note=f'VGA port {port:03x} read')
            elif kind == 'R':
                address, enables, value = int(fields[2], 16), int(fields[3], 16), int(fields[4], 16)
                mask = sum(0xff << (8 * lane) for lane in range(4) if enables >> lane & 1)
                p.mmio_read(0xa0000 + address, expect=value & mask, mask=mask)
            elif kind == 'M':
                p.delay(0.1)
                p.mark(fields[2], hold_ms)
                p.frame()
    flush()
    flush_writes()
    p.counters()
    return p


# ---- Checks -----------------------------------------------------------------

def check(program: Program, results: Results, reference: Results | None = None) -> dict:
    """Every command's status and expected data; frames against a reference run."""
    commands = program.commands()
    report = {'commands': len(commands), 'results': len(results.results), 'failures': [],
              'stages': [{'offset': f'{o:#x}', 'bytes': n, 'ms': round(ns / 1e6, 3),
                          'mib_per_s': round(n / (1 << 20) / (ns / 1e9), 1) if ns else None}
                         for o, n, ns in results.stages],
              'marks': [{'name': name, 's': round(ns / 1e9, 3), 'done': done} for name, ns, done in results.marks],
              'seconds': round(results.total_ns / 1e9, 3)}
    failures = report['failures']
    if len(results.results) != len(commands):
        failures.append(f'{len(results.results)} results for {len(commands)} commands')
    names = {number: name for name, number in OPS.items()}
    for n, (command, (data, clocks, status, _sequence)) in enumerate(zip(commands, results.results)):
        if status:
            failures.append(f'command {n} {names[command.op]} {command.note} status {status}')
        elif command.mask and (data & command.mask) != command.value:
            failures.append(f'command {n} {names[command.op]} {command.note or ""} read {data:#x}, '
                            f'expected {command.value:#x} under {command.mask:#x}')
        if len(failures) > 50:
            failures.append('more failures not listed')
            break
    report['frames'] = [{'width': w, 'height': h, 'buffer': b, 'count': c} for w, h, b, c, _ in results.frames]
    if reference is not None:
        if len(reference.frames) != len(results.frames):
            failures.append(f'{len(results.frames)} frames, the reference has {len(reference.frames)}')
        for n, (frame, expected) in enumerate(zip(results.frames, reference.frames)):
            if frame[:2] != expected[:2] or frame[4] != expected[4]:
                differing = sum(a != b for a, b in zip(frame[4][::4], expected[4][::4]))
                failures.append(f'frame {n} ({frame[0]}x{frame[1]}) differs from the reference '
                                f'({expected[0]}x{expected[1]}) in {differing} bytes of each 4')
        report['reference_frames'] = len(reference.frames)
    report['status'] = 'fail' if failures else 'pass'
    return report


def write_frame_ppm(frame, path: Path):
    width, height, _buffer, _count, pixels = frame
    rgb = bytearray()
    for n in range(width * height):
        value = int.from_bytes(pixels[4 * n:4 * n + 4], 'little')
        rgb += bytes((value >> 16 & 0xff, value >> 8 & 0xff, value & 0xff))
    path.write_bytes(f'P6\n{width} {height}\n255\n'.encode() + rgb)


PROGRAMS = ('smoke', 'mode13', 'sound', 'opl2', 'ata', 'vga-trace')


def build_program(name: str, clock_hz: int, *, opl2_log: Path | None = None, trace: Path | None = None,
                  hold_ms: int = 2500) -> Program:
    if name == 'smoke':
        return smoke(clock_hz)
    if name == 'mode13':
        return mode13(clock_hz)
    if name == 'sound':
        return sound(clock_hz)
    if name == 'opl2':
        return opl2(clock_hz, opl2_log or Path(__file__).resolve().parents[1] / 'cores/fes-pc386/sim/fixtures/sound-opl2.log')
    if name == 'ata':
        return ata(clock_hz)
    if name == 'vga-trace':
        if trace is None:
            raise DiagError('vga-trace needs --trace')
        return vga_trace(clock_hz, trace, hold_ms)
    raise DiagError(f'unknown program {name}')


def program_checks(name: str, program: Program, results: Results) -> list[str]:
    if name == 'mode13':
        errors = []
        if results.frames:
            errors += check_mode13(results, results.frames[0])
        else:
            errors.append('mode 13h program has no frame')
        picture = bytes(mode13_pattern(x, y) for y in range(200) for x in range(320))
        if not results.dumps or results.dumps[0][1] != picture:
            errors.append('COPY_OUT of VRAM through the VGA does not return the picture')
        return errors
    if name == 'ata':
        return check_ata(program, results)
    return []


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    commands = parser.add_subparsers(dest='command', required=True)
    write = commands.add_parser('write')
    write.add_argument('--program', choices=PROGRAMS, required=True)
    write.add_argument('--clock-hz', type=int, required=True)
    write.add_argument('--trace', type=Path)
    write.add_argument('--opl2-log', type=Path)
    write.add_argument('--hold-ms', type=int, default=2500)
    write.add_argument('output', type=Path)
    checker = commands.add_parser('check')
    checker.add_argument('--name', choices=PROGRAMS)
    checker.add_argument('--reference', type=Path)
    checker.add_argument('--frames', type=Path, help='write each frame as a PPM here')
    checker.add_argument('program', type=Path)
    checker.add_argument('results', type=Path)
    args = parser.parse_args()
    try:
        if args.command == 'write':
            program = build_program(args.program, args.clock_hz, opl2_log=args.opl2_log, trace=args.trace,
                                    hold_ms=args.hold_ms)
            args.output.write_bytes(program.encode())
            print(json.dumps({'program': args.program, 'commands': len(program.commands()),
                              'bytes': args.output.stat().st_size}))
            return 0
        program = decode_program(args.program.read_bytes())
        results = decode_results(args.results.read_bytes())
        reference = decode_results(args.reference.read_bytes()) if args.reference else None
        report = check(program, results, reference)
        if args.name:
            report['failures'] += program_checks(args.name, program, results)
            report['status'] = 'fail' if report['failures'] else 'pass'
        if args.frames:
            args.frames.mkdir(parents=True, exist_ok=True)
            for n, frame in enumerate(results.frames):
                write_frame_ppm(frame, args.frames / f'{n:02d}.ppm')
        print(json.dumps(report, indent=1))
        return 0 if report['status'] == 'pass' else 1
    except DiagError as exc:
        print(f'pc386_diag: {exc}', file=sys.stderr)
        return 2


if __name__ == '__main__':
    raise SystemExit(main())
