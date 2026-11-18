#!/usr/bin/env python3
"""Route an existing fes.ramtest synth.json exactly as the producer does."""
import argparse, hashlib, inspect, json, os, sys
from pathlib import Path

ap = argparse.ArgumentParser()
ap.add_argument("root")
ap.add_argument("--fixture", required=True)   # synth.json, relative to root
ap.add_argument("--dest", required=True)      # output dir, relative to root
ap.add_argument("--cache-root", default="/home/deano/fes/out/cache/misteross-toolchains")
a = ap.parse_args()

root = Path(a.root).resolve()
sys.path.insert(0, str(root))
os.chdir(root)
os.environ.setdefault("FES_TOOLCHAIN_GPU_ROUTER", "HIP")

from scripts import build_fes_ramtest as ramtest
from scripts import fes_build_common as board
from scripts import fes_de10nano_evidence as board_evidence
from scripts.functional_execution import FunctionalInvocation
from scripts.search_placer_qor import route_after_synth

tools = (ramtest.authenticate_for(root, 100, Path(a.cache_root))
         if "memory_mhz" in inspect.signature(ramtest.authenticate_for).parameters
         else ramtest.authenticate_for(root, Path(a.cache_root)))
inv = FunctionalInvocation(tools, 0)
dest = root / a.dest
dest.mkdir(parents=True, exist_ok=True)
try:
    winner = route_after_synth(
        nextpnr=tools["nextpnr-mistral"].path, fixture=root / a.fixture, dest=dest,
        device=board.TARGET, qsf=root / ramtest.QSF, sdc=root / board_evidence.SDC,
        freq="74.25", seeds=ramtest.PLACER_SEEDS, weights=(ramtest.PLACER_TIMING_WEIGHT,),
        critexp=ramtest.PLACER_CRITICALITY_EXPONENT, budget=len(ramtest.PLACER_SEEDS),
        mode="first-pass", extra=(), timeout=ramtest.ROUTE_TIMEOUT_SECONDS,
        gpu_devices=(0,), env=inv.env, audit_source_root=root, core_id="fes.ramtest")
finally:
    inv.close()

rbf = dest / "core.rbf"
print(json.dumps({
    "tree_head": os.popen("git rev-parse HEAD").read().strip(),
    "fixture": a.fixture,
    "fixture_sha256": hashlib.sha256((root / a.fixture).read_bytes()).hexdigest(),
    "winner_seed": winner.seed, "winner_weight": winner.weight,
    "core_rbf_sha256": hashlib.sha256(rbf.read_bytes()).hexdigest(),
    "core_rbf_bytes": rbf.stat().st_size,
    "tools": {n: t.identity for n, t in tools.items()},
}, indent=2))
