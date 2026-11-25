#!/usr/bin/env python3
"""List the fitted endpoints covered by the local retimed tension review."""
from pathlib import Path
import re
import sys

from boards.de25nano.scanout.machine_findings import bases, table


RULE = 'TMC-20602'
REVIEW = 'retimed-machine-control-tension'


def targets(drc):
    basis = next((basis for basis in bases().BASES if basis['name'] == REVIEW), None)
    summary = re.search(r'^; TMC-20602 - [^;]+; Low\s*;\s*(\d+)\s*;', drc, re.M)
    if basis is None or summary is None or int(summary[1]) == 0:
        return ()
    _, rows = table(drc, RULE)
    return tuple(row[0] for row in rows
                 if all(re.fullmatch(pattern, row[column]) for column, pattern in basis['match'].items()))


if __name__ == '__main__':
    for node in targets(Path(sys.argv[1]).read_text()):
        print(node)
