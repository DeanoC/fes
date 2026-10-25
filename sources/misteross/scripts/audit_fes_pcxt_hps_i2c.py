#!/usr/bin/env python3
"""Audit frozen PC/XT HPS I2C connectivity and timing coverage; no signoff."""
import argparse
import hashlib
import json
import math
from pathlib import Path

HPS = 'cyclonev_hps_interface_peripheral_i2c'
SITE = HPS + '.52.60.0'
PADS = {'HDMI_I2C_SCL': ('PIN_U10', 'MISTRAL_IO.6.0.0', 'out_clk', 'scl'),
        'HDMI_I2C_SDA': ('PIN_AA4', 'MISTRAL_IO.4.0.2', 'out_data', 'sda')}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def audit(design, boundaries=None):
    top = design['modules']['top']
    cells = top['cells']
    drivers, users = {}, {}
    for name, cell in cells.items():
        for port, bits in cell['connections'].items():
            for bit in bits:
                direction = cell['port_directions'][port]
                if direction == 'output':
                    require(bit not in drivers, 'multiple cell drivers')
                    drivers[bit] = (name, port)
                elif direction in ('input', 'inout'):
                    users.setdefault(bit, []).append((name, port))

    def scalar(cell, port):
        bits = cell['connections'].get(port, [])
        require(len(bits) == 1, 'missing or nonscalar ' + port)
        return bits[0]

    def pinmap(cell, expected):
        raw = cell['attributes'].get('FES_PINMAP_V1')
        require(isinstance(raw, str), 'missing native pin map')
        mapping = json.loads(bytes.fromhex(raw))
        require(mapping == dict(count=len(expected), pins={name:[0, name] for name in expected}),
                'inverted, incomplete or unexpected native pin map')

    def low(bit):
        if bit == '0':
            return True
        if bit not in drivers:
            return False
        name, port = drivers[bit]
        cell = cells[name]
        if not (cell['type'] == 'MISTRAL_CONST' and port == 'Q'
                and cell['parameters'].get('LUT') == '0' * 32
                and cell['connections'] == {'Q':[bit]}):
            return False
        raw = cell['attributes'].get('FES_PINMAP_V1')
        return (isinstance(raw, str) and json.loads(bytes.fromhex(raw)) ==
                {'count':1, 'pins':{'Q':[0, 'COMBOUT']}})

    endpoints, pads, instance = [], [], None
    for external, (pin, bel, output, feedback) in PADS.items():
        require(external in top['ports'], 'missing external I2C pad')
        port = top['ports'][external]
        require(port['direction'] == 'inout' and len(port['bits']) == 1, 'I2C pad must be bidirectional')
        matches = [(n, cells[n]) for n, p in users.get(port['bits'][0], []) if p == 'PAD']
        require(len(matches) == 1, 'missing or ambiguous pad cell')
        name, pad = matches[0]
        require(pad['type'] == 'MISTRAL_IO' and not pad['parameters'], 'requires unregistered native IO')
        require(pad['attributes'].get('NEXTPNR_BEL') == bel and pad['attributes'].get('LOC') == pin,
                'wrong physical I2C pad')
        require(pad['attributes'].get('IO_STANDARD') == '3.3-V LVTTL', 'wrong I2C electrical standard')
        pinmap(pad, ('I', 'O', 'OE', 'PAD'))
        require(low(scalar(pad, 'I')), 'I2C data must be proven constant low')
        enable = scalar(pad, 'OE')
        require(enable in drivers, 'undriven I2C output enable')
        source, source_port = drivers[enable]
        hps = cells[source]
        require(hps['type'] == HPS and source_port == output
                and hps['attributes'].get('NEXTPNR_BEL') == SITE, 'wrong HPS output-enable source')
        require(users[enable] == [(name, 'OE')], 'unexpected output-enable fanout')
        pinmap(hps, ('out_clk', 'out_data', 'scl', 'sda'))
        if instance is None:
            instance = source
        require(source == instance, 'I2C pads use different HPS controllers')
        read = scalar(pad, 'O')
        require(scalar(hps, feedback) == read and users.get(read) == [(source, feedback)],
                'missing, crossed or unexpectedly shared HPS feedback')
        require(drivers.get(read) == (name, 'O'), 'wrong feedback driver')
        pads.append(dict(port=external, cell=name, bel=bel, pin=pin, constant_data=0,
                         mode='constant-low data, active-high OE, input feedback'))
        for role, driver, driver_port, sink, sink_port in (
                ('oe', source, output, name, 'OE'), ('feedback', name, 'O', source, feedback)):
            endpoints.append(dict(port=external, role=role, source_cell=driver, source_port=driver_port,
                                  sink_cell=sink, sink_port=sink_port))
    require(sum(c['type'] == HPS for c in cells.values()) == 1, 'unexpected HPS I2C instance inventory')
    observations, unsupported = [], []
    if boundaries is not None:
        rows = boundaries['arcs']
        require(len(rows) == len(endpoints), 'incomplete native boundary inventory')
        expected = {(e['port'], e['role']):e for e in endpoints}
        seen = set()
        for row in rows:
            key = (row['port'], row['role'])
            require(key in expected and key not in seen, 'unknown or duplicate native boundary')
            seen.add(key)
            require(all(row.get(k) == v for k, v in expected[key].items()), 'native boundary is misbound')
            require(type(row['analogue_supported']) is bool, 'missing native model status')
            observation = dict(expected[key], analogue_supported=row['analogue_supported'])
            if row['analogue_supported']:
                for edge in ('rise', 'fall'):
                    values = row['native_slow100_' + edge + '_ps']
                    require(len(values) == 2 and all(type(v) in (int, float) and math.isfinite(v) for v in values)
                            and 0 <= values[0] <= values[1], 'invalid native delay observation')
                    observation[edge + '_ps'] = values
            else:
                observation.update(rise_ps=None, fall_ps=None)
                unsupported.append(dict(expected[key], reason='native analogue route model unavailable'))
            observations.append(observation)
        require(seen == set(expected), 'native boundary coverage differs')
    else:
        unsupported = [dict(e, reason='native boundary extraction not supplied') for e in endpoints]
    return dict(scope=__doc__, structure_valid=True, hps_instance=instance, hps_bel=SITE,
                controller_index=2, pads=pads, endpoints=endpoints, native_observations=observations,
                unsupported_native_boundaries=unsupported, native_fast_corner_qualified=False,
                hard_ip_launch_bounds_ns=None, hard_ip_capture_bounds_ns=None,
                declared_phase_relation_to_fpga_clocks=None, timing_exception_valid=False,
                physical_qualified=False, hardware_accepted=False,
                remaining=['Hard-IP launch/capture bounds and native OE source waveforms.',
                           'Native route fast-corner bounds; fitted references use their own routes.',
                           'Immutable running configuration and source identity.',
                           'Board pull-up, capacitance, flight and receiver/filter requirements.'])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--route-receipt', type=Path, required=True,
                        help='immutable ordinary-producer wrapper receipt with artifact hashes')
    parser.add_argument('--boundary', type=Path,
                        help='optional read-only native-boundary extraction; unsupported delays remain unknown')
    parser.add_argument('--boundary-receipt', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(bool(args.boundary) == bool(args.boundary_receipt), 'boundary and its receipt must be supplied together')
    receipt = json.loads(args.route_receipt.read_text())
    require(receipt['completed'] and receipt['postflight_identity_pass'] and receipt['returncode'] == 0,
            'requires a successful immutable ordinary producer')
    output = Path(receipt['output'])
    paths = [output / n for n in ('diagnostic.json', 'routed.json', 'diagnostic.rbf')]
    for path in paths:
        require(digest(path) == receipt['artifacts_sha256'][str(path)], 'route artifact hash differs')
    producer = json.loads(paths[0].read_text())
    require(producer['status'] == 'routed-diagnostic-unqualified' and producer['half_rate_board']
            and not producer['working_tree_changes'] and producer['revision'] == receipt['FES_commit'],
            'requires committed board50 diagnostic sources')
    require(digest(paths[2]) == producer['rbf_sha256'], 'producer RBF identity differs')
    boundary = None
    if args.boundary:
        capture = json.loads(args.boundary_receipt.read_text())
        require(capture['returncode'] == 0 and capture['postflight_identity_pass']
                and capture['output_sha256'] == digest(args.boundary), 'invalid native-boundary receipt')
        for path in paths[1:]:
            require(capture['input_sha256'].get(str(path)) == digest(path), 'boundary belongs to another route')
        boundary = json.loads(args.boundary.read_text())
    result = audit(json.loads(paths[1].read_text()), boundary)
    result.update(FES_commit=producer['revision'], seed=producer['seed'],
                  inputs_sha256={str(p):digest(p) for p in [args.route_receipt, *paths]},
                  boundary_sha256=digest(args.boundary) if args.boundary else None)
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print('PASS: four HPS enable/feedback identities and constant-low IO structure; timing/physical qualification open')


if __name__ == '__main__':
    main()
