#!/usr/bin/env python3
"""cdc_compare.py CONTROL_PROJECT PROJECT...: method.md's F5.

#2224's drc_findings.py rows, restricted to the CDC-* and RDC-* rules. A
project passes when it has no such row that the control lacks. Rows the
control has and the project lacks are listed too, but they do not fail it.
Prints one JSON object and exits 1 if any project fails.
"""
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / '2026-11-24-de25-486sx-fmax'))
from drc_findings import findings  # noqa: E402

RULES = ('CDC-', 'RDC-')


def crossing_rows(project):
    return {rule: rows for rule, rows in findings(project).items() if rule.startswith(RULES)}


def main(control, *projects):
    if not projects:
        raise SystemExit(__doc__)
    base = crossing_rows(control)
    result = {'control': str(control), 'rules': sorted(base), 'projects': {}}
    for project in projects:
        mine = crossing_rows(project)
        new = {rule: sorted(mine[rule] - base.get(rule, set())) for rule in mine}
        gone = {rule: sorted(base[rule] - mine.get(rule, set())) for rule in base}
        result['projects'][str(project)] = {
            'new': {rule: rows for rule, rows in new.items() if rows},
            'absent': {rule: rows for rule, rows in gone.items() if rows},
            'counts': {rule: len(rows) for rule, rows in sorted(mine.items())},
            'passes': not any(new.values()),
        }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if all(entry['passes'] for entry in result['projects'].values()) else 1


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
