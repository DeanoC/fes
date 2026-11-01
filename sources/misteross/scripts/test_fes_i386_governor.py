#!/usr/bin/env python3
"""Check the i386 speed governor's charges, pacing and interrupt latency on the RTL.

Host simulation only (docs/i386-cpu-design.md, "Speed control").
cores/fes-common/sim/i386/governor_bench.asm runs in lockstep_tb.cpp's
governor mode, the RTL alone with port 0 at its fixed latency and I/O done at
once, under max and under each preset, at the package's sealed clock: GOV_N
and GOV_D are the producer's (scripts/build_fes_pc386_quartus.py), so cycles
are the kit's. For each benchmark loop, between its two marks:

  charges  the reference clocks the governor charged must equal the table's
           (governor.toml through scripts/fes_i386_governor.py), which
           BENCHES below lists instruction by instruction with each one's
           memory bus cycles, wait states and dword accesses on a 16-bit bus
           included. Under max it charges nothing.
  pacing   the time between the marks must be within 5% (the budget) of
           those clocks at the preset's frequency, unless the RTL at max
           takes longer than the preset's time less 5%: then the preset is
           beyond the CPU on that loop, which the report says, and the loop
           may take no longer than max's time plus the budget. A preset in
           ACCEPTED must reach every loop.

Two more runs check the record's other words: 286-12 with its frequency
overridden to 6 MHz (word 1) must pace every loop at 6 MHz, and 286-12 with
turbo at launch (word 3) must run each loop in exactly max's cycles and
charge nothing.

The interrupt latency run raises INTR (the handler's OUT is its first
instruction) while a loop of REP MOVSB, DIV, PUSH and POP runs. Under a
preset every latency, in reference clocks, must stay within the window, the
largest instruction or element charge the loop makes, and the delivery's own
charge. Every run must halt with the same registers as max's.

With --yosys the CPU is first run through that Yosys (read, proc -norom,
flatten, opt), as test_fes_i386_lockstep.py --yosys does, and the netlist is
simulated instead, at the CPU's default GOV_N/GOV_D (a 100 MHz clock), since
Yosys has elaborated the parameters away.

  python3 scripts/test_fes_i386_governor.py
  python3 scripts/test_fes_i386_governor.py --preset 1 --preset 3
  python3 scripts/test_fes_i386_governor.py --yosys /abs/path/to/pc386-locked/yosys

Writes build/sim/fes-i386-governor/report.json.
"""
from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_pc386_quartus as producer
from scripts import fes_i386_governor as governor
from scripts import fes_i386_oracle as oracle
from scripts import test_fes_i386_lockstep as lockstep
from scripts import test_fes_i386_test386 as test386

ROOT = Path(__file__).resolve().parents[1]
PROGRAM = lockstep.SIM / 'governor_bench.asm'
OUTPUT = ROOT / 'build/sim/fes-i386-governor'
CLOCK_HZ = producer.SYSTEM_CLOCK_HZ
WINDOW = 64
BUDGET = 0.05
# The presets that must reach every loop. The 486 presets are faster than
# the RTL on some loops, which the report lists.
ACCEPTED = (1, 2, 3, 4, 5)
OVERRIDE = (1, 60)  # 286-12 at 6 MHz


@dataclass(frozen=True)
class Op:
    """One instruction's charge: its class's cost column (1: taken, or a REP
    element), its memory bus cycles, whether they are dwords, and whether
    its address is base, index and displacement."""
    cls: str
    column: int = 0
    accesses: int = 0
    dword: bool = False
    ea3: bool = False


def r(cls: str) -> Op:
    return Op(cls)


def taken(cls: str) -> Op:
    return Op(cls, 1)


def m(cls: str, accesses: int, dword: bool = False, ea3: bool = False) -> Op:
    return Op(cls, 0, accesses, dword, ea3)


def element(cls: str, accesses: int) -> Op:
    return Op(cls, 1, accesses)


START = [r('out_imm')]          # the first mark's OUT is charged inside the loop's span
END = [r('mov_r')]              # MOV AL before the second mark's OUT
DEC_JNZ = [r('alu_rr'), taken('jcc')]
DEC_FALL = [r('alu_rr'), r('jcc')]


def loop(n: int, body: list[Op], last: list[Op] | None = None, setup: list[Op] = ()) -> list[tuple[int, list[Op]]]:
    """A benchmark that counts BP down from n: setup, then the body n - 1
    times with its branch taken, once more falling through (last)."""
    return [(1, START + [r('mov_r')] + list(setup)), (n - 1, body + DEC_JNZ),
            (1, (last if last is not None else body) + DEC_FALL), (1, END)]


# The benchmarks in governor_bench.asm, by mark: name and the instructions
# between the marks. Each class name is chosen from the instruction here,
# apart from the RTL's classify().
BENCHES: list[tuple[str, list[tuple[int, list[Op]]]]] = [
    ('alu', loop(4000, [r('alu_rr')] * 4)),
    ('memory', loop(3000, [m('mov_load', 1), m('alu_load', 1, ea3=True), m('mov_store', 1), m('alu_rmw', 2),
                           m('cmp_load', 1)])),
    ('dword', loop(3000, [m('mov_load', 1, dword=True), r('alu_rr'), m('mov_store', 1, dword=True),
                          r('shiftn_r')])),
    ('branch', loop(3000, [r('alu_rr'), r('jcc'), r('jmp'), m('call', 1), m('ret', 1)])),
    ('stack', loop(3000, [m('push_r', 1), m('push_r', 1), m('pop_r', 1), m('pop_r', 1), m('pushf', 1),
                          m('popf', 1)])),
    ('muldiv', loop(1000, [r('mov_r'), r('mul16_r'), r('div16_r'), r('mul16_r'), r('mul8_r')])),
    ('string', loop(200, [r('mov_r'), r('mov_r'), r('mov_r'), m('rep_movs', 0), *[element('rep_movs', 2)] * 32,
                          r('mov_r'), m('rep_stos', 0), *[element('rep_stos', 1)] * 16, m('movs', 2),
                          m('lods', 1)])),
    ('shift', loop(3000, [r('shift1_r'), r('shiftn_r'), r('shiftn_r'), r('rc1_r'), m('shift1_m', 2)])),
    # Turbo Pascal's CRT delay loop: 4999 passes back to the compare's JE,
    # then the count's borrow ends it at JC.
    ('crt', [(1, START + [r('mov_r'), r('alu_rr')]),
             (4999, [r('alu_ri'), r('alu_ri'), r('jcc'), m('cmp_load', 1), taken('jcc')]),
             (1, [r('alu_ri'), r('alu_ri'), taken('jcc')]), (1, END)]),
    ('io', loop(2000, [r('in_imm'), r('alu_ri'), r('out_imm')])),
    ('int', loop(500, [m('int', 5), m('iret', 3)])),
    ('misc', loop(2000, [m('lptr', 2), r('xchg_r'), m('lea', 0, ea3=True), r('flag'), r('flag')])),
    ('loop', [(1, START + [r('mov_r')]), (2999, [r('nop'), taken('loop')]), (1, [r('nop'), r('loop')]), (1, END)]),
    ('pusha', loop(500, [m('pusha', 8), m('popa', 8)])),
    ('i386', loop(2000, [m('movx_m', 1), r('setcc_r'), r('bt_r')])),
]
IRQ_LOOP = [r('mov_r'), m('rep_movs', 0), element('rep_movs', 2), r('alu_ri'), r('mov_r'), r('alu_rr'),
            r('div16_r'), m('push_r', 1), m('pop_r', 1), r('alu_rr'), taken('jcc')]
DELIVERY = [m('int', 5)]


class GovernorBenchError(RuntimeError):
    pass


def charge(table: governor.Table, preset: governor.Preset, op: Op) -> int:
    cls = table.by_name(op.cls)
    t = preset.table
    clocks = (cls.cost if op.column == 0 else cls.column1)[t]
    clocks += int(op.ea3 and cls.memory)
    per_access = preset.wait + (preset.wait + 2 if preset.bus16 and op.dword else 0)
    return clocks + op.accesses * per_access


def expected(table: governor.Table, preset: governor.Preset, parts: list[tuple[int, list[Op]]]) -> int:
    return sum(times * sum(charge(table, preset, op) for op in ops) for times, ops in parts)


def run(testbench: Path, program: Path, preset: int, period: int, frequency: int = 0, turbo: int = 0) -> dict:
    env = dict(os.environ, FES_I386_MODE='governor', FES_I386_LATENCY='0', FES_I386_PROGRAM=str(program),
               FES_I386_PRESET=str(preset), FES_I386_FREQUENCY=str(frequency), FES_I386_TURBO=str(turbo),
               FES_I386_IRQ_PERIOD=str(period))
    result = subprocess.run([str(testbench)], env=env, capture_output=True, text=True, timeout=3600)
    if result.returncode not in (0, 1):
        raise GovernorBenchError(f'preset {preset}: testbench exited {result.returncode}\n{result.stderr[-2000:]}')
    marks, irqs, state, run_line = [], [], None, None
    for line in result.stdout.splitlines():
        kind, _, rest = line.partition(' ')
        if kind == 'MARK':
            port, value, cycle, charged = rest.split()
            marks.append({'port': int(port, 16), 'value': int(value), 'cycle': int(cycle), 'charged': int(charged)})
        elif kind == 'IRQ':
            irqs.append(int(rest))
        elif kind == 'STATE':
            state = rest.split()
        elif kind == 'GOVERNOR':
            run_line = {key: int(value) for key, value in (field.split('=') for field in rest.split())}
    if run_line is None or not run_line['halted']:
        raise GovernorBenchError(f'preset {preset}: the program did not finish: {run_line}\n{result.stdout[-2000:]}')
    return {'preset': preset, 'marks': marks, 'irqs': irqs, 'state': state, 'run': run_line}


def spans(result: dict) -> list[dict]:
    """Each benchmark's cycles and charges between its marks."""
    at = {mark['value']: mark for mark in result['marks'] if mark['port'] == 0x80}
    found = []
    for n, (name, _) in enumerate(BENCHES):
        first, second = at.get(2 * n), at.get(2 * n + 1)
        if first is None or second is None:
            raise GovernorBenchError(f'preset {result["preset"]}: {name} has no marks')
        found.append({'name': name, 'cycles': second['cycle'] - first['cycle'],
                      'charged': second['charged'] - first['charged']})
    return found


def latencies(result: dict) -> list[int]:
    handled = [mark['cycle'] for mark in result['marks'] if mark['port'] == 0x81]
    out = []
    for raised in result['irqs']:
        entry = next((cycle for cycle in handled if cycle >= raised), None)
        if entry is None:
            raise GovernorBenchError(f'preset {result["preset"]}: the INTR at {raised} was never handled')
        out.append(entry - raised)
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--preset', type=int, action='append', choices=range(1, 8),
                        help='a preset to run beside max (default every preset)')
    parser.add_argument('--irq-period', type=int, default=20000,
                        help='cycles from one handler to the next INTR (default 20000)')
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
    clock_hz = 100_000_000 if yosys else CLOCK_HZ
    OUTPUT.mkdir(parents=True, exist_ok=True)
    table = governor.load()
    presets = {p.id: p for p in table.presets}
    chosen = sorted(set(args.preset or presets))
    try:
        nasm = test386.nasm(args.download, args.jobs)
        program = OUTPUT / 'governor_bench.bin'
        assembled = subprocess.run([str(nasm), '-f', 'bin', '-o', str(program), str(PROGRAM)],
                                   capture_output=True, text=True)
        if assembled.returncode:
            raise GovernorBenchError('nasm failed on governor_bench.asm\n' + assembled.stderr)
        rates = producer.guest_rates(clock_hz)
        testbench = lockstep.verilate(verilator, oracle.link_inputs(args.jobs, args.download), args.jobs, yosys,
                                      None if yosys else {'GOV_N': rates['GOV_N'], 'GOV_D': rates['GOV_D']})
        with ThreadPoolExecutor(max_workers=args.jobs) as executor:
            futures = {preset: executor.submit(run, testbench, program, preset, args.irq_period)
                       for preset in [0, *chosen]}
            override = executor.submit(run, testbench, program, OVERRIDE[0], args.irq_period, OVERRIDE[1])
            turbo = executor.submit(run, testbench, program, OVERRIDE[0], args.irq_period, 0, 1)
            results = {preset: future.result() for preset, future in futures.items()}
            override, turbo = override.result(), turbo.result()
    except (OSError, oracle.OracleError, test386.Test386Error, lockstep.LockstepError, GovernorBenchError,
            subprocess.TimeoutExpired) as error:
        print('i386 governor bench error:', error, file=sys.stderr)
        return 2

    failures = []
    report = {'clock_hz': clock_hz, 'yosys': yosys, 'window': WINDOW, 'budget': BUDGET, 'accepted': list(ACCEPTED), 'presets': {}}
    base = spans(results[0])
    for span in base:
        if span['charged']:
            failures.append(f'max: {span["name"]} charged {span["charged"]}')
    report['max'] = {'spans': base, 'run': results[0]['run'], 'latency_cycles': latencies(results[0])}
    print(f'{"loop":8} {"max cyc":>9} ' + ' '.join(f'{presets[p].name:>20}' for p in chosen))
    rows = {span['name']: [f'{span["name"]:8} {span["cycles"]:9}'] for span in base}
    for preset_id in chosen:
        preset = presets[preset_id]
        result = results[preset_id]
        entry = {'name': preset.name, 'run': result['run'], 'loops': []}
        if result['state'] != results[0]['state']:
            failures.append(f'{preset.name}: halted with {result["state"]}, max with {results[0]["state"]}')
        hz = preset.frequency * 100_000
        for span, at_max, (name, parts) in zip(spans(result), base, BENCHES):
            want = expected(table, preset, parts)
            table_cycles = want * clock_hz / hz
            error = span['cycles'] / table_cycles - 1
            beyond = at_max['cycles'] > table_cycles * (1 - BUDGET)
            loop_entry = {'name': name, 'cycles': span['cycles'], 'charged': span['charged'], 'table_clocks': want,
                          'table_cycles': round(table_cycles, 1), 'error': round(error, 5), 'beyond_cpu': beyond}
            entry['loops'].append(loop_entry)
            if span['charged'] != want:
                failures.append(f'{preset.name} {name}: charged {span["charged"]} reference clocks, the table {want}')
            if beyond:
                if preset_id in ACCEPTED:
                    failures.append(f'{preset.name} {name}: the RTL at max ({at_max["cycles"]} cycles) does not reach '
                                    f'the table time ({table_cycles:.0f})')
                elif span['cycles'] > at_max['cycles'] * (1 + BUDGET):
                    failures.append(f'{preset.name} {name}: {span["cycles"]} cycles, slower than max ({at_max["cycles"]})')
            elif abs(error) > BUDGET:
                failures.append(f'{preset.name} {name}: {span["cycles"]} cycles, {error:+.2%} from the table '
                                f'({table_cycles:.0f})')
            rows[name].append(f'{span["cycles"]:9} {error:+7.2%}{" beyond" if beyond else "       "}')
        # Interrupt latency: within the window, the largest charge the loop
        # makes, and the delivery's.
        lat = [cycles * hz / clock_hz for cycles in latencies(result)]
        bound = WINDOW + max(charge(table, preset, op) for op in IRQ_LOOP) + sum(
            charge(table, preset, op) for op in DELIVERY)
        entry['latency'] = {'count': len(lat), 'max_clocks': round(max(lat), 1) if lat else None,
                            'mean_clocks': round(sum(lat) / len(lat), 1) if lat else None, 'bound_clocks': bound,
                            'debt_max': result['run']['debt_max']}
        if not lat:
            failures.append(f'{preset.name}: no interrupt was raised')
        elif max(lat) > bound:
            failures.append(f'{preset.name}: interrupt latency {max(lat):.1f} reference clocks over the bound {bound}')
        report['presets'][preset.name] = entry
    for name in rows:
        print('  '.join(rows[name]))
    for preset_id in chosen:
        lat = report['presets'][presets[preset_id].name]['latency']
        print(f'{presets[preset_id].name}: {lat["count"]} interrupts, latency max {lat["max_clocks"]} mean '
              f'{lat["mean_clocks"]} reference clocks (bound {lat["bound_clocks"]}), debt max {lat["debt_max"]}')
    # Word 1: the frequency override paces every loop at its frequency.
    preset = presets[OVERRIDE[0]]
    hz = OVERRIDE[1] * 100_000
    entry = {'preset': preset.name, 'frequency': OVERRIDE[1], 'loops': []}
    for span, (name, parts) in zip(spans(override), BENCHES):
        want = expected(table, preset, parts)
        error = span['cycles'] / (want * clock_hz / hz) - 1
        entry['loops'].append({'name': name, 'cycles': span['cycles'], 'charged': span['charged'], 'error': round(error, 5)})
        if span['charged'] != want or abs(error) > BUDGET:
            failures.append(f'{preset.name} at {hz / 1e6:g} MHz {name}: {span["cycles"]} cycles ({error:+.2%}), '
                            f'charged {span["charged"]} of {want}')
    report['override'] = entry
    worst = max(abs(loop['error']) for loop in entry['loops'])
    print(f'{preset.name} at {hz / 1e6:g} MHz (word 1): every loop within {worst:.2%} of the table')
    # Word 3: turbo at launch is max.
    same = [a['cycles'] == b['cycles'] and a['charged'] == 0 for a, b in zip(spans(turbo), base)]
    report['turbo'] = {'preset': preset.name, 'loops_as_max': sum(same), 'loops': len(same)}
    if not all(same) or turbo['state'] != results[0]['state']:
        failures.append(f'{preset.name} with turbo: {sum(same)} of {len(same)} loops ran as at max')
    print(f'{preset.name} with turbo (word 3): {sum(same)} of {len(same)} loops in max\'s cycles, nothing charged')
    report['failures'] = failures
    (OUTPUT / ('report-netlist.json' if yosys else 'report.json')).write_text(json.dumps(report, indent=2) + '\n')
    for failure in failures:
        print('FAIL', failure)
    print(('FAIL' if failures else 'PASS') + ' i386 governor (host simulation only; report: '
          f'{OUTPUT / ("report-netlist.json" if yosys else "report.json")})')
    return 1 if failures else 0


if __name__ == '__main__':
    sys.exit(main())
