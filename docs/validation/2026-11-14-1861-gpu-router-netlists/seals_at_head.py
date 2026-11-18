#!/usr/bin/env python3
"""Are this record's condition-3 seals still seals of the head's sources?

Condition 3 never carries across a base move: it is about the identity that
ships. But a seal is not invalidated by a merge that leaves its inputs alone --
it is invalidated by a merge that changes them. The revision string a seal
records is the weaker test of the two, because it says which tree was checked
out rather than which bytes were compiled, and it goes stale on every merge
whether or not anything the build read has moved.

So re-check the stronger one at the final head: for every row in cond3.json,
read the build summary the seal came from and compare each digest in its
`inputs` map against the file at that path in the tree given here. All matching
means the sealed build consumed exactly the bytes this head holds, and the
identity it closed timing on is the identity this head produces.

Not every producer writes an inputs map into its build summary. The ZX81
family's Quartus summaries carry `build_id` and `revision` and nothing else, so
for those the tie is the second anchor: a design-identity comparison between the
tree the seal was taken in and this head, from design_identity.py. A design-v1
BUILD_ID *is* that identity, so two trees that agree on it produce the same
artefact -- which is the thing condition 3 is about. Pass that comparison with
--design-move. A row with neither anchor stays unverifiable, by name.

  python3 seals_at_head.py --cond3 reports-cond3/cond3.json --tree <checkout> \
      [--design-move seal-to-head-design.json] [--json out]
"""
from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from pathlib import Path


def _design_anchor(key: str, moves: dict | None, sealed_at: str | None,
                   sources_tree: str) -> tuple[str, str] | None:
    """The seal tree and this head agree on what this artefact's design is.

    The detail line this returns names the comparison's tree A as *the seal
    tree*, so that had better be what it is. Handed the wrong comparison --
    say a base-move measurement, whose tree A is the old merge base -- the
    numbers would all be real and the claim attached to them false, which is
    the one failure prose cannot catch. So the row's own `taken_at_head` has
    to be the revision the comparison was taken from, or this anchor is
    refused by name rather than relabelled.

    Both ends have to be checked, and the first version of this guard checked
    only tree A. Codex raised the other end on #1862: handed a comparison from
    an earlier run of this record -- the right seal tree, but a tree B that is
    some older head -- the guard would pass it, and the detail line would say
    "this head" of a comparison that ended somewhere else. So tree B's
    `sources_tree` must be the requested checkout's. The tree object and not
    the revision, because this record's own documentation commits move the
    revision while leaving every byte a producer reads alone, and a guard that
    rejected those would be retired the first time it cried wolf.
    """
    if not moves:
        return None
    row = moves.get("rows", {}).get(key if key == "splash" else f"fes.{key}")
    if row is None:
        return None
    tree_a = moves.get("tree_a", {}).get("revision") or ""
    if not sealed_at or tree_a != sealed_at:
        return "unverifiable", (
            f"the design comparison is from {tree_a[:12] or '?'}, not this row's "
            f"seal tree {str(sealed_at)[:12]}")
    ended_at = moves.get("tree_b", {}).get("sources_tree")
    if ended_at is None:
        return "unverifiable", (
            "the design comparison records no tree_b sources_tree, so it cannot be "
            "shown to end at this head; re-run design_identity.py")
    if ended_at != sources_tree:
        return "unverifiable", (
            f"the design comparison ends at sources tree {ended_at[:12]}, not this "
            f"head's {sources_tree[:12]}")
    verdict = row.get("verdict", "")
    if verdict.endswith("unchanged"):
        return "intact", (f"design identity unchanged between the seal tree "
                          f"{tree_a[:12]} and this head "
                          f"({row['tree_b']['design_files']} design inputs)")
    return "MOVED", f"design identity comparison says: {verdict[:120]}"


def verify(summary: Path, root: Path) -> tuple[str, str]:
    if not summary.is_file():
        return "unverifiable", f"the seal's summary is gone from {summary.parent}"
    data = json.loads(summary.read_text())
    inputs = data.get("inputs")
    if not isinstance(inputs, dict) or not inputs:
        return "unverifiable", "the summary carries no inputs map"
    wrong = []
    for relative, digest in sorted(inputs.items()):
        path = root / relative
        if not path.is_file():
            wrong.append(f"{relative} absent")
        elif hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            wrong.append(relative)
    if wrong:
        return "MOVED", f"{len(wrong)} of {len(inputs)} inputs differ: " + ", ".join(wrong[:3])
    return "intact", f"all {len(inputs)} input digests match this head"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--cond3", type=Path, required=True)
    parser.add_argument("--tree", type=Path, required=True,
                        help="the checkout whose head the seals must still match")
    parser.add_argument("--design-move", type=Path,
                        help="design_identity.py json comparing the seal tree to this head")
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    moves = (json.loads(arguments.design_move.read_text())
             if arguments.design_move is not None else None)
    root = (arguments.tree / "sources/misteross").resolve()
    def rev(spec: str) -> str:
        return subprocess.run(("git", "-C", str(arguments.tree), "rev-parse", spec),
                              capture_output=True, text=True).stdout.strip()

    head = rev("HEAD")
    # The anchor this record cites is the source tree object, not the commit.
    # A commit SHA moves every time this record's own documents are committed,
    # including the commit that stores this very result -- so citing it would
    # leave the statement one commit stale the moment it was written, for a
    # reason that has nothing to do with what was measured. The tree object of
    # sources/misteross changes exactly when those sources change, which is the
    # thing the seals are claims about.
    sources = rev("HEAD:sources/misteross")
    rows = json.loads(arguments.cond3.read_text())
    result, bad = {}, []
    print(f"head             {head}")
    print(f"sources/misteross {sources}  <- the anchor; a docs commit does not move it")
    print(f'{"artefact":20} {"sealed at":14} {"verdict":13} detail')
    for key in sorted(rows):
        row = rows[key]
        verdict, detail = verify(Path(row["source"]), root)
        if verdict == "unverifiable":
            fallback = _design_anchor(key, moves, row.get("taken_at_head"),
                                      sources)
            if fallback is not None:
                verdict, detail = fallback
        if verdict != "intact":
            bad.append(key)
        result[key] = {"sealed_at": row.get("taken_at_head"), "build_id": row.get("build_id"),
                       "summary": row["source"], "verdict": verdict, "detail": detail}
        print(f"{key:20} {str(row.get('taken_at_head'))[:12]:14} {verdict:13} {detail}")
    print()
    print(f"{len(rows) - len(bad)} of {len(rows)} seals still match this head"
          + ("" if not bad else "; NOT INTACT: " + ", ".join(bad)))
    if arguments.json:
        arguments.json.resolve().write_text(json.dumps(
            {"head": head, "sources_tree": sources, "tree": str(root), "rows": result,
             "intact": not bad, "not_intact": bad}, indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json.resolve()}")
    return 1 if bad else 0


if __name__ == "__main__":
    raise SystemExit(main())
