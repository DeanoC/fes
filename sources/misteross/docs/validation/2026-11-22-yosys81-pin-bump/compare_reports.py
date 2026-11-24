#!/usr/bin/env python3
"""Compare functional i386 lockstep/frontend evidence across Yosys pins."""
import json
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent / "reports"


def without_provenance(value, report_kind):
    if isinstance(value, dict):
        result = {}
        for key, item in value.items():
            if key == "verilator":
                continue
            if key == "netlist":
                continue
            if key == "tracked_changes" and report_kind == "lockstep":
                continue
            result[key] = without_provenance(item, report_kind)
        return result
    if isinstance(value, list):
        return [without_provenance(item, report_kind) for item in value]
    return value


def compare(stem, kind):
    old = json.loads((ROOT / f"{stem}-old.json").read_text())
    new = json.loads((ROOT / f"{stem}-new.json").read_text())
    if without_provenance(old, kind) != without_provenance(new, kind):
        print(f"{stem}: functional report differs", file=sys.stderr)
        return False
    if kind == "lockstep":
        if not old["run"]["passed"] or not new["run"]["passed"]:
            print(f"{stem}: one run did not pass", file=sys.stderr)
            return False
        old_hash = old["netlist"]["netlist_sha256"]
        new_hash = new["netlist"]["netlist_sha256"]
        print(f"{stem}: functional reports match; netlist sha256 old={old_hash} new={new_hash}")
    else:
        print(f"{stem}: functional reports match; netlist/tool provenance differs as expected")
    return True


if __name__ == "__main__":
    ok = compare("lockstep", "lockstep") and compare("frontend", "frontend")
    raise SystemExit(0 if ok else 1)
