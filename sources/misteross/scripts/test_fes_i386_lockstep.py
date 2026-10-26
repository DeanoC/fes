#!/usr/bin/env python3
"""Run fes_i386_cpu in retirement lockstep with the i386 oracle.

Host simulation only. No network access occurs unless --download is given,
which fetches the pinned Bochs oracle (scripts/fes_i386_oracle.py) and the
pinned SingleStepTests/80386 corpus (scripts/test_fes_i386_vectors.py) into
the ignored build directory.

cores/fes-common/sim/i386/lockstep_tb.cpp runs the CPU top beside the oracle
and compares every retirement: EIP, the GPRs, EFLAGS, the segment registers,
memory writes and exceptions, with random latency on the fetch port. Two
sources of programs:

  corpus  SingleStepTests/80386 initial states for the instructions this
          build executes (SCOPE below), each run to its HLT. The RTL's final
          state must also equal the corpus's, undefined flags included: where
          the oracle and the corpus disagree, divergences.toml says which the
          FES CPU follows.
  random  Random programs of the same instructions with prefixes, branches
          and loops, so results forward between instructions and taken
          branches kill younger uops. A few seeds run again without latency;
          the retirement trace must not change.

An instruction that faults (#UD for LOCK, for example) must fault in the RTL
with the same vector and nothing retired. Exception delivery is not built,
so such a run ends there and its final state is not compared.

Beside the lockstep, every one-byte and 0Fh opcode followed by every second
byte that SCOPE leaves out must stop the RTL with FAULT_UNIMPLEMENTED before
anything retires (the testbench's unbuilt mode), so the lockstep, which only
runs what SCOPE names, cannot pass over an unbuilt instruction.

With --yosys the CPU is first run through that Yosys (read, proc -norom,
flatten, opt) and the netlist it writes is simulated instead of the RTL.

Example:
  python3 scripts/test_fes_i386_lockstep.py --download
  python3 scripts/test_fes_i386_lockstep.py --opcode 00 --opcode D3.4 --random-programs 0
  python3 scripts/test_fes_i386_lockstep.py --yosys /abs/path/to/i386-locked/yosys

Writes build/sim/fes-i386-lockstep/report.json.
"""
from __future__ import annotations

import argparse
from collections import Counter
from concurrent.futures import ProcessPoolExecutor
import gzip
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_frontend as frontend
from scripts import test_fes_i386_vectors as vectors

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-i386-lockstep'
CPU_RTL = ('fes_i386_pkg.sv', 'fes_i386_oplen.sv', 'fes_i386_lendec.sv', 'fes_i386_lstage.sv',
           'fes_i386_fetchbuf.sv', 'fes_i386_qalign.sv', 'fes_i386_frontend.sv', 'fes_i386_amode.sv',
           'fes_i386_dfwd.sv', 'fes_i386_agu.sv', 'fes_i386_decode.sv', 'fes_i386_dops.sv', 'fes_i386_shift.sv',
           'fes_i386_shflags.sv', 'fes_i386_exctl.sv', 'fes_i386_ex.sv', 'fes_i386_cpu.sv')
TOP = 'fes_i386_cpu'

# What the CPU executes, by MOO stem without 66h/67h prefixes: every form of
# each, memory forms included. Each later slice adds its stems here.
_ALU = [f'{row + column:02X}' for row in range(0, 0x40, 8) for column in range(6)]
SCOPE: frozenset[str] = frozenset([
    *_ALU,
    *(f'{opcode}.{reg}' for opcode in ('80', '81', '82', '83', 'C0', 'C1', 'D0', 'D1', 'D2', 'D3')
      for reg in range(8)),
    *(f'{opcode:02X}' for opcode in range(0x40, 0x50)),
    *(f'{opcode}.{reg}' for opcode in ('F6', 'F7') for reg in range(4)),
    'FE.0', 'FE.1', 'FF.0', 'FF.1', '84', '85', '88', '89', '8A', '8B', '8C', '8E', 'C6', 'C7',
    '0FA4', '0FA5', '0FAC', '0FAD', '0FB6', '0FB7', '0FBE', '0FBF',
    *(f'0F{opcode:02X}' for opcode in range(0x90, 0xA0)),
    '8D', '90', '98', '99', '9E', '9F', 'A8', 'A9', 'E0', 'E1', 'E2', 'E3', 'E9', 'EB',
    'F4', 'F5', 'F8', 'F9', 'FA', 'FB', 'FC', 'FD',
    *(f'{opcode:02X}' for opcode in [*range(0x70, 0x80), *range(0xB0, 0xC0)]),
    *(f'0F{opcode:02X}' for opcode in range(0x80, 0x90)),
    # The stack: PUSH and POP of registers, immediates and segment registers,
    # CALL rel, RET, LEAVE and JMP r/m.
    *(f'{opcode:02X}' for opcode in range(0x50, 0x60)),
    '06', '07', '0E', '16', '17', '1E', '1F', '0FA0', '0FA1', '0FA8', '0FA9',
    '68', '6A', 'C2', 'C3', 'C9', 'E8', 'FF.4',
])


class LockstepError(RuntimeError):
    pass


def in_scope(stem: str) -> bool:
    """Whether this build executes the stem's instructions."""
    return vectors.base_opcode(stem) in SCOPE


# Opcodes whose MOO stem carries the ModRM reg field, as lockstep_tb.cpp's
# grouped().
GROUPED_ONE = frozenset([0x80, 0x81, 0x82, 0x83, 0xC0, 0xC1, 0xD0, 0xD1, 0xD2, 0xD3, 0xF6, 0xF7, 0xFE, 0xFF])


def unbuilt_encodings() -> list[str]:
    """Every opcode (one-byte, then 0Fh) and following byte SCOPE leaves out."""
    lines = []
    for two_byte in (False, True):
        for opcode in range(256):
            if not two_byte and (opcode in vectors.PREFIXES or opcode == 0x0F):
                continue
            for following in range(256):
                code = bytes([0x0F, opcode, following] if two_byte else [opcode, following])
                stem = ('0F' if two_byte else '') + f'{opcode:02X}'
                if not two_byte and opcode in GROUPED_ONE or two_byte and opcode == 0xBA:
                    stem += f'.{(following >> 3) & 7}'
                # Every following byte, ModRM or not: a decoder that misreads
                # one as ModRM must still stop.
                if not in_scope(stem):
                    lines.append(code.hex())
    return lines


def run_unbuilt(testbench: str, env: dict, lines: list[str]) -> dict:
    env = dict(env, FES_I386_MODE='unbuilt', FES_I386_LATENCY='0')
    result = subprocess.run([testbench], input=''.join(line + '\n' for line in lines), env=env,
                            capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise LockstepError(f'unbuilt: testbench exited with {result.returncode}\n{result.stderr[-4000:]}')
    summary, failures = parse_output(result.stdout)
    if summary['runs'] != len(lines):
        raise LockstepError(f'unbuilt: testbench ran {summary["runs"]} of {len(lines)} encodings')
    return {'summary': summary, 'failures': failures}


def run_reset_store(testbench: str, env: dict) -> dict:
    env = dict(env, FES_I386_MODE='reset-store', FES_I386_LATENCY='1')
    result = subprocess.run([testbench], env=env, capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise LockstepError(f'reset-store: testbench exited with {result.returncode}\n{result.stderr[-4000:]}')
    summary, failures = parse_output(result.stdout)
    if summary['runs'] != 1:
        raise LockstepError(f'reset-store: testbench ran {summary["runs"]} checks, expected one')
    return {'summary': summary, 'failures': failures}


def divergence_table(entries: list[dict], path: Path) -> None:
    """The opcode-class entries, for the testbench to apply per retirement."""
    lines = []
    for entry in entries:
        if 'opcodes' not in entry:
            continue
        stems = '*' if entry['opcodes'] == ['*'] else ','.join(entry['opcodes'])
        lines.append(' '.join((entry['id'], entry['follow'], stems, ','.join(entry.get('when', [])) or '-',
                               ','.join(entry['fields']))))
    path.write_text('\n'.join(lines) + '\n')


def label_fields(entries: list[dict], stem: str, test: vectors.MooTest) -> tuple[list[str], list[str]]:
    """Fields that file- and case-scoped entries give to the corpus and to the oracle."""
    label = f'{stem}#{test.index}'
    corpus, oracle_side = [], []
    for entry in entries:
        if 'opcodes' in entry or not vectors.applies(entry, stem, label, None):
            continue
        if 'cases' in entry and entry['cases'][label] != test.hash:
            continue
        (corpus if entry['follow'] == 'corpus' else oracle_side).extend(entry['fields'])
    return corpus, oracle_side


def verilate(verilator: str, linked: list[Path], jobs: int, yosys: str | None) -> Path:
    output = OUTPUT / ('netlist' if yosys else 'rtl')
    output.mkdir(parents=True, exist_ok=True)
    if yosys:
        design = [frontend.synthesize_netlist(yosys, [RTL / path for path in CPU_RTL], TOP, output)]
        warnings = ['-Wno-fatal']
    else:
        design = [RTL / path for path in CPU_RTL]
        warnings = []
    command = [verilator, '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', TOP,
               '-Wall', '-Wno-UNUSEDSIGNAL', *warnings, '--Mdir', str(output),
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM}', '-LDFLAGS', '-pthread',
               *map(str, design), str(SIM / 'lockstep_tb.cpp'), *map(str, linked)]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise LockstepError(f'verilator failed; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + TOP)


def hold_failures(gaps: dict) -> list[str]:
    """D's flag and store holds, cycle-exactly where nothing else can hold:
    without latency a flag consumer right after a shift or rotate retires two
    cycles after it, and anything else one; a memory read right after a store
    at least two, and any other instruction after a store one."""
    failures = [f'FAIL flag hold: {count} {key} retirements {gap} cycles apart'
                for key, expected in (('shift_consumer', 2), ('after_shift', 1))
                for gap, count in gaps[key].items() if gap != expected]
    failures += [f'FAIL store hold: {count} store_load retirements {gap} cycles apart'
                 for gap, count in gaps['store_load'].items() if gap < 2]
    failures += [f'FAIL store hold: {count} after_store retirements {gap} cycles apart'
                 for gap, count in gaps['after_store'].items() if gap != 1]
    return failures


def parse_output(text: str) -> tuple[dict, list[str]]:
    summary, failures = None, []
    for line in text.splitlines():
        if line.startswith('{'):
            summary = json.loads(line)
        elif line.startswith('FAIL '):
            failures.append(line)
        elif line.strip():
            raise LockstepError('unexpected testbench output: ' + line)
    if summary is None:
        raise LockstepError('testbench printed no summary')
    return summary, failures


def run_file(path: str, testbench: str, env: dict, revoked: frozenset[str], entries: list[dict],
             limit: int | None) -> dict:
    stem = Path(path).name[:-len('.MOO.gz')]
    moo = vectors.parse_moo(gzip.decompress(Path(path).read_bytes()))
    tests = [test for test in (moo.tests[:limit] if limit else moo.tests) if test.hash not in revoked]
    lines = []
    for test in tests:
        corpus, oracle_side = label_fields(entries, stem, test)
        lines.append(' '.join([vectors.stream_line(stem, test), f'{len(corpus):x}', *corpus,
                               f'{len(oracle_side):x}', *oracle_side]))
    result = subprocess.run([testbench], input=''.join(line + '\n' for line in lines), env=env,
                            capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise LockstepError(f'{stem}: testbench exited with {result.returncode}\n{result.stderr[-4000:]}')
    summary, failures = parse_output(result.stdout)
    if summary['runs'] != len(tests):
        raise LockstepError(f'{stem}: testbench ran {summary["runs"]} of {len(tests)} tests')
    return {'stem': stem, 'tests': len(tests), 'summary': summary, 'failures': failures}


def run_random(testbench: str, env: dict, seed: int, programs: int, latency: bool) -> dict:
    env = dict(env, FES_I386_MODE='random', FES_I386_SEED=str(seed), FES_I386_PROGRAMS=str(programs),
               FES_I386_LATENCY='1' if latency else '0')
    result = subprocess.run([testbench], env=env, capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise LockstepError(f'random seed {seed}: testbench exited with {result.returncode}\n{result.stderr[-4000:]}')
    summary, failures = parse_output(result.stdout)
    return {'seed': seed, 'latency': latency, 'summary': summary, 'failures': failures}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true',
                        help='explicitly fetch the pinned oracle and corpus into ignored build/')
    parser.add_argument('--corpus', type=Path, help='an existing SingleStepTests/80386 data directory (absolute)')
    parser.add_argument('--opcode', action='append', help="MOO file stem, e.g. 'D3.4' or '6601'")
    parser.add_argument('--limit-per-file', type=int)
    parser.add_argument('--random-programs', type=int, default=2000, help='random programs per seed (default 2000)')
    parser.add_argument('--random-seeds', type=int, default=16, help='seeds, run in parallel (default 16)')
    parser.add_argument('--seed', type=int, default=1, help='the first random seed')
    parser.add_argument('--no-latency', action='store_true', help='no fetch latency in the corpus run')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--yosys', help="simulate this Yosys's netlist of the CPU instead of the RTL")
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    args = parser.parse_args()
    if not 1 <= args.jobs <= 64:
        parser.error('--jobs must be between 1 and 64')
    if args.corpus is not None and not args.corpus.is_absolute():
        parser.error('--corpus must be absolute')
    if args.random_programs < 0 or args.random_seeds < 1 or args.seed < 0:
        parser.error('--random-programs must be nonnegative, --random-seeds positive, --seed nonnegative')
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    verilator = shutil.which(args.verilator)
    if verilator is None:
        print(f'selected Verilator not found: {args.verilator}', file=sys.stderr)
        return 2
    OUTPUT.mkdir(parents=True, exist_ok=True)
    report: dict[str, object] = {}
    try:
        entries = vectors.load_divergences()
        table = OUTPUT / 'divergences.txt'
        divergence_table(entries, table)
        yosys = None
        if args.yosys:
            yosys = shutil.which(args.yosys)
            if yosys is None:
                raise LockstepError(f'selected Yosys not found: {args.yosys}')
            report['yosys'] = yosys
        linked = oracle.link_inputs(args.jobs, args.download)
        testbench = str(verilate(verilator, linked, args.jobs, yosys))
        env = dict(os.environ, FES_I386_DIVERGENCES=str(table), FES_I386_LATENCY='0' if args.no_latency else '1')
        reset_store = run_reset_store(testbench, env)

        corpus = args.corpus or vectors.OUTPUT / 'corpus' / vectors.REVISION
        if args.download:
            vectors.download(corpus, argparse.Namespace(opcode=args.opcode, download_jobs=8))
        if not corpus.is_dir():
            raise LockstepError(f'corpus directory does not exist: {corpus}; pass --download')
        paths = sorted(path for path in corpus.glob('*.MOO.gz')
                       if vectors.base_opcode(path.name[:-len('.MOO.gz')]) in SCOPE
                       and (not args.opcode or path.name[:-len('.MOO.gz')] in args.opcode))
        if args.opcode and len(paths) != len(set(args.opcode)):
            raise LockstepError('requested opcode files are absent or outside the scope')
        vectors.verify(corpus, paths + [corpus / 'revocation_list.txt'])
        revoked = frozenset(vectors.revoked_hashes((corpus / 'revocation_list.txt').read_text()))
        with ProcessPoolExecutor(max_workers=args.jobs) as executor:
            files = [executor.submit(run_file, str(path), testbench, env, revoked, entries, args.limit_per_file)
                     for path in paths]
            randoms = [executor.submit(run_random, testbench, env, args.seed + n, args.random_programs, True)
                       for n in range(args.random_seeds if args.random_programs else 0)]
            # The same programs without latency must retire the same trace.
            steady = [executor.submit(run_random, testbench, env, args.seed + n, args.random_programs, False)
                      for n in range(min(2, args.random_seeds) if args.random_programs else 0)]
            unbuilt_lines = unbuilt_encodings()
            unbuilt_chunks = [unbuilt_lines[n::args.jobs] for n in range(args.jobs)]
            unbuilt = [executor.submit(run_unbuilt, testbench, env, chunk) for chunk in unbuilt_chunks if chunk]
            corpus_results = [future.result() for future in files]
            unbuilt_results = [future.result() for future in unbuilt]
            random_results = [future.result() for future in randoms]
            steady_results = [future.result() for future in steady]
    except (OSError, oracle.OracleError, frontend.FrontendError, vectors.CorpusError, LockstepError,
            json.JSONDecodeError, KeyError) as error:
        print('i386 lockstep error:', error, file=sys.stderr)
        return 2

    exit_code = 0
    by_opcode: dict[str, Counter] = {}
    corpus_failures = []
    for result in corpus_results:
        summary = result['summary']
        counts = by_opcode.setdefault(vectors.base_opcode(result['stem']), Counter())
        counts.update({'tests': result['tests'], 'passed': summary['passed'], 'failed': summary['failed'],
                       'retired_to_hlt': summary['halts'], 'faults': summary['faults'],
                       'retirements': summary['retirements']})
        corpus_failures += result['failures']
    totals = sum(by_opcode.values(), Counter())
    fault_vectors = sum((Counter(result['summary']['fault_vectors']) for result in corpus_results), Counter())
    divergences = sum((Counter(result['summary']['divergence_retirements']) for result in corpus_results), Counter())
    random_totals = sum((Counter({key: result['summary'][key] for key in
                                  ('runs', 'passed', 'failed', 'retirements', 'cycles', 'halts', 'faults',
                                   'retire_limits', 'smc_stops', 'load_wait_cycles')}) for result in random_results),
                         Counter())
    random_failures = [line for result in random_results + steady_results for line in result['failures']]
    hash_mismatches = [result['seed'] for result in steady_results
                       if result['summary']['trace_hash'] != next(
                           other['summary']['trace_hash'] for other in random_results if other['seed'] == result['seed'])]
    # Cycles between retirements without latency, by what retired (see
    # lockstep_tb.cpp's Summary).
    gaps = {key: dict(sorted(sum((Counter({int(gap): count for gap, count in result['summary']['gaps'][key].items()})
                                  for result in steady_results), Counter()).items()))
            for key in ('sequential', 'prefixed', 'lea', 'selector', 'after_taken', 'after_not_taken',
                        'shift_consumer', 'after_shift', 'store_load', 'after_store', 'address', 'after_segment',
                        'after_stack')}
    held = hold_failures(gaps)
    unbuilt_failures = [line for result in unbuilt_results for line in result['failures']]
    unbuilt_runs = sum(result['summary']['runs'] for result in unbuilt_results)
    random_failures += held
    reset_store_failures = reset_store['failures']
    if corpus_failures or random_failures or hash_mismatches or unbuilt_failures or reset_store_failures:
        exit_code = 1
    report.update({
        'reset_store': reset_store,
        'corpus': {'revision': vectors.REVISION, 'files': len(corpus_results), 'totals': dict(totals),
                   'fault_vectors': dict(fault_vectors), 'divergence_retirements': dict(divergences),
                   'by_opcode': {stem: dict(counts) for stem, counts in sorted(by_opcode.items())},
                   'failures': corpus_failures[:200]},
        'unbuilt': {'encodings': unbuilt_runs, 'failures': unbuilt_failures[:200]},
        'random': {'seeds': [result['seed'] for result in random_results], 'programs_per_seed': args.random_programs,
                   'totals': dict(random_totals), 'latency_free_reruns': [result['seed'] for result in steady_results],
                   'trace_hash_mismatches': hash_mismatches, 'latency_free_gaps': gaps,
                   'failures': random_failures[:200]},
    })
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(f"corpus: {len(corpus_results)} files, {len(by_opcode)} opcodes, {totals['tests']} tests: "
          f"{totals['passed']} pass ({totals['retired_to_hlt']} to HLT, {totals['faults']} faulting "
          f"{dict(fault_vectors)}), {totals['failed']} fail; {totals['retirements']} retirements compared")
    if random_results:
        print(f"random: {len(random_results)} seeds x {args.random_programs} programs, {random_totals['retirements']} "
              f"retirements: {random_totals['passed']} pass ({random_totals['halts']} to HLT, {random_totals['faults']} "
              f"faulting, {random_totals['retire_limits']} at the retirement limit, {random_totals['smc_stops']} at a "
              f"store into their code; {random_totals['load_wait_cycles']} cycles waiting for a load's bytes), "
              f"{random_totals['failed']} fail; "
              f"latency-free reruns of seeds {[result['seed'] for result in steady_results]}: "
              f"{'trace unchanged' if not hash_mismatches else 'TRACE CHANGED for ' + str(hash_mismatches)}")
        def share(histogram: dict, cycles: int) -> str:
            total = sum(histogram.values())
            return f'{100 * histogram.get(cycles, 0) / total:.1f}%' if total else '-'
        common = max(gaps['after_taken'], key=gaps['after_taken'].get) if gaps['after_taken'] else None
        print(f"cycles between retirements without latency: {sum(gaps['sequential'].values())} other "
              f"instructions, {share(gaps['sequential'], 1)} one cycle apart; "
              f"{sum(gaps['after_not_taken'].values())} after a branch not taken, "
              f"{share(gaps['after_not_taken'], 1)} one cycle apart; {sum(gaps['after_taken'].values())} after a "
              f"taken branch, {share(gaps['after_taken'], common) if common else '-'} {common} cycles apart; "
              f"{sum(gaps['after_shift'].values())} after a shift, {share(gaps['after_shift'], 1)} one cycle apart; "
              f"{sum(gaps['shift_consumer'].values())} flag consumers after a shift, "
              f"{share(gaps['shift_consumer'], 2)} two cycles apart; "
              f"{sum(gaps['store_load'].values())} memory reads after a store, "
              f"{share(gaps['store_load'], 2)} two cycles apart; "
              f"{sum(gaps['after_store'].values())} others after a store, {share(gaps['after_store'], 1)} one cycle apart")
    print(f"unbuilt encodings: {unbuilt_runs} outside SCOPE, {len(unbuilt_failures)} not stopped as unimplemented")
    print(f"reset-store-on-reset: {reset_store['summary']['passed']} pass, {reset_store['summary']['failed']} fail")
    for line in (reset_store_failures + unbuilt_failures + corpus_failures + random_failures)[:40]:
        print(line)
    print(('i386 lockstep FAILED' if exit_code else 'i386 lockstep passed')
          + f' (host simulation only; report: {OUTPUT / "report.json"})')
    return exit_code


if __name__ == '__main__':
    raise SystemExit(main())
