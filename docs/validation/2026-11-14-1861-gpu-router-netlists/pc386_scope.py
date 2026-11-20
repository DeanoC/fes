#!/usr/bin/env python3
"""Does this PR touch anything the new `fes.pc386` record hashes? Answer by name.

DeanoC/fes#1951 registered `fes.pc386` in the support matrix while this PR was
open, so the affected-set measurement (design_identity.py) has no tree-A
counterpart for it and refuses to guess: its producer lives outside scripts/ and
builds its closure as an explicit path set inside its own create_build_record,
not from roots in scripts/source_closures.json.

This answers the row the one way that needs no build: take the exact path set
that producer hashes, and intersect it with the files this PR changes. An empty
intersection means the PR cannot move that record's identity, whatever else is
true of it.

The four-term path expression below is *reproduced* from
cores/fes-pc386/tools/build_pc386_product.py create_build_record (PINNED_INPUTS,
the build_fes_pc386_quartus import closure, and the three named extras). It is a
copy and can drift from the producer, so this program re-reads it from the tree
being measured and prints the term count; if the producer's expression changes,
the printed closure size changes with it and the copy must be rechecked.

  python3 pc386_scope.py --tree <main-worktree> --diff <base>...<head> [--json out]
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

MISTEROSS = "sources/misteross"
PRODUCER = "cores/fes-pc386/tools/build_pc386_product.py"


def closure_paths(tree: Path) -> list[str]:
    root = (tree / MISTEROSS).resolve()
    sys.path.insert(0, str(root))
    os.chdir(root)
    import importlib.util
    spec = importlib.util.spec_from_file_location("pc386_product", root / PRODUCER)
    producer = importlib.util.module_from_spec(spec)
    sys.modules["pc386_product"] = producer
    spec.loader.exec_module(producer)
    from scripts import source_closure
    return sorted(set(producer.PINNED_INPUTS)
                  | source_closure.imports("build_fes_pc386_quartus", root)
                  | {"scripts/source_closure.py", "scripts/bootstrap.sh",
                     "scripts/toolchain_cache.py"})


def changed(repo: Path, spec: str) -> list[str]:
    out = subprocess.run(("git", "-C", str(repo), "diff", "--name-only", spec),
                         capture_output=True, text=True, check=True).stdout.split()
    prefix = MISTEROSS + "/"
    return sorted(p[len(prefix):] for p in out if p.startswith(prefix))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree", type=Path, required=True,
                        help="a worktree at the main commit that registered the row")
    parser.add_argument("--repo", type=Path, required=True,
                        help="the PR's checkout, for the diff")
    parser.add_argument("--diff", required=True, help="e.g. origin/main...HEAD")
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    if arguments.json is not None:
        arguments.json = arguments.json.resolve()   # before any chdir
    repo = arguments.repo.resolve()
    pr_files = changed(repo, arguments.diff)
    hashed = closure_paths(arguments.tree.resolve())
    overlap = sorted(set(hashed) & set(pr_files))
    print(f"producer            {PRODUCER}")
    print(f"paths it hashes     {len(hashed)}")
    print(f"this PR changes     {len(pr_files)} under {MISTEROSS}/")
    for path in pr_files:
        print(f"  {'HASHED BY pc386' if path in hashed else 'not hashed':16} {path}")
    print()
    if overlap:
        print("MOVED: fes.pc386 hashes " + ", ".join(overlap)
              + " -- it is an affected artefact and owes its own D12 answer")
    else:
        print("fes.pc386 is NOT affected: it hashes none of the files this PR changes")
    result = {"producer": PRODUCER, "tree": str(arguments.tree),
              "revision": subprocess.run(("git", "-C", str(arguments.tree), "rev-parse", "HEAD"),
                                         capture_output=True, text=True).stdout.strip(),
              "hashed_path_count": len(hashed), "hashed_paths": hashed,
              "pr_changed_misteross": pr_files, "overlap": overlap,
              "verdict": "affected" if overlap else "not affected"}
    if arguments.json:
        arguments.json.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
