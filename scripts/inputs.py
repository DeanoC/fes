"""Validate parent source selections and derive concrete assembly inputs."""
from pathlib import Path
import subprocess
import tomllib
import re

from module_sources import COMPONENTS, describe


def git(root, *args):
    # Git show paths are repository-relative, whereas callers operate on modules.
    # Preserve the real repository/commit while resolving a module-relative file.
    if len(args) == 2 and args[0] == "show" and ":" in args[1]:
        revision, relative = args[1].split(":", 1)
        prefix = subprocess.check_output(
            ["git", "-C", str(root), "rev-parse", "--show-prefix"], text=True).strip()
        if prefix and not relative.startswith(":"):
            args = ("show", revision + ":" + prefix + relative)
    return subprocess.check_output(["git", "-C", str(root), *args], text=True).strip()


def validate(root, profile=None):
    root = Path(root)
    revisions = {name: describe(root, name)["commit"] for name in COMPONENTS}
    for name, revision in (profile or {}).get("sources", {}).items():
        if name not in revisions or not re.fullmatch(r"[0-9a-f]{40}", str(revision)):
            raise ValueError(f"invalid profile source revision: {name}={revision}")
        revisions[name] = describe(root, name, revision)["commit"]
    return revisions


def selected_runtime_lock(raw, revision):
    """Override the standalone default only in a disposable assembly overlay."""
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("invalid selected runtime revision")
    original = tomllib.loads(raw)
    if not isinstance(original.get("mister_runtime"), dict):
        raise ValueError("native input policy lacks runtime settings")
    section = re.search(r"(?ms)^\[mister_runtime\][ \t]*\n(.*?)(?=^\[|\Z)", raw)
    if section is None:
        raise ValueError("native input policy lacks runtime section")
    body, count = re.subn(r"(?m)^commit[ \t]*=.*$", "commit = '" + revision + "'", section.group(1))
    if count == 0:
        body = "commit = '" + revision + "'\n" + body
    elif count != 1:
        raise ValueError("native input policy has multiple runtime commits")
    generated = raw[:section.start(1)] + body + raw[section.end(1):]
    expected = original.copy()
    expected["mister_runtime"] = dict(original["mister_runtime"], commit=revision)
    if tomllib.loads(generated) != expected:
        raise ValueError("runtime selection changed unrelated assembly policy")
    return generated
