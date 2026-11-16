"""Derive the read-only Agilex 5 pinmux contract from the sealed handoff.

The pinned SPL/TF-A omit the final four FPGA handoff words: the generic
offsets 0x1c0/0x1c4 are not Agilex 5 registers. Never turn them into reads.
This descriptor describes 164 expected registers, not boot admission.
"""
import hashlib
import json
import re
from pathlib import Path
import struct

SCOPE = 'sealed-cold-hps-io-v1'
BASE = 0x10d12000
MUX = tuple(range(0, 0xa0, 4)) + tuple(range(0x100, 0x120, 4))
CTRL = tuple(range(0, 0x70, 4)) + tuple(range(0xd0, 0x120, 4))
FPGA = (0, 4, 8, 12, 16, 20, 24, 28, 32, 40, 44, 48, 52, 56, 60, 64, 68, 72, 80, 84)
DELAY = tuple(range(0, 0xc0, 4))
GROUPS = ((0x10, b'PMUX', 0x1000, MUX), (0x1a0, b'IOCT', 0x1130, CTRL),
          (0x330, b'FPGA', 0x1300, FPGA + (0x1c0, 0x1c4)), (0x3f0, b'DLAY', 0x1400, DELAY))

# The pinned Terasic Agilex 5 SPL reads five SDRAM words at 0x634+0x10;
# TF-A's handoff struct ends at 0x648. Preserve the union of those reads,
# including all clock/peripheral bytes and the extra four SDRAM words.
# Quartus also emits an unconsumed text suffix after this boundary. Its exact
# bytes remain covered by the compiler receipt and the HPS I/O provenance map.
CONFIGURATION_BYTES = 0x634 + 0x10 + 5 * 4


def configuration_bytes(raw):
    """Complete pinned boot-consumer bytes, distinct from file provenance."""
    parse_handoff(raw)
    if len(raw) < CONFIGURATION_BYTES:
        raise ValueError('incomplete Agilex 5 boot configuration')
    for position, magic, length in ((0x580, b'CLKS', 0xa0),
                                     (0x620, b'PERI', 0x14), (0x634, b'SDRM', 0x14)):
        if (raw[position:position+4] != magic or
                struct.unpack_from('>I', raw, position+4)[0] != length or
                raw[position+8:position+16] != bytes(8)):
            raise ValueError('unsupported Agilex 5 boot configuration section')
    return raw[:CONFIGURATION_BYTES]


def validate(mapping, context):
    offsets=[base+offset for _,magic,base,items in GROUPS
             for offset in (FPGA if magic==b'FPGA' else items)]
    if (not isinstance(mapping,dict) or set(mapping)!={'format','scope','hps_io_hash','handoff_sha256','base','registers'} or
            type(mapping['format']) is not int or mapping['format']!=1 or mapping['scope']!=SCOPE or
            type(mapping['base']) is not int or mapping['base']!=BASE or
            mapping['hps_io_hash']!=context['hps_io_hash'] or
            any(not isinstance(mapping[k],str) or not re.fullmatch('[0-9a-f]{64}',mapping[k]) or int(mapping[k],16)==0
                for k in ('hps_io_hash','handoff_sha256')) or
            not isinstance(mapping['registers'],list) or len(mapping['registers'])!=164):
        raise ValueError('unsupported complete HPS I/O map')
    for offset,row in zip(offsets,mapping['registers']):
        if (not isinstance(row,dict) or set(row)!={'offset','value','mask'} or
                any(type(row[k]) is not int for k in row) or row['offset']!=offset or
                row['mask']!=0xffffffff or not 0<=row['value']<=0xffffffff or
                offset<0x1130 and row['value']>15 or 0x1300<=offset<=0x1354 and row['value']>1):
            raise ValueError('HPS I/O map has an unsupported register, value or mask')
    return mapping


def parse_handoff(raw):
    if not isinstance(raw, bytes) or not 0x580 <= len(raw) <= 4096 or raw[:16] != b'BOOT\0\5\0\1' + bytes(8):
        raise ValueError('unsupported Agilex 5 pinmux handoff header')
    registers = []
    for position, magic, base, offsets in GROUPS:
        length = 16 + len(offsets) * 8
        if raw[position:position+4] != magic or struct.unpack_from('>I', raw, position+4)[0] != length or raw[position+8:position+16] != bytes(8):
            raise ValueError('pinmux handoff group header differs from the pinned layout')
        for index, offset in enumerate(offsets):
            actual, value = struct.unpack_from('>II', raw, position+16+index*8)
            if actual != offset:
                raise ValueError('pinmux handoff register order or address differs from the pinned layout')
            if magic == b'FPGA' and index >= len(FPGA):
                if value != 0:
                    raise ValueError('unsupported generic pinmux words changed')
                continue
            if magic == b'PMUX' and value > 15 or magic == b'FPGA' and value > 1:
                raise ValueError('unsupported pinmux selection value')
            registers.append({'offset': base+offset, 'value': value, 'mask': 0xffffffff})
    if len(registers) != 164 or len({r['offset'] for r in registers}) != 164:
        raise ValueError('incomplete or overlapping pinmux contract')
    return registers


def compiled_handoff(project):
    project = Path(project)
    receipt = project/'board/hps/build.json'
    handoff = project/'board/hps/project/output_files/hps_bootloader_handoff.bin'
    for path in (receipt, handoff):
        if any(p.is_symlink() for p in (path, *path.parents)):
            raise ValueError('pinmux source contains a symlink')
    if not 0 < receipt.stat().st_size <= 1 << 20 or not 0x580 <= handoff.stat().st_size <= 4096:
        raise ValueError('bounded compiled pinmux inputs required')
    compiled = json.loads(receipt.read_text())
    raw = handoff.read_bytes()
    digest = hashlib.sha256(raw).hexdigest()
    if compiled['generated_handoff_and_sdc_sha256'].get('output_files/hps_bootloader_handoff.bin') != digest:
        raise ValueError('pinmux handoff differs from the actual compiled handoff')
    return raw, digest


def configuration_sha256(project, context):
    raw, digest = compiled_handoff(project)
    validate({'format': 1, 'scope': SCOPE, 'hps_io_hash': context['hps_io_hash'],
              'handoff_sha256': digest, 'base': BASE, 'registers': parse_handoff(raw)}, context)
    return hashlib.sha256(configuration_bytes(raw)).hexdigest()


def derive(project, context):
    raw, digest = compiled_handoff(project)
    return validate({'format': 1, 'scope': SCOPE, 'hps_io_hash': context['hps_io_hash'],
            'handoff_sha256': digest, 'base': BASE, 'registers': parse_handoff(raw)},context)
