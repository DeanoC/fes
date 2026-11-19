#!/usr/bin/env python3
"""#1984 S2: does adding design-v2 move any recorded core's BUILD_ID? Measured, per core.

Two trees, A (the base) and B (the S2 head). Each tree is measured in its own
interpreter, so each side runs its own scripts/input_identity.py and its own
producers:

  every support-matrix row  design_identity.py (the #1861 record's tool)
        closes the row's producer's own source roots, and this tree's own
        input_identity.with_identities computes a design-v1 identity over that
        closure with empty parameters. This is a closure-level stand-in, not
        the row's BUILD_ID: it shows the design-role closure and the identity
        function are unchanged. The parameters a producer records are computed
        by its own code, which is in that closure.
  pc386 (fes.pc386)  the registered producer's own create_build_record, run
        selection-only with no tool, and its record's design identity: the
        BUILD_ID a seal of that tree would carry.
  the sealed #1951 record  validated by each tree's module, and its stored
        design identity recomputed.

  python3 -I no_id_moves.py --tree-a <base checkout> --tree-b <head checkout> [--json out.json]

Exit status 1 if any ID differs or any row is unanswered.
"""
from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
DESIGN_IDENTITY = HERE.parent / '2026-11-14-1861-gpu-router-netlists/design_identity.py'
SEALED = HERE.parent / '2026-11-18-1984-pc386-identity-scope/sealed-1951-build-inputs.json'

# Runs inside one tree, in a fresh interpreter.
PROBE = r'''
import contextlib, importlib.util, io, json, os, sys
from pathlib import Path
tree, tool, sealed = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
root = tree / 'sources/misteross'
spec = importlib.util.spec_from_file_location('design_identity', tool)
di = importlib.util.module_from_spec(spec); spec.loader.exec_module(di)
result = {'cores': {}, 'revision': di._revision(tree)}
for core_id, script, *_ in di._rows(tree):
    try:
        with contextlib.redirect_stdout(io.StringIO()):
            measured = di.measure(tree, script)
    except di._Unanswered as refusal:
        result['cores'][core_id] = {'producer': script, 'unanswered': str(refusal)}
        continue
    sys.path.insert(0, str(root))
    from scripts.input_identity import with_identities
    synthetic = {'format': 2, 'source_inputs': measured['closure'], 'tools': {}, 'parameters': {}}
    if measured.get('record_format') == 1:
        design_id = None  # A format-1 record hashes the whole record; no design projection.
    else:
        design_id = with_identities(synthetic)['identities']['design']
    result['cores'][core_id] = {'producer': script, 'design_files': measured['design_files'],
                                'design_closure_sha256': measured['design_sha256'],
                                'v1_design_id': design_id}
os.chdir(root)
sys.path[:0] = [str(root), str(root / 'cores/fes-pc386/tools')]
for name in [n for n in sys.modules if n == 'scripts' or n.startswith('scripts.') or 'build_' in n]:
    del sys.modules[name]
import build_pc386_product as product
from scripts import input_identity
record = json.loads(product.create_build_record(
    root, 'https://github.com/DeanoC/fes', '0' * 40,
    {'yosys': 'selection-only', 'quartus': 'selection-only'}, 1, 'HIP',
    execution={'toolchain': 'quartus', 'selection_only': True}))
result['pc386_record'] = {'policy': record['parameters']['identity_policy'],
                          'build_id': record['identities']['design'][:32],
                          'design': record['identities']['design']}
stored = json.loads(Path(sealed).read_text())
try:
    input_identity.validate_identities(stored)
    validates = True
except ValueError:
    validates = False
result['sealed_1951'] = {'validates': validates, 'design': input_identity.identities(stored)['design']}
print(json.dumps(result))
'''


def probe(tree: Path) -> dict:
    out = subprocess.run([sys.executable, '-I', '-c', PROBE, str(tree), str(DESIGN_IDENTITY), str(SEALED)],
                         capture_output=True, text=True, cwd=tree)
    if out.returncode:
        raise SystemExit(f'{tree}: probe failed\n{out.stdout}{out.stderr}')
    return json.loads(out.stdout.strip().splitlines()[-1])


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--tree-a', type=Path, required=True)
    parser.add_argument('--tree-b', type=Path, required=True)
    parser.add_argument('--json', type=Path)
    args = parser.parse_args()
    a, b = probe(args.tree_a.resolve()), probe(args.tree_b.resolve())
    print(f"tree A {a['revision']}\ntree B {b['revision']}\n")
    print(f"{'row':22} {'files':>5}  {'closure v1 id, A':34} {'closure v1 id, B':34} verdict")
    bad = []
    for core in sorted(set(a['cores']) | set(b['cores'])):
        ra, rb = a['cores'].get(core), b['cores'].get(core)
        if ra is None or rb is None or 'unanswered' in ra or 'unanswered' in rb:
            why = (rb or ra or {}).get('unanswered', 'row missing on one side')
            if core == 'fes.pc386':  # Answered by its own producer's record below.
                verdict = 'see the fes.pc386 producer row'
            else:
                verdict = 'UNANSWERED: ' + why
                bad.append(core)
            ida = idb = '-'
            files = '-'
        else:
            key = 'v1_design_id' if ra['v1_design_id'] is not None else 'design_closure_sha256'
            ida, idb = ra[key][:32], rb[key][:32]
            files = rb['design_files']
            same = ra[key] == rb[key] and ra['design_closure_sha256'] == rb['design_closure_sha256']
            verdict = ('same' if same else 'MOVED') + ('' if key == 'v1_design_id' else ' (format 1: pinned inputs)')
            if not same:
                bad.append(core)
        print(f'{core:22} {files!s:>5}  {ida:34} {idb:34} {verdict}')
    pa, pb = a['pc386_record'], b['pc386_record']
    same = pa == pb
    print(f"\n{'fes.pc386 producer':22} {pa['policy']:>5}  {pa['build_id']:34} {pb['build_id']:34} "
          f"{'same' if same else 'MOVED'} (create_build_record, selection-only)")
    if not same:
        bad.append('fes.pc386 producer')
    sa, sb = a['sealed_1951'], b['sealed_1951']
    same = sa == sb and sa['validates']
    print(f"{'sealed #1951 record':22} {'':>5}  {sa['design'][:32]:34} {sb['design'][:32]:34} "
          f"{'validates on both, same' if same else 'MOVED or fails validation'}")
    if not same:
        bad.append('sealed #1951 record')
    print('\n' + ('NOTHING MOVED: every closure-level row, the pc386 BUILD_ID and the sealed record'
                  if not bad else 'MOVED OR UNANSWERED: ' + ', '.join(bad)))
    if args.json:
        args.json.write_text(json.dumps({'tree_a': a, 'tree_b': b, 'moved_or_unanswered': bad},
                                        indent=1, sort_keys=True) + '\n')
    return 1 if bad else 0


if __name__ == '__main__':
    raise SystemExit(main())
