#!/usr/bin/env python3
"""drc_findings.py REF_PROJECT PROJECT...: the design assistant's violation rows, compared.

For each rule with violations, the detail table's rows (fitter-copy suffixes and the scanout
prefix normalised away, purely numeric cells dropped; an LNT-30010 row is keyed by its driving net
alone, since its example endpoints and fan-out counts are the fitter's; TMC-20602, placement
tension, is left out) in each PROJECT that are not in REF_PROJECT's, and the reverse. Prints
"same" when a project's rows for every rule equal the reference's.
"""
import re
import sys
from pathlib import Path

PREFIX = 'soc_inst|subsys_periph|fes_scanout|fes_scanout|'


def findings(project):
    text = (Path(project) / 'output_files/golden_top.tq.drc.signoff.rpt').read_text(errors='replace')
    rows = {}
    for m in re.finditer(r'^; ([A-Z]{3}-\d{5}) - [^;]*;\s*\n\+[-+]+\n(;.*?)\n\+[-+]+\n((?:;.*\n)*?)\+', text, re.M):
        rule, body = m.group(1), m.group(3)
        out = set()
        for line in body.splitlines():
            cells = [c.strip() for c in line.strip(';').split(';')]
            cells = [c for c in cells if c and not re.fullmatch(r'-?[\d.]+', c)]
            if rule == 'LNT-30010':
                cells = cells[:1]
            cell = ' ; '.join(cells)
            cell = cell.replace(PREFIX, '')
            cell = re.sub(r'~(?:_Duplicate(?:_?\d+)?(?:DUPLICATE)?|DUPLICATE)', '', cell)
            out.add(cell)
        if rule != 'TMC-20602':
            rows[rule] = out
    return rows


def main(ref, *projects):
    base = findings(ref)
    for project in projects:
        mine = findings(project)
        diffs = []
        for rule in sorted(set(base) | set(mine)):
            extra, missing = mine.get(rule, set()) - base.get(rule, set()), base.get(rule, set()) - mine.get(rule, set())
            for row in sorted(extra):
                diffs.append(f'  + {rule}: {row[:200]}')
            for row in sorted(missing):
                diffs.append(f'  - {rule}: {row[:200]}')
        print(project, 'same' if not diffs else f'{len(diffs)} rows differ')
        print('\n'.join(diffs))


if __name__ == '__main__':
    main(*sys.argv[1:])
