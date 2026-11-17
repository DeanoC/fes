#!/usr/bin/env python3
"""Render D12 condition 2: both sides at one forced BUILD_ID, RBFs compared.

Covers both lanes. The nextpnr artefacts come from rbf_at_build_id.py under
cond2/, the ZX81 family from quartus_rbf_at_build_id.py under qcond2/. A row
is claimed only where both legs exist; a missing leg prints NOT MEASURED with
the leg named, so a partially collected run cannot read as a result.

The inputs-moved column is the point of the comparison: the producer's declared
inputs differ between the sides, which is why BUILD_ID moved, while the
hardware does not. A row where they did *not* move is flagged, because then the
comparison is not testing anything.

  python3 summarise_cond2.py [--json OUT]
"""
from __future__ import annotations
import argparse, json
from pathlib import Path

ROOT = Path("/data/fes1861-netlist")
LANES = {
    "nextpnr": (ROOT / "cond2", "pinned_inputs_sha256",
                ("fes.menu", "fes.c64", "fes.spectrum", "fes.spectrum128",
                 "fes.spectrum-plus3", "fes.pentagon")),
    "quartus-lite": (ROOT / "qcond2", "declared_inputs_sha256",
                     ("zx81", "zx80", "ts1000", "ts1500")),
}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    rows, claimed, missing = {}, [], []
    print(f'{"artefact":20} {"lane":13} {"build_id":34} {"inputs":7} verdict')
    for lane, (directory, inputs_key, artefacts) in LANES.items():
        for artefact in artefacts:
            key = artefact.removeprefix("fes.")
            legs = {}
            for leg, label in (("base", "main-control"), ("head", "branch-forced")):
                path = directory / f"{key}-{label}" / "rbf.json"
                legs[leg] = json.loads(path.read_text()) if path.is_file() else None
            if any(v is None for v in legs.values()):
                absent = [l for l, v in legs.items() if v is None]
                missing.append(f"{artefact}: {', '.join(absent)}")
                print(f"{artefact:20} {lane:13} {'-':34} {'-':7} NOT MEASURED ({', '.join(absent)})")
                continue
            b, h = legs["base"], legs["head"]
            if b["build_id"] != h["build_id"]:
                verdict = "INVALID: the two legs used different BUILD_IDs"
            elif b["rbf_sha256"] == h["rbf_sha256"]:
                verdict = "PASS"
            else:
                verdict = "FAIL: RBF differs"
            inputs_moved = b[inputs_key] != h[inputs_key]
            if verdict == "PASS" and inputs_moved:
                claimed.append(artefact)
            rows[artefact] = {
                "lane": lane, "build_id": b["build_id"], "verdict": verdict,
                "inputs_moved": inputs_moved,
                "rbf_sha256": b["rbf_sha256"], "rbf_bytes": b["rbf_bytes"],
                "netlist_or_qsf": b.get("netlist_sha256") or b.get("canonical_qsf_sha256"),
                "seed": b.get("placer_seed") or b.get("seed"),
                "base_revision": b["revision"], "head_revision": h["revision"],
                f"base_{inputs_key}": b[inputs_key], f"head_{inputs_key}": h[inputs_key],
            }
            print(f'{artefact:20} {lane:13} {b["build_id"]:34} '
                  f'{"moved" if inputs_moved else "SAME!":7} {verdict}')
    print()
    print(f"{len(claimed)} artefact(s) claimed on condition 2")
    if missing:
        print("not measured: " + "; ".join(missing))
    if arguments.json:
        arguments.json.write_text(json.dumps(
            {"rows": rows, "claimed": sorted(claimed), "not_measured": missing},
            indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json}")
    return 0 if not missing and len(claimed) == len(rows) else 1


if __name__ == "__main__":
    raise SystemExit(main())
