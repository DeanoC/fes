#!/usr/bin/env python3
"""Render condition 1 at the new base: two agreeing passes, or no claim.

DeanoC/yosys#77 at 3a5accbd has two intermittent presentations. One segfaults
in OPT_CLEAN, which is loud. The other exits 0 and writes a materially
different netlist, which is silent -- so a single pass can report a byte
difference that is the compiler, or hide one that is real. Every artefact is
therefore synthesised twice per side, and a row is only claimed where both
passes agree on both sides.

  python3 summarise_cond1.py [--pass1 DIR] [--pass2 DIR] [--json OUT]
"""
from __future__ import annotations
import argparse, json
from pathlib import Path

HERE = Path(__file__).resolve().parent
ARTEFACTS = ("menu", "splash", "pong", "ramtest", "riscv", "coleco", "c64",
             "spectrum", "spectrum128", "spectrum-plus3", "pentagon")


def load(directory: Path, artefact: str, side: str):
    path = directory / f"{artefact}-{side}" / "netlist.json"
    return json.loads(path.read_text()) if path.is_file() else None


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--pass1", type=Path, default=HERE.parent / "cond1-p1")
    parser.add_argument("--pass2", type=Path, default=HERE.parent / "cond1-p2")
    parser.add_argument("--json", type=Path)
    arguments = parser.parse_args()
    rows, incomplete = {}, []
    print(f'{"artefact":18} {"pass 1":22} {"pass 2":22} verdict')
    for artefact in ARTEFACTS:
        got = {(p, s): load(getattr(arguments, f"pass{p}"), artefact, f"id{s}")
               for p in (1, 2) for s in ("base", "head")}
        if any(v is None for v in got.values()):
            missing = [f"pass{p}/{s}" for (p, s), v in got.items() if v is None]
            incomplete.append(f"{artefact}: {', '.join(missing)}")
            print(f"{artefact:18} {'-':22} {'-':22} NOT MEASURED ({', '.join(missing)})")
            continue
        # Prefer the plain digest; fall back to D12's src exemption and say so.
        verdicts, detail = [], {}
        for p in (1, 2):
            b, h = got[(p, "base")], got[(p, "head")]
            if b["netlist_sha256"] == h["netlist_sha256"]:
                verdicts.append("identical")
            elif b["netlist_sha256_src_exempt"] == h["netlist_sha256_src_exempt"]:
                verdicts.append("identical (src exempt)")
            else:
                verdicts.append("DIFFERS")
            detail[f"pass{p}"] = {
                "base_sha256": b["netlist_sha256"], "head_sha256": h["netlist_sha256"],
                "base_src_exempt": b["netlist_sha256_src_exempt"],
                "head_src_exempt": h["netlist_sha256_src_exempt"],
                "base_revision": b["revision"], "head_revision": h["revision"],
                "base_pinned": b["pinned_inputs_sha256"], "head_pinned": h["pinned_inputs_sha256"],
                "tree_clean": b["tree_clean"] and h["tree_clean"],
                "verdict": verdicts[-1],
            }
        agree = verdicts[0] == verdicts[1]
        # The pinned-input contrast is the argument: the inputs moved, the netlist did not.
        inputs_moved = detail["pass1"]["base_pinned"] != detail["pass1"]["head_pinned"]
        verdict = (verdicts[0] if agree else
                   f"PASSES DISAGREE ({verdicts[0]} / {verdicts[1]})")
        rows[artefact] = {"verdict": verdict, "passes_agree": agree,
                          "pinned_inputs_moved": inputs_moved, **detail}
        print(f"{artefact:18} {verdicts[0]:22} {verdicts[1]:22} "
              f"{verdict}{'' if inputs_moved else '  [WARNING: pinned inputs did not move]'}")
    claimed = [a for a, r in rows.items() if r["passes_agree"] and "DIFFERS" not in r["verdict"]]
    print()
    print(f"{len(claimed)} of {len(ARTEFACTS)} artefacts claimed on two agreeing passes")
    if incomplete:
        print("not measured: " + "; ".join(incomplete))
    if arguments.json:
        arguments.json.write_text(json.dumps(
            {"rows": rows, "claimed": sorted(claimed), "incomplete": incomplete},
            indent=2, sort_keys=True) + "\n")
        print(f"wrote {arguments.json}")
    return 0 if len(claimed) == len(ARTEFACTS) else 1


if __name__ == "__main__":
    raise SystemExit(main())
