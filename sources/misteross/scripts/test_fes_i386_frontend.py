#!/usr/bin/env python3
"""Check the i386 front end and AG address unit; the front end against the oracle.

Host simulation only. No network access occurs unless --download is given,
which fetches the pinned Bochs oracle (scripts/fes_i386_oracle.py) into the
ignored build directory.

Verilator testbenches in cores/fes-common/sim/i386:

  lendec_tb.cpp    The 16 per-byte length decoders, exhaustively: no prefix or
                   each of the eleven, every one- and two-byte opcode, every
                   ModRM and every SIB byte, in 16- and 32-bit default size,
                   against the oracle's decoder.
  frontend_tb.cpp  The whole front end on sampled streams: random bytes,
                   prefix-heavy bytes, long prefix runs, and generated defined
                   instructions whose issue cycles are checked exactly (one per
                   cycle, plus one cycle for each prefix before the last).
  agu_tb.cpp       The AG address unit against the architectural address,
                   random and 16-bit wrap-edge operands. Needs no oracle.

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

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/i386'
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-i386-frontend'
LENDEC_RTL = ('fes_i386_pkg.sv', 'fes_i386_oplen.sv', 'fes_i386_lendec.sv')
FRONTEND_RTL = (*LENDEC_RTL, 'fes_i386_fetchbuf.sv', 'fes_i386_qalign.sv', 'fes_i386_frontend.sv')
# testbench name -> (top module, RTL files, extra SystemVerilog under SIM, C++ source, links the oracle)
BENCHES = {
    'lendec': ('fes_i386_lendec', LENDEC_RTL, (), 'lendec_tb.cpp', True),
    'frontend': ('frontend_tb_top', FRONTEND_RTL, ('frontend_tb_top.sv',), 'frontend_tb.cpp', True),
    'agu': ('fes_i386_agu', ('fes_i386_agu.sv',), (), 'agu_tb.cpp', False),
}


class FrontendError(RuntimeError):
    pass


def verilate(verilator: str, name: str, linked: list[Path], jobs: int) -> Path:
    """Build one testbench against the oracle; return the executable."""
    top, rtl, sim, source, uses_oracle = BENCHES[name]
    output = OUTPUT / name
    output.mkdir(parents=True, exist_ok=True)
    command = [verilator, '--cc', '--exe', '--build', '-j', str(jobs), '-O3', '--top-module', top,
               '-Wall', '-Wno-UNUSEDSIGNAL', '--Mdir', str(output),
               '-CFLAGS', f'-std=c++17 -O2 -I{SIM}', '-LDFLAGS', '-pthread',
               *(str(RTL / path) for path in rtl), *(str(SIM / path) for path in sim),
               str(SIM / source), *(map(str, linked) if uses_oracle else ())]
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


def summary(lendec: list[dict], frontend: list[dict], agu: list[dict]) -> list[str]:
    lines = []
    for record in lendec:
        if 'default_size' in record:
            lines.append(f"length decoders, {record['default_size']}-bit: {record['cases']} cases, "
                         f"{record['compared']} compared, {record['oracle_undefined']} undefined in the oracle, "
                         f"{record['mismatches']} mismatches")
        else:
            lines.append(f"length decoders, directed: {record['directed']} checks, "
                         f"{record['directed_failures']} failures")
    for record in frontend:
        line = (f"front end {record['stream']}, {record['default_size']}-bit: {record['instructions']} instructions, "
                f"{record['compared']} compared, {record['too_long']} too long, {record['failures']} failures")
        if record['issue_cycles']:
            line += (f"; {record['instructions']} issued in {record['issue_cycles']} cycles, "
                     f"{record['timing_failures']} timing failures")
        lines.append(line)
    for record in agu:
        lines.append(f"AG address unit: {record['agu_vectors']} vectors, {record['wrapped_16']} with a 16-bit wrap, "
                     f"{record['failures']} failures")
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
        results = {}
        for name in selected:
            executable = verilate(verilator, name, linked, args.jobs)
            records, failures, code = run(executable, env)
            results[name] = records
            report[name] = {'records': records, 'failures': failures}
            if code or failures or failed(records):
                exit_code = 1
    except (OSError, oracle.OracleError, FrontendError, json.JSONDecodeError) as error:
        print('i386 front-end check error:', error, file=sys.stderr)
        return 2
    OUTPUT.mkdir(parents=True, exist_ok=True)
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    for line in summary(results.get('lendec', []), results.get('frontend', []), results.get('agu', [])):
        print(line)
    for name in results:
        for line in report[name]['failures'][:40]:
            print(line)
    print(('i386 checks FAILED' if exit_code else 'i386 checks passed') +
          f' (host simulation only; report: {OUTPUT / "report.json"})')
    return exit_code


if __name__ == '__main__':
    raise SystemExit(main())
