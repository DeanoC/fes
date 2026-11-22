#!/usr/bin/env python3
"""Does a checkout still build the sealed design? Prints the claim and exits 1 if not.

The registered pc386 producer's own create_build_record runs selection-only on
TREE (no tool, no Quartus). Its design identity, which is the first 32 hex
digits of BUILD_ID, must equal the design identity of the migration seal's
record (seal/build-inputs.json beside this script: the fes#2087 seal). A checkout that only adds
records, docs, tests or other files outside pc386's design closure passes; any
design-input change fails, and needs a new seal.

  python3 -I head_matches_seal.py TREE
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


def main() -> int:
    tree = Path(sys.argv[1]).resolve()
    sealed = json.loads((Path(__file__).resolve().parent / 'seal' / 'build-inputs.json').read_text())
    out = subprocess.run([sys.executable, '-I', '-c', PROBE, str(tree)], capture_output=True, text=True,
                         cwd=tree / 'sources/misteross', check=True)
    head = json.loads(out.stdout.strip().splitlines()[-1])
    revision = subprocess.run(['git', '-C', str(tree), 'rev-parse', 'HEAD'], capture_output=True, text=True).stdout.strip()
    same = head['policy'] == sealed['parameters']['identity_policy'] and head['design'] == sealed['identities']['design']
    print(f"sealed  {sealed['revision']}  {sealed['parameters']['identity_policy']}  BUILD_ID {sealed['identities']['design'][:32]}")
    print(f"tree    {revision}  {head['policy']}  BUILD_ID {head['design'][:32]}")
    print('SAME DESIGN: this tree builds the sealed design' if same else
          'DIFFERENT DESIGN: this tree needs a new seal')
    return 0 if same else 1


if __name__ == '__main__':
    raise SystemExit(main())
