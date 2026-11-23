#!/usr/bin/env python3
"""de10_design_id.py TREE: the DE10 pc386 design identity (BUILD_ID's design half) that the
registered producer's create_build_record selects for TREE, selection-only (no tool, no
Quartus), as the i486 records' head_matches_seal.py computes it. Run it on a base and a head:
equal output means the head builds the same DE10 design.

  python3 -I de10_design_id.py TREE
"""
import json
import subprocess
import sys
from pathlib import Path

PROBE = r'''
import json, sys
from pathlib import Path
root = Path(sys.argv[1]).resolve() / 'sources/misteross'
sys.path[:0] = [str(root), str(root / 'cores/fes-pc386/tools')]
import build_pc386_product as product
record = json.loads(product.create_build_record(
    root, 'https://github.com/DeanoC/fes', '0' * 40,
    {'yosys': 'selection-only', 'quartus': 'selection-only'}, 1, 'CUDA',
    execution={'toolchain': 'quartus', 'selection_only': True}))
print(json.dumps({'policy': record['parameters']['identity_policy'], 'design': record['identities']['design']}))
'''

tree = Path(sys.argv[1]).resolve()
out = subprocess.run([sys.executable, '-I', '-c', PROBE, str(tree)], capture_output=True, text=True,
                     cwd=tree / 'sources/misteross', check=True)
head = json.loads(out.stdout.strip().splitlines()[-1])
revision = subprocess.run(['git', '-C', str(tree), 'rev-parse', 'HEAD'], capture_output=True, text=True).stdout.strip()
dirty = subprocess.run(['git', '-C', str(tree), 'status', '--porcelain', '--untracked-files=no'],
                       capture_output=True, text=True).stdout.strip()
print(f"{revision}{' (modified)' if dirty else ''}  {head['policy']}  BUILD_ID {head['design'][:32]}")
