#!/usr/bin/env python3
"""Measure a contained Z80 Cyclone V timing envelope; never generate/deploy RBFs.

Example (explicit native tool paths):
  python3 scripts/benchmark_fes_z80.py --yosys /path/bin/yosys \
      --nextpnr /path/bin/nextpnr-mistral --variant fast --seed 1 --seed 2

Results describe the CPU plus stimulus/signature envelope. They are diagnostic
place-and-route estimates, not authenticated packages or board acceptance.
"""
import argparse
from collections import Counter
from datetime import datetime, timezone
import hashlib
import json
import math
from pathlib import Path
import re
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
RTL = Path('cores/fes-common/rtl/z80')
SIM = Path('cores/fes-common/sim/z80')
DEVICE = '5CSEBA6U23I7'
TOP = 'benchmark_top'


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def tool_identity(path, version_arg):
    executable = Path(path).expanduser().resolve(strict=True)
    result = subprocess.run([str(executable), version_arg], check=True,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    return {'path': str(executable), 'sha256': digest(executable), 'version': result.stdout.strip()}


def run(command, output, log):
    print('Running ' + ' '.join(command), flush=True)
    started = time.monotonic()
    with log.open('w') as stream:
        result = subprocess.run(command, cwd=output, stdout=stream, stderr=subprocess.STDOUT)
    return {'command': command, 'exit_code': result.returncode,
            'elapsed_seconds': round(time.monotonic() - started, 3), 'log': str(log.relative_to(output))}


def timing_summary(report, log):
    data = json.loads(report.read_text())
    clocks = data.get('fmax', {})
    # Keep the native report's per-clock records and resource names intact.
    measured = [float(record['achieved']) for record in clocks.values()
                if isinstance(record, dict) and 'achieved' in record]
    if not measured:
        # The log also reports placement timing; the last record per clock
        # is the routed result and must replace those earlier estimates.
        final_clocks = dict(re.findall(
            r"Max frequency for clock '([^']+)':\s*([0-9.]+)\s*MHz", log.read_text()))
        measured = [float(value) for value in final_clocks.values()]
    if not measured:
        raise ValueError('router report contains no achieved clock frequency')
    paths = data.get('critical_paths', [])
    compact = []
    for path in paths:
        segments = path.get('path', [])
        if not segments:
            continue
        routing = sum(segment['delay'] for segment in segments if segment['type'] == 'routing')
        total = sum(segment['delay'] for segment in segments)
        compact.append({'source': segments[0]['from'], 'destination': segments[-1]['to'],
                        'delay_ns': total, 'routing_ns': routing, 'logic_ns': total - routing})
    return {'fmax_mhz': min(measured), 'clocks': clocks,
            'utilization': data.get('utilization', {}),
            'critical_paths': paths, 'critical_path_summary': compact}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--yosys', required=True, help='explicit Yosys executable')
    parser.add_argument('--nextpnr', required=True, help='explicit nextpnr-mistral executable')
    parser.add_argument('--variant', choices=('fast', 'nmos'), default='fast')
    parser.add_argument('--seed', type=int, action='append', help='repeat to measure several routing seeds')
    parser.add_argument('--target-mhz', type=float, default=100.0)
    parser.add_argument('--require-target', action='store_true',
                        help='fail after recording evidence if any selected seed misses the target')
    parser.add_argument('--output', type=Path, help='optional diagnostic output directory')
    args = parser.parse_args()
    seeds = args.seed or [1]
    if any(seed < 1 or seed > 0x7fffffff for seed in seeds):
        parser.error('--seed must be between 1 and 2147483647')
    if len(set(seeds)) != len(seeds):
        parser.error('--seed values must be distinct')
    if not math.isfinite(args.target_mhz) or args.target_mhz <= 0:
        parser.error('--target-mhz must be finite and positive')

    try:
        tools = {'yosys': tool_identity(args.yosys, '-V'),
                 'nextpnr': tool_identity(args.nextpnr, '--version')}
        files = [RTL / 'fes_z80_alu.sv', RTL / 'fes_z80_engine.sv']
        files += ([RTL / 'fes_z80_fast.sv'] if args.variant == 'fast' else
                  [RTL / 'fes_z80_bus.sv', RTL / 'fes_z80_nmos.sv'])
        files += [SIM / 'benchmark_top.sv', SIM / 'benchmark.qsf', Path('scripts/benchmark_fes_z80.py')]
        selected = {path.as_posix(): (ROOT / path).read_bytes() for path in files}
        hashes = {path: hashlib.sha256(contents).hexdigest() for path, contents in selected.items()}
        identity = {'variant': args.variant, 'device': DEVICE, 'top': TOP,
                    'target_mhz': args.target_mhz, 'router': 'router2',
                    'seeds': seeds, 'timing_gate_required': args.require_target,
                    'sources': hashes, 'tools': tools}
        fingerprint = hashlib.sha256(json.dumps(identity, sort_keys=True).encode()).hexdigest()
        output = (args.output or ROOT / 'build/benchmark/fes-z80' / args.variant / fingerprint[:12]).resolve()
        output.mkdir(parents=True, exist_ok=True)
        snapshot = output / 'sources'
        for path, contents in selected.items():
            destination = snapshot / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_bytes(contents)
        summary = {'format': 1, 'classification': 'contained CPU timing-envelope diagnostic',
                   'hardware_acceptance': False, 'rbf_generated': False,
                   'source_fingerprint': fingerprint,
                   'created_utc': datetime.now(timezone.utc).isoformat(),
                   **identity, 'runs': []}
        summary_path = output / 'summary.json'
        write_json(summary_path, summary)

        source_arguments = ' '.join('sources/' + path.as_posix() for path in files if path.suffix == '.sv')
        program = (f'read_verilog -sv {source_arguments}; '
                   f'chparam -set NMOS {int(args.variant == "nmos")} {TOP}; '
                   f'synth_intel_alm -family cyclonev -nobram -nolutram -nodsp -top {TOP}; '
                   'stat; write_json synth.json')
        (output / 'synth.ys').write_text(program + '\n')
        synthesis = run([tools['yosys']['path'], '-s', 'synth.ys'], output, output / 'synth.log')
        synthesis['program'] = program
        summary['synthesis'] = synthesis
        write_json(summary_path, summary)
        if synthesis['exit_code']:
            raise RuntimeError('synthesis failed; see ' + str(output / 'synth.log'))
        netlist = json.loads((output / 'synth.json').read_text())
        summary['synth_cells'] = dict(sorted(Counter(
            cell['type'] for cell in netlist['modules'][TOP]['cells'].values()).items()))
        summary['synth_json_sha256'] = digest(output / 'synth.json')
        write_json(output / 'resources.json', summary['synth_cells'])

        for seed in seeds:
            destination = output / f'seed-{seed}'
            destination.mkdir(exist_ok=True)
            command = [tools['nextpnr']['path'], '--json', str(output / 'synth.json'),
                       '--device', DEVICE, '--top', TOP,
                       '--qsf', str(snapshot / SIM / 'benchmark.qsf'),
                       '--freq', str(args.target_mhz), '--seed', str(seed), '--router', 'router2',
                       '--timing-allow-fail', '--write', str(destination / 'routed.json'),
                       '--report', str(destination / 'timing.json'), '--detailed-timing-report']
            route = {'seed': seed, **run(command, output, destination / 'route.log')}
            if route['exit_code'] == 0:
                route.update(timing_summary(destination / 'timing.json', destination / 'route.log'))
                route['output_sha256'] = {name: digest(destination / name)
                                          for name in ('routed.json', 'timing.json', 'route.log')}
                route['target_met'] = route['fmax_mhz'] >= args.target_mhz
                print(f'{args.variant} seed {seed}: {route["fmax_mhz"]:.3f} MHz; '
                      f'target {args.target_mhz:g} MHz {"met" if route["target_met"] else "missed"}', flush=True)
            summary['runs'].append(route)
            write_json(summary_path, summary)
            if route['exit_code']:
                raise RuntimeError('routing failed; see ' + str(destination / 'route.log'))
        summary['all_targets_met'] = all(route['target_met'] for route in summary['runs'])
        summary['worst_fmax_mhz'] = min(route['fmax_mhz'] for route in summary['runs'])
        summary['best_fmax_mhz'] = max(route['fmax_mhz'] for route in summary['runs'])
        write_json(summary_path, summary)
        print('Diagnostic evidence: ' + str(summary_path), flush=True)
        if args.require_target and not summary['all_targets_met']:
            print('Timing gate failed: at least one selected seed misses the target', file=sys.stderr)
            return 1
        return 0
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
