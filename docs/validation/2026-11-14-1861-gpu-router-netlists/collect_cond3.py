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


def main() -> int:
    result = {}
    for key, relative in OSS.items():
        path = TREE / relative / "build-summary.json"
        if not path.is_file():
            continue
        data = json.loads(path.read_text())
        result[key] = {"lane": "nextpnr", "build_id": data.get("build_id"),
                       "placer_seed": data.get("route", {}).get("placer_seed"),
                       "margins": margins(data.get("timing")),
                       "source": str(path)}
    for key in ("zx81", "zx80", "ts1000", "ts1500"):
        found = sorted((TREE / "build").glob(f"fes-{key}-quartus/*/attempt-*/build-summary.json"))
        if not found:
            continue
        data = json.loads(found[-1].read_text())
        runs = data.get("runs", [])
        result[key] = {"lane": "quartus-lite", "build_id": data.get("build_id"),
                       "seeds": data.get("seeds"),
                       "repeat_matches_signoff":
                           bool(runs) and runs[0].get("rbf_sha256") == data.get("repeat_rbf_sha256"),
                       "payload_sha256": data.get("payload_sha256"),
                       "margins": margins((runs[0] if runs else {}).get("timing")),
                       "source": str(found[-1])}
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / "cond3.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(f'{"artefact":18} {"lane":13} {"build_id":34} worst margin')
    for key, row in sorted(result.items()):
        worst = row["margins"].get("worst_ratio")
        print(f'{key:18} {row["lane"]:13} {str(row["build_id"]):34} '
              f'{"n/a" if worst is None else f"{worst:.4f}"}')
    print(f"\nwrote {OUT / 'cond3.json'} ({len(result)} artefacts)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
