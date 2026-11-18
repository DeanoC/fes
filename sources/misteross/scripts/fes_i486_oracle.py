"""Build the i386 lockstep oracle at Bochs CPU level 4 (test-only, never packaged).

A second configuration beside scripts/fes_i386_oracle.py, which stays the
level-3 oracle and a sealed input of pc386: this module changes nothing that
one reads or writes. It shares that module's pinned Bochs checkout,
compiler and adapter sources, which compile for either level, and keeps its
own configured tree (bochs-level4-*) and its own output directory
(bin-level4), which the level-3 provisioner's stale file sweeps do not match.

Level 4 differs from level 3 in the build by: the configure flag; Bochs's
CPU-model library (cpu/cpudb/libcpudb.a), which BX_CPU_C::initialize() calls
for its 486 model; and no Bochs patch, because the level-3 stack-limit patch
is written for `BX_CPU_LEVEL == 3` and compiles to nothing here. What the
level-4 CPU is, and where it is not a 486, is in
cores/fes-common/sim/i386/README.md.
"""
from __future__ import annotations

import hashlib
import os
from pathlib import Path
import shutil
import tempfile

from scripts import fes_i386_oracle as level3

LEVEL = 4
ROOT = level3.ROOT
SIM = level3.SIM
BUILD = level3.BUILD
OUTPUT = BUILD / 'bin-level4'
BOCHS_REVISION = level3.BOCHS_REVISION
BOCHS_CONFIGURE = tuple('--enable-cpu-level=4' if flag == '--enable-cpu-level=3' else flag
                        for flag in level3.BOCHS_CONFIGURE)
ORACLE_SOURCES = level3.ORACLE_SOURCES
CXXFLAGS = level3.CXXFLAGS
OracleError = level3.OracleError
bochs_source = level3.bochs_source
_compiler = level3._compiler
PROVISIONER_SHA256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
LINKED_NAMES = ('adapter', 'paramtree', 'cpu', 'fpu', 'softfloat', 'cpudb')
TREE_PREFIX = 'bochs-level4-'

if BOCHS_CONFIGURE == level3.BOCHS_CONFIGURE:
    raise ImportError('the level-3 configure line no longer names its CPU level')


def require_provisioner_unchanged() -> None:
    """Both this module and the level-3 one it builds on are as they were imported."""
    level3.require_provisioner_unchanged()
    if hashlib.sha256(Path(__file__).read_bytes()).hexdigest() != PROVISIONER_SHA256:
        raise OracleError('Bochs level-4 provisioner changed during the run')


def _provisioners() -> str:
    return level3._inputs_key(PROVISIONER_SHA256, level3.PROVISIONER_SHA256)


def build_bochs(source: Path, jobs: int) -> Path:
    """Configure Bochs at CPU level 4 and build its CPU libraries once per input set."""
    require_provisioner_unchanged()
    instrument = (SIM / 'bochs/instrument.h').read_bytes()
    key = level3._inputs_key(BOCHS_REVISION, BOCHS_CONFIGURE, instrument, _compiler(), _provisioners())
    tree = BUILD / (TREE_PREFIX + key)
    stamp = tree / 'fes-oracle.stamp'
    if stamp.is_file():
        try:
            valid = (stamp.read_text().strip() == key
                     and (tree / 'instrument/fes/instrument.h').read_bytes() == instrument)
        except OSError:
            valid = False
        if not valid:
            raise OracleError(f'{tree} has an invalid cached stamp')
        return tree
    for stale in BUILD.glob(TREE_PREFIX + '*'):
        shutil.rmtree(stale)
    shutil.copytree(source / 'bochs', tree)
    (tree / 'instrument/fes').mkdir()
    shutil.copy2(tree / 'instrument/stubs/Makefile.in', tree / 'instrument/fes/Makefile.in')
    (tree / 'instrument/fes/instrument.h').write_bytes(instrument)
    log = tree / 'fes-oracle-build.log'
    print('Configuring Bochs CPU level 4', flush=True)
    level3._run(['./configure', *BOCHS_CONFIGURE, 'CXX=' + _compiler()], tree, log)
    for directory, library in (('cpu', 'libcpu.a'), ('cpu/fpu', 'libfpu.a'), ('cpu/cpudb', 'libcpudb.a'),
                               ('cpu/softfloat3e', 'libsoftfloat.a')):
        level3._run(['make', '-j', str(jobs), library], tree / directory, log)
    level3._run([_compiler(), *CXXFLAGS, '-I.', '-Iinstrument/fes', '-c', 'gui/paramtree.cc',
                 '-o', 'fes-paramtree.o'], tree, log)
    stamp.write_text(key + '\n')
    return tree


def _adapter_key(tree: Path, inputs: list[bytes]) -> str:
    return level3._inputs_key(tree.name, CXXFLAGS, _compiler(), _provisioners(), *inputs)


def link_inputs(jobs: int = 4, download: bool = False) -> list[Path]:
    """Build the adapter against the level-4 tree; return what a testbench links, in link order."""
    bochs = build_bochs(bochs_source(download), jobs)
    inputs = [(SIM / item).read_bytes() for item in ORACLE_SOURCES]
    OUTPUT.mkdir(parents=True, exist_ok=True)
    oracle_object = OUTPUT / f'bochs_oracle-{_adapter_key(bochs, inputs)}.o'
    if not oracle_object.is_file():
        # Per-process names let concurrent runners build the same object.
        log = OUTPUT / f'bochs_oracle.{os.getpid()}.log'
        log.write_text('')
        temporary = OUTPUT / f'{oracle_object.name}.{os.getpid()}.tmp'
        with tempfile.TemporaryDirectory(prefix='bochs-oracle-sources-', dir=OUTPUT) as directory:
            snapshot = Path(directory)
            for name, data in zip(ORACLE_SOURCES, inputs):
                path = snapshot / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
            level3._run([_compiler(), *CXXFLAGS, f'-I{bochs}', f'-I{bochs}/instrument/fes', f'-I{snapshot}',
                         '-c', str(snapshot / 'bochs_oracle.cpp'), '-o', str(temporary)], OUTPUT, log)
        log.unlink()
        temporary.replace(oracle_object)
        for stale in OUTPUT.glob('bochs_oracle-*.o'):
            if stale != oracle_object:
                stale.unlink(missing_ok=True)
    return [oracle_object, bochs / 'fes-paramtree.o', bochs / 'cpu/libcpu.a', bochs / 'cpu/fpu/libfpu.a',
            bochs / 'cpu/softfloat3e/libsoftfloat.a', bochs / 'cpu/cpudb/libcpudb.a']


def build_record(linked: list[Path]) -> dict:
    """Identify the level, source pin, both provisioners, adapter inputs and linked objects."""
    require_provisioner_unchanged()
    tree = linked[1].parent
    inputs = [(SIM / item).read_bytes() for item in ORACLE_SOURCES]
    if (not tree.name.startswith(TREE_PREFIX) or linked[0].name != f'bochs_oracle-{_adapter_key(tree, inputs)}.o'
            or (tree / 'instrument/fes/instrument.h').read_bytes() != inputs[2]):
        raise OracleError('built Bochs level-4 tree or adapter inputs differ from the current source')
    return {'cpu_level': LEVEL, 'revision': BOCHS_REVISION, 'provisioner_sha256': PROVISIONER_SHA256,
            'level3_provisioner_sha256': level3.PROVISIONER_SHA256,
            'adapter_sha256': level3.named_sources_sha256(ORACLE_SOURCES, inputs),
            'linked_sha256': {name: hashlib.sha256(path.read_bytes()).hexdigest()
                              for name, path in zip(LINKED_NAMES, linked, strict=True)}}


def build_testbench(name: str, source: Path, jobs: int = 4, download: bool = False) -> Path:
    """Build cores/fes-common/sim/i386/<source> against the level-4 oracle; return the executable."""
    linked = link_inputs(jobs, download)
    key = level3._inputs_key(*(path.name for path in linked), CXXFLAGS, _compiler(), (SIM / source.name).read_bytes())
    executable = OUTPUT / f'{name}-{key}'
    if executable.is_file():
        return executable
    log = OUTPUT / f'{name}.{os.getpid()}.log'
    log.write_text('')
    temporary = OUTPUT / f'{executable.name}.{os.getpid()}.tmp'
    level3._run([_compiler(), *CXXFLAGS, f'-I{SIM}', str(source), *map(str, linked), '-o', str(temporary)], OUTPUT, log)
    log.unlink()
    temporary.replace(executable)
    for stale in OUTPUT.glob(f'{name}-*'):
        if stale != executable and stale.suffix not in ('.tmp', '.o', '.log'):
            stale.unlink(missing_ok=True)
    return executable
