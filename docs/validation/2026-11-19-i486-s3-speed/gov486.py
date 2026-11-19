#!/usr/bin/env python3
"""The governor bench's loops at the 486 speeds, through the frequency override.

Runs scripts/test_fes_i386_governor.py's program and testbench (host simulation of
the RTL at the package's clock) under max, preset 7 (486DX2-66), and preset 6
(486DX-33) with record word 1 at 0 (its own 33.3 MHz), 200, 250, 500 and 667.
For each loop it reports the cycles, the table's time at that frequency and
whether the RTL at max is slower than that (beyond the CPU), with the bench's
own rules. It also checks that preset 6 at 667 runs every loop in exactly
preset 7's cycles and charges, which is what makes the override equal to a
named preset.

Run from sources/misteross:
  FES_TOOLCHAIN_CACHE_ROOT=/abs/misteross-toolchains \
    python3 ../../docs/validation/2026-11-19-i486-s3-speed/gov486.py OUT.json
"""
import json
import os
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

sys.path.insert(0, os.getcwd())
from scripts import test_fes_i386_governor as bench  # noqa: E402

FREQS = (0, 200, 250, 500, 667)


def main() -> int:
    out = Path(sys.argv[1])
    verilator = bench.i386_verilator.locate(Path(os.environ['FES_TOOLCHAIN_CACHE_ROOT']))
    table = bench.governor.load()
    presets = {p.id: p for p in table.presets}
    bench.OUTPUT.mkdir(parents=True, exist_ok=True)
    nasm = bench.test386.nasm(True, 8)
    program = bench.OUTPUT / 'governor_bench.bin'
    subprocess.run([str(nasm), '-f', 'bin', '-o', str(program), str(bench.PROGRAM)], check=True)
    rates = bench.producer.guest_rates(bench.CLOCK_HZ)
    testbench = bench.lockstep.verilate(verilator, bench.oracle.link_inputs(8, True), 8, None,
                                        {'GOV_N': rates['GOV_N'], 'GOV_D': rates['GOV_D']})
    runs = [(0, 0), (7, 0)] + [(6, f) for f in FREQS]
    with ThreadPoolExecutor(max_workers=8) as ex:
        fut = {k: ex.submit(bench.run, testbench, program, k[0], 20000, k[1]) for k in runs}
        res = {k: f.result() for k, f in fut.items()}
    base = bench.spans(res[(0, 0)])
    report = {'clock_hz': bench.CLOCK_HZ, 'budget': bench.BUDGET, 'verilator': verilator.record(),
              'max': base, 'runs': {}}
    failures = []
    for (pid, freq), result in res.items():
        if pid == 0:
            continue
        preset = presets[pid]
        f = freq or preset.frequency
        hz = f * 100_000
        key = f'{preset.name}@{f / 10:g}MHz' + ('' if not freq else ' (word 1)')
        loops = []
        if result['state'] != res[(0, 0)]['state']:
            failures.append(f'{key}: final state differs from max')
        for span, at_max, (name, parts) in zip(bench.spans(result), base, bench.BENCHES):
            want = bench.expected(table, preset, parts, freq)
            table_cycles = want * bench.CLOCK_HZ / hz
            beyond = at_max['cycles'] > table_cycles * (1 - bench.BUDGET)
            error = span['cycles'] / table_cycles - 1
            ceiling = want * bench.CLOCK_HZ / at_max['cycles'] / 1e6
            loops.append({'name': name, 'cycles': span['cycles'], 'charged': span['charged'], 'table_clocks': want,
                          'table_cycles': round(table_cycles, 1), 'error': round(error, 5), 'beyond_cpu': beyond,
                          'ceiling_mhz': round(ceiling, 1)})
            if span['charged'] != want:
                failures.append(f'{key} {name}: charged {span["charged"]}, table {want}')
            if not beyond and abs(error) > bench.BUDGET:
                failures.append(f'{key} {name}: {error:+.2%} from the table')
        report['runs'][key] = {'preset': pid, 'word1': freq, 'mhz': f / 10, 'loops': loops,
                               'beyond': [l['name'] for l in loops if l['beyond_cpu']]}
        print(f'{key:32} beyond on {len(report["runs"][key]["beyond"])}/{len(loops)}: '
              f'{", ".join(report["runs"][key]["beyond"]) or "-"}; worst reached error '
              f'{max((abs(l["error"]) for l in loops if not l["beyond_cpu"]), default=0):.2%}')
    a, b = bench.spans(res[(6, 667)]), bench.spans(res[(7, 0)])
    same = all(x['cycles'] == y['cycles'] and x['charged'] == y['charged'] for x, y in zip(a, b))
    report['preset6_667_equals_preset7'] = same
    if not same:
        failures.append('preset 6 at word 1 = 667 differs from preset 7')
    print(f'preset 6 @ 667 == preset 7, loop for loop (cycles and charges): {same}')
    report['failures'] = failures
    out.write_text(json.dumps(report, indent=1) + '\n')
    for f in failures:
        print('FAIL', f)
    print('FAIL' if failures else 'PASS')
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
