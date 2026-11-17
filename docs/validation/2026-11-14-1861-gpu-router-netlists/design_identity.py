#!/usr/bin/env python3
"""Does this PR move a registered artefact's design identity? Answer per row.

An artefact's design ID hashes the design-role half of its producer's source
closure (scripts/input_identity.py: input_role). So "is this core affected"
is a mechanical question about two trees, with no build, no Quartus and no
GPU: take each support-matrix row's producer, take that producer's own source
roots, close over them in each tree, keep the design-role entries, and
compare the digests.

The roots come from the producer itself, not from scripts/source_closures.json:
that manifest holds only the twelve registered nextpnr producers, so reading it
alone would silently miss build_fes_zx81_quartus and build_fes_splash, whose
roots are declared in the producer module.

  python3 design_identity.py --tree-a <dir> --tree-b <dir> [--json out.json]
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


def _rows(tree: Path):
    data = json.loads((tree / "docs/support-matrix.json").read_text())
    out = []
    for row in data["cores"]:
        script = row.get("producer_script")
        if not script:
            continue
        out.append((row["id"], Path(script).stem, row.get("check_level"),
                    row.get("tier"), row.get("board")))
    # The splash firmware has no producer_script cell but is a registered
    # artefact with its own producer, so name it here rather than miss it.
    splash = next((row for row in data["cores"] if row["id"] == "splash"), None)
    if splash is not None and not splash.get("producer_script"):
        out.append(("splash", "build_fes_splash", splash.get("check_level"),
                    splash.get("tier"), splash.get("board")))
    return out


def _load(tree: Path, module: str):
    for stale in [n for n in sys.modules if n == "scripts" or n.startswith("scripts.")]:
        del sys.modules[stale]
    root = tree / MISTEROSS
    sys.path.insert(0, str(root))
    os.chdir(root)
    try:
        return importlib.import_module(f"scripts.{module}")
    finally:
        sys.path.remove(str(root))


def _roots(producer):
    """The producer's own declared closure roots, however it declares them."""
    value = getattr(producer, "SOURCE_ROOTS", None)
    if value:
        return sorted(set(value))
    if callable(getattr(producer, "source_roots", None)):
        return sorted(set(producer.source_roots(producer.ROOT)))
    # nextpnr producers resolve roots at record time from the audited manifest.
    from scripts.functional_execution import source_roots_for_producer
    pinned = getattr(producer, "PINNED_INPUTS", None) or getattr(producer, "INPUTS", None)
    if pinned is None and hasattr(producer, "inputs_for"):
        pinned = tuple(producer.inputs_for())
    if pinned is None:
        raise SystemExit(f"{producer.__name__}: no pinned inputs and no SOURCE_ROOTS")
    return sorted(set(source_roots_for_producer(f"scripts.{producer.__name__.split('.')[-1]}",
                                                tuple(pinned), producer.ROOT)))


def measure(tree: Path, module: str) -> dict:
    producer = _load(tree, module)
    from scripts.export_core_package import source_input_closure
    from scripts.compiler_read_audit import POLICY
    from scripts.input_identity import input_role
    roots = _roots(producer)
    closure = source_input_closure(producer.ROOT, roots, policy=POLICY)
    design = {p: d for p, d in closure.items() if input_role(p) == "design"}
    other = {p: d for p, d in closure.items() if input_role(p) != "design"}
    return {
        "roots": len(roots),
        "closure_files": len(closure),
        "design_files": len(design),
        "design_sha256": hashlib.sha256(
            json.dumps(sorted(design.items()), separators=(",", ":")).encode()).hexdigest(),
        "non_design_sha256": hashlib.sha256(
            json.dumps(sorted(other.items()), separators=(",", ":")).encode()).hexdigest(),
        "design": design,
    }


def _revision(tree: Path) -> str:
    return subprocess.run(("git", "-C", str(tree), "rev-parse", "HEAD"),
                          capture_output=True, text=True, check=True).stdout.strip()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree-a", type=Path, required=True)
    parser.add_argument("--tree-b", type=Path, required=True)
    parser.add_argument("--json", type=Path)
    args = parser.parse_args()
    a, b = args.tree_a.resolve(), args.tree_b.resolve()
    rows_a, rows_b = _rows(a), _rows(b)
    ids_a = {row[0]: row for row in rows_a}
    result = {"tree_a": {"path": str(a), "revision": _revision(a)},
              "tree_b": {"path": str(b), "revision": _revision(b)},
              "rows": {}}
    print(f'{"artefact":22} {"producer":28} {"chk":8} {"design":7} {"verdict"}')
    for core_id, module, check_level, _tier, _board in rows_b:
        ma = measure(a, module) if core_id in ids_a and ids_a[core_id][1] == module else None
        mb = measure(b, module)
        if ma is None:
            verdict = "NEW ROW OR PRODUCER CHANGED -- no tree-a counterpart"
        elif ma["design_sha256"] == mb["design_sha256"]:
            verdict = "design identity unchanged"
        else:
            moved = sorted(k for k in set(ma["design"]) | set(mb["design"])
                           if ma["design"].get(k) != mb["design"].get(k))
            verdict = "MOVED: " + ", ".join(moved)
        result["rows"][core_id] = {
            "producer": module, "check_level": check_level,
            "tree_a": None if ma is None else {k: v for k, v in ma.items() if k != "design"},
            "tree_b": {k: v for k, v in mb.items() if k != "design"},
            "verdict": verdict,
        }
        print(f"{core_id:22} {module:28} {str(check_level):8} {mb['design_files']:<7} {verdict}")
    moved = [c for c, row in result["rows"].items() if row["verdict"].startswith("MOVED")]
    unchanged = [c for c, row in result["rows"].items()
                 if row["verdict"] == "design identity unchanged"]
    unanswered = [c for c, row in result["rows"].items()
                  if not row["verdict"].startswith(("MOVED", "design identity unchanged"))]
    result["summary"] = {"moved": sorted(moved), "unchanged": sorted(unchanged),
                         "unanswered": sorted(unanswered)}
    print()
    print(f"{len(moved)} affected, {len(unchanged)} unchanged, {len(unanswered)} unanswered")
    if args.json:
        args.json.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
        print(f"wrote {args.json}")
    # This program reports a scope; it is not a gate. An unanswered row is the
    # only failure, because that is the one case where the scope is unknown.
    return 1 if unanswered else 0


if __name__ == "__main__":
    raise SystemExit(main())
