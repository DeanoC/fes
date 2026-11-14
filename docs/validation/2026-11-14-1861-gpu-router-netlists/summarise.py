#!/usr/bin/env python3
"""Render the results table from reports/, so no digest is transcribed by hand.

  python3 summarise.py            # print the table
  python3 summarise.py --write    # replace it between README.md's markers
"""
from __future__ import annotations

import argparse
import collections
import glob
import json
import os

START = "<!-- results:start -->"
END = "<!-- results:end -->"

# What each core's producer closure picks up from this PR's diff.
CHANGED = {
    "fes.pong": "`build_fes_pong.py`, `fes_de10nano_evidence.py`",
    "fes.menu": "`build_fes_menu_package.py`, `build_fes_menu.py`, `fes_de10nano_evidence.py`",
    "fes.ramtest": "`build_fes_ramtest.py`, `fes_de10nano_evidence.py`",
    "fes.riscv": "`build_fes_riscv.py`, `fes_de10nano_evidence.py`",
}
HELPER_ONLY = "`fes_de10nano_evidence.py`"


def table() -> str:
    sides: dict[str, dict[str, dict]] = collections.defaultdict(dict)
    for path in sorted(glob.glob(os.path.join(os.path.dirname(__file__) or ".",
                                              "reports/*/netlist.json"))):
        record = json.load(open(path))
        side = "main" if os.path.basename(os.path.dirname(path)).endswith("-main") else "branch"
        sides[record["core"]][side] = record
    rows = ["| Core | Changed files in its closure | Pinned inputs | Netlist sha256 | Match |",
            "| --- | --- | --- | --- | --- |"]
    for core, found in sorted(sides.items()):
        main_record, branch = found.get("main"), found.get("branch")
        if not (main_record and branch):
            rows.append(f"| `{core}` | {CHANGED.get(core, HELPER_ONLY)} | — | — | one side only |")
            continue
        for side, record in (("main", main_record), ("branch", branch)):
            if not record.get("revision"):
                raise SystemExit(f"{core} {side}: record has no revision; re-run it")
            # Only the pinned inputs decide the netlist, so an edit elsewhere in
            # the tree (a doc, this very file) does not invalidate a run. A
            # modified pinned input does, and the driver refuses those outright.
            if record.get("dirty_pinned_inputs"):
                raise SystemExit(f"{core} {side}: recorded from modified pinned inputs: "
                                 + ", ".join(record["dirty_pinned_inputs"]))
        # The inputs are expected to differ: that is why BUILD_ID moves at all.
        inputs = ("differ" if main_record["pinned_inputs_sha256"]
                  != branch["pinned_inputs_sha256"] else "**identical**")
        same = main_record["netlist_sha256"] == branch["netlist_sha256"]
        shown = main_record["netlist_sha256"] if same else (
            f"main `{main_record['netlist_sha256'][:16]}…` vs "
            f"branch `{branch['netlist_sha256'][:16]}…`")
        verdict = "**byte-identical**" if same else "**DIFFERS**"
        cell = f"`{shown}`" if same else shown
        rows.append(f"| `{core}` | {CHANGED.get(core, HELPER_ONLY)} | {inputs} | {cell} "
                    f"| {verdict} |")
    revisions = {(r["revision"][:9]) for found in sides.values()
                 for r in found.values() if r.get("revision")}
    rows.append("")
    rows.append("Revisions measured: " + ", ".join(f"`{r}`" for r in sorted(revisions)) + ".")
    rows.append("Each row's pinned inputs were unmodified at the revision shown; "
                "`summarise.py` refuses to render a record with no revision or with a "
                "modified pinned input, and the driver refuses to write one.")
    return "\n".join(rows)


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true",
                        help="replace the table between README.md's markers")
    arguments = parser.parse_args(argv)
    rendered = table()
    if not arguments.write:
        print(rendered)
        return 0
    readme = os.path.join(os.path.dirname(__file__) or ".", "README.md")
    with open(readme) as stream:
        text = stream.read()
    head, _, rest = text.partition(START)
    _, _, tail = rest.partition(END)
    if not rest or not tail:
        raise SystemExit(f"{readme}: results markers are missing")
    with open(readme, "w") as stream:
        stream.write(f"{head}{START}\n{rendered}\n{END}{tail}")
    print(f"wrote {readme}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
