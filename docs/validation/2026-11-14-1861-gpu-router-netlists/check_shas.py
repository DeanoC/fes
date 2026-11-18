#!/usr/bin/env python3
"""Does every full-length SHA named in this record resolve in the repository?

Written after Codex caught a hand-typed revision in reports-cond3/cond3.json
(#1862, P1): the tail had been invented from a short form, and nothing consumed
the value, so it survived into the record. One typo is a reason to check the
rest rather than assume it was the only one.

Checks 40-hex strings in this directory's JSON, Markdown and Python files. A
BUILD_ID is 32 hex and is not a git object, so it is left alone.

  python3 check_shas.py [--root <record dir>] [--repo <git dir>]
"""
from __future__ import annotations
import argparse, re, subprocess, sys
from pathlib import Path

SHA40 = re.compile(r"\b[0-9a-f]{40}\b")
SKIP_KEYS = ("sha256", "digest")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parent)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parents[3])
    arguments = parser.parse_args()
    found: dict[str, set[str]] = {}
    for path in sorted(arguments.root.rglob("*")):
        if not path.is_file() or path.suffix not in (".json", ".md", ".py", ".log", ".sh"):
            continue
        try:
            text = path.read_text(errors="replace")
        except OSError:
            continue
        for line in text.splitlines():
            # A sha256 digest is 64 hex and will not match; a 40-hex value on a
            # line that names a digest field is still skipped, to be safe.
            if any(key in line.lower() for key in SKIP_KEYS):
                continue
            for sha in SHA40.findall(line):
                found.setdefault(sha, set()).add(str(path.relative_to(arguments.root)))
    bad, kinds = {}, {}
    for sha in sorted(found):
        out = subprocess.run(("git", "-C", str(arguments.repo), "cat-file", "-t", sha),
                             capture_output=True, text=True)
        kind = out.stdout.strip()
        # Not only commits. The seal check anchors on the `sources/misteross`
        # tree object, deliberately, because a commit SHA moves whenever this
        # record's own documents are committed. So accept any object this
        # repository holds and print which kind it is; the thing being audited
        # is that no 40-hex value here was typed rather than read.
        if out.returncode != 0 or kind not in ("commit", "tree", "blob", "tag"):
            bad[sha] = found[sha]
        kinds[sha] = kind or "unresolved"
    print(f"{len(found)} distinct 40-hex value(s) referenced in {arguments.root.name}")
    for sha in sorted(found):
        print(f"  {sha} {'DOES NOT RESOLVE' if sha in bad else kinds[sha]}")
        if sha in bad:
            for where in sorted(found[sha]):
                print(f"      named in {where}")
    print()
    print(f"every referenced object resolves ({sum(1 for k in kinds.values() if k == 'commit')} "
          f"commit, {sum(1 for k in kinds.values() if k == 'tree')} tree)" if not bad
          else f"{len(bad)} referenced object(s) do not resolve")
    return 1 if bad else 0


if __name__ == "__main__":
    raise SystemExit(main())
