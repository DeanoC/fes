#!/usr/bin/env python3
"""#1984 S4: only pc386's BUILD_ID moves, and it moves as the one-time v1-to-v2 migration.

It reuses S2's per-tree probe (../2026-11-19-1984-s2-design-v2/no_id_moves.py),
which measures each tree in its own interpreter with that tree's own modules.

Pass rule, fixed before the run:
  - every support-matrix closure-level row is the same on both trees;
  - the pc386 producer row records design-v1 on tree A and design-v2 on tree B,
    and its BUILD_ID differs (the planned migration; any other change of
    policy, or an unchanged ID, fails);
  - the sealed #1951 design-v1 record validates on both trees with the same
    design identity.

  python3 -I no_id_moves.py --tree-a <base checkout> --tree-b <head checkout> [--json out.json]

Exit status 1 if the rule fails or a row is unanswered.
"""
from __future__ import annotations

import argparse
import importlib.util
import json
from pathlib import Path

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location('s2_no_id_moves', HERE.parent / '2026-11-19-1984-s2-design-v2/no_id_moves.py')
s2 = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(s2)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('--tree-a', type=Path, required=True)
    parser.add_argument('--tree-b', type=Path, required=True)
    parser.add_argument('--json', type=Path)
    args = parser.parse_args()
    a, b = s2.probe(args.tree_a.resolve()), s2.probe(args.tree_b.resolve())
    print(f"tree A {a['revision']}\ntree B {b['revision']}\n")
    print(f"{'row':22} {'files':>5}  {'id, A':34} {'id, B':34} verdict")
    bad = []
    for core in sorted(set(a['cores']) | set(b['cores'])):
        ra, rb = a['cores'].get(core), b['cores'].get(core)
        if ra is None or rb is None or 'unanswered' in ra or 'unanswered' in rb:
            if core == 'fes.pc386':
                print(f'{core:22} {"-":>5}  {"-":34} {"-":34} see the fes.pc386 producer row')
                continue
            bad.append(core)
            print(f'{core:22} {"-":>5}  {"-":34} {"-":34} UNANSWERED: ' + (rb or ra or {}).get('unanswered', 'missing'))
            continue
        key = 'v1_design_id' if ra['v1_design_id'] is not None else 'design_closure_sha256'
        same = ra[key] == rb[key] and ra['design_closure_sha256'] == rb['design_closure_sha256']
        if not same:
            bad.append(core)
        print(f"{core:22} {rb['design_files']!s:>5}  {ra[key][:32]:34} {rb[key][:32]:34} "
              f"{'same' if same else 'MOVED'}{'' if key == 'v1_design_id' else ' (format 1: pinned inputs)'}")
    pa, pb = a['pc386_record'], b['pc386_record']
    migrated = pa['policy'] == 'design-v1' and pb['policy'] == 'design-v2' and pa['build_id'] != pb['build_id']
    print(f"\n{'fes.pc386 producer':22} {'':>5}  {pa['policy'] + ' ' + pa['build_id']:34} "
          f"{pb['policy'] + ' ' + pb['build_id']:34} "
          f"{'MOVED as the v1-to-v2 migration' if migrated else 'NOT THE PLANNED MIGRATION'}")
    if not migrated:
        bad.append('fes.pc386 producer')
    sa, sb = a['sealed_1951'], b['sealed_1951']
    same = sa == sb and sa['validates']
    print(f"{'sealed #1951 record':22} {'':>5}  {sa['design'][:32]:34} {sb['design'][:32]:34} "
          f"{'validates on both, same' if same else 'MOVED or fails validation'}")
    if not same:
        bad.append('sealed #1951 record')
    print('\n' + ('PASS: only the pc386 producer moved, as the v1-to-v2 migration; every other row '
                  'and the sealed #1951 record are the same' if not bad else 'FAIL: ' + ', '.join(bad)))
    if args.json:
        args.json.write_text(json.dumps({'tree_a': a, 'tree_b': b, 'failed': bad}, indent=1, sort_keys=True) + '\n')
    return 1 if bad else 0


if __name__ == '__main__':
    raise SystemExit(main())
