"""Read-only GitHub task reconciliation; comments are data, never commands."""
import argparse
from datetime import datetime, timezone, timedelta
import json
from pathlib import Path
import re
import subprocess

STATES = {"ready", "working", "review", "validation", "blocked", "done"}
MARKER = re.compile(r"^FES-TASK-(ACK|UPDATE|REVIEW)[ \t]*\r?$", re.M)
JSON_BLOCK = re.compile(r"\s*```json[ \t]*\r?\n(.*?)\r?\n```[ \t]*(?:\r?\n|$)", re.S)
ACK_FIELDS = ("runner", "vendor", "host", "worktree", "branch", "base")


def records(comments):
    found, errors = [], []
    for comment in comments:
        body = comment.get("body") or ""
        markers = list(MARKER.finditer(body))
        for index, match in enumerate(markers):
            end = markers[index + 1].start() if index + 1 < len(markers) else len(body)
            block = JSON_BLOCK.match(body[match.end():end])
            where = comment.get("html_url") or comment["created_at"]
            prefix = f"unparseable FES-TASK-{match[1]} record ({where})"
            if not block:
                errors.append(prefix + ": marker must be followed directly by a fenced JSON object; correct the comment")
                continue
            try:
                value = json.loads(block[1])
            except ValueError:
                errors.append(prefix + ": invalid JSON; correct the comment")
                continue
            if not isinstance(value, dict):
                errors.append(prefix + ": JSON must be an object; correct the comment")
                continue
            found.append((match[1], value, comment["created_at"]))
    return sorted(found, key=lambda entry: entry[2]), errors


def complete_ack(value):
    return all(isinstance(value.get(field), str) and value[field].strip()
               for field in ACK_FIELDS) and bool(re.fullmatch(r"[0-9a-fA-F]{40}", value["base"]))


def findings(issue, comments, blockers, now, stale_hours):
    labels = {item["name"] for item in issue.get("labels", [])}
    state_labels = {label for label in labels if label.startswith("task:")}
    problems = []
    if len(state_labels) != 1 or not state_labels <= {"task:" + s for s in STATES}:
        problems.append("missing, unknown or conflicting task state")
    state = next(iter(state_labels), "").removeprefix("task:") if len(state_labels) == 1 else ""
    entries, record_errors = records(comments)
    problems.extend(record_errors)
    claims = [entry for entry in entries if entry[0] == "ACK" and complete_ack(entry[1])]
    claim = claims[-1] if claims else None
    runner = claim[1]["runner"] if claim else ""
    if state in {"working", "review", "validation", "done"} and not claim:
        problems.append("missing complete runner acknowledgement")
    open_blockers = [item["number"] for item in blockers if item["state"] == "open"]
    if open_blockers:
        problems.append("blocked by " + ", ".join("#" + str(n) for n in open_blockers))
        if state in {"ready", "working", "review", "done"}:
            problems.append("state conflicts with unmet dependencies")
    if state == "working" and claim:
        # Only ACK/UPDATE comments, not arbitrary edits/reviews, renew a claim.
        progress = [entry for entry in entries if entry[0] in {"ACK", "UPDATE"} and
                    entry[1].get("runner") == runner and entry[2] >= claim[2]]
        last = datetime.fromisoformat(progress[-1][2].replace("Z", "+00:00"))
        if now - last > timedelta(hours=stale_hours):
            problems.append(f"runner update older than {stale_hours:g} hours; inspect, do not reassign")
    current = [entry for entry in entries if claim and entry[2] >= claim[2]]
    updates = [entry for entry in current if entry[0] == "UPDATE" and entry[1].get("runner") == runner]
    pr = next((value.get("pr") for _, value, _ in reversed(updates) if value.get("pr")), "")
    reviewed = any(kind == "REVIEW" and value.get("reviewer") and value["reviewer"] != runner and
                   isinstance(value.get("url"), str) and value["url"].startswith("https://github.com/")
                   and (not pr or value["url"].split("#")[0].rstrip("/") == pr.rstrip("/"))
                   for kind, value, _ in current)
    if state in {"validation", "done"} and not reviewed:
        problems.append("no recorded independent review; inspect actual PR review")
    if state == "review" and not reviewed:
        problems.append("awaiting recorded independent review")
    if state == "done" and issue["state"] == "open":
        problems.append("done label on open issue")
    if issue["state"] == "closed" and state != "done":
        problems.append("closed without done task state/evidence check")
    if "kind:acceptance" in labels:
        if issue["state"] == "open":
            problems.append("real-node acceptance still open")
        else:
            evidence = any(value.get("evidence") for _, value, _ in updates)
            if not evidence:
                problems.append("closed acceptance without recorded evidence; verify before outcome closure")
    return state or "unknown", runner or "unclaimed", problems


def gh_list(path):
    result = subprocess.run(["gh", "api", "--paginate", "--slurp", path],
                            check=True, text=True, capture_output=True, timeout=120)
    pages = json.loads(result.stdout)
    if not isinstance(pages, list) or any(not isinstance(page, list) for page in pages):
        raise ValueError("GitHub API returned an unexpected list shape")
    return [item for page in pages for item in page]


def cell(value):
    # Keep untrusted issue titles/runner text inside one Markdown table cell.
    return str(value).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;").replace("|", "&#124;").replace("\n", " ").replace("\r", " ")


def report(repo, milestone, issues, now, stale_hours):
    lines = ["# FES coordination report", "",
             f"Repository: {repo}; milestone: {milestone}; observed: {now.isoformat()}", "",
             "Read-only snapshot. An issue, recorded review or closed dependency is not hardware acceptance.",
             "No assignments, messages or state changes were made.", "",
             "| Issue | Task state | Runner | Attention / dependencies |",
             "| --- | --- | --- | --- |"]
    count = 0
    ready = []
    for issue in sorted(issues, key=lambda item: item["number"]):
        if "pull_request" in issue:
            continue
        count += 1
        path = f"repos/{repo}/issues/{issue['number']}"
        comments = gh_list(path + "/comments?per_page=100")
        blockers = gh_list(path + "/dependencies/blocked_by?per_page=100")
        state, runner, problems = findings(issue, comments, blockers, now, stale_hours)
        link = f"https://github.com/{repo}/issues/{issue['number']}"
        if state == "ready" and runner == "unclaimed" and issue["state"] == "open" and not problems:
            ready.append(f"[#{issue['number']}]({link})")
        lines.append(f"| [#{issue['number']}]({link}) {cell(issue['title'])} | {cell(state)} | {cell(runner)} | {cell('; '.join(problems) or 'No coordination flags; evidence still needs inspection')} |")
    if not count:
        raise ValueError("No issues found for the requested milestone; no report published")
    lines += ["", "Ready to claim (informational): " + (", ".join(ready) or "none") + ".",
              "", f"{count} task/outcome issues observed.",
              "Working claims are flagged after the configured runner-update interval; a flag never transfers ownership.", ""]
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", default="DeanoC/fes")
    parser.add_argument("--milestone", type=int, default=1)
    parser.add_argument("--stale-hours", type=float, default=24)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repo) or args.milestone < 1 or args.stale_hours <= 0:
        parser.error("invalid repository, milestone or stale interval")
    try:
        issues = gh_list(f"repos/{args.repo}/issues?milestone={args.milestone}&state=all&per_page=100")
        text = report(args.repo, args.milestone, issues, datetime.now(timezone.utc), args.stale_hours)
    except (subprocess.SubprocessError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f"Coordination observation failed; no fresh report published: {error}\n")
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(text)
    else:
        print(text, end="")


if __name__ == "__main__":
    main()
