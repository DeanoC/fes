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

  random      the level-3 suite's random programs (lockstep_tb.cpp
              FES_I386_MODE=random) at the 486SX identity against the
              level-4 oracle: the same five sets and seeds
              (test_fes_i386_lockstep.py's random, protected-mode, task,
              debug and undefined programs), each with 486 forms in it
              (Generator::isa486: BSWAP, XADD, CMPXCHG, INVD, WBINVD, INVLPG
              and PTE remaps, CPUID, AC toggles) from a 486's start state
              (CR0.AM mostly set, WP and NE at random, AC and ID at random),
              and two seeds again without latency, whose traces must not
              change. Every program must keep step; the only rule of the
              level-4 oracle's own is CPUID's (lockstep_tb.cpp cpuid_rule).
              Each 486 form must retire somewhere in the suite, and #AC and
              a WP fault must be delivered.

  python3 scripts/test_fes_i486_lockstep.py --download
  python3 scripts/test_fes_i486_lockstep.py --oracle-only
  python3 scripts/test_fes_i486_lockstep.py --netlist
  python3 scripts/test_fes_i486_lockstep.py --identity 486sx --write-expected
  python3 scripts/test_fes_i486_lockstep.py --identity 486sx --jobs 4
  python3 scripts/test_fes_i486_lockstep.py --identity 486sx --random-programs 0 --pm-programs 0 \
      --task-programs 0 --debug-programs 0 --undefined-programs 0

Writes build/sim/fes-i486-lockstep/report.json. Exit status 1 on any failure.
"""
from __future__ import annotations

import argparse
from collections import Counter
from concurrent.futures import ProcessPoolExecutor
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
from scripts import test_fes_i386_vectors as vectors
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


# The 486 forms (lockstep_tb.cpp count_i486_form) that must retire somewhere
# in the random suite, and the deliveries it must reach: #AC, and a CPL-0
# write that WP stops. CPUID of leaf 1 and above is held to the 486SX's
# answer by the CPUID rule (cpuid-486sx).
REQUIRED_FORMS = ('bswap32', 'bswap16', 'xadd-r8', 'xadd-r16', 'xadd-r32', 'xadd-m8', 'xadd-m16', 'xadd-m32',
                  'xadd-m16-lock', 'cmpxchg-r32-equal', 'cmpxchg-r32-unequal', 'cmpxchg-r8-equal', 'cmpxchg-m16-equal',
                  'cmpxchg-m16-unequal', 'cmpxchg-m32-equal-lock', 'invd', 'wbinvd', 'invlpg', 'cpuid-0', 'cpuid-1',
                  'cpuid-2+', 'ac-toggle', 'ac-fault', 'wp-fault')
# The random suite's summary counts, added over its seeds.
RANDOM_COUNTS = ('runs', 'passed', 'failed', 'retirements', 'cycles', 'halts', 'faults', 'shutdowns', 'nested',
                 'retire_limits', 'io_accesses', 'external_interrupts', 'nmis', 'rep_interrupts', 'vector_moved_stops',
                 'window_code_stops', 'code_store_stops', 'stray_stops', 'mmio_runs', 'mmio_accesses', 'replays',
                 'smc_restarts', 'pm_retirements', 'cs_rule', 'ss_error_rule', 'fetch_rule', 'iret_save_rule',
                 'dr6_rule', 'db_rf_rule')


def random_suite(testbench: Path, args: argparse.Namespace) -> dict:
    """The level-3 suite's random program sets at level 4 (the module's docstring), args.jobs at a time."""
    table = testbench.parent / 'divergences.txt'
    level3.divergence_table(vectors.load_divergences(), table)
    env = dict(os.environ, FES_I386_DIVERGENCES=str(table), FES_I386_PRESET='0')
    bench = str(testbench)
    # (set, seed, programs, latency, pm_weight, tasks, debug, undefined), as test_fes_i386_lockstep.py's.
    jobs = [('random', args.seed + n, args.random_programs, True, 2, False, False, False)
            for n in range(args.random_seeds if args.random_programs else 0)]
    jobs += [('steady', args.seed + n, args.random_programs, False, 2, False, False, False)
             for n in range(min(2, args.random_seeds) if args.random_programs else 0)]
    jobs += [('pmode', level3.PM_SEED + args.seed + n, args.pm_programs, True, level3.PM_WEIGHT, False, False, False)
             for n in range(args.random_seeds if args.pm_programs else 0)]
    jobs += [('tasks', level3.TASK_SEED + args.seed + n, args.task_programs, True, level3.PM_WEIGHT, True, False,
              False) for n in range(args.random_seeds if args.task_programs else 0)]
    jobs += [('debug', level3.DEBUG_SEED + args.seed + n, args.debug_programs, True, 2, False, True, False)
             for n in range(args.random_seeds if args.debug_programs else 0)]
    jobs += [('undefined', level3.UNDEFINED_SEED + args.seed + n, args.undefined_programs, True, level3.PM_WEIGHT,
              False, False, True) for n in range(args.random_seeds if args.undefined_programs else 0)]
    with ProcessPoolExecutor(max_workers=args.jobs) as executor:
        futures = [(name, executor.submit(level3.run_random, bench, env, seed, programs, latency, weight, tasks,
                                          debug, undefined))
                   for name, seed, programs, latency, weight, tasks, debug, undefined in jobs]
        results = [(name, future.result()) for name, future in futures]
    out: dict[str, object] = {'divergences_sha256': hashlib.sha256(table.read_bytes()).hexdigest(), 'sets': {}}
    forms, divergences, vectors_by_set, failures = Counter(), Counter(), {}, []
    for name in dict.fromkeys(name for name, _ in results):
        chosen = [result for set_name, result in results if set_name == name]
        totals = sum((Counter({key: result['summary'][key] for key in RANDOM_COUNTS}) for result in chosen), Counter())
        set_forms = sum((Counter(result['summary'].get('i486_forms', {})) for result in chosen), Counter())
        set_divergences = sum((Counter(result['summary']['divergence_retirements']) for result in chosen), Counter())
        set_vectors = sum((Counter(result['summary']['fault_vectors']) for result in chosen), Counter())
        out['sets'][name] = {'seeds': [result['seed'] for result in chosen],
                             'programs_per_seed': chosen[0]['summary']['runs'] if chosen else 0,
                             'totals': dict(totals), 'i486_forms': dict(sorted(set_forms.items())),
                             'divergence_retirements': dict(sorted(set_divergences.items())),
                             'fault_vectors': dict(sorted(set_vectors.items())),
                             'trace_hashes': {str(result['seed']): result['summary']['trace_hash'] for result in chosen},
                             'failures': [line for result in chosen for line in result['failures']][:200]}
        failures += [line for result in chosen for line in result['failures']]
        if name != 'steady':
            forms += set_forms
        divergences += set_divergences
    hashes = {result['seed']: result['summary']['trace_hash'] for name, result in results if name == 'random'}
    mismatches = [result['seed'] for name, result in results
                  if name == 'steady' and result['summary']['trace_hash'] != hashes.get(result['seed'])]
    out['i486_forms'] = dict(sorted(forms.items()))
    out['divergence_retirements'] = dict(sorted(divergences.items()))
    out['latency_free_mismatches'] = mismatches
    out['missing_forms'] = [form for form in REQUIRED_FORMS if not forms.get(form)] if jobs else []
    out['failure_count'] = len(failures)
    out['failures'] = failures[:400]
    return out


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
    parser.add_argument('--random-programs', type=int, default=2000,
                        help='486SX random programs per seed against the level-4 oracle (default 2000)')
    parser.add_argument('--random-seeds', type=int, default=16, help='seeds per random set (default 16)')
    parser.add_argument('--seed', type=int, default=1, help='the first random seed')
    parser.add_argument('--pm-programs', type=int, default=500, help='protected-mode programs per seed (default 500)')
    parser.add_argument('--task-programs', type=int, default=250, help='task-switch programs per seed (default 250)')
    parser.add_argument('--debug-programs', type=int, default=250, help='debug programs per seed (default 250)')
    parser.add_argument('--undefined-programs', type=int, default=250,
                        help='programs with undefined encodings per seed (default 250)')
    parser.add_argument('--random-only', action='store_true', help='the random suite alone, without the cases')
    args = parser.parse_args()
    if (min(args.random_programs, args.pm_programs, args.task_programs, args.debug_programs,
            args.undefined_programs, args.seed) < 0 or args.random_seeds < 1):
        parser.error('program counts and --seed must be nonnegative, --random-seeds positive')
    if args.random_only and (args.oracle_only or args.identity != '486sx' or args.write_expected):
        parser.error('--random-only runs the 486SX alone, and checks no expected list')
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
                if identity == '486sx' and (args.random_programs or args.pm_programs or args.task_programs
                                            or args.debug_programs or args.undefined_programs):
                    random = random_suite(testbench, args)
                    if yosys:
                        random['netlist'] = yosys_netlist.report_record(ROOT, yosys, args.gpu_router,
                                                                        testbench.parent / f'{level3.TOP}.netlist.v')
                    report['random'] = random
                    failures += [f'486sx random: {line}' for line in random['failures']]
                    if random['failure_count'] > len(random['failures']):
                        failures.append(f"486sx random: {random['failure_count'] - len(random['failures'])} more failures")
                    failures += [f'486sx random: seed {seed} retired a different trace without latency'
                                 for seed in random['latency_free_mismatches']]
                    failures += [f'486sx random: no {form} in the suite' for form in random['missing_forms']]
                if args.random_only:
                    continue
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
    if 'random' in report:
        for name, row in report['random']['sets'].items():
            print(f"random {name}: {row['totals'].get('passed', 0)} of {row['totals'].get('runs', 0)} programs keep step")
        print('486 forms:', ', '.join(f'{form} {count}' for form, count in report['random']['i486_forms'].items()))
    for failure in failures:
        print('FAIL', failure)
    print(f"report: {OUTPUT / 'report.json'}")
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
