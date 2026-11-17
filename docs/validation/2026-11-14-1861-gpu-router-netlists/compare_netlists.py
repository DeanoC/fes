#!/usr/bin/env python3
"""Compare two sets of netlist digests, naming both sides and refusing gaps.

Two questions in this record have the same shape, so they share one program
rather than two near-copies that can drift apart:

  branch vs base at one revision   -- D12 condition 1
  main vs main across two bases    -- D12's merge-forward second limb, which
                                      carries a recorded comparison for a core
                                      whose compiled source set changed if
                                      main's own netlist is byte-identical at
                                      the old base and the new one

Both sides are synthesised at BUILD_ID 0*32 by netlist_digest.py, so the
constant is already equal and nothing is normalised away afterwards; the `src`
exemption is applied only where the plain digests differ, and is reported.

  python3 compare_netlists.py --a DIR --a-side nb2 --b DIR --b-side nh2 \
      --artefacts ramtest spectrum ... [--json OUT]
"""
from __future__ import annotations
import argparse, json
from pathlib import Path


def load(directory: Path, artefact: str, side: str):
    path = directory / f"{artefact}-{side}" / "netlist.json"
    return json.loads(path.read_text()) if path.is_file() else None


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--a", type=Path, required=True)
    parser.add_argument("--a-side", required=True)
    parser.add_argument("--b", type=Path, required=True)
    parser.add_argument("--b-side", required=True)
    parser.add_argument("--artefacts", nargs="+", required=True)
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    if arguments.json is not None:
        arguments.json = arguments.json.resolve()
    rows, missing, moved = {}, [], []
    print(f'{"artefact":18} {arguments.a_side:>20} {arguments.b_side:>20}  verdict')
    for artefact in arguments.artefacts:
        a = load(arguments.a, artefact, arguments.a_side)
        b = load(arguments.b, artefact, arguments.b_side)
        if a is None or b is None:
            absent = [s for s, v in ((arguments.a_side, a), (arguments.b_side, b)) if v is None]
            missing.append(f"{artefact}: {', '.join(absent)}")
            print(f'{artefact:18} {"-":>20} {"-":>20}  NOT MEASURED ({", ".join(absent)})')
            continue
        if a["netlist_sha256"] == b["netlist_sha256"]:
            verdict = "identical"
        elif a.get("netlist_sha256_src_exempt") == b.get("netlist_sha256_src_exempt"):
            verdict = "identical (src exempt)"
        else:
            verdict = "MOVED"
            moved.append(artefact)
        rows[artefact] = {
            "verdict": verdict,
            f"{arguments.a_side}_sha256": a["netlist_sha256"],
            f"{arguments.b_side}_sha256": b["netlist_sha256"],
            f"{arguments.a_side}_revision": a["revision"],
            f"{arguments.b_side}_revision": b["revision"],
            "pinned_inputs_moved": a["pinned_inputs_sha256"] != b["pinned_inputs_sha256"],
            "both_trees_clean": a["tree_clean"] and b["tree_clean"],
        }
        print(f'{artefact:18} {a["netlist_sha256"][:18]:>20} {b["netlist_sha256"][:18]:>20}  '
              f'{verdict}  [pinned {"moved" if rows[artefact]["pinned_inputs_moved"] else "same"}]')
    print()
    print(f"{len(rows) - len(moved)} identical, {len(moved)} moved, {len(missing)} not measured")
    if moved:
        print("moved: " + ", ".join(moved))
    if missing:
        print("not measured: " + "; ".join(missing))
    if arguments.json:
        arguments.json.write_text(json.dumps(
            {"a": {"dir": str(arguments.a), "side": arguments.a_side},
             "b": {"dir": str(arguments.b), "side": arguments.b_side},
             "rows": rows, "moved": moved, "not_measured": missing},
            indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json}")
    return 1 if (moved or missing) else 0


if __name__ == "__main__":
    raise SystemExit(main())
