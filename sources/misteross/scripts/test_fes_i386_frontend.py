#!/usr/bin/env python3
"""Check the i386 front end against the oracle, and D's address operands and AG.

Host simulation only. No network access occurs unless --download is given,
which fetches the pinned Bochs oracle (scripts/fes_i386_oracle.py) into the
ignored build directory.

Verilator testbenches in cores/fes-common/sim/i386:

  lendec_tb.cpp    The per-byte length decode, L1's 16 decoders and L2's sums,
                   exhaustively: no prefix or each of the eleven, every one- and
                   two-byte opcode, every ModRM and every SIB byte, in 16- and
                   32-bit default size and for every prefix-accumulator
                   variant, against the oracle's decoder.
  frontend_tb.cpp  The whole front end on sampled streams: random bytes,
                   prefix-heavy bytes, long prefix runs, and generated defined
                   instructions whose issue cycles are checked exactly (one per
                   cycle, plus one cycle for each prefix before the last);
                   then the same with random fetch latency, untimed; and
                   a delayed fetch response while Q crosses buffer halves.
  dag_tb.cpp       D's address operands (the forwarding table, scaling, the
                   late operand and its stalls) and AG, cycle by cycle: every
                   memory ModRM and SIB in both address sizes as a uop stream
                   whose producers move through AG to WB, for the stall rule
                   and the architectural address, then each kind of result
                   timed into the next uop's address. Needs no oracle.

With --yosys, each bench first runs its RTL through that Yosys (read, proc,
flatten, opt) and simulates the netlist Yosys wrote instead: the routed
spikes are built from Yosys's reading of the SystemVerilog, and this shows
it is the same design. The front-end bench also synthesizes Q's aligner on
its own, as the spikes do, and fails if a multiplier or carry cell appears:
Q has no wide sum (DeanoC/yosys#59).

Example:
  python3 scripts/test_fes_i386_frontend.py --download
  python3 scripts/test_fes_i386_frontend.py --instructions 50000 --seed 7

Writes build/sim/fes-i386-frontend/report.json.
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

from scripts import fes_i386_oracle as oracle
from scripts.benchmark_fes_i386 import SYNTHESIS

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-i386-frontend'
LENDEC_RTL = ('fes_i386_pkg.sv', 'fes_i386_oplen.sv', 'fes_i386_lendec.sv', 'fes_i386_lstage.sv')
FRONTEND_RTL = (*LENDEC_RTL, 'fes_i386_fetchbuf.sv', 'fes_i386_qalign.sv', 'fes_i386_frontend.sv')
DAG_RTL = ('fes_i386_pkg.sv', 'fes_i386_amode.sv', 'fes_i386_dfwd.sv', 'fes_i386_agu.sv')
# testbench name -> (top module, RTL files, extra SystemVerilog under SIM, C++ source, links the oracle,
#                    the module --yosys turns into a netlist)
BENCHES = {
    'lendec': ('lendec_tb_top', LENDEC_RTL, ('lendec_tb_top.sv',), 'lendec_tb.cpp', True, 'lendec_tb_top'),
    'frontend': ('frontend_tb_top', FRONTEND_RTL, ('frontend_tb_top.sv',), 'frontend_tb.cpp', True,
                 'fes_i386_frontend'),
    'dag': ('dag_tb_top', DAG_RTL, ('dag_tb_top.sv',), 'dag_tb.cpp', False, 'dag_tb_top'),
}


class FrontendError(RuntimeError):
    pass


def synthesize_netlist(yosys: str, sources: list[Path], top: str, output: Path) -> Path:
    """Write Yosys's reading of top, from sources, as a flat Verilog netlist."""
    netlist = output / f'{top}.netlist.v'
    script = (f'read_verilog -sv {" ".join(map(str, sources))}; hierarchy -top {top}; '
              f'proc -norom; flatten; opt -purge; write_verilog -noattr {netlist}')
    log = output / 'yosys.log'
    with log.open('w') as stream:
        result = subprocess.run([yosys, '-q', '-p', script], cwd=output, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise FrontendError(f'yosys failed for {top}; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return netlist


def yosys_netlist(yosys: str, name: str, output: Path) -> Path:
    """Write Yosys's reading of a bench's RTL as a flat Verilog netlist."""
    _, rtl, sim, _, _, synthesized = BENCHES[name]
    sources = [*(RTL / path for path in rtl), *(SIM / path for path in sim if Path(path).stem == synthesized)]
    return synthesize_netlist(yosys, sources, synthesized, output)


def qalign_synthesis(yosys: str) -> dict[str, int]:
    """Synthesize Q's aligner on its own; count its multiplier and carry cells."""
    output = OUTPUT / 'qalign-synthesis'
    output.mkdir(parents=True, exist_ok=True)
    sources = ' '.join(str(RTL / path) for path in ('fes_i386_pkg.sv', 'fes_i386_qalign.sv'))
    netlist = output / 'synth.json'
    script = f'read_verilog -sv {sources}; hierarchy -top fes_i386_qalign; {SYNTHESIS}; write_json {netlist}'
    log = output / 'yosys.log'
    with log.open('w') as stream:
        result = subprocess.run([yosys, '-q', '-p', script], cwd=output, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise FrontendError(f'yosys failed for qalign; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    cells = json.loads(netlist.read_text())['modules']['fes_i386_qalign']['cells'].values()
    types = [cell['type'] for cell in cells]
    return {'cells': len(types), 'multipliers': sum(t.startswith('MISTRAL_MUL') for t in types),
            'carry_cells': types.count('MISTRAL_ALUT_ARITH')}


def verilate(verilator: str, name: str, linked: list[Path], jobs: int, yosys: str | None = None) -> Path:
    """Build one testbench, from the RTL or Yosys's netlist of it; return the executable."""
    top, rtl, sim, source, uses_oracle, synthesized = BENCHES[name]
    output = OUTPUT / (name + ('-netlist' if yosys else ''))
    output.mkdir(parents=True, exist_ok=True)
    if yosys:
        # The netlist replaces the synthesized module and everything under it;
        # a wrapper left as SystemVerilog still needs the package's types.
        design = [yosys_netlist(yosys, name, output), RTL / 'fes_i386_pkg.sv',
                  *(SIM / path for path in sim if Path(path).stem != synthesized)]
        warnings = ['-Wno-fatal', '+define+FES_I386_NETLIST']
    else:
        design = [*(RTL / path for path in rtl), *(SIM / path for path in sim)]
        warnings = []
    command = [verilator, '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', top,
               '-Wall', '-Wno-UNUSEDSIGNAL', *warnings, '--Mdir', str(output),
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM}', '-LDFLAGS', '-pthread',
               *map(str, design), str(SIM / source), *(map(str, linked) if uses_oracle else ())]
    log = output / 'build.log'
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
    if result.returncode:
        raise FrontendError(f'verilator failed for {name}; see {log}\n' + '\n'.join(log.read_text().splitlines()[-30:]))
    return output / ('V' + top)


def parse(text: str) -> tuple[list[dict], list[str]]:
    """Split testbench output into its JSON records and its failure lines."""
    records, failures = [], []
    for line in text.splitlines():
        if line.startswith('{'):
            records.append(json.loads(line))
        elif line.startswith(('MISMATCH ', 'FAIL ')):
            failures.append(line)
        elif line.strip():
            raise FrontendError('unexpected testbench output: ' + line)
    return records, failures


def failed(records: list[dict]) -> bool:
    """Whether any record reports a failure, or the records are incomplete."""
    for record in records:
        for key in ('mismatches', 'failures', 'directed_failures', 'timing_failures'):
            if record.get(key):
                return True
    return not records


def summary(lendec: list[dict], frontend: list[dict], dag: list[dict]) -> list[str]:
    lines = []
    for record in lendec:
        if 'default_size' in record:
            lines.append(f"length decoders, {record['default_size']}-bit: {record['cases']} cases, "
                         f"{record['compared']} compared, {record['oracle_undefined']} undefined in the oracle, "
                         f"{record['variants_compared']} accumulator variants compared, "
                         f"{record['mismatches']} mismatches")
        else:
            lines.append(f"length decoders, directed: {record['directed']} checks, "
                         f"{record['directed_failures']} failures")
    for record in frontend:
        line = (f"front end {record['stream']}, {record['default_size']}-bit: {record['instructions']} instructions, "
                f"{record['compared']} compared, {record['too_long']} too long, {record['failures']} failures")
        if record.get('fetch_latency'):
            line += f"; Q left a half L2 held no block for {record['l2_empty_releases']} times"
        if record['issue_cycles']:
            latency = ', '.join(f'{cycles} cycles x{count}' for cycles, count in record['redirect_to_issue'].items())
            line += (f"; {record['instructions']} issued in {record['issue_cycles']} cycles, "
                     f"{record['timing_failures']} timing failures; redirect to first issue: {latency}")
        if record['stream'] == 'fetch-stalled':
            line += (f"; {record['fetch_wait_cycles']} fetch wait cycles, "
                     f"{record['boundary_wait_cycles']} at the cross-half instruction, "
                     f"{record['cross_half_instructions']} cross-half instructions")
        lines.append(line)
    for record in dag:
        waits = record['wait_cycles']
        lines.append(f"D and AG: {record['uops']} uops in {record['dag_cases']} D cycles, {record['addresses']} "
                     f"addresses ({record['wrapped_16']} with a 16-bit wrap), late base {sum(record['late_base'])}, "
                     f"late unscaled index {sum(record['late_index'])}, stalls: {record['stall_not_ready']} "
                     f"not ready, {record['stall_both_late']} both late, {record['stall_scaled_late']} late scaled "
                     f"index; {record['released']} released after stalling; cycles waited behind the uop ahead "
                     f"(base, index, scaled index): early {waits['early']}, load {waits['load']}, "
                     f"late {waits['late']}; {record['failures']} failures")
    return lines


def run(executable: Path, env: dict[str, str]) -> tuple[list[dict], list[str], int]:
    result = subprocess.run([str(executable)], cwd=ROOT, env=env, capture_output=True, text=True)
    if result.returncode not in (0, 1):
        raise FrontendError(f'{executable.name} exited with {result.returncode}\n{result.stderr[-4000:]}')
    records, failures = parse(result.stdout)
    return records, failures, result.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--download', action='store_true',
                        help='explicitly fetch the pinned oracle into ignored build/')
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    parser.add_argument('--jobs', type=int, default=min(os.cpu_count() or 2, 16))
    parser.add_argument('--instructions', type=int, default=400000,
                        help='instructions per front-end stream (default 400000)')
    parser.add_argument('--seed', type=int, default=1)
    parser.add_argument('--bench', choices=tuple(BENCHES), action='append')
    parser.add_argument('--yosys', help='simulate this Yosys\'s netlist of each bench instead of the RTL')
    args = parser.parse_args()
    if not 1 <= args.jobs <= 64:
        parser.error('--jobs must be between 1 and 64')
    if args.instructions < 1 or args.seed < 0:
        parser.error('--instructions must be positive and --seed nonnegative')
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    verilator = shutil.which(args.verilator)
    if verilator is None:
        print(f'selected Verilator not found: {args.verilator}', file=sys.stderr)
        return 2
    env = dict(os.environ, FES_I386_THREADS=str(args.jobs),
               FES_I386_INSTRUCTIONS=str(args.instructions), FES_I386_SEED=str(args.seed))
    report: dict[str, object] = {'instructions_per_stream': args.instructions, 'seed': args.seed}
    exit_code = 0
    try:
        selected = args.bench or tuple(BENCHES)
        linked = oracle.link_inputs(args.jobs, args.download) if any(BENCHES[name][4] for name in selected) else []
        yosys = None
        if args.yosys:
            yosys = shutil.which(args.yosys)
            if yosys is None:
                raise FrontendError(f'selected Yosys not found: {args.yosys}')
            report['yosys'] = yosys
        results = {}
        for name in selected:
            executable = verilate(verilator, name, linked, args.jobs, yosys)
            records, failures, code = run(executable, env)
            results[name] = records
            report[name] = {'records': records, 'failures': failures}
            if code or failures or failed(records):
                exit_code = 1
        if yosys and 'frontend' in selected:
            report['qalign_synthesis'] = synthesis = qalign_synthesis(yosys)
            if synthesis['multipliers'] or synthesis['carry_cells']:
                exit_code = 1
    except (OSError, oracle.OracleError, FrontendError, json.JSONDecodeError) as error:
        print('i386 front-end check error:', error, file=sys.stderr)
        return 2
    OUTPUT.mkdir(parents=True, exist_ok=True)
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    for line in summary(results.get('lendec', []), results.get('frontend', []), results.get('dag', [])):
        print(line)
    if 'qalign_synthesis' in report:
        synthesis = report['qalign_synthesis']
        print(f"Q's aligner on its own: {synthesis['cells']} cells, {synthesis['multipliers']} multipliers, "
              f"{synthesis['carry_cells']} carry cells")
    for name in results:
        for line in report[name]['failures'][:40]:
            print(line)
    print(('i386 checks FAILED' if exit_code else 'i386 checks passed') +
          f' (host simulation only; report: {OUTPUT / "report.json"})')
    return exit_code


if __name__ == '__main__':
    raise SystemExit(main())
