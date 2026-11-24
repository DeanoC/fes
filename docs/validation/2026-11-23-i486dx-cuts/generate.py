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
FITTED = ("l1i2", "l1d2", "l12", "xshift", "xdisp", "x87", "cuts")
LOCKSTEP = ("l1i2", "l1d2", "l12", "xshift", "xdisp", "x87", "cuts", "qhalf")
X87_UNIT = ("base", "xshift", "xdisp", "x87", "cuts")
CPI_RUNS = ("l1i2", "l1d2", "l12", "qhalf", "cuts")
WHAT = {
    "l1i2": "L1I at 8 KiB: 2-way, 128 sets",
    "l1d2": "L1D at 8 KiB: 2-way, 128 sets",
    "l12": "`l1i2` + `l1d2`",
    "xshift": "the envelope's multiplier as a serial shifter",
    "xdisp": "the x87 core's `dispatch` register removed",
    "x87": "`xshift` + `xdisp`",
    "cuts": "`l12` + `xshift` + `xdisp` + round 1's `lfwd`",
    "qhalf": "Q issues at most once every two clocks (CPI bound only, never fitted)",
}
# Rule 3's sums: the combined fit against its parts (lfwd is round 1's seed-1 saving, method rule 3).
COMBINED = {"l12": ("l1i2", "l1d2"), "x87": ("xshift", "xdisp"), "cuts": ("l12", "xshift", "xdisp", "lfwd")}
LFWD_ROUND1 = 360
BUDGET = 1221           # method rule 5
DX_LOWEST_FMAX = 63.4   # method rule 6, as written there
# Kit A DOOM MAX.CFG realtics, the plan record's governor rule.
REALTICS = {"max": 2062, "486DX2-66 (7)": 2912, "486DX-33 (6)": 4682}


def load(path: str) -> dict:
    return json.loads((HERE / path).read_text())


def fit(name: str) -> dict:
    j = load(f"fits/{name}/fit.json")
    r = j["resources"]
    return {"alms": r["alms"]["used"], "dsp": r["dsp_blocks"]["used"], "m20k": r["ram_blocks"]["used"],
            "registers": r["registers"]["used"], "status": j["status"], "seed": j["seed"],
            "fmax": min(float(c["restricted_fmax"]) for x in j["timing"]["fmax"] for c in x["clocks"])}


def seeds(name: str) -> list[dict]:
    return [fit(f"{name}-s{s}") for s in (1, 2, 3)]


def render() -> str:
    dx = seeds("dx")
    dx_alms = [f["alms"] for f in dx]
    dx_median, dx_spread = statistics.median(dx_alms), max(dx_alms) - min(dx_alms)
    lock = load("lockstep.json")
    passed = {v: not lock[v]["new_failures"] for v in LOCKSTEP}
    unit = load("x87unit.json")
    unit_base = unit["base"]["summary"].split("[")[0]
    for v in X87_UNIT:
        passed[v] = passed.get(v, True) and unit[v]["rc"] == 0 and unit[v]["summary"].split("[")[0] == unit_base

    out = ["# Results (written by generate.py from the committed evidence; do not edit)", ""]
    out += ["## A. Area: `fes_i386_cpu` on Agilex 5 (A5EB013BB23BE4SCS, Quartus Pro 26.3, 61.44 MHz, virtual pins)", ""]
    out += ["`dx` is the plan record's three seeds, copied unchanged:", "",
            "| Seed | ALMs needed | DSP | M20K | Fit | Worst-corner Fmax estimate (MHz) |",
            "| ---: | ---: | ---: | ---: | --- | ---: |"]
    for f in dx:
        out.append(f"| {f['seed']} | {f['alms']:,.0f} | {f['dsp']:.0f} | {f['m20k']:.0f} | {f['status']} | "
                   f"{f['fmax']:.2f} |")
    out += ["", f"Median {dx_median:,.0f}, spread {dx_spread:,.0f}.", ""]
    out += ["| Variant | What it changes | ALMs, seeds 1 / 2 / 3 | Median | Spread | Band N | Saving (medians, "
            "rule 1) | Saving at seed 1 | Verdict | DSP Δ | M20K Δ | Fmax estimates (MHz) | Correctness (B) |",
            "| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: | --- | --- |"]
    savings = {"lfwd": LFWD_ROUND1}
    for v in FITTED:
        fs = seeds(v)
        alms = [f["alms"] for f in fs]
        med, spread = statistics.median(alms), max(alms) - min(alms)
        band = max(100.0, dx_spread, spread)
        saving = dx_median - med
        savings[v] = saving
        if any(f["status"] != "pass" for f in fs):
            verdict = "fit failed (rule 7)"
        elif not passed[v]:
            verdict = "not valid: fails section B"
        elif abs(saving) <= band:
            verdict = "under noise"
        else:
            verdict = "saving" if saving > 0 else "growth"
        out.append(f"| `{v}` | {WHAT[v]} | {' / '.join(f'{a:,.0f}' for a in alms)} | {med:,.0f} | {spread:,.0f} | "
                   f"{band:,.0f} | {saving:+,.0f} | {dx[0]['alms'] - alms[0]:+,.0f} | {verdict} | "
                   f"{fs[0]['dsp'] - dx[0]['dsp']:+.0f} | {fs[0]['m20k'] - dx[0]['m20k']:+.0f} | "
                   f"{' / '.join(f'{f['fmax']:.2f}' for f in fs)} | {'passes' if passed[v] else 'FAILS'} |")
    out += ["", "DSP and M20K deltas are the same at every seed of a variant (one netlist); they are not converted "
            "into ALMs (rule 4).", ""]
    out += ["**Rule 3, combined fits against their parts:**", ""]
    for whole, parts in COMBINED.items():
        total = sum(savings[p] for p in parts)
        label = " + ".join(f"`{p}` {savings[p]:+,.0f}" + (" (round 1)" if p == "lfwd" else "") for p in parts)
        out.append(f"- `{whole}` saves {savings[whole]:+,.0f}; {label} sum to {total:+,.0f} "
                   f"(difference {savings[whole] - total:+,.0f}).")
    cuts = savings["cuts"]
    out += ["", f"**Rule 5, the budget:** `cuts` saves {cuts:,.0f} against the plan's {BUDGET:,} ALMs over the device; "
            + (f"it closes the gap first-order, with {cuts - BUDGET:,.0f} to spare. This is a CPU-envelope saving "
               "set against machine figures, not a fit of the DX." if cuts >= BUDGET else
               f"{BUDGET - cuts:,.0f} ALMs of the gap remain."), ""]
    xd = seeds("xdisp")
    low = [f"{f['fmax']:.2f}" for f in xd if f["fmax"] < DX_LOWEST_FMAX]
    out += [f"**Rule 6, `xdisp`'s timing:** its estimates are {', '.join(f'{f['fmax']:.2f}' for f in xd)} MHz "
            f"against `dx`'s lowest, {DX_LOWEST_FMAX} MHz: "
            + (f"{', '.join(low)} below it, called out." if low else "none below it.")
            + " Timing is reported, not judged.", ""]

    out += ["## B. Correctness", "", "### Lockstep (`--random-programs 500 --random-seeds 8 --limit-per-file 20`)", "",
            "| Variant | Exit | New failures beyond `base` | Verdict |", "| --- | ---: | --- | --- |",
            f"| `base` | {lock['base']['rc']} | (the plan record's run; failures: "
            f"{len(lock['base']['failures'])}) | reference |"]
    for v in LOCKSTEP:
        new = lock[v]["new_failures"]
        out.append(f"| `{v}` | {lock[v]['rc']} | {'; '.join(new) if new else 'none'} | "
                   f"{'passes' if not new else 'FAILS (rule B)'} |")
    out += ["", "### The x87 unit bench (`scripts/test_fes_i386_x87_unit.py --download`)", "",
            "| Build | Exit | Summary |", "| --- | ---: | --- |"]
    for v in X87_UNIT:
        out.append(f"| `{v}` | {unit[v]['rc']} | {unit[v]['summary']} |")
    out += ["", "Rule B compares the pass and fail counts (the text before the bracket). The bracket's FBSTP "
            "\"converted\" count differs between runs of the bench.", ""]

    runs = load("runs.json")
    out += ["## C. CPI", "",
            "The bench charges port 0's first data 18 clocks against the kit's 11, so the L1 variants' losses "
            "are upper bounds (method, section C).", ""]
    out += ["### C1. DOOM v1.9 `-timedemo demo3`, handover at page flip 40, window h2", ""]
    out += _cpi_table("doom", ("base",) + CPI_RUNS, passed, savings, h2=True)
    out += ["", "**The governor bound** (2,062 × r against the presets' realtics):", "",
            "| Variant | r | Bound | DX2-66 (2,912) | DX-33 (4,682) |", "| --- | ---: | ---: | --- | --- |"]
    b2 = load("profiles/doom-base-h2.json")
    for v in CPI_RUNS:
        r = load(f"profiles/doom-{v}-h2.json")["cpi"] / b2["cpi"]
        bound = REALTICS["max"] * r
        cells = ["kept by the bound" if bound <= REALTICS[p] else "not settled by the bound"
                 for p in ("486DX2-66 (7)", "486DX-33 (6)")]
        out.append(f"| `{v}` | {r:.3f} | {bound:,.0f} | {cells[0]} | {cells[1]} |")
    out += ["", "The x87 variants (`xshift`, `xdisp`, `x87`): not applicable (DOOM issues no x87 instruction).", ""]
    env = dict(l.split("=", 1) for l in (HERE / "calibration/t3d.env").read_text().splitlines()
               if "=" in l and not l.startswith("#"))
    lo, hi = float(env["T3D_FROM"]) + 0.5, float(env["T3D_LIMIT"])
    out += [f"### C2. 3DBench 1.0, RTL from {env['T3D_FROM']} s, window {lo:.1f}–{hi:.1f} s (amendment 1)", ""]
    out += _cpi_table("3d", ("base",) + CPI_RUNS, passed, savings, h2=False)
    same = [v for v in CPI_RUNS if v != "l12" and load(f"profiles/3d-{v}.json") == load("profiles/3d-l12.json")]
    if same:
        out += ["", f"{', '.join(f'`{v}`' for v in same)}'s 3DBench window is identical to `l12`'s in every "
                "counter: in this real-mode window, load forwarding and the x87 change nothing.", ""]
    out += ["Every 3DBench run ends at its `--limit-seconds` (`END limit`, which `machine_tb` exits 1 for); "
            "the handover and end lines are in [runs.json](runs.json).", ""]
    q = runs["quake-base"]
    stop = next((l for l in q if l.startswith("STOP")), None)
    out += ["### C3. Quake bring-up", "",
            f"`base`'s attempt: {'; '.join(f'`{l}`' for l in q)}; the runner's status `{' '.join(runs['quake-base-status'])}`.",
            ""]
    if stop:
        out += [f"The RTL stopped before the window (`{stop}`), so the bring-up failed and **every Quake cell is "
                "not measurable** (method C3). `xshift`, `xdisp`, `x87` and `cuts` were not run on Quake.", ""]
    out += ["### C4. Latency bench (clocks per loop; RTL alone at max)", ""]
    names = ("base",) + LOCKSTEP
    lat = {n: {l["name"]: l["cycles"] for l in load(f"latency/{n}.json")["loops"]} for n in names}
    out += ["| Loop | " + " | ".join(f"`{n}`" for n in names) + " |",
            "| --- | " + " | ".join("---:" for _ in names) + " |"]
    for loop in lat["base"]:
        out.append(f"| {loop} | " + " | ".join(f"{lat[n][loop]:,}" for n in names) + " |")
    out.append("")
    return "\n".join(out)


def _cpi_table(bench: str, names: tuple, passed: dict, savings: dict, h2: bool) -> list[str]:
    def prof(v: str) -> dict:
        return load(f"profiles/{bench}-{v}{'-h2' if h2 else ''}.json")
    base = prof("base")
    rows = ["| Variant | Window start cycle | Clocks | Retired | CPI | CPI / `base` | Saving (ALMs) | "
            "CPI loss per 100 ALMs saved | Section B |",
            "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |"]
    for v in names:
        p = prof(v)
        comparable = p["from_cycle"] == base["from_cycle"]
        r = p["cpi"] / base["cpi"]
        s = savings.get(v)
        per = "—" if not s or s <= 0 or v == "base" else f"{100 * (r - 1) / (s / 100):.2f}%"
        rows.append(f"| `{v}` | {p['from_cycle']:,}{'' if comparable else ' (not comparable)'} | {p['cycles']:,} | "
                    f"{p['retired']:,} | {p['cpi']:.3f} | {r:.3f} | {'—' if s is None else f'{s:+,.0f}'} | {per} | "
                    f"{'reference' if v == 'base' else ('passes' if passed[v] else 'FAILS lockstep (rule B)')} |")
    return rows


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    text = render()
    target = HERE / "results.md"
    if args.check:
        if not target.is_file() or target.read_text() != text:
            print("results.md differs from what generate.py writes", file=sys.stderr)
            return 1
        return 0
    target.write_text(text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
