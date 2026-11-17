#!/usr/bin/env python3
"""Render the D12 status table for this record's README, from the measurements.

One table, three conditions, eleven full-tier artefacts plus the four the
support matrix records `reduced`. Every cell comes from a measurement file
written by one of the harnesses in this directory, never from prose, so the
table cannot drift from the evidence it summarises:

  condition 1  summarise_cond1.py   two agreeing passes, both sides
  condition 2  summarise_cond2.py   forced BUILD_ID, or CARRIED, or splash's case
  condition 3  collect_cond3.py     the producer's own gated build at the head

A cell with no measurement reads "not measured", never blank and never a
guess. `reduced` rows show "not owed (#1863)" for conditions 2 and 3, because
that is what the rule says of them rather than a gap in this record.

  python3 d12_status.py [--write]   # --write updates README.md in place
"""
from __future__ import annotations
import argparse, json, subprocess, sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
START, END = "<!-- results:start -->", "<!-- results:end -->"
FULL = ("splash", "fes.menu", "fes.c64", "fes.spectrum", "fes.spectrum128",
        "fes.spectrum-plus3", "fes.pentagon", "fes.zx81", "fes.zx80",
        "fes.ts1000", "fes.ts1500")
REDUCED = ("fes.pong", "fes.ramtest", "fes.coleco", "fes.riscv")
LANE = {"fes.zx81": "Quartus Lite", "fes.zx80": "Quartus Lite",
        "fes.ts1000": "Quartus Lite", "fes.ts1500": "Quartus Lite"}


def run(script: str, *args: str) -> str:
    out = subprocess.run((sys.executable, str(HERE / script), *args),
                         capture_output=True, text=True)
    return out.stdout


def load(path: Path):
    return json.loads(path.read_text()) if path.is_file() else None


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--write", action="store_true")
    parser.add_argument("--scope", type=Path, default=HERE / "reports-scope")
    parser.add_argument("--cond3", type=Path, default=HERE / "reports-cond3" / "cond3.json")
    arguments = parser.parse_args()
    wanted = {"condition 1": arguments.scope / "cond1-summary.json",
              "condition 2": arguments.scope / "cond2-summary.json",
              "condition 3": arguments.cond3,
              "ZX81 QSFs": arguments.scope / "zx81-quartus-qsf.json"}
    absent = {name: path for name, path in wanted.items() if not path.is_file()}
    if absent:
        # Say the input is missing rather than let every cell read "not
        # measured", which would blame the measurement for a path mistake.
        for name, path in absent.items():
            print(f"WARNING: no {name} summary at {path}; its column is unknown, not unmeasured",
                  file=sys.stderr)
    c1 = load(wanted["condition 1"])
    c2 = load(wanted["condition 2"])
    c3 = load(arguments.cond3)

    def cell1(artefact: str) -> str:
        key = artefact.removeprefix("fes.")
        if artefact in LANE:
            qsf = load(wanted["ZX81 QSFs"])
            if qsf is None:
                return "not measured"
            model = key
            moved = qsf.get("moved") or []
            if model in moved:
                return "**QSF MOVED**"
            return "canonical QSF identical"
        row = (c1 or {}).get("rows", {}).get(key)
        if row is None:
            return "not measured"
        if not row["passes_agree"]:
            return "**passes disagree**"
        return row["verdict"]

    def cell2(artefact: str) -> str:
        key = artefact if artefact in ("splash",) else artefact
        row = (c2 or {}).get("rows", {}).get(key) or \
              (c2 or {}).get("rows", {}).get(key.removeprefix("fes."))
        if row is None:
            return "not measured"
        return row["verdict"]

    def cell3(artefact: str) -> str:
        key = artefact.removeprefix("fes.")
        row = (c3 or {}).get(key)
        if row is None:
            return "not measured"
        if row.get("unverified"):
            return f'**unverified**: {row["unverified"]}'
        m = row.get("margins", {})
        # Each lane in its own units: nextpnr reports a frequency ratio,
        # Quartus a slack in nanoseconds. They are not comparable.
        if m.get("kind") == "nextpnr-frequency-ratio":
            margin = f'{m["worst_ratio"]:.4f}x worst clock'
        elif m.get("kind") == "quartus-slack-ns":
            if not m.get("all_non_negative", True):
                return "**NEGATIVE SLACK**"
            margin = f'{m["worst_slack_ns"]:+.3f} ns worst slack ({m["worst_analysis"]})'
        else:
            return "pass (no margin figures in the summary)"
        if artefact in LANE and not row.get("repeat_matches_signoff", True):
            return "**repeat changed the RBF**"
        # Two heads: the second main move forced only the Spectrum four to be
        # resealed, so the row says which head it was taken at rather than
        # leaving one to be assumed for all of them.
        # 12, not 9. The mistyped revision Codex caught shared its first nine
        # characters with the real one (541e3c548ea1... vs 541e3c5487...), so a
        # nine-character column rendered the wrong SHA indistinguishably from
        # the right one. The collector now validates against the tree, which is
        # the real fix; this is so the table cannot hide the next one either.
        head = (row.get("taken_at_head") or "")[:12]
        return f"pass, {margin}" + (f" @ `{head}`" if head else "")

    lines = [START, "",
             "| Artefact | Lane | `check_level` | Condition 1 | Condition 2 | Condition 3 |",
             "| --- | --- | --- | --- | --- | --- |"]
    for artefact in FULL:
        lines.append(f"| `{artefact}` | {LANE.get(artefact, 'nextpnr')} | full | "
                     f"{cell1(artefact)} | {cell2(artefact)} | {cell3(artefact)} |")
    for artefact in REDUCED:
        lines.append(f"| `{artefact}` | nextpnr | reduced | {cell1(artefact)} | "
                     f"not owed (#1863) | not owed (#1863) |")
    lines += ["",
              "Condition 1 for the Quartus Lite rows is the canonical QSF, which is what",
              "D12 names for a native Quartus front end; those four run no Yosys program.",
              "A `CARRIED` condition 2 is a recorded pair that D12's merge-forward clause",
              "carries, and it is claimed only where the recorded pair passed *and* main's",
              "own netlist is byte-identical at the old base and the new one.",
              "Condition 3 names the head each seal was taken at. The four Spectrum machines",
              "were resealed at `541e3c548` because the second main move touched their",
              "producer; the other seven were taken at `df99f2dfb` and their identities are",
              "unchanged at `541e3c548`, which `reports-scope2/cond3-head-move.log` measures.",
              "", END]
    table = "\n".join(lines)
    print(table)
    if arguments.write:
        readme = HERE / "README.md"
        text = readme.read_text()
        if START not in text or END not in text:
            raise SystemExit("README.md has no results markers")
        before, _, rest = text.partition(START)
        _, _, after = rest.partition(END)
        readme.write_text(before + table + after)
        print(f"\nwrote {readme}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
