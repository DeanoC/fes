"""Committed component identities and disposable build snapshots.

Gitlinks retain index selection. Tracked modules select an actual FES commit,
never a synthetic child commit. A module snapshot is a full FES clone: callers
must mount its repository root when Git metadata is needed inside a container.
This module neither changes source selections nor restores generated overlays.
"""
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


COMPONENTS = ("FogCast", "libmister-runtime", "misteross", "mister-packages")


def _git(root, *args, check=True):
    result = subprocess.run(
        ["git", "-C", str(root), *args], stdout=subprocess.PIPE,
        stderr=subprocess.PIPE, text=True,
        env=dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_NO_LAZY_FETCH="1",
                 GIT_TERMINAL_PROMPT="0"))
    if check and result.returncode:
        raise ValueError(f"Git source inspection failed: {' '.join(args)}: {result.stderr.strip()}")
    return result


def _value(root, *args):
    return _git(root, *args).stdout.strip()


def _root(root):
    root = Path(root).resolve()
    if Path(_value(root, "rev-parse", "--show-toplevel")).resolve() != root:
        raise ValueError("source root must be the FES Git checkout root")
    return root


def _commit(root, revision):
    if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("source revision must be a full lowercase Git commit")
    if _git(root, "cat-file", "-e", revision + "^{commit}", check=False).returncode:
        raise ValueError(f"source revision {revision} is unavailable; fetch its repository history")
    return revision


def _origin(root):
    result = _git(root, "remote", "get-url", "--all", "origin", check=False)
    if result.returncode:
        return None
    urls = result.stdout.splitlines()
    if len(urls) != 1 or not urls[0]:
        raise ValueError("source repository must have at most one origin URL")
    return urls[0]


def normalize_known_origin(origin):
    """Canonicalize transport spellings of known first-party GitHub origins.

    Unknown URLs remain themselves: a fork or local clone must never acquire
    first-party provenance merely because its directory has a familiar name.
    Source checkout configuration is not modified; snapshots use the result.
    """
    if origin is None:
        return None
    match = re.fullmatch(
        r"(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)"
        r"DeanoC/([A-Za-z0-9_.-]+?)(?:\.git)?", origin, re.IGNORECASE)
    repositories = {name.lower(): name for name in ("fes", *COMPONENTS)}
    if match and match[1].lower() in repositories:
        return "https://github.com/DeanoC/" + repositories[match[1].lower()] + ".git"
    return origin


def _without_symlinks(root, path):
    current = root
    for part in path.relative_to(root).parts:
        current /= part
        if current.is_symlink():
            raise ValueError(f"source or snapshot path must not traverse a symlink: {current}")


def describe(root, name, revision=None, *, require_clean=True):
    """Return source provenance, selecting index gitlinks or committed modules.

    ``revision`` is a child commit for a gitlink, and a FES commit for a module.
    Cleanliness refers to the current component checkout, even when selecting a
    historical commit. Unrelated FES edits do not dirty a tracked module. When
    ``require_clean=False``, ``dirty`` reports module-local changes; selection
    still refers only to committed bytes. An unborn legacy parent has no
    ``root_commit``; its initialized gitlinks can still be inspected.
    """
    root = _root(root)
    if name not in COMPONENTS:
        raise ValueError(f"unknown source component: {name}")
    relative = "sources/" + name
    child = root / relative
    _without_symlinks(root, child)
    rows = _value(root, "ls-files", "--stage", "-z", "--", relative).split("\0")
    entries = [row.split("\t", 1) for row in rows if row]
    committed_entry = _git(root, "ls-tree", "HEAD", "--", relative, check=False).stdout
    committed_module = committed_entry.startswith("040000 tree ")
    if (not entries and not committed_module) or any(len(row) != 2 or row[0].split()[2] != "0" for row in entries):
        raise ValueError(f"{relative}: missing or conflicting source selection")
    head = _git(root, "rev-parse", "--verify", "HEAD", check=False)
    root_commit = head.stdout.strip() if head.returncode == 0 else None
    gitlink = len(entries) == 1 and entries[0][0].split()[0] == "160000"
    if gitlink:
        if entries[0][1] != relative or not (child / ".git").exists():
            raise ValueError(f"{relative}: legacy submodule checkout is incomplete; use the current consolidated FES checkout")
        if Path(_value(child, "rev-parse", "--show-toplevel")).resolve() != child:
            raise ValueError(f"{relative}: expected an independent source checkout")
        selected = entries[0][0].split()[1]
        if _value(child, "rev-parse", "HEAD") != selected:
            raise ValueError(f"{relative}: HEAD differs from parent pin")
        status = _value(child, "status", "--porcelain=v1", "-z", "--untracked-files=all")
        source, module_path = child, "."
        commit = _commit(child, selected if revision is None else revision)
        tree = _value(child, "rev-parse", commit + "^{tree}")
    else:
        if any(not row[1].startswith(relative + "/") for row in entries):
            raise ValueError(f"{relative}: expected a tracked module directory")
        if (child / ".git").exists() or (child.exists() and not child.is_dir()):
            raise ValueError(f"{relative}: expected a tracked module, not a nested repository")
        status = _value(root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", relative)
        source, module_path = root, relative
        commit = _commit(root, root_commit if revision is None else revision)
        tree_result = _git(root, "rev-parse", "--verify", commit + ":" + relative, check=False)
        if tree_result.returncode or _git(root, "cat-file", "-t", tree_result.stdout.strip(), check=False).stdout.strip() != "tree":
            raise ValueError(f"{relative}: selected commit has no tracked module directory")
        tree = tree_result.stdout.strip()
    if require_clean and status:
        raise ValueError(f"{relative}: source checkout is dirty; commit module changes before building")
    return {"kind": "gitlink" if gitlink else "module", "repository": normalize_known_origin(_origin(source)),
            "commit": commit, "path": module_path, "tree": tree,
            "root_commit": root_commit, "dirty": bool(status)}


def materialize(root, name, revision, destination):
    """Clone committed source under ignored out/work and return its module path.

    The caller serializes builds. Existing snapshots must match identity and be
    clean; generated overlays must be explicitly handled by their owning build
    code before reuse. Local clone objects are independent of the source object
    directory; moving a snapshot does not leave .git pointers into the source.
    """
    root = _root(root)
    identity = describe(root, name, revision)
    destination = Path(destination)
    if not destination.is_absolute():
        destination = root / destination
    # Resolve lexical '..' before checking containment, but never follow links.
    destination = Path(os.path.abspath(destination))
    work = root / "out/work"
    if destination == work or work not in destination.parents:
        raise ValueError("source snapshots must be beneath ignored out/work")
    _without_symlinks(root, destination)
    relative = destination.relative_to(root).as_posix()
    if _git(root, "check-ignore", "-q", "--", relative, check=False).returncode:
        raise ValueError("source snapshot destination must be ignored by FES")
    if _value(root, "ls-files", "--", relative):
        raise ValueError("source snapshot destination must not contain tracked FES files")
    source = root / "sources" / name if identity["kind"] == "gitlink" else root
    if not destination.exists():
        destination.parent.mkdir(parents=True, exist_ok=True)
        temporary = Path(tempfile.mkdtemp(prefix=".source-", dir=destination.parent))
        try:
            _git(root, "clone", "--no-hardlinks", "--no-checkout", "--local", str(source), str(temporary))
            if identity["repository"] is None:
                _git(temporary, "remote", "remove", "origin")
            else:
                _git(temporary, "remote", "set-url", "origin", identity["repository"])
            _git(temporary, "checkout", "--detach", identity["commit"])
            temporary.rename(destination)
        finally:
            if temporary.exists():
                shutil.rmtree(temporary)
    if (destination / ".git").is_symlink() or not (destination / ".git").is_dir():
        raise ValueError(f"source snapshot must have independent Git metadata: {destination}")
    if (Path(_value(destination, "rev-parse", "--show-toplevel")).resolve() != destination or
            _value(destination, "rev-parse", "HEAD") != identity["commit"] or
            normalize_known_origin(_origin(destination)) != identity["repository"] or
            _value(destination, "status", "--porcelain=v1", "-z", "--untracked-files=all")):
        raise ValueError(f"source snapshot is changed; inspect {destination} before rebuilding")
    # Older snapshots may retain an equivalent SSH/HTTPS spelling. Normalize
    # once here, after validating source/cleanliness, never in the producer.
    if _origin(destination) != identity["repository"]:
        _git(destination, "remote", "set-url", "origin", identity["repository"])
    module = destination if identity["path"] == "." else destination / identity["path"]
    _without_symlinks(destination, module)
    if not module.is_dir():
        raise ValueError(f"source snapshot lacks module: {module}")
    return module
