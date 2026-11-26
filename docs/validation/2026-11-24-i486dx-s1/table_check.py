#!/usr/bin/env python3
"""table_check.py SEED=PROJECT...: the reviewed-signoff table of this checkout
(boards/de25nano/pc386/signoff_bases.py, through scanout/machine_findings.py)
applied to each compiled seed: its golden_top.tq.drc.signoff.rpt, its compiled
SDC and QSF, as the producer's gate reads them (machine_findings.findings, less
the record-source binding, which is of the fitted record's revision). Prints
each seed's reviewed rules and groups, or the refusal."""
import json
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3] / 'sources/misteross'
sys.path.insert(0, str(ROOT))
import importlib.util
spec = importlib.util.spec_from_file_location('mf', ROOT / 'boards/de25nano/scanout/machine_findings.py')
mf = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mf)

ok = True
for arg in sys.argv[1:]:
    name, project = arg.split('=', 1)
    project = Path(project)
    drc = (project / 'output_files/golden_top.tq.drc.signoff.rpt').read_text()
    sdc = (project / 'fes_mailbox/machine-clocks.sdc').read_text()
    qsf = (project / 'golden_top.qsf').read_text()
    table = mf.bases()
    try:
        mf.assignments(qsf, table.TOGGLE_ASSIGNMENT)
        entries = mf.review(drc, sdc, table.BASES)
        print(name, 'PASS', json.dumps([{e['id']: [(g['name'], g['rows']) for g in e['groups']]} for e in entries]))
    except Exception as error:  # the gate's own refusal
        ok = False
        print(name, 'REFUSED', error)
sys.exit(0 if ok else 1)
