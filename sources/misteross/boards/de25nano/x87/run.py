#!/usr/bin/env python3
"""Simulate the standalone DE25 x87 arithmetic pipeline against host x87.

Uses the explicit --verilator, VERILATOR environment setting, or PATH. This
is host-only arithmetic/protocol evidence: it does not seal or deploy a core.
The independent instruction reference requires an x86 host with x87.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT))
import build as backend_build  # noqa: E402  (needs ROOT on the path)

OUTPUT = ROOT.parents[2] / 'build/sim/de25-x87-pipeline'


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=min(8, os.cpu_count() or 1))
    parser.add_argument('--samples', type=int, default=256,
                        help='random cases per operation, precision and rounding mode (default: 256)')
    args = parser.parse_args()
    if args.jobs < 1 or args.samples < 0:
        parser.error('--jobs must be positive and --samples nonnegative')
    if platform.machine().lower() not in ('x86_64', 'amd64', 'i386', 'i686'):
        parser.error('the independent host-x87 reference needs an x86 host')
    verilator = shutil.which(args.verilator)
    if not verilator:
        parser.error(f'Verilator executable not found: {args.verilator}')
    OUTPUT.mkdir(parents=True, exist_ok=True)
    # The backend's RTL is shared i386 RTL (backend_build.RTL names it in dependency
    # order, package first), because fes_i386_x87 instantiates fes_x87_core.
    sources = [backend_build.SHARED_RTL / name for name in backend_build.RTL]
    inputs = [*sources, ROOT / 'sim/x87_pipe_tb.cpp', Path(__file__).resolve()]
    bindings = {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in inputs}

    def sources_unchanged() -> bool:
        for path, digest in bindings.items():
            if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                print(f'Simulation input changed during the run: {path}', file=sys.stderr)
                return False
        return True

    # Verilation stays single-threaded (#1925). The pinned Verilator 5.032 runs
    # V3FuncOpt on worker threads, and its concat balancing races on the shared
    # dtype table, corrupting the heap on about 4% of runs at 4 threads.
    # Upstream made that pass serial in verilator/verilator#6440 (first in
    # 5.042). Verilation takes 0.05 s; only the C++ build uses --jobs.
    command = [verilator, '--cc', '--exe', '--build', '--build-jobs', str(args.jobs),
               '--verilate-jobs', '1', '-O3',
               '--top-module', 'fes_x87_pipe', '-Wall', '-Wno-UNUSEDSIGNAL',
               '--Mdir', str(OUTPUT), '-CFLAGS', '-std=c++17 -O2',
               *map(str, sources), str(ROOT / 'sim/x87_pipe_tb.cpp')]
    with (OUTPUT / 'build.log').open('w') as log:
        build = subprocess.run(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT)
    if build.returncode:
        print('\n'.join((OUTPUT / 'build.log').read_text().splitlines()[-40:]), file=sys.stderr)
        print(f'Build failed; full log: {OUTPUT / "build.log"}', file=sys.stderr)
        return 1
    if not sources_unchanged():
        return 1
    try:
        result = subprocess.run([str(OUTPUT / 'Vfes_x87_pipe'), '--samples', str(args.samples)],
                                cwd=ROOT, capture_output=True, text=True, timeout=1200)
    except subprocess.TimeoutExpired:
        print('Simulation exceeded its 20-minute cap', file=sys.stderr)
        return 1
    if not sources_unchanged():
        return 1
    (OUTPUT / 'simulation.log').write_text(result.stdout + result.stderr)
    metrics = {}
    passed = failed = None
    failures = []
    for line in result.stdout.splitlines():
        if line.startswith('METRIC '):
            _, key, raw = line.split()
            metrics[key] = int(raw)
        elif line.startswith('SUMMARY '):
            _, good, bad = line.split()
            passed, failed = int(good), int(bad)
        elif line.startswith('FAIL '):
            failures.append(line[5:])
    report = {
        'top': 'fes_x87_pipe', 'evidence': 'host-only arithmetic and protocol simulation',
        'reference': 'host x87 instructions with FNSAVE/FRSTOR preservation',
        'verilator': subprocess.check_output([verilator, '--version'], text=True).strip(),
        'samples_per_operation_mode': args.samples,
        'source_sha256': {str(path.relative_to(ROOT.parents[2])): digest for path, digest in bindings.items()},
        'passed': passed, 'failed': failed, 'metrics': metrics, 'failures': failures,
    }
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(result.stdout, end='')
    if result.stderr:
        print(result.stderr, file=sys.stderr, end='')
    print(f'Report: {OUTPUT / "report.json"}')
    return 1 if result.returncode or failed or passed is None else 0


if __name__ == '__main__':
    sys.exit(main())
