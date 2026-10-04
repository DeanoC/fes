#!/usr/bin/env python3
"""Independent Graphics I oracle from the open diagnostic ROM's VDP writes.

This bounded interpreter executes only the diagnostic emitter's instructions,
then renders Graphics I directly from its VRAM/register values. It reads no
FPGA RTL and does not inherit the old framebuffer preview's horizontal delay.
"""
import collections
import hashlib
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
SOURCE = ROOT / 'sources/misteross/cores/fes-coleco/diagnostic/generate.py'
W, H = 1280, 720
VIEWPORT = (384, 168, 896, 552)
PALETTE = {0: (0, 0, 0), 1: (0, 0, 0), 2: (33, 200, 66), 6: (212, 82, 77)}
CLASSES = {0: 'black', 1: 'green', 2: 'red'}
INDEX_CLASS = {0: 0, 1: 0, 2: 1, 6: 2}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def emitted_rom():
    spec = importlib.util.spec_from_file_location('open_graphics_emitter', SOURCE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.cartridge()


def tone_adapter(rom):
    rom = bytearray(rom)
    if rom[:4] != bytes((0xf3, 0x31, 0, 0x64)):
        raise ValueError('fixture adapter expects the original DI/stack entry')
    entry = 0x8000 + len(rom)
    rom[:3] = bytes((0xc3, entry & 255, entry >> 8))
    trampoline = bytearray((0xf3, 0x31, 0, 0x64))
    for value in (0x84, 0x20, 0x90):
        trampoline.extend((0x3e, value, 0xd3, 0xe0))
    trampoline.extend((0xc3, 4, 0x80))
    return bytes(rom + trampoline)


def vdp_from_rom(rom):
    pc, a, bc, de, hl, zero = 0x8000, 0, 0, 0, 0, False
    vram, registers = bytearray(16384), [0] * 8
    pointer, latch, writes = 0, None, 0
    sn_writes = []

    def read(address):
        if not 0x8000 <= address < 0x8000 + len(rom):
            raise ValueError(f'diagnostic reads outside its ROM: {address:04x}')
        return rom[address - 0x8000]

    def out(port, value):
        nonlocal pointer, latch, writes
        if port == 0xe0:
            sn_writes.append(value)
        elif port == 0xbe:
            vram[pointer] = value
            pointer = (pointer + 1) & 0x3fff
            writes += 1
        elif port == 0xbf:
            if latch is None:
                latch = value
            else:
                if value & 0x80:
                    registers[value & 7] = latch
                else:
                    pointer = (latch | (value << 8)) & 0x3fff
                latch = None
        else:
            raise ValueError(f'unexpected diagnostic port: {port:02x}')

    for steps in range(250000):
        instruction = read(pc)
        pc += 1
        if instruction == 0xf3:  # DI
            pass
        elif instruction in (0x01, 0x11, 0x21, 0x31):  # LD rr,nn
            value = read(pc) | (read(pc + 1) << 8)
            pc += 2
            if instruction == 0x01: bc = value
            elif instruction == 0x11: de = value
            elif instruction == 0x21: hl = value
        elif instruction == 0x3e:  # LD A,n
            a = read(pc); pc += 1
        elif instruction == 0xd3:  # OUT (n),A
            out(read(pc), a); pc += 1
        elif instruction == 0xdb:  # IN A,(BF), clear control latch
            if read(pc) != 0xbf: raise ValueError('unexpected diagnostic IN')
            a, latch = 0, None; pc += 1
        elif instruction == 0xaf: a, zero = 0, True  # XOR A
        elif instruction == 0x0b: bc = (bc - 1) & 0xffff
        elif instruction == 0x78: a = bc >> 8
        elif instruction == 0xb1: a |= bc & 255; zero = a == 0
        elif instruction == 0x7e: a = read(hl)
        elif instruction == 0x23: hl = (hl + 1) & 0xffff
        elif instruction == 0x1b: de = (de - 1) & 0xffff
        elif instruction == 0x7a: a = de >> 8
        elif instruction == 0xb3: a |= de & 255; zero = a == 0
        elif instruction == 0x20:  # JR NZ,e
            offset = read(pc); pc += 1
            if not zero: pc += offset - 256 if offset & 128 else offset
        elif instruction == 0xc3: pc = read(pc) | (read(pc + 1) << 8)
        elif instruction == 0x76:  # Settled diagnostic HALT
            return vram, registers, {'executed_instructions': steps + 1,
                'vram_write_count': writes, 'vram_sha256': sha(vram),
                'vdp_registers': registers, 'sn_writes': sn_writes}
        else:
            raise ValueError(f'unsupported diagnostic instruction {instruction:02x} at{pc - 1:04x}')
    raise ValueError('diagnostic did not settle within its instruction bound')


def expected(fixture):
    receipt = json.loads(fixture.read_bytes())
    original = emitted_rom()
    adapted = tone_adapter(original)
    rom_path = fixture.parent / 'graphics-sn.rom'
    if sha(SOURCE.read_bytes()) != receipt['source_sha256'] or \
            sha(adapted) != receipt['rom_sha256'] or adapted != rom_path.read_bytes():
        raise ValueError('open emitter/adapter does not reproduce the declared capture ROM')
    vram, registers, provenance = vdp_from_rom(adapted)
    if registers != [0, 0xc0, 0, 0x80, 1, 0x36, 3, 1] or \
            provenance['sn_writes'] != [0x84, 0x20, 0x90]:
        raise ValueError('fixture no longer selects the bounded Graphics I/tone diagnostic')
    period = (provenance['sn_writes'][0] & 15) | ((provenance['sn_writes'][1] & 63) << 4)
    nominal = 3579545 / (32 * period)
    if abs(nominal - receipt['nominal_SN_hz']) > 1e-12:
        raise ValueError('fixture SN receipt disagrees with emitted tone writes')
    names, patterns, colors = (registers[2] & 15) << 10, (registers[4] & 7) << 11, registers[3] << 6
    indices = bytearray(256 * 192)
    for y in range(192):
        for x in range(256):
            name = vram[names + (y // 8) * 32 + x // 8]
            pattern = vram[patterns + name * 8 + y % 8]
            color = vram[colors + name // 8]
            index = color >> 4 if pattern & (128 >> (x % 8)) else color & 15
            if index not in PALETTE: raise ValueError('unexpected fixture palette index')
            indices[y * 256 + x] = index
    full = bytearray(W * H)
    rgb = bytearray(W * H * 3)
    for y in range(168, 552):
        for x in range(384, 896):
            index = indices[((y - 168) // 2) * 256 + (x - 384) // 2]
            full[y * W + x] = INDEX_CLASS[index]
            rgb[(y * W + x) * 3:(y * W + x) * 3 + 3] = bytes(PALETTE[index])
    provenance.update(source_path=str(SOURCE), source_sha256=sha(SOURCE.read_bytes()),
        rom_sha256=sha(adapted), rom_size=len(adapted), native_index_sha256=sha(indices),
        native_index_counts=dict(collections.Counter(indices)), full_class_sha256=sha(full),
        full_rgb_sha256=sha(rgb), viewport=list(VIEWPORT), horizontal_delay_pixels=0,
        rendered_from='emulated original ROM VDP writes; Graphics I VRAM decode; exact2x scaling',
        sn_period=period, nominal_SN_hz=nominal)
    return bytes(full), bytes(rgb), provenance
