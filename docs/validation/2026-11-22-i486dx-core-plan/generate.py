#!/usr/bin/env python3
"""Write results.md from this record's committed evidence, applying method.md's rules.

  python3 generate.py            # writes results.md beside this script
  python3 generate.py --check    # fails if results.md is not what the evidence gives
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import statistics
import sys

HERE = Path(__file__).resolve().parent
FUNCTIONAL = ("pred", "ras", "ind", "stld", "ealu", "lfwd", "speed")
WHAT = {
    "386": "`I486 = 0`: the 386 identity (scale, not an ablation)",
    "pred": "all prediction off: BTB #1511, RAS #1512, indirect #1571",
    "ras": "return-stack prediction off (lower bound)",
    "ind": "indirect prediction off (lower bound)",
    "stld": "store-to-load overlap test #1515 off",
    "ealu": "early ALU at AG #1517 off",
    "lfwd": "load forwarding into an address #1522 off",
    "speed": "`pred` + `stld` + `ealu` + `lfwd` together",
    "pg": "paging off (area bound only, not functional)",
}
SPEED_PARTS = ("pred", "stld", "ealu", "lfwd")
# Kit A DOOM MAX.CFG realtics, docs/validation/2026-11-19-pc386-local-bus-video/summary.md:133-138.
REALTICS = {"max": 2062, "486DX2-66 (7)": 2912, "486DX-33 (6)": 4682}


def fit(name: str) -> dict:
    j = json.loads((HERE / "fits" / name / "fit.json").read_text())
    r = j["resources"]
    fmax = min(float(c["restricted_fmax"]) for x in j["timing"]["fmax"] for c in x["clocks"])
    return {"alms": r["alms"]["used"], "registers": r["registers"]["used"], "dsp": r["dsp_blocks"]["used"],
            "m20k": r["ram_blocks"]["used"], "vpin_alms": r["alms_with_virtual_pins"]["used"],
            "status": j["status"], "fmax": fmax, "seed": j["seed"]}


def profile(variant: str, half: str) -> dict | None:
    path = HERE / "profiles" / f"{variant}-{half}.json"
    return json.loads(path.read_text()) if path.is_file() else None


def latency(variant: str) -> dict | None:
    path = HERE / "latency" / f"{variant}.json"
    if not path.is_file():
        return None
    return {loop["name"]: loop["cycles"] for loop in json.loads(path.read_text())["loops"]}


def render() -> str:
    dx = [fit(f"dx-s{s}") for s in (1, 2, 3)]
    dx_alms = [f["alms"] for f in dx]
    spread = max(dx_alms) - min(dx_alms)
    band = max(spread, 100.0)
    median = statistics.median(dx_alms)
    base = dx[0]["alms"]
    out = ["# Results (written by generate.py from the committed evidence; do not edit)", ""]
    out += ["## A. Area: `fes_i386_cpu` on Agilex 5 (A5EB013BB23BE4SCS, Quartus Pro 26.3, 61.44 MHz, virtual pins)", ""]
    out += ["`dx` (main `dafabb6db` + `I486 = 1`, 387 alive), three seeds of one netlist:", ""]
    out += ["| Seed | ALMs needed | Registers | DSP | M20K | Fit | Worst-corner Fmax estimate (MHz) |",
            "| ---: | ---: | ---: | ---: | ---: | --- | ---: |"]
    for f in dx:
        out.append(f"| {f['seed']} | {f['alms']:,.0f} | {f['registers']:,.0f} | {f['dsp']:.0f} | {f['m20k']:.0f} | "
                   f"{f['status']} | {f['fmax']:.2f} |")
    out += ["", f"Median {median:,.0f}, spread {spread:,.0f}; noise band N = max(spread, 100) = **{band:,.0f} ALMs** "
            "(method rule 1). Every fit's ALMs include "
            f"{dx[0]['vpin_alms']:,.0f} ALMs that hold virtual pins (the envelope's, not the CPU's); they are the "
            "same in every fit and cancel in a saving.", ""]
    out += ["| Variant | What is off | ALMs needed (seed 1) | Saving vs `dx` seed 1 | Verdict | Registers | M20K | "
            "Worst-corner Fmax estimate (MHz) |",
            "| --- | --- | ---: | ---: | --- | ---: | ---: | ---: |"]
    savings = {}
    for name in ("386", "pred", "ras", "ind", "stld", "ealu", "lfwd", "speed", "pg"):
        f = fit(f"{name}-s1")
        saving = base - f["alms"]
        savings[name] = saving
        verdict = "under noise" if abs(saving) <= band else ("saving" if saving > 0 else "growth")
        if name == "386":
            verdict = "scale: the 486 identity's cost" if abs(saving) > band else "scale: under noise"
        if name == "pg":
            verdict += "; bound only, never a candidate"
        out.append(f"| `{name}` | {WHAT[name]} | {f['alms']:,.0f} | {saving:+,.0f} | {verdict} | "
                   f"{f['registers']:,.0f} | {f['m20k']:.0f} | {f['fmax']:.2f} |")
    parts = sum(savings[p] for p in SPEED_PARTS)
    out += ["", f"Rule 4: `speed` saves {savings['speed']:,.0f}; its parts' single savings sum to {parts:,.0f} "
            f"(difference {savings['speed'] - parts:+,.0f}).", ""]

    out += ["## B. Lockstep of each functional ablation", ""]
    out += ["| Variant | Lockstep subset | Failures beyond `base` |", "| --- | --- | ---: |"]
    lock = json.loads((HERE / "lockstep.json").read_text()) if (HERE / "lockstep.json").is_file() else {}
    for name in ("base",) + FUNCTIONAL:
        row = lock.get(name)
        out.append(f"| `{name}` | {row['verdict'] if row else 'not run'} | "
                   f"{row['new_failures'] if row else '—'} |")
    out.append("")

    out += ["## C. CPI: DOOM v1.9 `-timedemo demo3`, handover at page flip 40, RTL for 1.0 s", ""]
    b2 = profile("base", "h2")
    if b2 is None:
        out += ["No profile window has been collected yet.", ""]
    else:
        out += ["| Variant | Handover cycle | h1 CPI | h2 CPI | h2 CPI / `base` | h2 retired | Saving (ALMs) | "
                "h2 CPI loss per 100 ALMs saved |",
                "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |"]
        ratios = {}
        for name in ("base",) + FUNCTIONAL:
            h1, h2 = profile(name, "h1"), profile(name, "h2")
            if h2 is None:
                out.append(f"| `{name}` | — | — | — | not run | — | — | — |")
                continue
            same = h2["from_cycle"] - (h1["from_cycle"] if h1 else 0)
            comparable = h1 is not None and h1["from_cycle"] == profile("base", "h1")["from_cycle"]
            r = h2["cpi"] / b2["cpi"]
            ratios[name] = r if comparable else None
            s = savings.get(name)
            per = "—" if not s or s <= band else f"{100 * (r - 1) / (s / 100):.2f}%"
            out.append(f"| `{name}` | {h1['from_cycle'] if h1 else '—'}{'' if comparable else ' (not comparable)'} | "
                       f"{h1['cpi'] if h1 else float('nan'):.3f} | {h2['cpi']:.3f} | {r:.3f} | {h2['retired']:,} | "
                       f"{'—' if s is None else f'{s:+,.0f}'} | {per} |")
        out += ["", "### The governor bound (method, question 3)", "",
                "| Variant | h2 CPI ratio r | `max` realtics bound 2,062 × r | DX2-66 (2,912) | DX-33 (4,682) |",
                "| --- | ---: | ---: | --- | --- |"]
        for name, r in ratios.items():
            if r is None:
                continue
            bound = REALTICS["max"] * r
            cells = ["kept by the bound" if bound <= REALTICS[p] else "not settled by the bound"
                     for p in ("486DX2-66 (7)", "486DX-33 (6)")]
            out.append(f"| `{name}` | {r:.3f} | {bound:,.0f} | {cells[0]} | {cells[1]} |")
        out.append("")
    lb = latency("base")
    if lb:
        out += ["## Latency bench (clocks per loop; RTL alone at max)", ""]
        names = ("base",) + FUNCTIONAL
        have = [n for n in names if latency(n)]
        out += ["| Loop | " + " | ".join(f"`{n}`" for n in have) + " |",
                "| --- | " + " | ".join("---:" for _ in have) + " |"]
        for loop in lb:
            out.append(f"| {loop} | " + " | ".join(f"{latency(n)[loop]:,}" for n in have) + " |")
        out.append("")
    return "\n".join(out)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    text = render()
    target = HERE / "results.md"
    if args.check:
        if target.read_text() != text:
            print("results.md differs from what generate.py writes", file=sys.stderr)
            return 1
        return 0
    target.write_text(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
