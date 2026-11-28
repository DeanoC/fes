#!/usr/bin/python3 -I
"""Run the worker, receipt validator and smoke adapters from the requested main."""
from contextlib import ExitStack
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

REPO = Path.home() / "fes"
BASE = Path.home() / ".local/share/fes-qualification"


def run_snapshot(checkout, policy, binding):
    # Earlier workers insert ~/fes/scripts at import time. Load the frozen
    # validator first, then remove the ambient path before any lazy imports.
    scripts = checkout / "scripts"
    sys.path.insert(0, str(scripts))
    import core_dev_accept
    if Path(core_dev_accept.__file__).resolve() != scripts / "core_dev_accept.py":
        raise ValueError("validator outside frozen checkout")
    path = policy / "scripts/powerboat/nightly-kit-worker.py"
    if binding == "--check":
        import runpy
        sys.argv = [str(path), binding]
        runpy.run_path(str(path), run_name="__main__")
        return
    spec = importlib.util.spec_from_file_location("nightly_worker", path)
    worker = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(worker)
    sys.path[:] = [p for p in sys.path if p != str(REPO / "scripts")]
    sys.stdout.buffer.write(worker.accept(binding))


def launch(binding):
    if binding != "--check" and not re.fullmatch("[0-9a-f]{64}", binding):
        raise ValueError("invalid request digest")
    git = ["git", "-C", REPO]
    subprocess.run([*git, "fetch", "-q", "origin", "main"], check=True)
    head = subprocess.check_output([*git, "rev-parse", "origin/main"], text=True).strip()
    policy_head = head
    if binding != "--check":
        remote = str(BASE / "store/requests" / binding)
        data = subprocess.check_output(["ssh", "-oBatchMode=yes", "fes-build-3090", "cat", remote])
        request = json.loads(data)["request"]
        encoded = (json.dumps(request, sort_keys=True, separators=(",", ":")) + "\n").encode()
        head = request["main"]
        if hashlib.sha256(encoded).hexdigest() != binding or not re.fullmatch("[0-9a-f]{40}", head):
            raise ValueError("invalid frozen request")
        subprocess.run([*git, "merge-base", "--is-ancestor", head, "origin/main"], check=True)
        policy_head = request.get("policy_main", head)
        if "policy_main" in request:
            if not re.fullmatch("[0-9a-f]{40}", policy_head) or not re.fullmatch("[0-9a-f]{64}", request.get("replaces", "")):
                raise ValueError("invalid replacement policy")
            subprocess.run([*git, "merge-base", "--is-ancestor", head, policy_head], check=True)
            subprocess.run([*git, "merge-base", "--is-ancestor", policy_head, "origin/main"], check=True)
    with tempfile.TemporaryDirectory(dir=BASE) as tmp, ExitStack() as cleanup:
        def checkout_at(name, revision):
            path = Path(tmp) / name
            subprocess.run([*git, "worktree", "add", "--quiet", "--detach", path, revision], check=True)
            cleanup.callback(subprocess.run, [*git, "worktree", "remove", "--force", path], check=True)
            return path

        checkout = checkout_at("source", head)
        policy = checkout if policy_head == head else checkout_at("policy", policy_head)
        if binding != "--check":
            hil = request["hil"]
            paths = dict(hil.get("smoke_adapters", {}))
            if "worker_sha256" in hil:
                paths["scripts/powerboat/nightly-kit-worker.py"] = hil["worker_sha256"]
            for name, digest in paths.items():
                path = (policy / name).resolve()
                if not path.is_relative_to(policy) or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                    raise ValueError("frozen HIL executable mismatch")
        cleanup.callback(os.chdir, Path.cwd())
        os.chdir(checkout)
        run_snapshot(checkout, policy, binding)


if __name__ == "__main__":
    os.umask(0o077)
    sys.dont_write_bytecode = True
    if len(sys.argv) != 2:
        sys.exit("expected request digest or --check")
    launch(sys.argv[1])
