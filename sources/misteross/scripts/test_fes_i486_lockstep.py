#!/usr/bin/env python3
"""Run the directed 486 cases on the level-4 oracle, and the RTL beside it.

Two checks of scripts/fes_i486_cases.py's programs, both against Bochs at CPU
level 4 (scripts/fes_i486_oracle.py):

  self-check  each program on the oracle alone (i486_tb): its ending and
              registers must be what the case's row says a 486 gives.
  lockstep    each program on fes_i386_cpu in lockstep with the oracle
              (lockstep_tb, FES_I386_MODE=i486), at each CPU identity: the
              386 (I486 = 0), which every package builds, and the 486SX
              (I486 = 1, #1962 slice 2). A case either keeps step to its end
              or stops at its first mismatch, which must not come before the
              case's probe, and each identity's list of stops must equal its
              file in cores/fes-common/sim/i386: i486-expected-stops.txt for
              the 386, i486-expected-stops-486sx.txt for the 486SX. A change
              to what either identity does edits its list in the same commit.

The lockstep runs twice per identity and both runs must give the same list.
With --netlist each identity's CPU is first run through the seal's Yosys
program with its I486 value (scripts/yosys_netlist.py, as
test_fes_i386_lockstep.py --netlist does), and that netlist is simulated.

  python3 scripts/test_fes_i486_lockstep.py --download
  python3 scripts/test_fes_i486_lockstep.py --oracle-only
  python3 scripts/test_fes_i486_lockstep.py --netlist
  python3 scripts/test_fes_i486_lockstep.py --identity 486sx --write-expected

Writes build/sim/fes-i486-lockstep/report.json. Exit status 1 on any failure.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i486_cases
from scripts import fes_i486_oracle as oracle
from scripts import i386_verilator
from scripts import test_fes_i386_lockstep as level3
from scripts import yosys_netlist

ROOT = Path(__file__).resolve().parents[1]
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-i486-lockstep'
# Each identity's I486 parameter and expected-stops list.
IDENTITIES = {'386': (0, SIM / 'i486-expected-stops.txt'), '486sx': (1, SIM / 'i486-expected-stops-486sx.txt')}


class I486Error(RuntimeError):
    pass


def ending(row: dict[str, str]) -> str:
    if row['exception'] != '-':
        return 'exception:' + row['exception']
    if row['int'] != '-':
        return 'int:' + row['int']
    return row['end']


def self_check(testbench: Path, cases: list[fes_i486_cases.Case]) -> dict:
    """Each case's oracle result against its row; the reset state against RESET."""
    result = subprocess.run([str(testbench)], input=''.join(case.line() + '\n' for case in cases),
                            capture_output=True, text=True)
    if result.returncode:
        raise I486Error('i486_tb failed: ' + result.stderr[-2000:])
    rows, reset = {}, None
    for line in result.stdout.splitlines():
        words = line.split()
        if words[0] == 'RESET':
            reset = dict(word.split('=', 1) for word in words[1:])
        elif words[0] == 'CASE':
            rows[words[1]] = dict(word.split('=', 1) for word in words[2:])
        else:
            raise I486Error('unexpected i486_tb output: ' + line)
    if reset is None or set(rows) != {case.label for case in cases} or len(rows) != len(cases):
        raise I486Error('i486_tb did not report the reset state and every case once')
    out = {'reset': [], 'cases': []}
    for name, (want, source) in fes_i486_cases.RESET.items():
        got = int(reset[name], 16)
        out['reset'].append({'field': name, 'expected': f'{want:08x}', 'oracle': f'{got:08x}', 'source': source,
                             'pass': got == want})
    for case in cases:
        row = rows[case.label]
        differences = []
        for name, want in case.expect.items():
            got = ending(row) if name == 'end' else int(row[name], 16)
            if got != want:
                differences.append({'field': name, 'expected': want if name == 'end' else f'{want:x}',
                                    'oracle': got if name == 'end' else f'{got:x}'})
        out['cases'].append({'label': case.label, 'what': case.what, 'source': case.source,
                             'expected': {name: value if name == 'end' else f'{value:x}'
                                          for name, value in case.expect.items()},
                             'divergence': case.divergence, 'i486': case.i486, 'differences': differences,
                             'pass': not differences})
    return out


def verilate(verilator: i386_verilator.LockedVerilator, linked: list[Path], jobs: int, i486: int,
             yosys: Path | None) -> Path:
    """lockstep_tb with the level-4 oracle and the CPU at one identity, the RTL
    (-GI486) or the seal's netlist (hierarchy -chparam), in its own directory."""
    output = OUTPUT / f"{'netlist' if yosys else 'rtl'}-I486{i486}"
    output.mkdir(parents=True, exist_ok=True)
    if yosys:
        design = [yosys_netlist.seal_netlist(yosys, output, ROOT, parameters={'I486': i486})]
        warnings = [*level3.frontend.NETLIST_WARNINGS]
    else:
        design = [level3.RTL / path for path in level3.CPU_RTL]
        warnings = []
    command = [str(verilator.path), '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', level3.TOP,
               '-Wall', '-Wno-UNUSEDSIGNAL', *warnings, '--Mdir', str(output),
               *([] if yosys else [f'-GI486={i486}']),
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM} -I{level3.DDR_MODEL}' + (' -DFES_I386_NETLIST' if yosys else ''),
               '-LDFLAGS', '-pthread', *map(str, design), str(SIM / 'lockstep_tb.cpp'), *map(str, linked)]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, env=verilator.environment(), stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise I486Error(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + level3.TOP)


def stops(testbench: Path, cases: list[fes_i486_cases.Case]) -> dict[str, str]:
    """One lockstep run: each case's 'agree' or 'stop @<eip>.<field> <oracle> <rtl>'."""
    env = dict(os.environ, FES_I386_MODE='i486', FES_I386_LATENCY='1')
    result = subprocess.run([str(testbench)], input=''.join(case.line() + '\n' for case in cases), env=env,
                            capture_output=True, text=True)
    out = {}
    for line in result.stdout.splitlines():
        if line.startswith('I486 '):
            _, label, verdict = line.split(' ', 2)
            if label in out:
                raise I486Error('case reported twice: ' + label)
            out[label] = verdict
        elif not line.startswith('{'):
            raise I486Error('unexpected testbench output: ' + line)
    labels = {case.label for case in cases}
    probes = set(out) - labels
    if (result.returncode or not labels <= set(out) or not probes
            or any(not label.startswith('ud-') or label[3:] in labels for label in probes)):
        raise I486Error(f'lockstep_tb i486 mode failed (status {result.returncode}): ' + result.stderr[-2000:])
    return out


def probe_cases(measured: dict[str, str], cases: list[fes_i486_cases.Case]) -> list[fes_i486_cases.Case]:
    """The #UD probes the testbench ran after the cases (ud_probes16(true)), as cases with no probe:
    a 486's answer is the 386's, #UD, so each must agree."""
    labels = {case.label for case in cases}
    return [fes_i486_cases.Case(label, 'lockstep_tb.cpp ud_probes16(true)', {'end': 'exception:6'}, {}, None,
                                what='the 386 #UD probe ' + label[3:] + ', which a 486 keeps')
            for label in measured if label not in labels]


EXPECTED_HEADERS = {'386': '''\
# What fes_i386_cpu does with each directed 486 case against the level-4
# oracle: "agree", or its first mismatch as lockstep_tb prints it,
# "stop @<eip>.<field> <oracle> <rtl>". scripts/test_fes_i486_lockstep.py
# fails when a run differs from this list; write it with --write-expected.
# The CPU is a 386, so a stop here is a 486 feature not built yet.
''', '486sx': '''\
# What fes_i386_cpu with the 486SX identity (I486 = 1, #1962 slice 2) does
# with each directed 486 case against the level-4 oracle: "agree", or its
# first mismatch as lockstep_tb prints it, "stop @<eip>.<field> <oracle>
# <rtl>". scripts/test_fes_i486_lockstep.py fails when a run differs from
# this list; write it with --identity 486sx --write-expected. Each stop here
# is by design: docs/validation/2026-11-21-i486-s2-isa says why.
'''}


def write_expected(identity: str, cases: list[fes_i486_cases.Case], measured: dict[str, str]) -> None:
    IDENTITIES[identity][1].write_text(EXPECTED_HEADERS[identity]
                                       + ''.join(f'{case.label} {measured[case.label]}\n' for case in cases))


def read_expected(path: Path) -> dict[str, str]:
    out = {}
    for line in path.read_text().splitlines():
        if line and not line.startswith('#'):
            label, verdict = line.split(' ', 1)
            if label in out:
                raise I486Error(f'{path.name} lists {label} twice')
            out[label] = verdict
    return out


def stop_eip(verdict: str) -> int | None:
    """The EIP of a stop's first mismatch; None if the mismatch names none."""
    if not verdict.startswith('stop @'):
        return None
    return int(verdict[len('stop @'):].split('.', 1)[0], 16)


def judge(cases: list[fes_i486_cases.Case], measured: dict[str, str], expected: dict[str, str] | None) -> list[dict]:
    rows = []
    for case in cases:
        verdict = measured[case.label]
        probe = fes_i486_cases.CODE + case.probe if case.probe is not None else None
        eip = stop_eip(verdict)
        # A stop must be at or after the first instruction that needs a 486.
        isolated = verdict == 'agree' or (probe is not None and eip is not None and eip >= probe)
        rows.append({'label': case.label, 'probe': None if probe is None else f'{probe:x}', 'rtl': verdict,
                     'isolated': isolated, 'divergence': case.divergence,
                     'as_expected': None if expected is None else expected.get(case.label) == verdict})
    return rows


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true', help='fetch the pinned Bochs if it is not in build/')
    parser.add_argument('--oracle-only', action='store_true', help='the self-check alone, without Verilator')
    parser.add_argument('--write-expected', action='store_true',
                        help="write each identity's measured stops to its list instead of checking against it")
    parser.add_argument('--identity', choices=('386', '486sx', 'both'), default='both',
                        help='the CPU identities to run (default both)')
    i386_verilator.add_argument(parser)
    yosys_netlist.add_arguments(parser)
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    args = parser.parse_args()
    cases = fes_i486_cases.cases()
    if len({case.label for case in cases}) != len(cases):
        parser.error('case labels are not unique')
    OUTPUT.mkdir(parents=True, exist_ok=True)
    report: dict[str, object] = {}
    failures: list[str] = []
    try:
        linked = oracle.link_inputs(args.jobs, args.download)
        report['oracle'] = oracle.build_record(linked)
        report['testbench_sha256'] = hashlib.sha256((SIM / 'lockstep_tb.cpp').read_bytes()).hexdigest()
        report['cases_sha256'] = hashlib.sha256(''.join(case.line() + '\n' for case in cases).encode()).hexdigest()
        checked = self_check(oracle.build_testbench('i486', SIM / 'i486_tb.cpp', args.jobs, args.download), cases)
        report['self_check'] = checked
        failures += [f"self-check reset {row['field']}: expected {row['expected']}, oracle {row['oracle']}"
                     for row in checked['reset'] if not row['pass']]
        failures += [f"self-check {row['label']}: {row['differences']}" for row in checked['cases'] if not row['pass']]
        if not args.oracle_only:
            verilator = i386_verilator.locate(args.cache_root)
            report['verilator'] = verilator.record()
            yosys = yosys_netlist.locate(args)
            report['lockstep'] = {}
            for identity in (('386', '486sx') if args.identity == 'both' else (args.identity,)):
                i486, path = IDENTITIES[identity]
                testbench = verilate(verilator, linked, args.jobs, i486, yosys.path if yosys else None)
                first, second = stops(testbench, cases), stops(testbench, cases)
                if first != second:
                    failures.append(f'{identity}: two lockstep runs gave different stops: '
                                    + ', '.join(sorted(label for label in first if first[label] != second[label])))
                run_cases = cases + probe_cases(first, cases)
                if args.write_expected:
                    write_expected(identity, run_cases, first)
                expected = read_expected(path) if path.is_file() else None
                rows = judge(run_cases, first, expected)
                record = {'i486': i486, 'expected': path.name, 'cases': rows,
                          'agree': sum(row['rtl'] == 'agree' for row in rows),
                          'stop': sum(row['rtl'] != 'agree' for row in rows), 'runs_equal': first == second}
                if yosys:
                    record['netlist'] = yosys_netlist.report_record(ROOT, yosys, args.gpu_router,
                                                                    testbench.parent / f'{level3.TOP}.netlist.v')
                report['lockstep'][identity] = record
                failures += [f"{identity}: lockstep {row['label']}: {row['rtl']} is before the probe at {row['probe']}"
                             for row in rows if not row['isolated']]
                if expected is None:
                    failures.append(f'{path.name} is absent; write it with --identity {identity} --write-expected')
                else:
                    failures += [f"{identity}: lockstep {row['label']}: {row['rtl']!r}, "
                                 f"expected {expected.get(row['label'])!r}" for row in rows if not row['as_expected']]
                    failures += [f'{identity}: expected stop for a case that no longer exists: {label}'
                                 for label in sorted(set(expected) - set(first))]
        oracle.require_provisioner_unchanged()
    except (OSError, oracle.OracleError, i386_verilator.VerilatorError, yosys_netlist.NetlistError,
            I486Error) as error:
        print('i486 lockstep error:', error, file=sys.stderr)
        return 2
    report['failures'] = failures
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    passed = sum(row['pass'] for row in checked['cases'])
    print(f"self-check: {passed} of {len(cases)} cases and {sum(row['pass'] for row in checked['reset'])} of "
          f"{len(checked['reset'])} reset fields as a 486's documents give them on the level-{report['oracle']['cpu_level']} oracle")
    for identity, lockstep in report.get('lockstep', {}).items():
        print(f"lockstep {identity}{' netlist' if 'netlist' in lockstep else ''}: {lockstep['agree']} cases agree, "
              f"{lockstep['stop']} stop; two runs equal: {lockstep['runs_equal']}")
    for failure in failures:
        print('FAIL', failure)
    print(f"report: {OUTPUT / 'report.json'}")
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
