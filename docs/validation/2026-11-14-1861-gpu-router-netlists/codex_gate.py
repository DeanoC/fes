#!/usr/bin/env python3
"""Read the Codex review gate at one exact head, on all three channels.

A clean receipt is not a pass on its own, and neither is an anchored-comment
count. The three things that have to hold together:

  1. a codex-review-state:v1 marker whose code_review.commit_sha IS this head
     and whose status is "completed" -- an older marker says nothing about it;
  2. no review comment *raised* at this head, which means
     original_commit_id == head. NOT commit_id: GitHub advances commit_id on a
     still-applicable comment as the diff moves, so twelve answered findings
     from five earlier heads all report commit_id == head. A naive count fails
     the gate on its own history;
  3. every finding thread answered -- each top-level review comment has at
     least one reply beneath it.

The marker's own has_unresolved_findings boolean is reported but not gated on:
it tracks GitHub thread *resolution*, which is the reviewer's to set, not
whether the author replied.

  python3 codex_gate.py --repo DeanoC/fes --pr 1862 --head <sha>
"""
from __future__ import annotations
import argparse, json, subprocess, sys


def api(path: str):
    """Every row as one JSON object per line.

    An earlier version asked gh for raw pages and split the concatenated
    arrays by counting brackets. That counts brackets inside string literals
    too, so it broke on the first comment body containing one. Letting gh's
    --jq emit JSON Lines removes the parsing entirely.
    """
    out = subprocess.run(("gh", "api", path, "--paginate", "--jq", ".[]"),
                         capture_output=True, text=True)
    if out.returncode != 0:
        raise SystemExit(f"gh api failed for {path}: {out.stderr.strip()[:200]}")
    return [json.loads(line) for line in out.stdout.splitlines() if line.strip()]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--pr", required=True)
    parser.add_argument("--head", required=True)
    arguments = parser.parse_args()
    head = arguments.head

    issue = api(f"repos/{arguments.repo}/issues/{arguments.pr}/comments?per_page=100")
    markers = [c for c in issue if "codex-review-state:v1" in (c.get("body") or "")]
    marker, state = None, None
    for c in markers:
        body = c["body"]
        start = body.index("codex-review-state:v1") + len("codex-review-state:v1")
        blob = body[start:].strip()
        blob = blob[:blob.rindex("}") + 1] if "}" in blob else blob
        try:
            parsed = json.loads(blob)
        except ValueError:
            continue
        if (parsed.get("code_review") or {}).get("commit_sha") == head:
            marker, state = c, parsed

    review = api(f"repos/{arguments.repo}/pulls/{arguments.pr}/comments?per_page=100")
    raised_here = [c for c in review if c.get("original_commit_id") == head]
    tops = [c for c in review if not c.get("in_reply_to_id")]
    replies = {c.get("in_reply_to_id") for c in review if c.get("in_reply_to_id")}
    unanswered = [c for c in tops if c["id"] not in replies]

    print(f"head {head}")
    print(f"  1. marker at this head          : "
          f"{'yes' if marker else 'NO (' + str(len(markers)) + ' marker(s), none for this head)'}")
    if state:
        cr = state.get("code_review") or {}
        print(f"     status                      : {cr.get('status')}")
        print(f"     has_any_findings            : {cr.get('has_any_findings')}"
              f"   (gated: resolution-independent, measured on #1829)")
        print(f"     has_unresolved_findings     : {cr.get('has_unresolved_findings')}"
              f"   (reported, not gated: reviewer-owned resolution state)")
    print(f"  2. comments RAISED at this head : {len(raised_here)}"
          + ("" if raised_here else "   (none)"))
    for c in raised_here:
        print(f"       {c['id']} {c['user']['login']} {c.get('path')}")
    print(f"  3. unanswered finding threads   : {len(unanswered)}"
          + ("" if unanswered else "   (none)"))
    for c in unanswered:
        print(f"       {c['id']} {c['user']['login']} {c.get('path')}")
    print(f"  context: {len(review)} review comments total, "
          f"{len([c for c in review if c.get('commit_id') == head])} report commit_id == head "
          f"(carried forward, not raised here)")
    cr = (state or {}).get("code_review", {})
    ok = (bool(marker) and cr.get("status") == "completed"
          and cr.get("has_any_findings") is False
          and not raised_here and not unanswered)
    print()
    print("GATE PASSES" if ok else "GATE DOES NOT PASS YET")
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
