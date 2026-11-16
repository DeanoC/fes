#!/usr/bin/env python3
"""Check fes_i386_x87 against the host's own x87 (#1768 slice 2).

Host simulation only; no network access and no oracle. An x86-64 host's FPU
is the same 80-bit unit a 387 is, so cores/fes-common/sim/i386/x87_unit_tb.cpp
drives the unit's CPU interface and compares each result and status word with
the host's own instruction:

  loads    FLD m32 and m64, FILD m16, m32 and m64, over the operand classes
           the 387 PRM names and random bits, in all four rounding modes
  stores   FST, FSTP m32, m64 and m80, FIST and FISTP m16, m32 and m64, the
           same way, with the masked overflow, underflow, invalid and
           precision results
  examine  FXAM's classes and FTST's comparison
  state    the constants under each rounding mode, FNINIT, FNCLEX, FLDCW,
           the port F1h reset, FINCSTP, FDECSTP, FCHS, FABS, FXCH, FFREE,
           FLD ST(i), the stack faults and the unmasked error path
  protocol the no-wait forms under the board's BUSY# latch, the latch's edge,
           a squashed instruction and one squashed after it has retired, and
           that a control form leaves the condition codes alone

Where the host and a 387 differ the case follows the 387 Programmer's
Reference Manual, not the host; those are in the unit's header and in
sim/i386/divergences.toml.

With --netlist the unit first runs through the seal's Yosys program
(scripts/yosys_netlist.py) with the pc386 lock's Yosys from its --cache-root
slot, and that netlist is simulated instead, so the bench checks the design
Quartus is given.

Verilator is the i386 lock's, from its authenticated slot in the shared
toolchain cache (scripts/i386_verilator.py): pass --cache-root or set
FES_TOOLCHAIN_CACHE_ROOT.

Example:
  python3 scripts/test_fes_i386_x87_unit.py
  python3 scripts/test_fes_i386_x87_unit.py --netlist

Writes build/sim/fes-i386-x87-unit/report.json.
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import i386_verilator
from scripts import yosys_netlist

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-i386-x87-unit'
TOP = 'fes_i386_x87'
# fes_x87_pkg is in both builds: fes_i386_x87 names its operation encoding
# unconditionally, and a package declares types rather than logic, so the
# default build parses it and instantiates nothing from it.
RTL_SOURCES = ('fes_i386_pkg.sv', 'fes_x87_pkg.sv', 'fes_i386_x87_mul.sv', 'fes_i386_x87.sv')
# --backend de25 adds the DE25 arithmetic core and selects it with
# X87_BACKEND=1. The bench itself is unchanged: the envelope's behaviour is
# what it checks, and that behaviour must not depend on which arithmetic
# answers, so the same cases passing both ways is the pass rule.
DE25_SOURCES = ('fes_x87_fast.sv', 'fes_x87_slow.sv', 'fes_x87_core.sv')
BACKENDS = {'de10': 0, 'de25': 1}
# Verilator on a Yosys netlist: the lint warnings its flat output raises by
# construction, as the other i386 benches pass them
# (scripts/test_fes_i386_frontend.py).
NETLIST_WARNINGS = ['-Wno-CASEOVERLAP', '-Wno-UNDRIVEN', '-Wno-UNOPTFLAT', '-Wno-WIDTHEXPAND', '-Wno-WIDTHTRUNC',
                    '--no-assert-case']


class X87UnitError(RuntimeError):
    pass


def verilate(verilator: i386_verilator.LockedVerilator, jobs: int, yosys: Path | None = None,
             backend: str = 'de10') -> Path:
    """Build the bench, from the RTL or Yosys's netlist of the unit."""
    output = OUTPUT / (('netlist' if yosys else 'rtl') + ('' if backend == 'de10' else f'-{backend}'))
    sources = RTL_SOURCES if backend == 'de10' else (RTL_SOURCES[:-1] + DE25_SOURCES + RTL_SOURCES[-1:])
    select = [f'-GX87_BACKEND={BACKENDS[backend]}'] if not yosys else []
    output.mkdir(parents=True, exist_ok=True)
    if yosys:
        netlist = yosys_netlist.seal_netlist(yosys, output, ROOT,
                                             sources=[RTL / path for path in sources], top=TOP,
                                             parameters={'X87_BACKEND': BACKENDS[backend]})
        design = [netlist, RTL / 'fes_i386_pkg.sv']
        warnings = [*NETLIST_WARNINGS, '+define+FES_I386_NETLIST']
    else:
        design = [RTL / path for path in sources]
        warnings = []
    command = [str(verilator.path), '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', TOP,
               '-Wall', '-Wno-UNUSEDSIGNAL', *warnings, *select, '--Mdir', str(output),
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM}', *map(str, design), str(SIM / 'x87_unit_tb.cpp')]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, env=verilator.environment(), stdout=stream,
                                stderr=subprocess.STDOUT)
    if result.returncode:
        raise X87UnitError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + TOP)


def parse(text: str) -> tuple[int, int, list[str]]:
    """The bench's passed and failed counts, and its failure lines."""
    failures = [line for line in text.splitlines() if line.startswith('FAIL ')]
    for line in text.splitlines():
        if line.startswith('SUMMARY '):
            _, passed, failed = line.split()
            return int(passed), int(failed), failures
    raise X87UnitError('the bench printed no SUMMARY line')


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    i386_verilator.add_argument(parser)
    parser.add_argument('--netlist', action='store_true', help="simulate the seal's Yosys netlist of the unit")
    parser.add_argument('--backend', choices=sorted(BACKENDS), default='de10',
                        help="which arithmetic answers: 'de10' the unit's own sequenced datapath "
                             "(the default and the verified reference), 'de25' the pipelined fes_x87_core")
    parser.add_argument('--jobs', type=int, default=8)
    args = parser.parse_args(argv)

    OUTPUT.mkdir(parents=True, exist_ok=True)
    verilator = i386_verilator.locate(args.cache_root)
    yosys = None
    if args.netlist:
        # Through the cache API, as the Verilator above is: the per-tool
        # layout has no directory a caller can assemble, and a path not held
        # for this process can be pruned mid-run (i386_verilator.locate_yosys).
        try:
            yosys = i386_verilator.locate_yosys(args.cache_root)
        except i386_verilator.VerilatorError as error:
            raise X87UnitError(f"--netlist needs the lock's Yosys: {error}") from error
    bench = verilate(verilator, args.jobs, yosys, args.backend)
    result = subprocess.run([str(bench)], capture_output=True, text=True, timeout=1800)
    passed, failed, failures = parse(result.stdout)
    report = {
        'top': TOP,
        'netlist': bool(args.netlist),
        'backend': args.backend,
        'verilator': verilator.record(),
        'passed': passed,
        'failed': failed,
        'failures': failures,
    }
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(f'fes_i386_x87 against the host x87: {passed} passed, {failed} failed'
          + (' (netlist)' if args.netlist else ''))
    for line in failures[:10]:
        print('  ' + line)
    return 1 if failed or result.returncode else 0


if __name__ == '__main__':
    sys.exit(main())
