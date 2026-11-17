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
  decimal  FBLD and FBSTP (#1958), which only the DE25 backend builds: with
           --backend de25 exactly against the host, in every rounding mode,
           masked and unmasked, alone and in flight between other forms;
           with --backend de10 that the unit keeps both forms out

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

Writes build/sim/fes-i386-x87-unit/report.json, which names the revision and
the sources it tested and gives the packed decimal forms' counts. A --netlist
run also checks, by name, that the netlist holds none of the registers only
the other backend builds.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import i386_verilator
from scripts import x87_envelope_fit
from scripts import x87_trans_ucode
from scripts import probe_fes_i386_x87_trans as trans_probe
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
DE25_SOURCES = ('fes_x87_fast.sv', 'fes_x87_slow.sv', 'fes_x87_core.sv',
                'fes_x87_trans_pkg.sv', 'fes_x87_trans_rom.sv', 'fes_x87_trans.sv')
BACKENDS = {'de10': 0, 'de25': 1}
# Verilator on a Yosys netlist: the lint warnings its flat output raises by
# construction, as the other i386 benches pass them
# (scripts/test_fes_i386_frontend.py).
NETLIST_WARNINGS = ['-Wno-CASEOVERLAP', '-Wno-UNDRIVEN', '-Wno-UNOPTFLAT', '-Wno-WIDTHEXPAND', '-Wno-WIDTHTRUNC',
                    '--no-assert-case']


class X87UnitError(RuntimeError):
    pass


def tested_sources(backend: str) -> list[Path]:
    """What the bench is built from: the unit's RTL for this backend, the C++
    bench, this runner, the netlist recipe and the audit a netlist run reads
    its register lists from."""
    rtl = RTL_SOURCES if backend == 'de10' else (RTL_SOURCES[:-1] + DE25_SOURCES + RTL_SOURCES[-1:])
    return sorted({*(RTL / path for path in rtl), SIM / 'x87_unit_tb.cpp', Path(__file__).resolve(),
                   Path(yosys_netlist.__file__).resolve(), Path(x87_envelope_fit.__file__).resolve(),
                   Path(x87_trans_ucode.__file__).resolve()})


def source_record(backend: str) -> dict:
    """The revision and sources a run tested, so a report names what it is a
    report of, as the lockstep runner's source_record does: `tracked_changes`
    lists tracked files that differ from the revision, and a report with any
    is not a report of that revision alone."""
    repository = ROOT.parents[1]

    def git(*arguments: str) -> str:
        result = subprocess.run(['git', '-C', str(repository), *arguments], capture_output=True, text=True)
        if result.returncode:
            raise X87UnitError('cannot read the tested revision: ' + result.stderr.strip())
        return result.stdout

    files = {str(path.relative_to(repository)): hashlib.sha256(path.read_bytes()).hexdigest()
             for path in tested_sources(backend)}
    return {'fes_revision': git('rev-parse', 'HEAD').strip(),
            'tracked_changes': sorted(line[3:] for line in git('status', '--porcelain', '--untracked-files=no').splitlines()),
            'files': files,
            'files_sha256': hashlib.sha256(json.dumps(files, sort_keys=True).encode()).hexdigest()}


def netlist_exclusion(netlist: Path, backend: str) -> dict:
    """Which backend-only registers the Yosys netlist holds, by name: those
    written only inside arms guarded for this backend (the control: if none
    of them can be found, an absence below means nothing) and those written
    only inside arms guarded for the other one, which must all be gone.
    The lists are x87_envelope_fit.guarded_only_registers', derived from the
    RTL. A DE10 netlist must not hold the packed decimal registers (#1958)
    and a DE25 netlist must not hold the sequenced arithmetic's."""
    text = netlist.read_text()
    rtl = (RTL / 'fes_i386_x87.sv').read_text()

    def held(name: str) -> bool:
        return re.search(rf'(?<![A-Za-z0-9_$]){re.escape(name)}(?![A-Za-z0-9_$])', text) is not None

    own = {name: held(name) for name in x87_envelope_fit.guarded_only_registers(rtl, BACKENDS[backend])}
    other = {name: held(name) for name in x87_envelope_fit.guarded_only_registers(rtl, 1 - BACKENDS[backend])}
    if not own or not other:
        raise X87UnitError('the guarded-register audit derived an empty list, so the exclusion check checks nothing')
    if not any(own.values()):
        raise X87UnitError(f'the netlist holds none of the {backend} backend\'s own registers by name, '
                           'so an absence means nothing')
    return {'own_backend_only': own, 'other_backend_only': other,
            'leaked': sorted(name for name, present in other.items() if present)}


def bcd_forms(text: str) -> dict | None:
    """The bench's count of packed decimal instructions (#1958): how many FBLD
    and FBSTP the unit was given and how many conversions its sequencer was
    seen to enter. On the DE10 backend, which does not build them, the issued
    count is of the not-built checks and the converted count must be zero."""
    for line in text.splitlines():
        if line.startswith('BCDFORMS '):
            fields = line.split()
            return {'fbld': {'issued': int(fields[3]), 'converted': int(fields[5])},
                    'fbstp': {'issued': int(fields[8]), 'converted': int(fields[10])}}
    return None


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
               # -D, not +define+: the bench is C++, and it has to know the backend
               # on a netlist build too, where no -G reaches the design.
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM} -DFES_X87_BACKEND={BACKENDS[backend]}',
               *map(str, design), str(SIM / 'x87_unit_tb.cpp')]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, env=verilator.environment(), stdout=stream,
                                stderr=subprocess.STDOUT)
    if result.returncode:
        raise X87UnitError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + TOP)


def transcendentals(text: str, args) -> dict | None:
    """What the bench says of the transcendentals: per instruction, the cases,
    how many entered the engine, and the whole histogram of the distance
    between the unit's results and the host's."""
    rows = {}
    for line in text.splitlines():
        if line.startswith('TRANS '):
            fields = line.split()
            index = fields.index('distance')
            row = {fields[n]: int(fields[n + 1]) for n in range(2, index, 2)}
            row['distance'] = {key: int(count) for key, count in (item.split(':') for item in fields[index + 1:])}
            rows[fields[1]] = row
    if not rows:
        return None
    single = {line.split()[0]: line.split()[1:] for line in text.splitlines()
              if line.startswith(('TRANS_VECTORS ', 'TRANS_BOUND ', 'TRANS_SWEEP '))}
    qemu_rows = {}
    for line in text.splitlines():
        if line.startswith('TRANS_QEMU '):
            fields = line.split()
            index = fields.index('outside')
            qemu_rows[fields[1]] = {'rows': int(fields[3]), 'inside': int(fields[5]), 'worst_outside': int(fields[7]),
                                    'outside': {k: int(v) for k, v in (item.split(':') for item in fields[index + 1:])}}
    if args.qemu_brackets and not qemu_rows:
        raise X87UnitError('--qemu-brackets was given and the bench reported no bracket rows')
    return {'bound_host': args.transcendental_ulp_host,
            'qemu_brackets': ({'revision': trans_probe.QEMU_REVISION, 'license': trans_probe.QEMU_LICENSE,
                               'files_sha256': {name: digest for name, digest, _, _ in trans_probe.QEMU_TABLES},
                               'instructions': qemu_rows} if qemu_rows else None),
            'bound_is': 'proposed, not agreed: see the record\'s bound.md',
            'model_vectors_compared_exactly': int(single['TRANS_VECTORS'][0]),
            'reset_and_squash_offsets_inside_engine': int(single['TRANS_SWEEP'][1]),
            'cases': sum(line.startswith(('PASS trans-', 'FAIL trans-')) for line in text.splitlines()),
            'instructions': rows}


def latencies(text: str) -> dict[str, int]:
    """The bench's per-form latency lines, so the record's table is generated
    from the run rather than typed out of a terminal."""
    out = {}
    for line in text.splitlines():
        if line.startswith('LATENCY '):
            _, form, pc, clocks = line.split()
            out[f'{form}.{pc}'] = int(clocks)
    return out


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
    parser.add_argument('--transcendental-ulp-host', type=int,
                        help="the bound, in representable values, on a transcendental result's distance from "
                             "the host's. No default: unset, every such compare fails")
    parser.add_argument('--qemu-brackets', action='store_true',
                        help="also run QEMU's pinned bracket tables for F2XM1, FYL2X, FYL2XP1 and FPATAN on the unit")
    parser.add_argument('--download', action='store_true',
                        help='with --qemu-brackets: explicitly fetch the pinned tables into ignored build/')
    parser.add_argument('--transcendental-vectors', type=int, default=250,
                        help="the model's vectors a class and rounding mode, compared exactly (default 250)")
    parser.add_argument('--testfloat-manifest', type=Path,
                        help='also run extF80 TestFloat vectors listed in this generated manifest')
    parser.add_argument('--jobs', type=int, default=8)
    args = parser.parse_args(argv)

    OUTPUT.mkdir(parents=True, exist_ok=True)
    verilator = i386_verilator.locate(args.cache_root)
    source = source_record(args.backend)
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
    command = [str(bench)]
    # The transcendentals (#1958), DE25 only. The bound against the host has
    # no default: unset, every transcendental result compare fails. The
    # model's instruction-level vectors are generated here, so the exact
    # compare is against the model this revision holds.
    environment = {name: value for name, value in os.environ.items() if not name.startswith('FES_X87_TRANS_')}
    if args.transcendental_ulp_host is not None:
        environment['FES_X87_TRANS_ULP_HOST'] = str(args.transcendental_ulp_host)
    if args.backend == 'de25':
        program = x87_trans_ucode.build_program(x87_trans_ucode.build_rom())
        vectors = bench.parent / 'trans-vectors.txt'
        vectors.write_text('\n'.join(x87_trans_ucode.unit_vectors(program, args.transcendental_vectors, 1958)) + '\n')
        environment['FES_X87_TRANS_VECTORS'] = str(vectors)
        if args.qemu_brackets:
            # QEMU's tests/tcg/i386 tables for F2XM1, FYL2X, FYL2XP1 and FPATAN:
            # GPL-2.0-or-later test inputs, pinned by commit and SHA-256 in
            # probe_fes_i386_x87_trans.py, fetched only with --download, parsed
            # here and never committed.
            try:
                brackets, qemu = trans_probe.qemu_vectors(args.download)
            except trans_probe.oracle.OracleError as error:
                raise X87UnitError(f'--qemu-brackets: {error}') from error
            environment['FES_X87_TRANS_BRACKETS'] = str(brackets)
    if args.testfloat_manifest:
        command.extend(['--testfloat-manifest', str(args.testfloat_manifest.resolve())])
    result = subprocess.run(command, capture_output=True, text=True, timeout=3600, env=environment)
    passed, failed, failures = parse(result.stdout)
    testfloat = next((line.split() for line in result.stdout.splitlines() if line.startswith('TESTFLOAT ')), None)
    if args.testfloat_manifest and testfloat is None:
        raise X87UnitError('the bench did not report its TestFloat vector/configuration counts')
    exclusion = netlist_exclusion(bench.parent / f'{TOP}.netlist.v', args.backend) if args.netlist else None
    forms = bcd_forms(result.stdout)
    if forms is None:
        raise X87UnitError('the bench did not report its packed decimal form counts')
    # A run during which a tested source changed is a report of neither.
    if source_record(args.backend) != source:
        raise X87UnitError('a tested source or the revision changed during the run')
    report = {
        'top': TOP,
        'netlist': bool(args.netlist),
        'backend': args.backend,
        'source': source,
        'verilator': verilator.record(),
        'passed': passed,
        'failed': failed,
        'failures': failures,
        'cases': sum(line.startswith(('PASS ', 'FAIL ')) for line in result.stdout.splitlines()),
        'bcd_cases': sum(line.startswith(('PASS bcd-', 'FAIL bcd-')) for line in result.stdout.splitlines()),
        'bcd_forms': forms,
        'transcendentals': transcendentals(result.stdout, args),
        'netlist_exclusion': exclusion,
        'latency_clocks': latencies(result.stdout),
        'testfloat': ({'vectors': int(testfloat[1]), 'configs': int(testfloat[2])} if testfloat else None),
    }
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(f'fes_i386_x87 against the host x87: {passed} passed, {failed} failed'
          + (' (netlist)' if args.netlist else '')
          + f" [{args.backend}; packed decimal: {report['bcd_cases']} cases, FBLD {forms['fbld']['issued']} issued "
            f"{forms['fbld']['converted']} converted, FBSTP {forms['fbstp']['issued']} issued "
            f"{forms['fbstp']['converted']} converted]"
          + (f"; TestFloat extF80: {testfloat[1]} vectors across {testfloat[2]} configurations"
             if testfloat else ''))
    for line in failures[:10]:
        print('  ' + line)
    if exclusion is not None:
        print(f"  netlist holds {sum(exclusion['own_backend_only'].values())} of "
              f"{len(exclusion['own_backend_only'])} {args.backend}-only registers by name and "
              f"{len(exclusion['leaked'])} of {len(exclusion['other_backend_only'])} of the other backend's"
              + (f": {exclusion['leaked']}" if exclusion['leaked'] else ''))
    return 1 if failed or result.returncode or (exclusion and exclusion['leaked']) else 0


if __name__ == '__main__':
    sys.exit(main())
