#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Replay settled half-clock NMOS Z80 pin captures through the real RTL wrapper.

Inputs are levels immediately BEFORE an edge; outputs are levels sampled AFTER
that edge plus sample_delay_ns. The first row anchors the first opcode's T1
rising edge after reset; physical RESET release latency is deliberately outside
this digital integration contract. No CPU implementation supplies expectations.
Use --self-test for an explicitly synthetic Zilog-manual NOP waveform, or pass
operator-supplied trace JSON. --require-physical rejects synthetic expectations.
"""
import argparse
import copy
import csv
import hashlib
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
RTL = ROOT / 'cores/fes-common/rtl/z80'
SIM = ROOT / 'cores/fes-common/sim/z80'
INPUTS = ('din', 'wait_n', 'int_n', 'nmi_n', 'busrq_n')
CONTROLS = ('m1_n', 'mreq_n', 'iorq_n', 'rd_n', 'wr_n', 'rfsh_n', 'halt_n', 'busak_n')
OUTPUTS = ('a', 'dout', *CONTROLS)
CSV_FIELDS = ('edge_time_ns', 'edge', *INPUTS, *OUTPUTS)
FORMAT = 'fes-z80-pin-trace-v1'


def integer(value, maximum, label):
    if type(value) is not int or not 0 <= value <= maximum:
        raise ValueError('invalid integer: ' + label)
    return value


def real(value, label):
    if type(value) not in (int, float) or not math.isfinite(value):
        raise ValueError('invalid finite number: ' + label)
    return value


def text_field(source, field):
    if not isinstance(source.get(field), str) or not source[field].strip():
        raise ValueError('missing nonempty source.' + field)


def unique_object(pairs):
    value = {}
    for key, item in pairs:
        if key in value: raise ValueError('duplicate JSON key: ' + key)
        value[key] = item
    return value


def load_trace(path, csv_path=None):
    trace = json.loads(path.read_text(), object_pairs_hook=unique_object)
    if csv_path is not None:
        if not isinstance(trace, dict) or 'samples' in trace:
            raise ValueError('CSV uses metadata JSON without a samples field')
        with csv_path.open(newline='') as stream:
            reader = csv.DictReader(stream)
            if tuple(reader.fieldnames or ()) != CSV_FIELDS:
                raise ValueError('CSV columns must be ' + ','.join(CSV_FIELDS))
            samples = []
            for row in reader:
                if None in row or any(value is None for value in row.values()):
                    raise ValueError('wrong CSV column count')
                values = {key: None if row[key] == '' else int(row[key], 16) if row[key].lower().startswith('0x')
                          else int(row[key]) for key in (*INPUTS, *OUTPUTS)}
                samples.append({'edge_time_ns': float(row['edge_time_ns']), 'edge': row['edge'],
                                'inputs': {key: values[key] for key in INPUTS},
                                'outputs': {key: values[key] for key in OUTPUTS}})
        trace['samples'] = samples
    return trace


def validate(trace, require_physical=False):
    if not isinstance(trace, dict) or trace.get('format') != FORMAT:
        raise ValueError('unsupported pin-trace format')
    source = trace.get('source')
    if not isinstance(source, dict) or source.get('kind') not in ('physical', 'synthetic'):
        raise ValueError('source.kind must be physical or synthetic')
    text_field(source, 'description')
    if require_physical and source['kind'] != 'physical':
        raise ValueError('physical qualification requires a physical capture; synthetic data rejected')
    if source['kind'] == 'physical':
        for field in ('manufacturer', 'technology', 'part', 'capture_tool', 'capture_sha256', 'initialization'):
            text_field(source, field)
        if source['manufacturer'] != 'Zilog' or source['technology'] != 'NMOS':
            raise ValueError('physical baseline is original Zilog NMOS')
        if not re.fullmatch('[0-9a-f]{64}', source['capture_sha256']):
            raise ValueError('source.capture_sha256 must identify the original raw capture')
    clock_hz = integer(trace.get('clock_hz'), 10_000_000, 'clock_hz')
    if clock_hz == 0: raise ValueError('clock_hz must be positive')
    half_period = 500_000_000 / clock_hz
    delay = real(trace.get('sample_delay_ns'), 'sample_delay_ns')
    tolerance = real(trace.get('timestamp_tolerance_ns'), 'timestamp_tolerance_ns')
    if not 0 < delay < half_period or not 0 <= tolerance < half_period / 4:
        raise ValueError('settling delay/timestamp tolerance outside half-clock window')
    if trace.get('start') != 'first-m1-t1-after-reset':
        raise ValueError('trace must start at first-m1-t1-after-reset')
    samples = trace.get('samples')
    if not isinstance(samples, list) or not 9 <= len(samples) <= 1_000_000:
        raise ValueError('trace needs 9..1000000 consecutive half-edge samples')
    previous_time = None
    for index, sample in enumerate(samples):
        label = 'sample[' + str(index) + ']'
        if not isinstance(sample, dict): raise ValueError('invalid ' + label)
        if sample.get('edge') != ('rise' if index % 2 == 0 else 'fall'):
            raise ValueError('missing/duplicate/wrong clock edge at ' + label)
        time = real(sample.get('edge_time_ns'), label + '.edge_time_ns')
        if time < 0 or (previous_time is not None and abs(time - previous_time - half_period) > tolerance):
            raise ValueError('missing edge or inconsistent clock interval at ' + label)
        previous_time = time
        inputs, outputs = sample.get('inputs'), sample.get('outputs')
        if not isinstance(inputs, dict) or set(inputs) != set(INPUTS):
            raise ValueError('all five input channels required at ' + label)
        if not isinstance(outputs, dict) or set(outputs) != set(OUTPUTS):
            raise ValueError('all output channels required at ' + label)
        for field in INPUTS: integer(inputs[field], 255 if field == 'din' else 1, label + '.' + field)
        for field in CONTROLS: integer(outputs[field], 1, label + '.' + field)
        for field in ('a', 'dout'):
            if outputs[field] is not None: integer(outputs[field], 65535 if field == 'a' else 255, label + '.' + field)
        selected = outputs['busak_n'] and (not outputs['mreq_n'] or not outputs['iorq_n'] or not outputs['rfsh_n'])
        if selected and outputs['a'] is None:
            raise ValueError('selected/refresh address cannot be omitted at ' + label)
        if outputs['busak_n'] and not outputs['wr_n']:
            if outputs['dout'] is None: raise ValueError('write data cannot be omitted at ' + label)
        elif outputs['dout'] is not None:
            raise ValueError('dout is only meaningful during CPU writes at ' + label)
    first = samples[0]
    if (first['outputs'] != dict(a=0, dout=None, m1_n=0, mreq_n=1, iorq_n=1,
                                rd_n=1, wr_n=1, rfsh_n=1, halt_n=1, busak_n=1)
            or any(first['inputs'][field] != 1 for field in INPUTS[1:])):
        raise ValueError('first sample is not an unblocked post-reset T1 anchor at PC0')
    return trace


def write_stream(trace, path):
    with path.open('w') as stream:
        for row in trace['samples']:
            numbers = [int(row['edge'] == 'rise'), *(row['inputs'][key] for key in INPUTS),
                       *(row['outputs'][key] if row['outputs'][key] is not None else -1 for key in OUTPUTS)]
            stream.write(' '.join(map(str, numbers)) + '\n')


def run(binary, trace, output, detail_limit):
    stream = output / 'samples.txt'
    write_stream(trace, stream)
    result = subprocess.run([str(binary), str(stream), str(detail_limit)], cwd=ROOT,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    return result


def self_test(binary, trace, output):
    result = run(binary, trace, output, 20)
    if result.returncode: raise ValueError('synthetic smoke waveform failed:\n' + result.stdout)
    (output / 'result.log').write_text(result.stdout)
    print(result.stdout, end='')
    metadata = {key: value for key, value in trace.items() if key != 'samples'}
    metadata_path, csv_path = output / 'smoke-metadata.json', output / 'smoke-samples.csv'
    metadata_path.write_text(json.dumps(metadata, indent=2) + '\n')
    with csv_path.open('w', newline='') as stream:
        writer = csv.DictWriter(stream, fieldnames=CSV_FIELDS)
        writer.writeheader()
        for row in trace['samples']:
            writer.writerow({'edge_time_ns': row['edge_time_ns'], 'edge': row['edge'],
                             **row['inputs'], **row['outputs']})
    converted = validate(load_trace(metadata_path, csv_path))
    if converted != trace: raise ValueError('CSV changed the sample values')
    result = run(binary, converted, output, 20)
    if result.returncode: raise ValueError('CSV smoke replay failed:\n' + result.stdout)
    (output / 'csv-result.log').write_text(result.stdout)
    negatives = 0
    for field, index, replacement in (('mreq_n', 1, 1), ('rfsh_n', 4, 1), ('a', 2, 1)):
        changed = copy.deepcopy(trace)
        changed['samples'][index]['outputs'][field] = replacement
        validate(changed)
        result = run(binary, changed, output, 1)
        (output / ('negative-' + field + '.log')).write_text(result.stdout)
        if result.returncode != 1: raise ValueError('negative ' + field + ' did not fail')
        negatives += 1
    corrupt = []
    for field, value in (('samples', []), ('clock_hz', 0), ('sample_delay_ns', -1)):
        changed = copy.deepcopy(trace); changed[field] = value; corrupt.append(changed)
    changed = copy.deepcopy(trace); changed['samples'][1]['edge'] = 'rise'; corrupt.append(changed)
    changed = copy.deepcopy(trace); changed['samples'][1]['outputs'].pop('rd_n'); corrupt.append(changed)
    changed = copy.deepcopy(trace); changed['samples'][1]['outputs']['a'] = None; corrupt.append(changed)
    changed = copy.deepcopy(trace); changed['source']['kind'] = 'physical'; corrupt.append(changed)
    for changed in corrupt:
        try: validate(changed)
        except ValueError: negatives += 1
        else: raise ValueError('malformed trace was accepted')
    try: validate(trace, require_physical=True)
    except ValueError: negatives += 1
    else: raise ValueError('synthetic trace accepted as physical evidence')
    write_stream(trace, output / 'samples.txt')
    print(f'Pin replay self-test PASS: synthetic smoke and {negatives} negative controls; no physical acceptance.')


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--trace', type=Path)
    parser.add_argument('--samples-csv', type=Path, help='normalized CSV; --trace then supplies metadata only')
    parser.add_argument('--raw-capture', type=Path, help='optionally verify original capture bytes against source.capture_sha256')
    parser.add_argument('--require-physical', action='store_true')
    parser.add_argument('--self-test', action='store_true')
    parser.add_argument('--max-detailed-failures', type=int, default=20)
    parser.add_argument('--jobs', type=int, default=2)
    parser.add_argument('--verilator', default=os.environ.get('VERILATOR', 'verilator'))
    args = parser.parse_args()
    if bool(args.trace) == args.self_test: parser.error('choose --trace or --self-test')
    if args.self_test and (args.samples_csv or args.raw_capture or args.require_physical):
        parser.error('--self-test is synthetic and does not use capture arguments')
    if not 1 <= args.jobs <= 32 or args.max_detailed_failures < 0: parser.error('invalid jobs/detail limit')
    if os.environ.get('FES_TOOLCHAIN_CACHE_ROOT') or os.environ.get('CACHE_ROOT'):
        parser.error('simulation does not use the shared compiler cache')
    output = ROOT / 'build/sim/fes-z80-pin-trace'
    try:
        trace = validate(load_trace(SIM / 'pin-trace-smoke.json' if args.self_test else args.trace,
                                    args.samples_csv), args.require_physical)
        if args.raw_capture:
            if hashlib.sha256(args.raw_capture.read_bytes()).hexdigest() != trace['source'].get('capture_sha256'):
                raise ValueError('raw capture does not match source.capture_sha256')
        output.mkdir(parents=True, exist_ok=True)
        files = [RTL / name for name in ('fes_z80_alu.sv', 'fes_z80_engine.sv', 'fes_z80_bus.sv', 'fes_z80_nmos.sv')]
        source_files = [*files, SIM / 'pin_trace_tb.cpp', Path(__file__).resolve()]
        identities = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
                      for path in source_files}
        build = output / 'model'
        command = [args.verilator, '--cc', '--exe', '--build', '--top-module', 'fes_z80_nmos', '-Wall',
                   '-j', str(args.jobs), '--Mdir', str(build), '-CFLAGS', '-std=c++17 -O2',
                   *(str(path) for path in files), str(SIM / 'pin_trace_tb.cpp')]
        with (output / 'build.log').open('w') as stream:
            result = subprocess.run(command, cwd=ROOT, stdout=stream, stderr=subprocess.STDOUT)
        if result.returncode:
            sys.stderr.write((output / 'build.log').read_text()); return result.returncode
        if identities != {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
                          for path in source_files}:
            raise ValueError('RTL/checker sources changed while the model was being built')
        (output / 'inputs.json').write_text(json.dumps({'source': trace['source'], 'clock_hz': trace['clock_hz'],
            'sample_delay_ns': trace['sample_delay_ns'], 'sources': identities,
            'normalized_trace_sha256': hashlib.sha256(json.dumps(trace, sort_keys=True).encode()).hexdigest()},
            indent=2, sort_keys=True) + '\n')
        binary = build / 'Vfes_z80_nmos'
        if args.self_test:
            self_test(binary, trace, output); return 0
        print('Source:', trace['source']['kind'], '-', trace['source']['description'], flush=True)
        result = run(binary, trace, output, args.max_detailed_failures)
        (output / 'result.log').write_text(result.stdout)
        print(result.stdout, end='')
        print('Digital samples after clock edges only; RESET release latency, electrical high-Z and analog timing excluded.')
        if trace['source']['kind'] == 'synthetic': print('Synthetic expectations; no physical acceptance.')
        else: print('Physical provenance supplied by operator; this trace does not establish complete chip equivalence.')
        return result.returncode
    except (OSError, ValueError, KeyError, json.JSONDecodeError, csv.Error) as error:
        print('Pin-trace qualification error:', error, file=sys.stderr); return 2


if __name__ == '__main__':
    raise SystemExit(main())
