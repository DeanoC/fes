#!/usr/bin/env python3
"""Search HeAP timing weight and placer seed after a single synthesis.

The FES Coleco/ZX81 recipes currently bake one weight into the producer and
take the first seed that meets the clock constraints. That feels like luck
because:

- the sealed BUILD_ID is hashed into synth.json, so a hand-picked winner
  written back into the recipe changes the netlist and invalidates the search
- seed is a Monte Carlo draw of HeAP, not a continuous optimum
- first-to-pass is the right factory policy for "hit 52 MHz"; it is the wrong
  policy for a CPU, where faster is materially better

This tool keeps the *search policy* in the command line (seed list, weight
list, budget). It synthesizes nothing. Point it at an existing synth.json,
evaluate place-and-route candidates, and write a ranking. The winner belongs
in build evidence (`placer_seed`, `placer_heap_timingweight`), not in a
recipe constant that would change BUILD_ID.

Selection is deterministic: passing candidates outrank failing ones; then
higher worst-clock ratio (achieved/constraint); then higher sum of ratios;
then lower weight; then lower seed.

Staged search (default) is not a full grid:

1. Probe every weight on a few diverse seeds (first, middle, last).
2. Lock the best weight from those probes.
3. Sweep the remaining seeds at that weight until the budget is exhausted.

``--mode grid`` evaluates the Cartesian product (still bounded by
``--budget``). ``--mode first-pass`` stops at the first candidate that meets
every constraint, matching today's producers.

GPU: ``--gpu-devices 0,1`` runs staged/grid candidates as separate nextpnr
processes, one HIP device each (7900 XTX + R9700). First-pass stays
sequential so the first closing seed is deterministic. Packing several
seeds into one kernel is later; occupancy is per placement.
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor
import contextvars
from dataclasses import asdict, dataclass
from pathlib import Path
from queue import Queue
from typing import Callable, Mapping, Sequence


class SearchError(Exception):
    """No evaluated placement met every clock constraint."""


@dataclass(frozen=True)
class Candidate:
    seed: int
    weight: int
    critexp: int
    passing: bool
    worst_ratio: float
    sum_ratio: float
    fmax: dict[str, tuple[float, float]]
    log: str
    run_dir: str = ""

    def key(self) -> tuple:
        return (self.passing, self.worst_ratio, self.sum_ratio, -self.weight, -self.seed)


def _parse_clock(text: str) -> tuple[str | None, float]:
    if ":" not in text:
        raise ValueError("clock must be name:mhz or :mhz")
    name, mhz = text.split(":", 1)
    return (name or None, float(mhz))


def _parse_ints(text: str) -> tuple[int, ...]:
    values = tuple(int(part) for part in text.split(",") if part.strip() != "")
    if not values:
        raise ValueError("need at least one integer")
    return values


def _probe_seeds(seeds: Sequence[int]) -> tuple[int, ...]:
    if len(seeds) == 1:
        return (seeds[0],)
    mid = seeds[len(seeds) // 2]
    ordered = []
    for seed in (seeds[0], mid, seeds[-1]):
        if seed not in ordered:
            ordered.append(seed)
    return tuple(ordered)


def _parse_fmax(report: Mapping[str, object]) -> dict[str, tuple[float, float]]:
    raw = report.get("fmax")
    if not isinstance(raw, dict) or not raw:
        raise ValueError("timing report has no fmax table")
    fmax: dict[str, tuple[float, float]] = {}
    for name, row in raw.items():
        if not isinstance(row, dict):
            continue
        achieved = float(row["achieved"])
        constraint = float(row["constraint"])
        if constraint <= 0:
            raise ValueError(f"non-positive constraint for {name}")
        fmax[str(name)] = (achieved, constraint)
    if not fmax:
        raise ValueError("timing report has no clock rows")
    return fmax


def _match_clock(
    fmax: Mapping[str, tuple[float, float]],
    name_contains: str | None,
    expected: float,
) -> tuple[str, float, float]:
    matches: list[tuple[str, float, float]] = []
    tolerance = max(1e-6, expected * 5e-5)
    for name, (achieved, constraint) in fmax.items():
        if name_contains is not None and name_contains not in name:
            continue
        if abs(constraint - expected) <= tolerance:
            matches.append((name, achieved, constraint))
    if len(matches) != 1:
        label = name_contains or f"{expected:g} MHz"
        raise ValueError(f"timing report must contain exactly one {label} clock")
    return matches[0]


def _score_report(
    report: Mapping[str, object],
    required: Sequence[tuple[str | None, float]] | None = None,
) -> tuple[bool, float, float, dict[str, tuple[float, float]]]:
    fmax = _parse_fmax(report)
    if required:
        rows = [_match_clock(fmax, name, expected) for name, expected in required]
    else:
        rows = [(name, achieved, constraint) for name, (achieved, constraint) in fmax.items()]
    ratios = [achieved / constraint for _, achieved, constraint in rows]
    passing = all(achieved >= constraint for _, achieved, constraint in rows)
    return passing, min(ratios), sum(ratios), fmax


def _run_nextpnr(
    nextpnr: Path,
    fixture: Path,
    output: Path,
    *,
    device: str,
    qsf: Path,
    sdc: Path | None,
    freq: str | None,
    seed: int,
    weight: int,
    critexp: int,
    extra: Sequence[str],
    timeout: int,
    required: Sequence[tuple[str | None, float]] | None = None,
    env=None,
    audit_source_root=None,
) -> Candidate:
    run_dir = output / f"s{seed}-w{weight}-c{critexp}"
    if run_dir.exists():
        shutil.rmtree(run_dir)
    run_dir.mkdir(parents=True)
    report = run_dir / "timing.json"
    log_path = run_dir / "route.log"
    command = [
        str(nextpnr),
        "--json", str(fixture),
        "--device", device,
        "--qsf", str(qsf),
        "--seed", str(seed),
        "--placer-heap-timingweight", str(weight),
        "--placer-heap-critexp", str(critexp),
        "--report", str(report),
        "--write", str(run_dir / "routed.json"),
        "--rbf", str(run_dir / "core.rbf"),
        "--compress-rbf",
        "--detailed-timing-report",
        "--timing-allow-fail",
        *extra,
    ]
    if sdc is not None:
        command += ["--sdc", str(sdc)]
    if freq is not None:
        command += ["--freq", freq]
    try:
        with log_path.open("w") as log:
            from scripts.compiler_read_audit import audited_run
            runner = subprocess.run if audit_source_root is None else audited_run
            result = runner(
                command,
                **({"source_root": audit_source_root} if audit_source_root is not None else {}),
                env=env,
                stdout=log,
                stderr=subprocess.STDOUT,
                check=False,
                timeout=timeout,
            )
    except (OSError, subprocess.TimeoutExpired) as exc:
        extra_text = f"search_placer_qor: {exc}\n"
        previous = log_path.read_text() if log_path.exists() else ""
        log_path.write_text(previous + extra_text)
        return Candidate(
            seed=seed,
            weight=weight,
            critexp=critexp,
            passing=False,
            worst_ratio=0.0,
            sum_ratio=0.0,
            fmax={},
            log=str(log_path),
            run_dir=str(run_dir),
        )
    text = log_path.read_text() if log_path.exists() else ""
    rbf = run_dir / "core.rbf"
    finished = (
        "Info: Program finished normally." in text
        and report.is_file()
        and rbf.is_file()
        and rbf.stat().st_size > 0
    )
    # nextpnr-mistral can emit an intermediate timing ERROR, repair, finish a
    # payload, and still exit 1. Score a finished run; crash/timeout stays 0.
    if not finished:
        if result.returncode != 0:
            extra_text = f"search_placer_qor: tool failed with exit {result.returncode}\n"
            log_path.write_text(text + extra_text)
        return Candidate(
            seed=seed,
            weight=weight,
            critexp=critexp,
            passing=False,
            worst_ratio=0.0,
            sum_ratio=0.0,
            fmax={},
            log=str(log_path),
            run_dir=str(run_dir),
        )
    try:
        passing, worst, total, fmax = _score_report(json.loads(report.read_text()), required)
    except (ValueError, json.JSONDecodeError, KeyError, OSError) as exc:
        extra_text = f"search_placer_qor: {exc}\n"
        log_path.write_text(text + extra_text)
        return Candidate(
            seed=seed,
            weight=weight,
            critexp=critexp,
            passing=False,
            worst_ratio=0.0,
            sum_ratio=0.0,
            fmax={},
            log=str(log_path),
            run_dir=str(run_dir),
        )
    return Candidate(
        seed=seed,
        weight=weight,
        critexp=critexp,
        passing=passing,
        worst_ratio=worst,
        sum_ratio=total,
        fmax=fmax,
        log=str(log_path),
        run_dir=str(run_dir),
    )


def plan_staged(
    seeds: Sequence[int],
    weights: Sequence[int],
    budget: int,
) -> list[tuple[int, int]]:
    """Return (seed, weight) pairs in evaluation order, unique, budget-capped."""
    if budget < 1:
        raise ValueError("budget must be at least 1")
    planned: list[tuple[int, int]] = []
    seen: set[tuple[int, int]] = set()

    def add(seed: int, weight: int) -> None:
        key = (seed, weight)
        if key in seen or len(planned) >= budget:
            return
        seen.add(key)
        planned.append(key)

    probes = _probe_seeds(seeds)
    for weight in weights:
        for seed in probes:
            add(seed, weight)
    return planned


def evaluate_pairs(
    pairs: Sequence[tuple[int, int]],
    run: Callable[[int, int], Candidate],
    workers: int,
) -> list[Candidate]:
    if workers <= 1 or len(pairs) <= 1:
        return [run(seed, weight) for seed, weight in pairs]
    with ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(contextvars.copy_context().run, run, seed, weight) for seed, weight in pairs]
        return [future.result() for future in futures]


def extend_seed_sweep(
    planned: list[tuple[int, int]],
    seeds: Sequence[int],
    weight: int,
    budget: int,
) -> list[tuple[int, int]]:
    seen = set(planned)
    for seed in seeds:
        key = (seed, weight)
        if key in seen or len(planned) >= budget:
            continue
        seen.add(key)
        planned.append(key)
    return planned


def search(
    *,
    nextpnr: Path,
    fixture: Path,
    output: Path,
    device: str,
    qsf: Path,
    sdc: Path | None,
    freq: str | None,
    seeds: Sequence[int],
    weights: Sequence[int],
    critexp: int,
    budget: int,
    mode: str,
    extra: Sequence[str],
    timeout: int,
    required: Sequence[tuple[str | None, float]] | None = None,
    gpu_devices: Sequence[int] = (),
    run_one=None,
    env=None,
    audit_source_root=None,
) -> list[Candidate]:
    gpu_pool: Queue[int | None] = Queue()
    assigned = list(gpu_devices) if gpu_devices else [None]
    for gpu in assigned:
        gpu_pool.put(gpu)

    def run(seed: int, weight: int) -> Candidate:
        if run_one is not None:
            return run_one(seed, weight)
        gpu = gpu_pool.get()
        try:
            extra_run = list(extra)
            if gpu is not None:
                extra_run += ["--gpu-device", str(gpu)]
            return _run_nextpnr(
                nextpnr,
                fixture,
                output,
                device=device,
                qsf=qsf,
                sdc=sdc,
                freq=freq,
                seed=seed,
                weight=weight,
                critexp=critexp,
                extra=tuple(extra_run),
                timeout=timeout,
                required=required,
                env=env,
                audit_source_root=audit_source_root,
            )
        finally:
            gpu_pool.put(gpu)

    # First-pass must stay sequential so the first closing seed is stable.
    workers = 1 if mode == "first-pass" else max(1, len(assigned) if gpu_devices else 1)
    results: list[Candidate] = []
    if mode == "grid":
        pairs = [(seed, weight) for weight in weights for seed in seeds][:budget]
        return sorted(evaluate_pairs(pairs, run, workers), key=lambda item: item.key(), reverse=True)
    if mode == "first-pass":
        for weight in weights:
            for seed in seeds:
                if len(results) >= budget:
                    return sorted(results, key=lambda item: item.key(), reverse=True)
                candidate = run(seed, weight)
                results.append(candidate)
                if candidate.passing:
                    return sorted(results, key=lambda item: item.key(), reverse=True)
        return sorted(results, key=lambda item: item.key(), reverse=True)

    pairs = plan_staged(seeds, weights, budget)
    results = evaluate_pairs(pairs, run, workers)
    best_weight = sorted(results, key=lambda item: item.key(), reverse=True)[0].weight
    extra_pairs = extend_seed_sweep(list(pairs), seeds, best_weight, budget)[len(pairs) :]
    results.extend(evaluate_pairs(extra_pairs, run, workers))
    return sorted(results, key=lambda item: item.key(), reverse=True)


def _candidate_json(item: Candidate) -> dict:
    payload = asdict(item)
    payload["fmax"] = {
        name: {"achieved": achieved, "constraint": constraint}
        for name, (achieved, constraint) in item.fmax.items()
    }
    return payload


def ranking_document(
    ranked: Sequence[Candidate],
    *,
    mode: str,
    budget: int,
    critexp: int,
    seeds: Sequence[int],
    weights: Sequence[int],
) -> dict:
    return {
        "mode": mode,
        "budget": budget,
        "critexp": critexp,
        "seeds": list(seeds),
        "weights": list(weights),
        "winner": None if not ranked else _candidate_json(ranked[0]),
        "ranking": [_candidate_json(item) for item in ranked],
    }


def promote_candidate(candidate: Candidate, dest: Path) -> None:
    src = Path(candidate.run_dir)
    mapping = {
        "timing.json": dest / "timing.json",
        "routed.json": dest / "routed.json",
        "core.rbf": dest / "core.rbf",
        "route.log": dest / "nextpnr.log",
    }
    for name, target in mapping.items():
        source = src / name
        if not source.is_file():
            raise SearchError(f"winner is missing {name}: {source}")
        if target.exists() or target.is_symlink():
            target.unlink()
        shutil.copy2(source, target)


def route_after_synth(
    *,
    nextpnr: Path,
    fixture: Path,
    dest: Path,
    device: str,
    qsf: Path,
    sdc: Path | None,
    freq: str | None,
    seeds: Sequence[int],
    weights: Sequence[int],
    critexp: int,
    budget: int,
    mode: str,
    extra: Sequence[str],
    timeout: int = 600,
    required: Sequence[tuple[str | None, float]] | None = None,
    gpu_devices: Sequence[int] = (),
    run_one=None,
    env=None,
    audit_source_root=None,
) -> Candidate:
    """Place-and-route candidates after synth.json exists; promote the winner."""
    search_dir = dest / "qor-search"
    if search_dir.exists():
        shutil.rmtree(search_dir)
    search_dir.mkdir(parents=True)
    ranked = search(
        nextpnr=nextpnr,
        fixture=fixture,
        output=search_dir,
        device=device,
        qsf=qsf,
        sdc=sdc,
        freq=freq,
        seeds=seeds,
        weights=weights,
        critexp=critexp,
        budget=budget,
        mode=mode,
        extra=extra,
        timeout=timeout,
        required=required,
        gpu_devices=gpu_devices,
        run_one=run_one,
        env=env,
        audit_source_root=audit_source_root,
    )
    (dest / "qor-ranking.json").write_text(
        json.dumps(
            ranking_document(
                ranked,
                mode=mode,
                budget=budget,
                critexp=critexp,
                seeds=seeds,
                weights=weights,
            ),
            indent=2,
            sort_keys=True,
        )
        + "\n"
    )
    if not ranked:
        raise SearchError("placer QoR search produced no candidates")
    winner = ranked[0]
    if not winner.passing:
        raise SearchError(
            f"no placement met timing (best seed={winner.seed} weight={winner.weight} "
            f"worst_ratio={winner.worst_ratio:.4f})"
        )
    promote_candidate(winner, dest)
    return winner


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--nextpnr", type=Path, required=True)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--device", default="5CSEBA6U23I7")
    parser.add_argument("--qsf", type=Path, required=True)
    parser.add_argument("--sdc", type=Path)
    parser.add_argument("--freq")
    parser.add_argument("--seeds", default="4,1,2,3,5,12,7,10")
    parser.add_argument("--weights", default="10,100,300,1000,2000")
    parser.add_argument("--critexp", type=int, default=5)
    parser.add_argument("--budget", type=int, default=24)
    parser.add_argument("--mode", choices=("staged", "grid", "first-pass"), default="staged")
    parser.add_argument("--timeout", type=int, default=600)
    parser.add_argument("--extra", nargs="*", default=[])
    parser.add_argument(
        "--clock",
        action="append",
        default=[],
        metavar="NAME:MHZ",
        help="required clock for pass/fail (repeatable). Use :mhz to match constraint only.",
    )
    parser.add_argument(
        "--gpu-devices",
        default="",
        help="comma-separated HIP device indices for staged/grid (e.g. 0,1)",
    )
    args = parser.parse_args(argv)
    try:
        seeds = _parse_ints(args.seeds)
        weights = _parse_ints(args.weights)
        required = tuple(_parse_clock(item) for item in args.clock) or None
        gpu_devices = _parse_ints(args.gpu_devices) if args.gpu_devices.strip() else ()
    except ValueError as exc:
        print(f"search_placer_qor: {exc}", file=sys.stderr)
        return 2
    args.output.mkdir(parents=True, exist_ok=True)
    ranked = search(
        nextpnr=args.nextpnr.resolve(),
        fixture=args.fixture.resolve(),
        output=args.output.resolve(),
        device=args.device,
        qsf=args.qsf.resolve(),
        sdc=None if args.sdc is None else args.sdc.resolve(),
        freq=args.freq,
        seeds=seeds,
        weights=weights,
        critexp=args.critexp,
        budget=args.budget,
        mode=args.mode,
        extra=tuple(args.extra),
        timeout=args.timeout,
        required=required,
        gpu_devices=gpu_devices,
    )
    report = ranking_document(
        ranked,
        mode=args.mode,
        budget=args.budget,
        critexp=args.critexp,
        seeds=seeds,
        weights=weights,
    )
    (args.output / "ranking.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    if not ranked:
        print("search_placer_qor: no candidates", file=sys.stderr)
        return 1
    winner = ranked[0]
    print(
        f"winner seed={winner.seed} weight={winner.weight} "
        f"passing={str(winner.passing).lower()} worst_ratio={winner.worst_ratio:.4f}"
    )
    for item in ranked:
        clocks = " ".join(
            f"{name}={achieved:.2f}/{constraint:.2f}"
            for name, (achieved, constraint) in sorted(item.fmax.items())
        )
        print(
            f"  seed {item.seed:3d}  w {item.weight:4d}  "
            f"{'PASS' if item.passing else 'FAIL'}  {clocks}"
        )
    return 0 if winner.passing else 1


if __name__ == "__main__":
    raise SystemExit(main())
