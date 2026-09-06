"""Validate the parent gitlinks and the child's runtime lock."""
from pathlib import Path
import subprocess
import tomllib
import re

COMPONENTS = ("FogCast", "libmister-runtime", "misteross", "mister-packages")

def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()

def validate(root, profile=None):
    root = Path(root)
    revisions = {}
    for name in COMPONENTS:
        path = "sources/" + name
        entries = git(root, "ls-files", "--stage", "--", path).splitlines()
        if len(entries) != 1:
            raise ValueError(f"{path}: missing or conflicting submodule pin")
        fields = entries[0].split()
        if fields[0] != "160000" or fields[2] != "0":
            raise ValueError(f"{path}: expected a submodule pin")
        child = root / path
        if not (child / ".git").exists():
            raise ValueError(f"{path}: run git submodule update --init --recursive")
        if git(child, "rev-parse", "HEAD") != fields[1]:
            raise ValueError(f"{path}: HEAD differs from parent pin")
        if git(child, "status", "--porcelain", "--untracked-files=all"):
            raise ValueError(f"{path}: source checkout is dirty")
        revisions[name] = fields[1]
    for name, revision in (profile or {}).get("sources", {}).items():
        if name not in revisions or not re.fullmatch(r"[0-9a-f]{40}", str(revision)):
            raise ValueError(f"invalid profile source revision: {name}={revision}")
        child = root / "sources" / name
        exists = subprocess.run(["git", "-C", str(child), "cat-file", "-e", revision + "^{commit}"],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if exists.returncode:
            raise ValueError(f"{name}: profile revision {revision} is unavailable; fetch component history")
        revisions[name] = revision
    lock = tomllib.loads(git(root / "sources/FogCast", "show",
                           revisions["FogCast"] + ":build/native-runtime.inputs.lock.toml"))
    if lock["mister_runtime"]["commit"] != revisions["libmister-runtime"]:
        raise ValueError("FogCast runtime lock differs from parent runtime pin")
    return revisions
