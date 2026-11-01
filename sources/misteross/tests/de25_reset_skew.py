#!/usr/bin/env python3
"""Exercise independent one-cycle FIFO reset-release delays in a private model.

Consumes a passing --startup interconnect_sim.py result. Only the private reset
synchronizer model changes: selected first stages retain reset for one extra
receiving-clock edge. This finite digital diagnostic neither models analog
metastability nor closes RDC-50003, reset/boot epochs or hardware acceptance.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
TOP = 'fes_de25_scanout_interconnect_test'
RESET = 'qsys_top/altera_reset_controller_1925/synth/altera_reset_synchronizer.v'
RESET_SHA = '00ab6af552953e87cb43cd5205a997f1657eef7bf252e3064ece684874197c6a'
CHAINS = [f'periph.{bridge}.{bridge}.{role}_fifo.my_altera_avalon_dc_fifo_{role}_fifo.{side}_reset_sync'
          for bridge in ('fes_ctrl_cdc', 'fes_mem_cdc')
          for role in ('cmd', 'rsp') for side in ('write', 'read')]
PHASES = (1, 17, 296, 297, 399, 400, 593, 799)


def sha(data):
    return hashlib.sha256(data).hexdigest()


def require(ok, message):
    if not ok:
        raise ValueError(message)


def replace(source, before, after):
    require(source.count(before) == 1, 'model derivation anchor changed: ' + before)
    return source.replace(before, after)


def constants(module):
    result = {}
    for var in module.findall('var'):
        if var.get('param') != 'true':
            continue
        value = var.find('const')
        match = re.fullmatch(r"\d+'s?h([0-9a-f]+)", value.get('name', '') if value is not None else '')
        if match:
            result[var.get('name')] = int(match[1], 16)
    return result


def check_chains(xml, injected):
    root = ET.parse(xml).getroot()
    modules = {m.get('name'): m for m in root.findall('.//module')}
    found = {}
    for cell in root.findall('.//cell'):
        module = modules.get(cell.get('submodname'))
        if module is None or module.get('origName') != 'altera_reset_synchronizer':
            continue
        values = constants(module)
        hierarchy = cell.get('hier')
        if hierarchy not in {TOP + '.' + name for name in CHAINS}:
            require(not injected or values.get('FES_DELAY_INDEX') == 0xffffffff,
                    'delay injected outside the eight FIFO chains')
            continue
        require(values.get('ASYNC_RESET') == 1 and values.get('DEPTH') == 4,
                'FIFO reset mode/depth changed: ' + hierarchy)
        index = CHAINS.index(hierarchy.removeprefix(TOP + '.'))
        if injected:
            require(values.get('FES_DELAY_INDEX') == index, 'wrong reset-delay index: ' + hierarchy)
        require(hierarchy not in found, 'duplicate FIFO reset chain')
        found[hierarchy] = values
    require(len(found) == 8, 'expected exactly eight elaborated FIFO reset chains')
    return found


def prepare(proof, output):
    receipt_data = (proof / 'receipt.json').read_bytes()
    receipt = json.loads(receipt_data)
    require(receipt.get('status') == 'pass' and len(receipt.get('cases', [])) == 72 and
            'startup' in receipt, 'passing actual parent startup proof required')
    helper = ROOT / 'boards/de25nano/scanout/interconnect_sim.py'
    require(sha(helper.read_bytes()) == receipt['helper_sha256'], 'transport helper changed')
    spec = importlib.util.spec_from_file_location('reset_skew_transport', helper)
    transport = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(transport)
    require(receipt['source_sha256'].get(RESET) == RESET_SHA, 'reset source is not the reviewed 26.3 source')
    before = {}
    for name, expected in receipt['source_sha256'].items():
        path = (proof / 'sources' / name).resolve(strict=True)
        path.relative_to((proof / 'sources').resolve(strict=True))
        data = path.read_bytes()
        require(sha(data) == expected, 'startup source changed: ' + name)
        target = output / 'sources' / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        before[path] = data
    for name, expected in ((TOP + '.v', receipt['wrapper_sha256']),
                           ('modeled_m20k_storage.v', receipt['ram_storage_model']['sha256']),
                           ('elaboration.xml', receipt['elaboration_sha256']),
                           *receipt['bench_source_sha256'].items()):
        path = proof / name
        data = path.read_bytes()
        require(sha(data) == expected, 'startup input changed: ' + name)
        before[path] = data
        (output / name).write_bytes(data)
    baseline_chains = check_chains(proof / 'elaboration.xml', False)
    model = (output / 'sources' / RESET).read_text()
    model = replace(model, 'parameter DEPTH       = 2',
                    'parameter DEPTH       = 2,\n    parameter FES_DELAY_INDEX = -1')
    model = replace(model, '    // -----------------------------------------------\n    // Synchronizer register chain.', '''    // Diagnostic-only first-stage uncertainty; original shift/output stages stay intact.
    wire fes_hold_first;
    generate if (FES_DELAY_INDEX >= 0) begin : fes_delay_model
        reg release_seen;
        always @(posedge clk or posedge reset_in)
            if (reset_in) release_seen <= 1'b0;
            else release_seen <= 1'b1;
        assign fes_hold_first = !release_seen &&
            $root.fes_de25_scanout_interconnect_test.fifo_reset_delay_mask[FES_DELAY_INDEX];
    end else begin
        assign fes_hold_first = 1'b0;
    end endgenerate

    // -----------------------------------------------
    // Synchronizer register chain.''')
    model = replace(model, 'altera_reset_synchronizer_int_chain[DEPTH-1] <= 0;',
                    'altera_reset_synchronizer_int_chain[DEPTH-1] <= fes_hold_first;')
    (output / 'sources' / RESET).write_text(model)
    wrapper = (output / (TOP + '.v')).read_text()
    wrapper = replace(wrapper, 'module ' + TOP + ' (', 'module ' + TOP + ''' (
    input wire [7:0] fifo_reset_delay_mask,
    output wire [7:0] fifo_reset_merged, fifo_reset_active,''')
    bindings = []
    for i, chain in enumerate(CHAINS):
        bindings += [f'defparam {chain}.FES_DELAY_INDEX = {i};',
                     f'assign fifo_reset_merged[{i}] = {chain}.reset_in;',
                     f'assign fifo_reset_active[{i}] = {chain}.reset_out;']
    wrapper = replace(wrapper, 'endmodule', '\n'.join(bindings) + '\nendmodule')
    (output / (TOP + '.v')).write_text(wrapper)
    bench = (output / 'interconnect_tb.cpp').read_text()
    bench = replace(bench, '        bool ar_accepted = mem_rise',
                    '        unsigned fes_reset_before = dut.fifo_reset_merged;\n        bool ar_accepted = mem_rise')
    bench = replace(bench, '        dut.eval();\n        if (pixel_rise && dut.reset_n)',
                    '        dut.eval();\n        fes_reset_audit(fes_reset_before, dut.fifo_reset_active, mem_rise, pixel_rise);\n'
                    '        if (pixel_rise && dut.reset_n)')
    (output / 'interconnect_tb.cpp').write_text(bench)
    bench_path = Path(__file__).with_name('de25_reset_skew_tb.cpp')
    (output / bench_path.name).write_bytes(bench_path.read_bytes())
    before[bench_path] = bench_path.read_bytes()
    before[helper] = helper.read_bytes()
    return receipt, receipt_data, before, transport, baseline_chains


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--startup-proof', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    proof, output = args.startup_proof.resolve(strict=True), args.output.resolve()
    require(output != proof and proof not in output.parents, 'use a new output outside the startup proof')
    output.mkdir(parents=True, exist_ok=False)
    result = {'format': 1, 'status': 'failed', 'classification': 'finite-independent-fifo-reset-release-delay-model',
              'hardware_access': 'not-performed', 'RDC_50003_closed': False,
              'physical_metastability_model': False, 'whole_reset_boot_epoch_acceptance': False}
    try:
        receipt, receipt_data, before, transport, chains = prepare(proof, output)
        result.update(parent_receipt_sha256=sha(receipt_data), input_source_sha256=receipt['source_sha256'],
                      canonical_build_inputs_sha256=receipt['build_inputs_sha256'], parent_chains=chains,
                      reset_delay_choices_cycles=[0, 1], independent_fifo_chains=8,
                      clock_phases=list(PHASES), guard_delays_system_cycles=[0, 100512],
                      model_helper_sha256=sha(Path(__file__).read_bytes()))
        def remap(command):
            return [value.replace(str(proof), str(output)) for value in command]
        elaborate = remap(receipt['elaboration_command'])
        with (output / 'elaboration.log').open('w') as log:
            subprocess.run(elaborate, check=True, stdout=log, stderr=subprocess.STDOUT)
        result['injected_chains'] = check_chains(output / 'elaboration.xml', True)
        result['ram_cells'] = transport.guard_elaborated_ram(output / 'elaboration.xml')
        command = remap(receipt['build_command'])
        require(command[-1] == str(output / 'startup_tb.cpp'), 'startup build bench differs')
        command[-1] = str(output / 'de25_reset_skew_tb.cpp')
        result['build_command'] = command
        with (output / 'build.log').open('w') as log:
            subprocess.run(command, check=True, stdout=log, stderr=subprocess.STDOUT)
        binary = output / 'obj' / ('V' + TOP)
        result['executable_sha256'] = sha(binary.read_bytes())
        with (output / 'simulation.log').open('w') as log:
            subprocess.run([str(binary)], check=True, stdout=log, stderr=subprocess.STDOUT)
        cases = [json.loads(line) for line in (output / 'simulation.log').read_text().splitlines() if line.startswith('{')]
        expected = [f'reset_skew_phase_{phase}_delay_{delay}_mask_{mask}' for phase in PHASES
                    for delay in ((0, 100512) if phase == 1 else (0,)) for mask in range(256)]
        require([c.get('case') for c in cases] == expected and all(c.get('status') == 'pass' for c in cases),
                'missing, repeated, out-of-order or failed reset-skew case')
        require(cases[0]['ram_storage_self_test_passed'], 'RAM self-test missing')
        require(all(c['axi_bursts'] and c['axi_beats'] and c['endpoint128_beats'] for c in cases
                    if '_delay_0_' in c['case']), 'healthy case did not drain actual generated transport')
        result['cases'] = cases
        with (output / 'negative-control.log').open('w') as log:
            negative = subprocess.run([str(binary), '--negative-release-monitor'], stdout=log, stderr=subprocess.STDOUT)
        require(negative.returncode == 1 and 'FIFO reset-release edge mismatch' in (output / 'negative-control.log').read_text(),
                'release monitor did not reject the missing one-cycle delay')
        require(all(p.read_bytes() == data for p, data in before.items()), 'bound input changed during proof')
        require((proof / 'receipt.json').read_bytes() == receipt_data, 'parent proof changed')
        result.update(status='pass', healthy_cases=2048, timeout_cases=256,
                      negative_release_monitor='pass: missing delay rejected')
    except (OSError, ValueError, KeyError, ET.ParseError, subprocess.CalledProcessError) as error:
        result['error'] = str(error)
    finally:
        result['derived_input_sha256'] = {str(p.relative_to(output)): sha(p.read_bytes())
                                          for p in sorted(output.rglob('*')) if p.is_file() and
                                          ('sources' in p.relative_to(output).parts or p.suffix in ('.v', '.cpp'))}
        result['log_sha256'] = {p.name: sha(p.read_bytes()) for p in output.glob('*.log')}
        (output / 'receipt.json').write_text(json.dumps(result, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'status': result['status'], 'receipt': str(output / 'receipt.json')}))
    return 0 if result['status'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
