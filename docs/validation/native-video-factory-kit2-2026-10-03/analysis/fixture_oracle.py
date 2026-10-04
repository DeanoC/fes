#!/usr/bin/env python3
"""Bounded existing SGM probe gate plus the unchanged native Graphics I oracle.

The tiny gate interpreter executes only the existing cartridge's trampoline.
The unchanged Graphics I interpreter handles the original graphics body. This
models diagnostic intent; physical proof still requires its pass picture and
both tones from the selected artifact on the kit.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import sys

import native_oracle as native

BASE = Path(__file__).resolve().parent
FIXTURE = BASE.parent / 'fixture'
ROOT = BASE.parents[2]
SOURCE = ROOT / 'sources/misteross/cores/fes-coleco/diagnostic'


class ProbeRejected(ValueError):
    def __init__(self, evidence):
        self.evidence = evidence
        super().__init__('SGM probe stopped before video: ' + json.dumps(evidence, sort_keys=True))


def sha(data):
    return hashlib.sha256(data).hexdigest()


def sgm_gate(rom, *, fault=None):
    """Execute <=512 trampoline instructions; fail loops have no video writes."""
    if fault not in (None, 'sgm_absent', 'ram_stuck_zero', 'console_mirror',
                     'lower_window_absent', 'ay_readback_wrong'):
        raise ValueError('unknown bounded SGM diagnostic fault')
    console, expansion, ay = bytearray(1024), bytearray(32768), bytearray(16)
    pc, a, zero, selected = 0x8000, 0, False, 0
    lower = upper = False
    writes, reads, ay_reads, sn_writes, trace = [], [], [], [], []
    last_comparison = None

    def fetch(address):
        if not 0x8000 <= address < 0x8000 + len(rom):
            raise ValueError('probe executed outside its ROM')
        return rom[address - 0x8000]

    def claimed(address):
        return fault != 'sgm_absent' and ((address < 0x2000 and lower) or
                                        (0x2000 <= address < 0x8000 and upper))

    def read(address):
        if claimed(address):
            value = 0 if fault == 'ram_stuck_zero' else expansion[address]
            kind = 'sgm'
        elif 0x6000 <= address < 0x8000:
            value, kind = console[address & 1023], 'console'
        else:
            value, kind = 255, 'unmapped'
        reads.append({'address': address, 'value': value, 'mapping': kind})
        return value

    def write(address, value):
        if claimed(address):
            expansion[address] = value
            kind = 'sgm'
            if fault == 'console_mirror' and address >= 0x6000:
                console[address & 1023] = value
        elif 0x6000 <= address < 0x8000:
            console[address & 1023], kind = value, 'console'
        else:
            kind = 'unmapped'
        writes.append({'address': address, 'value': value, 'mapping': kind})

    for steps in range(512):
        if pc == 0x8004:
            return {'executed_instructions': steps, 'returned_to_graphics': True,
                    'memory_reads': reads, 'memory_writes': writes, 'ay_readbacks': ay_reads,
                    'ay_registers': list(ay), 'sn_writes': sn_writes,
                    'video_writes_before_gate': 0, 'io_writes': trace,
                    'scope': 'Boundary sentinels and console-RAM isolation only; not an exhaustive SRAM test.'}
        here, instruction = pc, fetch(pc)
        pc += 1
        if instruction == 0xf3:
            pass
        elif instruction == 0x31:
            assert fetch(pc) | (fetch(pc + 1) << 8) == 0x6400
            pc += 2
        elif instruction == 0x3e:
            a = fetch(pc); pc += 1
        elif instruction in (0x32, 0x3a):
            address = fetch(pc) | (fetch(pc + 1) << 8); pc += 2
            if instruction == 0x32: write(address, a)
            else: a = read(address)
        elif instruction == 0xfe:
            expected = fetch(pc); pc += 1
            zero = a == expected
            last_comparison = {'observed': a, 'expected': expected}
        elif instruction == 0xc3:
            pc = fetch(pc) | (fetch(pc + 1) << 8)
        elif instruction == 0xc2:
            destination = fetch(pc) | (fetch(pc + 1) << 8); pc += 2
            if not zero:
                if destination != here:
                    raise ValueError('unrecognized reject branch')
                raise ProbeRejected({'fault': fault, 'pc': here,
                                     'executed_instructions': steps + 1,
                                     'comparison': last_comparison, 'video_writes': 0})
        elif instruction == 0xd3:
            port = fetch(pc); pc += 1
            trace.append({'port': port, 'value': a})
            if port == 0x53: upper = bool(a & 1)
            elif port == 0x7f: lower = not bool(a & 2) and fault != 'lower_window_absent'
            elif port == 0x50: selected = a & 15
            elif port == 0x51: ay[selected] = a
            elif port == 0xe0: sn_writes.append(a)
            else: raise ValueError('probe wrote an unexpected port before its pass gate')
        elif instruction == 0xdb:
            port = fetch(pc); pc += 1
            if port != 0x52: raise ValueError('probe read an unexpected port')
            a = ay[selected] ^ (1 if fault == 'ay_readback_wrong' else 0)
            if fault == 'sgm_absent': a = 255
            ay_reads.append({'register': selected, 'value': a})
        else:
            raise ValueError(f'unsupported probe opcode {instruction:02x} at {here:04x}')
    raise ValueError('probe exceeded the bounded trampoline instruction count')


def emitted_sgm():
    sys.path.insert(0, str(SOURCE))
    try:
        spec = importlib.util.spec_from_file_location('existing_sgm_emitter', SOURCE / 'sgm_probe.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module.cartridge()
    finally:
        sys.path.pop(0)


def expected(prepared=FIXTURE / 'prepared.json'):
    receipt = json.loads(prepared.read_bytes())
    for source in receipt['sources'].values():
        if sha((ROOT / source['path']).read_bytes()) != source['sha256']:
            raise ValueError('fixture source changed after preparation')
    graphics = native.emitted_rom()
    sn = native.tone_adapter(graphics)
    sgm = emitted_sgm()
    for name, data in (('graphics-sn', sn), ('sgm-probe', sgm)):
        item = receipt['roms'][name]
        if sha(data) != item['sha256'] or data != Path(item['path']).read_bytes():
            raise ValueError('fixture does not reproduce from its declared open source')
    if sgm[3:len(graphics)] != graphics[3:]:
        raise ValueError('SGM fixture changed the original graphics body/table addresses')
    gate = sgm_gate(sgm)
    assert len(gate['memory_reads']) == 8 and len(gate['memory_writes']) == 7
    assert len(gate['ay_readbacks']) == 4 and gate['sn_writes'] == [0x84, 0x20, 0x90]
    assert gate['ay_registers'][:2] == [254, 0] and gate['ay_registers'][7:9] == [62, 15]
    plain_vram, plain_registers, plain = native.vdp_from_rom(graphics)
    sn_vram, sn_registers, sn_state = native.vdp_from_rom(sn)
    assert plain_vram == sn_vram and plain_registers == sn_registers
    # native.expected reproduces the original SN fixture then executes every
    # Graphics I write and renders the aligned full-frame oracle independently.
    expected_receipt = BASE / 'graphics-oracle-input.json'
    expected_receipt.write_text(json.dumps({'source_sha256': receipt['sources']['generate.py']['sha256'],
                                          'rom_sha256': receipt['roms']['graphics-sn']['sha256'],
                                          'nominal_SN_hz': receipt['nominal_tones_hz']['SN']}) + '\n')
    # The unchanged helper's fixture convention uses a sibling graphics-sn.rom.
    target = BASE / 'graphics-sn.rom'
    if not target.exists(): target.write_bytes(sn)
    if target.read_bytes() != sn: raise ValueError('analysis fixture copy changed')
    full, rgb, original = native.expected(expected_receipt)
    return full, rgb, {'classification': 'Independent offline fixture intent; not physical SGM acceptance.',
                       'prepared_sha256': sha(prepared.read_bytes()), 'graphics_oracle': original,
                       'sgm_probe_gate': gate, 'sgm_rom_sha256': sha(sgm),
                       'sgm_graphics_body_unchanged': True,
                       'nominal_tones_hz': receipt['nominal_tones_hz']}
