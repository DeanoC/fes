#!/usr/bin/env python3
"""Rebuild the condition-3 seal records from the producers' own summaries.

D12 condition 3 is "the PR's own build passes the core's normal timing and seal
gates". Review raised that the margins quoted in the README existed only in its
prose: the seal records carried a pass status, build id, seed and RBF metadata,
but no timing, so the numbers could not be audited. They are now copied
verbatim from each build's own `build-summary.json`, per clock domain, and the
table is rendered from those files rather than written by hand.

  python3 seal_margins.py --refs /data/fes1861-netlist/ref   # rebuild records
  python3 seal_margins.py                                    # print the table
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os

HERE = os.path.dirname(os.path.abspath(__file__))
SEALS = os.path.join(HERE, "reports-seal")
ARTIFACTS = ("menu", "zx81", "spectrum", "c64")


def margins(timing: dict) -> dict:
    """achieved/constraint per domain, and the worst of them."""
    out = {}
    for domain, values in sorted(timing.items()):
        if isinstance(values, dict) and values.get("achieved_mhz") and values.get("constraint_mhz"):
            out[domain] = {
                "achieved_mhz": values["achieved_mhz"],
                "constraint_mhz": values["constraint_mhz"],
                "ratio": values["achieved_mhz"] / values["constraint_mhz"],
                "status": values.get("status"),
            }
    if out:
        out["worst_ratio"] = min(v["ratio"] for v in out.values() if isinstance(v, dict))
    return out


def rebuild(refs: str) -> None:
    for artifact in ARTIFACTS:
        record = {}
        for side in ("main", "branch"):
            directory = os.path.join(refs, f"{artifact}-{side}")
            summary = json.load(open(os.path.join(directory, "build-summary.json")))
            rbf = os.path.join(directory, "core.rbf")
            with open(rbf, "rb") as stream:
                digest = hashlib.sha256(stream.read()).hexdigest()
            record[side] = {
                "build_id": summary["build_id"],
                "status": summary.get("status"),
                "placer_seed": summary.get("route", {}).get("placer_seed"),
                "rbf_sha256": digest,
                "rbf_bytes": os.path.getsize(rbf),
                "timing": summary.get("timing"),
                "margins": margins(summary.get("timing") or {}),
                "source": f"{directory}/build-summary.json",
            }
        record["note"] = (
            "Each side's own producer at its own BUILD_ID; the RBFs differ because the "
            "constant is in the netlist. Condition 2 is measured at an equal BUILD_ID in "
            "reports-cond2/. Condition 3 is the branch row's status, and the timing block "
            "is copied verbatim from that build's own build-summary.json.")
        path = os.path.join(SEALS, f"{artifact}-normal-seals.json")
        with open(path, "w") as stream:
            stream.write(json.dumps(record, indent=2, sort_keys=True) + "\n")
        print(f"wrote {path}")


def table() -> str:
    rows = ["| Artifact | main worst | branch worst | branch status |",
            "| --- | --- | --- | --- |"]
    for artifact in ARTIFACTS:
        path = os.path.join(SEALS, f"{artifact}-normal-seals.json")
        if not os.path.exists(path):
            continue
        record = json.load(open(path))
        main = record["main"]["margins"].get("worst_ratio")
        branch = record["branch"]["margins"].get("worst_ratio")
        rows.append(f"| `fes.{artifact}` | {main:.3f} | {branch:.3f} "
                    f"| {record['branch']['status']} |")
    return "\n".join(rows)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--refs", help="directory of preserved <artifact>-<side> builds")
    arguments = parser.parse_args(argv)
    if arguments.refs:
        rebuild(arguments.refs)
    print(table())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
