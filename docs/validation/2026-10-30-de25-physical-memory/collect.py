#!/usr/bin/env python3
"""Check frozen hardware console results offline; performs no hardware access."""
import argparse
import hashlib
import json
from pathlib import Path
import re


def parse(text):
    tokens = re.findall(r'[{}]|[^\s{}]+', text)
    position = 0

    def dictionary():
        nonlocal position
        result = {}
        while position < len(tokens):
            if tokens[position] == '}':
                position += 1
                return result
            key = tokens[position]
            position += 1
            if key in result or key == '{' or position == len(tokens):
                raise ValueError('Malformed or duplicate snapshot key')
            value = tokens[position]
            position += 1
            if value == '{':
                result[key] = dictionary()
            elif value == '}':
                raise ValueError('Missing snapshot value')
            else:
                result[key] = int(value) if value.isdecimal() else value
        raise ValueError('Unterminated snapshot')

    if not tokens or tokens[0] != '{':
        raise ValueError('Expected Tcl dictionary')
    position = 1
    result = dictionary()
    if position != len(tokens):
        raise ValueError('Trailing snapshot data')
    return result


def collect(logs):
    results = []
    for name, kind, capacities in [
        ('smoke-ram.log', 'SMOKE', {'fpga': 4096, 'sdram': 4096}),
        ('full-ram.log', 'FULL', {'fpga': 1073741824, 'sdram': 134217728}),
    ]:
        data = (logs / name).read_bytes()
        found = set()
        for line in data.decode().splitlines():
            match = re.fullmatch(rf'{kind}_RESULT (fpga|sdram) (.+)', line)
            if not match:
                continue
            bank, raw = match.groups()
            if bank in found:
                raise ValueError('Duplicate bank result')
            found.add(bank)
            row = parse(raw)
            length = capacities[bank]
            if any(row.get(k) != value for k, value in {
                'status': 18, 'fault': 0, 'errors': 0, 'bad_bits': 0,
                'outcome': 'PASS', 'pattern': 6, 'samples': 1024,
                'written_bytes': length * 7, 'read_bytes': length * 7 + 32768,
                'generation': 1, 'sequence': 2 if kind == 'SMOKE' else 3,
            }.items()):
                raise ValueError('Incomplete or failing hardware snapshot')
            endpoints = row['endpoints']
            for endpoint in endpoints.values():
                if endpoint['build_id'] != '6ee668470911276a9e553d0025c7ef0b' or endpoint['fault'] != 0:
                    raise ValueError('Endpoint identity/fault mismatch')
            if endpoints['hps']['status'] != 0:
                raise ValueError('HPS bank was not disabled')
            results.append({'test': kind.lower(), 'bank': bank, 'length_bytes': length,
                            'snapshot': row, 'log_sha256': hashlib.sha256(data).hexdigest()})
        if found != set(capacities):
            raise ValueError('Missing bank result')
    return {'format': 1, 'classification': 'physical-board-diagnostic', 'results': results}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--logs', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    if args.output.exists():
        parser.error('Output must be new')
    args.output.write_text(json.dumps(collect(args.logs), indent=2) + '\n')
    print('Four physical scan results match identities, byte counts and success criteria')
