"""Validate the parent gitlinks and the child's runtime lock."""
from pathlib import Path
import subprocess
import tomllib

COMPONENTS = ("FogCast", "libmister-runtime")

def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()

def validate(root):
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
    lock = tomllib.loads((root / "sources/FogCast/build/native-runtime.inputs.lock.toml").read_text())
    if lock["mister_runtime"]["commit"] != revisions["libmister-runtime"]:
        raise ValueError("FogCast runtime lock differs from parent runtime pin")
    return revisions
