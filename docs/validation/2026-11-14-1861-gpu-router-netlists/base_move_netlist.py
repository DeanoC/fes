#!/usr/bin/env python3
"""Is main's own netlist byte-identical at the old evidence base and the new one?

D12's merge-forward clause has a second limb: "A core whose compiled source set
did change still keeps its recorded comparison where main's own netlist is
byte-identical at the old base and the new one, apart from the BUILD_ID
constant and the paths this rule already exempts." Both sides here are *main*,
and both were synthesised with BUILD_ID pinned to 32 zeros, so the constant is
already equal and nothing is masked out afterwards.

This is what decides whether the recorded condition-2 routes for fes.menu,
fes.c64 and fes.spectrum carry across the base move or have to be re-run. It
reads the old-base digest from the committed record and the new-base digest
from the current condition-1 pass; it never recomputes the old one, because the
committed digest is the evidence.

  python3 base_move_netlist.py --record <record dir> --new <cond1 pass dir>
"""
from __future__ import annotations
import argparse, json
from pathlib import Path

ARTEFACTS = ("menu", "splash", "pong", "ramtest", "riscv", "coleco", "c64",
             "spectrum", "spectrum128", "spectrum-plus3", "pentagon")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--record", type=Path, required=True)
    parser.add_argument("--new", type=Path, required=True)
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    rows, pending = {}, []
    print(f'{"artefact":18} {"old base":20} {"new base":20} verdict')
    for art in ARTEFACTS:
        old_p = arguments.record / "reports" / f"{art}-main" / "netlist.json"
        new_p = arguments.new / f"{art}-idbase" / "netlist.json"
        if not old_p.is_file():
            print(f'{art:18} {"-":20} {"-":20} no old-base record')
            rows[art] = {"verdict": "no old-base record"}
            continue
        if not new_p.is_file():
            print(f'{art:18} {"-":20} {"-":20} new base not yet measured')
            pending.append(art)
            continue
        o, n = json.loads(old_p.read_text()), json.loads(new_p.read_text())
        # The exemption digest was added to this harness after the old pass, so
        # an old record may not carry it. Fall back to the plain digest rather
        # than treating a missing field as agreement.
        ox = o.get("netlist_sha256_src_exempt", o["netlist_sha256"])
        nx = n.get("netlist_sha256_src_exempt", n["netlist_sha256"])
        if o["netlist_sha256"] == n["netlist_sha256"]:
            verdict = "identical"
        elif ox == nx:
            verdict = "identical (src exempt)"
        else:
            verdict = "MOVED -- recorded comparison does not carry"
        rows[art] = {"verdict": verdict,
                     "old_revision": o["revision"], "new_revision": n["revision"],
                     "old_netlist_sha256": o["netlist_sha256"],
                     "new_netlist_sha256": n["netlist_sha256"],
                     "old_pinned": o["pinned_inputs_sha256"],
                     "new_pinned": n["pinned_inputs_sha256"],
                     "pinned_moved": o["pinned_inputs_sha256"] != n["pinned_inputs_sha256"]}
        print(f'{art:18} {o["netlist_sha256"][:18]:20} {n["netlist_sha256"][:18]:20} {verdict}'
              f'   [pinned {"moved" if rows[art]["pinned_moved"] else "same"}]')
    moved = [a for a, r in rows.items() if r["verdict"].startswith("MOVED")]
    print()
    print(f'{len(moved)} artefact(s) whose recorded comparison does not carry'
          + (f": {', '.join(moved)}" if moved else ""))
    if pending:
        print("pending at the new base: " + ", ".join(pending))
    if arguments.json:
        arguments.json.write_text(json.dumps({"rows": rows, "moved": moved,
                                              "pending": pending},
                                             indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json}")
    return 1 if (moved or pending) else 0


if __name__ == "__main__":
    raise SystemExit(main())
