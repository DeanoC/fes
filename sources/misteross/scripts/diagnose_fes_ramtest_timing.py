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
    "yosys": "22bf145df1438aa46eb912beab5b5c19e491906a",
    "nextpnr": "24f9a1db0737559977a13abb2d05cceafa5e6142",
}
SEED = 2
REPLICATION_BUDGET = 4
REMAP_CANDIDATE = 0
REMAP_GROUPS = 8
MEMORY_CLOCK = "ram_clock.clocks[0]"
SIGNOFF_MARKER = "Running signoff timing analysis..."
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


def final_hold_violation(candidate) -> bool:
    """Require a completed HIP signoff; screen only its final reported holds."""
    if set(candidate.fmax) != {name for name, _ in REQUIRED_CLOCKS}:
        raise board.BuildError(f"diagnostic route did not produce the three expected clock rows; see {candidate.log}")
    log = Path(candidate.log).read_text()
    board._require_gpu_backend(log)
    if SIGNOFF_MARKER not in log:
        raise board.BuildError(f"diagnostic route did not reach final signoff; see {candidate.log}")
    return "Hold/min time violation" in log.rsplit(SIGNOFF_MARKER, 1)[1]


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
    remap_dir = output / "remap-search"
    fresh_directory(search_dir)
    fresh_directory(remap_dir)

    invocation = FunctionalInvocation(authenticated, gpu_device)
    try:
        identities = {name: tool.identity for name, tool in authenticated.items()}
        inputs = {
            "format": 1, "repository": repository, "revision": revision,
            "recipe": RECIPE, "memory_mhz": 130, "seed": SEED,
            "placer_heap_timingweight": ramtest.PLACER_TIMING_WEIGHT,
            "placer_heap_critexp": ramtest.PLACER_CRITICALITY_EXPONENT,
            "replicate_enables": REPLICATION_BUDGET,
            "remap": {"candidate": REMAP_CANDIDATE, "groups": REMAP_GROUPS},
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
        search_options = dict(
            nextpnr=authenticated["nextpnr-mistral"].path,
            fixture=output / "synth.json", device=board.TARGET,
            qsf=root / ramtest.QSF, sdc=root / ramtest.board_evidence.SDC,
            freq="74.25", seeds=(SEED,), weights=(ramtest.PLACER_TIMING_WEIGHT,),
            critexp=ramtest.PLACER_CRITICALITY_EXPONENT, budget=1, mode="first-pass",
            timeout=ramtest.ROUTE_TIMEOUT_SECONDS, gpu_devices=(gpu_device,),
            required=REQUIRED_CLOCKS,
            env=invocation.env, audit_source_root=root)
        extra = ("--router", "gpu", "--replicate-enables", str(REPLICATION_BUDGET))
        baseline = search(output=search_dir, extra=extra, **search_options)[0]
        baseline_hold = final_hold_violation(baseline)
        remapped = search(output=remap_dir, extra=(*extra,
            "--remap-critical", str(Path(baseline.run_dir) / "timing.json"),
            "--remap-candidate", str(REMAP_CANDIDATE),
            "--remap-groups", str(REMAP_GROUPS)), **search_options)[0]
        remap_log = Path(remapped.log).read_text()
        no_candidate = (not remapped.fmax
            and "Local remap: 0 qualified candidates; no candidate applied." in remap_log
            and "Requested local-remap candidate was not qualified; routing was not started." in remap_log)
        remap_hold = None if no_candidate else final_hold_violation(remapped)
        select_remap = (not no_candidate and not remap_hold
            and remapped.fmax[MEMORY_CLOCK][0] > baseline.fmax[MEMORY_CLOCK][0]
            and all(remapped.fmax[name][0] >= baseline.fmax[name][0] for name, _ in REQUIRED_CLOCKS))
        ranked = [remapped, baseline] if select_remap else [baseline, remapped]
        document = ranking_document(ranked, mode="first-pass", budget=2,
                                    critexp=ramtest.PLACER_CRITICALITY_EXPONENT,
                                    seeds=(SEED,), weights=(ramtest.PLACER_TIMING_WEIGHT,))
        document.update({
            "diagnostic_only": True, "build_id": build_id, "valid_clock_set": True,
            "remap_status": "no_candidate" if no_candidate else "selected" if select_remap else "rejected",
            "remap": inputs["remap"], "baseline_log": baseline.log,
            "remap_log": remapped.log,
            "final_reported_hold_violation": {"baseline": baseline_hold, "remap": remap_hold},
        })
        board._write_atomic(output / "ranking.json", (json.dumps(document, indent=2, sort_keys=True) + "\n").encode())
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
