#!/usr/bin/env python3
"""FreeDOS to the prompt on the DE25 pc386 machine, through the board's window.

system_tb (cores/fes-pc386/sim) on pc386_de25_sim: the derived pc386_system
(derive.py: no display plane), the model --model names as the producer
builds its CPU: the 486SX (I486, the default) or the i486DX (I486,
X87_PRESENT and i486dx/derive.py's CPU),
behind fes_de25_hps_window's guards, rebase and bounds and
fes_de25_window_arbiter, built with FES_PC386_DE25. The suite, its
checks and its report are sim_fes_pc386.py's system suite unchanged; only the
testbench is this board's. Host-only: no kit, package or routed result.

    python3 boards/de25nano/pc386/sim.py [--download] [--jobs N]
"""
from pathlib import Path
import argparse
import importlib
import importlib.util
import json
import os
import re
import sys

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(HERE))
from scripts import sim_fes_pc386 as sim  # noqa: E402
import derive  # noqa: E402

# pc386/build.py's I486, read from its source so the bench needs none of the producer's imports.
I486 = int(re.search(r'^I486 = (\d)$', (HERE / 'build.py').read_text(), re.M).group(1))
i486dx = importlib.import_module('i486dx.derive')
MODELS = ('486sx', '486dx')

DE25_RTL = (HERE / 'pc386_dma2_registers.sv', HERE / 'fes_de25_hps_window.v', HERE / 'fes_de25_window_arbiter.v', HERE / 'fes_de25_window_unpack.v',
            ROOT / 'cores/fes-common/rtl/fes_hps_ddr_guard.v', HERE / 'sim/pc386_de25_sim.sv')


def derived_sources(output: Path, model: str = MODELS[0]) -> dict[Path, Path]:
    """The three DE10 files derive.py changes, written for this build; for the 486DX, its pc386_system's CPU
    retargeted, its presenter re-armed, and the i486DX CPU sources in place of the integer core's."""
    output.mkdir(parents=True, exist_ok=True)
    rtl = ROOT / 'cores/fes-pc386/rtl'
    result = {}
    for name, step in (('pc386_machine.sv', derive.retarget_machine), ('pc386_system.sv', derive.retarget_system),
                       ('pc386_at_core.sv', derive.retarget_at_core)):
        data, _ = step((rtl / name).read_bytes())
        if model == '486dx' and name == 'pc386_system.sv':
            data, _ = derive.retarget_system_cpu(data)
        (output / name).write_bytes(data)
        result[rtl / name] = output / name
    if model == '486dx':
        presenter = ROOT / derive.PRESENTER_UPSTREAM
        guarded, _ = derive.retarget_presenter_loaded(presenter.read_bytes())
        result[presenter] = output / presenter.name
        result[presenter].write_bytes(guarded)
        for old, new in zip(sim.CPU_RTL, i486dx.sim_sources(output.parent / 'i486dx', ROOT, rename=True)):
            if old != new:
                result[old] = new
    return result


def system_testbench(args) -> Path:
    """system_tb on pc386_de25_sim, at the model's own DE25 rates, as sim.system_testbench builds pc386_system."""
    # The producer's own module, loaded here and not at import: it brings the board and the toolchain with it.
    spec = importlib.util.spec_from_file_location('de25_pc386_build', HERE / 'build.py')
    build = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(build)
    images = sim.firmware.images(args.download, args.jobs)
    rom = sim.firmware.write_rom_hex(images)
    # The model's parameters as the producer elaborates them (build.py machine_parameters: its guest rates at
    # its own machine clock, the CPU identity, and for the 486DX the 387), except that the top declares I486
    # as a bit; pc386_de25_sim passes them to pc386_system (tests/test_de25_i486dx.py elaborates them).
    # The 486SX's X87_PRESENT is pc386_system's own 0, which the producer leaves unset; the bench's top
    # defaults to 1, so the bench sets it for both models.
    parameters = {**build.machine_parameters(args.model), 'ROM_HEX': f'"{rom}"', 'I486': f"1'b{I486}",
                  'X87_PRESENT': 1 if args.model == '486dx' else 0}
    # Under rtl/, so the bench's .vlt waivers for those files apply as they do on the DE10.
    swap = derived_sources(sim.OUTPUT / 'derived' / 'rtl', args.model)
    missing = set(swap) - set(sim.SYSTEM_RTL)
    if missing:
        raise sim.SimError(f'derived files are not in the system RTL: {sorted(map(str, missing))}')
    rtl = tuple(swap.get(path, path) for path in sim.SYSTEM_RTL) + DE25_RTL
    sim.verilate(args.verilator, 'system', 'pc386_de25_sim', rtl, sim.CORE / 'sim/system_tb.cpp', [], args.jobs,
                 parameters, include=(HERE / 'generated',),
                 flags=('-Wno-TIMESCALEMOD', '--prefix', 'Vpc386_system', '-CFLAGS', '-DFES_PC386_DE25'),
                 defines=('FES_PC386_DE25',))
    return sim.OUTPUT / 'system' / 'obj' / 'Vpc386_system'


def run_at_model_clock(args) -> dict:
    """sim.run_system with the model's machine clock in place of the DE10's. run_system passes
    pc386_quartus.SYSTEM_CLOCK_HZ (61.44 MHz) to system_tb as --clock-hz, which system_tb takes for the
    system/pixel clock ratio, its timeouts and its reported seconds; at the 486SX's 92.16 MHz that is wrong
    by 1.5x. The shared module is DE10 evidence and stays as it is, so the clock is substituted here for the
    run and restored."""
    from scripts import pc386_quartus as producer
    spec = importlib.util.spec_from_file_location('de25_pc386_build_clock', HERE / 'build.py')
    build = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(build)
    shared = producer.SYSTEM_CLOCK_HZ
    producer.SYSTEM_CLOCK_HZ = build.model_clock(args.model)['hz']
    try:
        return sim.run_system(args)
    finally:
        producer.SYSTEM_CLOCK_HZ = shared


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--model', choices=MODELS, default=MODELS[0], help='the producer model to build (default 486sx)')
    parser.add_argument('--download', action='store_true')
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--limit-seconds', type=float, default=None)
    args = parser.parse_args()
    args.limit_seconds = args.limit_seconds or sim.default_limit_seconds('system')
    args.rebuild_image, args.preset, args.frequency, args.turbo = False, 0, 0, 0
    sim.OUTPUT = ROOT / ('build/sim/de25-pc386' if args.model == MODELS[0] else 'build/sim/de25-pc386-' + args.model)
    sim.system_testbench = system_testbench
    try:
        report = run_at_model_clock(args)
    except (sim.SimError, OSError) as error:
        print(f'FAIL de25 system: {error}', file=sys.stderr)
        return 1
    (sim.OUTPUT / 'system').mkdir(parents=True, exist_ok=True)
    (sim.OUTPUT / 'system' / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    for row in report['screen']:
        print('|' + row + '|')
    print('STATUS', report['status'])
    for check in report['record_checks']:
        print('CHECK', 'PASS' if check['pass'] else 'FAIL', check['check'])
    print('END', report['end'])
    for failure in report['failures']:
        print('FAILURE', failure)
    print(('FAIL' if report['failures'] else 'PASS') + ' de25 system')
    return 1 if report['failures'] else 0


if __name__ == '__main__':
    sys.exit(main())
