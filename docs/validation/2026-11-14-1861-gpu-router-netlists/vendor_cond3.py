#!/usr/bin/env python3
"""Bring a condition-3 collection's seal summaries into the record itself.

`collect_cond3.py` runs against live build trees, so every row it writes
names its summary by absolute path inside that campaign's scratch
(`/data/fes1861-netlist/identity/...`). `seals_at_head.py` *opens* that
path: it re-checks a seal by hashing the files the summary's `inputs` map
names. So once the scratch is deleted -- and 72 GB of it was -- the
record's strongest check cannot run at all. The eleven summaries are not
reproducible from the merged record either: they are sign-off output of
hours of Quartus and nextpnr, and because the `BUILD_ID` constant is
synthesised with `chparam` and reshuffles placement, a rerun is not
obliged to land on the same achieved frequencies. This record measures a
9.4% swing on identical RTL from identity alone.

So copy each summary in beside the row that cites it and repoint `source`
at the copy, as a path relative to `cond3.json`, which is what makes a
fresh clone enough to verify. `source_sha256` records the bytes that were
copied, so a later edit of a vendored summary is caught rather than
trusted, and `original_source` keeps the provenance the absolute path
carried.

Idempotent: run against an already-vendored `cond3.json` and it verifies
the copies instead of rewriting them.

  python3 vendor_cond3.py --cond3 <collected cond3.json> \
      --into reports-cond3 [--check]
"""
from __future__ import annotations

import argparse
import hashlib
import json
import shutil
from pathlib import Path

SUBDIR = "summaries"


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--cond3", type=Path, required=True,
                        help="the collection to vendor; may already be vendored")
    parser.add_argument("--into", type=Path, required=True,
                        help="the record directory that will hold cond3.json and its summaries")
    parser.add_argument("--check", action="store_true",
                        help="verify an already-vendored collection, copying nothing")
    arguments = parser.parse_args()

    rows = json.loads(arguments.cond3.read_text())
    into = arguments.into.resolve()
    bad = []
    for key in sorted(rows):
        row = rows[key]
        # Flattened to one directory per artefact. The scratch layout had a
        # `build/` component and a Quartus attempt directory in it, which say
        # nothing here and invite a .gitignore rule to eat the file silently.
        relative = f"{SUBDIR}/{key}/build-summary.json"
        target = into / relative
        before = len(bad)
        source = Path(row["source"])
        if not source.is_absolute():
            source = (arguments.cond3.resolve().parent / source).resolve()
        if arguments.check:
            if not target.is_file():
                bad.append(f"{key}: no vendored summary at {relative}")
                continue
            recorded = row.get("source_sha256")
            if recorded is None:
                bad.append(f"{key}: vendored but carries no source_sha256")
            elif digest(target) != recorded:
                bad.append(f"{key}: {relative} is not the bytes source_sha256 names")
            if row.get("source") != relative:
                bad.append(f"{key}: source is {row.get('source')!r}, not {relative!r}")
            # Only call it ok if this row added nothing to `bad` -- printing ok
            # beside a row that just failed is how a failing check reads as a
            # passing one.
            if len(bad) == before:
                print(f"{key:18} {relative}  ok")
            continue
        if not source.is_file():
            bad.append(f"{key}: {source} is gone, so this row cannot be vendored")
            continue
        taken = digest(source)
        recorded = row.get("source_sha256")
        if recorded is not None and recorded != taken:
            bad.append(f"{key}: {source} is not the bytes source_sha256 already names")
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        # Re-running this on an already-vendored collection resolves source and
        # target to the same file, and shutil would raise. The copy is a no-op
        # there; the digest recorded below is the check that matters.
        if source.resolve() != target.resolve():
            shutil.copy2(source, target)
        row["source_sha256"] = taken
        # Keep where it came from, and keep the tree it was measured in under a
        # name that does not read as a path something still opens.
        row.setdefault("original_source", str(source))
        if "tree" in row:
            row["original_tree"] = row.pop("tree")
        row["source"] = relative
        print(f"{key:18} {source}\n{'':18} -> {relative}  {taken[:12]}")

    if not arguments.check:
        out = into / "cond3.json"
        out.write_text(json.dumps(rows, indent=2, sort_keys=True) + "\n")
        print(f"\nwrote {out}")
    print(f"{len(rows) - len(bad)} of {len(rows)} rows "
          + ("verified" if arguments.check else "vendored")
          + ("" if not bad else "; PROBLEMS:\n  " + "\n  ".join(bad)))
    return 1 if bad else 0


if __name__ == "__main__":
    raise SystemExit(main())
