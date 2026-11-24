#!/usr/bin/env python3
"""de25_matches_fit.py TREE: does TREE's HEAD still select the DE25 pc386 design the fits built?

The registered DE25 producer's create_build_record runs at TREE's HEAD with the fitted
record's own tool identities and execution inputs (fits/build-inputs.json and
fits/execution.json beside this script; no tool runs). Its design identity must equal the
fitted record's. Prints both and exits 1 if they differ.

  python3 -I de25_matches_fit.py TREE
"""
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
PROBE = r'''
import json, subprocess, sys
from pathlib import Path
root = Path(sys.argv[1]).resolve() / 'sources/misteross'
sys.path.insert(0, str(root))
from boards.de25nano.producers import build_pc386_package as pc386
fitted = json.loads(Path(sys.argv[2]).read_text())
execution = json.loads(Path(sys.argv[3]).read_text())
revision = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=root, text=True).strip()
record = json.loads(pc386.create_build_record(root, fitted['repository'], revision, fitted['tools'], execution,
                                              model=fitted['parameters']['model']))
print(json.dumps({'revision': revision, 'design': record['identities']['design']}))
'''

tree = Path(sys.argv[1]).resolve()
fitted = json.loads((HERE / 'fits/build-inputs.json').read_text())
out = subprocess.run([sys.executable, '-I', '-c', PROBE, str(tree), str(HERE / 'fits/build-inputs.json'),
                      str(HERE / 'fits/execution.json')], capture_output=True, text=True,
                     cwd=tree / 'sources/misteross', check=True)
head = json.loads(out.stdout.strip().splitlines()[-1])
print(f"fitted  {fitted['revision']}  design {fitted['identities']['design']}")
print(f"tree    {head['revision']}  design {head['design']}")
same = head['design'] == fitted['identities']['design']
print('SAME DESIGN: the tree builds the fitted DE25 design' if same else 'DIFFERENT DESIGN: the fits do not cover this tree')
sys.exit(0 if same else 1)
