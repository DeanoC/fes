#!/usr/bin/env python3
"""List the fitted endpoints that require the separate retimed tension trace."""
from pathlib import Path
import re
import sys

from boards.de25nano.scanout.machine_findings import bases, table


RULE = 'TMC-20602'
REVIEW = 'retimed-machine-control-tension'


def targets(drc):
    summary = re.search(r'^; TMC-20602 - [^;]+; Low\s*;\s*(\d+)\s*;', drc, re.M)
    if summary is None or int(summary[1]) == 0:
        return ()
    original = tuple(basis for basis in bases().BASES if basis['rule'] == RULE and basis['name'] != REVIEW)
    _, rows = table(drc, RULE)
    # A producer pass means these remaining rows hit the retimed basis.
    # After a refusal, trace the same rows along with any still unrecognized.
    return tuple(row[0] for row in rows
                 if not any(all(re.fullmatch(pattern, row[column]) for column, pattern in basis['match'].items())
                            for basis in original))


if __name__ == '__main__':
    for node in targets(Path(sys.argv[1]).read_text()):
        print(node)
