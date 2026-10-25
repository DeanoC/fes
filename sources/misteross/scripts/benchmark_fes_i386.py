#!/usr/bin/env python3
"""Route the i386 timing spikes on the DE10-Nano; never generate or deploy RBFs.

A CPU-only routed harness like the RISC-V QoR one (cores/fes-riscv/qor.md):
each spike is a block of fes_i386_cpu inside a stimulus/signature envelope
(cores/fes-common/sim/i386/spike) with one clock, KEY0 and LED0 on pads.

  frontend  F1, F2, the fetch buffer with its 16 length decoders, and Q,
            fetching from an M10K memory model
  ag        the AG stage: forwarding mux, 4:2 compression, the linear and
            offset adders and the 16-bit-wrap high-half select
  m2        the M2 stage: M10K tag and data reads, four tag compares against
            a registered page number, way select

Tools come from the selected toolchain lock's authenticated slot (default: the
repository toolchain.lock). Synthesis permits M10K and DSP cells and forbids
LUT RAM, as the design's rules do. Every route uses the GPU router through
gpu-run, with CPU fallback rejected. Timing is allowed to fail so a miss is
measured; nothing here is a signoff.

Example:
  python3 scripts/benchmark_fes_i386.py --cache-root /abs/out/cache/misteross-toolchains \\
      --spike ag --seed 1 --seed 2

Results describe the spike envelope. They are diagnostic place-and-route
measurements, not packages or board acceptance.
"""
from __future__ import annotations

import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import re
import sys
import time

if __package__ in (None, ''):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import fes_build_common as board

ROOT = Path(__file__).resolve().parents[1]
RTL = Path('cores/fes-common/rtl/i386')
SPIKE = Path('cores/fes-common/sim/i386/spike')
DEVICE = '5CSEBA6U23I7'
FRONTEND_RTL = ('fes_i386_pkg.sv', 'fes_i386_oplen.sv', 'fes_i386_lendec.sv', 'fes_i386_fetchbuf.sv',
                'fes_i386_qalign.sv', 'fes_i386_frontend.sv')
SPIKES = {
    'frontend': ('spike_frontend', FRONTEND_RTL),
    'ag': ('spike_ag', ('fes_i386_agu.sv',)),
    'm2': ('spike_m2', ()),
}
# M10K and DSP allowed, LUT RAM forbidden (docs/i386-cpu-design.md, platform rules).
SYNTHESIS = 'synth_intel_alm -family cyclonev -nolutram'


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path: Path, value) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def sources(spike: str) -> list[Path]:
    top, rtl = SPIKES[spike]
    return [*(RTL / name for name in rtl), SPIKE / 'spike_stimulus.sv', SPIKE / f'{top}.sv']


_CELL_SUFFIX = re.compile(r'_MISTRAL_.*$')


def _owner(cell: str) -> str:
    """The RTL signal a mapped cell name starts from."""
    return _CELL_SUFFIX.sub('', cell)


def path_summary(path: dict) -> dict:
    """Compress one nextpnr critical path to what a reader needs."""
    segments = path.get('path', [])
    logic = [segment for segment in segments if segment['type'] == 'logic']
    routing = sum(segment['delay'] for segment in segments if segment['type'] == 'routing')
    total = sum(segment['delay'] for segment in segments)
    cells = [segment['to']['cell'] for segment in logic]
    owners = []
    for cell in [segments[0]['from']['cell'] if segments else '', *cells,
                 segments[-1]['to']['cell'] if segments else '']:
        owner = _owner(cell)
        if owner and (not owners or owners[-1] != owner):
            owners.append(owner)
    return {
        'from': segments[0]['from']['cell'] if segments else None,
        'to': segments[-1]['to']['cell'] if segments else None,
        'delay_ns': round(total, 3), 'routing_ns': round(routing, 3), 'logic_ns': round(total - routing, 3),
        'logic_cells': len(logic),
        'carry_cells': sum('ALUT_ARITH' in cell for cell in cells),
        'm10k_source': bool(segments) and 'M10K' in segments[0]['from']['cell'],
        'signals': owners,
    }


def timing_summary(report: Path) -> dict:
    data = json.loads(report.read_text())
    clocks = data.get('fmax', {})
    achieved = [float(record['achieved']) for record in clocks.values() if 'achieved' in record]
    if not achieved:
        raise ValueError(f'{report} has no achieved clock frequency')
    return {'fmax_mhz': min(achieved), 'clocks': clocks, 'utilization': data.get('utilization', {}),
            'critical_paths': [path_summary(path) for path in data.get('critical_paths', [])]}


def expected_backend(identity: str) -> str:
    return 'cuda' if 'gpu-router=CUDA' in identity else 'hip'


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--spike', choices=tuple(SPIKES), action='append')
    parser.add_argument('--seed', type=int, action='append', help='placer/router seed; repeat (default 1-4)')
    parser.add_argument('--target-mhz', type=float, default=100.0)
    parser.add_argument('--lock', type=Path, default=Path('toolchain.lock'),
                        help='toolchain lock relative to sources/misteross (default: toolchain.lock)')
    parser.add_argument('--cache-root', type=Path, help='shared misteross-toolchains cache root')
    parser.add_argument('--gpu-device', type=int, default=0)
    parser.add_argument('--output', type=Path, help='diagnostic output directory')
    args = parser.parse_args()
    spikes = args.spike or list(SPIKES)
    seeds = args.seed or [1, 2, 3, 4]
    if any(seed < 1 or seed > 0x7fffffff for seed in seeds) or len(set(seeds)) != len(seeds):
        parser.error('--seed values must be distinct and between 1 and 2147483647')
    if not math.isfinite(args.target_mhz) or args.target_mhz <= 0:
        parser.error('--target-mhz must be finite and positive')
    if args.cache_root is not None and not args.cache_root.is_absolute():
        parser.error('--cache-root must be absolute')
    if board.gpu_run_active():
        parser.error('run outside gpu-run; each route takes its own lease')

    try:
        tools = board._authenticate_tools(ROOT, lock_path=ROOT / args.lock, cache_root=args.cache_root)
        yosys, nextpnr = tools['yosys'], tools['nextpnr-mistral']
        backend = expected_backend(nextpnr.identity)
        selected = {path.as_posix(): (ROOT / path).read_bytes()
                    for spike in spikes for path in sources(spike)}
        selected[(SPIKE / 'spike.qsf').as_posix()] = (ROOT / SPIKE / 'spike.qsf').read_bytes()
        selected['scripts/benchmark_fes_i386.py'] = Path(__file__).read_bytes()
        identity = {'device': DEVICE, 'target_mhz': args.target_mhz, 'router': 'gpu', 'gpu_backend': backend,
                    'synthesis': SYNTHESIS, 'lock': args.lock.as_posix(), 'lock_sha256': digest(ROOT / args.lock),
                    'sources': {path: hashlib.sha256(data).hexdigest() for path, data in sorted(selected.items())},
                    'tools': {name: tool.identity for name, tool in sorted(tools.items())}}
        fingerprint = hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest()
        output = (args.output or ROOT / 'build/benchmark/fes-i386' / fingerprint[:12]).resolve()
        output.mkdir(parents=True, exist_ok=True)
        snapshot = output / 'sources'
        for path, data in selected.items():
            destination = snapshot / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(data)
        summary = {'format': 1, 'classification': 'i386 timing spike: contained routed diagnostic',
                   'hardware_acceptance': False, 'rbf_generated': False, 'source_fingerprint': fingerprint,
                   'created_utc': datetime.now(timezone.utc).isoformat(), **identity, 'spikes': {}}
        summary_path = output / 'summary.json'
        runs_path = output / 'runs.jsonl'
        write_json(summary_path, summary)

        route_env = dict(os.environ, NEXTPNR_REQUIRE_GPU='1')
        for spike in spikes:
            top, _ = SPIKES[spike]
            work = output / spike
            work.mkdir(exist_ok=True)
            files = ' '.join(str(snapshot / path) for path in sources(spike))
            program = f'read_verilog -sv {files}; {SYNTHESIS} -top {top}; stat; write_json synth.json'
            (work / 'synth.ys').write_text(program + '\n')
            started = time.monotonic()
            with (work / 'yosys.log').open('w') as log:
                result = board._run_command([str(yosys.path), '-s', 'synth.ys'], cwd=work,
                                            stdout=log, stderr=log)
            if result.returncode:
                raise RuntimeError(f'synthesis failed; see {work / "yosys.log"}')
            netlist = json.loads((work / 'synth.json').read_text())
            cells = dict(sorted(Counter(cell['type'] for cell in netlist['modules'][top]['cells'].values()).items()))
            record = {'top': top, 'synth_seconds': round(time.monotonic() - started, 1), 'cells': cells,
                      'synth_json_sha256': digest(work / 'synth.json'), 'runs': []}
            summary['spikes'][spike] = record
            write_json(summary_path, summary)

            for seed in seeds:
                destination = work / f'seed-{seed}'
                destination.mkdir(exist_ok=True)
                command = [str(nextpnr.path), '--json', str(work / 'synth.json'), '--device', DEVICE,
                           '--top', top, '--qsf', str(snapshot / SPIKE / 'spike.qsf'),
                           '--freq', f'{args.target_mhz:g}', '--seed', str(seed),
                           '--router', 'gpu', '--gpu-device', str(args.gpu_device), '--timing-allow-fail',
                           '--write', str(destination / 'routed.json'), '--report', str(destination / 'timing.json'),
                           '--detailed-timing-report']
                started = time.monotonic()
                with (destination / 'route.log').open('w') as log:
                    result = board._run_command(command, cwd=work, env=route_env, stdout=log, stderr=log,
                                                gpu_label=f'i386-spike-{spike}-seed{seed}')
                elapsed = round(time.monotonic() - started, 1)
                log_text = (destination / 'route.log').read_text(errors='replace')
                run = {'spike': spike, 'seed': seed, 'argv': command, 'exit_code': result.returncode,
                       'elapsed_seconds': elapsed, 'env': {'NEXTPNR_REQUIRE_GPU': '1'},
                       **board.gpu_route_evidence(log_text, args.gpu_device)}
                if result.returncode == 0:
                    run['gpu_backend'] = board._require_gpu_backend(log_text, backend)
                    run.update(timing_summary(destination / 'timing.json'))
                    run['target_met'] = run['fmax_mhz'] >= args.target_mhz
                    print(f'{spike} seed {seed}: {run["fmax_mhz"]:.2f} MHz; '
                          f'target {args.target_mhz:g} MHz {"met" if run["target_met"] else "missed"}', flush=True)
                record['runs'].append(run)
                with runs_path.open('a') as stream:
                    stream.write(json.dumps({k: v for k, v in run.items() if k != 'critical_paths'}
                                            | {'critical_path': (run.get('critical_paths') or [None])[0]},
                                            sort_keys=True) + '\n')
                write_json(summary_path, summary)
                if result.returncode:
                    raise RuntimeError(f'routing failed; see {destination / "route.log"}')
            fmax = [run['fmax_mhz'] for run in record['runs']]
            record.update({'worst_fmax_mhz': min(fmax), 'best_fmax_mhz': max(fmax),
                           'all_targets_met': all(run['target_met'] for run in record['runs'])})
            write_json(summary_path, summary)
        print('Diagnostic evidence: ' + str(summary_path), flush=True)
        return 0
    except (OSError, ValueError, RuntimeError, board.BuildError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
