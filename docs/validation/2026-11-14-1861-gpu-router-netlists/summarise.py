#!/usr/bin/env python3
"""Render the results table from reports/, so no digest is transcribed by hand.

  python3 summarise.py
"""
from __future__ import annotations

import collections
import glob
import json
import os

# What each core's producer closure picks up from this PR's diff.
CHANGED = {
    "fes.pong": "`build_fes_pong.py`, `fes_de10nano_evidence.py`",
    "fes.menu": "`build_fes_menu_package.py`, `build_fes_menu.py`, `fes_de10nano_evidence.py`",
    "fes.ramtest": "`build_fes_ramtest.py`, `fes_de10nano_evidence.py`",
    "fes.riscv": "`build_fes_riscv.py`, `fes_de10nano_evidence.py`",
}
HELPER_ONLY = "`fes_de10nano_evidence.py`"


def main() -> int:
    sides: dict[str, dict[str, dict]] = collections.defaultdict(dict)
    for path in sorted(glob.glob(os.path.join(os.path.dirname(__file__) or ".",
                                              "reports/*/netlist.json"))):
        record = json.load(open(path))
        side = "main" if os.path.basename(os.path.dirname(path)).endswith("-main") else "branch"
        sides[record["core"]][side] = record
    print("| Core | Changed files in its closure | Netlist sha256 | Match |")
    print("| --- | --- | --- | --- |")
    for core, found in sorted(sides.items()):
        main_record, branch = found.get("main"), found.get("branch")
        if not (main_record and branch):
            print(f"| `{core}` | {CHANGED.get(core, HELPER_ONLY)} | — | one side only |")
            continue
        same = main_record["netlist_sha256"] == branch["netlist_sha256"]
        shown = main_record["netlist_sha256"] if same else (
            f"main `{main_record['netlist_sha256'][:16]}…` vs "
            f"branch `{branch['netlist_sha256'][:16]}…`")
        verdict = "**byte-identical**" if same else "**DIFFERS**"
        cell = f"`{shown}`" if same else shown
        print(f"| `{core}` | {CHANGED.get(core, HELPER_ONLY)} | {cell} | {verdict} |")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
