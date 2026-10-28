#!/usr/bin/env python3
"""Run SingleStepTests/80386 against the external i386 oracle.

No network access occurs unless --download is explicitly supplied. The corpus
is SingleStepTests/80386 at the fixed revision below: real-mode instruction
states captured from an Intel 80386EX by Daniel Balsom (MIT; data public
domain). Only README, LICENSE, revocation_list.txt and the MOO test data are
consumed, never generator code. Tests listed in revocation_list.txt are skipped.

Example (downloads the corpus into the ignored build directory):
  python3 scripts/test_fes_i386_vectors.py --download
Example with an existing local corpus and two opcode files:
  python3 scripts/test_fes_i386_vectors.py --corpus /absolute/80386 --opcode D3.4 --opcode 27

Each test sets the initial registers and RAM, steps the oracle until it
executes the test's terminating HLT, and compares architectural state only:
the 20 MOO registers (EFLAGS bits 0-17), the final RAM bytes, writes to bytes
the corpus did not list, and the exception or interrupt taken. Bus cycles are
not compared: the corpus comes from a 386EX's 16-bit bus. Undefined flags are
compared too; the corpus decides them. Every failure must be covered by an
entry in cores/fes-common/sim/i386/divergences.toml, and every entry must still
match a failure, or the run fails.
"""
from __future__ import annotations

import argparse
from concurrent.futures import ProcessPoolExecutor, ThreadPoolExecutor
from dataclasses import dataclass, field
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import subprocess
import sys
import tomllib
import urllib.parse
import urllib.request

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i386_oracle as oracle

ROOT = Path(__file__).resolve().parents[1]
REVISION = '459d49fbe6280e9ed46fee887b58dacd9cb880ab'
REPOSITORY = 'https://github.com/SingleStepTests/80386'
RAW = 'https://raw.githubusercontent.com/SingleStepTests/80386/' + REVISION + '/'
API = 'https://api.github.com/repos/SingleStepTests/80386/git/trees/' + REVISION + '?recursive=1'
DATA = 'v1_ex_real_mode'
METADATA = ('README.md', 'LICENSE', 'revocation_list.txt')
CORPUS = 'SingleStepTests/80386'
DIVERGENCES = ROOT / 'cores/fes-common/sim/i386/divergences.toml'
OUTPUT = ROOT / 'build/sim/fes-i386-vectors'
STEM = re.compile(r'(?:67)?(?:66)?(?:0F)?[0-9A-F]{2}(?:\.[0-7])?')
REGISTERS = ('cr0', 'cr3', 'eax', 'ebx', 'ecx', 'edx', 'esi', 'edi', 'ebp', 'esp',
             'cs', 'ds', 'es', 'fs', 'gs', 'ss', 'eip', 'eflags', 'dr6', 'dr7')
EFLAGS = REGISTERS.index('eflags')
FLAG_NAMES = {0: 'CF', 2: 'PF', 4: 'AF', 6: 'ZF', 7: 'SF', 8: 'TF', 9: 'IF', 10: 'DF',
              11: 'OF', 12: 'IOPL', 13: 'IOPL', 14: 'NT', 16: 'RF', 17: 'VM'}
MAX_FILE = 64 * 1024 * 1024


class CorpusError(ValueError):
    pass


# ---- MOO format (github.com/dbalsom/moo, doc/moo_format_v1.md) ---------------

@dataclass
class MooTest:
    index: int
    name: str
    instruction: bytes
    initial: list[int]
    initial_ram: list[tuple[int, int]]
    final_mask: int
    final: dict[int, int]
    final_ram: list[tuple[int, int]]
    exception: tuple[int, int] | None
    hash: str


@dataclass
class MooFile:
    cpu: str
    tests: list[MooTest] = field(default_factory=list)


def _chunks(data: bytes, start: int, end: int):
    offset = start
    while offset < end:
        if offset + 8 > end:
            raise CorpusError('truncated MOO chunk header')
        tag = data[offset:offset + 4].decode('ascii', 'replace')
        (length,) = struct.unpack_from('<I', data, offset + 4)
        if offset + 8 + length > end:
            raise CorpusError(f'MOO chunk {tag!r} overruns its parent')
        yield tag, offset + 8, length
        offset += 8 + length


def _registers(data: bytes, offset: int, length: int) -> tuple[int, dict[int, int]]:
    """Decode an RG32 or RM32 chunk: a 20-bit presence mask, then present values."""
    if length < 4:
        raise CorpusError('short RG32/RM32 chunk')
    (mask,) = struct.unpack_from('<I', data, offset)
    if mask >> len(REGISTERS):
        raise CorpusError('RG32/RM32 mask names an unknown register')
    present = [n for n in range(len(REGISTERS)) if mask >> n & 1]
    if length < 4 + 4 * len(present):
        raise CorpusError('RG32/RM32 chunk shorter than its mask')
    values = struct.unpack_from(f'<{len(present)}I', data, offset + 4)
    return mask, dict(zip(present, values))


def _ram(data: bytes, offset: int, length: int) -> list[tuple[int, int]]:
    (count,) = struct.unpack_from('<I', data, offset)
    if length < 4 + 5 * count:
        raise CorpusError('RAM chunk shorter than its count')
    entries = [struct.unpack_from('<IB', data, offset + 4 + 5 * n) for n in range(count)]
    if len({address for address, _ in entries}) != count:
        raise CorpusError('duplicate RAM address')
    return entries


def _state(data: bytes, offset: int, length: int):
    # RM32 undefined-flag masks are skipped: undefined flags are compared too.
    registers = ram = None
    for tag, start, size in _chunks(data, offset, offset + length):
        if tag == 'RG32':
            registers = _registers(data, start, size)
        elif tag == 'RAM ':
            ram = _ram(data, start, size)
        elif tag == 'REGS':
            raise CorpusError('16-bit REGS state in a 386 test')
    if registers is None or ram is None:
        raise CorpusError('state without RG32 and RAM chunks')
    return registers, ram


def parse_moo(data: bytes) -> MooFile:
    chunks = list(_chunks(data, 0, len(data)))
    if not chunks or chunks[0][0] != 'MOO ':
        raise CorpusError('not a MOO file')
    _, offset, length = chunks[0]
    if length < 12:
        raise CorpusError('short MOO header')
    major, _minor, count, cpu = struct.unpack_from('<BBxxI4s', data, offset)
    if major != 1:
        raise CorpusError(f'unsupported MOO major version {major}')
    moo = MooFile(cpu=cpu.decode('ascii', 'replace'))
    for tag, offset, length in chunks[1:]:
        if tag == 'TEST':
            moo.tests.append(_test(data, offset, length))
    if len(moo.tests) != count:
        raise CorpusError(f'MOO header promises {count} tests, file has {len(moo.tests)}')
    return moo


def _test(data: bytes, offset: int, length: int) -> MooTest:
    (index,) = struct.unpack_from('<I', data, offset)
    parts: dict[str, tuple[int, int]] = {}
    for tag, start, size in _chunks(data, offset + 4, offset + length):
        parts[tag] = (start, size)
    for required in ('NAME', 'BYTS', 'INIT', 'FINA', 'HASH'):
        if required not in parts:
            raise CorpusError(f'test {index} lacks {required}')
    start, _ = parts['NAME']
    (name_length,) = struct.unpack_from('<I', data, start)
    name = data[start + 4:start + 4 + name_length].decode('ascii', 'replace')
    start, _ = parts['BYTS']
    (byte_count,) = struct.unpack_from('<I', data, start)
    instruction = data[start + 4:start + 4 + byte_count]
    (initial_mask, initial), initial_ram = _state(data, *parts['INIT'])
    if initial_mask != (1 << len(REGISTERS)) - 1:
        raise CorpusError(f'test {index} initial state does not list every register')
    (final_mask, final), final_ram = _state(data, *parts['FINA'])
    exception = None
    if 'EXCP' in parts:
        start, size = parts['EXCP']
        if size < 5:
            raise CorpusError(f'test {index} has a short EXCP chunk')
        exception = struct.unpack_from('<BI', data, start)
    start, size = parts['HASH']
    if size != 20:
        raise CorpusError(f'test {index} hash is not SHA-1 sized')
    return MooTest(index, name, instruction, [initial[n] for n in range(len(REGISTERS))], initial_ram,
                   final_mask, final, final_ram, exception, data[start:start + 20].hex())


def revoked_hashes(text: str) -> set[str]:
    hashes = set()
    for line in text.splitlines():
        line = line.strip().lower()
        if not line or line.startswith('#'):
            continue
        if not re.fullmatch(r'[0-9a-f]{40}', line):
            raise CorpusError('malformed revocation list entry: ' + line)
        hashes.add(line)
    return hashes


def stream_line(stem: str, test: MooTest) -> str:
    values = [f'{stem}#{test.index}', *(f'{value:x}' for value in test.initial), f'{test.final_mask:x}']
    values += [f'{test.final[n]:x}' for n in sorted(test.final)]
    for ram in (test.initial_ram, test.final_ram):
        values.append(f'{len(ram):x}')
        values += [f'{address:x} {value:x}' for address, value in ram]
    values.append(f'{test.exception[0]:x}' if test.exception else '-1')
    return ' '.join(values)


# ---- Failure classification and the divergence list --------------------------

def failure_keys(test: MooTest, mismatches: list[tuple[str, int, int]]) -> list[str]:
    """Name what differs at the granularity the divergence list uses."""
    keys = set()
    flag_address = test.exception[1] if test.exception else None
    for name, expected, actual in mismatches:
        if name == 'eflags':
            for bit in range(18):
                if (expected ^ actual) >> bit & 1:
                    keys.add('eflags.' + FLAG_NAMES.get(bit, f'bit{bit}'))
        elif name.startswith('ram.'):
            address = int(name[4:], 16)
            if flag_address is not None and address in (flag_address, flag_address + 1):
                shift = 8 * (address - flag_address)
                for bit in range(8):
                    if (expected ^ actual) >> bit & 1:
                        keys.add('stack.flags.' + FLAG_NAMES.get(bit + shift, f'bit{bit + shift}'))
            else:
                keys.add('ram')
        elif name.startswith('write.'):
            keys.add('unlisted-write')
        else:
            keys.add(name)
    return sorted(keys)


# Opcodes that take a ModRM byte (one-byte map, then the 0F map), for the 386.
MODRM_ONE = frozenset([*(b for row in range(0, 0x40, 8) for b in range(row, row + 4)),
                       0x62, 0x63, 0x69, 0x6B, *range(0x80, 0x90), 0xC0, 0xC1, *range(0xC4, 0xC8),
                       *range(0xD0, 0xD4), *range(0xD8, 0xE0), 0xF6, 0xF7, 0xFE, 0xFF])
MODRM_TWO = frozenset([0x00, 0x01, 0x02, 0x03, *range(0x20, 0x27), *range(0x90, 0xA0), 0xA3, 0xA4, 0xA5,
                       0xAB, 0xAC, 0xAD, 0xAF, *range(0xB2, 0xB8), *range(0xBA, 0xC0)])
PREFIXES = frozenset([0x26, 0x2E, 0x36, 0x3E, 0x64, 0x65, 0xF0, 0xF2, 0xF3, 0x66, 0x67])
SHIFT_GROUPS = frozenset(['C0', 'C1', 'D0', 'D1', 'D2', 'D3'])
DOUBLE_SHIFTS = frozenset(['0FA4', '0FA5', '0FAC', '0FAD'])
PREDICATES = ('invalid-sib', 'count>0', 'count>1', 'count>=width', 'count>width', 'imm8=0', 'opsize32',
              'slot-straddles-wrap', 'frame-over-vector', 'idiv-unchecked-overflow')
SEGMENT_PREFIXES = {0x26: 'es', 0x2E: 'cs', 0x36: 'ss', 0x3E: 'ds', 0x64: 'fs', 0x65: 'gs'}
GPRS = ('eax', 'ecx', 'edx', 'ebx', 'esp', 'ebp', 'esi', 'edi')


def base_opcode(stem: str) -> str:
    """The MOO file stem without its operand- and address-size prefixes."""
    if stem.startswith('67'):
        stem = stem[2:]
    return stem[2:] if stem.startswith('66') else stem


def idiv386_faults(dividend: int, divisor: int, n: int) -> bool:
    """Whether the 386EX raises #DE for IDIV of the 2n-bit dividend by the
    n-bit divisor (fes_i386_muldiv): it divides a negative dividend's one's
    complement by the divisor's magnitude, a bit per step with no bit out at
    the top, and checks the quotient, with the one more the complement owes,
    for its sign's range. An overflowing dividend leaves a garbage quotient."""
    mask = (1 << n) - 1
    negative, divisor_negative = dividend >> (2 * n - 1) & 1, divisor >> (n - 1) & 1
    magnitude = -divisor & mask if divisor_negative else divisor
    value = ~dividend & ((1 << 2 * n) - 1) if negative else dividend
    remainder, low, quotient = value >> n, value & mask, 0
    for _ in range(n):
        remainder = (remainder << 1 | low >> (n - 1)) & mask
        low = low << 1 & mask
        bit = remainder >= magnitude
        quotient = quotient << 1 | bit
        if bit:
            remainder = (remainder - magnitude) & mask
    if negative and (remainder + 1) & mask == magnitude:
        quotient += 1
    half = 1 << (n - 1)
    return magnitude == 0 or (quotient > half if negative != divisor_negative else quotient >= half)


def idiv_overflows(dividend: int, divisor: int, n: int) -> bool:
    """Whether IDIV's quotient is outside the signed range, or the divisor zero."""
    if divisor == 0:
        return True
    signed_dividend = dividend - (1 << 2 * n) if dividend >> (2 * n - 1) & 1 else dividend
    signed_divisor = divisor - (1 << n) if divisor >> (n - 1) & 1 else divisor
    quotient = abs(signed_dividend) // abs(signed_divisor)
    if (signed_dividend < 0) != (signed_divisor < 0):
        quotient = -quotient
    return not -(1 << (n - 1)) <= quotient < 1 << (n - 1)


def modrm_operand(test: MooTest, code: bytes, position: int, address32: bool, segment: str | None,
                  size: int) -> int:
    """The r/m operand of the ModRM at code[position], size bytes (real mode, an
    invalid SIB's scaled base included)."""
    def register(name: str) -> int:
        return test.initial[REGISTERS.index(name)]
    modrm = code[position]
    mod, rm, end = modrm >> 6, modrm & 7, position + 1
    if mod == 3:
        if size == 1:
            return register(GPRS[rm & 3]) >> (8 if rm & 4 else 0) & 0xFF
        return register(GPRS[rm]) & ((1 << 8 * size) - 1)
    if address32:
        base, index, scale = rm, None, 0
        if rm == 4:
            sib = code[end]
            end += 1
            scale, index, base = sib >> 6, (sib >> 3) & 7, sib & 7
            index = None if index == 4 else index
            if base == 5 and mod == 0:
                base = None
        elif rm == 5 and mod == 0:
            base = None
        if base is None or mod == 2:
            displacement = int.from_bytes(code[end:end + 4], 'little')
        else:
            displacement = int.from_bytes(code[end:end + 1], 'little', signed=True) if mod == 1 else 0
        offset = displacement
        if base is not None:
            offset += register(GPRS[base]) * ((1 << scale) if index is None and rm == 4 else 1)
        if index is not None:
            offset += register(GPRS[index]) << scale
        offset &= 0xFFFFFFFF
        stack = base in (4, 5)
    else:
        bases = (('ebx', 'esi'), ('ebx', 'edi'), ('ebp', 'esi'), ('ebp', 'edi'), ('esi',), ('edi',), ('ebp',), ('ebx',))
        names = bases[rm]
        if mod == 0 and rm == 6:
            names, displacement = (), int.from_bytes(code[end:end + 2], 'little')
        elif mod == 1:
            displacement = int.from_bytes(code[end:end + 1], 'little', signed=True)
        else:
            displacement = int.from_bytes(code[end:end + 2], 'little') if mod == 2 else 0
        offset = (displacement + sum(register(name) & 0xFFFF for name in names)) & 0xFFFF
        stack = 'ebp' in names
    linear = (register(segment or ('ss' if stack else 'ds')) & 0xFFFF) * 16 + offset
    ram = dict(test.initial_ram)
    return sum(ram.get(linear + k, 0) << 8 * k for k in range(size))


def predicates(stem: str, test: MooTest) -> set[str]:
    """Facts about a test's instruction that scope divergence entries."""
    code, index, address32, operand32, segment = test.instruction, 0, False, False, None
    while index < len(code) and code[index] in PREFIXES:
        address32 |= code[index] == 0x67
        operand32 |= code[index] == 0x66
        segment = SEGMENT_PREFIXES.get(code[index], segment)
        index += 1
    two_byte = code[index] == 0x0F
    opcode = code[index + 1] if two_byte else code[index]
    position = index + (2 if two_byte else 1)
    facts = set()
    if (opcode in MODRM_TWO) if two_byte else (opcode in MODRM_ONE):
        modrm = code[position]
        mod, rm, end = modrm >> 6, modrm & 7, position + 1
        if mod != 3 and address32:
            if rm == 4:
                sib = code[end]
                end += 1
                if (sib >> 3) & 7 == 4 and sib >> 6:
                    facts.add('invalid-sib')  # SIB index "none" with a scale
                if mod == 0 and sib & 7 == 5:
                    end += 4
            end += {0: 4 if rm == 5 else 0, 1: 1, 2: 4}[mod]
        elif mod != 3:
            end += {0: 2 if rm == 6 else 0, 1: 1, 2: 2}[mod]
        base = base_opcode(stem).split('.')[0]
        count = None
        if base in ('D0', 'D1'):
            count = 1
        elif base in ('D2', 'D3', '0FA5', '0FAD'):
            count = test.initial[REGISTERS.index('ecx')] & 0x1F
        elif base in ('C0', 'C1', '0FA4', '0FAC'):
            count = code[end] & 0x1F
        if count is not None:
            width = 8 if base in ('C0', 'D0', 'D2') else 32 if '66' in stem else 16
            facts |= {name for name, holds in (('count>0', count > 0), ('count>1', count > 1),
                                               ('count>=width', count >= width),
                                               ('count>width', count > width)) if holds}
        # IDIV whose quotient overflows, which the 386EX's check lets through.
        if not two_byte and opcode in (0xF6, 0xF7) and (modrm >> 3) & 7 == 7:
            n = 8 if opcode == 0xF6 else 32 if operand32 else 16
            divisor = modrm_operand(test, code, position, address32, segment, n // 8)
            eax, edx = (test.initial[REGISTERS.index(name)] for name in ('eax', 'edx'))
            dividend = (eax & 0xFFFF if n == 8 else (edx & 0xFFFF) << 16 | eax & 0xFFFF if n == 16
                        else edx << 32 | eax)
            if idiv_overflows(dividend, divisor, n) and not idiv386_faults(dividend, divisor, n):
                facts.add('idiv-unchecked-overflow')
    elif not two_byte and opcode in (0xD4, 0xD5) and code[position] == 0:
        facts.add('imm8=0')
    # The corpus runs in 16-bit code: a 32-bit operand size is a 66h prefix.
    if operand32:
        facts.add('opsize32')
    # PUSHA's or POPA's eight slots wrap the 16-bit stack misaligned, so one
    # straddles FFFFh.
    if not two_byte and opcode in (0x60, 0x61):
        size = 4 if operand32 else 2
        sp = test.initial[REGISTERS.index('esp')] & 0xFFFF
        start = (sp - 8 * size) & 0xFFFF if opcode == 0x60 else sp
        if start + 8 * size > 0x10000 and start % size:
            facts.add('slot-straddles-wrap')
    # The frame of the exception or interrupt the test takes (its three
    # words below SP, on the 16-bit stack) overlaps the vector's entry.
    if test.exception is not None:
        vector = test.exception[0]
        base = test.initial[REGISTERS.index('ss')] * 16
        sp = test.initial[REGISTERS.index('esp')] & 0xFFFF
        if {base + ((sp - k) & 0xFFFF) for k in range(1, 7)} & set(range(vector * 4, vector * 4 + 4)):
            facts.add('frame-over-vector')
    return facts


def load_divergences(path: Path = DIVERGENCES) -> list[dict]:
    """[[vectors]] entries: opcodes+fields(+when) for a class, or cases for named tests."""
    document = tomllib.loads(path.read_text())
    entries = document.get('vectors', [])
    seen = set()
    for entry in entries:
        name = entry.get('id', '?')
        for key in ('id', 'fields', 'deciding_case', 'deciding_hash', 'oracle', 'corpus', 'follow'):
            if key not in entry:
                raise CorpusError(f'divergence entry {name} lacks {key}')
        if name in seen:
            raise CorpusError('duplicate divergence id: ' + name)
        seen.add(name)
        if entry['follow'] not in ('corpus', 'oracle'):
            raise CorpusError(f'{name}: follow is "corpus" or "oracle"')
        if sum(key in entry for key in ('opcodes', 'files', 'cases')) != 1:
            raise CorpusError(f'{name}: give one of opcodes, files or cases')
        if 'opcodes' in entry and entry['opcodes'] != ['*'] and not all(
                STEM.fullmatch(stem) and base_opcode(stem) == stem for stem in entry['opcodes']):
            raise CorpusError(f'{name}: opcodes are MOO stems without 66/67 prefixes, or "*"')
        if 'files' in entry and not all(STEM.fullmatch(stem) for stem in entry['files']):
            raise CorpusError(f'{name}: malformed MOO file stem')
        if 'cases' in entry:
            pairs = entry['cases']
            if not all(isinstance(pair, list) and len(pair) == 2 and re.fullmatch(r'[0-9a-f]{40}', pair[1])
                       for pair in pairs) or len({pair[0] for pair in pairs}) != len(pairs):
                raise CorpusError(f'{name}: cases are distinct [<file>#<index>, <hash>] pairs')
            entry['cases'] = dict(pairs)
        if not set(entry.get('when', [])) <= set(PREDICATES):
            raise CorpusError(f'{name}: unknown predicate in when')
        if not re.fullmatch(r'[0-9a-f]{40}', entry['deciding_hash']):
            raise CorpusError(f'{name}: deciding_hash must be a test hash')
        stem, _, index = entry['deciding_case'].partition('#')
        if not index.isdigit() or not STEM.fullmatch(stem) or not applies(entry, stem, entry['deciding_case'], None):
            raise CorpusError(f'{name}: deciding case must be a <file>#<index> the entry covers')
    return entries


def applies(entry: dict, stem: str, label: str, facts: set[str] | None) -> bool:
    if 'cases' in entry:
        return label in entry['cases']
    if 'files' in entry and stem not in entry['files']:
        return False
    if 'opcodes' in entry and entry['opcodes'] != ['*'] and base_opcode(stem) not in entry['opcodes']:
        return False
    return facts is None or set(entry.get('when', [])) <= facts


def coverage(entries: list[dict], stem: str, failure: dict) -> list[str] | None:
    """Ids of the entries that together cover every failing key, or None."""
    contributing, covered = [], set()
    for entry in entries:
        if not applies(entry, stem, failure['label'], set(failure['predicates'])):
            continue
        if 'cases' in entry and entry['cases'][failure['label']] != failure['hash']:
            continue
        fields = set(failure['keys']) if entry['fields'] == ['*'] else set(entry['fields']) & set(failure['keys'])
        if fields:
            contributing.append(entry['id'])
            covered |= fields
    return contributing if covered >= set(failure['keys']) else None


# ---- Corpus download and verification ---------------------------------------

def fetch(url: str) -> bytes:
    request = urllib.request.Request(url, headers={'User-Agent': 'FES-i386-data-qualification'})
    with urllib.request.urlopen(request, timeout=120) as response:
        data = response.read(MAX_FILE + 1)
    if len(data) > MAX_FILE:
        raise CorpusError('download exceeds 64 MiB: ' + url)
    return data


def git_blob(data: bytes) -> str:
    return hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest()


def selected(stem: str, args) -> bool:
    return STEM.fullmatch(stem) is not None and (not args.opcode or stem in args.opcode)


def download(corpus: Path, args) -> None:
    corpus.mkdir(parents=True, exist_ok=True)
    tree = json.loads(fetch(API))
    if tree.get('truncated'):
        raise CorpusError('GitHub returned a truncated inventory')
    blobs = {entry['path']: entry['sha'] for entry in tree['tree'] if entry['type'] == 'blob'}
    wanted = [path for path in blobs if path.startswith(DATA + '/') and path.endswith('.MOO.gz')
              and selected(path[len(DATA) + 1:-len('.MOO.gz')], args)]
    if not wanted:
        raise CorpusError('no selected opcode files at the pinned revision')
    receipt_path = corpus / 'fes-i386-corpus.json'
    receipt = json.loads(receipt_path.read_text()) if receipt_path.exists() else {}
    files = receipt.get('files', {}) if receipt.get('revision') == REVISION else {}
    missing = [name for name in METADATA if name not in blobs]
    if missing:
        raise CorpusError('pinned revision lacks ' + ', '.join(missing))

    def receive(path: str):
        name = path.rsplit('/', 1)[-1]
        target = corpus / name
        if target.is_file() and git_blob(target.read_bytes()) == blobs[path]:
            return name, blobs[path]
        data = fetch(RAW + urllib.parse.quote(path))
        if git_blob(data) != blobs[path]:
            raise CorpusError('download does not match the pinned Git blob: ' + path)
        temporary = target.with_suffix('.tmp')
        temporary.write_bytes(data)
        temporary.replace(target)
        return name, blobs[path]

    print(f'Fetching {len(wanted)} MOO files and metadata at {REVISION} (data only)', flush=True)
    with ThreadPoolExecutor(max_workers=args.download_jobs) as executor:
        files.update(dict(executor.map(receive, sorted(wanted) + list(METADATA))))
    receipt = {'repository': REPOSITORY, 'revision': REVISION, 'license': 'MIT; data public domain',
               'files': files}
    receipt_path.write_text(json.dumps(receipt, indent=2, sort_keys=True) + '\n')


def verify(corpus: Path, paths: list[Path]) -> None:
    receipt_path = corpus / 'fes-i386-corpus.json'
    if not receipt_path.exists():
        print('Local corpus: revision/provenance not verified by this runner', flush=True)
        return
    receipt = json.loads(receipt_path.read_text())
    if receipt.get('revision') != REVISION or not isinstance(receipt.get('files'), dict):
        raise CorpusError(f'{receipt_path} is not a receipt for revision {REVISION}')
    for path in paths:
        if git_blob(path.read_bytes()) != receipt['files'].get(path.name):
            raise CorpusError('cached data differs from the corpus receipt: ' + path.name)
    print('Corpus:', receipt['repository'], 'revision', receipt['revision'], flush=True)


# ---- Running -----------------------------------------------------------------

def run_file(path: str, testbench: str, revoked: frozenset[str], limit: int | None):
    stem = Path(path).name[:-len('.MOO.gz')]
    moo = parse_moo(gzip.decompress(Path(path).read_bytes()))
    if moo.cpu != '386E':
        raise CorpusError(f'{stem}: CPU {moo.cpu!r} is not the 386EX')
    tests = moo.tests[:limit] if limit else moo.tests
    kept = [test for test in tests if test.hash not in revoked]
    lines = ''.join(stream_line(stem, test) + '\n' for test in kept)
    result = subprocess.run([testbench], input=lines, capture_output=True, text=True)
    if result.returncode:
        raise CorpusError(f'{stem}: testbench failed\n{result.stderr}')
    by_label = {f'{stem}#{test.index}': test for test in kept}
    failures, summary = [], None
    for line in result.stdout.splitlines():
        words = line.split()
        if words[0] == 'SUMMARY':
            summary = (int(words[1]), int(words[2]))
        elif words[0] == 'FAIL':
            test = by_label[words[1]]
            items = words[2:]
            mismatches = [(items[n], int(items[n + 1], 16), int(items[n + 2], 16)) for n in range(0, len(items), 3)]
            failures.append({'label': words[1], 'hash': test.hash, 'name': test.name,
                             'keys': failure_keys(test, mismatches),
                             'predicates': sorted(predicates(stem, test)),
                             'mismatches': [f'{name} {expected:x} {actual:x}' for name, expected, actual in mismatches]})
    if summary is None or summary[0] + summary[1] != len(kept):
        raise CorpusError(f'{stem}: testbench summary does not account for every test')
    return {'stem': stem, 'tests': len(tests), 'revoked': len(tests) - len(kept), 'passed': summary[0],
            'failures': failures, 'complete': limit is None or limit >= len(moo.tests)}


def evaluate(results: list[dict], entries: list[dict], max_detailed: int, whole_corpus: bool) -> int:
    """Match failures to the divergence list; return the process exit status."""
    used: dict[str, int] = {}
    deciding_seen = set()
    uncovered = []
    for result in results:
        for failure in result['failures']:
            contributing = coverage(entries, result['stem'], failure)
            if contributing is None:
                uncovered.append(failure)
                continue
            for identifier in contributing:
                used[identifier] = used.get(identifier, 0) + 1
                entry = next(item for item in entries if item['id'] == identifier)
                if failure['label'] == entry['deciding_case'] and failure['hash'] == entry['deciding_hash']:
                    deciding_seen.add(identifier)
    complete = {result['stem'] for result in results if result['complete']}
    status = 0
    for failure in uncovered[:max_detailed]:
        print('UNLISTED', failure['label'], failure['hash'], repr(failure['name']), ' '.join(failure['keys']),
              '{' + ' '.join(failure['predicates']) + '}', '|', '; '.join(failure['mismatches']), flush=True)
    if uncovered:
        print(f'{len(uncovered)} failures have no divergence entry', flush=True)
        status = 1
    for entry in entries:
        if 'opcodes' in entry:
            if not whole_corpus:
                continue  # an opcode class is only judged on the whole corpus
        elif not ({label.partition('#')[0] for label in entry['cases']} if 'cases' in entry
                  else set(entry['files'])) <= complete:
            continue  # not every file of this entry ran in full
        if entry['id'] not in used:
            print('STALE divergence entry (no failure matches):', entry['id'], flush=True)
            status = 1
        elif entry['id'] not in deciding_seen:
            print('Divergence entry whose deciding case no longer fails:', entry['id'], flush=True)
            status = 1
    for identifier, count in sorted(used.items()):
        print(f'divergence {identifier}: {count} failures', flush=True)
    return status


def write_report(results: list[dict], path: Path) -> None:
    groups: dict[tuple, dict] = {}
    for result in results:
        for failure in result['failures']:
            group = groups.setdefault((result['stem'], tuple(failure['keys'])), {
                'file': result['stem'], 'keys': failure['keys'], 'count': 0, 'first': failure})
            group['count'] += 1
    report = {'revision': REVISION, 'files': [{key: result[key] for key in ('stem', 'tests', 'revoked', 'passed')}
                                              | {'failed': len(result['failures'])} for result in results],
              'groups': sorted(groups.values(), key=lambda group: (group['file'], group['keys']))}
    path.write_text(json.dumps(report, indent=1) + '\n')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--corpus', type=Path)
    parser.add_argument('--download', action='store_true',
                        help='explicitly fetch the pinned corpus and the pinned oracle into ignored build/')
    parser.add_argument('--opcode', action='append', help="MOO file stem, e.g. 'D3.4' or '6601'")
    parser.add_argument('--limit-per-file', type=int)
    parser.add_argument('--max-detailed-failures', type=int, default=40)
    parser.add_argument('--download-jobs', type=int, default=8)
    parser.add_argument('--jobs', type=int, default=os.cpu_count() or 2)
    args = parser.parse_args()
    if args.limit_per_file is not None and args.limit_per_file < 1:
        parser.error('--limit-per-file must be positive')
    if not 1 <= args.jobs <= 64 or not 1 <= args.download_jobs <= 16:
        parser.error('invalid job count')
    if args.corpus is not None and not args.corpus.is_absolute():
        parser.error('--corpus must be absolute')
    if args.download and args.corpus is not None:
        parser.error('--download uses ignored build/; use --corpus for existing data')
    if args.opcode:
        for stem in args.opcode:
            if not STEM.fullmatch(stem):
                parser.error('invalid opcode file stem: ' + stem)
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    corpus = args.corpus or OUTPUT / 'corpus' / REVISION
    OUTPUT.mkdir(parents=True, exist_ok=True)
    try:
        entries = load_divergences()
        if args.download:
            download(corpus, args)
        if not corpus.is_dir():
            raise CorpusError(f'corpus directory does not exist: {corpus}; pass --download')
        paths = sorted(path for path in corpus.glob('*.MOO.gz') if selected(path.name[:-len('.MOO.gz')], args))
        if not paths:
            raise CorpusError('no selected MOO files in the corpus')
        missing = set(args.opcode or ()) - {path.name[:-len('.MOO.gz')] for path in paths}
        if missing:
            raise CorpusError('requested opcode files are absent: ' + ', '.join(sorted(missing)))
        verify(corpus, paths + [corpus / 'revocation_list.txt'])
        revoked = frozenset(revoked_hashes((corpus / 'revocation_list.txt').read_text()))
        testbench = oracle.build_testbench('i386-vectors', oracle.SIM / 'vectors_tb.cpp',
                                           jobs=args.jobs, download=args.download)
        with ProcessPoolExecutor(max_workers=args.jobs) as executor:
            futures = [executor.submit(run_file, str(path), str(testbench), revoked, args.limit_per_file)
                       for path in paths]
            results = [future.result() for future in futures]
    except (OSError, CorpusError, oracle.OracleError, KeyError, json.JSONDecodeError, tomllib.TOMLDecodeError) as error:
        print('i386 vector qualification error:', error, file=sys.stderr)
        return 2
    tests = sum(result['tests'] for result in results)
    revoked_count = sum(result['revoked'] for result in results)
    passed = sum(result['passed'] for result in results)
    failed = sum(len(result['failures']) for result in results)
    write_report(results, OUTPUT / 'report.json')
    print(f'{CORPUS} {REVISION[:12]}: {len(results)} files, {tests} tests, {revoked_count} revoked, '
          f'{passed} pass, {failed} fail (report: {OUTPUT / "report.json"})', flush=True)
    whole = not args.opcode and all(result['complete'] for result in results)
    return evaluate(results, entries, args.max_detailed_failures, whole)


if __name__ == '__main__':
    raise SystemExit(main())
