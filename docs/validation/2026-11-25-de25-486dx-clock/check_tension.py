#!/usr/bin/env python3
"""Audit the complete TimeQuest trace without changing the package verdict."""
import collections
import csv
import json
import math
from pathlib import Path
import sys

expected_clock = 'fes_machine_pll|machine_pll_outclk0'
corners = {'Slow fix4 100C Model', 'Slow fix4 0C Model', 'Slow fix4a 0C Model', 'Fast fix4 0C Model'}
checks = {('ownership', ''), ('setup', 'from'), ('setup', 'to'), ('hold', 'from'),
          ('hold', 'to'), ('recovery', 'to'), ('removal', 'to')}
targets = set(Path(sys.argv[1]).read_text().splitlines())
expected = {(corner, node, check, direction)
            for corner in corners for node in targets for check, direction in checks}
seen = set()
issues = []
minima = collections.defaultdict(list)
with Path(sys.argv[2]).open() as stream:
    for row in csv.DictReader(stream, delimiter='\t'):
        corner, node, check, direction = (row[name] for name in ('corner', 'node', 'check', 'direction'))
        key = corner, node, check, direction
        if key in seen:
            issues.append(dict(reason='duplicate evidence row', row=row))
        seen.add(key)
        if row['status'] == 'ok':
            if (row['exact_matches'], row['owner_clocks'], row['sync_stage']) != ('1', expected_clock, '0'):
                issues.append(dict(reason='unproved machine-clock register ownership', row=row))
            if check == 'ownership':
                continue
            endpoint = 'from' if direction == 'from' else 'to'
            slack = float(row['slack_ns'])
            if (row[endpoint] != node or row[endpoint + '_clock'] != expected_clock
                    or not math.isfinite(slack) or slack < 0):
                issues.append(dict(reason='endpoint or timing result does not qualify', row=row))
            minima[check].append((slack, corner, node))
        elif not (row['status'] == 'n/a' and check in ('recovery', 'removal') and row['async_edges'] == '0'
                  and row['exact_matches'] == '1' and row['owner_clocks'] == expected_clock):
            issues.append(dict(reason='missing or unresolved evidence', row=row))

data = {'pass': expected == seen and not issues, 'expected_checks': len(expected), 'observed_checks': len(seen),
        'missing_checks': sorted(expected - seen), 'extra_checks': sorted(seen - expected),
        'minimum_slacks': {name: {'count': len(slacks), 'worst': min(slacks)} for name, slacks in minima.items()},
        'issues': issues}
print(json.dumps(data, indent=2))
sys.exit(0 if data['pass'] else 1)
