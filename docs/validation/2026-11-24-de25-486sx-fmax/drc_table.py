#!/usr/bin/env python3
"""drc_table.py LABEL=PROJECT...: method2.md's signoff-rule table.

Reads each project's output_files/golden_top.tq.drc.signoff.rpt (the design assistant's
signoff report) and prints, as markdown, one row per rule that has a violation in any project:
its violation count in each, with the first LABEL as the reference. A count that differs from
the reference's is in bold. Also writes nothing; JSON with --json.
"""
import json
import re
import sys
from pathlib import Path

ROW = re.compile(r'^; ([A-Z]{3}-\d{5}) - (.+?)\s*; (\w+)\s*; (\d+)\s*; (\d+)\s*;', re.M)


def rules(project):
    text = (Path(project) / 'output_files/golden_top.tq.drc.signoff.rpt').read_text(errors='replace')
    found = {m.group(1): {'name': m.group(2), 'severity': m.group(3), 'violations': int(m.group(4)),
                          'waived': int(m.group(5))} for m in ROW.finditer(text)}
    if not found:
        raise ValueError(f'{project}: no rule rows')
    return found


def main(args):
    as_json = '--json' in args
    pairs = [a.split('=', 1) for a in args if a != '--json']
    table = {label: rules(path) for label, path in pairs}
    if as_json:
        print(json.dumps(table, indent=1, sort_keys=True))
        return
    labels = [label for label, _ in pairs]
    ref = labels[0]
    ids = sorted({r for t in table.values() for r, v in t.items() if v['violations']})
    print('| Rule | Severity | ' + ' | '.join(labels) + ' |')
    print('| --- | --- | ' + ' | '.join('---:' for _ in labels) + ' |')
    for rule in ids:
        info = next(t[rule] for t in table.values() if rule in t)
        cells = []
        for label in labels:
            n = table[label].get(rule, {}).get('violations', 0)
            cells.append(str(n) if n == table[ref].get(rule, {}).get('violations', 0) else f'**{n}**')
        print(f"| {rule} {info['name']} | {info['severity']} | " + ' | '.join(cells) + ' |')


if __name__ == '__main__':
    main(sys.argv[1:])
