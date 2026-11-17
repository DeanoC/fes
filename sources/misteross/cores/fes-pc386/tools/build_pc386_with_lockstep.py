#!/usr/bin/env python3
"""Run the full i386 netlist suite, then seal pc386 with its report.

Use a task-private checkout with an unused build/sim/fes-i386-lockstep path.
The stock runner and sealer are unchanged; their normal validation remains
responsible for accepting or rejecting the report and output.
"""
from __future__ import annotations

import argparse
from pathlib import Path
import subprocess
import sys


def build(root: Path, *, cache_root: Path | None, gpu_router: str | None,
          seed: int | None, package_output: Path | None) -> int:
    root = root.resolve()
    if seed is not None and (type(seed) is not int or not 1 <= seed <= 0x7fffffff):
        raise ValueError('Quartus seed must be an integer from 1 to 2147483647')
    runner = root / 'scripts/test_fes_i386_lockstep.py'
    sealer = root / 'scripts/build_fes_pc386_quartus.py'
    if not runner.is_file() or not sealer.is_file():
        raise ValueError('root must contain the standard i386 runner and pc386 sealer')
    report_dir = root / 'build/sim/fes-i386-lockstep'
    report_dir.parent.mkdir(parents=True, exist_ok=True)
    try:
        # The stock runner uses this fixed path. Reserve it atomically before
        # launching, so an old report is never consumed as this run's output.
        report_dir.mkdir()
    except FileExistsError as exc:
        raise ValueError(f'lockstep output already exists; preserve or move it before running: {report_dir}') from exc
    common = []
    if cache_root is not None:
        common.extend(('--cache-root', str(cache_root)))
    if gpu_router is not None:
        common.extend(('--gpu-router', gpu_router))
    suite = [sys.executable, str(runner), '--netlist', *common]
    status = subprocess.run(suite, cwd=root, check=False).returncode
    if status:
        print(f'full netlist lockstep failed (exit {status}); output retained at {report_dir}', file=sys.stderr)
        return status
    report = report_dir / 'report.json'
    if report.is_symlink() or not report.is_file():
        raise ValueError(f'lockstep did not write a regular report: {report}')
    command = [sys.executable, str(sealer), '--root', str(root), '--lockstep-report', str(report), *common]
    if seed is not None:
        command.extend(('--seed', str(seed)))
    if package_output is not None:
        command.extend(('--package-output', str(package_output)))
    return subprocess.run(command, cwd=root, check=False).returncode


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[3])
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'))
    parser.add_argument('--seed', type=int)
    parser.add_argument('--package-output', type=Path)
    args = parser.parse_args(argv)
    try:
        return build(args.root, cache_root=args.cache_root, gpu_router=args.gpu_router,
                     seed=args.seed, package_output=args.package_output)
    except (OSError, ValueError) as exc:
        print(f'pc386 lockstep and seal: {exc}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
