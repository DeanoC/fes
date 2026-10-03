#!/usr/bin/env python3
"""Optional NMOS instruction-state qualification against external JSON data.

No network access occurs unless --download is explicitly supplied. The data
oracle is SingleStepTests/z80 v1 at the fixed revision below, MIT licensed
Copyright (c) 2024 SingleStepTests. Its README says these tests were generated
from a translated Ares core and subsequent fixes. They are independent software
expectations, not physical Zilog measurements. Only README, LICENSE, repository
metadata and JSON test data are consumed; never generator/CPU implementation.

Example (downloads selected data into the ignored build directory):
  python3 scripts/test_fes_z80_vectors.py --download --group base --group cb
Example with an existing local v1 corpus:
  python3 scripts/test_fes_z80_vectors.py --corpus /absolute/z80/v1 --opcode 'ed b0'

One retired instruction corresponds to one fixture, including one repeat-block
iteration or HALT entry. --pins runs the complete NMOS wrapper and compares
total T states, ordered memory/I/O transactions and observed M1/refresh cycles.
The corpus simplifies memory strobes to one T state and omits M1/RFSH, so exact
strobe widths, half-cycle phases and unstrobed addresses are not compared.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import urllib.parse
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
REVISION = 'ebe1875d48f374bcfd4b505d8eb8ee751568b5f7'
REPOSITORY = 'https://github.com/SingleStepTests/z80'
RAW = 'https://raw.githubusercontent.com/SingleStepTests/z80/' + REVISION + '/'
API = 'https://api.github.com/repos/SingleStepTests/z80/git/trees/' + REVISION + '?recursive=1'
FIELDS = ('pc', 'sp', 'a', 'b', 'c', 'd', 'e', 'f', 'h', 'l', 'i', 'r', 'ei',
          'wz', 'ix', 'iy', 'af_', 'bc_', 'de_', 'hl_', 'im', 'p', 'q', 'iff1', 'iff2')
WORD_FIELDS = frozenset({'pc', 'sp', 'wz', 'ix', 'iy', 'af_', 'bc_', 'de_', 'hl_'})
BIT_FIELDS = frozenset({'ei', 'p', 'iff1', 'iff2'})
GROUPS = ('base', 'cb', 'ed', 'dd', 'fd', 'ddcb', 'fdcb')
FIXTURE_NAME = re.compile(r'(?:[0-9a-f]{2}|(?:cb|ed|dd|fd) [0-9a-f]{2}|(?:dd|fd) cb __ [0-9a-f]{2})\.json')


def fetch(url):
    request = urllib.request.Request(url, headers={'User-Agent': 'FES-Z80-data-qualification'})
    with urllib.request.urlopen(request, timeout=60) as response:
        data = response.read(16 * 1024 * 1024 + 1)
    if len(data) > 16 * 1024 * 1024:
        raise ValueError('fixture/metadata exceeds 16 MiB: ' + url)
    return data


def family(name):
    if name.startswith('dd cb '): return 'ddcb'
    if name.startswith('fd cb '): return 'fdcb'
    return name.split(' ')[0] if ' ' in name else 'base'


def selected(name, args):
    if not FIXTURE_NAME.fullmatch(name): return False
    return ((not args.group or family(name) in args.group)
            and (not args.opcode or name[:-5] in args.opcode))


def require_opcodes(names, args):
    missing = set(args.opcode or ()) - {name[:-5] for name in names}
    if missing:
        raise ValueError('requested opcode fixtures are absent: ' + ', '.join(sorted(missing)))


def download(corpus, args):
    corpus.mkdir(parents=True, exist_ok=True)
    tree = json.loads(fetch(API))
    if tree.get('truncated'):
        raise ValueError('GitHub returned a truncated fixture inventory')
    entries = [entry for entry in tree['tree']
               if entry['type'] == 'blob' and entry['path'].startswith('v1/')
               and selected(entry['path'][3:], args)]
    if not entries:
        raise ValueError('no selected fixture names at pinned revision')
    require_opcodes((entry['path'][3:] for entry in entries), args)
    old_path = corpus / 'fes-z80-corpus.json'
    old = json.loads(old_path.read_text()) if old_path.exists() else {}
    old_files = old.get('files', {}) if old.get('revision') == REVISION else {}

    def receive(entry):
        name = entry['path'][3:]
        target = corpus / name
        prior = old_files.get(name, {})
        if (target.is_file() and prior.get('count', 0) >= args.limit_per_opcode
                and hashlib.sha256(target.read_bytes()).hexdigest() == prior.get('sha256')):
            return name, prior
        raw = fetch(RAW + urllib.parse.quote(entry['path']))
        git_sha = hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()
        if git_sha != entry['sha']:
            raise ValueError('download does not match pinned Git blob: ' + name)
        cases = json.loads(raw)
        if not isinstance(cases, list) or not cases:
            raise ValueError('expected a nonempty fixture array: ' + name)
        cases = cases[:args.limit_per_opcode]
        encoded = (json.dumps(cases, separators=(',', ':')) + '\n').encode()
        temporary = target.with_suffix('.tmp')
        temporary.write_bytes(encoded)
        temporary.replace(target)
        return name, {'count': len(cases), 'sha256': hashlib.sha256(encoded).hexdigest(),
                      'source_git_blob': entry['sha'], 'source_sha256': hashlib.sha256(raw).hexdigest()}

    print(f'Downloading {len(entries)} fixture files at {REVISION} (data only)', flush=True)
    with ThreadPoolExecutor(max_workers=args.download_jobs) as executor:
        records = dict(executor.map(receive, entries))
    (corpus / 'LICENSE.SingleStepTests').write_bytes(fetch(RAW + 'LICENSE'))
    (corpus / 'README.SingleStepTests.MD').write_bytes(fetch(RAW + 'README.MD'))
    old_files.update(records)
    manifest = {'repository': REPOSITORY, 'revision': REVISION, 'format': 'v1',
                'license': 'MIT', 'files': old_files}
    old_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n')


def number(value, maximum, label):
    if type(value) is not int or not 0 <= value <= maximum:
        raise ValueError('invalid fixture integer: ' + label)
    return value


def registers(state, label):
    if not isinstance(state, dict): raise ValueError('invalid state: ' + label)
    values = []
    for field in FIELDS:
        maximum = 65535 if field in WORD_FIELDS else 1 if field in BIT_FIELDS else 2 if field == 'im' else 255
        values.append(number(state[field], maximum, label + '.' + field))
    if state['q'] not in (0, state['f']):
        raise ValueError('Q encoding differs from documented corpus: ' + label)
    return values


def ram(state, label):
    values, seen = [], set()
    entries = state['ram']
    if not isinstance(entries, list) or len(entries) > 65536:
        raise ValueError('invalid RAM list: ' + label)
    for entry in entries:
        if not isinstance(entry, list) or len(entry) != 2:
            raise ValueError('invalid RAM pair: ' + label)
        address = number(entry[0], 65535, label + '.address')
        if address in seen: raise ValueError('duplicate RAM address: ' + label)
        seen.add(address)
        values.extend((address, number(entry[1], 255, label + '.value')))
    return [len(entries), *values]


def pin_expectations(case, label):
    cycles = case['cycles']
    if not isinstance(cycles, list) or not 1 <= len(cycles) <= 1024:
        raise ValueError('invalid cycle list: ' + label)
    transactions = []
    kinds = {'r-m-': 0, '-wm-': 1, 'r--i': 2, '-w-i': 3}
    for cycle in cycles:
        if not isinstance(cycle, list) or len(cycle) != 3:
            raise ValueError('invalid cycle triple: ' + label)
        address, data, pins = cycle
        if address is not None: number(address, 65535, label + '.cycle.address')
        if data is not None: number(data, 255, label + '.cycle.data')
        if not isinstance(pins, str): raise ValueError('invalid cycle pins: ' + label)
        if pins != '----' and (pins not in kinds or address is None):
            raise ValueError('unsupported corpus pin encoding: ' + label)
    for index, (address, data, pins) in enumerate(cycles):
        if pins == '----': continue
        # Simplified read strobes precede the fixture's sampled read byte;
        # write bytes are supplied alongside their simplified write strobe.
        value = data if pins[1] == 'w' else cycles[index + 1][1] if index + 1 < len(cycles) else None
        transactions.extend((kinds[pins], address, number(value, 255, label + '.transaction.data')))

    # Public Z80 prefix rules supply the M1 count because the corpus omits M1.
    # Indexed CB displacement/operation bytes are ordinary reads, not M1s.
    memory = dict(case['initial']['ram'])
    pc, indexed, m1_count = case['initial']['pc'], False, 0
    for _ in range(256):
        if pc not in memory: raise ValueError('missing opcode RAM for M1 count: ' + label)
        opcode = memory[pc]
        pc = (pc + 1) & 65535
        m1_count += 1
        if opcode in (0xdd, 0xfd):
            indexed = True
            continue
        if opcode == 0xed or (opcode == 0xcb and not indexed):
            m1_count += 1
        break
    else:
        raise ValueError('unterminated prefix sequence: ' + label)
    i_reg, r_reg = case['initial']['i'], case['initial']['r']
    refreshes = [(i_reg << 8) | (r_reg & 128) | ((r_reg + index) & 127)
                 for index in range(m1_count)]
    return [len(cycles), len(transactions) // 3, *transactions, len(refreshes), *refreshes]


def write_stream(files, output, args):
    count = 0
    with output.open('w') as stream:
        for path in files:
            cases = json.loads(path.read_text())
            if not isinstance(cases, list) or not cases:
                raise ValueError('expected a nonempty fixture array: ' + str(path))
            for case in cases[:args.limit_per_opcode]:
                if not isinstance(case, dict): raise ValueError('invalid fixture object: ' + str(path))
                if not isinstance(case['name'], str): raise ValueError('invalid fixture name')
                label = urllib.parse.quote(path.stem, safe='') + '/' + urllib.parse.quote(case['name'], safe='')
                values = registers(case['initial'], label) + registers(case['final'], label)
                values += ram(case['initial'], label) + ram(case['final'], label)
                ports = case.get('ports', [])
                if not isinstance(ports, list) or len(ports) > 65536:
                    raise ValueError('invalid ports list: ' + label)
                values.append(len(ports))
                for port in ports:
                    if not isinstance(port, list) or len(port) != 3 or port[2] not in ('r', 'w'):
                        raise ValueError('invalid port triple: ' + label)
                    values.extend((number(port[0], 65535, label + '.port'),
                                   number(port[1], 255, label + '.port.value'), int(port[2] == 'w')))
                if args.pins:
                    values += pin_expectations(case, label)
                stream.write(label + ' ' + ' '.join(map(str, values)) + '\n')
                count += 1
    return count


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--corpus', type=Path)
    parser.add_argument('--download', action='store_true', help='explicitly fetch pinned JSON data into ignored build/')
    parser.add_argument('--pins', action='store_true', help='qualify complete NMOS wrapper state, T count, ordered bus accesses and M1/refresh')
    parser.add_argument('--group', choices=GROUPS, action='append')
    parser.add_argument('--opcode', action='append', help="exact fixture stem, e.g. 'ed b0' or 'dd cb __ 46'")
    parser.add_argument('--limit-per-opcode', type=int, default=100)
    parser.add_argument('--max-detailed-failures', type=int, default=20)
    parser.add_argument('--download-jobs', type=int, default=8)
    parser.add_argument('--jobs', type=int, default=2)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if not 1 <= args.limit_per_opcode <= 1000: parser.error('--limit-per-opcode must be 1..1000')
    if not 1 <= args.jobs <= 32 or not 1 <= args.download_jobs <= 16: parser.error('invalid job count')
    if args.max_detailed_failures < 0: parser.error('--max-detailed-failures must be nonnegative')
    if args.corpus is not None and not args.corpus.is_absolute(): parser.error('--corpus must be absolute')
    if args.download and args.corpus is not None: parser.error('--download uses ignored build/; use --corpus for existing data')
    if not args.download and args.corpus is None: parser.error('supply --corpus or explicitly request --download')
    if args.opcode:
        args.opcode = [opcode.lower() for opcode in args.opcode]
        for opcode in args.opcode:
            if not FIXTURE_NAME.fullmatch(opcode + '.json'):
                parser.error('invalid opcode fixture stem: ' + opcode)
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    corpus = args.corpus or ROOT / 'build/sim/fes-z80-vectors/corpus' / REVISION
    output = ROOT / 'build/sim/fes-z80-vectors'
    output.mkdir(parents=True, exist_ok=True)
    try:
        if args.download: download(corpus, args)
        if not corpus.is_dir(): raise ValueError('corpus directory does not exist: ' + str(corpus))
        files = sorted(path for path in corpus.glob('*.json') if selected(path.name, args))
        if not files: raise ValueError('no selected opcode fixtures in corpus')
        require_opcodes((path.name for path in files), args)
        manifest_path = corpus / 'fes-z80-corpus.json'
        if manifest_path.exists():
            manifest = json.loads(manifest_path.read_text())
            if (not isinstance(manifest, dict) or not isinstance(manifest.get('files'), dict)
                    or not isinstance(manifest.get('repository'), str)
                    or not isinstance(manifest.get('revision'), str)):
                raise ValueError('invalid corpus receipt: ' + str(manifest_path))
            for path in files:
                record = manifest['files'].get(path.name)
                if not isinstance(record, dict) or hashlib.sha256(path.read_bytes()).hexdigest() != record.get('sha256'):
                    raise ValueError('cached data differs from corpus receipt: ' + path.name)
            print('Corpus:', manifest['repository'], 'revision', manifest['revision'], flush=True)
        else:
            print('Local corpus: revision/provenance not verified by this runner', flush=True)
        stream = output / ('pin-fixtures.txt' if args.pins else 'fixtures.txt')
        count = write_stream(files, stream, args)
        print(f'Selected {len(files)} opcode files, {count} vectors', flush=True)
        top = 'fes_z80_nmos' if args.pins else 'fes_z80_engine'
        build = output / ('pins' if args.pins else 'engine')
        build.mkdir(exist_ok=True)
        rtl = ROOT / 'cores/fes-common/rtl/z80'
        command = [args.verilator, '--cc', '--exe', '--build', '--public-flat-rw',
                   '--top-module', top, '-Wall', '-j', str(args.jobs),
                   '--Mdir', str(build), '-CFLAGS', '-std=c++17 -O2' + (' -DPIN_QUALIFICATION' if args.pins else ''),
                   str(rtl / 'fes_z80_alu.sv'), str(rtl / 'fes_z80_engine.sv'),
                   *([str(rtl / 'fes_z80_bus.sv'), str(rtl / 'fes_z80_nmos.sv')] if args.pins else []),
                   str(ROOT / 'cores/fes-common/sim/z80/vectors_tb.cpp')]
        log = output / ('pin-build.log' if args.pins else 'build.log')
        with log.open('w') as destination:
            result = subprocess.run(command, cwd=ROOT, stdout=destination, stderr=subprocess.STDOUT)
        if result.returncode:
            sys.stderr.write(log.read_text())
            return result.returncode
        return subprocess.run([str(build / ('V' + top)), str(stream), str(args.max_detailed_failures)], cwd=ROOT).returncode
    except (OSError, ValueError, KeyError, json.JSONDecodeError) as error:
        print('External-vector qualification error:', error, file=sys.stderr)
        return 2


if __name__ == '__main__':
    raise SystemExit(main())
