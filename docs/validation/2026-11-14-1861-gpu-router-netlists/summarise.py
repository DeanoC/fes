#!/usr/bin/env python3
"""Render the results table from reports/, so no digest is transcribed by hand.

  python3 summarise.py            # print the table
  python3 summarise.py --write    # replace it between README.md's markers
"""
from __future__ import annotations

import argparse
import collections
import glob
import hashlib
import json
import os

START = "<!-- results:start -->"
END = "<!-- results:end -->"
HERE = os.path.dirname(os.path.abspath(__file__))

# D12 exempts src file paths: the checkout and the tool install directory. Only
# fes.splash needs it, because it refuses a shared cache root and so uses a
# local install per tree; every other artifact shares one install and matches
# byte for byte. netlist_digest.py records that digest per run, replacing the
# tree path and nothing else, so a non-path difference still shows up here.
def _exempt_src_paths(found):
    digests = {side: record.get("netlist_sha256_src_exempt") for side, record in found.items()}
    if not all(digests.values()):
        return None
    return digests["main"] if digests["main"] == digests["branch"] else None

# What each core's producer closure picks up from this PR's diff.
CHANGED = {
    "fes.pong": "`build_fes_pong.py`, `fes_de10nano_evidence.py`",
    "fes.menu": "`build_fes_menu_package.py`, `build_fes_menu.py`, `fes_de10nano_evidence.py`",
    "fes.ramtest": "`build_fes_ramtest.py`, `fes_de10nano_evidence.py`",
    "fes.riscv": "`build_fes_riscv.py`, `fes_de10nano_evidence.py`",
    "fes.splash": "`fes_de10nano_evidence.py`",
}
HELPER_ONLY = "`fes_de10nano_evidence.py`"


def table() -> str:
    sides: dict[str, dict[str, dict]] = collections.defaultdict(dict)
    for path in sorted(glob.glob(os.path.join(HERE, "reports/*/netlist.json"))):
        record = json.load(open(path))
        name = os.path.basename(os.path.dirname(path))
        side = "main" if name.endswith("-main") else "branch"
        sides[record["core"]][side] = record
    # Pass 2 lives beside the committed pass 1 and is required to agree with it:
    # DeanoC/yosys#77 has a presentation that exits 0 with a different netlist,
    # which only repetition detects.
    second: dict[str, dict[str, str]] = collections.defaultdict(dict)
    # Default to the committed pass-2 reports, so the table reproduces from a
    # fresh checkout. NETLIST_PASS2 points at a working directory mid-sweep.
    pass2 = os.environ.get("NETLIST_PASS2") or os.path.join(HERE, "reports-pass2")
    for path in sorted(glob.glob(os.path.join(pass2, "*/netlist.json"))):
        record = json.load(open(path))
        name = os.path.basename(os.path.dirname(path))
        side = "main" if name.endswith("-main") else "branch"
        second[record["core"]][side] = record["netlist_sha256"]
    rows = ["| Core | Changed files in its closure | Pinned inputs | Netlist sha256 "
            "| Two passes | Match |",
            "| --- | --- | --- | --- | --- | --- |"]
    for core, found in sorted(sides.items()):
        main_record, branch = found.get("main"), found.get("branch")
        if not (main_record and branch):
            rows.append(f"| `{core}` | {CHANGED.get(core, HELPER_ONLY)} | — | — | — "
                        "| one side only |")
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
        exempt = None if same else _exempt_src_paths(found)
        if same:
            shown, verdict = main_record["netlist_sha256"], "**byte-identical**"
        elif exempt:
            shown = f"{exempt} (after D12's `src` path exemption)"
            verdict = "**identical, src paths exempt**"
        else:
            shown = (f"main `{main_record['netlist_sha256'][:16]}…` vs "
                     f"branch `{branch['netlist_sha256'][:16]}…`")
            verdict = "**DIFFERS**"
        repeats = second.get(core, {})
        if len(repeats) < 2:
            passes, verdict = "pass 2 missing", "**unconfirmed**"
        elif all(repeats[side] == found[side]["netlist_sha256"] for side in ("main", "branch")):
            passes = "agree"
        else:
            passes, verdict = "**DISAGREE**", "**NOT REPRODUCIBLE**"
        cell = f"`{shown}`" if same else shown
        rows.append(f"| `{core}` | {CHANGED.get(core, HELPER_ONLY)} | {inputs} | {cell} "
                    f"| {passes} | {verdict} |")
    per_side: dict[str, set[str]] = {"main": set(), "branch": set()}
    for found in sides.values():
        for side, record in found.items():
            if record.get("revision"):
                per_side[side].add(record["revision"][:9])
    rows.append("")
    for side in ("main", "branch"):
        if per_side[side]:
            rows.append(f"Revisions measured on {side}: "
                        + ", ".join(f"`{r}`" for r in sorted(per_side[side])) + ".")
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
    readme = os.path.join(HERE, "README.md")
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
