#!/usr/bin/env python3
"""Collect each condition-3 gated build's identity, margins and seal verdict.

Reads the build-summary.json each producer wrote, so the numbers in the record
are the producer's own and not re-derived here. A margin is printed as the
ratio of achieved to constrained, because a core that clears by under one per
cent is the case a later failure would be misread as a regression.
"""
from __future__ import annotations
import json, sys
from pathlib import Path

TREE = Path("/data/fes1861-netlist/identity/c3head/sources/misteross")
OUT = Path("/data/fes1861-netlist/cond3")
LOGS = Path("/data/fes1861-netlist/cond3-logs")
# The revision condition 3 must have been taken at. A summary from any other
# revision is a stale directory, not this run, and is refused rather than
# collected: a build-summary.json sitting at a deterministic path is exactly
# the kind of artefact that gets attributed to the wrong run.
EXPECTED_REVISION = None  # set from --revision
OSS = {"menu": "build/oss/fes-menu-package", "splash": "build/fes-splash",
       "c64": "build/fes-c64-oss", "spectrum": "build/fes-spectrum-oss",
       "spectrum128": "build/fes-spectrum128-oss",
       "spectrum-plus3": "build/fes-spectrum-plus3-oss",
       "pentagon": "build/fes-pentagon-oss"}


def margins(timing: dict) -> dict:
    out = {}
    for name, row in (timing or {}).items():
        if not isinstance(row, dict):
            continue
        a, c = row.get("achieved_mhz"), row.get("constraint_mhz")
        if isinstance(a, (int, float)) and isinstance(c, (int, float)) and c:
            out[name] = {"achieved_mhz": a, "constraint_mhz": c,
                         "ratio": a / c, "status": row.get("status")}
    if out:
        out["worst_ratio"] = min(v["ratio"] for v in out.values() if isinstance(v, dict))
    return out


def announced_output(key: str) -> Path | None:
    """The output directory the Quartus producer named in its own log.

    build_fes_zx81_quartus prints "<core_id> Quartus build <build_id>: <dir>"
    before it compiles, so the run says where it wrote rather than this
    program guessing from a glob. Globbing picked the last path in sort order,
    which is not the newest and need not be this run at all.
    """
    log = LOGS / f"{key}.log"
    if not log.is_file():
        return None
    for line in log.read_text(errors="replace").splitlines():
        if " Quartus build " in line and ": " in line:
            return Path(line.rsplit(": ", 1)[1].strip())
    return None


def check(key: str, data: dict, path: Path, revision: str | None) -> str | None:
    """Why this summary must not be collected, or None if it is sound."""
    if data.get("build_id") in (None, ""):
        return "summary has no build_id"
    if revision is not None and data.get("revision") not in (None, revision):
        return f'summary revision {str(data.get("revision"))[:9]} is not the head {revision[:9]}'
    log = LOGS / f"{key}.log"
    if log.is_file() and path.stat().st_mtime < log.stat().st_mtime - 1:
        return "summary predates this run's log: a stale build directory"
    return None


def main() -> int:
    import argparse, subprocess
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--revision", help="the head condition 3 was taken at")
    parser.add_argument("--allow-unverified", action="store_true",
                        help="collect summaries that fail the staleness check, marking them")
    arguments = parser.parse_args()
    revision = arguments.revision
    if revision is None:
        revision = subprocess.run(("git", "-C", str(TREE), "rev-parse", "HEAD"),
                                  capture_output=True, text=True).stdout.strip() or None
    result = {}
    refused = []
    for key, relative in OSS.items():
        path = TREE / relative / "build-summary.json"
        if not path.is_file():
            continue
        data = json.loads(path.read_text())
        why = check(key, data, path, revision)
        if why and not arguments.allow_unverified:
            refused.append(f"{key}: {why}")
            continue
        result[key] = {"lane": "nextpnr", "build_id": data.get("build_id"),
                       "unverified": why,
                       "placer_seed": data.get("route", {}).get("placer_seed"),
                       "margins": margins(data.get("timing")),
                       "source": str(path)}
    for key in ("zx81", "zx80", "ts1000", "ts1500"):
        announced = announced_output(key)
        if announced is not None and (announced / "build-summary.json").is_file():
            chosen = announced / "build-summary.json"
        else:
            found = sorted((TREE / "build").glob(f"fes-{key}-quartus/*/attempt-*/build-summary.json"),
                           key=lambda p: p.stat().st_mtime)
            if not found:
                continue
            chosen = found[-1]
        data = json.loads(chosen.read_text())
        why = check(key, data, chosen, revision)
        if why and not arguments.allow_unverified:
            refused.append(f"{key}: {why}")
            continue
        runs = data.get("runs", [])
        result[key] = {"lane": "quartus-lite", "build_id": data.get("build_id"),
                       "unverified": why,
                       "seeds": data.get("seeds"),
                       "repeat_matches_signoff":
                           bool(runs) and runs[0].get("rbf_sha256") == data.get("repeat_rbf_sha256"),
                       "payload_sha256": data.get("payload_sha256"),
                       "margins": margins((runs[0] if runs else {}).get("timing")),
                       "source": str(chosen)}
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "cond3.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(f'{"artefact":18} {"lane":13} {"build_id":34} worst margin')
    for key, row in sorted(result.items()):
        worst = row["margins"].get("worst_ratio")
        print(f'{key:18} {row["lane"]:13} {str(row["build_id"]):34} '
              f'{"n/a" if worst is None else f"{worst:.4f}"}')
    if refused:
        print("\nREFUSED (not collected):")
        for line in refused:
            print("  " + line)
    print(f"\nwrote {OUT / 'cond3.json'} ({len(result)} artefacts collected"
          + (f", {len(refused)} refused)" if refused else ")"))
    return 1 if refused else 0


if __name__ == "__main__":
    raise SystemExit(main())
