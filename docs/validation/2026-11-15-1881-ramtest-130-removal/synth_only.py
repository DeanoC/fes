#!/usr/bin/env python3
"""Run fes.ramtest's Yosys step alone with a caller-chosen BUILD_ID."""
import argparse, hashlib, json, os, sys
from pathlib import Path

ap = argparse.ArgumentParser()
ap.add_argument("root")                      # sources/misteross of the tree
ap.add_argument("--build-id", default="0" * 32)
ap.add_argument("--out", required=True)      # directory for synth.json, relative to root
ap.add_argument("--cache-root", default="/home/deano/fes/out/cache/misteross-toolchains")
a = ap.parse_args()

root = Path(a.root).resolve()
sys.path.insert(0, str(root))
os.chdir(root)
os.environ.setdefault("FES_TOOLCHAIN_GPU_ROUTER", "HIP")

from scripts import build_fes_ramtest as ramtest
from scripts import fes_build_common as board
from scripts.functional_execution import FunctionalInvocation

import inspect
def call(fn, *args, **kw):
    """Call a producer helper across the signature change this PR makes."""
    names = inspect.signature(fn).parameters
    if "memory_mhz" in names:
        kw["memory_mhz"] = 100
    return fn(*args, **kw)

tools = (ramtest.authenticate_for(root, 100, Path(a.cache_root))
         if "memory_mhz" in inspect.signature(ramtest.authenticate_for).parameters
         else ramtest.authenticate_for(root, Path(a.cache_root)))
identities = {n: t.identity for n, t in tools.items()}
inv = FunctionalInvocation(tools, 0)
paths = {n: tools[n].path for n in ("yosys", "nextpnr-mistral")}

out = Path(a.out)
(root / out).mkdir(parents=True, exist_ok=True)
cmd = call(ramtest.build_commands, root, a.build_id, paths)[0]
# Point the Yosys program at the requested output directory.
default = ramtest.OUTPUT_100.as_posix()
cmd = tuple(part.replace(f"{default}/synth.json", f"{out.as_posix()}/synth.json") for part in cmd)
try:
    board._run_tool(cmd, root, root / out / "yosys.log",
                    output_relative=out, env=inv.env, audit_source_root=root)
finally:
    inv.close()

synth = root / out / "synth.json"
data = synth.read_bytes()
print(json.dumps({
    "tree_head": os.popen("git rev-parse HEAD").read().strip(),
    "pinned_inputs_modified": os.popen(
        "git status --porcelain -- " + " ".join(call(ramtest.inputs_for))).read().strip(),
    "pinned_input_digest": hashlib.sha256("".join(
        f"{p} {board._sha256(root / p)}\n" for p in sorted(call(ramtest.inputs_for))).encode()).hexdigest(),
    "build_id": a.build_id,
    "tools": identities,
    "synth_json_sha256": hashlib.sha256(data).hexdigest(),
    "synth_json_bytes": len(data),
    "synth_json_path": str(synth),
}, indent=2))
