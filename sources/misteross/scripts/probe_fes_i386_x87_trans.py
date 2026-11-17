#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Measure how far the Bochs oracle and the host's x87 disagree on the x87
transcendentals (#1958).

Host simulation only; no RTL. cores/fes-common/sim/i386/x87_trans_probe.cpp
runs F2XM1, FYL2X, FPTAN, FPATAN, FYL2XP1, FSINCOS, FSIN and FCOS on the
oracle and on this host's FPU over the same operands and reports the distance
between the two results in representable extended values, per operand class.

Neither reference is a 387. The lockstep compares the RTL with the oracle and
the unit bench compares it with the host, so a tolerance that both gates are
to pass under cannot be narrower than what this measures; that is all the
figure is evidence for. It says nothing about a 387's own results.

With --brackets it also judges each reference on its own against a third:
QEMU's tests/tcg/i386 tables for F2XM1, FYL2X, FYL2XP1 and FPATAN, whose rows
give the two extended values that bracket the mathematical result. Those
files are GPL-2.0-or-later test inputs, pinned below by commit and SHA-256,
fetched into the ignored build/ directory only with --download, parsed at run
time and never committed; the report carries counts, not their rows.

No network access occurs unless --download is given (the pinned oracle,
scripts/fes_i386_oracle.py, and with --brackets the pinned QEMU tables).

    python3 scripts/probe_fes_i386_x87_trans.py --samples 20000 --output report.json

The report names the FES revision, the tracked files differing from it, a
SHA-256 for each source it ran, the oracle's build record and the host CPU.
"""
from __future__ import annotations

import argparse
from fractions import Fraction
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from scripts import fes_i386_oracle as oracle  # noqa: E402

SOURCES = (oracle.SIM / 'x87_trans_probe.cpp', oracle.SIM / 'i386_oracle.h', oracle.SIM / 'bochs_oracle.cpp',
           Path(__file__).resolve(), Path(oracle.__file__).resolve())


# QEMU v11.1.3. The tables are test inputs: fetched, parsed, never committed.
QEMU_REVISION = '005c61c0ef6f6ae615283440b4bb7be51b2c6ddd'
QEMU_URL = 'https://gitlab.com/qemu-project/qemu/-/raw/{revision}/tests/tcg/i386/{name}'
QEMU_LICENSE = 'GPL-2.0-or-later'
# The file, its SHA-256, the instruction, and which table columns are the
# operand loaded first (ST(1) of a two-operand form) and second (ST(0)).
QEMU_TABLES = (
    ('test-i386-f2xm1.c', 'f88f87576a477fcb9d89073983aeedb44e7a68efedba250a7495afbf1ae9be0b', 'F2XM1', 1),
    ('test-i386-fpatan.c', '5a511b4c74ca2001ad74eda869e3c9e06169e5469b2d0b42084da3c738fc1cef', 'FPATAN', 2),
    ('test-i386-fyl2x.c', '080ffac186bb996f8bbffaad5940ebfbb85844bd1ffd74eb2854c0d84d0c014a', 'FYL2X', 2),
    ('test-i386-fyl2xp1.c', 'bb0cf7cba34e41fa76f805fe62e4b18bd880dd70fb4f653ed12b7c5a7e745758', 'FYL2XP1', 2),
)
QEMU_BUILD = ROOT / 'build/sim/fes-i386-x87-qemu' / QEMU_REVISION
TOKEN = re.compile(r'-?(?:0x[0-9a-f]+(?:\.[0-9a-f]*)?p[+-]?\d+L|\d+\.\d+L|__builtin_infl\(\))')


def extended(token: str) -> str:
    """A C long double literal as an x87 extended value, 20 hex digits. Exact
    for the hex-float literals; a decimal literal rounds to nearest even."""
    sign = token.startswith('-')
    body = token.lstrip('-')
    if body == '__builtin_infl()':
        return f'{0x7fff | sign << 15:04x}{1 << 63:016x}'
    if body.startswith('0x'):
        mantissa, exponent = body[2:-1].split('p')
        whole, _, fraction = mantissa.partition('.')
        value = Fraction(int(whole + fraction, 16), 16 ** len(fraction)) * Fraction(2) ** int(exponent)
    else:
        value = Fraction(body[:-1])
    if value == 0:
        return f'{sign << 15:04x}{0:016x}'
    exponent = value.numerator.bit_length() - value.denominator.bit_length()
    if Fraction(2) ** exponent > value:
        exponent -= 1
    biased = max(exponent + 16383, 1)          # a denormal keeps the exponent of the smallest normal
    scaled = value / Fraction(2) ** (biased - 16383 - 63)
    significand = scaled.numerator // scaled.denominator
    remainder = scaled - significand
    if remainder > Fraction(1, 2) or remainder == Fraction(1, 2) and significand & 1:
        significand += 1
    if significand >> 64:
        significand >>= 1
        biased += 1
    if not significand >> 63:
        biased = 0
    return f'{biased | sign << 15:04x}{significand:016x}'


def qemu_vectors(download: bool) -> tuple[Path, dict]:
    """Write the bracket vectors from the pinned QEMU tables; return the file
    and what it was made from."""
    QEMU_BUILD.mkdir(parents=True, exist_ok=True)
    lines, counts = [], {}
    for name, digest, instruction, operands in QEMU_TABLES:
        path = QEMU_BUILD / name
        if not path.is_file():
            if not download:
                raise oracle.OracleError(f'{path} is missing: pass --download to fetch the pinned QEMU tables')
            with urllib.request.urlopen(QEMU_URL.format(revision=QEMU_REVISION, name=name), timeout=60) as response:
                path.write_bytes(response.read())
        if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise oracle.OracleError(f'{path} is not the pinned QEMU table')
        rows, inside = 0, False
        for line in path.read_text().splitlines():
            if 'tests[] = {' in line:
                inside = True
            elif line == '};':
                inside = False
            elif inside and line.lstrip().startswith('{'):
                tokens = TOKEN.findall(line)
                if len(tokens) != operands + 2:
                    raise oracle.OracleError(f'{name}: cannot parse {line.strip()[:40]!r}')
                values = [extended(token) for token in tokens]
                # arg0 is ST(0) and arg1 ST(1); the probe loads ST(1) first.
                first, second = (values[1], values[0]) if operands == 2 else (values[0], '0' * 20)
                lines.append(f'{instruction} {first} {second} {values[-2]} {values[-1]}')
                rows += 1
        if not rows:
            raise oracle.OracleError(f'{name}: no table rows')
        counts[instruction] = rows
    vectors = QEMU_BUILD / 'brackets.txt'
    vectors.write_text('\n'.join(lines) + '\n')
    return vectors, {'revision': QEMU_REVISION, 'license': QEMU_LICENSE,
                     'files_sha256': {name: digest for name, digest, _, _ in QEMU_TABLES}, 'rows': counts}


CLASS_EDGES = ((0, '0'), (1, '1'), (2, '2'), (4, '4'), (16, '16'), (256, '256'), (65536, '65536'))


def _ordinal(text: str):
    """(sign, the integer that orders the magnitudes) of a finite non-zero
    normal or denormal value; None for anything else."""
    sign_exponent, significand = int(text[:4], 16), int(text[4:], 16)
    exponent = sign_exponent & 0x7fff
    if exponent == 0x7fff or significand == 0 or bool(significand >> 63) != bool(exponent):
        return None
    return sign_exponent >> 15, significand if exponent == 0 else (exponent - 1) << 63 | significand


def compare_hosts(first: Path, second: Path) -> dict:
    """Two --host-dump files, line by line: per instruction and operand class,
    how many results are bit-identical, the whole histogram of the distance
    between the two hosts' results, and how often the flags and the condition
    codes differ. Class -1 is the QEMU bracket tables' operands."""
    def header(path: Path) -> tuple[dict, list[str]]:
        lines = path.read_text().splitlines()
        if not lines or not lines[0].startswith('# host-dump '):
            raise oracle.OracleError(f'{path} is not a host dump')
        fields = dict(re.findall(r'(\w+)=("[^"]*"|\S+)', lines[0]))
        return ({key: value.strip('"') for key, value in fields.items()}
                | {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(), 'rows': len(lines) - 1}), lines[1:]
    (meta_a, rows_a), (meta_b, rows_b) = header(first), header(second)
    if len(rows_a) != len(rows_b):
        raise oracle.OracleError('the two dumps are of different sweeps')
    classes: dict[tuple[str, str], dict] = {}
    for line_a, line_b in zip(rows_a, rows_b):
        a, b = line_a.split(), line_b.split()
        if a[:4] != b[:4]:
            raise oracle.OracleError('the two dumps are of different operands')
        row = classes.setdefault((a[0], a[1]), {
            'count': 0, 'values': 0, 'identical': 0, 'not_a_distance': 0, 'flags_differ': 0, 'c0_c2_c3_differ': 0,
            'c1_differ': 0, 'distance': {label: 0 for _, label in CLASS_EDGES} | {'more': 0}, 'worst_distance': 0})
        row['count'] += 1
        status = int(a[6], 16) ^ int(b[6], 16)
        row['flags_differ'] += bool(status & 0x00ff)
        row['c0_c2_c3_differ'] += bool(status & 0x4500)
        row['c1_differ'] += bool(status & 0x0200)
        # FPTAN's value is under its pushed 1.0; FSINCOS has two; the rest one.
        for column in {'FPTAN': (5,), 'FSINCOS': (4, 5)}.get(a[0], (4,)):
            row['values'] += 1
            if a[column] == b[column]:
                row['identical'] += 1
                row['distance']['0'] += 1
                continue
            x, y = _ordinal(a[column]), _ordinal(b[column])
            if x is None or y is None or x[0] != y[0]:
                row['not_a_distance'] += 1
                continue
            distance = abs(x[1] - y[1])
            row['worst_distance'] = max(row['worst_distance'], distance)
            # Each value lands in exactly one bin: the smallest edge it is within.
            row['distance'][next((label for edge, label in CLASS_EDGES if distance <= edge), 'more')] += 1
    return {'what': 'distance between two hosts\' own x87 units on the x87 transcendentals; no RTL and no oracle',
            'hosts': [meta_a, meta_b],
            'probe_sha256': hashlib.sha256((oracle.SIM / 'x87_trans_probe.cpp').read_bytes()).hexdigest(),
            'classes': [{'instruction': name, 'class': int(index), **row}
                        for (name, index), row in sorted(classes.items())]}


def source_record() -> dict:
    repository = ROOT.parents[1]

    def git(*arguments: str) -> str:
        return subprocess.run(['git', '-C', str(repository), *arguments], capture_output=True, text=True,
                              check=True).stdout
    files = {path.relative_to(repository).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
             for path in SOURCES}
    return {'fes_revision': git('rev-parse', 'HEAD').strip(),
            'tracked_changes': sorted(line[3:] for line in
                                      git('status', '--porcelain', '--untracked-files=no').splitlines()),
            'files': files}


def host_cpu() -> str:
    for line in Path('/proc/cpuinfo').read_text().splitlines():
        if line.startswith('model name'):
            return line.split(':', 1)[1].strip()
    return 'unknown'


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--samples', type=int, default=20000, help='operands per operand class (default 20000)')
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--download', action='store_true',
                        help='explicitly fetch the pinned oracle, and with --brackets the pinned QEMU tables, '
                             'into ignored build/')
    parser.add_argument('--brackets', action='store_true',
                        help="also judge each reference against QEMU's bracket tables")
    parser.add_argument('--jobs', type=int, default=4)
    parser.add_argument('--compare-hosts', type=Path, nargs=2, metavar='DUMP',
                        help="compare two hosts' `x87_trans_probe --host-dump` files instead of probing "
                             '(the probe\'s header says how to build it on a host with no oracle)')
    parser.add_argument('--output', type=Path, required=True, help='the report to write (absolute)')
    args = parser.parse_args()
    if not args.output.is_absolute():
        parser.error('--output must be absolute')
    if args.compare_hosts:
        try:
            report = compare_hosts(*args.compare_hosts)
        except (OSError, oracle.OracleError) as error:
            print('x87 transcendental probe error:', error, file=sys.stderr)
            return 2
        args.output.write_text(json.dumps(report, indent=2) + '\n')
        print(f'x87 host comparison: {report["hosts"][0]["cpu"]} against {report["hosts"][1]["cpu"]} '
              f'(report: {args.output})')
        return 0
    try:
        source = source_record()
        linked = oracle.link_inputs(args.jobs, args.download)
        probe = oracle.build_testbench('i386-x87-trans-probe', oracle.SIM / 'x87_trans_probe.cpp', args.jobs)
        result = subprocess.run([str(probe), str(args.samples), str(args.seed)], capture_output=True, text=True,
                                check=False)
        if result.returncode:
            print(result.stdout[-2000:], result.stderr[-2000:], file=sys.stderr)
            raise oracle.OracleError(f'the probe exited {result.returncode}')
        measured = json.loads(result.stdout)
        if args.brackets:
            vectors, qemu = qemu_vectors(args.download)
            result = subprocess.run([str(probe), '--vectors', str(vectors)], capture_output=True, text=True,
                                    check=False)
            if result.returncode:
                print(result.stdout[-2000:], result.stderr[-2000:], file=sys.stderr)
                raise oracle.OracleError(f'the bracket run exited {result.returncode}')
            bracketed = json.loads(result.stdout)
            if not bracketed['oracle_ran_every_program']:
                raise oracle.OracleError('the oracle did not finish every bracket program')
            if {name: row['count'] for name, row in bracketed['brackets'].items()} != qemu['rows']:
                raise oracle.OracleError('the bracket run did not judge every parsed row')
            measured['qemu'] = qemu
            measured['brackets'] = bracketed['brackets']
        if source_record() != source:
            raise oracle.OracleError('the probed revision or its sources changed during the run')
        record = oracle.build_record(linked)
    except (OSError, oracle.OracleError, json.JSONDecodeError, subprocess.CalledProcessError) as error:
        print('x87 transcendental probe error:', error, file=sys.stderr)
        return 2
    report = {'what': 'distance between the Bochs oracle and the host x87 on the x87 transcendentals; no RTL',
              'source': source, 'oracle': record, 'host_cpu': host_cpu(), **measured}
    args.output.write_text(json.dumps(report, indent=2) + '\n')
    print(f'x87 transcendental probe: {args.samples} operands per class, seed {args.seed} '
          f'(host simulation only; report: {args.output})')
    return 0


if __name__ == '__main__':
    sys.exit(main())
