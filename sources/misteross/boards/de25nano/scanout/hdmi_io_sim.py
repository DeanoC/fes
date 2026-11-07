#!/usr/bin/env python3
"""Test the retained HDMI initializer with a digital I2C slave; never program."""
from pathlib import Path
import argparse
import hashlib
import importlib.util
import json
import re
import subprocess
import time

HERE = Path(__file__).resolve().parent


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def checked(command, output, *, cwd=None, timeout=60):
    with output.open('wb') as log:
        subprocess.run(list(map(str, command)), cwd=cwd, stdout=log, stderr=subprocess.STDOUT,
                       check=True, timeout=timeout)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--yosys', type=Path, required=True)
    parser.add_argument('--verilator', default='verilator')
    args = parser.parse_args()
    project = args.project.resolve(strict=True)
    output = args.output.resolve()
    if output == project or project in output.parents:
        parser.error('proof output must be outside the producer project')
    output.mkdir(parents=True, exist_ok=False)
    start = time.monotonic()
    receipt = {'classification': 'HDMI digital protocol and reset-modeled controller equivalence',
        'status': 'failed', 'hardware_access': 'not-performed',
        'not_proven': ['physical IO timing/electrical behavior', 'metastability/MTBF',
                       'asynchronous reset timing', 'PLL behavior', 'video capture', 'normal-kit acceptance']}
    try:
        inputs_path = project.parent / 'inputs.json'
        inputs_sha = sha(inputs_path)
        inputs = json.loads(inputs_path.read_text())['scanout']['hdmi_inputs']
        vendor = project.parents[2] / 'hdmi-vendor-inputs'
        vendor_bytes = {name: (vendor / name).read_bytes() for name in inputs['vendor_source_sha256']}
        for name, digest in inputs['vendor_source_sha256'].items():
            if hashlib.sha256(vendor_bytes[name]).hexdigest() != digest:
                raise ValueError('retained vendor source differs')
        for name, digest in inputs['derived_source_sha256'].items():
            if sha(project / 'fes_mailbox' / name) != digest:
                raise ValueError('derived compiler input differs')
        raw_config = vendor_bytes['I2C_HDMI_Config.v'].decode()
        rows = re.findall(r"^\s*(\d+)\s*:\s*LUT_DATA\s*<=\s*16'h([0-9a-fA-F]{4})", raw_config, re.M)
        lut = {int(n): int(v, 16) for n, v in rows if int(n) < 33}
        if sorted(lut) != list(range(33)) or len(rows) != 33:
            raise ValueError('expected exactly the 33 selected vendor LUT rows')
        (output / 'expected_lut.h').write_text('static const unsigned expected_lut[33] = {' +
            ','.join(hex(lut[n]) for n in range(33)) + '};\n')
        files = [project / 'fes_mailbox' / name for name in inputs['derived_source_sha256']]
        files += [project / 'fes_mailbox/fes_de25_scanout_board.v', project / 'fes_mailbox/fes_de25_top_reset.v',
                  HERE / 'hdmi_io_tb.v', HERE / 'hdmi_io_tb.cpp', HERE.parent / 'display/sim_models.v']
        helper_path = HERE / 'hdmi_inputs.py'
        before = {str(p): sha(p) for p in [*files, helper_path]}
        before.update({str(vendor / n): digest for n, digest in inputs['vendor_source_sha256'].items()})
        before[str(inputs_path)] = inputs_sha
        command = [args.verilator, '--cc', '--exe', '--build', '-O3', '--top-module', 'fes_de25_hdmi_io_tb',
            '--Mdir', str(output / 'obj'), '-Wno-DECLFILENAME', '-Wno-UNUSEDSIGNAL', '-Wno-PINCONNECTEMPTY',
            # The retained vendor cell leaves three unused test/direction pins
            # open, uses a bounded 16-bit divider comparison and a hold-default
            # case. Its combinational LUT uses nonblocking assignment syntax.
            '-Wno-PINMISSING', '-Wno-WIDTHEXPAND', '-Wno-CASEINCOMPLETE', '-Wno-COMBDLY',
            '-Wno-SYNCASYNCNET', '-CFLAGS', '-O2 -I' + str(output), *files]
        checked(command, output / 'build.log')
        executable = output / 'obj/Vfes_de25_hdmi_io_tb'
        checked([executable], output / 'simulation.log')
        cases = [json.loads(line) for line in (output / 'simulation.log').read_text().splitlines()]
        if [c['case'] for c in cases] != ['hdmi-inputs-nack-' + str(n) for n in (3, 0, 1, 2)] or any(c['status'] != 'pass' for c in cases):
            raise ValueError('incomplete HDMI protocol cases')
        spec = importlib.util.spec_from_file_location('hdmi_proof_derivation', helper_path)
        helper = importlib.util.module_from_spec(spec); spec.loader.exec_module(helper)
        gold = helper.controller_inputs(vendor_bytes['I2C_Controller.v'])
        gate = (project / 'fes_mailbox/fes_de25_scanout_i2c_controller.v').read_text()
        for name, text in [('gold', gold), ('gate', gate)]:
            (output / (name + '.v')).write_text(text.replace('module fes_de25_scanout_i2c_controller', 'module ' + name, 1))
        proof = 'read_verilog gold.v gate.v; proc; opt; async2sync; opt; equiv_make gold gate equiv; hierarchy -top equiv; equiv_simple; equiv_induct -seq 2; equiv_status -assert'
        checked([args.yosys, '-Q', '-T', '-p', proof], output / 'equivalence.log', cwd=output, timeout=45)
        if '40 are proven and 0 are unproven' not in (output / 'equivalence.log').read_text():
            raise ValueError('controller equivalence inventory changed')
        if any(sha(Path(name)) != digest for name, digest in before.items()):
            raise ValueError('proof source changed during validation')
        receipt.update(status='pass', cases=cases, compiled_source_sha256=before,
            executable_sha256=sha(executable), vendor_inputs=inputs,
            controller_equivalence={'equiv_cells': 40, 'unproven': 0, 'reset_model': 'async2sync',
                'gold_sha256': sha(output / 'gold.v'), 'gate_sha256': sha(output / 'gate.v'),
                'yosys_sha256': sha(args.yosys.resolve()), 'proof': proof},
            source_clock_hz=50000000, divider_edges=5002, hardware_acceptance=False,
            slave_model='open-drain ACK delayed 3.51 us within the low phase; no analog model',
            verilator_version=subprocess.check_output([args.verilator, '--version'], text=True).strip(),
            yosys_version=subprocess.check_output([args.yosys, '-V'], text=True).strip())
    finally:
        receipt['elapsed_seconds'] = round(time.monotonic() - start, 3)
        (output / 'receipt.json').write_text(json.dumps(receipt, sort_keys=True, indent=2) + '\n')
    print(json.dumps({'status': receipt['status'], 'cases': len(receipt['cases']), 'seconds': receipt['elapsed_seconds']}))


if __name__ == '__main__':
    main()
