#!/usr/bin/env python3
"""Audit CPU100/board50 routed synchronizers; grant no exceptions or physical signoff."""
from __future__ import annotations

import argparse
from collections import Counter, defaultdict
import hashlib
import json
import math
from pathlib import Path


def require(condition, message):
    if not condition:
        raise ValueError(message)


def audit(routed, timing, pixel_clock='clk_pixel'):
    m = routed['modules']['top']
    cells = m['cells']
    require(timing['timing_summary']['final_analogue_model'] is True,
            'requires the final analogue report')
    clocks = {c['net']: c for c in json.loads(m['settings']['timing/io_clocks'])}
    primary = {'clocks[0]', 'clocks[1]', 'clocks[2]', pixel_clock}
    require(primary <= set(clocks) and set(timing['fmax']) == primary,
            'requires the CPU100/board50 clock inventory')
    require(clocks['clocks[0]']['period'] == [10, 10] and
            clocks['clocks[2]']['period'] == [20, 20], 'wrong CPU/board periods')
    require(clocks['clocks[1]']['period'] == [20, 20] and
            clocks['clocks[0]']['phase'] == clocks['clocks[2]']['phase'] == 0 and
            clocks['clocks[1]']['phase'] == 10, 'wrong memory clock phases')
    require(all(math.isclose(timing['fmax'][name]['constraint'], mhz, rel_tol=1e-6)
                for name, mhz in {'clocks[0]': 100, 'clocks[1]': 50,
                                 'clocks[2]': 50, pixel_clock: 74.25}.items()),
            'wrong reported clock targets')
    clock_bits = {name: m['netnames'][name]['bits'][0] for name in primary}
    require(len(set(clock_bits.values())) == 4, 'aliased primary clocks')
    drivers, users = {}, defaultdict(list)
    for name, cell in cells.items():
        for port, bits in cell['connections'].items():
            for bit in bits:
                if cell['port_directions'][port] == 'output':
                    require(bit not in drivers, 'multiple net drivers')
                    drivers[bit] = (name, port)
                else:
                    users[bit].append((name, port))

    if pixel_clock != 'clk_pixel':
        # A caller must declare the new identity. Bind it to architecture-owned
        # video PLL output zero; never guess an alias or accept the HDMI phase.
        pixel = clocks[pixel_clock]
        require(pixel['group'] == 'video_clock' and pixel['phase'] == 0 and
                len(pixel['period']) == len(pixel['high']) == len(pixel['low']) == 2 and
                all(math.isclose(p, 1000 / 74.25, rel_tol=1e-6) for p in pixel['period']),
                'declared pixel clock has the wrong PLL group, phase or period')
        require(all(math.isclose(p, 500 / 74.25, rel_tol=1e-6)
                    for p in pixel['high'] + pixel['low']),
                'declared pixel clock has the wrong duty cycle')
        driver = drivers.get(clock_bits[pixel_clock])
        require(driver is not None and driver[1] == 'Q', 'missing pixel clock buffer')
        buffer = cells[driver[0]]
        pll = cells.get('video_clock', {})
        require(buffer['type'] == 'MISTRAL_CLKBUF' and not buffer['parameters'] and
                set(buffer['connections']) == {'A', 'Q'} and
                pll.get('type') == 'altera_pll' and
                len(pll.get('connections', {}).get('outclk', [])) == 1 and
                buffer['connections']['A'] == pll['connections']['outclk'],
                'declared pixel clock is not buffered video PLL output zero')
        metadata = buffer.get('attributes', {}).get('FES_PINMAP_V1')
        require(metadata is not None, 'missing pixel clock buffer pin metadata')
        try:
            metadata = json.loads(bytes.fromhex(metadata))
        except (ValueError, TypeError) as exc:
            raise ValueError('malformed pixel clock buffer pin metadata') from exc
        require(metadata ==
                {'count': 2, 'pins': {'A': [0, 'A'], 'Q': [0, 'Q']}},
                'unsupported pixel clock buffer pin mapping')

    def pinmap(name):
        cell = cells[name]
        try:
            record = json.loads(bytes.fromhex(cell['attributes']['FES_PINMAP_V1']))
            pins = record['pins']
        except (KeyError, ValueError, TypeError) as exc:
            raise ValueError('missing or malformed native pin metadata: ' + name) from exc
        require(record['count'] == len(pins) and
                all(len(v) == 2 and type(v[0]) is int and v[0] in (0, 1, 2, 3)
                    for v in pins.values()), 'invalid native pin identities: ' + name)
        if cell['type'] == 'MISTRAL_FF':
            require(all(v[1] == k for k, v in pins.items()), 'remapped FF pin identity')
        else:
            require(set(pins) == {'A', 'Q'} and pins['A'][1] in
                    {'F0', 'F1', 'E0', 'E1'} and pins['Q'][1] == 'COMBOUT',
                    'unsupported routed buffer pin map')
        return {k: v[0] for k, v in pins.items()}

    def bit(name, port):
        bits = cells[name]['connections'].get(port, [])
        require(len(bits) == 1, 'missing scalar connection: ' + name + '.' + port)
        return bits[0]

    def upstream(net):
        """Allow actual routed identity buffers, never arbitrary combinational logic."""
        buffers, seen = [], set()
        while True:
            require(net not in seen and net in drivers, 'cyclic or undriven stage input')
            seen.add(net)
            name, port = drivers[net]
            if cells[name]['type'] != 'MISTRAL_BUF':
                return name, port, buffers
            require(port == 'Q' and set(cells[name]['connections']) == {'A', 'Q'},
                    'unsupported identity buffer')
            require(not cells[name]['parameters'] and pinmap(name) == {'A': 0, 'Q': 0},
                    'inverted or unqualified identity buffer')
            buffers.append(name)
            net = bit(name, 'A')

    stages = {n for n, c in cells.items() if c['type'] == 'MISTRAL_FF' and
              c['attributes'].get('async_reg') == 'true'}
    require(len(stages) == 12, 'expected exactly twelve board50 synchronizer FFs')
    maps = {n: pinmap(n) for n in stages}
    for n, pins in maps.items():
        require(pins.get('CLK') == 0 and pins.get('ENA') == 2 and
                pins.get('SLOAD') == 1 and pins.get('DATAIN') == 0,
                'unsupported stage clock, enable or load: ' + n)
        require(not cells[n]['parameters'], 'unexpected FF parameters: ' + n)
        require(cells[n]['attributes'].get('NEXTPNR_BEL'), 'unplaced stage: ' + n)

    previous = {n: upstream(bit(n, 'DATAIN')) for n in stages}
    tails = {n for n, (driver, port, _) in previous.items() if driver in stages and port == 'Q'}
    heads = {previous[n][0] for n in tails}
    require(len(heads) == len(tails) == 6 and not heads & tails and heads | tails == stages,
            'expected six disjoint two-stage chains')
    endpoints = defaultdict(list)
    for net in timing['detailed_net_timings']:
        for row in net['endpoints']:
            endpoints[row['cell'], row['port']].append((net, row))

    chains = []
    for tail in sorted(tails):
        head, _, buffers = previous[tail]
        require(bit(head, 'CLK') == bit(tail, 'CLK'), 'different stage clocks')
        clock = [n for n, b in clock_bits.items() if b == bit(head, 'CLK')]
        require(len(clock) == 1, 'unknown synchronizer clock')
        clock = clock[0]
        for port in ('ACLR', 'SCLR'):
            require(maps[head][port] == maps[tail][port] and
                    cells[head]['connections'][port] == cells[tail]['connections'][port],
                    'different stage reset controls')
        # Every head-to-tail branch must be exclusive, including routed buffers.
        path = [head] + list(reversed(buffers))
        for source, sink in zip(path, path[1:] + [tail]):
            require(users[bit(source, 'Q')] == [(sink, 'DATAIN' if sink == tail else 'A')],
                    'interstage fanout or logic at ' + source)
        source, source_port, _ = previous[head]
        reset_chain = (cells[source]['type'] == 'MISTRAL_CONST' and
                       cells[source]['parameters'].get('LUT') == '0' * 31 + '1')
        if reset_chain:
            require(maps[head]['ACLR'] == 0 and maps[head]['SCLR'] == 1,
                    'reset chain must have active-low asynchronous clear and no synchronous override')
        else:
            require(maps[head]['ACLR'] == 2 and maps[head]['SCLR'] == 3,
                    'video synchronizer must retain the common synchronous reset')
        entries = endpoints[tail, 'DATAIN']
        require(len(entries) == 1, 'missing or duplicate interstage timing row')
        net, row = entries[0]
        event = 'posedge ' + clock
        require((net['driver'], net['port']) == drivers[bit(tail, 'DATAIN')] and
                row['launch_event'] == row['event'] == event and
                row.get('setup_checked') and row.get('hold_checked'),
                'interstage path is cut, unpaired or bound to another driver')
        for field in ('setup_slack_ns', 'hold_slack_ns'):
            value = row[field]
            require(type(value) in (int, float) and math.isfinite(value) and value >= 0,
                    'interstage timing failure')
        chains.append({'head': head, 'tail': tail, 'kind': 'reset' if reset_chain else 'video',
                       'clock': clock, 'identity_buffers': buffers,
                       'source': [source, source_port],
                       'setup_margin_ns': row['setup_slack_ns'],
                       'hold_margin_ns': row['hold_slack_ns'],
                       'tail_consumers': sorted(users[bit(tail, 'Q')])})
    reset_consumers = []
    combinational = {'MISTRAL_ALUT' + str(n) for n in range(1, 7)} | {'MISTRAL_BUF', 'MISTRAL_NOT'}
    for chain in chains:
        if chain['kind'] != 'reset':
            continue
        pending = [(bit(chain['tail'], 'Q'), False)]
        visited, active, terminals = set(), set(), set()
        while pending:
            net, leaving = pending.pop()
            if leaving:
                active.remove(net)
                visited.add(net)
                continue
            if net in visited:
                continue
            require(net not in active, 'combinational cycle in reset consumer cone')
            active.add(net)
            pending.append((net, True))
            for name, port in users[net]:
                cell = cells[name]
                if cell['type'] in combinational:
                    pending.extend((b, False) for p, bits in cell['connections'].items()
                                   if cell['port_directions'][p] == 'output' for b in bits)
                else:
                    terminals.add((name, port))
        coverage = []
        for name, port in sorted(terminals):
            cell = cells[name]
            require(cell['type'] == 'MISTRAL_FF' and port in {'DATAIN', 'SCLR', 'ENA', 'ACLR'},
                    'unsupported reset consumer: ' + name + '.' + port)
            pins = pinmap(name)
            require(pins['CLK'] == 0 and pins[port] in (0, 3) and
                    bit(name, 'CLK') == clock_bits[chain['clock']],
                    'reset consumer has unsupported clock or effective control: ' + name)
            event = 'posedge ' + chain['clock']
            matches = [(net, row) for net, row in endpoints[name, port]
                       if row['launch_event'] == event]
            require(len(matches) == 1, 'missing or ambiguous reset consumer timing: ' + name)
            net, row = matches[0]
            require((net['driver'], net['port']) == drivers[bit(name, port)] and
                    row['event'] == event and row.get('setup_checked') and row.get('hold_checked'),
                    'reset consumer path is cut, unpaired or misbound: ' + name)
            for field in ('setup_slack_ns', 'hold_slack_ns'):
                value = row[field]
                require(type(value) in (int, float) and math.isfinite(value) and value >= 0,
                        'reset consumer timing failure: ' + name)
            coverage.append({'cell': name, 'port': port,
                             'setup_margin_ns': row['setup_slack_ns'],
                             'hold_margin_ns': row['hold_slack_ns']})
        require(coverage, 'reset tail has no modeled consumers')
        reset_consumers.append({'tail': chain['tail'], 'clock': chain['clock'],
                                'terminals': coverage,
                                'asynchronous_clear_consumers': sum(c['port'] == 'ACLR' for c in coverage),
                                'setup_min_ns': min(c['setup_margin_ns'] for c in coverage),
                                'hold_min_ns': min(c['hold_margin_ns'] for c in coverage)})
    require(Counter((c['kind'], c['clock']) for c in chains) ==
            {('reset', 'clocks[2]'): 2, ('reset', pixel_clock): 1,
             ('video', 'clocks[2]'): 1, ('video', pixel_clock): 2},
            'unexpected chain inventory')
    return {'scope': 'Host-only board50 synchronizer and first-capture reset-cone timing audit. '
                     'No exceptions, CDC protocol proof or physical/MTBF signoff.',
            'structure_valid': True, 'interstage_setup_hold_pass': True, 'chains': chains,
            'reset_consumer_timing_pass': True, 'reset_consumers': reset_consumers,
            'reset_consumer_policy_valid': False,
            'consumer_policy_note': 'Additional asynchronous clears retain pulse/control obligations; '
                                    'timed rows grant no exception or physical acceptance.',
            'timing_exception_valid': False, 'physical_qualified': False,
            'remaining': ['PLL lock/reset waveform and relock', 'metastability and MTBF',
                          'downstream asynchronous reset pulse/control safety', 'bundled video bank physical stability']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--routed', type=Path, required=True)
    parser.add_argument('--timing', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--receipt', type=Path, required=True)
    parser.add_argument('--pixel-clock', default='clk_pixel',
                        help='Explicit native pixel identity; alternatives require video PLL output-zero binding')
    args = parser.parse_args()
    receipt = json.loads(args.receipt.read_text())
    require(receipt['completed'] and receipt['postflight_identity_pass'] and
            receipt['returncode'] == 0, 'requires a successful immutable route receipt')
    for path in (args.routed, args.timing):
        require(path.resolve().parent == args.receipt.resolve().parent and
                hashlib.sha256(path.read_bytes()).hexdigest() == receipt['artifacts'][path.name],
                'route artifact does not match its receipt')
    result = audit(json.loads(args.routed.read_text()), json.loads(args.timing.read_text()), args.pixel_clock)
    result['inputs_sha256'] = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                              for p in (args.routed, args.timing, args.receipt)}
    args.output.write_text(json.dumps(result, indent=2) + '\n')
    print('PASS: six chains and their reset consumer timing pass; physical qualification open')


if __name__ == '__main__':
    main()
