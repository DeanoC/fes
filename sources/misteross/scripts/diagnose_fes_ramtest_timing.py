#!/usr/bin/env python3
"""Measure the 130 MHz RAM tester with the accepted opt-in compiler stack.

This is a host-only timing diagnostic. A completed route remains in build/ even
when it misses timing; it is not validated or exported as a loadable package.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

from scripts import build_fes_ramtest as ramtest
from scripts import fes_build_common as board
from scripts.functional_execution import FunctionalInvocation
from scripts.search_placer_qor import ranking_document, search

ROOT = Path(__file__).resolve().parents[1]
RECIPE = "scripts/diagnose_fes_ramtest_timing.py"
LOCK = "toolchains/ramtest-timing.lock"
OUTPUT = Path("build/fes-ramtest-timing-130")
TOOLCHAIN_ROOT = Path("build/toolchain-ramtest-timing")
TOOL_COMMITS = {
    **board.EXPECTED_TOOL_COMMITS,
    "yosys": "acf441cc385277617e60059691b2f801c9e02660",
    "nextpnr": "d91c902b056fd69a5e897eb8d78a67b568798852",
}
SEED = 2
REPLICATION_BUDGET = 4
REQUIRED_CLOCKS = (
    ("display.pixel_clk", 74.25),
    ("ram_clock.clocks[0]", 130.0),
    ("ram_clock.clocks[1]", 130.0),
)
PINNED_INPUTS = tuple(dict.fromkeys((*ramtest.inputs_for(130), RECIPE, LOCK)))


def fresh_directory(path: Path) -> None:
    if path.is_symlink() or (path.exists() and not path.is_dir()):
        raise board.BuildError(f"diagnostic output must be a non-symlink directory: {path}")
    if path.exists():
        shutil.rmtree(path)
    path.mkdir()


def run(*, gpu_device: int = 0, cache_root: Path | None = None) -> Path:
    root = ROOT.resolve()
    if Path.cwd().resolve() != root:
        raise board.BuildError(f"run the timing diagnostic from {root}")
    repository, revision = board._require_clean_source(root, pinned_inputs=PINNED_INPUTS)
    authenticated = board._authenticate_tools(
        root, lock_path=root / LOCK, toolchain_root=root / TOOLCHAIN_ROOT,
        expected_commits=TOOL_COMMITS, cache_root=cache_root)
    output = board._prepare_output(root, relative=OUTPUT,
                                   build_outputs=("synth.json", "yosys.log", "run-inputs.json", "ranking.json"))
    search_dir = output / "qor-search"
    fresh_directory(search_dir)

    invocation = FunctionalInvocation(authenticated, gpu_device)
    try:
        identities = {name: tool.identity for name, tool in authenticated.items()}
        inputs = {
            "format": 1, "repository": repository, "revision": revision,
            "recipe": RECIPE, "memory_mhz": 130, "seed": SEED,
            "placer_heap_timingweight": ramtest.PLACER_TIMING_WEIGHT,
            "placer_heap_critexp": ramtest.PLACER_CRITICALITY_EXPONENT,
            "replicate_enables": REPLICATION_BUDGET,
            "sources": {name: board._sha256(root / name) for name in sorted(PINNED_INPUTS)},
            "tools": identities, "execution": invocation.inputs,
        }
        build_id = hashlib.sha256(json.dumps(inputs, sort_keys=True, separators=(",", ":")).encode()).hexdigest()[:32]
        inputs["build_id"] = build_id
        board._write_atomic(output / "run-inputs.json", (json.dumps(inputs, indent=2, sort_keys=True) + "\n").encode())
        commands = ramtest.build_commands(root, build_id,
            {name: authenticated[name].path for name in ("yosys", "nextpnr-mistral")},
            memory_mhz=130, output_relative=OUTPUT)
        board._run_tool(commands[0], root, output / "yosys.log",
                        output_relative=OUTPUT, env=invocation.env, audit_source_root=root)
        ranked = search(
            nextpnr=authenticated["nextpnr-mistral"].path,
            fixture=output / "synth.json", output=search_dir, device=board.TARGET,
            qsf=root / ramtest.QSF, sdc=root / ramtest.board_evidence.SDC,
            freq="74.25", seeds=(SEED,), weights=(ramtest.PLACER_TIMING_WEIGHT,),
            critexp=ramtest.PLACER_CRITICALITY_EXPONENT, budget=1, mode="first-pass",
            extra=("--router", "gpu", "--replicate-enables", str(REPLICATION_BUDGET)),
            timeout=ramtest.ROUTE_TIMEOUT_SECONDS, gpu_devices=(gpu_device,),
            required=REQUIRED_CLOCKS,
            env=invocation.env, audit_source_root=root)
        document = ranking_document(ranked, mode="first-pass", budget=1,
                                    critexp=ramtest.PLACER_CRITICALITY_EXPONENT,
                                    seeds=(SEED,), weights=(ramtest.PLACER_TIMING_WEIGHT,))
        document["diagnostic_only"] = True
        document["build_id"] = build_id
        document["valid_clock_set"] = bool(ranked and set(ranked[0].fmax) == {name for name, _ in REQUIRED_CLOCKS})
        board._write_atomic(output / "ranking.json", (json.dumps(document, indent=2, sort_keys=True) + "\n").encode())
        if not document["valid_clock_set"]:
            raise board.BuildError(f"diagnostic route did not produce the three expected clock rows; see {search_dir}")
        if board._require_clean_source(root, pinned_inputs=PINNED_INPUTS) != (repository, revision):
            raise board.BuildError("source identity changed during timing diagnostic")
        invocation.verify()
        return output / "ranking.json"
    finally:
        invocation.close()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gpu-device", type=int, default=0)
    parser.add_argument("--cache-root", type=Path)
    args = parser.parse_args()
    print(run(gpu_device=args.gpu_device, cache_root=args.cache_root))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
