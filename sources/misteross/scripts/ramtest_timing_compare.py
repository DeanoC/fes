#!/usr/bin/env python3
"""Summarize saved RAM-test timing paths without changing RTL or running tools.

Quartus data delay and nextpnr path totals use different accounting. Compare
setup relationship minus slack, retain the raw components, and resolve logical
register bits through the netlist rather than generated cell-name suffixes.
This is diagnostic reporting, not the package timing acceptance gate.
"""
from __future__ import annotations

import argparse
from collections import defaultdict
import hashlib
import json
from pathlib import Path
import re


def register_signals(module: dict, cell: str) -> list[str]:
    bits = module["cells"][cell]["connections"]["Q"]
    names = []
    for name, net in module["netnames"].items():
        if net.get("hide_name"):
            continue
        for index, bit in enumerate(net["bits"]):
            if bit not in bits:
                continue
            offset = net.get("offset", 0)
            names.append(name if len(net["bits"]) == 1 else f"{name}[{offset + index}]")
    return sorted(names)


def nextpnr_paths(report: dict, module: dict) -> list[dict]:
    result = []
    for path in report["critical_paths"]:
        if path["from"] != path["to"] or path["from"] == "<async>":
            continue
        delays = defaultdict(float)
        for arc in path["path"]:
            delays[arc["type"]] += arc["delay"]
        launch = next(a["from"] for a in path["path"] if a["type"] == "clk-to-q")
        latch = next(a["to"] for a in path["path"] if a["type"] == "setup")
        total = sum(delays.values())
        result.append({
            "clock": path["from"], "from": launch, "to": latch,
            "from_signals": register_signals(module, launch["cell"]),
            "to_signals": register_signals(module, latch["cell"]),
            "relationship_ns": path["max_delay"], "effective_setup_ns": total,
            "slack_ns": path["max_delay"] - total, "delays_ns": dict(delays),
            "logic_arcs": sum(a["type"] == "logic" for a in path["path"]),
        })
    if not result:
        raise ValueError("nextpnr report has no same-clock register paths")
    return result


def quartus_paths(report: str) -> list[dict]:
    chunks = re.split(r"Path #\d+: Setup slack is\s+([-+\d.]+)\s*\n", report)
    result = []
    for slack, body in zip(chunks[1::2], chunks[2::2]):
        rows = {}
        for line in body.splitlines():
            if line.startswith(";"):
                fields = [field.strip() for field in line.split(";")[1:-1]]
                if len(fields) > 1:
                    rows.setdefault(fields[0], fields[1:])
        relationship = float(rows["Setup Relationship"][0])
        result.append({
            "from": rows["From Node"][0], "to": rows["To Node"][0],
            "relationship_ns": relationship, "slack_ns": float(slack),
            "effective_setup_ns": relationship - float(slack),
            "data_delay_ns": float(rows["Data Delay"][0]),
            "logic_levels": int(rows["Number of Logic Levels"][1]),
        })
    if not result:
        raise ValueError("Quartus report has no setup paths")
    return result


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--timing", type=Path, required=True)
    parser.add_argument("--netlist", type=Path, required=True)
    parser.add_argument("--quartus", type=Path, action="append", required=True)
    args = parser.parse_args()
    inputs = [args.timing, args.netlist, *args.quartus]
    contents = {p: p.read_bytes() for p in inputs}
    report = json.loads(contents[args.timing])
    module = json.loads(contents[args.netlist])["modules"]["top"]
    print(json.dumps({
        "inputs_sha256": {str(p): hashlib.sha256(contents[p]).hexdigest() for p in inputs},
        "nextpnr_fmax": report["fmax"],
        "nextpnr": nextpnr_paths(report, module),
        "quartus": {str(p): quartus_paths(contents[p].decode()) for p in args.quartus},
    }, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
