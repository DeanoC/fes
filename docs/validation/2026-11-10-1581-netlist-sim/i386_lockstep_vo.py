#!/usr/bin/env python3
"""S1c: run scripts/test_fes_i386_lockstep.py unchanged, on the RTL or on a Quartus .vo.

usage: i386_lockstep_vo.py REPO OUTPUT [--vo FILE --sim-lib DIR [--extra FILE ...] [--vflag FLAG ...]]
       -- <harness args>

REPO is sources/misteross. OUTPUT replaces the harness's build/sim/fes-i386-lockstep, so
the RTL and netlist runs keep separate reports. With --vo, the harness's verilate() is
replaced by one that compiles the .vo with the Intel models named in MODELS from
--sim-lib (quartus/eda/sim_lib of the locked install) as -v libraries; --extra files are
compiled as ordinary sources ahead of them (a trial cell model); everything else (lockstep_tb.cpp,
the oracle, the corpus, the report) is the harness's own.
"""
import subprocess
import sys
from pathlib import Path

argv = sys.argv[1:]
split = argv.index('--')
mine, harness = argv[:split], argv[split + 1:]
repo, output = Path(mine[0]).resolve(), Path(mine[1]).resolve()
vo = sim_lib = None
vflags = []
extra = []
rest = mine[2:]
while rest:
    flag, value = rest[0], rest[1]
    rest = rest[2:]
    if flag == '--vo':
        vo = Path(value).resolve()
    elif flag == '--sim-lib':
        sim_lib = Path(value).resolve()
    elif flag == '--extra':
        extra.append(str(Path(value).resolve()))
    elif flag == '--vflag':
        vflags.append(value)
    else:
        raise SystemExit(f'unknown option {flag}')

sys.path.insert(0, str(repo))
from scripts import test_fes_i386_lockstep as lockstep  # noqa: E402

MODELS = ('cyclonev_atoms.v', 'altera_lnsim.sv', 'altera_primitives.v')
lockstep.OUTPUT = output
rtl_verilate = lockstep.verilate


def verilate_vo(verilator, linked, jobs, yosys, parameters=None):
    assert yosys is None and not parameters
    out = output / 'quartus-vo'
    out.mkdir(parents=True, exist_ok=True)
    command = [verilator, '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', lockstep.TOP,
               '-Wno-fatal', *vflags, '--Mdir', str(out),
               '-CFLAGS', f'-std=c++17 -O2 -I{lockstep.SIM} -I{lockstep.DDR_MODEL}', '-LDFLAGS', '-pthread',
               str(vo), *extra, *(arg for name in MODELS for arg in ('-v', str(sim_lib / name))),
               str(lockstep.SIM / 'lockstep_tb.cpp'), *map(str, linked)]
    (out / 'command.txt').write_text(' '.join(command) + '\n')
    with (out / 'build.log').open('w') as stream:
        result = subprocess.run(command, cwd=repo, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise lockstep.LockstepError(f'verilator failed on the .vo; see {out / "build.log"}')
    return out / ('V' + lockstep.TOP)


if vo is not None:
    lockstep.verilate = verilate_vo
sys.argv = ['test_fes_i386_lockstep.py', *harness]
raise SystemExit(lockstep.main())
