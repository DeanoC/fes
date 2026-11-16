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
        "virtual_port_count_as_run": inputs.get("virtual_port_count_as_run"),
        "virtual_port_count_note": inputs.get("virtual_port_count_note"),
        "real_pins": fit.get("real_pins"),
        "real_pins_note": fit.get("real_pins_note"),
        "unconstrained_clock_rows": fit.get("unconstrained_clock_rows"),
        "clock_coverage_note": fit.get("clock_coverage_note"),
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
            # A fit whose timing gate failed is a complete fit: its figures are
            # a finished fitter report, and only its verdict is a miss. Calling
            # that an incomplete fit would misdescribe a missing ladder rung,
            # whose area is exactly what a reader wants beside the ones that
            # closed.
            timing = fit.get("timing", {})
            # A missed rung is only a usable upper bound if its analysis
            # covered every clock and its envelope was the intended one. When
            # either check failed the runner leaves its error behind, and that
            # error must travel with the figures rather than be dropped while
            # the rung is published as a valid bound.
            invalid = [name for name in ("timing_parse_error", "resource_parse_error",
                                         "clock_coverage_error")
                       if fit.get(name)]
            # The absence of an error is not evidence that a check ran. A fit
            # directory made before those checks existed carries neither the
            # evidence nor an error, so a missed rung must show its pin count
            # and both zero-valued coverage rows affirmatively.
            if entry.get("real_pins") is None:
                invalid.append("no verified real-pin count")
            elif entry["real_pins"] > len(inputs["real_clocks"]):
                invalid.append(f"{entry['real_pins']} real pins against "
                               f"{len(inputs['real_clocks'])} real clocks")
            rows = {name: (setup, hold) for name, setup, hold
                    in (fit.get("unconstrained_clock_rows") or [])}
            if set(rows) != {"Illegal Clocks", "Unconstrained Clocks"}:
                invalid.append("no verified clock-coverage rows")
            elif any(int(setup) or int(hold) for setup, hold in rows.values()):
                invalid.append("TimeQuest reported unconstrained or illegal clocks")
            if invalid:
                entry["validation_errors"] = {name: fit.get(name, name) for name in invalid}
                entry["resources_stage"] = (
                    "fit whose own validation did not complete (" + ", ".join(invalid)
                    + "); NOT usable as a timing bound or an area figure")
            elif timing.get("status") == "fail":
                entry["resources_stage"] = (
                    "complete fit; the timing gate failed, so this is a missed rung and not a "
                    "partial result")
            else:
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
        # Only when EVERY fit supplies a commit: one fit with a revision beside
        # others with none would otherwise set the aggregate and read as
        # agreement, attributing the unversioned fits to that checkout.
        commits = [commit(fit) for fit in fits]
        revisions = sorted({value for value in commits if value})
        agreed = bool(revisions) and len(revisions) == 1 and all(commits)
        dirty = sorted(fit["fit"] for fit in fits if fit.get("source_clean") is False)
        unrecorded = sorted(fit["fit"] for fit in fits if not commit(fit))
        hosts = sorted({fit["host"] for fit in fits if fit["host"]})
        (record / "host.json").write_text(json.dumps({
            "fes_revision": revisions[0] if agreed else None,
            "fit_revisions": revisions,
            "collected_at_revision": collected_at,
            "fits_from_a_modified_checkout": dirty,
            "fits_with_no_recorded_revision": unrecorded,
            "revision_note": ("every fit was prepared from one revision"
                              if agreed else
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
