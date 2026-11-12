#!/usr/bin/env python3
"""Fit the x87 spike (#1768 slice 1) with the locked Quartus: diagnostic only.

The spike (cores/fes-common/sim/i386/spike/spike_x87.sv) is the lean 387
datapath the x87 plan describes, in variants by parameter (VARIANTS). This
script measures each variant's area and timing; it builds no package, writes
no RBF it keeps, and changes no source.

  standalone  spike_x87 in the spike envelope at the 61.44 MHz CPU clock:
              the pc386 lock's Yosys writes the netlist with the seal's
              program (scripts/yosys_netlist.py SEAL), and Quartus maps,
              fits and analyses it with the seal's settings (default
              optimisation, DSP blocks allowed) and the given seed.
  machine     pc386_system as the seal builds it, through the sealer's own
              project, firmware, guest rates and Yosys program, with
              x87s_port on the CPU's I/O bus (ports F8h-FFh). The overlay
              is a copy of pc386_system.sv written into the output
              directory; the checkout is not changed. --variant base fits
              the machine unchanged: the same-flow baseline every variant
              is compared with. The timing is read from TimeQuest's report
              and not gated, so a variant that misses still reports.

--hierarchy drops Yosys's flatten so the fit report lists ALMs per entity
(the area audit's method); the default keeps the seal's flatten.

Reports, at every corner: each clock's worst setup and hold slack (the
multicorner summary), the Fitter Summary's resources, and the slow-corner
critical paths in full (overall, and for the machine those into and out of
the x87). Writes <output>/summary.json and keeps the fit and timing reports;
--keep keeps Quartus's databases too.

  QUARTUS_ROOTDIR=/abs/<the Quartus install quartus.lock names>/quartus \\
    python3 scripts/quartus_x87_spike.py --cache-root /abs/toolchains --gpu-router CUDA \\
      --mode machine --variant a --seed 1 --output /abs/scratch/x87s1/machine-a-s1
"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_pc386_quartus as sealer
from scripts import yosys_netlist
from scripts.quartus_tools import load_quartus_lock, locate_quartus

ROOT = Path(__file__).resolve().parents[1]
SPIKE = Path('cores/fes-common/sim/i386/spike')
SPIKE_SOURCES = (SPIKE / 'spike_stimulus.sv', SPIKE / 'spike_x87.sv')
SYSTEM = Path('cores/fes-pc386/rtl/pc386_system.sv')
TOP = 'spike_x87'
CPU_MHZ = sealer.SYSTEM_CLOCK_HZ / 1e6
# The variants the decision needs (docs/validation/2026-11-12-i386-x87-fit-spike).
VARIANTS = {
    # The plan's option A: everything in the fabric; shifts, divide and
    # square root on the nine-DSP multiplier; transcendentals in microcode.
    'a': {'MUL': 0, 'SHIFT': 0, 'DIVSQRT': 1, 'TRANS': 1},
    # A with a radix-2 divide and square root unit beside the multiplier.
    'a-r2': {'MUL': 0, 'SHIFT': 0, 'DIVSQRT': 2, 'TRANS': 1},
    # A with LUT barrel shifters, as the design's own budget row has them.
    'a-lut': {'MUL': 0, 'SHIFT': 1, 'DIVSQRT': 1, 'TRANS': 1},
    # Narrower multipliers: three DSPs in three passes, one in nine. With
    # a slow multiplier the shifts are LUTs and divide is radix-2.
    'a-mul3': {'MUL': 1, 'SHIFT': 1, 'DIVSQRT': 2, 'TRANS': 1},
    'a-mul1': {'MUL': 2, 'SHIFT': 1, 'DIVSQRT': 2, 'TRANS': 1},
    # The plan's option D: divide and square root in the fabric,
    # transcendentals, FPREM and BCD on the HPS.
    'd': {'MUL': 0, 'SHIFT': 0, 'DIVSQRT': 1, 'TRANS': 0},
    # The lean fabric core: divide, square root and transcendentals on the HPS.
    'd-hps': {'MUL': 0, 'SHIFT': 0, 'DIVSQRT': 0, 'TRANS': 0},
}
BASE = 'base'
SYSTEM_CLOCK = sealer.CLOCKS[0][1]

# Where the overlay goes in pc386_system.sv: after the CPU's I/O wires, and in its IN data.
IO_RESULT = ('                io_result <= cpu_io_size == 2\'d0 ? {24\'d0, io_rdata[7:0]} :\n'
             '                             cpu_io_size == 2\'d1 ? {16\'d0, io_rdata[15:0]} : io_rdata;\n')
IO_WIRES = '    wire [31:0] cpu_io_wdata;\n'

PATHS_TCL = r'''
project_open {top} -revision {top}
create_timing_netlist
read_sdc
update_timing_netlist
set out [open "{out}" w]
proc dump {{out label paths}} {{
    set rank 0
    foreach_in_collection path $paths {{
        incr rank
        puts $out "path\t$label\t$rank\t[get_path_info $path -slack]\t[get_path_info $path -num_logic_levels]\t[get_path_info $path -data_delay]\t[get_node_info -name [get_path_info $path -from]]\t[get_node_info -name [get_path_info $path -to]]"
        foreach_in_collection point [get_path_info $path -arrival_points] {{
            set node [get_point_info $point -node]
            set name [expr {{$node eq "" ? "" : [get_node_info -name $node]}}]
            puts $out "point\t$label\t$rank\t[get_point_info $point -type]\t[get_point_info $point -incr]\t$name"
        }}
    }}
}}
dump $out worst [get_timing_paths -setup -npaths 3 -to_clock [get_clocks {{{clock}}}]]
{x87_paths}
close $out
delete_timing_netlist
project_close
'''
X87_PATHS = '''if {[catch {
    dump $out into_x87 [get_timing_paths -setup -npaths 3 -to [get_registers {*x87_*}]]
    dump $out from_x87 [get_timing_paths -setup -npaths 3 -from [get_registers {*x87_*}]]
} message]} { puts $out "error\t$message" }'''

MULTICORNER_ROW = re.compile(r'^;[ \t]+([^;\n]+?)[ \t]+;[ \t]+(\S+)[ \t]+;[ \t]+(\S+)[ \t]+;[^\n]*$', re.M)
ENTITY_HEADER = '; Compilation Hierarchy Node'


class SpikeError(RuntimeError):
    pass


def overlay_system(text: str, params: dict[str, int]) -> str:
    """pc386_system.sv with x87s_port on the CPU's I/O bus. Its IN data for
    ports F8h-FFh comes from the unit; the machine still runs the cycle."""
    for anchor in (IO_RESULT, IO_WIRES):
        if text.count(anchor) != 1:
            raise SpikeError(f'pc386_system.sv no longer has exactly one {anchor.strip()[:40]!r}')
    settings = ', '.join(f'.{name}({value})' for name, value in params.items())
    text = text.replace(IO_RESULT, IO_RESULT.replace('io_rdata', 'x87_io_rdata'))
    port = (
        '\n    // The x87 fit spike (scripts/quartus_x87_spike.py), a diagnostic overlay.\n'
        '    wire x87_hit;\n'
        '    wire [31:0] x87_rdata;\n'
        '    wire [31:0] x87_io_rdata = x87_hit ? x87_rdata : io_rdata;\n'
        f'    x87s_port #({settings}) x87 (\n'
        '        .clk(clk), .reset(cpu_reset), .io_valid(io_valid), .io_write(cpu_io_write), .io_port(cpu_io_port),\n'
        '        .io_wdata(cpu_io_wdata), .hit(x87_hit), .rdata(x87_rdata)\n'
        '    );\n')
    return text.replace(IO_WIRES, IO_WIRES + port)


def multicorner(sta_text: str) -> dict[str, dict[str, float | None]]:
    """Each clock's worst setup and hold slack over every corner, from the
    Multicorner Timing Analysis Summary."""
    start = sta_text.find('; Multicorner Timing Analysis Summary')
    if start < 0:
        raise SpikeError('timing report lacks the multicorner summary')
    block = sta_text[start:sta_text.find('; Design-wide TNS', start)]
    clocks = {}
    for name, setup, hold in MULTICORNER_ROW.findall(block):
        if name == 'Clock':
            continue
        clocks['worst' if name == 'Worst-case Slack' else name] = {
            'setup_ns': None if setup == 'N/A' else float(setup), 'hold_ns': None if hold == 'N/A' else float(hold)}
    return clocks


def resources(fit_text: str) -> dict:
    found = sealer.fit_resources(fit_text)
    alms = found['alms']
    alms['percent'] = round(100.0 * alms['used'] / alms['available'], 2)
    return found


def entities(fit_text: str, depth: int = 3) -> list[dict]:
    """The Fitter's Resource Utilization by Entity rows down to depth, and
    every x87s_ row: ALMs needed, registers, M10Ks and DSP blocks, with each
    entity's own share."""
    start = fit_text.find(ENTITY_HEADER)
    if start < 0:
        return []
    header = [cell.strip() for cell in fit_text[start:fit_text.find('\n', start)].strip(';').split(';')]
    column = {name: header.index(name) for name in header}
    rows = []
    for line in fit_text[start:].splitlines()[2:]:
        if line.startswith('+'):
            if rows:
                break
            continue
        cells = line.strip().strip(';').split(';')
        node = cells[0].rstrip()
        level = (len(node) - len(node.lstrip())) // 3
        if level >= depth and 'x87s_' not in node:
            continue

        def figure(name: str, cells=cells):
            value = cells[column[name]].strip()
            match = re.match(r'([0-9.]+)(?: \(([0-9.]+)\))?', value)
            return (float(match.group(1)), float(match.group(2)) if match.group(2) else None) if match else (None, None)
        alms, own = figure('ALMs needed [=A-B+C]')
        registers, _ = figure('Dedicated Logic Registers')
        m10k, _ = figure('M10Ks')
        dsp, _ = figure([name for name in header if name.startswith('DSP')][0])
        rows.append({'entity': node.strip(), 'level': level, 'alms': alms, 'own_alms': own,
                     'registers': registers, 'm10k': m10k, 'dsp': dsp})
    return rows


def paths(text: str) -> dict[str, list[dict]]:
    found: dict[str, dict[int, dict]] = {}
    for line in text.splitlines():
        fields = line.split('\t')
        if fields[0] == 'path':
            _, label, rank, slack, levels, delay, source, sink = fields
            found.setdefault(label, {})[int(rank)] = {
                'slack_ns': float(slack), 'logic_levels': int(levels), 'data_delay_ns': float(delay),
                'from': source, 'to': sink, 'cells': []}
        elif fields[0] == 'point' and fields[3] == 'cell':
            found[fields[1]][int(fields[2])]['cells'].append(fields[5])
        elif fields[0] == 'error':
            found.setdefault('error', {})[0] = {'message': '\t'.join(fields[1:])}
    return {label: [ranks[rank] for rank in sorted(ranks)] for label, ranks in found.items()}


def run(command: list[str], cwd: Path, log: Path, env: dict) -> None:
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT, check=False)
    if result.returncode:
        raise SpikeError(f'{Path(command[0]).name} failed (exit {result.returncode}); see {log}')


def flatten(program: str, hierarchy: bool) -> str:
    if not hierarchy:
        return program
    if program.count('flatten; ') != 1:
        raise SpikeError('the Yosys program no longer flattens exactly once')
    return program.replace('flatten; ', '')


PARAMOD = re.compile(r'\\\$paramod(?:\$[0-9a-f]+)?\\([A-Za-z_]\w*)\S*')


def plain_module_names(netlist: Path) -> None:
    """Quartus 17.0.2 can't find the ROM init file of an entity named $paramod$...; rename those."""
    text = netlist.read_text()
    netlist.write_text(PARAMOD.sub(
        lambda match: f'{match.group(1)}_{hashlib.sha1(match.group(0).encode()).hexdigest()[:8]}', text))


def standalone(args, yosys, bin_dir: Path, env: dict, output: Path) -> tuple[Path, str]:
    project = output / 'project'
    project.mkdir(parents=True)
    sources = [ROOT / path for path in SPIKE_SOURCES]
    yosys_netlist.run(yosys.path, flatten(yosys_netlist.program(
        yosys_netlist.SEAL, top=TOP, sources=sources, includes=[ROOT / p for p in yosys_netlist.PC386_INCLUDES],
        parameters=VARIANTS[args.variant], netlist=f'{TOP}.v'), args.hierarchy),
        cwd=project, program_file=project / f'{TOP}.ys', log=output / 'yosys.log')
    if args.hierarchy:
        plain_module_names(project / f'{TOP}.v')
    pins = (ROOT / SPIKE / 'spike.qsf').read_text()
    (project / f'{TOP}.qpf').write_text(f'PROJECT_REVISION = "{TOP}"\n')
    # The seal's settings (build_fes_pc386_quartus.project_qsf) with this top.
    (project / f'{TOP}.qsf').write_text(
        'set_global_assignment -name FAMILY "Cyclone V"\n'
        f'set_global_assignment -name DEVICE {sealer.board.TARGET}\n'
        f'set_global_assignment -name TOP_LEVEL_ENTITY {TOP}\n'
        'set_global_assignment -name PROJECT_OUTPUT_DIRECTORY "output_files"\n'
        'set_global_assignment -name NUM_PARALLEL_PROCESSORS ALL\n'
        f'set_global_assignment -name SEED {args.seed}\n'
        'set_global_assignment -name VERILOG_INPUT_VERSION SYSTEMVERILOG_2005\n'
        f'set_global_assignment -name VERILOG_FILE {TOP}.v\n'
        f'set_global_assignment -name SDC_FILE {TOP}.sdc\n' + pins)
    (project / f'{TOP}.sdc').write_text(
        f'create_clock -name clk -period {1000.0 / CPU_MHZ:.3f} [get_ports {{FPGA_CLK1_50}}]\n'
        'derive_clock_uncertainty\n')
    for tool in ('quartus_map', 'quartus_fit', 'quartus_sta'):
        run([str(bin_dir / tool), TOP], project, output / f'{tool}.log', env)
    return project, TOP


def machine(args, tools: dict, output: Path) -> tuple[Path, str]:
    project = sealer.write_project(ROOT, output, '0' * 32, args.seed)
    sealer.firmware_rom(ROOT)
    sources = list(sealer.MACHINE_SOURCES)
    if args.variant != BASE:
        overlay = output / 'overlay' / SYSTEM.name
        overlay.parent.mkdir()
        overlay.write_text(overlay_system((ROOT / SYSTEM).read_text(), VARIANTS[args.variant]))
        sources = [str(overlay) if source == SYSTEM.as_posix() else source for source in sources]
        if str(overlay) not in sources:
            raise SpikeError(f'{SYSTEM} is not among the sealer\'s machine sources')
        sources.append((SPIKE / 'spike_x87.sv').as_posix())
    netlist = (project / sealer.MACHINE_NETLIST).relative_to(ROOT) if project.is_relative_to(ROOT) \
        else project / sealer.MACHINE_NETLIST
    program = yosys_netlist.program(yosys_netlist.SEAL, top=sealer.MACHINE_TOP, sources=sources,
                                    includes=sealer.INCLUDE_DIRS, parameters=sealer.guest_rates(),
                                    netlist=Path(netlist).as_posix())
    yosys_netlist.run(tools['yosys'].path, flatten(program, args.hierarchy), cwd=ROOT,
                      program_file=output / 'machine.ys', log=output / 'yosys.log')
    if args.hierarchy:
        plain_module_names(project / sealer.MACHINE_NETLIST)
    sealer.run_quartus(tools['quartus_sh'], tools['quartus_root'], project, output / 'quartus.log')
    return project, sealer.board.TOP


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--cache-root', type=Path, default=os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or None,
                        help="the shared toolchain cache (absolute) holding the pc386 lock's slot")
    parser.add_argument('--gpu-router', choices=yosys_netlist.GPU_ROUTERS, default='HIP',
                        help='the pc386 lock slot whose Yosys writes the netlist (nothing runs on a GPU)')
    parser.add_argument('--mode', choices=('standalone', 'machine'), required=True)
    parser.add_argument('--variant', choices=(*VARIANTS, BASE), required=True)
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--hierarchy', action='store_true', help='keep the hierarchy for per-entity ALMs')
    parser.add_argument('--output', type=Path, required=True, help='a new directory (absolute)')
    parser.add_argument('--keep', action='store_true', help="keep Quartus's databases")
    args = parser.parse_args()
    try:
        if args.cache_root is None or not args.cache_root.is_absolute():
            raise ValueError('--cache-root must be absolute (or set FES_TOOLCHAIN_CACHE_ROOT)')
        if not args.output.is_absolute() or args.output.exists():
            raise ValueError('--output must be a new absolute directory')
        if args.mode == 'standalone' and args.variant == BASE:
            raise ValueError('the base variant is the machine without the spike')
        sealer._check_seed(args.seed)
        output = args.output
        output.mkdir(parents=True)
        if args.mode == 'machine':
            tools = sealer._authenticate_tools(ROOT, cache_root=args.cache_root, gpu_router=args.gpu_router)
            quartus_root, version, yosys = tools['quartus_root'], tools['quartus_version'], tools['yosys']
            project, revision = machine(args, tools, output)
            clock = SYSTEM_CLOCK
        else:
            yosys = yosys_netlist.authenticate(ROOT, cache_root=args.cache_root, gpu_router=args.gpu_router,
                                                lock=sealer.LOCK)
            lock = load_quartus_lock(ROOT, sealer.QUARTUS_LOCK)
            quartus_root, quartus_sh, version = locate_quartus(os.environ.get('QUARTUS_ROOTDIR', ''), ROOT, lock)
            env = dict(os.environ, QUARTUS_ROOTDIR=str(quartus_root))
            project, revision = standalone(args, yosys, quartus_sh.parent, env, output)
            clock = 'clk'
        bin_dir = Path(quartus_root) / 'bin'
        env = dict(os.environ, QUARTUS_ROOTDIR=str(quartus_root))
        built = project / 'output_files'
        fit_text = (built / f'{revision}.fit.rpt').read_text(errors='replace')
        sta_text = (built / f'{revision}.sta.rpt').read_text(errors='replace')
        (project / 'paths.tcl').write_text(PATHS_TCL.format(
            top=revision, out='paths.tsv', clock=clock, x87_paths=X87_PATHS if args.mode == 'machine' else ''))
        run([str(bin_dir / 'quartus_sta'), '-t', 'paths.tcl'], project, output / 'paths.log', env)
        timing = multicorner(sta_text)
        summary = {
            'classification': 'x87 fit spike (#1768 slice 1): diagnostic only, not a sealed labelled package',
            'created_utc': datetime.now(timezone.utc).isoformat(), 'quartus': version, 'mode': args.mode,
            'variant': args.variant, 'parameters': VARIANTS.get(args.variant, {}), 'seed': args.seed,
            'hierarchy': args.hierarchy, 'gpu_router': args.gpu_router, 'device': sealer.board.TARGET,
            'yosys': yosys_netlist.lock_record(ROOT, yosys, args.gpu_router, sealer.LOCK),
            'netlist': yosys_netlist.canonical(yosys_netlist.SEAL),
            'cpu_clock': {'name': clock, 'mhz': CPU_MHZ, **timing.get(clock, {})},
            'timing': timing, 'resources': resources(fit_text), 'entities': entities(fit_text),
            'paths': paths((project / 'paths.tsv').read_text()),
            'sources': {path.as_posix(): hashlib.sha256((ROOT / path).read_bytes()).hexdigest()
                        for path in (*SPIKE_SOURCES, SYSTEM)},
        }
        for name in (f'{revision}.fit.rpt', f'{revision}.sta.rpt', f'{revision}.map.rpt', 'paths.tsv'):
            source = built / name if name != 'paths.tsv' else project / name
            if source.is_file():
                shutil.copyfile(source, output / name)
        (output / 'summary.json').write_text(json.dumps(summary, indent=2, sort_keys=True) + '\n')
        if not args.keep:
            shutil.rmtree(project)
        cpu = summary['cpu_clock']
        print(f'{args.mode} {args.variant} seed {args.seed}: {summary["resources"]["alms"]["used"]} ALMs, '
              f'{summary["resources"]["dsp_blocks"]["used"]} DSP, {summary["resources"]["ram_blocks"]["used"]} M10K, '
              f'setup {cpu.get("setup_ns")} ns, hold {cpu.get("hold_ns")} ns', flush=True)
        return 0
    except (OSError, RuntimeError, ValueError, sealer.board.BuildError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
