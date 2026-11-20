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
RECORD = Path(__file__).resolve().parent
# Where the old-base condition-2 runs and the carry-forward measurement live.
# A row carries only where BOTH are true: the recorded pair itself passed, and
# main's own netlist is byte-identical at the old base and the new one
# (base_move_netlist.py). Neither alone is enough, so neither is assumed.
OLD_COND2 = RECORD / "reports-cond2"
CARRY = ROOT / "identity/base-move-netlist.json"
LANES = {
    "nextpnr": (ROOT / "cond2", "pinned_inputs_sha256",
                ("fes.menu", "fes.c64", "fes.spectrum", "fes.spectrum128",
                 "fes.spectrum-plus3", "fes.pentagon")),
    "quartus-lite": (ROOT / "qcond2", "declared_inputs_sha256",
                     ("zx81", "zx80", "ts1000", "ts1500")),
}


SEARCH = {"fes.spectrum": "fes-spectrum-oss", "fes.spectrum128": "fes-spectrum128-oss",
          "fes.spectrum-plus3": "fes-spectrum-plus3-oss", "fes.pentagon": "fes-pentagon-oss",
          "fes.c64": "fes-c64-oss", "fes.menu": "oss/fes-menu-package"}
TREES = {"main-control": ROOT / "identity/c2base/sources/misteross",
         "branch-forced": ROOT / "identity/c2head/sources/misteross"}


def seed_outcomes(artefact: str) -> dict[str, dict]:
    """Which seeds timed out on each leg, read from the per-seed route logs.

    PRECOMMITTED, written before fes.spectrum-plus3's result existed.

    The per-seed budget (pnr_recipe timeout_s) is enforced in
    scripts/nextpnr_lease.py against time.monotonic() minus GPU lease wait --
    wall clock, not CPU time. Powerboat is shared, so a seed that would close
    can exceed the budget purely because of load this session does not control
    and must not kill. The two legs of a pair run at different times under
    different foreign load, so they can in principle time out on different
    seeds and settle on different winners.

    That would make the RBFs differ for a reason that has nothing to do with
    this change. The rule, fixed in advance: a pair whose legs chose different
    winning seeds is reported NOT COMPARABLE and re-run when the host is quiet.
    It is NOT reported as a condition-2 failure, and the timeout is NOT raised
    to make it pass -- timeout_s is a route-deciding input, so changing it
    would make the comparison unrepresentative of production, and swapping a
    pass rule after seeing a result that fails it is exactly what a
    precommitted gate forbids.
    """
    out = {}
    relative = SEARCH.get(artefact)
    if relative is None:
        return out
    for leg, tree in TREES.items():
        search = tree / "build" / relative / "qor-search"
        if not search.is_dir():
            continue
        timed_out, completed = [], []
        for attempt in sorted(search.glob("s*-w*-c*")):
            log = attempt / "route.log"
            if not log.is_file():
                continue
            text = log.read_text(errors="replace")
            seed = attempt.name.split("-")[0].removeprefix("s")
            (timed_out if "route timed out after" in text else completed).append(seed)
        out[leg] = {"timed_out_seeds": timed_out, "completed_seeds": completed}
    return out


def carried(artefact: str) -> dict | None:
    """Does D12's merge-forward clause carry this artefact's recorded pair?"""
    key = artefact.removeprefix("fes.")
    control = OLD_COND2 / f"{key}-main-control.json"
    forced = OLD_COND2 / f"{key}-branch-forced.json"
    if not (control.is_file() and forced.is_file() and CARRY.is_file()):
        return None
    a, b = json.loads(control.read_text()), json.loads(forced.read_text())
    if a["build_id"] != b["build_id"] or a["rbf_sha256"] != b["rbf_sha256"]:
        return None
    row = json.loads(CARRY.read_text())["rows"].get(key)
    if row is None or not row.get("verdict", "").startswith("identical"):
        return None
    return {"recorded_build_id": a["build_id"], "recorded_rbf_sha256": a["rbf_sha256"],
            "recorded_revision": a["revision"],
            "main_netlist_at_both_bases": row["verdict"]}


def splash_row() -> dict | None:
    """splash is condition 2 without forcing the constant, and a stronger case.

    Its Yosys program sets no BUILD_ID at all, so the RBF cannot depend on one:
    the two sides reproduce the same RBF while their format-1 records -- which
    hash the revision -- give *different* BUILD_IDs. That is reproduction under
    main's BUILD_ID a fortiori, so it is reported on its own terms rather than
    folded in with the forced-constant rows, and the carry check still applies.
    """
    seal = RECORD / "reports-seal" / "splash.json"
    if not (seal.is_file() and CARRY.is_file()):
        return None
    d = json.loads(seal.read_text())
    row = json.loads(CARRY.read_text())["rows"].get("splash")
    if not (d.get("rbf_identical") and d.get("build_ids_differ")
            and d["main"]["status"] == "pass" and d["branch"]["status"] == "pass"):
        return None
    if row is None or not row.get("verdict", "").startswith("identical"):
        return None
    return {"lane": "nextpnr", "verdict": "PASS (no BUILD_ID in the program)",
            "main_build_id": d["main"]["build_id"], "branch_build_id": d["branch"]["build_id"],
            "rbf_sha256": d["main"]["rbf_sha256"], "rbf_bytes": d["main"]["rbf_bytes"],
            "main_netlist_at_both_bases": row["verdict"]}


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
                carry = carried(artefact)
                if carry is not None:
                    claimed.append(artefact)
                    rows[artefact] = {"lane": lane, "verdict": "CARRIED", **carry}
                    print(f'{artefact:20} {lane:13} {carry["recorded_build_id"]:34} '
                          f'{"moved":7} CARRIED from {carry["recorded_revision"][:9]} '
                          f'({carry["main_netlist_at_both_bases"]})')
                    continue
                absent = [l for l, v in legs.items() if v is None]
                missing.append(f"{artefact}: {', '.join(absent)}")
                print(f"{artefact:20} {lane:13} {'-':34} {'-':7} NOT MEASURED ({', '.join(absent)})")
                continue
            b, h = legs["base"], legs["head"]
            seeds = seed_outcomes(artefact)
            if b["build_id"] != h["build_id"]:
                verdict = "INVALID: the two legs used different BUILD_IDs"
            elif b["rbf_sha256"] == h["rbf_sha256"]:
                verdict = "PASS"
            elif (b.get("placer_seed") is not None
                  and b.get("placer_seed") != h.get("placer_seed")):
                # See seed_outcomes: a wall-clock seed budget on a shared host.
                verdict = (f"NOT COMPARABLE: legs settled on different winning seeds "
                           f"({b.get('placer_seed')} vs {h.get('placer_seed')}); re-run when quiet")
            else:
                verdict = "FAIL: RBF differs"
            inputs_moved = b[inputs_key] != h[inputs_key]
            if verdict == "PASS" and inputs_moved:
                claimed.append(artefact)
            elif verdict.startswith("NOT COMPARABLE"):
                missing.append(f"{artefact}: {verdict}")
            rows[artefact] = {
                "lane": lane, "build_id": b["build_id"], "verdict": verdict,
                "seed_outcomes": seeds,
                "winning_seed_base": b.get("placer_seed"),
                "winning_seed_head": h.get("placer_seed"),
                "inputs_moved": inputs_moved,
                "rbf_sha256": b["rbf_sha256"], "rbf_bytes": b["rbf_bytes"],
                "netlist_or_qsf": b.get("netlist_sha256") or b.get("canonical_qsf_sha256"),
                "seed": b.get("placer_seed") or b.get("seed"),
                "base_revision": b["revision"], "head_revision": h["revision"],
                f"base_{inputs_key}": b[inputs_key], f"head_{inputs_key}": h[inputs_key],
            }
            print(f'{artefact:20} {lane:13} {b["build_id"]:34} '
                  f'{"moved" if inputs_moved else "SAME!":7} {verdict}')
    sp = splash_row()
    if sp is None:
        missing.append("splash: seal record or carry measurement absent")
        print(f'{"splash":20} {"nextpnr":13} {"-":34} {"-":7} NOT MEASURED')
    else:
        rows["splash"] = sp
        claimed.append("splash")
        print(f'{"splash":20} {"nextpnr":13} {sp["main_build_id"]:34} '
              f'{"moved":7} {sp["verdict"]} -- RBF equal at two different ids')
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
