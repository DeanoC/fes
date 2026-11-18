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

The summaries this reads are vendored into the record (vendor_cond3.py), so a
row names its summary relative to its own cond3.json and carries that file's
`source_sha256`. Before the vendoring, every row pointed at a path inside a
campaign scratch directory that no longer exists, which made the strongest of
these checks unrunnable from a clone.

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
                   anchors: dict[str, str]) -> tuple[str, str] | None:
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

    This comparison covers the producer's source closure and nothing else, so
    it cannot speak for the inputs the producer reads from the parent
    repository -- config/core-recipes.toml above all, which the ZX81 Quartus
    producer declares as PARENT_CONFIG and which the placement digests are
    read from. Codex raised that next. Requiring the whole config/ tree to
    match would be both too strict and beside the point: main's third base
    move added 22 lines to that registry for a different core, so the tree
    moved while nothing any sealed artefact reads did. `_route_anchor` below
    checks the thing that is actually claimed, per artefact.
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
    there = moves.get("tree_b", {}).get("sources_tree")
    if there is None:
        return "unverifiable", (
            "the design comparison records no tree_b sources_tree, so it cannot be "
            "shown to end at this head; re-run design_identity.py")
    if there != anchors["sources_tree"]:
        return "unverifiable", (
            f"the design comparison ends at sources tree {there[:12]}, not this "
            f"head's {anchors['sources_tree'][:12]}")
    verdict = row.get("verdict", "")
    if verdict.endswith("unchanged"):
        return "intact", (f"design identity unchanged between the seal tree "
                          f"{tree_a[:12]} and this head "
                          f"({row['tree_b']['design_files']} design inputs)")
    return "MOVED", f"design identity comparison says: {verdict[:120]}"


def _route_anchor(key: str, routes: dict[str, dict],
                  sealed_at: str | None) -> tuple[str, str]:
    """This artefact's route-deciding inputs are the same at the seal and here.

    The seals' own anchors do not reach the parent repository: a design
    comparison covers the source closure, and a producer's inputs map covers
    the files it declares under sources/misteross. Neither covers
    config/core-recipes.toml, where the placement recipe comes from. So every
    row also needs a route_inputs.py comparison from its own seal tree to this
    head, whose per-artefact row includes `recipe_digest` and
    `design_recipe_digest` -- read through the producer's own pnr_recipe rather
    than restated here -- plus the lock, QSF and SDC.

    A row with no such comparison is unverifiable, not intact: this is the
    check that the parent-owned inputs did not move, and absent, nothing says
    they did not.
    """
    report = routes.get(sealed_at or "")
    if report is None:
        return "unverifiable", (
            f"no route-input comparison from this row's seal tree "
            f"{str(sealed_at)[:12]} to this head")
    row = report["rows"].get(key if key == "splash" else f"fes.{key}")
    if row is None:
        return "unverifiable", "that comparison has no row for this artefact"
    verdict = row.get("verdict") or {}
    if not verdict:
        return "unverifiable", "that comparison recorded no input digests for it"
    moved = sorted(name for name, value in verdict.items() if value == "MOVED")
    if moved:
        return "MOVED", "route-deciding inputs moved since the seal: " + ", ".join(moved)
    unanswered = sorted(name for name, value in verdict.items() if value == "?")
    if unanswered:
        return "unverifiable", ("route-deciding inputs unanswered since the seal: "
                               + ", ".join(unanswered))
    return "intact", f"and its {len(verdict)} route-deciding inputs are unchanged too"


def _usable_routes(paths, anchors: dict[str, str]) -> tuple[dict[str, dict], list[str]]:
    """Index the route comparisons that end at this head, by the tree they start from."""
    usable, refused = {}, []
    for path in paths or ():
        report = json.loads(Path(path).read_text())
        ends = report.get("tree_b", {})
        wrong = [name for name, here in anchors.items() if ends.get(name) != here]
        if wrong:
            refused.append(f"{Path(path).name}: does not end at this head ({', '.join(wrong)})")
            continue
        if report.get("dirty"):
            refused.append(f"{Path(path).name}: measured inputs were dirty")
            continue
        usable[report.get("tree_a", {}).get("revision", "")] = report
    return usable, refused


def verify(summary: Path, root: Path, expected: str | None = None,
           pinned: bool = False) -> tuple[str, str]:
    if not summary.is_file():
        return "unverifiable", f"the seal's summary is gone from {summary.parent}"
    # A vendored summary lives in the tree under review, so it is editable and
    # must be pinned. Leaving source_sha256 off would otherwise turn the check
    # below off silently, which is the quietest way to lose it.
    if pinned and expected is None:
        return "unpinned", ("a vendored summary with no source_sha256 is not tied to the "
                            "bytes the producer wrote")
    # The summaries are vendored into the record now, so they are editable
    # files in the tree being checked rather than read-only scratch. A kept
    # `source_sha256` that nothing reads proves nothing, so read it: this says
    # the summary being hashed against is byte-identical to the one the
    # producer wrote. A mismatch is its own verdict, not an "unverifiable"
    # row, because an edited seal must not be rescued by a design anchor.
    if expected is not None and hashlib.sha256(summary.read_bytes()).hexdigest() != expected:
        return "SUBSTITUTED", ("the summary is not the bytes source_sha256 names, so it is "
                               "not the one the producer wrote")
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
    parser.add_argument("--route-inputs", type=Path, nargs="+",
                        help="route_inputs.py json(s) comparing a seal tree to this head; "
                             "one per distinct seal tree in --cond3")
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
    # Every tree the design comparison must still be about: the sources, and
    # the parent repository's config/, which holds the recipe registry the
    # placement digests come from.
    anchors = {"sources_tree": sources, "config_tree": rev("HEAD:config")}
    routes, refused = _usable_routes(arguments.route_inputs, anchors)
    rows = json.loads(arguments.cond3.read_text())
    result, bad = {}, []
    print(f"head             {head}")
    print(f"sources/misteross {sources}  <- the anchor; a docs commit does not move it")
    print(f"config            {anchors['config_tree']}  <- the parent-owned inputs, "
          "core-recipes.toml among them")
    for line in refused:
        print(f"IGNORED route comparison -- {line}")
    print("route comparisons in use: "
          + (", ".join(f"{rev_[:12]} -> this head" for rev_ in sorted(routes)) or "none"))
    print(f'{"artefact":20} {"sealed at":14} {"verdict":13} detail')
    for key in sorted(rows):
        row = rows[key]
        # A vendored row names its summary relative to this cond3.json, so that
        # a fresh clone verifies wherever it was cloned to. A collection still
        # pointing into its own build scratch carries an absolute path.
        summary = Path(row["source"])
        vendored = not summary.is_absolute()
        if vendored:
            summary = arguments.cond3.resolve().parent / summary
        verdict, detail = verify(summary, root, row.get("source_sha256"), vendored)
        if verdict == "unverifiable":
            fallback = _design_anchor(key, moves, row.get("taken_at_head"),
                                      anchors)
            if fallback is not None:
                verdict, detail = fallback
        if verdict == "intact":
            # Neither anchor reaches the parent repository's inputs, so an
            # intact row is not finished until those are measured too.
            route, route_detail = _route_anchor(key, routes, row.get("taken_at_head"))
            if route != "intact":
                verdict, detail = route, f"{route_detail} (its own anchor held: {detail})"
            else:
                detail = f"{detail}, {route_detail}"
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
            {"head": head, "sources_tree": sources,
             "config_tree": anchors["config_tree"], "tree": str(root), "rows": result,
             "intact": not bad, "not_intact": bad}, indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json.resolve()}")
    return 1 if bad else 0


if __name__ == "__main__":
    raise SystemExit(main())
