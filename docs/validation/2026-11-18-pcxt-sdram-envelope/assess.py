#!/usr/bin/env python3
"""Recalculate the retained diagnostic against a cross-vendor pin envelope.

This arithmetic assessment cannot issue a physical timing or production PASS.
Exit 2 means qualification is blocked; malformed or changed evidence raises.
"""

import argparse
import csv
import hashlib
import json
from pathlib import Path


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def assess(fit):
    here = Path(__file__).resolve().parent
    reference = here.parent / "2026-11-17-pcxt-capture-path/coordinated/result.json"
    limits = json.loads((here / "devices.json").read_text())
    original = json.loads(reference.read_text())
    channels = fit / "conditional/channels.tsv"
    if digest(channels) != original["report_sha256"]["conditional/channels.tsv"]:
        raise ValueError("retained channel report changed")
    if digest(fit / "output_files/top.rbf") != original["rbf_sha256"]:
        raise ValueError("retained diagnostic payload changed")
    rows = list(csv.DictReader(channels.open(), delimiter="\t"))
    if len(rows) != 568:
        raise ValueError("incomplete channel inventory")
    # For a common controller use the most demanding requirement, not the
    # fastest chip's allowance. Output hold is a device guarantee: take MIN.
    devices = [device["pin_ns"] for device in limits["devices"]]
    envelope = {
        key: (min if key == "tOH_min" else max)(d[key] for d in devices)
        for key in devices[0]
    }
    groups = {
        "read": [r for r in rows if r["role"] == "read"],
        "command": [r for r in rows if r["role"] == "write"
                    and r["port"].startswith(("SDRAM_A[", "SDRAM_BA[", "SDRAM_n"))],
        "second_cs": [r for r in rows if r["role"] == "second_chip_select"],
    }
    margins = {}
    for name, group in groups.items():
        margins[name] = {}
        for kind in ("setup", "hold"):
            selected = [r for r in group if r["kind"] == kind]
            if name == "read":
                # Preserve #1954's selected-word relationship and raw failures.
                delta = (10 - (envelope["tAC_max"] - 6) if kind == "setup"
                         else -10 + envelope["tOH_min"] - 2.5)
            else:
                delta = (-(envelope["tIS_min"] - 1.5) if kind == "setup"
                         else -(envelope["tIH_min"] - 0.8))
            worst = min(selected, key=lambda r: float(r["slack_ns"]))
            margins[name][kind] = {
                "reference_margin_ns": round(float(worst["slack_ns"]) + delta, 3),
                "raw_margin_ns": float(worst["slack_ns"]),
                "corner": worst["corner"], "port": worst["port"],
                "rows": len(selected),
            }
    return {
        "classification": "conditional arithmetic on retained diagnostic; no new STA or fit",
        "envelope_ns": envelope,
        "reference_margins": margins,
        "nominal_cl2_cycle_headroom_ns": round(10 - envelope["tCK_CL2_min"], 3),
        "nominal_cl3_cycle_headroom_ns": round(10 - envelope["tCK_CL3_min"], 3),
        "cl3_note": "device cycle limit only; CL3 is not implemented or qualified here",
        "physical_timing": "BLOCKED",
        "production_build_authorized_by_timing": False,
        "production_seal": "NOT ATTEMPTED",
        "hil": "NOT STARTED",
        "default_off": True,
        "missing_bounds": limits["missing_bounds"],
        "inputs_sha256": {
            "devices.json": digest(here / "devices.json"),
            "prior_result.json": digest(reference),
            "conditional/channels.tsv": digest(channels),
            "diagnostic.rbf": digest(fit / "output_files/top.rbf"),
            "native_sta_report": digest(fit / "output_files/top.sta.rpt"),
        },
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fit", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    result = assess(args.fit)
    args.output.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print("BLOCKED: board electrical/clock/turnaround bounds are missing; no seal or HIL")
    raise SystemExit(2)
