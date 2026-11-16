#!/usr/bin/env python3
"""Run the slice-3 extF80 TestFloat gate against the i386 x87 unit.

Build Berkeley TestFloat at the pinned revision and pass its ``testfloat_gen``
binary. Level 1 vectors cover add, sub, mul, eq, le, lt and exact roundToInt
for all three x87 precision settings and four rounding modes. The generated
vectors are checked against the unit through the same CPU-side interface
bench used by ``test_fes_i386_x87_unit.py``.

Example:
  python3 scripts/test_fes_i386_x87_testfloat.py \
    --testfloat-gen /path/to/testfloat_gen --cache-root /path/to/misteross-toolchains
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / 'build/sim/fes-i386-x87-testfloat'
OPERATIONS = ('add', 'sub', 'mul', 'eq', 'le', 'lt', 'roundToInt')
PRECISIONS = (32, 64, 80)
ROUNDING = ('near_even', 'min', 'max', 'minMag')


class TestFloatError(RuntimeError):
    pass


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(block)
    return digest.hexdigest()


def configurations(limit: int | None = None) -> list[tuple[str, int, int, str]]:
    configs = [(operation, precision, rc, rounding)
               for precision in PRECISIONS
               for rc, rounding in enumerate(ROUNDING)
               for operation in OPERATIONS]
    if limit is not None:
        if limit < 1:
            raise TestFloatError('--limit-configs must be positive')
        configs = configs[:limit]
    return configs


def generate(testfloat_gen: Path, manifest: Path, limit: int | None = None) -> dict:
    vectors_dir = OUTPUT / 'vectors'
    vectors_dir.mkdir(parents=True, exist_ok=True)
    rc_flags = ('-rnear_even', '-rmin', '-rmax', '-rminMag')
    configs = configurations(limit)

    records = []
    for index, (operation, precision, rc, rounding) in enumerate(configs):
        vector_path = vectors_dir / f'{index:03d}-{operation}-p{precision}-rc{rc}.txt'
        command = [str(testfloat_gen), '-level', '1', '-seed', '1', f'-precision{precision}', rc_flags[rc]]
        if operation == 'roundToInt':
            command.append('-exact')
        command.append(f'extF80_{operation}')
        with vector_path.open('w', encoding='ascii') as output:
            result = subprocess.run(command, stdout=output, stderr=subprocess.PIPE, text=True)
        if result.returncode:
            raise TestFloatError(f"{' '.join(command)} exited {result.returncode}: {result.stderr[-2000:]}")
        with vector_path.open('r', encoding='ascii') as source:
            count = sum(1 for line in source if line.strip())
        if not count:
            raise TestFloatError(f"{' '.join(command)} generated no vectors")
        records.append({'operation': operation, 'precision': precision, 'rc': rc, 'rounding': rounding,
                        'vectors': count, 'path': vector_path.resolve().as_posix(), 'command': command})

    with manifest.open('w', encoding='utf-8') as output:
        for record in records:
            output.write(f"{record['operation']} {record['precision']} {record['rc']} {record['vectors']} "
                         f"{json.dumps(record['path'])}\n")
    return {'configs': records, 'vectors': sum(item['vectors'] for item in records)}


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--testfloat-gen', type=Path, required=True,
                        help='TestFloat testfloat_gen built from the pinned TestFloat/SoftFloat commits')
    parser.add_argument('--cache-root', type=Path, default=os.environ.get('FES_TOOLCHAIN_CACHE_ROOT'),
                        help='absolute toolchain cache root containing the locked Verilator')
    parser.add_argument('--jobs', type=int, default=8)
    parser.add_argument('--netlist', action='store_true', help="run against the seal's Yosys netlist")
    parser.add_argument('--limit-configs', type=int,
                        help='development smoke limit; omit to run all 84 precision/rounding configurations')
    args = parser.parse_args(argv)
    testfloat_gen = args.testfloat_gen.resolve()
    if not testfloat_gen.is_file() or not os.access(testfloat_gen, os.X_OK):
        raise TestFloatError(f'TestFloat generator is not executable: {testfloat_gen}')
    if args.cache_root is None:
        raise TestFloatError('pass --cache-root or set FES_TOOLCHAIN_CACHE_ROOT for the i386 lock Verilator')
    cache_root = Path(args.cache_root).resolve()
    OUTPUT.mkdir(parents=True, exist_ok=True)
    manifest = OUTPUT / 'manifest.txt'
    generated = generate(testfloat_gen, manifest, args.limit_configs)
    report = {'testfloat_gen': testfloat_gen.as_posix(), 'testfloat_gen_sha256': sha256(testfloat_gen),
              'testfloat_level': 1, 'generator_seed': 1, 'vectors': generated['vectors'],
              'configs': generated['configs'], 'netlist': args.netlist}
    command = [sys.executable, str(ROOT / 'scripts/test_fes_i386_x87_unit.py'), '--cache-root', str(cache_root),
               '--jobs', str(args.jobs), '--testfloat-manifest', str(manifest)]
    if args.netlist:
        command.append('--netlist')
    result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True)
    print(result.stdout, end='')
    if result.stderr:
        print(result.stderr, file=sys.stderr, end='')
    report['unit_returncode'] = result.returncode
    unit_report = ROOT / 'build/sim/fes-i386-x87-unit/report.json'
    report['unit_report'] = json.loads(unit_report.read_text()) if unit_report.is_file() else None
    if result.returncode == 0 and (report['unit_report'] is None or
                                   report['unit_report'].get('testfloat') !=
                                   {'vectors': generated['vectors'], 'configs': len(generated['configs'])}):
        raise TestFloatError('the unit report does not match the generated TestFloat manifest')
    report_path = OUTPUT / 'report.json'
    report_path.write_text(json.dumps(report, indent=2) + '\n')
    print(f"TestFloat extF80 level 1: {generated['vectors']} vectors in {len(generated['configs'])} configurations; "
          f"report {report_path}")
    return result.returncode


if __name__ == '__main__':
    try:
        sys.exit(main())
    except TestFloatError as error:
        print(f'i386 x87 TestFloat error: {error}', file=sys.stderr)
        sys.exit(2)
