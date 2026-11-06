#!/usr/bin/env python3
"""Measure fes_i386_cpu's latency microbenchmarks on the RTL: CPU Phase 4's before and after.

Host simulation only (docs/i386-cpu-design.md, "Phase 4: speed").
cores/fes-common/sim/i386/latency_bench.asm runs in lockstep_tb.cpp's governor
mode, the RTL alone at max with port 0 at its fixed latency, as the governor
bench runs (scripts/test_fes_i386_governor.py). Each loop runs between two
marks, so its clocks are the cycles between them. The RTL alone is
deterministic, so each loop must take exactly the clocks in EXPECTED. A Phase 4
slice that changes a loop's clocks updates EXPECTED in the same change, and
its record gives the before and after.

Per iteration is the loop's clocks over its iterations. Per case takes the
bare DEC BP and JNZ loop's iteration (jcc_loop) out of the iteration and
divides by the eight cases it runs; the loops' set-up moves are left in.

  python3 scripts/test_fes_i386_latency.py
  python3 scripts/test_fes_i386_latency.py --yosys /abs/path/to/pc386-locked/yosys

Writes build/sim/fes-i386-latency/report.json (report-netlist.json with --yosys).
"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_pc386_quartus as producer
from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_governor as governor_bench
from scripts import test_fes_i386_lockstep as lockstep
from scripts import test_fes_i386_test386 as test386

ROOT = Path(__file__).resolve().parents[1]
PROGRAM = lockstep.SIM / 'latency_bench.asm'
OUTPUT = ROOT / 'build/sim/fes-i386-latency'
UNROLL = 8

# The loops in latency_bench.asm, by mark: name, iterations, cases an
# iteration runs (0 for the bare loop), and the clocks between its marks on
# the RTL at max.
LOOPS: list[tuple[str, int, int]] = [
    ('jcc_loop', 4000, 0),
    ('jcc_taken', 1000, UNROLL),
    ('call_ret', 1000, UNROLL),
    ('call_reg', 1000, UNROLL),
    ('call_mem', 1000, UNROLL),
    ('far_call', 1000, UNROLL),
    ('loop', 1000, UNROLL),
    ('agi_load', 1000, UNROLL),
    ('agi_alu', 1000, UNROLL),
    ('store_load_same', 1000, UNROLL),
    ('store_load_other', 1000, UNROLL),
    ('store_load_line', 1000, UNROLL),
    ('store_load_part', 1000, UNROLL),
    ('store_load_alias', 1000, UNROLL),
]
# CPU Phase 4 slice 2, the BTB (Jcc, LOOP, JCXZ, JMP and CALL rel). Slice 1's
# baseline, before it: jcc_loop 47999, jcc_taken 103999, call_ret 199118,
# call_reg 245999, call_mem 246029, far_call 269029, loop 90999, agi_load
# 44999, agi_alu 52999, store_load_same and store_load_other 36029.
# CPU Phase 4 slice 4, the store-to-load wait only on a possible overlap.
# Before it, with slice 2: store_load_other 26059, store_load_line 26050,
# store_load_part 26030, store_load_alias 26059.
EXPECTED: dict[str, int] = {
    'jcc_loop': 8039,
    'jcc_taken': 77045,
    'call_ret': 180137,
    'call_reg': 236029,
    'call_mem': 236059,
    'far_call': 259060,
    'loop': 42270,
    'agi_load': 35039,
    'agi_alu': 43029,
    'store_load_same': 26059,
    'store_load_other': 18059,
    'store_load_line': 18057,
    'store_load_part': 26030,
    'store_load_alias': 26059,
}


class LatencyBenchError(RuntimeError):
    pass


def spans(result: dict) -> dict[str, int]:
    """Each loop's cycles between its marks."""
    at = {mark['value']: mark['cycle'] for mark in result['marks'] if mark['port'] == 0x80}
    found = {}
    for n, (name, _, _) in enumerate(LOOPS):
        if 2 * n not in at or 2 * n + 1 not in at:
            raise LatencyBenchError(f'{name} has no marks')
        found[name] = at[2 * n + 1] - at[2 * n]
    return found


def measure(cycles: dict[str, int]) -> list[dict]:
    """Clocks per iteration and per case for each loop."""
    base = cycles['jcc_loop'] / LOOPS[0][1]
    rows = []
    for name, iterations, cases in LOOPS:
        per_iteration = cycles[name] / iterations
        row = {'name': name, 'cycles': cycles[name], 'iterations': iterations,
               'per_iteration': round(per_iteration, 3)}
        if cases:
            row['per_case'] = round((per_iteration - base) / cases, 3)
        rows.append(row)
    return rows


def failures(rows: list[dict], expected: dict[str, int]) -> list[str]:
    out = []
    for row in rows:
        want = expected.get(row['name'])
        if want is None:
            out.append(f'{row["name"]}: {row["cycles"]} cycles, with no expected value')
        elif row['cycles'] != want:
            out.append(f'{row["name"]}: {row["cycles"]} cycles, expected {want} '
                       f'({row["cycles"] - want:+d}; a slice that changes it updates EXPECTED)')
    for name in expected.keys() - {row['name'] for row in rows}:
        out.append(f'{name}: expected but not run')
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true', help='fetch the pinned oracle and NASM if missing')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--yosys', help="simulate this Yosys's netlist of the CPU instead of the RTL")
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 8))
    args = parser.parse_args()
    verilator = shutil.which(args.verilator)
    if verilator is None:
        print(f'selected Verilator not found: {args.verilator}', file=sys.stderr)
        return 2
    yosys = shutil.which(args.yosys) if args.yosys else None
    if args.yosys and yosys is None:
        print(f'selected Yosys not found: {args.yosys}', file=sys.stderr)
        return 2
    OUTPUT.mkdir(parents=True, exist_ok=True)
    try:
        nasm = test386.nasm(args.download, args.jobs)
        program = OUTPUT / 'latency_bench.bin'
        assembled = subprocess.run([str(nasm), '-f', 'bin', '-o', str(program), str(PROGRAM)],
                                   capture_output=True, text=True)
        if assembled.returncode:
            raise LatencyBenchError('nasm failed on latency_bench.asm\n' + assembled.stderr)
        rates = producer.guest_rates(100_000_000 if yosys else producer.SYSTEM_CLOCK_HZ)
        testbench = lockstep.verilate(verilator, oracle.link_inputs(args.jobs, args.download), args.jobs, yosys,
                                      None if yosys else {'GOV_N': rates['GOV_N'], 'GOV_D': rates['GOV_D']})
        result = governor_bench.run(testbench, program, 0, 0)
        cycles = spans(result)
    except (OSError, oracle.OracleError, test386.Test386Error, lockstep.LockstepError,
            governor_bench.GovernorBenchError, LatencyBenchError, subprocess.TimeoutExpired) as error:
        print('i386 latency bench error:', error, file=sys.stderr)
        return 2
    rows = measure(cycles)
    found = failures(rows, EXPECTED)
    report = {'yosys': yosys, 'loops': rows, 'run': result['run'], 'failures': found}
    path = OUTPUT / ('report-netlist.json' if yosys else 'report.json')
    path.write_text(json.dumps(report, indent=2) + '\n')
    print(f'{"loop":18} {"cycles":>8} {"per iteration":>14} {"per case":>9}')
    for row in rows:
        per_case = f'{row["per_case"]:9.3f}' if 'per_case' in row else ' ' * 9
        print(f'{row["name"]:18} {row["cycles"]:8} {row["per_iteration"]:14.3f} {per_case}')
    for failure in found:
        print('FAIL', failure)
    print(('FAIL' if found else 'PASS') + f' i386 latency (host simulation only; report: {path})')
    return 1 if found else 0


if __name__ == '__main__':
    sys.exit(main())
