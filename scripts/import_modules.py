"""Rehearse a history-preserving first-party module import in a disposable clone.

Default operation is read-only. --execute stages the import, retains original
commits under refs/imports/, and records provenance. It never commits or pushes.
The integrator must make the final import commit with every required parent in
its result; refs alone are local retention, not published merge ancestry.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess


COMPONENTS = ("FogCast", "libmister-runtime", "misteross", "mister-packages")
TOOL_CHECKOUT = Path(__file__).resolve().parents[1]


def git(root, *args, check=True):
    result = subprocess.run(["git", "-C", str(root), *args], text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            env=dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_NO_LAZY_FETCH="1",
                                     GIT_TERMINAL_PROMPT="0"))
    if check and result.returncode:
        raise ValueError(f"Git import failed ({' '.join(args)}): {result.stderr.strip()}")
    return result


def value(root, *args):
    return git(root, *args).stdout.strip()


def safe_path(root, relative):
    path = root
    for part in Path(relative).parts:
        path /= part
        if path.is_symlink():
            raise ValueError(f"import path must not traverse a symlink: {path}")
    return path


def source_rows(repository, revision):
    rows = []
    raw = git(repository, "ls-tree", "-r", "-z", revision).stdout
    for record in filter(None, raw.split("\0")):
        metadata, path = record.split("\t", 1)
        mode, kind, digest = metadata.split()
        parts = Path(path).parts
        if (mode not in ("100644", "100755", "120000") or kind != "blob" or
                Path(path).is_absolute() or any(part in (".", "..", ".git") for part in parts)):
            raise ValueError(f"source tree contains unsupported import entry: {path}")
        rows.append(path)
    if not rows:
        raise ValueError("cannot import an empty source tree")
    return rows


def module_sections(root):
    """Find exact module sections while retaining unrelated submodule entries."""
    path = safe_path(root, ".gitmodules")
    if not path.is_file():
        raise ValueError("destination lacks tracked .gitmodules")
    if git(root, "ls-files", "--error-unmatch", ".gitmodules", check=False).returncode:
        raise ValueError("destination .gitmodules is not tracked")
    result = git(root, "config", "--file", str(path), "--get-regexp", r"^submodule\..*\.path$", check=False)
    if result.returncode not in (0, 1):
        raise ValueError("destination .gitmodules is malformed")
    sections = {}
    for line in result.stdout.splitlines():
        key, declared = line.split(None, 1)
        if declared in {"sources/" + name for name in COMPONENTS}:
            if declared in sections:
                raise ValueError(f"duplicate .gitmodules path: {declared}")
            section = key[:-len(".path")]
            # Refuse surprising extra keys in sections we are about to remove.
            keys = value(root, "config", "--file", str(path), "--name-only", "--get-regexp",
                         "^" + re.escape(section) + r"\.").splitlines()
            if any(key[len(section) + 1:] not in ("path", "url", "branch", "update", "ignore", "shallow", "fetchrecursesubmodules") for key in keys):
                raise ValueError(f"unexpected .gitmodules keys in {section}")
            sections[declared] = section
    if set(sections) != {"sources/" + name for name in COMPONENTS}:
        raise ValueError(".gitmodules must describe all four imported gitlinks exactly once")
    return sections


def inspect(destination, sources, *, allow_reselection=False):
    """Read-only plan. Sources map names to {repository: local path, commit: SHA}."""
    root = Path(destination).resolve()
    protected = {TOOL_CHECKOUT.resolve()}
    worktrees = git(TOOL_CHECKOUT, "worktree", "list", "--porcelain", check=False).stdout
    if worktrees.startswith("worktree "):
        protected.add(Path(worktrees.splitlines()[0][len("worktree "):]).resolve())
    if root in protected:
        raise ValueError("import requires a separate disposable destination, not the canonical or tool checkout")
    if value(root, "rev-parse", "--show-toplevel") != str(root):
        raise ValueError("destination must be a disposable FES Git checkout root")
    if set(sources) != set(COMPONENTS):
        raise ValueError("provide explicit sources for exactly the four first-party components")
    if value(root, "status", "--porcelain=v1", "-z", "--untracked-files=all"):
        raise ValueError("destination is dirty; use a clean disposable FES checkpoint")
    base = value(root, "rev-parse", "HEAD")
    sections = module_sections(root)
    manifest = safe_path(root, "config/source-imports.toml")
    if manifest.exists():
        raise ValueError("destination already has config/source-imports.toml")
    modules = []
    for name in COMPONENTS:
        relative = "sources/" + name
        child = safe_path(root, relative)
        entries = value(root, "ls-files", "--stage", "--", relative).splitlines()
        if len(entries) != 1:
            raise ValueError(f"{relative}: expected one selected gitlink")
        fields = entries[0].split()
        if fields[0] != "160000" or fields[2] != "0":
            raise ValueError(f"{relative}: expected one selected gitlink")
        prior_gitlink = fields[1]
        revision = sources[name]["commit"]
        if not isinstance(revision, str) or not re.fullmatch(r"[0-9a-f]{40}", revision):
            raise ValueError(f"{name}: explicit commit must be a full lowercase Git SHA")
        if revision != prior_gitlink and not allow_reselection:
            raise ValueError(f"{name}: explicit commit does not match selected pin; use --allow-reselection explicitly")
        repository = Path(sources[name]["repository"]).resolve()
        if not repository.is_dir() or repository == root:
            raise ValueError(f"{name}: provide a separate local component repository")
        if value(repository, "rev-parse", "--show-toplevel") != str(repository):
            raise ValueError(f"{name}: source must be a component Git checkout root")
        if git(repository, "cat-file", "-e", revision + "^{commit}", check=False).returncode:
            raise ValueError(f"{name}: selected commit is unavailable in explicit source")
        urls = value(repository, "remote", "get-url", "--all", "origin").splitlines()
        if len(urls) != 1 or not urls[0]:
            raise ValueError(f"{name}: source must record exactly one original origin URL")
        selected_url = value(root, "config", "--file", str(root / ".gitmodules"),
                             "--get", sections[relative] + ".url")
        if not selected_url:
            raise ValueError(f"{relative}: .gitmodules lacks original source URL")
        paths = source_rows(repository, revision)
        initialized = (child / ".git").exists()
        if initialized:
            if (Path(value(child, "rev-parse", "--show-toplevel")).resolve() != child or
                    value(child, "rev-parse", "HEAD") != prior_gitlink):
                raise ValueError(f"{relative}: existing checkout does not match selected pin")
            # Include ignored files: moving this checkout is not permission to
            # hide unrelated build outputs or files in a backup directory.
            if (value(child, "status", "--porcelain=v1", "-z", "--untracked-files=all") or
                    value(child, "ls-files", "--others", "--ignored", "--exclude-standard")):
                raise ValueError(f"{relative}: existing child checkout is dirty or has ignored files")
        elif child.exists() and (not child.is_dir() or any(child.iterdir())):
            raise ValueError(f"{relative}: uninitialized checkout contains unrelated files")
        backup = safe_path(root, "out/module-import-backups/" + name)
        if backup.exists():
            raise ValueError(f"backup already exists; inspect {backup}")
        if git(root, "check-ignore", "-q", "--", str(backup.relative_to(root)), check=False).returncode:
            raise ValueError("module import backups must be under ignored out/")
        reference = "refs/imports/" + name
        previous = git(root, "rev-parse", "--verify", reference, check=False)
        previous = previous.stdout.strip() if previous.returncode == 0 else None
        if previous is not None and previous != revision:
            raise ValueError(f"{reference} already retains another commit")
        prior_ref, existing_prior_ref, extra_parent = None, None, None
        if revision != prior_gitlink:
            if git(repository, "cat-file", "-e", prior_gitlink + "^{commit}", check=False).returncode:
                raise ValueError(f"{name}: prior gitlink history is unavailable in explicit source")
            ancestry = git(repository, "merge-base", "--is-ancestor", prior_gitlink, revision, check=False)
            if ancestry.returncode not in (0, 1):
                raise ValueError(f"{name}: cannot establish prior gitlink ancestry")
            if ancestry.returncode == 1:
                extra_parent = prior_gitlink
            prior_ref = "refs/imports/prior/" + name
            previous_prior = git(root, "rev-parse", "--verify", prior_ref, check=False)
            existing_prior_ref = previous_prior.stdout.strip() if previous_prior.returncode == 0 else None
            if existing_prior_ref is not None and existing_prior_ref != prior_gitlink:
                raise ValueError(f"{prior_ref} already retains another commit")
        modules.append({"name": name, "path": relative, "repository": str(repository),
                        "url": urls[0], "gitlink_url": selected_url, "commit": revision,
                        "prior_gitlink": prior_gitlink, "imported_commit": revision,
                        "prior_ref": prior_ref, "existing_prior_ref": existing_prior_ref,
                        "extra_parent": extra_parent,
                        "tree": value(repository, "rev-parse", revision + "^{tree}"),
                        "ref": reference, "existing_ref": previous,
                        "backup": str(backup) if initialized else None,
                        "section": sections[relative], "files": paths, "had_directory": child.is_dir()})
    return {"format": 1, "destination": str(root), "base_commit": base,
            "required_parents": list(dict.fromkeys([base, *(row["commit"] for row in modules),
                                                    *(row["extra_parent"] for row in modules if row["extra_parent"])])),
            "allow_reselection": allow_reselection,
            "modules": modules, "executed": False}


def provenance(plan):
    # JSON basic strings are valid TOML strings for these printable Git fields.
    quote = json.dumps
    lines = ["format = 1", "fes_base_commit = " + quote(plan["base_commit"]),
             "required_parents = " + quote(plan["required_parents"]), ""]
    for module in plan["modules"]:
        lines += ["[imports." + quote(module["name"]) + "]",
                  "path = " + quote(module["path"]), "repository = " + quote(module["url"]),
                  "gitlink_repository = " + quote(module["gitlink_url"]),
                  "prior_gitlink = " + quote(module["prior_gitlink"]),
                  "imported_commit = " + quote(module["imported_commit"]), "tree = " + quote(module["tree"]),
                  "history_ref = " + quote(module["ref"])]
        if module["prior_ref"]:
            lines.append("prior_history_ref = " + quote(module["prior_ref"]))
        lines.append("")
    return "\n".join(lines)


def _remove_created_module(root, module):
    """Remove only the enumerated imported files, never recursive user content."""
    child = root / module["path"]
    directories = {child}
    for relative in module["files"]:
        path = child / relative
        if path.is_file() or path.is_symlink():
            path.unlink()
        parent = path.parent
        while child in (parent, *parent.parents):
            directories.add(parent)
            if parent == child:
                break
            parent = parent.parent
    for directory in sorted(directories, key=lambda p: len(p.parts), reverse=True):
        if directory.exists():
            directory.rmdir()  # Deliberately refuses unrelated or concurrent files.


def prepare(destination, sources, *, execute=False, allow_reselection=False):
    plan = inspect(destination, sources, allow_reselection=allow_reselection)
    if not execute:
        return plan
    root = Path(plan["destination"])
    gitmodules = root / ".gitmodules"
    original_modules = gitmodules.read_bytes()
    moved, imported, fetched = [], [], []
    manifest = root / "config/source-imports.toml"
    try:
        for module in plan["modules"]:
            selections = [(module["commit"], module["ref"], module["existing_ref"])]
            if module["prior_ref"]:
                selections.append((module["prior_gitlink"], module["prior_ref"], module["existing_prior_ref"]))
            for revision, reference, previous in selections:
                git(root, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", module["repository"],
                    revision + ":" + reference)
                if previous is None:
                    fetched.append(reference)
        for module in plan["modules"]:
            child = root / module["path"]
            if module["backup"]:
                backup = Path(module["backup"])
                backup.parent.mkdir(parents=True, exist_ok=True)
                child.rename(backup)
                moved.append(module)
            elif child.exists():
                child.rmdir()
            git(root, "update-index", "--force-remove", "--", module["path"])
            imported.append(module)
            git(root, "read-tree", "--prefix=" + module["path"] + "/", "-u", module["ref"])
            git(root, "config", "--file", str(gitmodules), "--remove-section", module["section"])
        manifest.parent.mkdir(parents=True, exist_ok=True)
        manifest.write_text(provenance(plan))
        git(root, "add", "--", ".gitmodules", "config/source-imports.toml")
    except BaseException as exc:
        rollback_errors = []
        for module in reversed(imported):
            try:
                _remove_created_module(root, module)
            except OSError as error:
                rollback_errors.append(str(error))
        for module in imported:
            if not module["backup"] and module["had_directory"]:
                (root / module["path"]).mkdir(parents=True, exist_ok=True)
        for module in reversed(moved):
            try:
                Path(module["backup"]).rename(root / module["path"])
            except OSError as error:
                rollback_errors.append(str(error))
        gitmodules.write_bytes(original_modules)
        if manifest.exists():
            manifest.unlink()
        git(root, "read-tree", plan["base_commit"])
        for reference in fetched:
            git(root, "update-ref", "-d", reference)
        if rollback_errors:
            raise ValueError("import failed; rollback preserved unexpected files; inspect destination and backups: " + "; ".join(rollback_errors)) from exc
        raise
    plan["executed"] = True
    plan["staged_tree"] = value(root, "write-tree")
    plan["next_step"] = "Review staged changes, then create an import commit with all required_parents; no commit or push was performed. Backups retain original checkout bytes and must not be used as build sources."
    return plan


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", required=True, type=Path)
    parser.add_argument("--source", action="append", default=[], metavar="NAME=/LOCAL/REPOSITORY@COMMIT")
    parser.add_argument("--execute", action="store_true", help="stage import in the explicit disposable destination")
    parser.add_argument("--allow-reselection", action="store_true", help="explicitly import reviewed local commits different from existing gitlinks")
    args = parser.parse_args()
    sources = {}
    for entry in args.source:
        try:
            name, selected = entry.split("=", 1)
            repository, revision = selected.rsplit("@", 1)
        except ValueError:
            parser.error("source must be NAME=/LOCAL/REPOSITORY@FULL_COMMIT")
        if name in sources or not Path(repository).is_absolute():
            parser.error("sources require unique names and absolute local repository paths")
        sources[name] = {"repository": repository, "commit": revision}
    try:
        result = prepare(args.destination, sources, execute=args.execute, allow_reselection=args.allow_reselection)
    except (ValueError, OSError, subprocess.SubprocessError) as exc:
        parser.exit(2, f"module import failed: {exc}\n")
    print(json.dumps(result, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
