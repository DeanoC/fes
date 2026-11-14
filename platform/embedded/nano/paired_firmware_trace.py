"""Ordered paired-service firmware observations; grants no device authority."""
from configuration_trace import extract
from native_trace import RULES, DECIMAL
from service_trace import require
from window_trace import PATTERNS, HEX, TEXT, check_route


class Records:
    def __init__(self, data):
        self.rows = []
        for raw in data.splitlines():
            line = raw.decode('utf-8').rstrip('\r')
            if line.strip() in ('', 'NOTICE:'):
                continue
            matches = [(name, pattern.fullmatch(line)) for name, pattern in RULES]
            matches += [(name, PATTERNS[name].fullmatch(line)) for name in
                        ('scope', 'gate', 'master', 'closure', 'reader_drain', 'reader_route')]
            matches = [(name, match) for name, match in matches if match]
            require(len(matches) == 1, 'unknown or ambiguous paired firmware record: '+line[:120])
            name, match = matches[0]
            values = {}
            for key, value in match.groupdict().items():
                if name in ('scope', 'gate', 'master', 'closure', 'reader_drain', 'reader_route'):
                    values[key] = value if key in TEXT else int(value, 16 if
                        key in HEX or name == 'reader_drain' and key == 'status' else 10)
                else:
                    values[key] = int(value, 10 if key in DECIMAL or
                        name in ('master_stop', 'ddr_firewall') and key == 'generation' else 16)
            self.rows.append((name, values))
        self.index = 0

    def take(self, name):
        require(self.index < len(self.rows) and self.rows[self.index][0] == name,
                'missing or reordered paired firmware '+name)
        value = self.rows[self.index][1]
        self.index += 1
        return value

    def finish(self):
        require(self.index == len(self.rows), 'unconsumed paired firmware records')


def configuration(data, descriptor, owner, source, previous):
    parsed = extract(data, descriptor, generation=source, previous=previous, rearm=previous != 0,
                     require_activation=True, require_programmed=False, expected_owner=owner)
    require(not parsed['errors'] and parsed['complete'], 'incomplete or invalid paired configuration')
    timing = parsed['timing']
    require(timing is not None and 0 < timing['interval_ticks'] < 20*400000000,
            'paired configuration exceeded original 20s bound')
    rows = parsed['records']
    engines = [r['values'] for r in rows if r['stage'] == 'configuration_engine']
    phases = {r['values']['phase'] for r in rows if r['stage'] == 'configuration_mailbox'}
    require(len(engines) == 1 and engines[0]['credits'] == 4 and
            engines[0]['block_bytes'] == 0xffff0 and phases == {1, 2, 3},
            'paired configuration queue evidence differs')
    require(rows and rows[-1]['stage'] == 'configuration_complete' and
            not any(line.strip() for line in parsed['parts'][:rows[-1]['line']]),
            'paired route observation preceded configuration completion')
    return parsed, Records(b'\n'.join(parsed['parts']))


def menu_opening(data, descriptor, owner, source, previous, memory, *, retained_reader=None,retained_source_generation=None):
    parsed, records = configuration(data, descriptor, owner, source, previous)
    # RULES currently matches the literal command/word count without captures.
    query = records.take('fpga_query')
    require(query in ({}, dict(command=4, words=6)), 'menu readiness query differs')
    status = records.take('fpga_status')
    require(status['status'] == 0 and status['words'] == 6 and status['state'] == 0 and
            status['pins'] & (1 << 31) and status['soft'] & 3 == 3 and
            not status['soft'] & 8 and status['ready'] == 1 and
            all(0 <= status[k] <= 0xffffffff for k in
                ('state', 'version', 'pins', 'soft', 'error_location', 'error_details')),
            'menu FPGA is not ready')
    def route(generation, reset, gate):
        check_route(records.take, require, owner=owner, generation=generation,
                    reset=reset, memory=memory, with_gate=gate, retained_reader=retained_reader,
                    retained_source_generation=retained_source_generation)
    route(source, 15, False)
    require(records.take('scope') == dict(phase='control', owner=owner, generation=source+1, reset=13),
            'menu control opening differs')
    route(source+1, 13, True)
    route(source+1, 13, True)
    require(records.take('scope') == dict(phase='data', owner=owner, generation=source+2, reset=5),
            'menu data opening differs')
    records.finish()
    return dict(configuration_timing=parsed['timing'], configuration_records=parsed['records'],
                configuration_generation=source, runtime_admission=False)


def menu_closed(data, owner, source):
    records = Records(data)
    scope = dict(owner=owner, generation=source+2, reset=5)
    require(records.take('scope') == dict(scope, phase='close'), 'menu close scope differs')
    stop = records.take('master')
    polls = stop['reader_polls']
    # Two 22-word snapshots per poll and another stable pair in stopped().
    # Go normally already drained the reader. The secure callback can also
    # issue one disable and observe its completion; unstable polls are bounded.
    require(stop == dict(scope, status=0, reader_reads=44*(polls+1), reader_polls=polls,
                         disable=stop['disable'], probe_polls=2, cancel=0, complete=1) and
            1 <= polls <= 128 and (stop['disable'] == 0 or polls >= 2),
            'menu master stop is incomplete or exceeds its firmware bound')
    drain = records.take('reader_drain')
    require(drain == dict(scope, reader_generation=1, request_generation=1,
                         cookie=drain['cookie'], ack_generation=1, ack_cookie=drain['cookie'],
                         status=4, pending=0) and 0 < drain['cookie'] < 1 << 64,
            'menu drain has no matching completed reader generation/cookie')
    closure = records.take('closure')
    require(closure == dict(owner=owner, generation=source+3, reset=15, hardware_drained=8,
                           data_remained_contained=0, producers_quiescent=1, fabric_acknowledged=0),
            'menu closure lost containment or drain ownership')
    records.finish()
    return dict(master_stop=stop, reader_drain=drain, closure=closure,
                physical_ddr_drain_qualified=False)
