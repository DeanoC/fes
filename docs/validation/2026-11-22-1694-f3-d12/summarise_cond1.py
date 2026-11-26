#!/usr/bin/env python3
"""Condition 1's table from /data/fes-f3-d12: each artefact's netlist digest
at base and head on both passes, the pinned-input digests that differ between
the sides (the reason BUILD_ID moved), and the verdict. Writes cond1.json."""
import hashlib, json, sys
from pathlib import Path

D = Path("/data/fes-f3-d12")
ROWS = ["c64", "apple2", "atari-st", "spectrum", "spectrum128", "spectrum-plus3", "pentagon",
        "zx81", "zx80", "ts1000", "ts1500", "pcxt-dos"]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def side(directory, name, s):
    folder = directory / f"{name}-{s}"
    netlist = folder / ("elaborated.v" if name == "pcxt-dos" else "synth.json")
    summary = json.loads((folder / "netlist.json").read_text())
    inputs = summary.get("pinned_inputs_sha256") or summary.get("inputs_sha256")
    return {"netlist_sha256": sha(netlist), "inputs_sha256": inputs, "revision": summary["revision"],
            "tree_clean": summary.get("tree_clean")}


rows = []
for name in ROWS:
    passes = {}
    for p in ("p1", "p2"):
        directory = D / f"cond1-{p}"
        passes[p] = {s: side(directory, name, s) for s in ("base", "head")}
    nets = {(p, s): passes[p][s]["netlist_sha256"] for p in passes for s in passes[p]}
    stable = nets[("p1", "base")] == nets[("p2", "base")] and nets[("p1", "head")] == nets[("p2", "head")]
    same = stable and nets[("p1", "base")] == nets[("p1", "head")]
    inputs_moved = passes["p1"]["base"]["inputs_sha256"] != passes["p1"]["head"]["inputs_sha256"]
    rows.append({"artefact": "fes." + name, "base": passes["p1"]["base"], "head": passes["p1"]["head"],
                 "passes_agree": stable, "inputs_moved": inputs_moved,
                 "verdict": "byte-identical" if same and inputs_moved else ("differs" if stable else "unstable")})
json.dump(rows, open(Path(__file__).with_name("cond1.json"), "w"), indent=2)
print("| Artefact | Base netlist | Head netlist | Inputs moved | Verdict |\n|---|---|---|---|---|")
for r in rows:
    print(f"| `{r['artefact']}` | `{r['base']['netlist_sha256'][:16]}` | `{r['head']['netlist_sha256'][:16]}` | "
          f"{'yes' if r['inputs_moved'] else 'no'} | {r['verdict']} |")
