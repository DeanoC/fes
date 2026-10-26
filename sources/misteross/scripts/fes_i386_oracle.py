"""Fetch and build the external i386 lockstep oracle (test-only, never packaged).

The oracle is the Bochs CPU (LGPL-2.1-or-later) at the pinned revision below,
configured at CPU level 3 without an FPU. Bochs is fetched only when a caller
passes download=True, into the ignored build/ directory, and its sources never
enter a package or an RTL source list. Only Bochs' CPU, decoder and parameter
tree are compiled; cores/fes-common/sim/i386/bochs_oracle.cpp supplies the
platform objects and links them behind fes_i386::Oracle.
"""
from __future__ import annotations

import hashlib
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
SIM = ROOT / 'cores/fes-common/sim/i386'
BUILD = ROOT / 'build/sim/fes-i386-oracle'

BOCHS_REPOSITORY = 'https://github.com/bochs-emu/Bochs'
BOCHS_REVISION = '30cfd566ecc8deb33e56e801e32b444a7041b6d2'  # REL_3_1_FINAL
BOCHS_LICENSE = 'LGPL-2.1-or-later'
# A 386DX with debug registers. Bochs needs its FPU for WAIT and ESC to take the
# architected EM, MP and TS exceptions (without it WAIT raises #UD and ESC never
# faults); the FES CPU's empty-socket data results are not oracle-checked.
# Everything else is off because only the CPU is compiled.
BOCHS_CONFIGURE = (
    '--enable-cpu-level=3', '--disable-x86-64', '--disable-smp', '--enable-fpu',
    '--enable-x86-debugger', '--enable-instrumentation=instrument/fes', '--with-nogui',
    '--disable-pci', '--disable-cdrom', '--disable-show-ips', '--disable-stats',
    '--disable-docbook', '--disable-xpm', '--disable-large-ramfile',
    '--disable-monitor-mwait', '--disable-gameport', '--disable-busmouse',
    '--disable-usb', '--disable-plugins',
)
ORACLE_SOURCES = ('i386_oracle.h', 'bochs_oracle.cpp', 'bochs/instrument.h')
CXXFLAGS = ('-std=c++17', '-O2', '-Wall')


class OracleError(RuntimeError):
    pass


def _run(command, cwd, log):
    with log.open('a') as destination:
        destination.write('$ ' + ' '.join(map(str, command)) + '\n')
        destination.flush()
        result = subprocess.run(command, cwd=cwd, stdout=destination, stderr=subprocess.STDOUT)
    if result.returncode:
        tail = log.read_text().splitlines()[-40:]
        raise OracleError(f'{command[0]} failed in {cwd}; see {log}\n' + '\n'.join(tail))


def _git(source, *args):
    return subprocess.check_output(['git', '-C', str(source), *args], text=True).strip()


def bochs_source(download: bool) -> Path:
    """Return the pinned Bochs checkout, fetching it only when download is true."""
    source = BUILD / ('bochs-' + BOCHS_REVISION)
    if not (source / '.git').is_dir():
        if not download:
            raise OracleError(f'Bochs {BOCHS_REVISION} is not in {source}; pass --download to fetch it')
        partial = source.with_name(source.name + '.partial')
        shutil.rmtree(partial, ignore_errors=True)
        partial.mkdir(parents=True)
        print(f'Fetching Bochs {BOCHS_REVISION} ({BOCHS_LICENSE})', flush=True)
        for args in (('init', '-q'), ('fetch', '-q', '--depth', '1', BOCHS_REPOSITORY, BOCHS_REVISION),
                     ('checkout', '-q', 'FETCH_HEAD')):
            subprocess.run(['git', '-C', str(partial), *args], check=True)
        partial.rename(source)
    if _git(source, 'rev-parse', 'HEAD') != BOCHS_REVISION:
        raise OracleError(f'{source} is not at the pinned Bochs revision')
    if _git(source, 'status', '--porcelain'):
        raise OracleError(f'{source} has local changes; delete it and fetch again')
    return source


def _compiler():
    return os.environ.get('CXX', 'c++')


def _inputs_key(*parts) -> str:
    digest = hashlib.sha256()
    for part in parts:
        digest.update(part if isinstance(part, bytes) else str(part).encode())
        digest.update(b'\0')
    return digest.hexdigest()[:16]


def build_bochs(source: Path, jobs: int) -> Path:
    """Configure Bochs at CPU level 3 and build its CPU library once per input set."""
    instrument = (SIM / 'bochs/instrument.h').read_bytes()
    key = _inputs_key(BOCHS_REVISION, BOCHS_CONFIGURE, instrument, _compiler())
    tree = BUILD / ('bochs-level3-' + key)
    stamp = tree / 'fes-oracle.stamp'
    if stamp.is_file():
        return tree
    for stale in BUILD.glob('bochs-level3-*'):
        shutil.rmtree(stale)
    shutil.copytree(source / 'bochs', tree)
    (tree / 'instrument/fes').mkdir()
    shutil.copy2(tree / 'instrument/stubs/Makefile.in', tree / 'instrument/fes/Makefile.in')
    (tree / 'instrument/fes/instrument.h').write_bytes(instrument)
    log = tree / 'fes-oracle-build.log'
    print('Configuring Bochs CPU level 3', flush=True)
    _run(['./configure', *BOCHS_CONFIGURE, 'CXX=' + _compiler()], tree, log)
    _run(['make', '-j', str(jobs), 'libcpu.a'], tree / 'cpu', log)
    _run(['make', '-j', str(jobs), 'libfpu.a'], tree / 'cpu/fpu', log)
    _run(['make', '-j', str(jobs), 'libsoftfloat.a'], tree / 'cpu/softfloat3e', log)
    _run([_compiler(), *CXXFLAGS, '-I.', '-Iinstrument/fes', '-c', 'gui/paramtree.cc',
          '-o', 'fes-paramtree.o'], tree, log)
    stamp.write_text(key + '\n')
    return tree


def link_inputs(jobs: int = 4, download: bool = False) -> list[Path]:
    """Build the oracle adapter once per input set; return what a testbench links, in link order.

    A testbench includes i386_oracle.h from SIM and links these files after its
    own objects. Verilator testbenches pass them on the verilator command line.
    """
    bochs = build_bochs(bochs_source(download), jobs)
    inputs = [(SIM / item).read_bytes() for item in ORACLE_SOURCES]
    key = _inputs_key(bochs.name, CXXFLAGS, _compiler(), *inputs)
    output = BUILD / 'bin'
    output.mkdir(parents=True, exist_ok=True)
    oracle_object = output / f'bochs_oracle-{key}.o'
    if not oracle_object.is_file():
        # Per-process names let concurrent runners build the same object.
        log = output / f'bochs_oracle.{os.getpid()}.log'
        log.write_text('')
        temporary = output / f'{oracle_object.name}.{os.getpid()}.tmp'
        _run([_compiler(), *CXXFLAGS, f'-I{bochs}', f'-I{bochs}/instrument/fes', f'-I{SIM}',
              '-c', str(SIM / 'bochs_oracle.cpp'), '-o', str(temporary)], output, log)
        log.unlink()
        temporary.replace(oracle_object)
        for stale in output.glob('bochs_oracle-*.o'):
            if stale != oracle_object:
                stale.unlink(missing_ok=True)
    return [oracle_object, bochs / 'fes-paramtree.o', bochs / 'cpu/libcpu.a',
            bochs / 'cpu/fpu/libfpu.a', bochs / 'cpu/softfloat3e/libsoftfloat.a']


def build_testbench(name: str, source: Path, jobs: int = 4, download: bool = False) -> Path:
    """Build cores/fes-common/sim/i386/<source> against the oracle; return the executable."""
    linked = link_inputs(jobs, download)
    key = _inputs_key(*(path.name for path in linked), CXXFLAGS, _compiler(), (SIM / source.name).read_bytes())
    output = BUILD / 'bin'
    executable = output / f'{name}-{key}'
    if executable.is_file():
        return executable
    log = output / f'{name}.{os.getpid()}.log'
    log.write_text('')
    temporary = output / f'{executable.name}.{os.getpid()}.tmp'
    _run([_compiler(), *CXXFLAGS, f'-I{SIM}', str(source), *map(str, linked), '-o', str(temporary)], output, log)
    log.unlink()
    temporary.replace(executable)
    for stale in output.glob(f'{name}-*'):
        if stale != executable and stale.suffix not in ('.tmp', '.o', '.log'):
            stale.unlink(missing_ok=True)
    return executable
