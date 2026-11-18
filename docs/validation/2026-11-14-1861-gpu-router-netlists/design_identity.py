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
import importlib.util
import json
import os
import subprocess
import sys
from pathlib import Path, PurePosixPath

MISTEROSS = "sources/misteross"


def _rows(tree: Path):
    data = json.loads((tree / "docs/support-matrix.json").read_text())
    out = []
    for row in data["cores"]:
        script = row.get("producer_script")
        if not script:
            continue
        out.append((row["id"], PurePosixPath(script).as_posix(), row.get("check_level"),
                    row.get("tier"), row.get("board")))
    # The splash firmware has no producer_script cell but is a registered
    # artefact with its own producer, so name it here rather than miss it.
    splash = next((row for row in data["cores"] if row["id"] == "splash"), None)
    if splash is not None and not splash.get("producer_script"):
        out.append(("splash", "scripts/build_fes_splash.py", splash.get("check_level"),
                    splash.get("tier"), splash.get("board")))
    return out


def _module_name(script: str) -> str:
    """The dotted name for a producer at this path inside sources/misteross.

    Producers used to live only in scripts/, so this program derived the module
    from the file's stem. DeanoC/fes#1951 registered `fes.pc386`, whose producer
    is cores/fes-pc386/tools/build_pc386_product.py, and the stem assumption
    turned that new row into a ModuleNotFoundError traceback -- which also hid
    every row printed after it, `splash` among them. Derive the name from the
    whole path so a producer anywhere in the tree loads.
    """
    parts = PurePosixPath(script).with_suffix("").parts
    return ".".join(part.replace("-", "_") for part in parts)


def _load(tree: Path, script: str):
    for stale in [n for n in sys.modules
                  if n == "scripts" or n.startswith("scripts.") or "build_" in n]:
        del sys.modules[stale]
    root = tree / MISTEROSS
    sys.path.insert(0, str(root))
    os.chdir(root)
    try:
        name = _module_name(script)
        # There are no package __init__.py files under cores/, so a producer
        # outside scripts/ cannot be reached by import_module. Load it from its
        # file instead, under the name it would have had.
        spec = importlib.util.spec_from_file_location(name, root / script)
        if spec is None or spec.loader is None:
            raise SystemExit(f"{script}: no importable producer at this path")
        producer = importlib.util.module_from_spec(spec)
        sys.modules[name] = producer
        spec.loader.exec_module(producer)
        return producer
    finally:
        sys.path.remove(str(root))


def _emits_source_closure(producer, script: str) -> bool:
    """Does this producer's record hash a source closure at all?

    A format-1 producer (build_fes_splash) hashes its whole record instead:
    the explicit PINNED_INPUTS digests plus `repository` and `revision`. Asking
    source_roots_for_producer for its roots gets the broad fallback, the entire
    scripts/ directory, which that producer never uses. Reporting that fallback
    as its closure would attribute an invented 600-file design set and an
    invented list of moved files to a record that hashes neither.
    """
    text = (producer.ROOT / script).read_text()
    return "functional_record_fields" in text or "source_input_closure" in text


def _roots(producer, script: str):
    """The producer's own declared closure roots, however it declares them."""
    value = getattr(producer, "SOURCE_ROOTS", None)
    if value:
        return sorted(set(value))
    if callable(getattr(producer, "source_roots", None)):
        return sorted(set(producer.source_roots(producer.ROOT)))
    # The audited manifest is keyed by scripts.<name>. A producer outside
    # scripts/ is not in it, so asking anyway returns the broad fallback -- the
    # whole of scripts/ -- and this program would then report an invented
    # closure and an invented list of moved files for it. That is the splash
    # mis-attribution again, so refuse by name instead and let the row stand
    # unanswered until something measures what it really hashes.
    if not script.startswith("scripts/"):
        raise _Unanswered(
            f"{script}: closure is an explicit path set built in its own "
            "create_build_record, not roots in scripts/source_closures.json")
    # nextpnr producers resolve roots at record time from the audited manifest.
    from scripts.functional_execution import source_roots_for_producer
    pinned = getattr(producer, "PINNED_INPUTS", None) or getattr(producer, "INPUTS", None)
    if pinned is None and hasattr(producer, "inputs_for"):
        pinned = tuple(producer.inputs_for())
    if pinned is None:
        raise SystemExit(f"{producer.__name__}: no pinned inputs and no SOURCE_ROOTS")
    return sorted(set(source_roots_for_producer(f"scripts.{producer.__name__.split('.')[-1]}",
                                                tuple(pinned), producer.ROOT)))


class _Unanswered(Exception):
    """This program cannot say what that producer hashes. Say so by name."""


def measure(tree: Path, script: str) -> dict:
    producer = _load(tree, script)
    from scripts.export_core_package import source_input_closure
    from scripts.compiler_read_audit import POLICY
    try:
        from scripts.input_identity import input_role
        split = True
    except ModuleNotFoundError:
        # design-v1 (#1882) is newer than the old evidence base, so a tree from
        # before it has no role split. Compare the whole closure there and say
        # so, rather than refusing to answer or pretending to a split.
        input_role, split = (lambda path: "design"), False
    if not _emits_source_closure(producer, script):
        return _measure_format1(producer)
    roots = _roots(producer, script)
    closure = source_input_closure(producer.ROOT, roots, policy=POLICY)
    design = {p: d for p, d in closure.items() if input_role(p) == "design"}
    other = {p: d for p, d in closure.items() if input_role(p) != "design"}
    return {
        "role_split": split,
        "roots": len(roots),
        "closure_files": len(closure),
        "design_files": len(design),
        "design_sha256": hashlib.sha256(
            json.dumps(sorted(design.items()), separators=(",", ":")).encode()).hexdigest(),
        "closure_sha256": hashlib.sha256(
            json.dumps(sorted(closure.items()), separators=(",", ":")).encode()).hexdigest(),
        "non_design_sha256": hashlib.sha256(
            json.dumps(sorted(other.items()), separators=(",", ":")).encode()).hexdigest(),
        "design": design,
        "closure": closure,
    }


def _measure_format1(producer) -> dict:
    """What a format-1 record actually hashes: PINNED_INPUTS, and the revision.

    `revision` is a field of the record the identity is taken over, so such an
    identity moves on every commit whether or not any input changed. That is
    why the per-file list matters more here than the digest: it says which of
    the producer's own declared inputs this change touches.
    """
    inputs = tuple(getattr(producer, "PINNED_INPUTS"))
    listing = {relative: hashlib.sha256((producer.ROOT / relative).read_bytes()).hexdigest()
               for relative in sorted(inputs)}
    return {
        "record_format": 1,
        "identity_covers": "whole record: pinned inputs, repository and revision",
        "roots": 0,
        "closure_files": len(listing),
        "design_files": len(listing),
        "design_sha256": hashlib.sha256(
            json.dumps(sorted(listing.items()), separators=(",", ":")).encode()).hexdigest(),
        "closure_sha256": hashlib.sha256(
            json.dumps(sorted(listing.items()), separators=(",", ":")).encode()).hexdigest(),
        "non_design_sha256": None,
        "role_split": True,
        "design": listing,
        "closure": listing,
    }


def _revision(tree: Path, spec: str = "HEAD") -> str:
    return subprocess.run(("git", "-C", str(tree), "rev-parse", spec),
                          capture_output=True, text=True, check=True).stdout.strip()


def _tree_identity(tree: Path) -> dict:
    """What this comparison was taken over, in the two forms a reader needs.

    `revision` says which commit was checked out; `sources_tree` is the git
    tree object of sources/misteross, which changes exactly when those sources
    change. A consumer that must check "is this comparison still about the
    tree I am asking about" has to compare the latter, because a
    documentation-only commit -- this record's own, for one -- moves the
    revision and nothing a producer reads.
    """
    return {"path": str(tree), "revision": _revision(tree),
            "sources_tree": _revision(tree, "HEAD:" + MISTEROSS)}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tree-a", type=Path, required=True)
    parser.add_argument("--tree-b", type=Path, required=True)
    parser.add_argument("--json", type=Path)
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
    a, b = args.tree_a.resolve(), args.tree_b.resolve()
    rows_a, rows_b = _rows(a), _rows(b)
    ids_a = {row[0]: row for row in rows_a}
    result = {"tree_a": _tree_identity(a), "tree_b": _tree_identity(b),
              "rows": {}}
    print(f'{"artefact":22} {"producer":40} {"chk":8} {"design":7} {"verdict"}')
    for core_id, module, check_level, _tier, _board in rows_b:
        try:
            ma = (measure(a, module)
                  if core_id in ids_a and ids_a[core_id][1] == module else None)
            mb = measure(b, module)
        except _Unanswered as refusal:
            result["rows"][core_id] = {
                "producer": module, "check_level": check_level,
                "tree_a": None, "tree_b": None,
                "verdict": f"UNANSWERED: {refusal}"}
            print(f"{core_id:22} {module:40} {str(check_level):8} {'-':<7} "
                  f"UNANSWERED: {refusal}")
            continue
        if ma is None:
            verdict = "NEW ROW OR PRODUCER CHANGED -- no tree-a counterpart"
        else:
            # The design/toolchain split arrived with design-v1 (#1882). If
            # either side predates it, the two "design" sets are built by
            # different rules and differencing them reports every file the
            # newer side reclassifies as toolchain -- bootstrap.sh, the locks,
            # lockfile.py -- as though its bytes had changed. Compare the whole
            # closure on both sides instead, and say which was compared.
            split = ma.get("role_split", True) and mb.get("role_split", True)
            field = "design" if split else "closure"
            label = "design identity" if split else "source closure (no role split on one side)"
            if ma[f"{field}_sha256"] == mb[f"{field}_sha256"]:
                verdict = f"{label} unchanged"
            else:
                moved = sorted(k for k in set(ma[field]) | set(mb[field])
                               if ma[field].get(k) != mb[field].get(k))
                verdict = f"MOVED ({label}): " + ", ".join(moved)
        result["rows"][core_id] = {
            "producer": module, "check_level": check_level,
            "tree_a": None if ma is None else {k: v for k, v in ma.items()
                                               if k not in ("design", "closure")},
            "tree_b": {k: v for k, v in mb.items() if k not in ("design", "closure")},
            "verdict": verdict,
        }
        print(f"{core_id:22} {module:40} {str(check_level):8} "
              f"{mb['design_files']:<7} {verdict}")
    moved = [c for c, row in result["rows"].items() if row["verdict"].startswith("MOVED")]
    unchanged = [c for c, row in result["rows"].items()
                 if row["verdict"].endswith("unchanged")]
    unanswered = [c for c, row in result["rows"].items()
                  if not (row["verdict"].startswith("MOVED")
                          or row["verdict"].endswith("unchanged"))]
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
