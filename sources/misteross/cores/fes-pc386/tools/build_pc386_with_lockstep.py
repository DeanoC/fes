#!/usr/bin/env python3
"""Build the pc386 firmware, run the full i386 netlist suite, then seal pc386 with its report.

Use a task-private checkout with an unused build/sim/fes-i386-lockstep path.
The stock runner and sealer are unchanged; their normal validation remains
responsible for accepting or rejecting the report and output.
"""
from __future__ import annotations

import argparse
import fcntl
import os
import stat
import tempfile
from pathlib import Path
import subprocess
import sys


def _build(root: Path, *, cache_root: Path | None, gpu_router: str | None,
           seed: int | None, package_output: Path | None, download: bool = False,
           product: bool = False) -> int:
    root = Path(root).absolute()
    if root.resolve() != root:
        raise ValueError('root must not traverse a symlink')
    if seed is not None and (type(seed) is not int or not 1 <= seed <= 0x7fffffff):
        raise ValueError('Quartus seed must be an integer from 1 to 2147483647')
    runner = root / 'scripts/test_fes_i386_lockstep.py'
    sealer = root / ('cores/fes-pc386/tools/build_pc386_product.py' if product else 'scripts/build_fes_pc386_quartus.py')
    launcher = root / 'cores/fes-pc386/tools/pc386_run_lockstep.py'
    firmware = root / 'scripts/build_pc386_firmware.py'
    for source in (runner, sealer, firmware, *([launcher] if product else [])):
        current = root
        parts = source.relative_to(root).parts
        for index, part in enumerate(parts):
            current = current / part
            if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
                raise ValueError('runner and sealer must be regular, non-symlinked source files')
    if product:
        runs = root / 'build/sim/fes-i386-lockstep-runs'
        runs.mkdir(exist_ok=True)
        if runs.is_symlink() or not runs.is_dir():
            raise ValueError('pc386 run store must be a real directory')
        report_dir = Path(tempfile.mkdtemp(prefix='run-', dir=runs))
    else:
        report_dir = root / 'build/sim/fes-i386-lockstep'
        report_dir.parent.mkdir(parents=True, exist_ok=True)
        try:
            # The manual lane preserves its original fixed-path reservation.
            report_dir.mkdir()
        except FileExistsError as exc:
            raise ValueError(f'lockstep output already exists; preserve or move it before running: {report_dir}') from exc
    # Each stage owns its process group; a timeout or termination cleans only
    # that group, while the sealer propagates termination to its Quartus group.
    helper = Path(__file__).absolute().parent / 'pc386_owned_job.py'
    if helper.parent.resolve() != helper.parent or helper.is_symlink() or not helper.is_file():
        raise ValueError('pc386 job supervisor must be a regular source file')
    import pc386_owned_job
    if Path(pc386_owned_job.__file__).absolute() != helper:
        raise ValueError('pc386 job supervisor belongs to a different checkout')
    run = pc386_owned_job.run
    common = []
    if cache_root is not None:
        common.extend(('--cache-root', str(cache_root)))
    if gpu_router is not None:
        common.extend(('--gpu-router', gpu_router))
    # A cache miss must work from a clean checkout. The stock firmware builder
    # checks source provenance and the locked image and ROM hashes, and writes
    # the ROM hex the seal checks but never writes itself (#1984 S1). Do this
    # before the expensive lockstep suite and never update the lock.
    command = [sys.executable, str(firmware), '--jobs', '2']
    if download:
        command.append('--download')
    try:
        status = run(command, cwd=root, timeout=15 * 60, label='pc386 firmware')
    except TimeoutError:
        print('pc386 firmware preparation timed out', file=sys.stderr)
        return 124
    if status:
        print(f'pc386 firmware preparation failed (exit {status})', file=sys.stderr)
        return status
    suite = ([sys.executable, str(launcher), '--output', str(report_dir)] if product else [sys.executable, str(runner)])
    suite += ['--netlist', *(['--download'] if download else []), *common]
    try:
        status = run(suite, cwd=root, timeout=6 * 60 * 60, label='pc386 full lockstep')
    except TimeoutError:
        print(f'full netlist lockstep timed out; output retained at {report_dir}', file=sys.stderr)
        return 124
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
    try:
        return run(command, cwd=root, timeout=4 * 60 * 60, label='pc386 product seal', grace=12)
    except TimeoutError:
        print('pc386 product seal timed out; output retained', file=sys.stderr)
        return 124


def build(root: Path, *, cache_root: Path | None, gpu_router: str | None,
          seed: int | None, package_output: Path | None, download: bool = False,
          product: bool = False) -> int:
    if not product:
        return _build(root, cache_root=cache_root, gpu_router=gpu_router, seed=seed,
                      package_output=package_output, download=download, product=False)
    root = Path(root).absolute()
    if root.resolve() != root or not root.is_dir():
        raise ValueError('root must be a real, non-symlinked directory')
    for directory in (root / 'build', root / 'build/sim'):
        directory.mkdir(exist_ok=True)
        if directory.is_symlink() or not directory.is_dir():
            raise ValueError('pc386 build directories must not be symlinks')
    lock_path = root / 'build/sim/.pc386-product.lock'
    fd = os.open(lock_path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK, 0o600)
    with os.fdopen(fd, 'r+b') as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError('pc386 product lock must be a regular file')
        try:
            fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as exc:
            raise ValueError('another pc386 product build owns this checkout') from exc
        return _build(root, cache_root=cache_root, gpu_router=gpu_router, seed=seed,
                      package_output=package_output, download=download, product=True)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parents[3])
    parser.add_argument('--cache-root', type=Path)
    parser.add_argument('--gpu-router', choices=('HIP', 'CUDA'))
    parser.add_argument('--seed', type=int)
    parser.add_argument('--package-output', type=Path)
    parser.add_argument('--download', action='store_true', help='fetch the pinned Bochs source and complete corpus')
    parser.add_argument('--product', action='store_true', help='invoke the source-bound registered product sealer')
    args = parser.parse_args(argv)
    try:
        return build(args.root, cache_root=args.cache_root, gpu_router=args.gpu_router,
                     seed=args.seed, package_output=args.package_output, download=args.download, product=args.product)
    except (OSError, ValueError) as exc:
        print(f'pc386 lockstep and seal: {exc}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
