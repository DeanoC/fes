#!/usr/bin/env python3
"""Run the directed 486 cases on the level-4 oracle, and the RTL beside it.

Two checks of scripts/fes_i486_cases.py's programs, both against Bochs at CPU
level 4 (scripts/fes_i486_oracle.py):

  self-check  each program on the oracle alone (i486_tb): its ending and
              registers must be what the case's row says a 486 gives.
  lockstep    each program on fes_i386_cpu in lockstep with the oracle
              (lockstep_tb, FES_I386_MODE=i486). The CPU is a 386 until the
              486 identity is built, so a case either keeps step to its end
              or stops at its first mismatch, which must not come before the
              case's probe, and the list of stops must equal
              cores/fes-common/sim/i386/i486-expected-stops.txt. A change
              that builds a 486 feature edits that list in the same commit.

The lockstep runs twice and both runs must give the same list. It runs the
RTL only: there is no netlist check until the RTL has something to check.

  python3 scripts/test_fes_i486_lockstep.py --download
  python3 scripts/test_fes_i486_lockstep.py --oracle-only
  python3 scripts/test_fes_i486_lockstep.py --write-expected

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

ROOT = Path(__file__).resolve().parents[1]
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-i486-lockstep'
EXPECTED = SIM / 'i486-expected-stops.txt'


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


def verilate(verilator: i386_verilator.LockedVerilator, linked: list[Path], jobs: int) -> Path:
    """The level-4 lockstep_tb (the level-3 one with its patch) with the RTL and the level-4 oracle."""
    output = OUTPUT / 'rtl'
    output.mkdir(parents=True, exist_ok=True)
    command = [str(verilator.path), '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', level3.TOP,
               '-Wall', '-Wno-UNUSEDSIGNAL', '--Mdir', str(output),
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM} -I{level3.DDR_MODEL}', '-LDFLAGS', '-pthread',
               *(str(level3.RTL / path) for path in level3.CPU_RTL), str(oracle.lockstep_testbench(OUTPUT / 'source')),
               *map(str, linked)]
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
    if result.returncode or set(out) != {case.label for case in cases}:
        raise I486Error(f'lockstep_tb i486 mode failed (status {result.returncode}): ' + result.stderr[-2000:])
    return out


EXPECTED_HEADER = '''\
# What fes_i386_cpu does with each directed 486 case against the level-4
# oracle: "agree", or its first mismatch as lockstep_tb prints it,
# "stop @<eip>.<field> <oracle> <rtl>". scripts/test_fes_i486_lockstep.py
# fails when a run differs from this list; write it with --write-expected.
# The CPU is a 386, so a stop here is a 486 feature not built yet.
'''


def write_expected(cases: list[fes_i486_cases.Case], measured: dict[str, str]) -> None:
    EXPECTED.write_text(EXPECTED_HEADER + ''.join(f'{case.label} {measured[case.label]}\n' for case in cases))


def read_expected() -> dict[str, str]:
    out = {}
    for line in EXPECTED.read_text().splitlines():
        if line and not line.startswith('#'):
            label, verdict = line.split(' ', 1)
            if label in out:
                raise I486Error(f'{EXPECTED.name} lists {label} twice')
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
                        help='write the measured stops to i486-expected-stops.txt instead of checking against it')
    i386_verilator.add_argument(parser)
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
        report['testbench_patch_sha256'] = hashlib.sha256(oracle.TESTBENCH_PATCH.read_bytes()).hexdigest()
        report['cases_sha256'] = hashlib.sha256(''.join(case.line() + '\n' for case in cases).encode()).hexdigest()
        checked = self_check(oracle.build_testbench('i486', SIM / 'i486_tb.cpp', args.jobs, args.download), cases)
        report['self_check'] = checked
        failures += [f"self-check reset {row['field']}: expected {row['expected']}, oracle {row['oracle']}"
                     for row in checked['reset'] if not row['pass']]
        failures += [f"self-check {row['label']}: {row['differences']}" for row in checked['cases'] if not row['pass']]
        if not args.oracle_only:
            verilator = i386_verilator.locate(args.cache_root)
            report['verilator'] = verilator.record()
            testbench = verilate(verilator, linked, args.jobs)
            first, second = stops(testbench, cases), stops(testbench, cases)
            if first != second:
                failures.append('two lockstep runs gave different stops: '
                                + ', '.join(sorted(label for label in first if first[label] != second[label])))
            if args.write_expected:
                write_expected(cases, first)
            expected = read_expected() if EXPECTED.is_file() else None
            rows = judge(cases, first, expected)
            report['lockstep'] = {'cases': rows, 'agree': sum(row['rtl'] == 'agree' for row in rows),
                                  'stop': sum(row['rtl'] != 'agree' for row in rows), 'runs_equal': first == second}
            failures += [f"lockstep {row['label']}: {row['rtl']} is before the probe at {row['probe']}"
                         for row in rows if not row['isolated']]
            if expected is None:
                failures.append(f'{EXPECTED.name} is absent; write it with --write-expected')
            else:
                failures += [f"lockstep {row['label']}: {row['rtl']!r}, expected {expected.get(row['label'])!r}"
                             for row in rows if not row['as_expected']]
                failures += [f'expected stop for a case that no longer exists: {label}'
                             for label in sorted(set(expected) - set(first))]
        oracle.require_provisioner_unchanged()
    except (OSError, oracle.OracleError, i386_verilator.VerilatorError, I486Error) as error:
        print('i486 lockstep error:', error, file=sys.stderr)
        return 2
    report['failures'] = failures
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2, sort_keys=True) + '\n')
    passed = sum(row['pass'] for row in checked['cases'])
    print(f"self-check: {passed} of {len(cases)} cases and {sum(row['pass'] for row in checked['reset'])} of "
          f"{len(checked['reset'])} reset fields as a 486's documents give them on the level-{report['oracle']['cpu_level']} oracle")
    if 'lockstep' in report:
        lockstep = report['lockstep']
        print(f"lockstep: {lockstep['agree']} cases agree, {lockstep['stop']} stop; two runs equal: {lockstep['runs_equal']}")
    for failure in failures:
        print('FAIL', failure)
    print(f"report: {OUTPUT / 'report.json'}")
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
