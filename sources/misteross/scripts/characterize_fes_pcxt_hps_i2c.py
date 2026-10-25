#!/usr/bin/env python3
"""Characterize HPS boundary/pad paths with Quartus; never programs hardware."""
import argparse
from decimal import Decimal
import hashlib
import json
from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = ROOT / 'tests/fixtures/pcxt_hps_i2c_timing'
FILES = ('top.sv', 'top.qsf', 'top.qpf', 'clocks.sdc', 'audit.tcl')
CORNERS = ('7_slow_1100mv_100c', '7_slow_1100mv_-40c',
           'MIN_fast_1100mv_100c', 'MIN_fast_1100mv_-40c')
PINS = {'SCL': ('HDMI_I2C_SCL', 'out_clk', 'scl', 'PIN_U10'),
        'SDA': ('HDMI_I2C_SDA', 'out_data', 'sda', 'PIN_AA4')}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def normalized_source(name, text):
    if name == 'top.qsf':
        text = '\n'.join(line for line in text.splitlines() if line.strip() and not re.fullmatch(
            r'set_global_assignment -name LAST_QUARTUS_VERSION "17\.0\.2 Lite Edition"', line)) + '\n'
    return text


def sources(load_pf):
    require(load_pf in (0, 30), 'only the declared 0/30pF reference endpoints are supported')
    result = {name: normalized_source(name, (FIXTURE / name).read_text()) for name in FILES}
    result['top.qsf'] = result['top.qsf'].replace('BOARD_MODEL_FAR_C 0P', f'BOARD_MODEL_FAR_C {load_pf}P')
    return result


def parse_path(text, tag, direction, edge, bound, corner):
    require(tag in PINS and direction in ('out', 'in') and edge in ('rise', 'fall')
            and bound in ('early', 'late') and corner in CORNERS, 'invalid path identity')
    blocks = re.split(r'(?m)^Path #\d+:', text)[1:]
    require(len(blocks) == 1, 'expected exactly one complete reference path')
    block = blocks[0]

    def field(name):
        values = re.findall(r'(?m)^;\s*' + name + r'\s*;\s*([^;\n]+)', block)
        require(len(values) == 1, 'missing or duplicate ' + name)
        return values[0].strip()

    port, output, feedback, pin = PINS[tag]
    expected = ('hdmi_i2c|' + output, port) if direction == 'out' else (port, 'hdmi_i2c|' + feedback)
    require((field('From Node'), field('To Node')) == expected, 'wrong HPS/pad endpoint')
    delay = Decimal(field('Delay'))
    require(delay.is_finite() and delay >= 0, 'invalid path delay')
    points = []
    arrival = block.split('; Data Arrival Path')
    require(len(arrival) == 2, 'missing arrival path')
    for line in arrival[1].splitlines():
        columns = [value.strip() for value in line.split(';')[1:-1]]
        if len(columns) != 7 or not re.fullmatch(r'-?\d+\.\d+', columns[0]):
            continue
        if columns[3] not in ('', 'RE', 'IC', 'CELL'):
            continue
        total, incr = Decimal(columns[0]), Decimal(columns[1])
        require(total.is_finite() and incr.is_finite(), 'nonfinite physical point')
        if not columns[3]:
            if columns[6] == 'data path':
                continue
            require(not points and columns[6] == expected[0], 'wrong source point')
        if points:
            require(abs(total - Decimal(points[-1]['total_ns']) - incr) <= Decimal('.002'),
                    'inconsistent physical delay accumulation')
        points.append(dict(total_ns=str(total), incremental_ns=str(incr), transition=columns[2],
                           type=columns[3], location=columns[5], element=columns[6]))
    require(points and points[0]['element'] == expected[0] and points[-1]['element'] == expected[1],
            'incomplete physical path')
    require(Decimal(points[-1]['total_ns']) == delay, 'summary/physical delay mismatch')

    def one(kind, element):
        matches = [p for p in points if p['type'] == kind and p['element'] == element]
        require(len(matches) == 1, 'missing or duplicate buffer boundary: ' + element)
        return matches[0]

    if direction == 'out':
        ingress = one('IC', port + '~output|oe')
        buffer = one('CELL', port + '~output|o')
        require(buffer['location'].startswith('IOOBUF_') and points[-1]['location'] == pin,
                'wrong output buffer/pad')
        require(points[0]['location'] == 'HPSINTERFACEPERIPHERALI2C_X52_Y60_N111', 'wrong HPS site')
        require(buffer['transition'].endswith('R' if edge == 'rise' else 'F'), 'wrong output transition')
    else:
        ingress = one('IC', port + '~input|i')
        buffer = one('CELL', port + '~input|o')
        require(buffer['location'].startswith('IOIBUF_') and points[0]['location'] == pin,
                'wrong input buffer/pad')
        require(points[-1]['location'] == 'HPSINTERFACEPERIPHERALI2C_X52_Y60_N111', 'wrong HPS feedback site')
        require(ingress['transition'].startswith('R' if edge == 'rise' else 'F'), 'wrong input transition')
    return dict(corner=corner, pin=tag, direction=direction, edge=edge, bound=bound,
                source=expected[0], destination=expected[1], delay_ns=str(delay),
                buffer_ingress_ns=ingress['total_ns'], buffer_cell_ns=buffer['incremental_ns'], points=points)


def summarize_project(project, load_pf):
    expected = sources(load_pf)
    for name, content in expected.items():
        require(normalized_source(name, (project / name).read_text()) == content,
                'reference source differs: ' + name)
    fit = (project / 'output_files/top.fit.rpt').read_text(encoding='latin1')
    require('17.0.2 Build 602' in fit and 'Fitter was successful' in fit
            and re.search(r';\s*Timing Models\s*;\s*Final\s*;', fit),
            'requires a successful final-model fit')
    decoded = (project / 'decoded.bt').read_text()
    require(not re.search(r'^s GPIO\.(?:004|006)\.000:USE_OPEN_DRAIN\.[02] 1\b', decoded, re.M),
            'dedicated open-drain implementation does not match native OE')
    for source, destination in [('OUT_CLK', 'GPIO.006.000.0:OEIN.0'),
                                ('OUT_DATA', 'GPIO.004.000.2:OEIN.0')]:
        require(destination in decoded and 'HPS_PERIPHERAL_I2C.052.060:' + source in decoded,
                'missing decoded HPS/OE route endpoint')
    observations, reports = [], {}
    for corner in CORNERS:
        for tag in PINS:
            for direction in ('out', 'in'):
                for edge in ('rise', 'fall'):
                    for bound in ('early', 'late'):
                        path = project / 'reports' / corner / f'{tag}-{direction}-{edge}-{bound}.rpt'
                        observations.append(parse_path(path.read_text(encoding='latin1'), tag, direction,
                                                       edge, bound, corner))
                        reports[str(path.relative_to(project))] = digest(path)
    require(len(observations) == 64, 'incomplete corner/edge/direction inventory')
    for late in [row for row in observations if row['bound'] == 'late']:
        early = next(row for row in observations if all(row[k] == late[k] for k in
                     ('corner', 'pin', 'direction', 'edge')) and row['bound'] == 'early')
        require(Decimal(early['delay_ns']) <= Decimal(late['delay_ns']), 'reversed early/late bounds')
    return dict(scope='Fitted HPS atom-boundary/pad observations; no hard-IP clock-to-output, capture, native-route or board acceptance.',
                output_load_pf=load_pf, device='5CSEBA6U23I7', observations=observations,
                source_sha256={name: hashlib.sha256(content.encode()).hexdigest() for name, content in expected.items()},
                reports_sha256=reports, fitted_rbf_sha256=digest(project / 'output_files/top.rbf'),
                decoded_sha256=digest(project / 'decoded.bt'), reference_coverage_complete=True,
                hard_ip_launch_bounds_ns=None, hard_ip_capture_bounds_ns=None,
                physical_qualified=False, hardware=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--quartus-bin', type=Path, required=True)
    parser.add_argument('--mistral-cv', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--reuse-fits', action='store_true', help='validate every input and parse retained load0/load30 fits')
    args = parser.parse_args()
    version = subprocess.check_output([str(args.quartus_bin / 'quartus_sh'), '--version'], text=True)
    require('Version 17.0.2 Build 602' in version, 'requires Quartus 17.0.2 Build 602')
    args.output.mkdir(parents=True, exist_ok=args.reuse_fits)
    require(not (args.output / 'reference.json').exists(), 'reference output already exists')
    result = dict(scope=__doc__, quartus_version=version, projects={}, hardware=False, physical_qualified=False)
    for load in (0, 30):
        project = args.output / f'load{load}'
        if not args.reuse_fits:
            project.mkdir()
            for name, content in sources(load).items():
                (project / name).write_text(content)
            binding = {name:digest(project / name) for name in FILES}
            (project / 'input-binding.json').write_text(json.dumps(binding, indent=2) + '\n')
            commands = [('compile', [str(args.quartus_bin / 'quartus_sh'), '--flow', 'compile', 'top']),
                        ('audit', [str(args.quartus_bin / 'quartus_sta'), '-t', 'audit.tcl']),
                        ('convert', [str(args.quartus_bin / 'quartus_cpf'), '-c', '-o', 'bitstream_compression=on',
                                     'output_files/top.sof', 'output_files/top.rbf']),
                        ('decode', [str(args.mistral_cv), 'decomp', '5CSEBA6U23I7', 'output_files/top.rbf', 'decoded.bt'])]
            for name, command in commands:
                with (project / (name + '.log')).open('x') as log:
                    subprocess.run(command, cwd=project, stdout=log, stderr=subprocess.STDOUT, check=True)
        result['projects'][str(load)] = summarize_project(project, load)
    with (args.output / 'reference.json').open('x') as stream:
        stream.write(json.dumps(result, indent=2) + '\n')
    print('PASS: 128 complete fitted boundary observations; hard-IP launch/capture and physical qualification remain open')


if __name__ == '__main__':
    main()
