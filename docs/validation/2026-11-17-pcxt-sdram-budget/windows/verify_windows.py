#!/usr/bin/env python3
"""Prove controller windows and trace the unchanged production storage pipeline."""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import threading
import time
import tomllib

HERE = Path(__file__).resolve().parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def run(work, name, command, expected=0):
    stage = work / name
    stage.mkdir()
    if shutil.disk_usage(stage).free < 30 * 1024**3:
        raise RuntimeError('refuse: under 30 GiB free')
    start, stop = time.monotonic(), threading.Event()
    (stage / 'command.json').write_text(json.dumps({
        'command': list(map(str, command)), 'cwd': str(work), 'cap_seconds': 600,
        'start_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    }, indent=2) + '\n')

    def heartbeat():
        while not stop.is_set():
            (stage / 'heartbeat.json').write_text(json.dumps({
                'pid': os.getpid(), 'elapsed_s': round(time.monotonic() - start),
            }) + '\n')
            if stop.wait(15):
                return

    threading.Thread(target=heartbeat, daemon=True).start()
    with (stage / 'log').open('w') as log:
        child = subprocess.Popen(list(map(str, command)), cwd=work,
                                 stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            rc = child.wait(timeout=600)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGTERM)
            try:
                child.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
            rc = 124
    stop.set()
    (stage / 'result.json').write_text(json.dumps({
        'returncode': rc, 'elapsed_s': round(time.monotonic() - start, 3),
    }) + '\n')
    if rc != expected:
        raise RuntimeError(f'{name}: expected rc {expected}, got {rc}; see {stage}/log')
    return (stage / 'log').read_text()


def verilator_identity(selected):
    # A trusted wrapper always selects its sibling engine with these overrides
    # unset. Do not authenticate one executable and run a PATH/env substitute.
    if any(os.environ.get(k) for k in ('VERILATOR_BIN', 'VERILATOR_ROOT')):
        raise ValueError('unset Verilator engine/root overrides for this evidence run')
    found = shutil.which(str(selected))
    if found is None:
        raise ValueError('Verilator executable not found')
    wrapper = Path(found).resolve()
    engine = wrapper.with_name('verilator_bin').resolve()
    reference = json.loads((HERE / 'verilator-reference.json').read_text())
    if sha(wrapper) != reference['wrapper_sha256'] or sha(engine) != reference['engine_sha256']:
        raise ValueError('Verilator wrapper/engine differs from the recorded trace tool')
    version = subprocess.check_output([str(wrapper), '--version'], text=True, timeout=10).strip()
    if not version.startswith(reference['version_prefix']):
        raise ValueError('Verilator version differs from the recorded trace tool')
    sdk = subprocess.check_output([str(wrapper), '--getenv', 'VERILATOR_ROOT'], text=True, timeout=10).strip()
    return {'executable': str(wrapper), 'wrapper_sha256': sha(wrapper),
            'engine': str(engine), 'engine_sha256': sha(engine),
            'version': version, 'sdk_root': sdk,
            'reference_sha256': sha(HERE / 'verilator-reference.json')}


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for name in ('module-root', 'record', 'yosys', 'work', 'output'):
        p.add_argument('--' + name, type=Path, required=True)
    p.add_argument('--verilator', default='verilator')
    args = p.parse_args()
    root, work = args.module_root.resolve(), args.work.resolve()
    verilator = verilator_identity(args.verilator)
    work.mkdir(parents=True, exist_ok=True)
    record = json.loads(args.record.read_text())
    assert record['parameters']['system_clock_hz'] == 100000000
    assert record['parameters']['half_rate_board'] is False
    assert all(sha(root / name) == value for name, value in record['source_inputs'].items())
    lock = tomllib.loads((root / 'cores/fes-pcxt/quartus.lock').read_text())
    assert sha(args.yosys) == lock['yosys']['sha256']
    controller = root / 'cores/fes-ramtest/rtl/sdram_addon_port.v'
    frontend = root / 'cores/fes-pcxt/rtl/pcxt_memory.sv'
    capture = root / 'cores/fes-pcxt/sim/memory_top.sv'
    script = work / 'prove.ys'
    script.write_text('read_verilog -formal -sv ' + ' '.join(
        json.dumps(str(x)) for x in (controller, HERE / 'window_contract.sv')) + '\n' +
        'prep -top write_contract -flatten\nmemory_map\nopt_clean\nasync2sync\ndffunmap\nchformal -lower\n' +
        'sat -seq 8 -tempinduct -maxsteps 40 -prove-asserts -verify -timeout 120 -show-ports\n')
    proof = run(work, 'formal', [args.yosys.resolve(), '-s', script])
    assert 'Induction step proven: SUCCESS!' in proof
    original = controller.read_text()
    assert original.count('dq_out_p <= dq_out_c;') == 1
    assert original.count("rdata <= (rate == 2'd0) ? dq_rise : dq_fall;") == 2
    cases = [
        ('positive', controller, 0, None),
        ('negative-write', work / 'negative-write.v', 1, 'DQ not held 3 cycles before WRITE'),
        ('negative-read', work / 'negative-read.v', 1, 'rdata did not select old controller input stage'),
    ]
    cases[1][1].write_text(original.replace('dq_out_p <= dq_out_c;', "dq_out_p <= dq_out_c ^ {11'b0,state};"))
    cases[2][1].write_text(original.replace("rdata <= (rate == 2'd0) ? dq_rise : dq_fall;", 'rdata <= dq_rise;'))
    traces = {}
    for name, selected, expected, diagnostic in cases:
        obj = work / ('obj-' + name)
        run(work, 'build-' + name, [verilator['executable'], '--cc', '--exe', '--build', '--top-module',
            'contract_top', '-Wno-fatal', '-j', '2', '--Mdir', obj, selected, frontend, capture,
            HERE / 'trace_top.sv', HERE / 'trace.cpp'])
        log = run(work, name, [obj / 'Vcontract_top'], expected=expected)
        if diagnostic:
            assert diagnostic in log
        else:
            traces = [json.loads(x) for x in log.splitlines() if x.startswith('{')]
            assert traces[-1] == {'status': 'PASS', 'writes': 257, 'reads': 256, 'read_consumptions': 256}
    rows = {}
    for event, field in [('WRITE', 'data_stable_halfcycles'), ('WRITE', 'oe_stable_halfcycles'),
                         ('OE_RELEASE', 'after_write_halfcycles'), ('OE_ASSERT', 'after_read_halfcycles'),
                         ('CONSUME', 'sample_delay_halfcycles'), ('CONSUME', 'consume_delay_halfcycles')]:
        values = [r[field] for r in traces if r.get('event') == event]
        rows[event + '/' + field] = {'count': len(values), 'min': min(values), 'max': max(values)}
    base = json.loads((HERE.parent / 'summary.json').read_text())
    assert base['record_sha256'] == sha(args.record)
    result = {
        'classification': 'inductive RTL window proof and bounded tagged-pin trace; not physical signoff',
        'base_summary_sha256': sha(HERE.parent / 'summary.json'),
        'record_sha256': sha(args.record), 'recorded_source_inputs_equal': len(record['source_inputs']),
        'source_sha256': {str(x.relative_to(root)): sha(x) for x in (controller, frontend, capture)},
        'fixture_sha256': {x.name: sha(x) for x in HERE.iterdir() if x.is_file() and x.name != 'windows.json'},
        'verilator': verilator,
        'yosys_sha256': sha(args.yosys), 'formal': 'PASS: temporal induction',
        'formal_write': 'DQ/OE stable three prior cycles; A11/A12 masks stable two prior cycles',
        'formal_read': 'No reset in read window: five cycles after sampled READ pins, rdata equals the previous controller input sample',
        'trace': rows, 'negative_write': 'rejected after real initialization', 'negative_read': 'rejected after real initialization',
        'write_earlier_launch_cycles': 3,
        'algebraic_held_write_setup_ns': {str(r['seed']): round(r['minima']['write_dq']['setup']['slack_ns'] + 30, 3) for r in base['runs']},
        'read_setup_unchanged_ns': {str(r['seed']): r['minima']['read']['setup']['slack_ns'] for r in base['runs']},
        'producer_constraints_changed': False, 'physical_budget_complete': False,
    }
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    print('PASS: induction, 257 writes/256 reads, frontend readback tags and two rejecting mutations.')


if __name__ == '__main__':
    main()
