#!/usr/bin/env python3
"""D12 condition 1 for a native Quartus front end: the canonical QSF.

#1891 moved the four ZX81-family DE10 rows onto build_fes_zx81_quartus, which
reads the RTL with Quartus Lite and runs no Yosys program at all (the producer
says so itself). D12 anticipates that: its first condition is "the core's Yosys
netlist (for a native Quartus front end, its QSF) is byte-identical to main".

So the object to compare for those rows is the QSF, and the producer already
has the canonical form the record pins: canonical_qsf_sha256 builds the project
QSF at BUILD_ID 0*32, drops the seed and the per-run project path, and rewrites
the checkout prefix as <ROOT>. That is D12's own exemption set -- the BUILD_ID
constant and file paths -- applied by the producer rather than by this program,
so nothing here is normalised away after the fact.

It needs no Quartus install and compiles nothing.

  python3 qsf_identity.py --tree-a <dir> --tree-b <dir> [--json out.json]
"""
from __future__ import annotations

import argparse
import hashlib
import importlib
import json
import os
import subprocess
import sys
from pathlib import Path

MISTEROSS = "sources/misteross"
MODELS = ("zx81", "zx80", "ts1000", "ts1500")
ROW = {"zx81": "fes.zx81", "zx80": "fes.zx80", "ts1000": "fes.ts1000", "ts1500": "fes.ts1500"}


def _load(tree: Path):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    root = tree / MISTEROSS
    sys.path.insert(0, str(root))
    os.chdir(root)
    try:
        return importlib.import_module("scripts.build_fes_zx81_quartus")
    finally:
        sys.path.remove(str(root))


def _git(tree: Path, *args: str) -> str:
    return subprocess.run(("git", "-C", str(tree), *args),
                          capture_output=True, text=True, check=True).stdout.strip()


def measure(tree: Path) -> dict:
    producer = _load(tree)
    out = {"revision": _git(tree, "rev-parse", "HEAD"),
           "tree_clean": not _git(tree, "status", "--porcelain"),
           "models": {}}
    for model in MODELS:
        machine = producer.model_for(model)
        recipe = producer.selected_recipe(producer.ROOT, machine.core_id)
        # The producer's own canonical form, verbatim: BUILD_ID zeroed, seed
        # and project path dropped, checkout prefix rewritten.
        digest = producer.canonical_qsf_sha256(producer.ROOT, machine, recipe)
        project = producer.ROOT / "build/qsf-canonical/project"
        text = producer.project_qsf(producer.ROOT, project, "0" * 32,
                                    producer.recipe_seeds(recipe)[0], machine, recipe)
        canonical = producer.canonical_qsf(text, producer.ROOT, project)
        assert hashlib.sha256(canonical.encode()).hexdigest() == digest, model
        out["models"][model] = {
            "core_id": machine.core_id,
            "canonical_qsf_sha256": digest,
            "canonical_qsf_bytes": len(canonical.encode()),
            "recipe_sha256": hashlib.sha256(
                json.dumps(recipe, separators=(",", ":"), sort_keys=True).encode()).hexdigest(),
            "front_end": getattr(producer, "FRONT_END", None),
            "canonical_qsf": canonical,
        }
    return out


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree-a", type=Path, required=True)
    parser.add_argument("--tree-b", type=Path, required=True)
    parser.add_argument("--json", type=Path)
    parser.add_argument("--dump", type=Path)
    args = parser.parse_args()
    # Resolve every output path before anything chdirs. These programs import
    # producers, and _load() does os.chdir(tree) so the producer sees its own
    # tree as cwd -- so a relative --json or --dump path would be written
    # *inside the tree being measured*. That happened: three summaries landed
    # in nb2/ and nh2/ and were saved only by sources/misteross/.gitignore
    # listing *.json, which kept those worktrees clean by luck rather than by
    # design. A stray file in a measured tree can change the very cleanliness
    # the records assert.
    if args.json is not None:
        args.json = args.json.resolve()
    if args.dump is not None:
        args.dump = args.dump.resolve()
    a, b = args.tree_a.resolve(), args.tree_b.resolve()
    ma, mb = measure(a), measure(b)
    print(f'{"artefact":14} {"front end":14} verdict')
    moved = []
    for model in MODELS:
        ra, rb = ma["models"][model], mb["models"][model]
        same = ra["canonical_qsf_sha256"] == rb["canonical_qsf_sha256"]
        recipe_same = ra["recipe_sha256"] == rb["recipe_sha256"]
        if not same:
            moved.append(model)
        print(f'{ROW[model]:14} {str(ra["front_end"]):14} '
              f'QSF {"identical" if same else "MOVED"} {ra["canonical_qsf_sha256"][:16]}'
              f'{"" if same else " -> " + rb["canonical_qsf_sha256"][:16]}'
              f'   recipe {"identical" if recipe_same else "MOVED"}')
        if args.dump:
            args.dump.mkdir(parents=True, exist_ok=True)
            (args.dump / f"{model}-a.qsf").write_text(ra["canonical_qsf"])
            (args.dump / f"{model}-b.qsf").write_text(rb["canonical_qsf"])
    print()
    print("every canonical QSF is byte-identical" if not moved
          else f"QSF moved for: {', '.join(moved)}")
    if args.json:
        strip = lambda m: {"revision": m["revision"], "tree_clean": m["tree_clean"],
                           "models": {k: {n: v for n, v in row.items() if n != "canonical_qsf"}
                                      for k, row in m["models"].items()}}
        args.json.write_text(json.dumps(
            {"tree_a": {"path": str(a), **strip(ma)}, "tree_b": {"path": str(b), **strip(mb)},
             "moved": moved}, indent=2, sort_keys=True) + "\n")
        print(f"wrote {args.json}")
    return 1 if moved else 0


if __name__ == "__main__":
    raise SystemExit(main())
