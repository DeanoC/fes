#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Check fes_x87_trans against its model, bit for bit (#1958).

Host simulation only; no oracle and no host x87. The engine that runs the x87
transcendentals' microprogram is built with the backend it drives
(cores/fes-common/sim/i386/x87_trans_tb_top.sv) and run over vectors written
by the microprogram's exact model (scripts/x87_trans_ucode.py): for each, the
engine's last result before rounding, and the other result of a two-result
form, must equal the model's. There is no tolerance.

    python3 scripts/test_fes_x87_trans_engine.py --vectors 2000

Writes build/sim/fes-x87-trans-engine/report.json, which names the revision,
the tracked files differing from it and a SHA-256 for each tested source.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from scripts import i386_verilator  # noqa: E402
from scripts import x87_trans_ucode  # noqa: E402

RTL = ROOT / 'cores/fes-common/rtl/i386'
SIM = ROOT / 'cores/fes-common/sim/i386'
OUTPUT = ROOT / 'build/sim/fes-x87-trans-engine'
SOURCES = ('fes_x87_pkg.sv', 'fes_x87_fast.sv', 'fes_x87_slow.sv', 'fes_x87_core.sv', 'fes_x87_trans_pkg.sv',
           'fes_x87_trans_rom.sv', 'fes_x87_trans.sv')


def tested() -> list[Path]:
    return [*(RTL / name for name in SOURCES), SIM / 'x87_trans_tb_top.sv', SIM / 'x87_trans_tb.cpp',
            Path(__file__).resolve(), Path(x87_trans_ucode.__file__).resolve()]


def source_record() -> dict:
    repository = ROOT.parents[1]

    def git(*arguments: str) -> str:
        return subprocess.run(['git', '-C', str(repository), *arguments], capture_output=True, text=True,
                              check=True).stdout
    return {'fes_revision': git('rev-parse', 'HEAD').strip(),
            'tracked_changes': sorted(line[3:] for line in
                                      git('status', '--porcelain', '--untracked-files=no').splitlines()),
            'files': {str(path.relative_to(repository)): hashlib.sha256(path.read_bytes()).hexdigest()
                      for path in tested()}}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--vectors', type=int, default=2000, help='operands an instruction and operand class')
    parser.add_argument('--seed', type=int, default=1958)
    i386_verilator.add_argument(parser)
    parser.add_argument('--jobs', type=int, default=4)
    args = parser.parse_args()
    try:
        verilator = i386_verilator.locate(args.cache_root)
        source = source_record()
        program = x87_trans_ucode.build_program(x87_trans_ucode.build_rom())
        if any((RTL / name).read_text() != text for name, text in x87_trans_ucode.emit(program).items()):
            raise RuntimeError('the checked-in ROMs are not what the model generates: run x87_trans_ucode.py --emit')
        OUTPUT.mkdir(parents=True, exist_ok=True)
        vectors = OUTPUT / 'vectors.txt'
        lines = x87_trans_ucode.unit_vectors(program, args.vectors, args.seed, engine=True)
        vectors.write_text('\n'.join(lines) + '\n')
        log = OUTPUT / 'build.log'
        command = [str(verilator.path), '--cc', '--exe', '--build', '-j', str(args.jobs), '-O3', '--top-module',
                   'x87_trans_tb_top', '-Wall', '-Wno-UNUSEDSIGNAL', '--Mdir', str(OUTPUT / 'obj'), '-CFLAGS',
                   '-std=c++17 -O2', *(str(RTL / name) for name in SOURCES), str(SIM / 'x87_trans_tb_top.sv'),
                   str(SIM / 'x87_trans_tb.cpp')]
        with log.open('w') as stream:
            if subprocess.run(command, cwd=ROOT, env=verilator.environment(), stdout=stream,
                              stderr=subprocess.STDOUT).returncode:
                raise RuntimeError(f'verilator failed; see {log}')
        result = subprocess.run([str(OUTPUT / 'obj/Vx87_trans_tb_top'), str(vectors)], capture_output=True, text=True)
        summary = next(line.split() for line in result.stdout.splitlines() if line.startswith('SUMMARY '))
        if source_record() != source:
            raise RuntimeError('a tested source or the revision changed during the run')
    except (OSError, RuntimeError, StopIteration, i386_verilator.VerilatorError, subprocess.CalledProcessError) as error:
        print('x87 transcendental engine error:', error, file=sys.stderr)
        return 2
    passed, failed, most, total = map(int, summary[1:5])
    per = {}
    for line in lines:
        name = x87_trans_ucode.INSTRUCTIONS[int(line.split()[0])]
        per[name] = per.get(name, 0) + 1
    report = {'what': 'fes_x87_trans against its model, bit for bit; no tolerance', 'source': source,
              'verilator': verilator.record(), 'vectors': len(lines), 'vectors_by_instruction': per,
              'passed': passed, 'failed': failed, 'clocks_most': most,
              'clocks_mean': round(total / max(passed + failed, 1), 1),
              'failures': [line for line in result.stdout.splitlines() if line.startswith('FAIL ')][:20]}
    (OUTPUT / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    print(f'fes_x87_trans against its model: {passed} passed, {failed} failed of {len(lines)} vectors '
          f'(host simulation only; report: {OUTPUT / "report.json"})')
    return 1 if failed or passed != len(lines) or not passed else 0


if __name__ == '__main__':
    sys.exit(main())
