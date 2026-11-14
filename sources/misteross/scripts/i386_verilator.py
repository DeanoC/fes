#!/usr/bin/env python3
"""The i386 lock's Verilator, authenticated from its shared toolchain install.

The i386 CPU simulations (test_fes_i386_lockstep.py, test_fes_i386_frontend.py,
test_fes_i386_latency.py, test_fes_i386_governor.py, test_fes_i386_test386.py)
build their testbenches with the Verilator that
cores/fes-common/rtl/i386/toolchain.lock pins, never one found on PATH.

It comes from the Verilator install in the shared toolchain cache that the
lock's pin selects. Verilator takes no GPU lane, so every lock pinning the
same Verilator commit shares one install. bootstrap.sh builds it and
toolchain_cache.py authenticates it, as it does the Yosys and nextpnr the
producers take from their installs. The install's ready.json binds the pin,
the recipe digest, the host and compiler identity and every file under
install/, so verilator_bin and the runtime headers and sources each testbench
compiles against are checked, not only the wrapper script. The wrapper's
--version must match the identity the install recorded when it was built.

Provision it once per host:
  make toolchain-fes-i386 CACHE_ROOT=/abs/out/cache/misteross-toolchains
"""
from __future__ import annotations

import argparse
import dataclasses
import os
from pathlib import Path
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import lockfile
from scripts import toolchain_cache

ROOT = Path(__file__).resolve().parents[1]
LOCK = Path('cores/fes-common/rtl/i386/toolchain.lock')
GPU_ROUTER = 'OFF'
CACHE_ENVIRONMENT = 'FES_TOOLCHAIN_CACHE_ROOT'


class VerilatorError(RuntimeError):
    pass


@dataclasses.dataclass(frozen=True)
class LockedVerilator:
    path: Path
    commit: str
    sha256: str
    version: str
    install: str

    def environment(self, base: dict[str, str] | None = None) -> dict[str, str]:
        """base (default os.environ) without VERILATOR_ROOT, so the wrapper
        uses the install's own runtime rather than one the caller names."""
        environment = dict(os.environ if base is None else base)
        environment.pop('VERILATOR_ROOT', None)
        return environment

    def record(self) -> dict[str, str]:
        """The identity a report records."""
        return {'lock': LOCK.as_posix(), 'commit': self.commit, 'sha256': self.sha256,
                'version': self.version, 'install': self.install}


def add_argument(parser: argparse.ArgumentParser) -> None:
    parser.add_argument('--cache-root', type=Path, default=os.environ.get(CACHE_ENVIRONMENT) or None,
                        help=f'the shared misteross-toolchains cache root (absolute; default ${CACHE_ENVIRONMENT}) '
                             f'holding the i386 lock\'s Verilator install')


def locate(cache_root: Path | None) -> LockedVerilator:
    """The i386 lock's Verilator from its authenticated install under cache_root."""
    if cache_root is None:
        raise VerilatorError(f'no toolchain cache: pass --cache-root or set {CACHE_ENVIRONMENT}; provision the '
                             'install with `make toolchain-fes-i386 CACHE_ROOT=...`')
    if not cache_root.is_absolute():
        raise VerilatorError('--cache-root must be absolute')
    lock_path = ROOT / LOCK
    try:
        commit = lockfile.load_lock(lock_path)['verilator'].commit
        request = toolchain_cache.request_from_environment(ROOT, lock_path, cache_root=cache_root,
                                                           gpu_router=GPU_ROUTER)
        install = toolchain_cache.resolve_install(request, 'verilator')
    except (OSError, lockfile.LockfileError, toolchain_cache.CacheError) as error:
        raise VerilatorError(f'the i386 lock\'s Verilator install is not ready: {error}; provision it with '
                             '`make toolchain-fes-i386 CACHE_ROOT=...`') from error
    record = install.record
    if record['commit'] != commit:
        raise VerilatorError(f'install Verilator commit {record["commit"]}, lock {commit}')
    path = install.binary
    if path.is_symlink() or not path.is_file() or not os.access(path, os.X_OK):
        raise VerilatorError(f'install Verilator is not a regular executable: {path}')
    environment = {name: value for name, value in os.environ.items() if name != 'VERILATOR_ROOT'}
    try:
        result = subprocess.run([str(path), '--version'], cwd=ROOT, env=environment, capture_output=True,
                                text=True, timeout=30, check=False)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise VerilatorError(f'cannot run the install Verilator: {error}') from error
    version = result.stdout.strip()
    if result.returncode or version != record['identity'].strip():
        raise VerilatorError(f'install Verilator reports {version!r}, its install recorded {record["identity"]!r}')
    try:
        # Again after the probe, so nothing in the install changed under it.
        toolchain_cache.verify_install(request, 'verilator')
    except toolchain_cache.CacheError as error:
        raise VerilatorError(f'the i386 lock\'s Verilator install changed: {error}') from error
    return LockedVerilator(path=path, commit=commit, sha256=record['sha256'], version=version, install=install.key)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    add_argument(parser)
    args = parser.parse_args()
    try:
        verilator = locate(args.cache_root)
    except VerilatorError as error:
        print('i386 Verilator:', error, file=sys.stderr)
        return 2
    print(f'{verilator.path}\n{verilator.version}')
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
