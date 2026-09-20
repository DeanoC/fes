"""Read-only source selection and remote-main observations; no fetch or checkout."""
import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess

from inputs import COMPONENTS
from module_sources import describe


def git(root, *args, timeout=10):
    env = dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_TERMINAL_PROMPT="0", GIT_NO_LAZY_FETCH="1")
    return subprocess.run(["git", "-C", str(root), *args], text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          timeout=timeout, env=env)


def value(root, *args):
    result = git(root, *args)
    return result.stdout.strip() if result.returncode == 0 else None


def relation(root, selected, remote):
    if not selected or not remote:
        return "unknown"
    if selected == remote:
        return "equal"
    for revision in (selected, remote):
        if git(root, "cat-file", "-e", revision + "^{commit}").returncode:
            return "different-history-unavailable"
    forward = git(root, "merge-base", "--is-ancestor", selected, remote).returncode
    backward = git(root, "merge-base", "--is-ancestor", remote, selected).returncode
    if forward not in (0, 1) or backward not in (0, 1):
        return "different-history-unavailable"
    if forward == 0:
        return "behind"
    if backward == 0:
        return "ahead"
    # A shallow boundary cannot establish divergence.
    if value(root, "rev-parse", "--is-shallow-repository") == "true":
        return "different-history-unavailable"
    return "diverged"


def pin(root, path, committed=False):
    args = ("ls-tree", "HEAD", "--", path) if committed else ("ls-files", "--stage", "--", path)
    raw = value(root, *args)
    rows = raw.splitlines() if raw else []
    if len(rows) != 1:
        return None
    fields = rows[0].split()
    if fields[0] != "160000":
        return None
    if committed:
        return fields[2]
    return fields[1] if fields[2] == "0" else None


def is_module(root, name):
    if name == "FES":
        return False
    path = "sources/" + name
    committed = value(root, "ls-tree", "HEAD", "--", path)
    if committed and committed.split()[0] == "040000":
        return True
    entries = value(root, "ls-files", "--stage", "--", path)
    return bool(entries) and not (len(entries.splitlines()) == 1 and entries.split()[0] == "160000")


def observe(root, name, offline, timeout, parent_observation=None):
    parent = name == "FES"
    path = root if parent else root / "sources" / name
    module = describe(root, name, require_clean=False) if is_module(root, name) else None
    initialized = parent or module is not None or (path / ".git").exists()
    selected = module["commit"] if module else value(root, "rev-parse", "HEAD") if parent else pin(root, "sources/" + name)
    repository_root = root if module else path
    head = value(repository_root, "rev-parse", "HEAD") if initialized else None
    status_args = ("--", module["path"]) if module else ()
    status = git(repository_root, "status", "--porcelain=v1", "-z", "--untracked-files=all", *status_args) if initialized else None
    raw_status = status.stdout if status is not None and status.returncode == 0 else None
    # Keep porcelain records losslessly, including rename source records.
    changes = raw_status.split("\0") if raw_status else []
    changes = [entry for entry in changes if entry]
    row = {
        "name": name,
        "source_kind": "module" if module else "repository" if parent else "gitlink",
        "module_path": module["path"] if module else None,
        "module_tree": module["tree"] if module else None,
        "remote_owner": "FES" if parent or module else name,
        "selected": selected,
        "committed_selection": selected if parent or module else pin(root, "sources/" + name, True),
        "checkout_head": head,
        "checkout_state": "uninitialized" if not initialized else (
            "unavailable" if head is None or raw_status is None else "dirty" if changes else "clean"),
        "checkout_matches_selection": head == selected if head and selected else None,
        "branch": value(repository_root, "symbolic-ref", "--quiet", "--short", "HEAD") if initialized else None,
        "changes": changes,
        "remote_ref": "refs/heads/main",
        "remote_head": None,
        "remote_state": "offline" if offline else "unavailable",
        "observed_at": None,
        "relation": "unknown",
    }
    if module and parent_observation is not None:
        for key in ("remote_ref", "remote_head", "remote_state", "observed_at", "relation"):
            row[key] = parent_observation[key]
        return row
    if offline:
        return row
    remote = "origin" if initialized else value(root, "config", "-f", ".gitmodules", "--get", f"submodule.sources/{name}.url")
    if not remote:
        return row
    try:
        result = git(repository_root if initialized else root, "ls-remote", "--exit-code", remote,
                     row["remote_ref"], timeout=timeout)
        if result.returncode == 0:
            rows = [line.split() for line in result.stdout.splitlines()]
            matches = [fields[0] for fields in rows if len(fields) == 2 and fields[1] == row["remote_ref"]]
            if len(matches) == 1:
                row["remote_head"] = matches[0]
                row["remote_state"] = "observed"
                row["observed_at"] = datetime.now(timezone.utc).isoformat()
                row["relation"] = relation(repository_root if initialized else root, selected, matches[0])
        elif result.returncode == 2:
            row["remote_state"] = "missing-ref"
    except subprocess.TimeoutExpired:
        row["remote_state"] = "timeout"
    return row


def report(root, offline=False, timeout=10):
    root = Path(root).resolve()
    if value(root, "rev-parse", "--show-toplevel") != str(root):
        raise ValueError("--root must be the FES Git worktree root")
    with ThreadPoolExecutor(max_workers=5) as executor:
        parent = executor.submit(observe, root, "FES", offline, timeout)
        children = list(executor.map(
            lambda name: observe(root, name, offline, timeout,
                                 parent.result() if is_module(root, name) else None), COMPONENTS))
        rows = [parent.result(), *children]
    return {"schema_version": 1, "root": str(root), "sources": rows,
            "evidence": "Source observations only; build, CI, hardware and deployed state are not assessed."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--offline", action="store_true", help="do not contact remotes; freshness remains unknown")
    parser.add_argument("--json", action="store_true", help="emit full source identities and change records")
    parser.add_argument("--timeout", type=float, default=10, help="per-remote timeout in seconds (default: 10)")
    args = parser.parse_args()
    if not 0 < args.timeout <= 120:
        parser.error("--timeout must be greater than zero and at most 120 seconds")
    try:
        result = report(args.root, args.offline, args.timeout)
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        parser.exit(2, f"source status failed: {type(exc).__name__}\n")
    if args.json:
        print(json.dumps(result, indent=2))
    else:
        for row in result["sources"]:
            selected = (row["selected"] or "missing")[:12]
            remote = (row["remote_head"] or row["remote_state"])[:12]
            print(f'{row["name"]:18} selected={selected:12} main={remote:12} {row["relation"]}')
            print(f'  checkout={row["checkout_state"]} branch={row["branch"] or "detached/unavailable"}'
                  f' head={row["checkout_head"] or "unavailable"} matches-selection={row["checkout_matches_selection"]}')
            if row["source_kind"] == "module":
                print(f'  module={row["module_path"]} tree={row["module_tree"]} remote=FES main (shared repository)')
            if row["committed_selection"] != row["selected"]:
                print(f'  committed-selection={row["committed_selection"] or "missing"} (index selection differs)')
            if row["observed_at"]:
                print(f'  observed={row["observed_at"]}')
            for change in row["changes"]:
                print(f'  change={json.dumps(change)}')
        print(result["evidence"])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
