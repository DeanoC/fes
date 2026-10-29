#!/usr/bin/env python3
"""Route the i386 timing spikes through Quartus 17.0.2 for oracle evidence.

This script emits diagnostic timing evidence; it does not create a sealed,
labelled Quartus package. It answers whether the OSS flow's timing on
fes_i386_cpu (Yosys and nextpnr, see scripts/benchmark_fes_i386.py) is the
RTL's or the flow's. The whole-CPU spike is the question; the isolated spikes
(m2, dag, frontend) calibrate the two flows' timing on logic that is the same
on both sides.

Quartus 17.0.2's SystemVerilog front end rejects the i386 RTL (generate loops
must declare their genvar outside the loop and sit in generate/endgenerate,
and it misreads a variable index into a packed struct member that shares a
name with a module signal). So Quartus reads the netlist the i386 lock's
Yosys elaborates from the RTL: `read_verilog -sv; hierarchy; proc -norom;
flatten; opt -purge; write_verilog`. That is the netlist the lockstep checks
with --yosys, it is not technology-mapped, and Quartus does all synthesis
(LUT mapping, carry chains) and all placement and routing.

The oracle is Quartus at its best. The elaborated netlist has two kinds of
multiply: products of two signals (fes_i386_muldiv's 18x18s) and constant
multiplies, the packed-array strides the netlist still spells out (start*42
for dec[start]). The fastest mapping found puts the products in DSP blocks
and the constant multiplies in logic. Left to choose, Quartus also puts the
widest stride in a DSP block, and Q's worst path runs through it; with every
multiply in logic, the soft products limit EX. So Quartus's
DSP_BLOCK_BALANCING is logic elements, and each product of two signals is
marked (* multstyle = "dsp" *), which Quartus takes over that setting. The
marks are attributes only, so the logic is still the lockstep's netlist.

Each seed fits the same synthesis. TimeQuest reports, at the slow corner
(1100 mV, 100 C):
  - the worst setup path to every register, labelled by pipeline stage with
    benchmark_fes_i386's labels for the spike, with its logic levels and how
    much of its data path is interconnect;
  - the top critical paths in full;
and once, on the post-map netlist (no placement), the logic levels to every
register: Quartus's synthesis-only depth, to set beside Yosys's.

Example:
  QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus \\
    python3 scripts/quartus_fes_i386.py --yosys /abs/path/to/i386-locked/yosys

Writes build/quartus/fes-i386/<fingerprint>/summary.json.
"""
from __future__ import annotations

import argparse
import collections
from concurrent.futures import ThreadPoolExecutor
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

from scripts import benchmark_fes_i386 as benchmark
from scripts.quartus_tools import locate_quartus, quartus_version_line

ROOT = Path(__file__).resolve().parents[1]
DEVICE = '5CSEBA6U23I7'
NETLIST = 'read_verilog -sv {files}; hierarchy -top {top}; proc -norom; flatten; opt -purge; write_verilog -noattr {out}'
PATHS = 20000  # endpoints TimeQuest reports, one worst path each

# One worst path per register endpoint, plus the top paths in full. Fields
# are tab-separated; node names keep Quartus's spelling.
STA_TCL = r'''
project_open {top}
create_timing_netlist {post_map}
read_sdc
update_timing_netlist
set out [open "{out}" w]
puts $out "fmax\t[join [get_clock_fmax_info] \t]"
foreach_in_collection path [get_timing_paths -setup -npaths {paths} -nworst 1] {{
    set ic 0.0
    set cell 0.0
    foreach_in_collection point [get_path_info $path -arrival_points] {{
        set incr [get_point_info $point -incr]
        set type [get_point_info $point -type]
        if {{$type eq "ic"}} {{
            set ic [expr {{$ic + $incr}}]
        }} elseif {{$type ne "launch" && $type ne "clknet"}} {{
            set cell [expr {{$cell + $incr}}]
        }}
    }}
    puts $out "path\t[get_node_info -name [get_path_info $path -from]]\t[get_node_info -name [get_path_info $path -to]]\t[get_path_info $path -slack]\t[get_path_info $path -num_logic_levels]\t$ic\t$cell"
}}
set rank 0
foreach_in_collection path [get_timing_paths -setup -npaths 3] {{
    incr rank
    foreach_in_collection point [get_path_info $path -arrival_points] {{
        set node [get_point_info $point -node]
        set name [expr {{$node eq "" ? "" : [get_node_info -name $node]}}]
        puts $out "point\t$rank\t[get_point_info $point -type]\t[get_point_info $point -incr]\t$name"
    }}
}}
close $out
delete_timing_netlist
project_close
'''


class QuartusError(RuntimeError):
    pass


_MULTIPLY = re.compile(r'^(\s*assign\s+.+?\s*=\s*)(.+?) \* (.+);$', re.M)
_CONSTANT = re.compile(r"(?:\$(?:un)?signed\()?\d+(?:'s?[bodh][\da-fA-FxXzZ_?]+)?\)?")


def mark_products(netlist: str) -> tuple[str, list[str]]:
    """Mark each multiply of two signals (* multstyle = "dsp" *), for a DSP
    block, and leave each multiply by a constant unmarked, for logic. Returns
    the netlist and the marked assigns."""
    marked = []

    def mark(match):
        prefix, left, right = match.groups()
        if any(_CONSTANT.fullmatch(operand.strip()) for operand in (left, right)):
            return match.group(0)
        marked.append(match.group(0).strip())
        return f'{prefix}{left} * (* multstyle = "dsp" *) {right};'

    return _MULTIPLY.sub(mark, netlist), marked


_DECLARATION = re.compile(r'^\s*(?:wire|reg|input|output)\s+(?:signed\s+)?(?:\[(\d+):(\d+)\]\s+)?(\\\S+|\w+)\s*;')
_ASSIGN = re.compile(r'^\s*assign\s+(?:\{(.*)\}|(\S+(?:\s*\[[^\]]*\])?))\s*=\s*(_\d+_)\s*;')
_PART = re.compile(r'(\\\S+|\w+)(?:\s*\[(\d+)(?::(\d+))?\])?')


def register_names(netlist: str) -> dict[tuple[str, int], str]:
    """Yosys's write_verilog names a register that drives slices of several
    signals _NNNN_ and assigns it to them; map each of its bits to a signal."""
    widths, names = {}, {}
    for line in netlist.splitlines():
        match = _DECLARATION.match(line)
        if match:
            hi, lo, name = match.groups()
            widths[name.lstrip('\\')] = int(hi) - int(lo) + 1 if hi is not None else 1
    for line in netlist.splitlines():
        match = _ASSIGN.match(line)
        if not match:
            continue
        bit = 0
        target = match.group(1) if match.group(1) is not None else match.group(2)
        for part in reversed([item.strip() for item in target.split(',')]):
            found = _PART.fullmatch(part)
            if not found:
                break
            name, hi, lo = found.group(1).lstrip('\\'), found.group(2), found.group(3)
            width = 1 if hi is not None and lo is None else int(hi) - int(lo) + 1 if hi is not None else widths.get(name, 1)
            for _ in range(width):
                names[(match.group(3), bit)] = name
                bit += 1
    # Bits that drive a function's result variable ($-named) take the name
    # most of their register's other bits drive; so does the whole register
    # (bit -1), for the RAM-based shift registers Quartus infers from it.
    common = {}
    for (register, _), name in names.items():
        if '$' not in name:
            common.setdefault(register, collections.Counter())[name] += 1
    for key, name in list(names.items()):
        if '$' in name and key[0] in common:
            names[key] = names[(name, -1)] = common[key[0]].most_common(1)[0][0]
    for register, counts in common.items():
        names[(register, -1)] = counts.most_common(1)[0][0]
    return names


def node_label(name: str, stages, names: dict | None = None) -> str:
    """A Quartus register name, as the benchmark's 'cpu' labels read it."""
    # A RAM or RAM-based shift register Quartus inferred from a register.
    ram = re.match(r'(?:altshift_taps|altsyncram):(\\?[\w.$]+?)_rtl_\d+\|', name)
    if ram:
        name = (names or {}).get((ram.group(1), -1), ram.group(1))
    name = re.sub(r'~.*$', '', name).split('|')[-1].strip('\\ ')
    anonymous = re.fullmatch(r'(_\d+_)\[(\d+)\]', name)
    if anonymous and names:
        key = (anonymous.group(1), int(anonymous.group(2)))
        name = names.get(key, names.get((key[0], -1), name))
    name = re.sub(r'_reg$', '', re.sub(r'\[[^\]]*\]', '', name))
    if names and (name, -1) in names:
        name = names[(name, -1)]
    return next((label for label, pattern in stages if re.match(pattern, name)), 'envelope')


def parse_sta(text: str, target_mhz: float, stages, names: dict | None = None) -> dict:
    period = 1000.0 / target_mhz
    fmax, worst, levels, points = None, {}, {}, {}
    for line in text.splitlines():
        fields = line.split('\t')
        if fields[0] == 'fmax' and len(fields) > 1 and fields[1]:
            # {clock fmax restricted_fmax}
            values = fields[1].split()
            fmax = float(values[2]) if len(values) > 2 else None
        elif fields[0] == 'path':
            _, source, sink, slack, logic_levels, ic, cell = fields
            label = node_label(sink, stages, names)
            record = {'slack_ns': float(slack), 'logic_levels': int(logic_levels), 'from': source, 'to': sink,
                      'interconnect_ns': round(float(ic), 3), 'cell_ns': round(float(cell), 3)}
            if label not in worst or record['slack_ns'] < worst[label]['slack_ns']:
                worst[label] = record
            levels[label] = max(levels.get(label, 0), int(logic_levels))
        elif fields[0] == 'point':
            points.setdefault(int(fields[1]), []).append(
                {'type': fields[2], 'incr_ns': float(fields[3]), 'node': fields[4]})
    stages_out = {}
    for label, record in sorted(worst.items()):
        stages_out[label] = {**record, 'fmax_mhz': round(1000.0 / (period - record['slack_ns']), 2),
                             'max_logic_levels': levels[label]}
    critical = []
    for rank, path in sorted(points.items()):
        # The data path: the launch clock's own points are not part of it.
        path = [point for point in path if point['type'] not in ('launch', 'clknet')]
        ic = sum(point['incr_ns'] for point in path if point['type'] == 'ic')
        total = sum(point['incr_ns'] for point in path)
        cells = [point for point in path if point['type'] == 'cell']
        critical.append({'rank': rank, 'delay_ns': round(total, 3), 'interconnect_ns': round(ic, 3),
                         'cells': len(cells), 'nodes': [point['node'] for point in cells]})
    return {'fmax_mhz': fmax, 'stages': stages_out, 'critical_paths': critical}


def run(command: list[str], cwd: Path, log: Path, env: dict) -> None:
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=cwd, env=env, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise QuartusError(f'{command[0]} failed in {cwd}; see {log}')


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--yosys', required=True, help="the i386 lock's Yosys, which elaborates the netlist")
    parser.add_argument('--spike', choices=tuple(benchmark.SPIKES), default='cpu')
    parser.add_argument('--seed', type=int, action='append', help='fitter seed; repeat (default 1-4)')
    parser.add_argument('--target-mhz', type=float, default=100.0)
    parser.add_argument('--jobs', type=int, default=4, help='seeds fitted at once')
    parser.add_argument('--output', type=Path)
    parser.add_argument('--reuse', action='store_true', help='keep an existing synthesis and fits; re-run TimeQuest')
    args = parser.parse_args()
    seeds = args.seed or [1, 2, 3, 4]
    try:
        quartus_root, quartus_sh = locate_quartus(os.environ.get('QUARTUS_ROOTDIR', ''), ROOT)
        version = quartus_version_line(quartus_sh)
        bin_dir = quartus_sh.parent
        TOP = benchmark.SPIKES[args.spike][0]
        stages = benchmark.STAGES[args.spike]
        sources = [ROOT / path for path in benchmark.sources(args.spike)]
        digest = hashlib.sha256()
        for path in sources:
            digest.update(path.read_bytes())
        digest.update(Path(args.yosys).read_bytes())
        digest.update(f'{args.spike}{args.target_mhz}{version}'.encode())
        output = (args.output or ROOT / 'build/quartus/fes-i386' / digest.hexdigest()[:12]).resolve()
        base = output / 'synthesis'
        reuse = args.reuse and (base / 'db').is_dir()
        if base.exists() and not reuse:
            shutil.rmtree(base)
        base.mkdir(parents=True, exist_ok=True)
        env = dict(os.environ, QUARTUS_ROOTDIR=str(quartus_root))

        # The netlist Yosys elaborates, its products marked, then Quartus
        # synthesis once.
        elaborated, netlist = base / 'yosys.v', base / f'{TOP}.v'
        if not reuse:
            run([args.yosys, '-q', '-p', NETLIST.format(files=' '.join(map(str, sources)), top=TOP, out=elaborated)],
                base, base / 'yosys.log', env)
        text, products = mark_products(elaborated.read_text())
        netlist.write_text(text)
        pins = (ROOT / benchmark.SPIKE / 'spike.qsf').read_text()
        (base / f'{TOP}.qpf').write_text(f'PROJECT_REVISION = "{TOP}"\n')
        (base / f'{TOP}.qsf').write_text(
            'set_global_assignment -name FAMILY "Cyclone V"\n'
            f'set_global_assignment -name DEVICE {DEVICE}\n'
            f'set_global_assignment -name TOP_LEVEL_ENTITY {TOP}\n'
            'set_global_assignment -name NUM_PARALLEL_PROCESSORS 4\n'
            f'set_global_assignment -name VERILOG_FILE {TOP}.v\n'
            f'set_global_assignment -name SDC_FILE {TOP}.sdc\n'
            'set_global_assignment -name DSP_BLOCK_BALANCING "LOGIC ELEMENTS"\n'
            + pins)
        (base / f'{TOP}.sdc').write_text(
            f'create_clock -name clk -period {1000.0 / args.target_mhz:.3f} [get_ports {{FPGA_CLK1_50}}]\n')
        if not reuse:
            run([str(bin_dir / 'quartus_map'), TOP], base, base / 'map.log', env)
        (base / 'sta.tcl').write_text(STA_TCL.format(top=TOP, post_map='-post_map', out='sta.tsv', paths=PATHS))
        run([str(bin_dir / 'quartus_sta'), '-t', 'sta.tcl'], base, base / 'sta.log', env)
        names = register_names(text)
        post_map = parse_sta((base / 'sta.tsv').read_text(), args.target_mhz, stages, names)

        def fit(seed: int) -> dict:
            work = output / f'seed-{seed}'
            if not (reuse and (work / f'{TOP}.fit.summary').is_file()):
                if work.exists():
                    shutil.rmtree(work)
                shutil.copytree(base, work)
                run([str(bin_dir / 'quartus_fit'), TOP, f'--seed={seed}'], work, work / 'fit.log', env)
            (work / 'sta.tcl').write_text(STA_TCL.format(top=TOP, post_map='', out='sta.tsv', paths=PATHS))
            run([str(bin_dir / 'quartus_sta'), '-t', 'sta.tcl'], work, work / 'sta.log', env)
            result = parse_sta((work / 'sta.tsv').read_text(), args.target_mhz, stages, names)
            report = (work / f'{TOP}.fit.summary')
            result['fit_summary'] = report.read_text() if report.is_file() else ''
            listing = ', '.join(f'{label} {figure["fmax_mhz"]:.1f}' for label, figure in result['stages'].items())
            print(f'quartus seed {seed}: {result["fmax_mhz"]} MHz ({listing})', flush=True)
            return {'seed': seed, **result}

        with ThreadPoolExecutor(max_workers=args.jobs) as executor:
            runs = list(executor.map(fit, seeds))
        summary = {'classification': 'i386 Quartus oracle: diagnostic only, not a sealed labelled package',
                   'created_utc': datetime.now(timezone.utc).isoformat(), 'quartus': version,
                   'spike': args.spike, 'yosys': args.yosys,
                   'netlist': NETLIST.format(files='<spike sources>', top=TOP, out='yosys.v'),
                   'dsp_products': products, 'device': DEVICE, 'target_mhz': args.target_mhz,
                   'corner': 'slow 1100mV 100C (TimeQuest default)',
                   'sources': {path.relative_to(ROOT).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
                               for path in sources},
                   'post_map': post_map, 'runs': runs}
        (output / 'summary.json').write_text(json.dumps(summary, indent=2, sort_keys=True) + '\n')
        print('Diagnostic evidence: ' + str(output / 'summary.json'), flush=True)
        return 0
    except (OSError, RuntimeError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
