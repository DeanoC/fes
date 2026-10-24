#!/usr/bin/env python3
"""Boot test386.asm as the BIOS ROM of the external i386 oracle.

No network access occurs unless --download is explicitly supplied. test386.asm
(GPL-3.0-or-later, Marco Bortolin and contributors) is test input only: it is
fetched at the pinned revision into the ignored build directory, assembled
there with NASM (fetched as a pinned, hash-checked release), and never enters a
package or an RTL source list.

Example:
  python3 scripts/test_fes_i386_test386.py --download

The ROM is built as the 128 KiB version (task-switch tests included) with
TEST_UNDEF=1 and CPU_FAMILY=3, POST codes on port 190h and test386's text output
on port E9h. The run records every POST code, compares the POST EEh text with
test386-EE-reference.txt, and keeps the final 640 KiB of RAM. test386 stops at
its first failed check, so each check the oracle fails is stepped over only
when cores/fes-common/sim/i386/divergences.toml lists it; any other failure,
an unused entry or a final POST other than FFh fails the run.

The run then cross-checks test386's i386 undefined-behaviour cases against
SingleStepTests/80386 states with the same inputs (see cross_check). That needs
the corpus test_fes_i386_vectors.py downloads; --no-cross-check skips it.
"""
from __future__ import annotations

import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tomllib
import urllib.request

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_vectors as vectors

ROOT = Path(__file__).resolve().parents[1]
BUILD = ROOT / 'build/sim/fes-i386-test386'
TEST386_REPOSITORY = 'https://github.com/barotto/test386.asm'
TEST386_REVISION = 'cfd052d1e64d5375dea5a681c1eadeed64ceda2c'
NASM_VERSION = '2.16.03'
NASM_URL = f'https://www.nasm.us/pub/nasm/releasebuilds/{NASM_VERSION}/nasm-{NASM_VERSION}.tar.xz'
NASM_SHA256 = '1412a1c760bbd05db026b6c0d1657affd6631cd0a63cddb6f73cc6d4aa616148'
# Every EQU of src/configuration.asm, set explicitly.
CONFIGURATION = {
    'POST_PORT': '0x190', 'LPT_PORT': '0', 'LPT_STROBING': '0', 'COM_PORT': '0', 'COM_PORT_DIV': '0x0001',
    'OUT_PORT': '0xE9', 'TEST_UNDEF': '1', 'CPU_FAMILY': '3', 'BOCHS': '0', 'IBM_PS1': '0', 'DEBUG': '0',
    'ROM128': '1',
}
POST_PORT = int(CONFIGURATION['POST_PORT'], 0)
OUTPUT_PORT = int(CONFIGURATION['OUT_PORT'], 0)
ROM_BYTES = 0x20000
# ROM128 places the extension area at E0000h and the system BIOS at F0000h.
SECTION_BASES = {'.system_bios_extensions_area': 0xE0000, '.system_bios_area': 0xF0000}
DUMP_BYTES = 0xA0000
STEP_LIMIT = 200_000_000
CORPUS = 'test386.asm'


class Test386Error(ValueError):
    pass


# ---- Fetch and build ---------------------------------------------------------

def _git(source: Path, *args: str) -> str:
    return subprocess.check_output(['git', '-C', str(source), *args], text=True).strip()


def test386_source(download: bool) -> Path:
    source = BUILD / ('test386-' + TEST386_REVISION)
    if not (source / '.git').is_dir():
        if not download:
            raise Test386Error(f'test386.asm {TEST386_REVISION} is not in {source}; pass --download')
        partial = source.with_name(source.name + '.partial')
        shutil.rmtree(partial, ignore_errors=True)
        partial.mkdir(parents=True)
        print(f'Fetching test386.asm {TEST386_REVISION} (GPL-3.0-or-later, test input only)', flush=True)
        for args in (('init', '-q'), ('fetch', '-q', '--depth', '1', TEST386_REPOSITORY, TEST386_REVISION),
                     ('checkout', '-q', 'FETCH_HEAD')):
            subprocess.run(['git', '-C', str(partial), *args], check=True)
        partial.rename(source)
    if _git(source, 'rev-parse', 'HEAD') != TEST386_REVISION or _git(source, 'status', '--porcelain'):
        raise Test386Error(f'{source} is not a clean checkout of the pinned revision')
    return source


def nasm(download: bool, jobs: int) -> Path:
    tree = BUILD / f'nasm-{NASM_VERSION}'
    binary = tree / 'nasm'
    if binary.is_file():
        return binary
    archive = BUILD / f'nasm-{NASM_VERSION}.tar.xz'
    if not archive.is_file():
        if not download:
            raise Test386Error(f'NASM {NASM_VERSION} is not in {BUILD}; pass --download')
        print(f'Fetching NASM {NASM_VERSION}', flush=True)
        BUILD.mkdir(parents=True, exist_ok=True)
        request = urllib.request.Request(NASM_URL, headers={'User-Agent': 'FES-i386-test386'})
        with urllib.request.urlopen(request, timeout=120) as response:
            data = response.read(16 * 1024 * 1024)
        if hashlib.sha256(data).hexdigest() != NASM_SHA256:
            raise Test386Error('NASM archive does not match its pinned SHA-256')
        archive.write_bytes(data)
    if hashlib.sha256(archive.read_bytes()).hexdigest() != NASM_SHA256:
        raise Test386Error(f'{archive} does not match its pinned SHA-256')
    extract = BUILD / f'nasm-{NASM_VERSION}.partial'
    shutil.rmtree(extract, ignore_errors=True)
    extract.mkdir(parents=True)
    with tarfile.open(archive) as tar:
        tar.extractall(extract, filter='data')
    source = extract / f'nasm-{NASM_VERSION}'
    log = BUILD / 'nasm-build.log'
    with log.open('w') as destination:
        for command in (['./configure'], ['make', '-j', str(jobs), 'nasm']):
            if subprocess.run(command, cwd=source, stdout=destination, stderr=subprocess.STDOUT).returncode:
                raise Test386Error(f'building NASM failed; see {log}')
    shutil.rmtree(tree, ignore_errors=True)
    source.rename(tree)
    shutil.rmtree(extract)
    return binary


def configure(text: str) -> str:
    """Set every configuration EQU; each must appear exactly once."""
    for name, value in CONFIGURATION.items():
        pattern = re.compile(rf'^{name}\s+equ\s+\S+', re.MULTILINE)
        if len(pattern.findall(text)) != 1:
            raise Test386Error(f'configuration.asm does not define {name} exactly once')
        text = pattern.sub(f'{name} equ {value}', text)
    defined = set(re.findall(r'^(\w+)\s+equ\b', text, re.MULTILINE))
    if defined != set(CONFIGURATION):
        raise Test386Error('configuration.asm EQUs changed: ' + ', '.join(sorted(defined ^ set(CONFIGURATION))))
    return text


def assemble(source: Path, assembler: Path) -> Path:
    rom = BUILD / 'rom'
    shutil.rmtree(rom, ignore_errors=True)
    shutil.copytree(source / 'src', rom / 'src')
    configuration = rom / 'src/configuration.asm'
    configuration.write_text(configure(configuration.read_text()))
    result = subprocess.run([str(assembler), '-i' + str(rom / 'src') + '/', '-f', 'bin', 'src/test386.asm',
                             '-w-all', '-l', 'test386.lst', '-o', 'test386.bin'],
                            cwd=rom, capture_output=True, text=True)
    if result.returncode:
        raise Test386Error('NASM failed:\n' + result.stderr)
    if (rom / 'test386.bin').stat().st_size != ROM_BYTES:
        raise Test386Error('test386.bin is not the 128 KiB ROM')
    return rom


# ---- NASM listing ------------------------------------------------------------

LISTING_LINE = re.compile(r'^\s*\d+\s+([0-9A-F]{8})\s+([0-9A-F\[\]\(\)]+)(-?)\s*(?:<\d+>)?\s?(.*)$')
LABEL_LINE = re.compile(r'^\s*\d+\s+(?:<\d+>)?\s*([A-Za-z_.][\w.]*):\s*(?:;.*)?$')
# <n> marks a line from an included file or a macro expansion.
SECTION_LINE = re.compile(r'^\s*\d+\s+(?:<\d+>\s*)?section\s+(\S+)')
MACRO_LINE = re.compile(r'^\s*\d+\s+(?:[0-9A-F]{8}\s+[0-9A-F\[\]\(\)]+-?\s+)?<\d+>')
SOURCE_LINE = re.compile(r'^\s*\d+\s+(?:[0-9A-F]{8}\s+[0-9A-F\[\]\(\)]+-?\s+)?(.*)$')


def parse_listing(text: str) -> tuple[dict[int, dict], dict[str, int]]:
    """Map linear addresses to instructions and labels to addresses.

    Each instruction records its length, bytes, source text and origin: the
    top-level source line that produced it (the macro invocation for code a
    macro expands).
    """
    instructions: dict[int, dict] = {}
    labels: dict[str, int] = {}
    pending: list[str] = []
    section = None
    previous = None
    origin = ''
    for line in text.splitlines():
        directive = SECTION_LINE.match(line)
        if directive:
            section = SECTION_BASES.get(directive.group(1))  # code elsewhere is not in the ROM
        top = None if MACRO_LINE.match(line) else SOURCE_LINE.match(line)
        if top and top.group(1).split(';', 1)[0].strip():
            origin = top.group(1).split(';', 1)[0].strip()
        match = LISTING_LINE.match(line)
        if match is None:
            label = LABEL_LINE.match(line)
            if label and section is not None:
                pending.append(label.group(1))
            continue
        if section is None:
            continue
        address = section + int(match.group(1), 16)
        code = re.sub(r'[\[\]\(\)]', '', match.group(2)).lower()
        source = match.group(4).split(';', 1)[0].strip()
        if not source and previous is not None and previous['address'] + previous['length'] == address:
            previous['length'] += len(code) // 2  # continuation of a long instruction
            previous['bytes'] += code
            continue
        for label in pending:
            labels.setdefault(label, address)
        pending = []
        previous = {'address': address, 'length': len(code) // 2, 'source': source, 'bytes': code,
                    'origin': origin}
        instructions[address] = previous
    return instructions, labels


def normalise(source: str) -> str:
    return re.sub(r'\s+', ' ', source.strip().lower())


# ---- Divergence list ---------------------------------------------------------

def load_divergences(instructions: dict[int, dict], path: Path = vectors.DIVERGENCES) -> list[dict]:
    """[[test386]] entries, each resolved to the checks it steps over.

    A "branch" or "fault" entry names its checks by `origins` (test386 source
    lines; every instruction they expand to whose text is `listing`) or by one
    `address`. Code test386 copies into RAM is named by address and `bytes`.
    """
    entries = tomllib.loads(path.read_text()).get('test386', [])
    seen = set()
    for entry in entries:
        name = entry.get('id', '?')
        for key in ('id', 'kind', 'post', 'deciding_case', 'oracle', 'corpus', 'follow'):
            if key not in entry:
                raise Test386Error(f'test386 divergence {name} lacks {key}')
        if name in seen:
            raise Test386Error('duplicate divergence id: ' + name)
        seen.add(name)
        if entry['follow'] not in ('corpus', 'oracle'):
            raise Test386Error(f'{name}: follow is "corpus" or "oracle"')
        if entry['kind'] == 'ee':
            if not isinstance(entry.get('reference'), str):
                raise Test386Error(f'{name}: an EE divergence names its reference line')
            continue
        if entry['kind'] not in ('branch', 'fault') or not isinstance(entry.get('listing'), str):
            raise Test386Error(f'{name}: kind is branch, fault or ee; branch and fault name a listing')
        if ('origins' in entry) == ('address' in entry):
            raise Test386Error(f'{name}: give origins or address')
        entry['checks'] = {}
        if 'origins' in entry:
            if entry['deciding_case'] not in entry['origins']:
                raise Test386Error(f'{name}: the deciding case is one of its origins')
            for origin in entry['origins']:
                found = {address: item for address, item in instructions.items()
                         if item['origin'] == origin and normalise(item['source']) == normalise(entry['listing'])}
                if not found:
                    raise Test386Error(f'{name}: no {entry["listing"]!r} expands from {origin!r}')
                entry['checks'].update({address: (origin, item['length']) for address, item in found.items()})
        else:
            address, instruction = entry['address'], instructions.get(entry['address'])
            if instruction is None:
                code = entry.get('bytes', '')
                if not re.fullmatch(r'(?:[0-9a-f]{2})+', code) or not any(
                        item['bytes'] == code and normalise(item['source']) == normalise(entry['listing'])
                        for item in instructions.values()):
                    raise Test386Error(f'{name}: {address:#x} is outside the ROM and its bytes match no listing line')
                instruction = {'length': len(code) // 2, 'source': entry['listing']}
            elif normalise(instruction['source']) != normalise(entry['listing']):
                raise Test386Error(f'{name}: listing at {address:#x} is {instruction["source"]!r}, '
                                   f'not {entry["listing"]!r}')
            entry['checks'][address] = (entry['deciding_case'], instruction['length'])
        for address, (_, length) in entry['checks'].items():
            source = normalise(instructions.get(address, {'source': entry['listing']})['source'])
            if entry['kind'] == 'branch' and not re.match(r'(?:j\w+|call)\s', source):
                raise Test386Error(f'{name}: a branch check is a conditional jump or a call to error')
    return entries


# ---- Run ---------------------------------------------------------------------

def run_rom(testbench: Path, rom: Path, error: int, forgive: dict[int, int]) -> dict:
    arguments = [str(testbench), '--rom', str(rom / 'test386.bin'), '--post', hex(POST_PORT),
                 '--output', hex(OUTPUT_PORT), '--text', str(BUILD / 'test386-output.txt'),
                 '--dump', str(BUILD / 'test386-dump.bin'), '--dump-bytes', hex(DUMP_BYTES),
                 '--limit', str(STEP_LIMIT), '--error', hex(error)]
    for address, length in sorted(forgive.items()):
        arguments += ['--forgive', f'{address:#x}:{length}']
    result = subprocess.run(arguments, capture_output=True, text=True)
    if result.returncode:
        raise Test386Error('test386 testbench failed:\n' + result.stderr)
    run = {'posts': [], 'errors': [], 'forgiven': [], 'strays': [], 'end': None}
    for line in result.stdout.splitlines():
        words = line.split()
        if words[0] == 'POST':
            run['posts'].append(int(words[1], 16))
        elif words[0] == 'ERROR':
            run['errors'].append(int(words[1], 16))
        elif words[0] == 'FORGIVEN':
            run['forgiven'].append(int(words[1], 16))
        elif words[0] == 'STRAY':
            run['strays'].append(f'{words[1]} {words[2]}')
        elif words[0] == 'END':
            run['end'] = {'reason': words[1], 'steps': int(words[2]), 'cs': words[3], 'eip': words[4]}
    run['text'] = (BUILD / 'test386-output.txt').read_text(errors='replace')
    run['dump_sha256'] = hashlib.sha256((BUILD / 'test386-dump.bin').read_bytes()).hexdigest()
    return run


def ee_lines(text: str) -> list[str]:
    return [line.rstrip() for line in text.splitlines() if line.strip()]


def compare_ee(output: str, reference: str) -> list[tuple[str, str]]:
    """Pair oracle and reference EE lines; return the pairs that differ."""
    produced, expected = ee_lines(output), ee_lines(reference)
    differences = [(want, got) for want, got in zip(expected, produced) if want != got]
    for missing in expected[len(produced):]:
        differences.append((missing, '<missing>'))
    for extra in produced[len(expected):]:
        differences.append(('<none>', extra))
    return differences


def evaluate(run: dict, differences: list[tuple[str, str]], entries: list[dict],
             instructions: dict[int, dict], dump: bytes) -> int:
    status = 0
    posts = ' '.join(f'{code:02X}' for code in run['posts'])
    print(f'{CORPUS} {TEST386_REVISION[:12]}: POST {posts}; end {run["end"]}', flush=True)
    for address in run['errors']:
        source = instructions.get(address, {}).get('source', '?')
        post = run['posts'][-1] if run['posts'] else 0
        print(f'UNLISTED test386 failure at POST {post:02X}: {address:#x} {source!r}', flush=True)
        status = 1
    if not run['posts'] or run['posts'][-1] != 0xFF or run['end']['reason'] != 'halt':
        print('test386 did not finish with POST FFh', flush=True)
        status = 1
    for stray in run['strays']:
        print('STRAY access outside RAM and ROM:', stray, flush=True)
        status = 1
    for entry in entries:
        if entry['kind'] == 'ee':
            continue
        if 'bytes' in entry and dump[entry['address']:entry['address'] + len(entry['bytes']) // 2].hex() != entry['bytes']:
            print(f'{entry["id"]}: RAM at {entry["address"]:#x} does not hold the named instruction', flush=True)
            status = 1
        uses = {origin: 0 for origin, _ in entry['checks'].values()}
        for address in run['forgiven']:
            if address in entry['checks']:
                uses[entry['checks'][address][0]] += 1
        for origin, count in uses.items():
            if not count:
                print(f'STALE test386 divergence {entry["id"]}: {origin!r} no longer fails', flush=True)
                status = 1
        print(f'divergence {entry["id"]}: {sum(uses.values())} checks stepped over', flush=True)
    by_reference = {entry['reference']: entry for entry in entries if entry['kind'] == 'ee'}
    matched = set()
    for want, got in differences:
        entry = by_reference.get(want)
        if entry is not None and entry.get('oracle_line', got) == got:
            matched.add(entry['id'])
            continue
        print(f'UNLISTED EE difference: reference {want!r}, oracle {got!r}', flush=True)
        status = 1
    for entry in by_reference.values():
        if entry['id'] not in matched:
            print('STALE test386 EE divergence:', entry['id'], flush=True)
            status = 1
    print(f'EE lines: {len(ee_lines(run["text"]))}, differing from the reference: {len(differences)}', flush=True)
    print(f'final RAM ({DUMP_BYTES // 1024} KiB) SHA-256 {run["dump_sha256"]} in {BUILD / "test386-dump.bin"}',
          flush=True)
    return status


# ---- Cross-check with SingleStepTests ----------------------------------------
#
# test386's TEST_UNDEF cases name an input and the i386 result. Where a
# SingleStepTests state has the same inputs, the 386EX capture either agrees or
# the corpora disagree; disagreements must be listed under [[corpora]].

PS = {'CF': 0x001, 'PF': 0x004, 'AF': 0x010, 'ZF': 0x040, 'SF': 0x080, 'OF': 0x800}
PS_ARITH = sum(PS.values())
PREFIXES = {0x26, 0x2E, 0x36, 0x3E, 0x64, 0x65, 0xF0, 0xF2, 0xF3, 0x66, 0x67}
BYTE_REGISTERS = ('eax', 'ecx', 'edx', 'ebx')  # AL CL DL BL, then AH CH DH BH
WORD_REGISTERS = ('eax', 'ecx', 'edx', 'ebx', 'esp', 'ebp', 'esi', 'edi')


def constants(*texts: str) -> dict[str, int]:
    values: dict[str, int] = {}
    for text in texts:
        for name, expression in re.findall(r'^\s*(PS_\w+)\s+equ\s+(.+?)\s*(?:;.*)?$', text, re.MULTILINE):
            values[name] = evaluate_flags(expression, values)
    return values


def evaluate_flags(expression: str, values: dict[str, int]) -> int:
    total = 0
    for token in re.split(r'\|', expression.replace('(', '').replace(')', '')):
        token = token.strip()
        total |= int(token, 0) if re.fullmatch(r'0x[0-9a-fA-F]+|\d+', token) else values[token]
    return total


def test386_cases(source: Path) -> list[dict]:
    """The TEST_UNDEF flag cases of the pinned test386 (POST E0h)."""
    text = (source / 'src/test386.asm').read_text()
    values = constants((source / 'src/x86_e.asm').read_text(), text)
    start, end = text.index('bcd386FlagsTest:'), text.index('jmp arithLogicTests', text.index('rotate386FlagsTest:'))
    cases = []
    for line in text[start:end].splitlines():
        words = [word.strip() for word in line.split(';', 1)[0].replace('\t', ' ').split(None, 1)]
        if len(words) != 2:
            continue
        macro, args = words[0], [argument.strip() for argument in words[1].split(',')]
        if macro == 'testBCDflags':
            cases.append({'family': 'bcd', 'op': args[0], 'ax': int(args[1], 0),
                          'flags': evaluate_flags(args[2], values), 'expect': evaluate_flags(args[3], values)})
        elif macro in ('testShiftBFlags', 'testShiftWFlags'):
            cases.append({'family': 'shift', 'op': args[0], 'width': 8 if macro == 'testShiftBFlags' else 16,
                          'value': int(args[1], 0), 'count': int(args[2], 0),
                          'flags': evaluate_flags(args[3], values), 'expect': evaluate_flags(args[4], values)})
        elif macro == 'testBittestFlags':
            for op in ('bt', 'btc', 'btr', 'bts'):
                for width in (16, 32):
                    cases.append({'family': 'bittest', 'op': op, 'width': width, 'value': int(args[0], 0),
                                  'bit': int(args[1], 0), 'flags': evaluate_flags(args[2], values),
                                  'expect': evaluate_flags(args[3], values)})
    if not cases:
        raise Test386Error('no TEST_UNDEF cases found in test386.asm')
    return cases


BCD_FILES = {'aaa': '37', 'aas': '3F', 'daa': '27', 'das': '2F', 'aam': 'D4', 'aad': 'D5'}
SHIFT_FILES = {('rcl', 8): 'D2.2', ('rcr', 8): 'D2.3', ('shl', 8): 'D2.4', ('shr', 8): 'D2.5',
               ('rcl', 16): 'D3.2', ('rcr', 16): 'D3.3', ('shl', 16): 'D3.4', ('shr', 16): 'D3.5'}
BITTEST_EXTENSION = {'bt': 4, 'bts': 5, 'btr': 6, 'btc': 7}
BITTEST_REGISTER = {'bt': 'A3', 'bts': 'AB', 'btr': 'B3', 'btc': 'BB'}


def _opcode(test: vectors.MooTest) -> tuple[int, bytes]:
    """Return (index of the opcode byte, instruction bytes) after prefixes."""
    index = 0
    while test.instruction[index] in PREFIXES:
        index += 1
    return index, test.instruction


def _register(test: vectors.MooTest, number: int, width: int) -> int:
    if width == 8:
        value = test.initial[vectors.REGISTERS.index(BYTE_REGISTERS[number & 3])]
        return (value >> 8 if number & 4 else value) & 0xFF
    return test.initial[vectors.REGISTERS.index(WORD_REGISTERS[number])] & ((1 << width) - 1)


def _final_flags(test: vectors.MooTest) -> int:
    return test.final.get(vectors.EFLAGS, test.initial[vectors.EFLAGS])


def _modrm_register_form(test: vectors.MooTest):
    index, code = _opcode(test)
    position = index + (2 if code[index] == 0x0F else 1)
    modrm = code[position]
    return (modrm >> 6 == 3), modrm & 7, (modrm >> 3) & 7, code, position


def matching_states(case: dict, files: dict[str, vectors.MooFile]):
    """Yield (stem, test, compared flag mask) for corpus states with the case's inputs."""
    if case['family'] == 'bcd':
        stem = BCD_FILES[case['op']]
        for test in files[stem].tests:
            if test.exception:
                continue
            eax, flags = test.initial[vectors.REGISTERS.index('eax')], test.initial[vectors.EFLAGS]
            al, ah = eax & 0xFF, (eax >> 8) & 0xFF
            if case['op'] in ('aam', 'aad'):
                index, code = _opcode(test)
                if code[index + 1] != 0x0A or al != case['ax'] & 0xFF:
                    continue
                if case['op'] == 'aad' and ah != case['ax'] >> 8 & 0xFF:
                    continue
            else:
                relevant = PS['AF'] | (PS['CF'] if case['op'] in ('daa', 'das') else 0)
                if al != case['ax'] & 0xFF or (flags ^ case['flags']) & relevant:
                    continue
            yield stem, test, PS_ARITH
    elif case['family'] == 'shift':
        count = case['count'] & 31
        if count == 0:
            return  # flags are unchanged; nothing undefined to compare
        stem = SHIFT_FILES[(case['op'], case['width'])]
        rotate = case['op'] in ('rcl', 'rcr')
        for test in files[stem].tests:
            register, rm, _, _, _ = _modrm_register_form(test)
            if not register or test.exception:
                continue
            cl = test.initial[vectors.REGISTERS.index('ecx')] & 0xFF
            if (case['width'] == 8 and rm == 1) or (case['width'] == 16 and rm == 1):
                continue  # the count register is also the operand
            if cl & 31 != count or _register(test, rm, case['width']) != case['value']:
                continue
            if rotate and (test.initial[vectors.EFLAGS] ^ case['flags']) & PS['CF']:
                continue
            yield stem, test, (PS['CF'] | PS['OF']) if rotate else PS_ARITH
    else:
        prefix = '66' if case['width'] == 32 else ''
        for stem in (f'{prefix}0FBA.{BITTEST_EXTENSION[case["op"]]}', f'{prefix}0F{BITTEST_REGISTER[case["op"]]}'):
            immediate = '.' in stem
            for test in files[stem].tests:
                register, rm, reg, code, position = _modrm_register_form(test)
                if not register or test.exception:
                    continue
                offset = code[position + 1] if immediate else _register(test, reg, case['width'])
                if offset % case['width'] != case['bit'] or _register(test, rm, case['width']) != case['value']:
                    continue
                yield stem, test, PS['CF'] | PS['OF']


def state_cases(files: dict[str, vectors.MooFile]):
    """test386's non-flag i386 undefined results, checked on every matching state."""
    registers = vectors.REGISTERS
    for stem in ('6606', '660E', '6616', '661E', '660FA0', '660FA8'):
        for test in files[stem].tests:
            if test.exception:
                continue
            ss, sp = test.initial[registers.index('ss')], test.initial[registers.index('esp')] & 0xFFFF
            slot = (ss << 4) + ((sp - 4) & 0xFFFF)
            initial, final = dict(test.initial_ram), dict(test.final_ram)
            # test386: a 16-bit move leaves the upper word of the slot unchanged.
            agrees = all(final.get(address, initial.get(address)) == initial.get(address)
                         for address in (slot + 2, slot + 3))
            yield 'PUSH Sreg with a 32-bit operand writes 16 bits', stem, test, agrees
    for test in files['6661'].tests:
        if test.exception:
            continue
        ss, sp = test.initial[registers.index('ss')], test.initial[registers.index('esp')] & 0xFFFF
        initial = dict(test.initial_ram)
        image = [initial.get((ss << 4) + ((sp + 12 + n) & 0xFFFF)) for n in range(4)]
        if None in image:
            continue
        popped = int.from_bytes(bytes(image), 'little')
        esp = test.final.get(registers.index('esp'), test.initial[registers.index('esp')])
        # test386: the i386 loads ESP's upper word from the popped ESP image.
        yield 'POPAD with a 16-bit stack loads the high word of ESP', '6661', test, esp >> 16 == popped >> 16
    for stem, width in (('678D', 16), ('67668D', 32)):
        for test in files[stem].tests:
            index, code = _opcode(test)
            modrm, sib = code[index + 1], code[index + 2]
            mod, base = modrm >> 6, sib & 7
            if test.exception or mod == 3 or modrm & 7 != 4 or (sib >> 3) & 7 != 4 or not sib >> 6:
                continue
            if mod == 0 and base == 5:
                continue  # no base register to scale
            displacement = (int.from_bytes(code[index + 3:index + 4], 'little', signed=True) if mod == 1 else
                            int.from_bytes(code[index + 3:index + 7], 'little') if mod == 2 else 0)
            address = ((_register(test, base, 32) << (sib >> 6)) + displacement) & 0xFFFFFFFF
            destination = registers.index(WORD_REGISTERS[(modrm >> 3) & 7])
            result = test.final.get(destination, test.initial[destination])
            mask = (1 << width) - 1
            # test386: with SIB index 100b and a nonzero scale the i386 scales the base.
            yield 'LEA with SIB index 100b scales the base', stem, test, result & mask == address & mask


def cross_check(source: Path, corpus: Path, entries: list[dict]) -> int:
    needed = sorted({*BCD_FILES.values(), *SHIFT_FILES.values(), '6606', '660E', '6616', '661E', '660FA0',
                     '660FA8', '6661', '678D', '67668D', *(f'{p}0FBA.{n}' for p in ('', '66') for n in range(4, 8)),
                     *(f'{p}0F{o}' for p in ('', '66') for o in BITTEST_REGISTER.values())})
    paths = [corpus / f'{stem}.MOO.gz' for stem in needed]
    missing = [path.name for path in paths if not path.is_file()]
    if missing:
        raise Test386Error(f'cross-check needs SingleStepTests files in {corpus}: {", ".join(missing)}; '
                           'run test_fes_i386_vectors.py --download or pass --no-cross-check')
    vectors.verify(corpus, paths + [corpus / 'revocation_list.txt'])
    revoked = vectors.revoked_hashes((corpus / 'revocation_list.txt').read_text())
    files = {}
    for stem, path in zip(needed, paths):
        moo = vectors.parse_moo(gzip.decompress(path.read_bytes()))
        moo.tests = [test for test in moo.tests if test.hash not in revoked]
        files[stem] = moo
    used, status = set(), 0
    tally: dict[str, list[int]] = {}  # family -> [cases matched, cases, agreeing states, disagreeing states]

    def record(family: str, case_id: str, stem: str, test: vectors.MooTest, agrees: bool, detail: str = ''):
        nonlocal status
        counts = tally.setdefault(family, [0, 0, 0, 0])
        counts[2 if agrees else 3] += 1
        if agrees:
            return
        entry = next((item for item in entries if item['test386_case'] == case_id
                      and item['vectors_case'] == f'{stem}#{test.index}'), None)
        if entry is None or entry['vectors_hash'] != test.hash:
            print(f'UNLISTED corpus disagreement: test386 {case_id!r} vs {stem}#{test.index} {test.hash} '
                  f'{test.name!r} {detail}', flush=True)
            status = 1
        else:
            used.add(entry['id'])

    for case in test386_cases(source):
        family = f'{case["family"]} {case["op"]}'
        tally.setdefault(family, [0, 0, 0, 0])[1] += 1
        matches = list(matching_states(case, files))
        if matches:
            tally[family][0] += 1
        for stem, test, mask in matches:
            result = _final_flags(test) & mask
            record(family, case_label(case), stem, test, result == case['expect'] & mask,
                   f'flags {result:#06x}, test386 expects {case["expect"] & mask:#06x}')
    for case_id, stem, test, agrees in state_cases(files):
        tally.setdefault(case_id, [1, 1, 0, 0])
        record(case_id, case_id, stem, test, agrees)
    for entry in entries:
        if entry['id'] not in used:
            print('STALE corpora divergence entry:', entry['id'], flush=True)
            status = 1
    for family, (matched, cases, agreeing, disagreeing) in sorted(tally.items()):
        print(f'cross-check {family}: {matched}/{cases} cases with matching states, '
              f'{agreeing} agree, {disagreeing} disagree', flush=True)
    return status


def case_label(case: dict) -> str:
    if case['family'] == 'bcd':
        return f'{case["op"]} AX={case["ax"]:04X} FLAGS={case["flags"]:04X}'
    if case['family'] == 'shift':
        return f'{case["op"]} {case["width"]}-bit {case["value"]:#x} by {case["count"]} FLAGS={case["flags"]:04X}'
    return f'{case["op"]} {case["width"]}-bit {case["value"]:#x} bit {case["bit"]} FLAGS={case["flags"]:04X}'


def load_corpora_divergences(path: Path = vectors.DIVERGENCES) -> list[dict]:
    entries = tomllib.loads(path.read_text()).get('corpora', [])
    for entry in entries:
        for key in ('id', 'test386_case', 'vectors_case', 'vectors_hash', 'follow', 'reason'):
            if key not in entry:
                raise Test386Error(f'corpora divergence {entry.get("id", "?")} lacks {key}')
        if entry['follow'] not in (CORPUS, vectors.CORPUS):
            raise Test386Error(f'{entry["id"]}: follow names one of the two corpora')
    return entries


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true',
                        help='explicitly fetch pinned test386.asm, NASM and the oracle into ignored build/')
    parser.add_argument('--vectors-corpus', type=Path, help='SingleStepTests/80386 data for the cross-check')
    parser.add_argument('--no-cross-check', action='store_true')
    parser.add_argument('--jobs', type=int, default=os.cpu_count() or 2)
    args = parser.parse_args()
    if not 1 <= args.jobs <= 64:
        parser.error('invalid job count')
    if args.vectors_corpus is not None and not args.vectors_corpus.is_absolute():
        parser.error('--vectors-corpus must be absolute')
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    BUILD.mkdir(parents=True, exist_ok=True)
    try:
        source = test386_source(args.download)
        rom = assemble(source, nasm(args.download, args.jobs))
        instructions, labels = parse_listing((rom / 'test386.lst').read_text(errors='replace'))
        if 'error' not in labels:
            raise Test386Error('the listing has no error label')
        entries = load_divergences(instructions)
        testbench = oracle.build_testbench('i386-test386', oracle.SIM / 'test386_tb.cpp',
                                           jobs=args.jobs, download=args.download)
        forgive = {address: length for entry in entries if entry['kind'] != 'ee'
                   for address, (_, length) in entry['checks'].items()}
        run = run_rom(testbench, rom, labels['error'], forgive)
        differences = compare_ee(run['text'], (source / 'test386-EE-reference.txt').read_text())
        status = evaluate(run, differences, entries, instructions, (BUILD / 'test386-dump.bin').read_bytes())
        report = {'revision': TEST386_REVISION, 'configuration': CONFIGURATION,
                  'rom_sha256': hashlib.sha256((rom / 'test386.bin').read_bytes()).hexdigest(),
                  'posts': run['posts'], 'end': run['end'], 'forgiven': [hex(a) for a in run['forgiven']],
                  'ee_differences': differences, 'dump_sha256': run['dump_sha256']}
        if not args.no_cross_check:
            corpus = args.vectors_corpus or vectors.OUTPUT / 'corpus' / vectors.REVISION
            status |= cross_check(source, corpus, load_corpora_divergences())
        (BUILD / 'report.json').write_text(json.dumps(report, indent=1) + '\n')
        return status
    except (OSError, Test386Error, vectors.CorpusError, oracle.OracleError, KeyError,
            tomllib.TOMLDecodeError, subprocess.CalledProcessError) as error:
        print('test386 qualification error:', error, file=sys.stderr)
        return 2


if __name__ == '__main__':
    raise SystemExit(main())
