#!/usr/bin/env python3
"""Which SHAs this record cites would stop resolving if a branch were deleted?

check_shas.py answers "does every cited object resolve *now*". That is a
weaker question than it looks, because #1862 was squash-merged: the single
commit on main is a new object, and no commit from the branch is an
ancestor of it. The seal trees this record's condition-3 rows are taken in
are branch-side commits. They resolve today only because the merged
branch's ref still exists, and deleting a merged branch is routine.

So ask the sharper question: of the objects this record names, which are
unreachable from main, and is one ref enough to hold all of them?

  python3 sha_exposure.py [--root <record dir>] [--repo <git dir>] \
      [--from main] [--ref <branch or tag that should cover them>]
"""
from __future__ import annotations

import argparse
import re
import subprocess
from pathlib import Path

HERE = Path(__file__).resolve().parent
SHA = re.compile(r"\b[0-9a-f]{40}\b")
SUFFIXES = {".json", ".md", ".py", ".log", ".sh"}


def git(repo: Path, *arguments: str) -> subprocess.CompletedProcess:
    return subprocess.run(("git", "-C", str(repo)) + arguments,
                          capture_output=True, text=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", type=Path, default=HERE)
    parser.add_argument("--repo", type=Path, default=HERE)
    parser.add_argument("--from", dest="base", default="origin/main",
                        help="the ref a reader is expected to have")
    parser.add_argument("--ref", action="append", default=[],
                        help="a ref that should cover the exposed objects; repeatable")
    arguments = parser.parse_args()

    found: dict[str, set[str]] = {}
    skipped = 0
    for path in sorted(arguments.root.rglob("*")):
        if path.suffix not in SUFFIXES or not path.is_file():
            continue
        for line in path.read_text(errors="replace").splitlines():
            # check_shas.py's rule, so the two scripts count the same set. A
            # line carrying a digest field names a *toolchain* repository's
            # commit (`commit=...; sha256=...` in a netlist run's tools map),
            # which is not an object of this repository and never was.
            if any(key in line.lower() for key in ("sha256", "digest")):
                skipped += 1
                continue
            for sha in SHA.findall(line):
                found.setdefault(sha, set()).add(
                    str(path.relative_to(arguments.root)))

    exposed, missing, kinds = [], [], {}
    for sha in sorted(found):
        kind = git(arguments.repo, "cat-file", "-t", sha).stdout.strip()
        kinds[sha] = kind or "ABSENT"
        if not kind:
            missing.append(sha)
            continue
        if kind != "commit":
            # A tree is held by whatever commit holds it; the commits below
            # are what a ref keeps alive, so judge reachability on those.
            continue
        if git(arguments.repo, "merge-base", "--is-ancestor",
               sha, arguments.base).returncode != 0:
            exposed.append(sha)

    print(f"{skipped} lines naming a toolchain repository's commit were not checked")
    print(f"{len(found)} distinct objects cited; base {arguments.base} "
          f"= {git(arguments.repo, 'rev-parse', arguments.base).stdout.strip()[:12]}")
    for sha in missing:
        print(f"ABSENT   {sha}  cited in {', '.join(sorted(found[sha]))[:90]}")
    print(f"\n{len(exposed)} cited commits are NOT ancestors of {arguments.base}:")
    for sha in exposed:
        subject = git(arguments.repo, "log", "-1", "--format=%s", sha).stdout.strip()
        print(f"  {sha}  {subject[:70]}")
        for name in sorted(found[sha])[:3]:
            print(f"{'':44}cited in {name}")

    covered = True
    for ref in arguments.ref:
        resolved = git(arguments.repo, "rev-parse", ref).stdout.strip()
        if not resolved:
            print(f"\n{ref}: does not resolve, so it covers nothing")
            covered = False
            continue
        uncovered = [sha for sha in exposed
                     if git(arguments.repo, "merge-base", "--is-ancestor",
                            sha, ref).returncode != 0]
        print(f"\n{ref} ({resolved[:12]}) covers "
              f"{len(exposed) - len(uncovered)} of {len(exposed)}")
        for sha in uncovered:
            print(f"  NOT covered: {sha}")
        covered = covered and not uncovered
    if arguments.ref:
        print("\none ref is enough to hold every exposed object"
              if covered else "\nthese refs do not hold every exposed object")
    return 1 if missing else 0


if __name__ == "__main__":
    raise SystemExit(main())
