#!/usr/bin/env python3
"""Print the DE25 pc386 producer's design and toolchain identities at a revision.

Selection-only, as scripts/de25_seal_plan.py probes registered recipes (pc386
has no recipe row yet, P4): no Quartus, and no Yosys (PATH is cut to /usr/bin
and the toolchain cache is unset, so an elaboration could not run). The
revision is checked out into a temporary worktree, because the producer
requires a clean source.

    identity_probe.py REV [REV ...]
"""
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(subprocess.check_output(['git', 'rev-parse', '--show-toplevel'], text=True).strip())
ROWS = [{"core_id": "fes.pc386", "producer_module": "boards.de25nano.producers.build_pc386_package",
         "producer_script": "boards/de25nano/producers/build_pc386_package.py"}]


def probe_source(checkout):
    text = (checkout / 'scripts/de25_seal_plan.py').read_text()
    probe = re.search(r"PROBE = r'''(.*?)'''", text, re.S).group(1)
    # The pc386 record also names its netlist front end.
    old = '("quartus", "qsys", "ipgenerate", "pfg")'
    if probe.count(old) != 1:
        raise SystemExit('de25_seal_plan.py PROBE changed; update this script')
    return probe.replace(old, '("quartus", "qsys", "ipgenerate", "pfg", "yosys")')


def identity(revision):
    with tempfile.TemporaryDirectory() as temp:
        checkout = Path(temp) / 'tree'
        subprocess.run(['git', 'worktree', 'add', '-q', '--detach', str(checkout), revision], cwd=ROOT, check=True)
        try:
            env = {k: v for k, v in os.environ.items()
                   if not k.startswith('PYTHON') and k != 'FES_TOOLCHAIN_CACHE_ROOT'}
            env['PATH'] = '/usr/bin:/bin'
            source = checkout / 'sources/misteross'
            done = subprocess.run([sys.executable, '-I', '-c', probe_source(checkout), str(source), json.dumps(ROWS)],
                                  cwd=source, env=env, capture_output=True, text=True, timeout=900, check=True)
            entry = json.loads(done.stdout)['fes.pc386']
            if entry['error']:
                raise SystemExit(revision + ': ' + entry['error'])
            return entry['identities']
        finally:
            subprocess.run(['git', 'worktree', 'remove', '--force', str(checkout)], cwd=ROOT, check=True)


def main(revisions):
    results = {revision: identity(revision) for revision in revisions}
    for revision, identities in results.items():
        print(revision, json.dumps(identities, sort_keys=True))
    first = next(iter(results.values()))
    same = all(identities == first for identities in results.values())
    print('IDENTITIES ' + ('EQUAL' if same else 'DIFFER'))
    return 0 if same else 1


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
