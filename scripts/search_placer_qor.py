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

GPU note: one nextpnr HIP process already holds ~2.5 GB. Two devices can run
two seeds as separate processes (``--gpu-devices 0,1`` is reserved). Packing
several seeds into one kernel is a later change; occupancy is per placement.
"""

from __future__ import annotations

import argparse
import json
import shutil
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Mapping, Sequence


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


def _score_report(report: Mapping[str, object]) -> tuple[bool, float, float, dict[str, tuple[float, float]]]:
    raw = report.get("fmax")
    if not isinstance(raw, dict) or not raw:
        raise ValueError("timing report has no fmax table")
    fmax: dict[str, tuple[float, float]] = {}
    ratios: list[float] = []
    passing = True
    for name, row in raw.items():
        if not isinstance(row, dict):
            continue
        achieved = float(row["achieved"])
        constraint = float(row["constraint"])
        if constraint <= 0:
            raise ValueError(f"non-positive constraint for {name}")
        fmax[str(name)] = (achieved, constraint)
        ratios.append(achieved / constraint)
        if achieved < constraint:
            passing = False
    if not ratios:
        raise ValueError("timing report has no clock rows")
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
            subprocess.run(
                command,
                stdout=log,
                stderr=subprocess.STDOUT,
                check=True,
                timeout=timeout,
            )
        passing, worst, total, fmax = _score_report(json.loads(report.read_text()))
    except (OSError, subprocess.SubprocessError, ValueError, json.JSONDecodeError, KeyError) as exc:
        passing, worst, total, fmax = False, 0.0, 0.0, {}
        extra = f"search_placer_qor: {exc}\n"
        previous = log_path.read_text() if log_path.exists() else ""
        log_path.write_text(previous + extra)
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
    run_one=None,
) -> list[Candidate]:
    run = run_one or (
        lambda seed, weight: _run_nextpnr(
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
            extra=extra,
            timeout=timeout,
        )
    )
    results: list[Candidate] = []
    if mode == "grid":
        pairs = [(seed, weight) for weight in weights for seed in seeds][:budget]
        for seed, weight in pairs:
            results.append(run(seed, weight))
        return sorted(results, key=lambda item: item.key(), reverse=True)
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
    for seed, weight in pairs:
        results.append(run(seed, weight))
    best_weight = sorted(results, key=lambda item: item.key(), reverse=True)[0].weight
    extra_pairs = extend_seed_sweep(list(pairs), seeds, best_weight, budget)[len(pairs) :]
    for seed, weight in extra_pairs:
        results.append(run(seed, weight))
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
    run_one=None,
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
        run_one=run_one,
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
    args = parser.parse_args(argv)
    try:
        seeds = _parse_ints(args.seeds)
        weights = _parse_ints(args.weights)
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
