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


def validation_gaps(entry: dict, fit: dict, inputs: dict) -> dict:
    """What a fit has not evidenced about itself.

    The absence of an error is not evidence that a check ran: a fit directory
    made before the envelope and clock-coverage checks existed carries neither.
    So each fit must show its real-pin count and both zero-valued TimeQuest
    coverage rows affirmatively, whether it passed or missed.
    """
    gaps = {name: fit[name] for name in ("timing_parse_error", "resource_parse_error",
                                         "clock_coverage_error") if fit.get(name)}
    if entry.get("real_pins") is None:
        gaps["real_pins"] = "no verified real-pin count"
    elif entry["real_pins"] != len(inputs["real_clocks"]):
        # Exactly, not at most: fewer real pins than real clocks means a clock
        # input went virtual or was removed, and a fit collected from before
        # the runner's equality check may never have been tested for it.
        gaps["real_pins"] = (f"{entry['real_pins']} real pins against "
                             f"{len(inputs['real_clocks'])} real clocks")
    rows = {name: (setup, hold) for name, setup, hold
            in (fit.get("unconstrained_clock_rows") or [])}
    if set(rows) != {"Illegal Clocks", "Unconstrained Clocks"}:
        gaps["clock_coverage"] = "no verified clock-coverage rows"
    elif any(int(setup) or int(hold) for setup, hold in rows.values()):
        gaps["clock_coverage"] = "TimeQuest reported unconstrained or illegal clocks"
    return gaps


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
        # Every fit's figures carry the evidence that its envelope was the
        # intended one and that the analysis covered every clock -- a passing
        # fit as much as a missed rung, since a fit whose VIRTUAL_PIN
        # assignments were ignored would pass while measuring something else.
        missing = validation_gaps(entry, fit, inputs)
        # A fit that stopped before the final report has counts from the route
        # or place stage. They are not a finished fit's figures and must not be
        # presented as unqualified ones.
        if missing and fit["status"] == "pass":
            entry["validation_errors"] = missing
            entry["resources_stage"] = (
                "passing fit without its own validation evidence ("
                + ", ".join(sorted(missing)) + "); figures are the fitter's but the envelope "
                "and clock coverage are not evidenced here")
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
            if missing:
                entry["validation_errors"] = missing
                entry["resources_stage"] = (
                    "fit whose own validation did not complete ("
                    + ", ".join(sorted(missing))
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


def timing_rows(name: str, output: Path, fit: dict) -> list[str]:
    """That fit's per-corner rows, prefixed with its name.

    A fit the record publishes as having timing -- a status, a corner count,
    per-corner slacks -- must have the rows those came from. Returning none
    silently would let the record carry a timing verdict with no evidence
    behind it, which is the shape of claim this record exists to avoid.
    """
    path = output / "timing.tsv"
    if not path.is_file():
        if "timing" in fit:
            raise RecordError(
                f"{name}: fit.json carries a timing result but {path} is missing, so the "
                "per-corner evidence behind it is not in the record")
        return []
    lines = path.read_text().splitlines()
    if len(lines) < 2:
        raise RecordError(f"{name}: {path} has a header and no rows")
    rows = lines[1:]
    # A truncated table is as misleading as a missing one: the verdict covers
    # every corner, so the rows must too. Count the distinct corners the rows
    # name and require the recorded corner count.
    corners = {row.split("\t")[0] for row in rows if row.strip()}
    expected = (fit.get("timing") or {}).get("corner_count")
    if expected is not None and len(corners) != expected:
        raise RecordError(
            f"{name}: fit.json records a timing verdict over {expected} corner(s) but "
            f"{path} has rows for {len(corners)}; the table is truncated and the verdict "
            "would be published without the evidence for every corner")
    return [f"{name}\t{row}" for row in rows]


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
        seen: set[str] = set()
        rows = ["fit\tcorner\thold_only\tcheck\tcoverage\tpaths\tworst_slack_ns\tfrom\tto"]
        for item in args.fit:
            if "=" not in item:
                raise RecordError(f"--fit wants NAME=DIR, not {item!r}")
            name, _, directory = item.partition("=")
            # A repeated name would leave two indistinguishable rows in
            # quartus.json and timing.tsv while quartus-inputs.json, which is
            # keyed by name, silently kept only the last.
            if not name:
                raise RecordError(f"--fit needs a name before the '=': {item!r}")
            if name in seen:
                raise RecordError(f"--fit name {name!r} is given twice; each fit needs its own name")
            seen.add(name)
            output = Path(directory).resolve()
            entry = collect(name, output)
            fits.append(entry)
            rows.extend(timing_rows(name, output, json.loads((output / "fit.json").read_text())))
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
