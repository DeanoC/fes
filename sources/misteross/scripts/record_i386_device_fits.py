#!/usr/bin/env python3
"""Collect scripts/fit_i386_device.py runs into a docs/validation record.

Reads each fit's own inputs.json, netlist.json, fit.json and timing.tsv and
writes the record's host.json, quartus.json, quartus-inputs.json and
timing.tsv. It copies figures; it never computes one, and it never puts a
figure from one device beside a figure from another.

  python3 scripts/record_i386_device_fits.py --record docs/validation/<dated> \
      --fit cpu-de25=/abs/out/cpu-de25-61.44-s1 --fit cpu-de10=/abs/...
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
import platform
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
REPO = ROOT.parents[1]


class RecordError(RuntimeError):
    pass


def read(path: Path) -> dict:
    try:
        return json.loads(path.read_text())
    except (OSError, ValueError) as error:
        raise RecordError(f"cannot read {path}: {error}") from error


def collect(name: str, output: Path) -> dict:
    fit = read(output / "fit.json")
    inputs = read(output / "inputs.json")
    netlist = read(output / "netlist.json")
    entry = {
        "fit": name, "status": fit["status"], "top": fit["top"],
        "board": inputs["board"], "device": fit["device"], "family": fit["family"],
        "documented_device_alms": inputs["documented_device_alms"],
        "ram_block": inputs["ram_block"],
        "quartus": fit["quartus"]["release"], "quartus_edition": fit["quartus"]["edition"],
        "quartus_lock": fit["quartus"]["lock"], "seed": fit["seed"],
        "period_ns": fit["period_ns"], "clock_hz": inputs["clock_hz"],
        "real_clocks": inputs["real_clocks"], "virtual_port_count": inputs.get("virtual_port_count"),
        "real_pins": fit.get("real_pins"),
        "host": fit.get("host") or inputs.get("host"),
        "fes_revision": inputs.get("fes_revision"),
        "source_clean": (inputs["fes_revision"].get("clean")
                         if isinstance(inputs.get("fes_revision"), dict) else None),
        "netlist": netlist,
        "classification": fit["classification"],
        "hardware_acceptance": fit["hardware_acceptance"],
        "programming_artifact": fit["programming_artifact"],
    }
    if "resources" in fit:
        entry["resources"] = fit["resources"]
        # A fit that stopped before the final report has counts from the route
        # or place stage. They are not a finished fit's figures and must not be
        # presented as unqualified ones.
        if "resources_stage" in fit:
            entry["resources_stage"] = fit["resources_stage"]
        elif fit["status"] != "pass":
            entry["resources_stage"] = "incomplete fit; stage not recorded"
    if "timing" in fit:
        entry["timing_status"] = fit["timing"].get("status")
        entry["corner_count"] = fit["timing"].get("corner_count")
        entry["fmax_estimate_by_corner"] = fit["timing"].get("fmax")
    if "error" in fit:
        entry["error"] = fit["error"]
    return entry


def timing_rows(name: str, output: Path) -> list[str]:
    path = output / "timing.tsv"
    if not path.is_file():
        return []
    lines = path.read_text().splitlines()
    return [f"{name}\t{line}" for line in lines[1:]]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__,
                                     formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--record", type=Path, required=True)
    parser.add_argument("--fit", action="append", required=True, metavar="NAME=DIR")
    args = parser.parse_args(argv)
    try:
        record = (REPO / args.record).resolve() if not args.record.is_absolute() else args.record
        record.mkdir(parents=True, exist_ok=True)
        fits = []
        rows = ["fit\tcorner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto"]
        for item in args.fit:
            if "=" not in item:
                raise RecordError(f"--fit wants NAME=DIR, not {item!r}")
            name, _, directory = item.partition("=")
            output = Path(directory).resolve()
            fits.append(collect(name, output))
            rows.extend(timing_rows(name, output))
        # Each fit records the revision it was prepared from. The collector's
        # own HEAD is not that revision if the checkout moved in between, so it
        # is reported separately and a disagreement is named, never smoothed.
        collected_at = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=REPO, text=True).strip()
        def commit(fit):
            value = fit.get("fes_revision")
            return value.get("commit") if isinstance(value, dict) else value
        revisions = sorted({commit(fit) for fit in fits if commit(fit)})
        dirty = sorted(fit["fit"] for fit in fits if fit.get("source_clean") is False)
        unrecorded = sorted(fit["fit"] for fit in fits if not commit(fit))
        hosts = sorted({fit["host"] for fit in fits if fit["host"]})
        (record / "host.json").write_text(json.dumps({
            "fes_revision": revisions[0] if len(revisions) == 1 else None,
            "fit_revisions": revisions,
            "collected_at_revision": collected_at,
            "fits_from_a_modified_checkout": dirty,
            "fits_with_no_recorded_revision": unrecorded,
            "revision_note": ("every fit was prepared from one revision"
                              if len(revisions) == 1 else
                              "the fits were prepared from different revisions, or some predate "
                              "the revision being recorded; see each fit's own fes_revision")
                              + (". A commit alone does not describe a fit prepared from a modified "
                                 "checkout: those fits are named in fits_from_a_modified_checkout "
                                 "and each carries its own tracked diff" if dirty else ""),
            "hosts": hosts,
            "uname": platform.uname()._asdict(),
            "evidence_scope": "internal virtual-pin characterisation fits; no board I/O, no seal, "
                              "no programming artifact and no hardware acceptance",
            "comparison_note": "Cyclone V ALMs and Agilex 5 ALMs are different units, and M10K and "
                               "M20K are different blocks. Figures are reported per device with that "
                               "device's totals. No cross-device ratio is computed here.",
        }, indent=2, sort_keys=True) + "\n")
        (record / "quartus.json").write_text(json.dumps(
            {"fits": fits}, indent=2, sort_keys=True) + "\n")
        (record / "quartus-inputs.json").write_text(json.dumps(
            {fit["fit"]: {"netlist": fit["netlist"], "seed": fit["seed"],
                          "period_ns": fit["period_ns"], "quartus": fit["quartus"],
                          "device": fit["device"]} for fit in fits},
            indent=2, sort_keys=True) + "\n")
        (record / "timing.tsv").write_text("\n".join(rows) + "\n")
        print(f"record written: {record} ({len(fits)} fits)")
        return 0
    except (OSError, RecordError, subprocess.SubprocessError, ValueError) as error:
        print(str(error), file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
